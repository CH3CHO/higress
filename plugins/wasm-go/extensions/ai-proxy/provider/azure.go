package provider

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type azureServiceUrlType int

const (
	pathAzurePrefix                      = "/openai"
	pathAzureModelPlaceholder            = "{model}"
	pathAzureLegacyWithDeploymentsPrefix = "/openai/deployments/"
	pathAzureLegacyWithModelPrefix       = "/openai/deployments/" + pathAzureModelPlaceholder
	queryAzureApiVersion                 = "api-version"
)

const (
	azureServiceUrlTypeFull azureServiceUrlType = iota
	azureServiceUrlTypeWithDeployment
	azureServiceUrlTypeDomainOnly
)

const (
	requestFieldMaxTokens           = "max_tokens"
	requestFieldMaxCompletionTokens = "max_completion_tokens"
	requestFieldReasoningEffort     = "reasoning_effort"
	requestFieldExtraBody           = "extra_body"
	requestFieldResponseFormat      = "response_format"
	requestFieldTools               = "tools"
	requestFieldToolChoice          = "tool_choice"
	requestFieldEncodingFormat      = "encoding_format"

	encodingFormatBase64 = "base64"

	responseFormatToolName    = "json_tool_call"
	responseFormatToolChoice  = "{\"type\":\"function\",\"function\":{\"name\":\"" + responseFormatToolName + "\"}}"
	responseFormatToolsFormat = "[{\"type\":\"function\",\"function\":{\"name\":\"" + responseFormatToolName + "\",\"parameters\":%s}}]"

	minApiVersionYearSupportingResponseFormat  = 2024
	minApiVersionMonthSupportingResponseFormat = 8

	ctxKeyResponseFormatTransformed = "azure_response_format_transformed"
	ctxKeyBase64FormatCoerced       = "azure_base64_format_coerced"
)

var (
	azureModelIrrelevantApis = map[ApiName]bool{
		ApiNameModels:              true,
		ApiNameBatches:             true,
		ApiNameRetrieveBatch:       true,
		ApiNameCancelBatch:         true,
		ApiNameFiles:               true,
		ApiNameRetrieveFile:        true,
		ApiNameRetrieveFileContent: true,
	}
	regexAzureModelWithPath    = regexp.MustCompile("/openai/deployments/(.+?)(?:/(.*)|$)")
	azureRequestFieldBlacklist = []string{
		"timeout",
		"stream_timeout",
		"fallback",
		"thinking",
		"enable_thinking",
		"cache",
		"max_retries",
		"api_base",
		"deployment_id",
		"api_version",
		"api_key",
		"organization",
		"base_url",
		"default_headers",
	}
	azureUseMaxCompletionTokensModelKeywords = []string{
		"o1",
		"o3",
		"o4",
		"gpt-5",
	}
	azureReasoningEffortSupportedModelKeywords = []string{
		"o1",
		"o3",
		"o4",
		"gpt-5",
	}
	azureReasoningEffortUnsupportedModelKeywords = []string{
		"gpt-5-chat",
	}
	azureTemperature1OnlyModelKeywords = []string{ // Models that only support temperature=1
		"o1",
		"o3",
		"o4",
		"gpt-5",
	}
	azureResponseFormatSupportedModelKeywords = []string{
		"4o",
	}
	azureNullRequestFieldBlacklist = map[ApiName][]string{
		// If a field in the list has null as its value,
		// it shall be deleted from the request.
		ApiNameEmbeddings: {
			"encoding_format",
		},
	}
)

// azureProvider is the provider for Azure OpenAI service.
type azureProviderInitializer struct {
}

func (m *azureProviderInitializer) DefaultCapabilities() map[string]string {
	var capabilities = map[string]string{}
	for k, v := range (&openaiProviderInitializer{}).DefaultCapabilities() {
		if !strings.HasPrefix(v, PathOpenAIPrefix) {
			log.Warnf("azureProviderInitializer: capability %s has an unexpected path %s, skipping", k, v)
			continue
		}
		path := strings.TrimPrefix(v, PathOpenAIPrefix)
		if azureModelIrrelevantApis[ApiName(k)] {
			path = pathAzurePrefix + path
		} else {
			path = pathAzureLegacyWithModelPrefix + path
		}
		capabilities[k] = path
		log.Debugf("azureProviderInitializer: capability %s -> %s", k, path)
	}
	return capabilities
}

