package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
)

// claudeProvider is the provider for Claude service.
const (
	claudeDomain           = "api.anthropic.com"
	claudeDefaultVersion   = "2023-06-01"
	claudeDefaultMaxTokens = 4096
)

type claudeProviderInitializer struct{}

type claudeTextGenResponse struct {
	Id           string                 `json:"id"`
	Type         string                 `json:"type"`
	Role         string                 `json:"role"`
	Content      []claudeTextGenContent `json:"content"`
	Model        string                 `json:"model"`
	StopReason   *string                `json:"stop_reason"`
	StopSequence *string                `json:"stop_sequence"`
	Usage        claudeTextGenUsage     `json:"usage"`
	Error        *claudeTextGenError    `json:"error"`
}

type claudeTextGenContent struct {
	Type      string                 `json:"type,omitempty"`
	Text      string                 `json:"text,omitempty"`
	Id        string                 `json:"id,omitempty"`        // For tool_use
	Name      string                 `json:"name,omitempty"`      // For tool_use
	Input     map[string]interface{} `json:"input,omitempty"`     // For tool_use
	Signature string                 `json:"signature,omitempty"` // For thinking
	Thinking  string                 `json:"thinking,omitempty"`  // For thinking
}

type claudeTextGenUsage struct {
	InputTokens  int    `json:"input_tokens,omitempty"`
	OutputTokens int    `json:"output_tokens,omitempty"`
	ServiceTier  string `json:"service_tier,omitempty"`
}

type claudeTextGenError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type claudeTextGenStreamResponse struct {
	Type         string                 `json:"type"`
	Message      *claudeTextGenResponse `json:"message,omitempty"`
	Index        *int                   `json:"index,omitempty"`
	ContentBlock *claudeTextGenContent  `json:"content_block,omitempty"`
	Delta        *claudeTextGenDelta    `json:"delta,omitempty"`
	Usage        *claudeTextGenUsage    `json:"usage,omitempty"`
}

