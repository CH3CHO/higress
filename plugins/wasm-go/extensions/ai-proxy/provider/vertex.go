package provider

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/vertex"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
)

const (
	vertexAuthDomain   = "oauth2.googleapis.com"
	vertexDomain       = "{REGION}-aiplatform.googleapis.com"
	vertexGlobalDomain = "aiplatform.googleapis.com"
	// /v1/projects/{PROJECT_ID}/locations/{REGION}/publishers/google/models/{MODEL_ID}:{ACTION}
	vertexPathTemplate               = "/v1/projects/%s/locations/%s/publishers/google/models/%s:%s"
	vertexChatCompletionAction       = "generateContent"
	vertexChatCompletionStreamAction = "streamGenerateContent?alt=sse"
	vertexEmbeddingAction            = "predict"
)

const (
	ctxStreamIncludeUsage = "original_stream_include_usage"
)

type vertexProviderInitializer struct{}

func (v *vertexProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if config.vertexAuthKey == "" {
		return errors.New("missing vertexAuthKey in vertex provider config")
	}
	if config.vertexRegion == "" || config.vertexProjectId == "" {
		return errors.New("missing vertexRegion or vertexProjectId in vertex provider config")
	}
	if config.vertexAuthServiceName == "" {
		return errors.New("missing vertexAuthServiceName in vertex provider config")
	}
	return nil
}

func (v *vertexProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{
		string(ApiNameChatCompletion): vertexPathTemplate,
		string(ApiNameCompletion):     vertexPathTemplate,
		string(ApiNameEmbeddings):     vertexPathTemplate,
	}
}

func (v *vertexProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	config.setDefaultCapabilities(v.DefaultCapabilities())
	dnsCluster := wrapper.DnsCluster{
		Domain:      vertexAuthDomain,
		ServiceName: config.vertexAuthServiceName,
		Port:        443,
	}
	log.Infof("[vertex]: use dns cluster %s for auth", dnsCluster.ClusterName())
	return &vertexProvider{
		config:       config,
		client:       wrapper.NewClusterClient(dnsCluster),
		contextCache: createContextCache(&config),
	}, nil
}

type vertexProvider struct {
	client       wrapper.HttpClient
	config       ProviderConfig
	contextCache *contextCache
}

func (v *vertexProvider) GetProviderType() string {
	return providerTypeVertex
}

func (v *vertexProvider) GetApiName(path string) ApiName {
	if strings.HasSuffix(path, vertexChatCompletionAction) || strings.HasSuffix(path, vertexChatCompletionStreamAction) {
		return ApiNameChatCompletion
	}
	if strings.HasSuffix(path, vertexEmbeddingAction) {
		return ApiNameEmbeddings
	}
	return ""
}

func (v *vertexProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	v.config.handleRequestHeaders(v, ctx, apiName)
	return nil
}

func (v *vertexProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	var vertexRegionDomain string
	if v.config.vertexRegion == "global" {
		vertexRegionDomain = vertexGlobalDomain
	} else {
		vertexRegionDomain = strings.Replace(vertexDomain, "{REGION}", v.config.vertexRegion, 1)
	}
	util.OverwriteRequestHostHeader(headers, vertexRegionDomain)
}

func (v *vertexProvider) getToken() (cached bool, err error) {
	cacheKeyName := v.buildTokenKey()
	cachedAccessToken, err := v.getCachedAccessToken(cacheKeyName)
	if err == nil && cachedAccessToken != "" {
		_ = proxywasm.ReplaceHttpRequestHeader("Authorization", "Bearer "+cachedAccessToken)
		return true, nil
	}

	var key ServiceAccountKey
	if err := json.Unmarshal([]byte(v.config.vertexAuthKey), &key); err != nil {
		return false, fmt.Errorf("[vertex]: unable to unmarshal auth key json: %v", err)
	}

	if key.ClientEmail == "" || key.PrivateKey == "" || key.TokenURI == "" {
		return false, fmt.Errorf("[vertex]: missing auth params")
	}

	jwtToken, err := createJWT(&key)
	if err != nil {
		log.Errorf("[vertex]: unable to create JWT token: %v", err)
		return false, err
	}

	log.Debugf("[vertex]: created JWT token successfully: %s...", jwtToken[:20])
	err = v.getAccessToken(jwtToken)
	if err != nil {
		log.Errorf("[vertex]: unable to get access token: %v", err)
		return false, err
	}

	return false, err
}

