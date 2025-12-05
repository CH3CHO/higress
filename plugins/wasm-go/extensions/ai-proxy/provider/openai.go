package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// openaiProvider is the provider for OpenAI service.

const (
	defaultOpenaiDomain = "api.openai.com"
)

var (
	openaiRequestFieldBlacklist = []string{
		"timeout",
		"stream_timeout",
		"fallback",
		"fallbacks",
		"cache",
		"max_retries",
		"api_version",
	}
	openaiNullRequestFieldBlacklist = map[ApiName][]string{
		// If a field in the list has null as its value,
		// it shall be deleted from the request.
		ApiNameChatCompletion: {
			"stream",
		},
		ApiNameEmbeddings: {
			"encoding_format",
		},
	}
	completionsRequestFieldWhitelist = map[string]bool{
		"model": true,
		//"prompt":            true, prompt is transformed to messages so we don't include it here
		"best_of":           true,
		"echo":              true,
		"frequency_penalty": true,
		"logit_bias":        true,
		"logprobs":          true,
		"max_tokens":        true,
		"n":                 true,
		"presence_penalty":  true,
		"stop":              true,
		"stream":            true,
		"suffix":            true,
		"temperature":       true,
		"top_p":             true,
		"user":              true,
	}
)

type openaiProviderInitializer struct{}

func (m *openaiProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	return nil
}

func (m *openaiProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{
		string(ApiNameCompletion):                           PathOpenAICompletions,
		string(ApiNameChatCompletion):                       PathOpenAIChatCompletions,
		string(ApiNameEmbeddings):                           PathOpenAIEmbeddings,
		string(ApiNameImageGeneration):                      PathOpenAIImageGeneration,
		string(ApiNameImageEdit):                            PathOpenAIImageEdit,
		string(ApiNameImageVariation):                       PathOpenAIImageVariation,
		string(ApiNameAudioSpeech):                          PathOpenAIAudioSpeech,
		string(ApiNameModels):                               PathOpenAIModels,
		string(ApiNameFiles):                                PathOpenAIFiles,
		string(ApiNameRetrieveFile):                         PathOpenAIRetrieveFile,
		string(ApiNameRetrieveFileContent):                  PathOpenAIRetrieveFileContent,
		string(ApiNameBatches):                              PathOpenAIBatches,
		string(ApiNameRetrieveBatch):                        PathOpenAIRetrieveBatch,
		string(ApiNameCancelBatch):                          PathOpenAICancelBatch,
		string(ApiNameResponses):                            PathOpenAIResponses,
		string(ApiNameFineTuningJobs):                       PathOpenAIFineTuningJobs,
		string(ApiNameRetrieveFineTuningJob):                PathOpenAIRetrieveFineTuningJob,
		string(ApiNameFineTuningJobEvents):                  PathOpenAIFineTuningJobEvents,
		string(ApiNameFineTuningJobCheckpoints):             PathOpenAIFineTuningJobCheckpoints,
		string(ApiNameCancelFineTuningJob):                  PathOpenAICancelFineTuningJob,
		string(ApiNameResumeFineTuningJob):                  PathOpenAIResumeFineTuningJob,
		string(ApiNamePauseFineTuningJob):                   PathOpenAIPauseFineTuningJob,
		string(ApiNameFineTuningCheckpointPermissions):      PathOpenAIFineTuningCheckpointPermissions,
		string(ApiNameDeleteFineTuningCheckpointPermission): PathOpenAIFineDeleteTuningCheckpointPermission,
	}
}

// isDirectPath checks if the path is a known standard OpenAI interface path.
func isDirectPath(path string) bool {
	return strings.HasSuffix(path, "/completions") ||
		strings.HasSuffix(path, "/embeddings") ||
		strings.HasSuffix(path, "/audio/speech") ||
		strings.HasSuffix(path, "/images/generations") ||
		strings.HasSuffix(path, "/images/variations") ||
		strings.HasSuffix(path, "/images/edits") ||
		strings.HasSuffix(path, "/models") ||
		strings.HasSuffix(path, "/responses") ||
		strings.HasSuffix(path, "/fine_tuning/jobs") ||
		strings.HasSuffix(path, "/fine_tuning/checkpoints")
}

