package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
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
	}
	azureUseMaxCompletionTokensModelKeywords = []string{
		"o1",
		"o3",
		"o4",
		"gpt-5",
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
	return &azureProvider{
		config:             config,
		serviceUrl:         serviceUrl,
		serviceUrlType:     serviceUrlType,
		serviceUrlFullPath: serviceUrl.Path + "?" + serviceUrl.RawQuery,
		apiVersion:         apiVersion,
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
	transformedBody, err = m.transformRequestFields(ctx, transformedBody)
	if err != nil {
		log.Errorf("azureProvider: transform request fields failed: %v", err)
	}

	return
}

func (m *azureProvider) transformRequestFields(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	// Remove fields not supported by Azure OpenAI
	for _, field := range azureRequestFieldBlacklist {
		if transformedBody, err := sjson.DeleteBytes(body, field); err != nil {
			return body, fmt.Errorf("azureProvider: failed to delete %s from request body, err: %v", field, err)
		} else {
			body = transformedBody
		}
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
	log.Debugf("azureProvider: transformRequestFields for model %s", model)
	useMaxCompletionTokens := slices.ContainsFunc(azureUseMaxCompletionTokensModelKeywords, func(m string) bool {
		return strings.Contains(model, m)
	})
	if useMaxCompletionTokens {
		log.Debugf("azureProvider: model %s requires %s field instead of %s", model, requestFieldMaxCompletionTokens, requestFieldMaxTokens)
		if maxTokens := gjson.GetBytes(body, requestFieldMaxTokens); maxTokens.Exists() {
			log.Debugf("azureProvider: removing %s from request body for model %s", requestFieldMaxTokens, model)
			if transformedBody, err := sjson.DeleteBytes(body, requestFieldMaxTokens); err != nil {
				return body, fmt.Errorf("azureProvider: failed to delete %s in request body, err: %v", requestFieldMaxTokens, err)
			} else {
				body = transformedBody
			}
			log.Debugf("azureProvider: setting %s into request body for model %s: %s", requestFieldMaxCompletionTokens, model, maxTokens.Raw)
			if transformedBody, err := sjson.SetRawBytes(body, requestFieldMaxCompletionTokens, []byte(maxTokens.Raw)); err != nil {
				return body, fmt.Errorf("azureProvider: failed to set %s into request body, value: %s err: %v", requestFieldMaxTokens, maxTokens.Raw, err)
			} else {
				body = transformedBody
			}
		}
	}

	return body, nil
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
	path := util.MapRequestPathByCapability(string(apiName), originalPath, m.config.capabilities)
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