func (v *vertexProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	if !v.config.isSupportedAPI(apiName) {
		return types.ActionContinue, errUnsupportedApiName
	}
	if v.config.IsOriginal() {
		return types.ActionContinue, nil
	}
	headers := util.GetRequestHeaders()
	body, err := v.TransformRequestBodyHeaders(ctx, apiName, body, headers)
	util.ReplaceRequestHeaders(headers)
	_ = proxywasm.ReplaceHttpRequestBody(body)
	if err != nil {
		return types.ActionContinue, err
	}
	cached, err := v.getToken()
	if cached {
		return types.ActionContinue, nil
	}
	if err == nil {
		return types.ActionPause, nil
	}
	return types.ActionContinue, err
}

func (v *vertexProvider) TransformRequestBodyHeaders(ctx wrapper.HttpContext, apiName ApiName, body []byte, headers http.Header) ([]byte, error) {
	if apiName == ApiNameCompletion {
		var err error
		body, err = translateCompletionRequestToChatCompletionRequest(body)
		if err != nil {
			return nil, err
		}
		return v.onChatCompletionRequestBody(ctx, body, headers)
	}

	if apiName == ApiNameChatCompletion {
		return v.onChatCompletionRequestBody(ctx, body, headers)
	} else {
		return v.onEmbeddingsRequestBody(ctx, body, headers)
	}
}

func (v *vertexProvider) onChatCompletionRequestBody(ctx wrapper.HttpContext, body []byte, headers http.Header) ([]byte, error) {
	if newBody, err := vertex.DeleteInvalidRequestFields(body); err != nil {
		return nil, err
	} else {
		body = newBody
	}

	extendedParams, err := extractVertexExtendedParams(body)
	if err != nil {
		return nil, err
	}

	request := &chatCompletionRequest{}
	err = v.config.parseRequestAndMapModel(ctx, request, body)
	if err != nil {
		return nil, err
	}
	path := v.getRequestPath(ApiNameChatCompletion, request.Model, request.Stream)
	log.Debugf("[vertex]: request path: %s", path)
	util.OverwriteRequestPathHeader(headers, path)

	vertexRequest, err := v.buildVertexChatRequest(request, extendedParams)
	if err != nil {
		log.Errorf("[vertex]: unable to build valid vertex request: %v", err)
		return nil, err
	}

	vertexRequestBodyBytes, err := json.Marshal(vertexRequest)
	log.Debugf("[vertex]: request body after transform: %s", vertexRequestBodyBytes)
	return vertexRequestBodyBytes, err
}

func (v *vertexProvider) onEmbeddingsRequestBody(ctx wrapper.HttpContext, body []byte, headers http.Header) ([]byte, error) {
	request := &embeddingsRequest{}
	if err := v.config.parseRequestAndMapModel(ctx, request, body); err != nil {
		return nil, err
	}
	path := v.getRequestPath(ApiNameEmbeddings, request.Model, false)
	util.OverwriteRequestPathHeader(headers, path)

	vertexRequest := v.buildEmbeddingRequest(request)
	return json.Marshal(vertexRequest)
}

func (v *vertexProvider) OnStreamingEvent(ctx wrapper.HttpContext, name ApiName, event StreamEvent) ([]StreamEvent, error) {
	log.Infof("[vertexProvider] receive stream event: %+v", event)
	if name != ApiNameChatCompletion && name != ApiNameCompletion {
		return nil, nil
	}

	var out []StreamEvent

	var vertexResp vertexChatResponse
	if err := json.Unmarshal([]byte(event.Data), &vertexResp); err != nil {
		return nil, fmt.Errorf("unable to unmarshal vertex response: %v", err)
	}

	includeUsage := streamOptionsIncludeUsageFromContext(ctx)

	responses, isLastChunk := v.buildChatCompletionStreamResponse(ctx, &vertexResp)
	for _, response := range responses {
		responseBodyBytes, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal transformed vertex response: %w", err)
		}

		if includeUsage {
			modifiedBody, err := sjson.SetBytes(responseBodyBytes, "stream_options.include_usage", true)
			if err != nil {
				return nil, err
			}
			responseBodyBytes = modifiedBody
		}

		event.Data = string(responseBodyBytes) // modified event data in place

		if name == ApiNameCompletion {
			transformedEvents, err := handleCompletionsStreamingEvent(ctx, event)
			if err != nil {
				return nil, fmt.Errorf("failed to transform completions streaming event: %w", err)
			}
			out = append(out, transformedEvents...)
		} else {
			out = append(out, event)
		}
	}

	// vertex does not send a separate data: [DONE] message, so we need to add it ourselves
	if isLastChunk && !event.IsEndData() {
		out = append(out, StreamEvent{
			Data: streamEndDataValue,
		})
	}

	return out, nil
}