func (m *azureProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if config.azureServiceUrl == "" {
		return errors.New("missing azureServiceUrl in provider config")
	}
	if azureServiceUrl, err := url.Parse(config.azureServiceUrl); err != nil {
		return fmt.Errorf("invalid azureServiceUrl: %w", err)
	} else if strings.HasPrefix(azureServiceUrl.RawPath, pathAzureLegacyWithDeploymentsPrefix) && !azureServiceUrl.Query().Has(queryAzureApiVersion) {
		return fmt.Errorf("missing %s query parameter in azureServiceUrl: %s", queryAzureApiVersion, config.azureServiceUrl)
	}
	if config.apiTokens == nil || len(config.apiTokens) == 0 {
		return errors.New("no apiToken found in provider config")
	}
	return nil
}

func (m *azureProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	var serviceUrl *url.URL
	if u, err := url.Parse(config.azureServiceUrl); err != nil {
		return nil, fmt.Errorf("invalid azureServiceUrl: %w", err)
	} else {
		serviceUrl = u
	}

	modelSubMatch := regexAzureModelWithPath.FindStringSubmatch(serviceUrl.Path)
	defaultModel := "placeholder"
	var serviceUrlType azureServiceUrlType
	if modelSubMatch != nil {
		defaultModel = modelSubMatch[1]
		if modelSubMatch[2] != "" {
			serviceUrlType = azureServiceUrlTypeFull
		} else {
			serviceUrlType = azureServiceUrlTypeWithDeployment
		}
		log.Debugf("azureProvider: found default model from serviceUrl: %s", defaultModel)
	} else {
		serviceUrlType = azureServiceUrlTypeDomainOnly
		log.Debugf("azureProvider: no default model found in serviceUrl")
	}
	log.Debugf("azureProvider: serviceUrlType=%d", serviceUrlType)

	config.setDefaultCapabilities(m.DefaultCapabilities())
	apiVersion := serviceUrl.Query().Get(queryAzureApiVersion)
	log.Debugf("azureProvider: using %s: %s", queryAzureApiVersion, apiVersion)
	apiVersionYear, apiVersionMonth, err := parseAzureApiVersion(apiVersion)
	if err != nil {
		log.Warnf("azureProvider: failed to parse api version %s: %v", apiVersion, err)
	}
	return &azureProvider{
		config:             config,
		serviceUrl:         serviceUrl,
		serviceUrlType:     serviceUrlType,
		serviceUrlFullPath: serviceUrl.Path + "?" + serviceUrl.RawQuery,
		apiVersion:         apiVersion,
		apiVersionYear:     apiVersionYear,
		apiVersionMonth:    apiVersionMonth,
		defaultModel:       defaultModel,
		contextCache:       createContextCache(&config),
	}, nil
}

type azureProvider struct {
	config ProviderConfig

	contextCache       *contextCache
	serviceUrl         *url.URL
	serviceUrlFullPath string
	serviceUrlType     azureServiceUrlType
	apiVersion         string
	apiVersionYear     int
	apiVersionMonth    int
	defaultModel       string
}

func (m *azureProvider) GetProviderType() string {
	return providerTypeAzure
}

func (m *azureProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	m.config.handleRequestHeaders(m, ctx, apiName)
	return nil
}

func (m *azureProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	return m.config.handleRequestBody(m, m.contextCache, ctx, apiName, body)
}

func (m *azureProvider) TransformRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (transformedBody []byte, err error) {
	transformedBody = body
	err = nil

	transformedBody, err = m.config.defaultTransformRequestBody(ctx, apiName, body)
	if err != nil {
		return
	}

	// This must be called after the body is transformed, because it uses the model from the context filled by that call.
	if path := m.transformRequestPath(ctx, apiName); path != "" {
		err = util.OverwriteRequestPath(path)
		if err == nil {
			log.Debugf("azureProvider: overwrite request path to %s succeeded", path)
		} else {
			log.Errorf("azureProvider: overwrite request path to %s failed: %v", path, err)
		}
	}

	// This must be called after the body is transformed, because it uses the model from the context filled by that call.
	transformedBody, err = m.transformRequestFields(ctx, apiName, transformedBody)
	if err != nil {
		log.Errorf("azureProvider: transform request fields failed: %v", err)
	}

	return
}