func (m *openaiProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	if config.openaiCustomUrl == "" {
		config.setDefaultCapabilities(m.DefaultCapabilities())
		return &openaiProvider{
			config:       config,
			contextCache: createContextCache(&config),
		}, nil
	}
	customUrl := strings.TrimPrefix(strings.TrimPrefix(config.openaiCustomUrl, "http://"), "https://")
	pairs := strings.SplitN(customUrl, "/", 2)
	customPath := "/"
	if len(pairs) == 2 {
		customPath += pairs[1]
	}
	isDirectCustomPath := isDirectPath(customPath)
	capabilities := m.DefaultCapabilities()
	if !isDirectCustomPath {
		for key, mapPath := range capabilities {
			capabilities[key] = path.Join(customPath, strings.TrimPrefix(mapPath, "/v1"))
		}
	}
	config.setDefaultCapabilities(capabilities)
	log.Debugf("ai-proxy: openai provider customDomain:%s, customPath:%s, isDirectCustomPath:%v, capabilities:%v",
		pairs[0], customPath, isDirectCustomPath, capabilities)
	return &openaiProvider{
		config:             config,
		customDomain:       pairs[0],
		customPath:         customPath,
		isDirectCustomPath: isDirectCustomPath,
		contextCache:       createContextCache(&config),
	}, nil
}

type openaiProvider struct {
	config             ProviderConfig
	customDomain       string
	customPath         string
	isDirectCustomPath bool
	contextCache       *contextCache
}

func (m *openaiProvider) GetProviderType() string {
	return providerTypeOpenAI
}

func (m *openaiProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	m.config.handleRequestHeaders(m, ctx, apiName)
	return nil
}

func (m *openaiProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	if m.isDirectCustomPath {
		util.OverwriteRequestPathHeader(headers, m.customPath)
	} else if apiName != "" {
		effectiveApiName := apiName
		if effectiveApiName == ApiNameCompletion {
			// Azure OpenAI doesn't have a separate completion API,
			// so we need to convert it to chat completion API.
			effectiveApiName = ApiNameChatCompletion
		}
		util.OverwriteRequestPathHeaderByCapability(headers, string(effectiveApiName), m.config.capabilities)
	}

	if m.customDomain != "" {
		util.OverwriteRequestHostHeader(headers, m.customDomain)
	} else {
		util.OverwriteRequestHostHeader(headers, defaultOpenaiDomain)
	}
	if len(m.config.apiTokens) > 0 {
		util.OverwriteRequestAuthorizationHeader(headers, "Bearer "+m.config.GetApiTokenInUse(ctx))
	}
	headers.Del("Content-Length")
}

func (m *openaiProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	if !m.config.needToProcessRequestBody(apiName) {
		// We don't need to process the request body for other APIs.
		return types.ActionContinue, nil
	}
	return m.config.handleRequestBody(m, m.contextCache, ctx, apiName, body)
}

func (m *openaiProvider) TransformRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (transformedBody []byte, err error) {
	transformedBody = body
	err = nil

	if m.config.responseJsonSchema != nil {
		request := &chatCompletionRequest{}
		if err := decodeChatCompletionRequest(transformedBody, request); err != nil {
			return nil, err
		}
		log.Debugf("[ai-proxy] set response format to %s", m.config.responseJsonSchema)
		request.ResponseFormat = m.config.responseJsonSchema
		body, _ = json.Marshal(request)
	}

	transformedBody, err = m.config.defaultTransformRequestBody(ctx, apiName, transformedBody)
	if err != nil {
		return
	}

	transformedBody, err = m.transformRequestFields(ctx, apiName, transformedBody)
	if err != nil {
		log.Errorf("openaiProvider: transform request fields failed: %v", err)
	}

	return
}