func (v *vertexProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	if apiName == ApiNameChatCompletion {
		return v.onChatCompletionResponseBody(ctx, body)
	} else if apiName == ApiNameCompletion {
		bodyBytes, err := v.onChatCompletionResponseBody(ctx, body)
		if err != nil {
			return nil, err
		}
		// transform to completion response format
		return transformCompletionsResponseFields(ctx, bodyBytes)
	} else {
		return v.onEmbeddingsResponseBody(ctx, body)
	}
}

func (v *vertexProvider) onChatCompletionResponseBody(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	vertexResponse := &vertexChatResponse{}
	if err := json.Unmarshal(body, vertexResponse); err != nil {
		return nil, fmt.Errorf("unable to unmarshal vertex chat response: %v", err)
	}
	vertexResponseBytes, err := json.Marshal(vertexResponse) // TODO delete
	if err != nil {
		log.Errorf("unable to marshal vertex response: %v", err)
	} else {
		log.Debugf("vertex response: %s", vertexResponseBytes)
	}
	response, groundingMetadataList, urlContextMetadataList, safetyRatingsList, citationMetadataList := v.buildChatCompletionResponse(ctx, vertexResponse)
	respBytes, err := json.Marshal(response)
	if err != nil {
		return nil, err
	}

	// Add vertex-specific fields to response
	respBytes, err = setVertexExtraFieldsToResponse(respBytes, groundingMetadataList, urlContextMetadataList, safetyRatingsList, citationMetadataList)
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}

func (v *vertexProvider) buildChatCompletionResponse(ctx wrapper.HttpContext, response *vertexChatResponse) (*chatCompletionResponse,
	[]*vertex.GroundingMetadata, []*vertex.URLContextMetadata, [][]*vertex.SafetyRatings, []*vertex.CitationMetadata) {

	if response.PromptFeedback != nil && response.PromptFeedback["blockReason"] != "" {
		return handleVertexBlockedResponse(ctx, response), nil, nil, nil, nil
	}

	out := &chatCompletionResponse{
		Id:      response.ResponseId,
		Object:  objectChatCompletion,
		Created: time.Now().UnixMilli() / 1000,
		Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
		Choices: make([]chatCompletionChoice, 0, len(response.Candidates)),
		Usage:   calculateVertexUsage(response.UsageMetadata),
	}

	var groundingMetadataList []*vertex.GroundingMetadata
	var urlContextMetadataList []*vertex.URLContextMetadata
	var safetyRatingsList [][]*vertex.SafetyRatings
	var citationMetadataList []*vertex.CitationMetadata

	for _, candidate := range response.Candidates {
		if candidate.Content == nil {
			continue
		}
		choice := buildChatCompletionChoice(candidate)
		out.Choices = append(out.Choices, *choice)

		if candidate.GroundingMetadata != nil {
			groundingMetadataList = append(groundingMetadataList, candidate.GroundingMetadata)
		}
		if candidate.UrlContextMetadata != nil {
			urlContextMetadataList = append(urlContextMetadataList, candidate.UrlContextMetadata)
		}
		if candidate.SafetyRatings != nil {
			safetyRatingsList = append(safetyRatingsList, candidate.SafetyRatings)
		}
		if candidate.CitationMetadata != nil {
			citationMetadataList = append(citationMetadataList, candidate.CitationMetadata)
		}
	}
	fillResponseToolCallIndex(out)
	return out, groundingMetadataList, urlContextMetadataList, safetyRatingsList, citationMetadataList
}