type claudeTextGenDelta struct {
	Type         string  `json:"type"`
	Text         string  `json:"text,omitempty"`
	StopReason   *string `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

func (c *claudeProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if len(config.apiTokens) == 0 {
		return errors.New("no apiToken found in provider config")
	}
	return nil
}

func (c *claudeProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{
		string(ApiNameChatCompletion): PathAnthropicMessages,
		string(ApiNameCompletion):     PathAnthropicComplete,
		// docs: https://docs.anthropic.com/en/docs/build-with-claude/embeddings#voyage-http-api
		string(ApiNameEmbeddings): PathOpenAIEmbeddings,
		string(ApiNameModels):     PathOpenAIModels,
	}
}

func (c *claudeProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	config.setDefaultCapabilities(c.DefaultCapabilities())
	return &claudeProvider{
		config:       config,
		contextCache: createContextCache(&config),
	}, nil
}

type claudeProvider struct {
	config       ProviderConfig
	contextCache *contextCache

	messageId   string
	usage       usage
	serviceTier string
}

func (c *claudeProvider) GetProviderType() string {
	return providerTypeClaude
}

func (c *claudeProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	c.config.handleRequestHeaders(c, ctx, apiName)
	return nil
}

func (c *claudeProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	util.OverwriteRequestPathHeaderByCapability(headers, string(apiName), c.config.capabilities)
	util.OverwriteRequestHostHeader(headers, claudeDomain)

	headers.Set("x-api-key", c.config.GetApiTokenInUse(ctx))

	if c.config.apiVersion == "" {
		c.config.apiVersion = claudeDefaultVersion
	}

	headers.Set("anthropic-version", c.config.apiVersion)
}

func (c *claudeProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	if !c.config.isSupportedAPI(apiName) {
		return types.ActionContinue, nil
	}
	return c.config.handleRequestBody(c, c.contextCache, ctx, apiName, body)
}

func (c *claudeProvider) TransformRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	if apiName != ApiNameChatCompletion {
		return c.config.defaultTransformRequestBody(ctx, apiName, body)
	}
	request := &chatCompletionRequest{}
	if err := c.config.parseRequestAndMapModel(ctx, request, body); err != nil {
		return nil, err
	}
	claudeRequest := c.buildClaudeTextGenRequest(request)
	return json.Marshal(claudeRequest)
}

func (c *claudeProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	if apiName != ApiNameChatCompletion {
		return body, nil
	}
	claudeResponse := &claudeTextGenResponse{}
	if err := json.Unmarshal(body, claudeResponse); err != nil {
		return nil, fmt.Errorf("unable to unmarshal claude response: %v", err)
	}
	if claudeResponse.Error != nil {
		return nil, fmt.Errorf("claude response error, error_type: %s, error_message: %s", claudeResponse.Error.Type, claudeResponse.Error.Message)
	}
	response := c.responseClaude2OpenAI(ctx, claudeResponse)
	return json.Marshal(response)
}

func (c *claudeProvider) OnStreamingResponseBody(ctx wrapper.HttpContext, name ApiName, chunk []byte, isLastChunk bool) ([]byte, error) {
	if isLastChunk || len(chunk) == 0 {
		return nil, nil
	}
	// only process the response from chat completion, skip other responses
	if name != ApiNameChatCompletion {
		return chunk, nil
	}

	responseBuilder := &strings.Builder{}
	lines := strings.Split(string(chunk), "\n")
	for _, data := range lines {
		// only process the line starting with "data:"
		if strings.HasPrefix(data, "data:") {
			// extract json data from the line
			jsonData := strings.TrimPrefix(data, "data:")
			var claudeResponse claudeTextGenStreamResponse
			if err := json.Unmarshal([]byte(jsonData), &claudeResponse); err != nil {
				log.Errorf("unable to unmarshal claude response: %v", err)
				continue
			}
			response := c.streamResponseClaude2OpenAI(ctx, &claudeResponse)
			if response != nil {
				responseBody, err := json.Marshal(response)
				if err != nil {
					log.Errorf("unable to marshal response: %v", err)
					return nil, err
				}
				c.appendResponse(responseBuilder, string(responseBody))
			}
		}
	}
	modifiedResponseChunk := responseBuilder.String()
	log.Debugf("modified response chunk: %s", modifiedResponseChunk)
	return []byte(modifiedResponseChunk), nil
}

func (c *claudeProvider) buildClaudeTextGenRequest(origRequest *chatCompletionRequest) *claudeTextGenRequest {
	claudeRequest := claudeTextGenRequest{
		Model:  origRequest.Model,
		Stream: origRequest.Stream,
		anthropicMessagesCommonFields: anthropicMessagesCommonFields{
			MaxTokens:     origRequest.getMaxTokens(),
			StopSequences: origRequest.Stop,
			Temperature:   origRequest.Temperature,
			TopP:          origRequest.TopP,
		},
		// ServiceTier:   origRequest.ServiceTier,
	}
	if claudeRequest.MaxTokens == 0 {
		claudeRequest.MaxTokens = claudeDefaultMaxTokens
	}

	for _, message := range origRequest.Messages {
		if message.Role == roleSystem {
			claudeRequest.System = claudeSystemPrompt{
				StringValue: message.StringContent(),
				IsArray:     false,
			}
			continue
		}

		claudeMessage := claudeChatMessage{
			Role: message.Role,
		}
		if message.IsStringContent() {
			claudeMessage.Content = newStringContent(message.StringContent())
		} else {
			chatMessageContents := make([]claudeChatMessageContent, 0)
			for _, messageContent := range message.ParseContent() {
				switch messageContent.Type {
				case contentTypeText:
					chatMessageContents = append(chatMessageContents, claudeChatMessageContent{
						Type: contentTypeText,
						Text: messageContent.Text,
					})
				case contentTypeImageUrl:
					if strings.HasPrefix(messageContent.ImageUrl.Url, "data:") {
						parts := strings.SplitN(messageContent.ImageUrl.Url, ";", 2)
						if len(parts) != 2 {
							log.Errorf("invalid image url format: %s", messageContent.ImageUrl.Url)
							continue
						}
						chatMessageContents = append(chatMessageContents, claudeChatMessageContent{
							Type: "image",
							Source: &claudeChatMessageContentSource{
								Type:      "base64",
								MediaType: strings.TrimPrefix(parts[0], "data:"),
								Data:      strings.TrimPrefix(parts[1], "base64,"),
							},
						})
					} else {
						chatMessageContents = append(chatMessageContents, claudeChatMessageContent{
							Type: "image",
							Source: &claudeChatMessageContentSource{
								Type: "url",
								Url:  messageContent.ImageUrl.Url,
							},
						})
					}
				case contentTypeFile:
					chatMessageContents = append(chatMessageContents, claudeChatMessageContent{
						Type: "file",
						Source: &claudeChatMessageContentSource{
							Type:   "url",
							FileId: messageContent.File.FileId,
						},
					})
				default:
					log.Errorf("Unsupported content type: %s", messageContent.Type)
					continue
				}
			}
			claudeMessage.Content = newArrayContent(chatMessageContents)
		}
		claudeRequest.Messages = append(claudeRequest.Messages, claudeMessage)
	}

	for _, tool := range origRequest.Tools {
		claudeTool := claudeTool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		}
		claudeRequest.Tools = append(claudeRequest.Tools, claudeTool)
	}

	if tc := origRequest.getToolChoiceObject(); tc != nil {
		claudeRequest.ToolChoice = &claudeToolChoice{
			Name:                   tc.Function.Name,
			Type:                   tc.Type,
			DisableParallelToolUse: !origRequest.ParallelToolCalls,
		}
	}

	return &claudeRequest
}

func (c *claudeProvider) responseClaude2OpenAI(ctx wrapper.HttpContext, origResponse *claudeTextGenResponse) *chatCompletionResponse {
	choice := chatCompletionChoice{
		Index:        0,
		Message:      &chatMessage{Role: roleAssistant, Content: origResponse.Content[0].Text},
		FinishReason: util.Ptr(stopReasonClaude2OpenAI(origResponse.StopReason)),
	}

	response := &chatCompletionResponse{
		Id:                origResponse.Id,
		Created:           time.Now().UnixMilli() / 1000,
		Model:             ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
		SystemFingerprint: "",
		Object:            objectChatCompletion,
		Choices:           []chatCompletionChoice{choice},
	}

	// Include usage information if available
	if origResponse.Usage.InputTokens > 0 || origResponse.Usage.OutputTokens > 0 {
		response.Usage = &usage{
			PromptTokens:     origResponse.Usage.InputTokens,
			CompletionTokens: origResponse.Usage.OutputTokens,
			TotalTokens:      origResponse.Usage.InputTokens + origResponse.Usage.OutputTokens,
		}
	}

	return response
}

func stopReasonClaude2OpenAI(reason *string) string {
	if reason == nil {
		return ""
	}
	switch *reason {
	case "end_turn":
		return finishReasonStop
	case "stop_sequence":
		return finishReasonStop
	case "max_tokens":
		return finishReasonLength
	default:
		return *reason
	}
}

func (c *claudeProvider) streamResponseClaude2OpenAI(ctx wrapper.HttpContext, origResponse *claudeTextGenStreamResponse) *chatCompletionResponse {
	switch origResponse.Type {
	case "message_start":
		if origResponse.Message != nil {
			c.messageId = origResponse.Message.Id
			c.usage = usage{
				PromptTokens:     origResponse.Message.Usage.InputTokens,
				CompletionTokens: origResponse.Message.Usage.OutputTokens,
			}
			c.serviceTier = origResponse.Message.Usage.ServiceTier
		}
		var index int
		if origResponse.Index != nil {
			index = *origResponse.Index
		}
		choice := chatCompletionChoice{
			Index: index,
			Delta: &chatMessage{Role: roleAssistant, Content: ""},
		}
		return c.createChatCompletionResponse(ctx, origResponse, choice)

	case "content_block_delta":
		var index int
		if origResponse.Index != nil {
			index = *origResponse.Index
		}
		choice := chatCompletionChoice{
			Index: index,
			Delta: &chatMessage{Content: origResponse.Delta.Text},
		}
		return c.createChatCompletionResponse(ctx, origResponse, choice)

	case "message_delta":
		if origResponse.Usage != nil {
			c.usage.CompletionTokens += origResponse.Usage.OutputTokens
			c.usage.TotalTokens = c.usage.PromptTokens + c.usage.CompletionTokens
		}

		var index int
		if origResponse.Index != nil {
			index = *origResponse.Index
		}
		choice := chatCompletionChoice{
			Index:        index,
			Delta:        &chatMessage{},
			FinishReason: util.Ptr(stopReasonClaude2OpenAI(origResponse.Delta.StopReason)),
		}
		return c.createChatCompletionResponse(ctx, origResponse, choice)
	case "message_stop":
		return &chatCompletionResponse{
			Id:          c.messageId,
			Created:     time.Now().UnixMilli() / 1000,
			Model:       ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
			Object:      objectChatCompletionChunk,
			Choices:     []chatCompletionChoice{},
			ServiceTier: c.serviceTier,
			Usage: &usage{
				PromptTokens:     c.usage.PromptTokens,
				CompletionTokens: c.usage.CompletionTokens,
				TotalTokens:      c.usage.TotalTokens,
			},
		}
	case "content_block_stop", "ping", "content_block_start":
		log.Debugf("skip processing response type: %s", origResponse.Type)
		return nil
	default:
		log.Errorf("Unexpected response type: %s", origResponse.Type)
		return nil
	}
}

func (c *claudeProvider) createChatCompletionResponse(ctx wrapper.HttpContext, response *claudeTextGenStreamResponse, choice chatCompletionChoice) *chatCompletionResponse {
	return &chatCompletionResponse{
		Id:          c.messageId,
		Created:     time.Now().UnixMilli() / 1000,
		Model:       ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
		Object:      objectChatCompletionChunk,
		Choices:     []chatCompletionChoice{choice},
		ServiceTier: c.serviceTier,
	}
}

func (c *claudeProvider) appendResponse(responseBuilder *strings.Builder, responseBody string) {
	responseBuilder.WriteString(fmt.Sprintf("%s %s\n\n", streamDataItemKey, responseBody))
}

func (c *claudeProvider) insertHttpContextMessage(body []byte, content string, onlyOneSystemBeforeFile bool) ([]byte, error) {
	request := &claudeTextGenRequest{}
	if err := json.Unmarshal(body, request); err != nil {
		return nil, fmt.Errorf("unable to unmarshal request: %v", err)
	}

	systemStr := request.System.String()
	if systemStr == "" {
		request.System = claudeSystemPrompt{
			StringValue: content,
			IsArray:     false,
		}
	} else {
		request.System = claudeSystemPrompt{
			StringValue: content + "\n" + systemStr,
			IsArray:     false,
		}
	}

	return json.Marshal(request)
}

func (c *claudeProvider) GetApiName(path string) ApiName {
	if strings.Contains(path, PathAnthropicMessages) {
		return ApiNameChatCompletion
	}
	if strings.Contains(path, PathAnthropicComplete) {
		return ApiNameCompletion
	}
	if strings.Contains(path, PathOpenAIModels) {
		return ApiNameModels
	}
	if strings.Contains(path, PathOpenAIEmbeddings) {
		return ApiNameEmbeddings
	}
	return ""
}
