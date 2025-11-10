package provider

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	doubaoDomain                 = "ark.%s.volces.com"
	doubaoDefaultRegion          = "cn-beijing"
	doubaoChatCompletionPath     = "/api/v3/chat/completions"
	doubaoBotsChatCompletionPath = "/api/v3/bots/chat/completions"
	doubaoEmbeddingsPath         = "/api/v3/embeddings"
	doubaoImageGenerationPath    = "/api/v3/images/generations"
	doubaoResponsesPath          = "/api/v3/responses"
)

var (
	knownBotModelUsageProperties = []string{
		"completion_tokens",
		"prompt_tokens",
		"total_tokens",
		"completion_tokens_details",
		"prompt_tokens_details",
		"name",
	}
)

type doubaoProviderInitializer struct{}

func (m *doubaoProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if config.apiTokens == nil || len(config.apiTokens) == 0 {
		return errors.New("no apiToken found in provider config")
	}
	return nil
}

func (m *doubaoProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{
		string(ApiNameChatCompletion):  doubaoChatCompletionPath,
		string(ApiNameEmbeddings):      doubaoEmbeddingsPath,
		string(ApiNameImageGeneration): doubaoImageGenerationPath,
		string(ApiNameResponses):       doubaoResponsesPath,
	}
}

func (m *doubaoProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	defaultCapabilities := m.DefaultCapabilities()
	if config.doubaoUseBotApi {
		defaultCapabilities[string(ApiNameChatCompletion)] = doubaoBotsChatCompletionPath
	}
	config.setDefaultCapabilities(defaultCapabilities)
	region := config.doubaoRegion
	if region == "" {
		region = doubaoDefaultRegion
	}
	return &doubaoProvider{
		domain:       fmt.Sprintf(doubaoDomain, region),
		config:       config,
		contextCache: createContextCache(&config),
	}, nil
}

type doubaoProvider struct {
	domain       string
	config       ProviderConfig
	contextCache *contextCache
}

func (m *doubaoProvider) GetProviderType() string {
	return providerTypeDoubao
}

func (m *doubaoProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	m.config.handleRequestHeaders(m, ctx, apiName)
	return nil
}

func (m *doubaoProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	if !m.config.isSupportedAPI(apiName) {
		return types.ActionContinue, errUnsupportedApiName
	}
	return m.config.handleRequestBody(m, m.contextCache, ctx, apiName, body)
}

func (m *doubaoProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	util.OverwriteRequestPathHeaderByCapability(headers, string(apiName), m.config.capabilities)
	util.OverwriteRequestHostHeader(headers, m.domain)
	util.OverwriteRequestAuthorizationHeader(headers, "Bearer "+m.config.GetApiTokenInUse(ctx))
	headers.Del("Content-Length")
}

func (m *doubaoProvider) TransformRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	var err error
	switch apiName {
	case ApiNameResponses:
		// 移除火山 responses 接口暂时不支持的参数
		// 参考: https://www.volcengine.com/docs/82379/1569618
		// TODO: 这里应该用 DTO 处理
		for _, param := range []string{"parallel_tool_calls", "tool_choice"} {
			body, err = sjson.DeleteBytes(body, param)
			if err != nil {
				log.Warnf("[doubao] failed to delete %s in request body, err: %v", param, err)
			}
		}
	case ApiNameImageGeneration:
		// 火山生图接口默认会带上水印,但 OpenAI 接口不支持此参数
		// 参考: https://www.volcengine.com/docs/82379/1541523
		if res := gjson.GetBytes(body, "watermark"); !res.Exists() {
			body, err = sjson.SetBytes(body, "watermark", false)
			if err != nil {
				log.Warnf("[doubao] failed to set watermark in request body, err: %v", err)
			}
		}
	}
	return m.config.defaultTransformRequestBody(ctx, apiName, body)
}

func (m *doubaoProvider) GetApiName(path string) ApiName {
	if strings.Contains(path, doubaoChatCompletionPath) {
		return ApiNameChatCompletion
	}
	if strings.Contains(path, doubaoEmbeddingsPath) {
		return ApiNameEmbeddings
	}
	if strings.Contains(path, doubaoImageGenerationPath) {
		return ApiNameImageGeneration
	}
	if strings.Contains(path, doubaoResponsesPath) {
		return ApiNameResponses
	}
	return ""
}

func (m *doubaoProvider) OnStreamingEvent(ctx wrapper.HttpContext, apiName ApiName, event StreamEvent) ([]StreamEvent, error) {
	if apiName == ApiNameChatCompletion && m.config.doubaoUseBotApi {
		return m.onBotChatCompletionStreamingEvent(ctx, event)
	}
	return nil, nil
}

func (m *doubaoProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	if apiName == ApiNameChatCompletion && m.config.doubaoUseBotApi {
		return m.onBotChatCompletionResponseBody(ctx, body)
	}
	return body, nil
}

func (m *doubaoProvider) onBotChatCompletionStreamingEvent(ctx wrapper.HttpContext, event StreamEvent) ([]StreamEvent, error) {
	if updatedData, err := m.normalizeUsageData([]byte(event.Data)); err != nil {
		return nil, err
	} else {
		updatedEvent := event
		updatedEvent.Data = string(updatedData)
		return []StreamEvent{updatedEvent}, err
	}
}

func (m *doubaoProvider) onBotChatCompletionResponseBody(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	return m.normalizeUsageData(body)
}

func (m *doubaoProvider) normalizeUsageData(body []byte) ([]byte, error) {
	botUsage := gjson.GetBytes(body, "bot_usage")
	if botUsage.Exists() && botUsage.IsObject() {
		var err error
		usageOutput := make([]byte, 0)
		if modelUsage := botUsage.Get("model_usage"); modelUsage.Exists() && modelUsage.IsArray() {
			modelUsageArray := modelUsage.Array()
			if len(modelUsageArray) > 0 {
				modelUsageObj := modelUsageArray[0]
				for _, prop := range knownBotModelUsageProperties {
					usageOutput, err = copyJsonField(modelUsageObj, prop, usageOutput)
				}
			}
		}

		if actionUsage := botUsage.Get("action_usage"); actionUsage.Exists() {
			usageOutput, err = sjson.SetRawBytes(usageOutput, "action_detail", []byte(actionUsage.Raw))
			if err != nil {
				return body, err
			}
		}

		return sjson.SetRawBytes(body, "usage", []byte(usageOutput))
	}
	return nil, nil
}

func copyJsonField(src gjson.Result, key string, dest []byte) ([]byte, error) {
	if src.Exists() && src.IsObject() {
		if value := src.Get(key); value.Exists() {
			return sjson.SetRawBytes(dest, key, []byte(value.Raw))
		} else {
			return sjson.SetRawBytes(dest, key, []byte("null"))
		}
	}
	return dest, nil
}