func buildChatCompletionChoice(candidate *vertex.Candidate) *chatCompletionChoice {
	choice := &chatCompletionChoice{
		Index: candidate.Index,
		Message: &chatMessage{
			Role: roleAssistant,
		},
	}

	if candidate.FinishReason != "" {
		choice.FinishReason = util.Ptr(util.MapFinishReason(candidate.FinishReason))
	}

	parts := candidate.Content.Parts
	for _, part := range parts {
		if part.FunctionCall != nil {
			if tc := buildToolCall(part); tc != nil {
				choice.Message.ToolCalls = append(choice.Message.ToolCalls, *tc)
			}
		}
	}

	content, reasoningContent := getAssistantContentMessage(parts)
	choice.Message.Content = content
	if reasoningContent != "" {
		choice.Message.ReasoningContent = reasoningContent
	}

	choice.Logprobs = transformVertexLogprobs(candidate.LogprobsResult)

	return choice
}

func buildToolCall(part *vertex.Part) *toolCall {
	fc := part.FunctionCall
	argsBytes, err := json.Marshal(fc.Args)
	if err != nil {
		log.Errorf("convert vertex function call: marshal args failed: %v", err)
		return nil
	}
	out := &toolCall{
		Id:   fmt.Sprintf("call_%s", uuid.New().String()),
		Type: "function",
		Function: functionCall{
			Arguments: string(argsBytes),
			Name:      fc.Name,
		},
	}

	// Extract thoughtSignature from Gemini response (for Gemini 3 models)
	// thoughtSignature can be in functionCall or at the part level
	thoughtSignature := part.FunctionCall.ThoughtSignature
	if thoughtSignature == "" {
		// Check if thoughtSignature is at the part level
		thoughtSignature = part.ThoughtSignature
	}
	if thoughtSignature != "" {
		if out.ProviderSpecificFields == nil {
			out.ProviderSpecificFields = make(map[string]interface{})
		}
		out.ProviderSpecificFields["thought_signature"] = thoughtSignature
	}

	return out
}

func (v *vertexProvider) onEmbeddingsResponseBody(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	vertexResponse := &vertexEmbeddingResponse{}
	if err := json.Unmarshal(body, vertexResponse); err != nil {
		return nil, fmt.Errorf("unable to unmarshal vertex embeddings response: %v", err)
	}
	response := v.buildEmbeddingsResponse(ctx, vertexResponse)
	return json.Marshal(response)
}

func (v *vertexProvider) buildEmbeddingsResponse(ctx wrapper.HttpContext, vertexResp *vertexEmbeddingResponse) *embeddingsResponse {
	response := embeddingsResponse{
		Object: "list",
		Data:   make([]embedding, 0, len(vertexResp.Predictions)),
		Model:  ctx.GetContext(ctxKeyFinalRequestModel).(string),
	}
	totalTokens := 0
	for _, item := range vertexResp.Predictions {
		response.Data = append(response.Data, embedding{
			Object:    `embedding`,
			Index:     0,
			Embedding: item.Embeddings.Values,
		})
		if item.Embeddings.Statistics != nil {
			totalTokens += item.Embeddings.Statistics.TokenCount
		}
	}
	response.Usage.TotalTokens = totalTokens
	return &response
}