func (m *azureProvider) transformRequestFields(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	needReadResponseBody := false

	defer func() {
		if !needReadResponseBody {
			ctx.DontReadResponseBody()
		}
	}()

	// Expand extra_body field if exists before everything else
	body = expandExtraBodyField(body)

	// Remove fields not supported by Azure OpenAI
	for _, field := range azureRequestFieldBlacklist {
		if transformedBody, err := sjson.DeleteBytes(body, field); err != nil {
			return body, fmt.Errorf("azureProvider: failed to delete %s from request body, err: %v", field, err)
		} else {
			body = transformedBody
		}
	}

	if transformedBody, err := deleteNullValueFields(body, azureNullRequestFieldBlacklist[apiName]); err != nil {
		return body, fmt.Errorf("azureProvider: failed to delete blacklisted request fields with null value from request body: %v", err)
	} else {
		body = transformedBody
	}

	// Some newer models hosted by Azure uses max_completion_tokens instead of max_tokens,
	// So we need to make the conversion here.
	model := ""
	if m.serviceUrlType != azureServiceUrlTypeWithDeployment && m.serviceUrlType != azureServiceUrlTypeFull {
		model = ctx.GetStringContext(ctxKeyFinalRequestModel, "")
		log.Debugf("azureProvider: final request model from context %s", model)
	}
	if model == "" {
		log.Debugf("azureProvider: use default model %s", model)
		model = m.defaultModel
	}
	model = strings.ToLower(model)

	switch apiName {
	case ApiNameCompletion:
		if transformedBody, needReadResponseBodyLocal, err := m.transformCompletionsRequestFields(ctx, model, body); err != nil {
			return body, fmt.Errorf("azureProvider: transform completion request fields failed: %v", err)
		} else {
			needReadResponseBody = needReadResponseBodyLocal
			return transformedBody, nil
		}
	case ApiNameChatCompletion:
		if transformedBody, needReadResponseBodyLocal, err := m.transformChatCompletionRequestFields(ctx, model, body); err != nil {
			return body, fmt.Errorf("azureProvider: transform chat completion request fields failed: %v", err)
		} else {
			needReadResponseBody = needReadResponseBodyLocal
			return transformedBody, nil
		}
	case ApiNameEmbeddings:
		if transformedBody, needReadResponseBodyLocal, err := m.transformEmbeddingRequestFields(ctx, model, body); err != nil {
			return body, fmt.Errorf("azureProvider: transform chat completion request fields failed: %v", err)
		} else {
			needReadResponseBody = needReadResponseBodyLocal
			return transformedBody, nil
		}
	}

	return body, nil
}

func (m *azureProvider) transformCompletionsRequestFields(ctx wrapper.HttpContext, model string, body []byte) (_ []byte, needReadResponseBody bool, err error) {
	return transformCompletionsRequestFields(ctx, body)
}