func (m *openaiProvider) transformRequestFields(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	needReadResponseBody := false

	defer func() {
		if !needReadResponseBody {
			ctx.DontReadResponseBody()
		}
	}()

	// Expand extra_body field if exists before everything else
	body = expandExtraBodyField(body)

	// Remove fields not supported by OpenAI
	for _, field := range openaiRequestFieldBlacklist {
		if transformedBody, err := sjson.DeleteBytes(body, field); err != nil {
			return body, fmt.Errorf("openaiProvider: failed to delete %s from request body, err: %v", field, err)
		} else {
			body = transformedBody
		}
	}

	if transformedBody, err := deleteNullValueFields(body, openaiNullRequestFieldBlacklist[apiName]); err != nil {
		return body, fmt.Errorf("openaiProvider: failed to delete blacklisted request fields with null value from request body: %v", err)
	} else {
		body = transformedBody
	}

	switch apiName {
	case ApiNameCompletion:
		if transformedBody, needReadResponseBodyLocal, err := transformCompletionsRequestFields(ctx, body); err != nil {
			return body, fmt.Errorf("azureProvider: transform completion request fields failed: %v", err)
		} else {
			needReadResponseBody = needReadResponseBodyLocal
			return transformedBody, nil
		}
	case ApiNameChatCompletion:
		if transformedBody, err := m.transformChatCompletionsRequestFields(ctx, body); err != nil {
			return body, fmt.Errorf("azureProvider: transform chat completion request fields failed: %v", err)
		} else {
			return transformedBody, nil
		}
	}

	return body, nil
}

func (m *openaiProvider) OnStreamingEvent(ctx wrapper.HttpContext, name ApiName, event StreamEvent) ([]StreamEvent, error) {
	if name == ApiNameCompletion {
		return handleCompletionsStreamingEvent(ctx, event)
	}
	return nil, nil
}

func (m *openaiProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	switch apiName {
	case ApiNameCompletion:
		return transformCompletionsResponseFields(ctx, body)
	}
	return body, nil
}

func (m *openaiProvider) transformChatCompletionsRequestFields(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	if transformedBody, err := complementChatCompletionsMessageRole(body); err != nil {
		return body, fmt.Errorf("openaiProvider: complement chat completions message role failed: %v", err)
	} else {
		body = transformedBody
	}
	return body, nil
}

func transformCompletionsRequestFields(ctx wrapper.HttpContext, body []byte) (_ []byte, needReadResponseBody bool, err error) {
	var transformedBody []byte
	if prompt := gjson.GetBytes(body, "prompt"); prompt.Exists() {
		var messages []byte
		if prompt.IsArray() {
			items := prompt.Array()
			if len(items) == 0 {
				return body, needReadResponseBody, errors.New("empty prompt array")
			}
			firstItem := items[0]
			if firstItem.Type == gjson.String {
				var builder strings.Builder
				for _, item := range items {
					if builder.Len() == 0 {
						builder.WriteString("[")
					} else {
						builder.WriteString(",")
					}
					builder.WriteString("{\"role\":\"user\",\"content\":")
					builder.WriteString(item.Raw)
					builder.WriteString("}")
				}
				builder.WriteString("]")
				messages = []byte(builder.String())
			} else if firstItem.IsArray() {
				messages = []byte(fmt.Sprintf("[{\"role\":\"user\",\"content\":%s}]", firstItem.Raw))
			} else {
				return body, needReadResponseBody, fmt.Errorf("unsupported prompt item type: %s", firstItem.Type.String())
			}
		} else if prompt.Type == gjson.String {
			messages = []byte(fmt.Sprintf("[{\"role\":\"user\",\"content\":%s}]", prompt.Raw))
		} else {
			return body, needReadResponseBody, fmt.Errorf("unsupported prompt type: %s", prompt.Type.String())
		}
		if transformedBody, err = sjson.SetRawBytes(transformedBody, "messages", messages); err != nil {
			return body, needReadResponseBody, fmt.Errorf("failed to set messages in completion request body, err: %v", err)
		}
	}
	for field := range completionsRequestFieldWhitelist {
		if value := gjson.GetBytes(body, field); value.Exists() {
			if transformedBody, err = sjson.SetRawBytes(transformedBody, field, []byte(value.Raw)); err != nil {
				return body, needReadResponseBody, fmt.Errorf("failed to set %s in completion request body, err: %v", field, err)
			}
		}
	}
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && stream.Type == gjson.True {
		transformedBody, _ = sjson.SetBytes(transformedBody, "stream_options.include_usage", true)
	}
	return transformedBody, true, nil
}