func (v *vertexProvider) buildChatCompletionStreamResponse(ctx wrapper.HttpContext,
	vertexResp *vertexChatResponse) ([]*chatCompletionResponse, bool) {
	var chunk *vertex.Candidate
	isLastChunk := false
	if len(vertexResp.Candidates) > 0 {
		chunk = vertexResp.Candidates[0]
		isLastChunk = chunk.FinishReason != ""
	}

	var reasoningContent string
	var text string
	var tc *toolCall
	var finishReason string

	if chunk != nil && chunk.Content != nil && len(chunk.Content.Parts) > 0 {
		part := chunk.Content.Parts[0]
		if part.Text != "" {
			if part.Thought {
				reasoningContent = part.Text
			} else {
				text = part.Text
			}
		} else if part.FunctionCall != nil {
			tc = buildToolCall(part)
		}
	}

	if chunk != nil && chunk.FinishReason != "" {
		finishReason = util.MapFinishReason(chunk.FinishReason)
	}

	var toolCalls []toolCall
	if tc != nil {
		toolCalls = append(toolCalls, *tc)
	}

	var out []*chatCompletionResponse
	if !isLastChunk {
		choice := chatCompletionChoice{
			Index: 0,
			Delta: &chatMessage{
				Role:             roleAssistant,
				Content:          text,
				ReasoningContent: reasoningContent,
				ToolCalls:        toolCalls,
			},
			FinishReason: &finishReason,
		}

		out = append(out, &chatCompletionResponse{
			Id:      vertexResp.ResponseId,
			Object:  objectChatCompletionChunk,
			Created: time.Now().UnixMilli() / 1000,
			Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
			Choices: []chatCompletionChoice{choice},
		})
	} else {
		// for last chunk, split into three messages:
		// one with content only, one with finish reason only, and one with usage only

		if text != "" || reasoningContent != "" || len(toolCalls) > 0 {
			choiceWithContentOnly := chatCompletionChoice{
				Index: 0,
				Delta: &chatMessage{
					Role:             roleAssistant,
					Content:          text,
					ReasoningContent: reasoningContent,
					ToolCalls:        toolCalls,
				},
				FinishReason: nil,
			}
			out = append(out, &chatCompletionResponse{
				Id:      vertexResp.ResponseId,
				Object:  objectChatCompletionChunk,
				Created: time.Now().UnixMilli() / 1000,
				Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
				Choices: []chatCompletionChoice{choiceWithContentOnly},
			})
		}

		choiceWithFinishReasonOnly := chatCompletionChoice{
			Index:        0,
			Delta:        &chatMessage{}, // Align with the format returned by litellm
			FinishReason: &finishReason,
		}
		out = append(out, &chatCompletionResponse{
			Id:      vertexResp.ResponseId,
			Object:  objectChatCompletionChunk,
			Created: time.Now().UnixMilli() / 1000,
			Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
			Choices: []chatCompletionChoice{choiceWithFinishReasonOnly},
		})

		usageMetadata := vertexResp.UsageMetadata
		if usageMetadata != nil && usageMetadata.TotalTokenCount > 0 {
			finalUsage := &usage{
				PromptTokens:     usageMetadata.PromptTokenCount,
				CompletionTokens: usageMetadata.CandidatesTokenCount,
				TotalTokens:      usageMetadata.TotalTokenCount,
				CompletionTokensDetails: &completionTokensDetails{
					ReasoningTokens: usageMetadata.ThoughtsTokenCount,
				},
			}

			out = append(out, &chatCompletionResponse{
				Id:      vertexResp.ResponseId,
				Object:  objectChatCompletionChunk,
				Created: time.Now().UnixMilli() / 1000,
				Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
				Choices: []chatCompletionChoice{ // Align with the format returned by litellm
					{
						Index: 0,
						Delta: &chatMessage{},
					},
				},
				Usage: finalUsage,
			})
		}
	}

	for _, respItem := range out {
		fillResponseToolCallIndex(respItem)
	}

	return out, isLastChunk
}

func (v *vertexProvider) appendResponse(responseBuilder *strings.Builder, responseBody string) {
	responseBuilder.WriteString(fmt.Sprintf("%s %s\n\n", streamDataItemKey, responseBody))
}

func (v *vertexProvider) getRequestPath(apiName ApiName, modelId string, stream bool) string {
	action := ""
	if apiName == ApiNameEmbeddings {
		action = vertexEmbeddingAction
	} else if stream {
		action = vertexChatCompletionStreamAction
	} else {
		action = vertexChatCompletionAction
	}
	return fmt.Sprintf(vertexPathTemplate, v.config.vertexProjectId, v.config.vertexRegion, modelId, action)
}