func (m *azureProvider) transformChatCompletionRequestFields(ctx wrapper.HttpContext, model string, body []byte) (_ []byte, needReadResponseBody bool, err error) {
	needReadResponseBody = false

	log.Debugf("azureProvider: transformRequestFields for model %s", model)
	if containsKeywordInModel(model, azureUseMaxCompletionTokensModelKeywords) {
		log.Debugf("azureProvider: model %s requires %s field instead of %s", model, requestFieldMaxCompletionTokens, requestFieldMaxTokens)
		if maxTokens := gjson.GetBytes(body, requestFieldMaxTokens); maxTokens.Exists() {
			log.Debugf("azureProvider: removing %s from request body for model %s", requestFieldMaxTokens, model)
			if transformedBody, err := sjson.DeleteBytes(body, requestFieldMaxTokens); err != nil {
				return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to delete %s in request body, err: %v", requestFieldMaxTokens, err)
			} else {
				body = transformedBody
			}
			log.Debugf("azureProvider: setting %s into request body for model %s: %s", requestFieldMaxCompletionTokens, model, maxTokens.Raw)
			if transformedBody, err := sjson.SetRawBytes(body, requestFieldMaxCompletionTokens, []byte(maxTokens.Raw)); err != nil {
				return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to set %s into request body, value: %s err: %v", requestFieldMaxTokens, maxTokens.Raw, err)
			} else {
				body = transformedBody
			}
		}
	}

	if !containsKeywordInModel(model, azureReasoningEffortSupportedModelKeywords) || containsKeywordInModel(model, azureReasoningEffortUnsupportedModelKeywords) {
		log.Debugf("azureProvider: model %s doesn't support %s", model, requestFieldReasoningEffort)
		if transformedBody, err := sjson.DeleteBytes(body, requestFieldReasoningEffort); err != nil {
			return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to delete %s in request body, err: %v", requestFieldReasoningEffort, err)
		} else {
			body = transformedBody
		}
	}

	if containsKeywordInModel(model, azureTemperature1OnlyModelKeywords) {
		temperature := gjson.GetBytes(body, "temperature")
		if temperature.Exists() && temperature.Num != 1 {
			log.Debugf("azureProvider: model %s only supports temperature=1, which is %s in the request, removing temperature from request body", model, temperature.Raw)
			if transformedBody, err := sjson.DeleteBytes(body, "temperature"); err != nil {
				return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to delete temperature which isn't 1 from request body, err: %v", err)
			} else {
				body = transformedBody
			}
		}
	}

	if !m.isResponseFormatSupported(model) {
		log.Debugf("azureProvider: model %s and api-version %s doesn't support response_format parameter natively, transforming to tool_call", model, m.apiVersion)
		if transformedBody, err := m.transformResponseFormatParam(body); err != nil {
			return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to transform response_schema to tool_call in request body, err: %v", err)
		} else {
			body = transformedBody
			if stream := gjson.GetBytes(body, "stream"); !stream.Exists() || stream.Type != gjson.True {
				// Only need to process response body when it's not a streaming request
				ctx.SetContext(ctxKeyResponseFormatTransformed, true)
				needReadResponseBody = true
			}
		}
	} else {
		log.Debugf("azureProvider: model %s and api-version %s supports response_format parameter natively", model, m.apiVersion)
	}

	return body, needReadResponseBody, nil
}

func (m *azureProvider) transformEmbeddingRequestFields(ctx wrapper.HttpContext, model string, body []byte) (_ []byte, needReadResponseBody bool, err error) {
	needReadResponseBody = false

	log.Debugf("azureProvider: transformRequestFields for embedding model %s", model)
	if encodingFormat := gjson.GetBytes(body, requestFieldEncodingFormat); !encodingFormat.Exists() {
		log.Debugf("azureProvider: no %s field in embedding request body, set encoding_format to base64 to improve precision", requestFieldEncodingFormat)
		if transformedBody, err := sjson.SetBytes(body, requestFieldEncodingFormat, encodingFormatBase64); err != nil {
			return body, needReadResponseBody, fmt.Errorf("azureProvider: failed to set %s in request body, err: %v", requestFieldEncodingFormat, err)
		} else {
			body = transformedBody
			ctx.SetContext(ctxKeyBase64FormatCoerced, true)
			needReadResponseBody = true
		}
	}

	return body, needReadResponseBody, nil
}

func (m *azureProvider) transformRequestPath(ctx wrapper.HttpContext, apiName ApiName) string {
	originalPath := util.GetOriginalRequestPath()

	if m.config.IsOriginal() {
		return originalPath
	}

	if m.serviceUrlType == azureServiceUrlTypeFull {
		log.Debugf("azureProvider: use configured path %s", m.serviceUrlFullPath)
		return m.serviceUrlFullPath
	}

	log.Debugf("azureProvider: original request path: %s", originalPath)
	effectiveApiName := apiName
	if effectiveApiName == ApiNameCompletion {
		// Azure OpenAI doesn't have a separate completion API,
		// so we need to convert it to chat completion API.
		effectiveApiName = ApiNameChatCompletion
	}
	path := util.MapRequestPathByCapability(string(effectiveApiName), originalPath, m.config.capabilities)
	log.Debugf("azureProvider: path: %s", path)
	if strings.Contains(path, pathAzureModelPlaceholder) {
		log.Debugf("azureProvider: path contains placeholder: %s", path)
		var model string
		if m.serviceUrlType == azureServiceUrlTypeWithDeployment {
			model = m.defaultModel
		} else {
			model = ctx.GetStringContext(ctxKeyFinalRequestModel, "")
			log.Debugf("azureProvider: model from context: %s", model)
			if model == "" {
				model = m.defaultModel
				log.Debugf("azureProvider: use default model: %s", model)
			}
		}
		path = strings.ReplaceAll(path, pathAzureModelPlaceholder, model)
		log.Debugf("azureProvider: model replaced path: %s", path)
	}
	path = path + "?" + m.serviceUrl.RawQuery
	log.Debugf("azureProvider: final path: %s", path)

	return path
}