func transformCompletionsResponseFields(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	transformedBody := `{"object":"text_completion"}`
	if id := gjson.GetBytes(body, "id"); id.Exists() {
		transformedBody, _ = sjson.SetRaw(transformedBody, "id", id.Raw)
	}
	if created := gjson.GetBytes(body, "created"); created.Exists() {
		transformedBody, _ = sjson.SetRaw(transformedBody, "created", created.Raw)
	}
	if model := gjson.GetBytes(body, "model"); model.Exists() {
		transformedBody, _ = sjson.SetRaw(transformedBody, "model", model.Raw)
	}
	if choices := gjson.GetBytes(body, "choices"); choices.Exists() && choices.IsArray() {
		var builder strings.Builder
		for _, choice := range choices.Array() {
			if builder.Len() == 0 {
				builder.WriteString("[")
			} else {
				builder.WriteString(",")
			}
			choiceJson := `{"logprobs":null}`
			if content := choice.Get("message.content"); content.Exists() {
				choiceJson, _ = sjson.SetRaw(choiceJson, "text", content.Raw)
			}
			if index := choice.Get("index"); index.Exists() {
				choiceJson, _ = sjson.SetRaw(choiceJson, "index", index.Raw)
			}
			if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
				choiceJson, _ = sjson.SetRaw(choiceJson, "finish_reason", finishReason.Raw)
			}
			builder.WriteString(choiceJson)
		}
		builder.WriteString("]")
		transformedBody, _ = sjson.SetRaw(transformedBody, "choices", builder.String())
	}
	if usage := gjson.GetBytes(body, "usage"); usage.Exists() {
		transformedBody, _ = sjson.SetRaw(transformedBody, "usage", usage.Raw)
	}
	// TODO: hidden_params
	return []byte(transformedBody), nil
}

func handleCompletionsStreamingEvent(ctx wrapper.HttpContext, event StreamEvent) ([]StreamEvent, error) {
	data := event.Data
	transformedData := `{"object":"text_completion"}`
	if id := gjson.Get(data, "id"); id.Exists() {
		transformedData, _ = sjson.SetRaw(transformedData, "id", id.Raw)
	}
	if created := gjson.Get(data, "created"); created.Exists() {
		transformedData, _ = sjson.SetRaw(transformedData, "created", created.Raw)
	}
	if model := gjson.Get(data, "model"); model.Exists() {
		transformedData, _ = sjson.SetRaw(transformedData, "model", model.Raw)
	}
	if choice := gjson.Get(data, "choices.0"); choice.Exists() && choice.IsObject() {
		choiceJson := `{"logprobs":null}`
		if content := choice.Get("delta.content"); content.Exists() {
			choiceJson, _ = sjson.SetRaw(choiceJson, "text", content.Raw)
		}
		if index := choice.Get("index"); index.Exists() {
			choiceJson, _ = sjson.SetRaw(choiceJson, "index", index.Raw)
		}
		if finishReason := choice.Get("finish_reason"); finishReason.Exists() {
			choiceJson, _ = sjson.SetRaw(choiceJson, "finish_reason", finishReason.Raw)
		}
		transformedData, _ = sjson.SetRaw(transformedData, "choices.0", choiceJson)
	}
	if usage := gjson.Get(data, "usage"); usage.Exists() {
		transformedData, _ = sjson.SetRaw(transformedData, "usage", usage.Raw)
	}

	transformedEvent := event
	transformedEvent.Data = transformedData
	return []StreamEvent{transformedEvent}, nil
}