func (v *vertexProvider) buildVertexChatRequest(request *chatCompletionRequest, extendedParams *vertexExtendedParams) (*vertexChatRequest, error) {
	toolConfig, err := mapToolChoiceValues(request.ToolChoice)
	if err != nil {
		return nil, fmt.Errorf("unable to build vertex request: %w", err)
	}
	safetySettings := make([]*vertex.SafetySettingsConfig, 0)
	for category, threshold := range v.config.geminiSafetySetting {
		safetySettings = append(safetySettings, &vertex.SafetySettingsConfig{
			Category:  category,
			Threshold: threshold,
		})
	}

	fixChatMessages(request.Messages)

	systemInstruction, messages := buildVertexReqSystemInstruction(request)

	generationConfig, err := buildVertexReqGenerationConfig(request, extendedParams)
	if err != nil {
		return nil, fmt.Errorf("unable to build vertex request: %w", err)
	}

	tools, err := buildVertexReqTools(extendedParams.tools, extendedParams.functions)
	if err != nil {
		return nil, fmt.Errorf("unable to build vertex request: %w", err)
	}

	contents, err := transformMessagesToVertexContents(messages)
	if err != nil {
		return nil, fmt.Errorf("unable to build vertex request: %w", err)
	}

	vertexRequest := &vertexChatRequest{
		Contents:          contents,
		SafetySettings:    safetySettings,
		GenerationConfig:  generationConfig,
		Tools:             tools,
		ToolConfig:        toolConfig,
		SystemInstruction: systemInstruction,
	}
	if extendedParams.CachedContent != "" {
		vertexRequest.CachedContent = extendedParams.CachedContent
	}
	if len(extendedParams.SafetySettings) > 0 {
		vertexRequest.SafetySettings = extendedParams.SafetySettings
	}
	return vertexRequest, nil
}

func (v *vertexProvider) buildEmbeddingRequest(request *embeddingsRequest) *vertexEmbeddingRequest {
	inputs := request.ParseInput()
	instances := make([]vertexEmbeddingInstance, len(inputs))
	for i, input := range inputs {
		instances[i] = vertexEmbeddingInstance{
			Content: input,
		}
	}
	return &vertexEmbeddingRequest{Instances: instances}
}

type vertexChatRequest struct {
	CachedContent     string                         `json:"cachedContent,omitempty"`
	Contents          []*vertex.Content              `json:"contents"`
	SystemInstruction *vertex.SystemInstruction      `json:"systemInstruction,omitempty"`
	Tools             []*vertex.Tools                `json:"tools,omitempty"`
	ToolConfig        *vertex.ToolConfig             `json:"toolConfig,omitempty"`
	SafetySettings    []*vertex.SafetySettingsConfig `json:"safetySettings,omitempty"`
	GenerationConfig  *vertex.GenerationConfig       `json:"generationConfig,omitempty"`
	Labels            map[string]string              `json:"labels,omitempty"`
}

type vertexEmbeddingRequest struct {
	Instances  []vertexEmbeddingInstance `json:"instances"`
	Parameters *vertexEmbeddingParams    `json:"parameters,omitempty"`
}

type vertexEmbeddingInstance struct {
	TaskType string `json:"task_type"`
	Title    string `json:"title,omitempty"`
	Content  string `json:"content"`
}

type vertexEmbeddingParams struct {
	AutoTruncate bool `json:"autoTruncate,omitempty"`
}

type vertexChatResponse struct {
	Candidates     []*vertex.Candidate   `json:"candidates"`
	ResponseId     string                `json:"responseId,omitempty"`
	PromptFeedback map[string]any        `json:"promptFeedback,omitempty"`
	UsageMetadata  *vertex.UsageMetadata `json:"usageMetadata,omitempty"`
}

type vertexEmbeddingResponse struct {
	Predictions []vertexPredictions `json:"predictions"`
}

type vertexPredictions struct {
	Embeddings struct {
		Values     []float64         `json:"values"`
		Statistics *vertexStatistics `json:"statistics,omitempty"`
	} `json:"embeddings"`
}

type vertexStatistics struct {
	TokenCount int  `json:"token_count"`
	Truncated  bool `json:"truncated"`
}