func (m *azureProvider) isResponseFormatSupported(model string) bool {
	if !containsKeywordInModel(model, azureResponseFormatSupportedModelKeywords) {
		return false
	}
	if m.apiVersionYear < minApiVersionYearSupportingResponseFormat {
		return false
	}
	if m.apiVersionYear == minApiVersionYearSupportingResponseFormat && m.apiVersionMonth < minApiVersionMonthSupportingResponseFormat {
		return false
	}
	return true
}

func (m *azureProvider) transformResponseFormatParam(body []byte) ([]byte, error) {
	responseFormat := gjson.GetBytes(body, requestFieldResponseFormat)
	if !responseFormat.Exists() || !responseFormat.IsObject() {
		log.Debugf("azureProvider: " + requestFieldResponseFormat + " not found or not an object, skipping transformation")
		return body, nil
	}
	responseSchema := responseFormat.Get("response_schema")
	if !responseSchema.Exists() {
		responseSchema = responseFormat.Get("json_schema.schema")
	}
	if !responseSchema.Exists() || !responseSchema.IsObject() {
		log.Debugf("azureProvider: " + requestFieldResponseFormat + " doesn't contain response_schema or json_schema.schema, skipping transformation")
		return body, nil
	}

	log.Debugf("azureProvider: transforming response_format to tool_call in request body")

	tools := fmt.Sprintf(responseFormatToolsFormat, responseSchema.Raw)
	if transformedBody, err := sjson.SetRawBytes(body, requestFieldTools, []byte(tools)); err != nil {
		return body, fmt.Errorf("azureProvider: failed to set "+requestFieldTools+" in request body, err: %v", err)
	} else {
		body = transformedBody
	}

	if transformedBody, err := sjson.SetRawBytes(body, requestFieldToolChoice, []byte(responseFormatToolChoice)); err != nil {
		return body, fmt.Errorf("azureProvider: failed to set "+requestFieldToolChoice+" in request body, err: %v", err)
	} else {
		body = transformedBody
	}

	if transformedBody, err := sjson.DeleteBytes(body, requestFieldResponseFormat); err != nil {
		return body, fmt.Errorf("azureProvider: failed to delete "+requestFieldResponseFormat+" in request body, err: %v", err)
	} else {
		body = transformedBody
	}

	return body, nil
}

func (m *azureProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	// We need to overwrite the request path in the request headers stage,
	// because for some APIs, we don't read the request body and the path is model irrelevant.
	if overwrittenPath := m.transformRequestPath(ctx, apiName); overwrittenPath != "" {
		util.OverwriteRequestPathHeader(headers, overwrittenPath)
	}
	util.OverwriteRequestHostHeader(headers, m.serviceUrl.Host)
	headers.Set("api-key", m.config.GetApiTokenInUse(ctx))
	headers.Del("Content-Length")

	if !m.config.isSupportedAPI(apiName) || !m.config.needToProcessRequestBody(apiName) {
		// If the API is not supported or there is no need to process the body,
		// we should not read the request body and keep it as it is.
		ctx.DontReadRequestBody()
	}
}

func (m *azureProvider) OnStreamingEvent(ctx wrapper.HttpContext, name ApiName, event StreamEvent) ([]StreamEvent, error) {
	if name == ApiNameCompletion {
		return handleCompletionsStreamingEvent(ctx, event)
	}
	return nil, nil
}

func (m *azureProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	switch apiName {
	case ApiNameCompletion:
		return transformCompletionsResponseFields(ctx, body)
	case ApiNameChatCompletion:
		return m.transformChatCompletionResponseFields(ctx, body)
	case ApiNameEmbeddings:
		return m.transformEmbeddingsResponseFields(ctx, body)
	}
	return body, nil
}