type ServiceAccountKey struct {
	ClientEmail  string `json:"client_email"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	TokenURI     string `json:"token_uri"`
}

func createJWT(key *ServiceAccountKey) (string, error) {
	// 解析 PEM 格式的 RSA 私钥
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		log.Errorf("[createJWT] failed to decode PEM block: %+v", key.PrivateKey)
		return "", fmt.Errorf("invalid PEM block")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	rsaKey := parsedKey.(*rsa.PrivateKey)

	// 构造 JWT Header
	jwtHeader := map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": key.PrivateKeyID,
	}
	headerJSON, _ := json.Marshal(jwtHeader)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)

	// 构造 JWT Claims
	now := time.Now().Unix()
	claims := map[string]interface{}{
		"iss":   key.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   key.TokenURI,
		"iat":   now,
		"exp":   now + 3600, // 1 小时有效期
	}
	claimsJSON, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)

	signingInput := fmt.Sprintf("%s.%s", headerB64, claimsB64)
	hashed := sha256.Sum256([]byte(signingInput))
	signature, err := rsaKey.Sign(nil, hashed[:], crypto.SHA256)
	if err != nil {
		return "", err
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(signature)

	return fmt.Sprintf("%s.%s.%s", headerB64, claimsB64, sigB64), nil
}

func (v *vertexProvider) getAccessToken(jwtToken string) error {
	headers := [][2]string{
		{"Content-Type", "application/x-www-form-urlencoded"},
	}
	reqBody := "grant_type=urn:ietf:params:oauth:grant-type:jwt-bearer&assertion=" + jwtToken
	err := v.client.Post("/token", headers, []byte(reqBody), func(statusCode int, responseHeaders http.Header, responseBody []byte) {
		responseString := string(responseBody)
		defer func() {
			_ = proxywasm.ResumeHttpRequest()
		}()
		if statusCode != http.StatusOK {
			log.Errorf("failed to create vertex access key, status: %d body: %s", statusCode, responseString)
			_ = util.ErrorHandler("ai-proxy.vertex.load_ak_failed", fmt.Errorf("failed to load vertex ak"))
			return
		}
		responseJson := gjson.Parse(responseString)
		accessToken := responseJson.Get("access_token").String()
		_ = proxywasm.ReplaceHttpRequestHeader("Authorization", "Bearer "+accessToken)

		expiresIn := int64(3600)
		if expiresInVal := responseJson.Get("expires_in"); expiresInVal.Exists() {
			expiresIn = expiresInVal.Int()
		}
		expireTime := time.Now().Add(time.Duration(expiresIn) * time.Second).Unix()
		keyName := v.buildTokenKey()
		err := setCachedAccessToken(keyName, accessToken, expireTime)
		if err != nil {
			log.Errorf("[vertex]: unable to cache access token: %v", err)
		}
	}, v.config.timeout)
	return err
}

func (v *vertexProvider) buildTokenKey() string {
	region := v.config.vertexRegion
	projectID := v.config.vertexProjectId

	return fmt.Sprintf("vertex-%s-%s-access-token", region, projectID)
}

type cachedAccessToken struct {
	Token    string `json:"token"`
	ExpireAt int64  `json:"expireAt"`
}

func (v *vertexProvider) getCachedAccessToken(key string) (string, error) {
	data, _, err := proxywasm.GetSharedData(key)
	if err != nil {
		if errors.Is(err, types.ErrorStatusNotFound) {
			return "", nil
		}
		return "", err
	}
	if data == nil {
		return "", nil
	}

	var tokenInfo cachedAccessToken
	if err = json.Unmarshal(data, &tokenInfo); err != nil {
		return "", err
	}

	now := time.Now().Unix()
	refreshAhead := v.config.vertexTokenRefreshAhead

	if tokenInfo.ExpireAt > now+refreshAhead {
		return tokenInfo.Token, nil
	}

	return "", nil
}

func setCachedAccessToken(key string, accessToken string, expireTime int64) error {
	tokenInfo := cachedAccessToken{
		Token:    accessToken,
		ExpireAt: expireTime,
	}

	_, cas, err := proxywasm.GetSharedData(key)
	if err != nil && !errors.Is(err, types.ErrorStatusNotFound) {
		return err
	}

	data, err := json.Marshal(tokenInfo)
	if err != nil {
		return err
	}

	return proxywasm.SetSharedData(key, data, cas)
}

func streamOptionsIncludeUsageFromContext(ctx wrapper.HttpContext) bool {
	includeUsage, ok := ctx.GetContext(ctxStreamIncludeUsage).(bool)
	return ok && includeUsage
}