func (m *azureProvider) transformChatCompletionResponseFields(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	if !ctx.GetBoolContext(ctxKeyResponseFormatTransformed, false) {
		return body, nil
	}
	choices := gjson.GetBytes(body, "choices")
	if !choices.Exists() || !choices.IsArray() {
		log.Debugf("azureProvider: choices not found or not an array in response body, skipping transformation")
		return body, nil
	}
	for i, choice := range choices.Array() {
		toolCall := choice.Get("message.tool_calls")
		if !toolCall.Exists() || !toolCall.IsArray() || len(toolCall.Array()) != 1 {
			log.Debugf("azureProvider: tool_calls not found or not an array of length 1 in choice %d, skipping transformation", i)
			continue
		}
		firstToolCall := toolCall.Array()[0]
		if firstToolCall.Get("function.name").String() != responseFormatToolName {
			log.Debugf("azureProvider: first tool_call name is not %s in choice %d, skipping transformation", responseFormatToolName, i)
			continue
		}
		if transformedBody, err := sjson.DeleteBytes(body, fmt.Sprintf("choices.%d.message.tool_calls", i)); err != nil {
			return body, fmt.Errorf("azureProvider: failed to delete tool_calls in choice %d, err: %v", i, err)
		} else {
			body = transformedBody
		}
		arguments := firstToolCall.Get("function.arguments")
		if transformedBody, err := sjson.SetRawBytes(body, fmt.Sprintf("choices.%d.message.content", i), []byte(arguments.Raw)); err != nil {
			return body, fmt.Errorf("azureProvider: failed to set content in choice %d, err: %v", i, err)
		} else {
			body = transformedBody
		}
	}
	return body, nil
}

func (m *azureProvider) transformEmbeddingsResponseFields(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	if !ctx.GetBoolContext(ctxKeyBase64FormatCoerced, false) {
		return body, nil
	}
	data := gjson.GetBytes(body, "data")
	if !data.Exists() || !data.IsArray() {
		log.Debugf("azureProvider: data not found or not an array in response body, skipping transformation")
		return body, nil
	}
	for i, item := range data.Array() {
		embedding := item.Get("embedding")
		if !embedding.Exists() || embedding.Type != gjson.String {
			log.Debugf("azureProvider: embedding not found or not a string in data item %d, skipping transformation", i)
			continue
		}
		if decoded, err := decodeBase64FormatEmbeddingToJsonRaw(embedding.String()); err != nil {
			log.Warnf("azureProvider: failed to decode base64 embedding in data item %d, err: %v, skipping transformation", i, err)
		} else {
			if transformedBody, err := sjson.SetRawBytes(body, fmt.Sprintf("data.%d.embedding", i), decoded); err != nil {
				return body, fmt.Errorf("azureProvider: failed to set decoded embedding in data item %d, err: %v", i, err)
			} else {
				body = transformedBody
			}
		}
	}
	return body, nil
}

func decodeBase64FormatEmbeddingToJsonRaw(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode base64 failed: %w", err)
	}
	if len(raw)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding byte length %d not multiple of 4", len(raw))
	}
	n := len(raw) / 4
	floats := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(raw[i*4 : i*4+4])
		floats[i] = float64(math.Float32frombits(bits)) // To be consistent with LiteLLM
	}
	out, err := json.Marshal(floats)
	if err != nil {
		return nil, fmt.Errorf("marshal floats to json failed: %w", err)
	}
	return out, nil
}

func containsKeywordInModel(model string, keywords []string) bool {
	model = strings.ToLower(model)
	for _, keyword := range keywords {
		if strings.Contains(model, keyword) {
			return true
		}
	}
	return false
}

func parseAzureApiVersion(apiVersion string) (year int, month int, err error) {
	parts := strings.Split(apiVersion, "-")
	if len(parts) < 3 {
		err = fmt.Errorf("invalid api version format: %s", apiVersion)
		return
	}
	year, err = strconv.Atoi(parts[0])
	if err != nil {
		err = fmt.Errorf("invalid api version year: %s", parts[0])
	}
	month, err = strconv.Atoi(parts[1])
	if err != nil {
		err = fmt.Errorf("invalid api version month: %s", parts[1])
	}
	return
}
