package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/sjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/vertex"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
)

// Reasoning effort thinking budget constants
// These match Python's constants.py values
// Note: Use var instead of const because ThinkingConfig.ThinkingBudget is a pointer to int
var (
	defaultReasoningEffortNoneThinkingBudget                     = 0
	defaultReasoningEffortDisableThinkingBudget                  = 0
	defaultReasoningEffortMinimalThinkingBudgetGemini25Flash     = 1
	defaultReasoningEffortMinimalThinkingBudgetGemini25Pro       = 128
	defaultReasoningEffortMinimalThinkingBudgetGemini25FlashLite = 512
	defaultReasoningEffortMinimalThinkingBudget                  = 128 // Generic fallback
	defaultReasoningEffortLowThinkingBudget                      = 1024
	defaultReasoningEffortMediumThinkingBudget                   = 2048
	defaultReasoningEffortHighThinkingBudget                     = 4096
)

func transformMessagesToVertexContents(messages []chatMessage) ([]*vertex.Content, error) {
	return geminiConvertMessagesWithHistory(messages)
}

func buildVertexTools(toolChoice interface{}, tools []*vertexExtendedTool, functions []*vertexFunction) ([]*vertex.Tools, *vertex.ToolConfig, error) {
	// Build toolConfig from toolChoice
	toolConfig, err := mapToolChoiceValues(toolChoice)
	if err != nil {
		return nil, nil, err
	}

	if len(tools) == 0 && len(functions) == 0 {
		return nil, toolConfig, nil
	}

	var functionDeclarations []*vertex.FunctionDeclaration

	builder := &vertexToolBuilder{}

	if tools != nil {
		for _, t := range tools {
			if t.Function != nil {
				fd, err := buildVertexFunctionDeclaration(t.Function.Name, t.Function.Description, t.Function.Parameters)
				if err != nil {
					return nil, toolConfig, err
				}
				functionDeclarations = append(functionDeclarations, fd)
			}
			builder.addTool(t.SpecificTool)
		}
	} else if functions != nil {
		for _, f := range functions {
			if f.Name != "" {
				fd, err := buildVertexFunctionDeclaration(f.Name, f.Description, f.Parameters)
				if err != nil {
					return nil, toolConfig, err
				}
				functionDeclarations = append(functionDeclarations, fd)
			}
			builder.addTool(vertex.SpecificTool{
				GoogleSearch:          f.GoogleSearch,
				GoogleSearchRetrieval: f.GoogleSearchRetrieval,
				EnterpriseWebSearch:   f.EnterpriseWebSearch,
				CodeExecution:         f.CodeExecution,
				GoogleMaps:            f.GoogleMaps,
				URLContext:            f.URLContext,
			})
		}
	}

	vertexTools := builder.getTools()
	if len(functionDeclarations) > 0 {
		vertexTools = append([]*vertex.Tools{
			{
				FunctionDeclarations: functionDeclarations,
			},
		}, vertexTools...)
	}

	// Apply retrieval config to toolConfig if needed
	if retrievalConfig := builder.getRetrievalConfig(); retrievalConfig != nil {
		if toolConfig == nil {
			toolConfig = &vertex.ToolConfig{}
		}
		if toolConfig.RetrievalConfig == nil {
			toolConfig.RetrievalConfig = retrievalConfig
		}
	}

	if len(vertexTools) == 0 {
		return nil, toolConfig, nil
	}

	return vertexTools, toolConfig, nil
}

// buildVertexFunctionDeclaration processes function parameters and creates a FunctionDeclaration
func buildVertexFunctionDeclaration(name, description string, parameters map[string]interface{}) (*vertex.FunctionDeclaration, error) {
	if len(parameters) > 0 {
		parameters = vertex.RemoveAdditionalProperties(parameters)
		parameters = vertex.RemoveStrictFromSchema(parameters)
		var err error
		parameters, err = vertex.BuildVertexSchema(parameters, false)
		if err != nil {
			return nil, fmt.Errorf("failed to build vertex function parameters schema: %w", err)
		}
	}

	return &vertex.FunctionDeclaration{
		Name:        name,
		Description: description,
		Parameters:  parameters,
	}, nil
}

// vertexToolBuilder helps build Tools and collect retrieval configs
type vertexToolBuilder struct {
	tools           []*vertex.Tools
	retrievalConfig *map[string]interface{}
}

func (b *vertexToolBuilder) addTool(ext vertex.SpecificTool) {
	if ext.GoogleSearch != nil {
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				GoogleSearch: ext.GoogleSearch,
			},
		})
	}
	if ext.GoogleSearchRetrieval != nil {
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				GoogleSearchRetrieval: ext.GoogleSearchRetrieval,
			},
		})
	}
	if ext.EnterpriseWebSearch != nil {
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				EnterpriseWebSearch: ext.EnterpriseWebSearch,
			},
		})
	}
	if ext.CodeExecution != nil {
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				CodeExecution: ext.CodeExecution,
			},
		})
	}
	if ext.GoogleMaps != nil {
		cleanedConfig, retrievalConfig := vertex.ExtractGoogleMapsRetrievalConfig(*ext.GoogleMaps)
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				GoogleMaps: &cleanedConfig,
			},
		})
		// Store retrieval config for toolConfig
		if len(retrievalConfig) > 0 {
			b.retrievalConfig = &retrievalConfig
		}
	}
	if ext.URLContext != nil {
		b.tools = append(b.tools, &vertex.Tools{
			SpecificTool: vertex.SpecificTool{
				URLContext: ext.URLContext,
			},
		})
	}
}

func (b *vertexToolBuilder) getTools() []*vertex.Tools {
	return b.tools
}

// getRetrievalConfig returns the collected retrieval config, or nil if none
func (b *vertexToolBuilder) getRetrievalConfig() *map[string]interface{} {
	return b.retrievalConfig
}

func mapToolChoiceValues(toolChoice interface{}) (*vertex.ToolConfig, error) {
	if toolChoice == nil {
		return nil, nil
	}
	var mode string
	if toolChoiceStr, ok := toolChoice.(string); ok {
		switch strings.ToLower(toolChoiceStr) {
		case "required":
			mode = "ANY"
		case "auto":
			mode = "AUTO"
		case "none":
			mode = "NONE"
		default:
			return nil, fmt.Errorf("cannot map tool choice: unknown value %s", toolChoiceStr)
		}
	}
	if mode != "" {
		return &vertex.ToolConfig{
			FunctionCallingConfig: &vertex.FunctionCallingConfig{
				Mode: mode,
			},
		}, nil
	}

	if toolChoiceMap, ok := toolChoice.(map[string]interface{}); ok {
		f := toolChoiceMap["function"]
		if f != nil {
			if fm, ok := f.(map[string]interface{}); ok {
				name := fm["name"]
				if nameStr, ok := name.(string); ok {
					return &vertex.ToolConfig{
						FunctionCallingConfig: &vertex.FunctionCallingConfig{
							Mode:                 "ANY",
							AllowedFunctionNames: []string{nameStr},
						},
					}, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("cannot map tool choice: %+v", toolChoice)
}

// buildVertexContentParts converts a message's multimodal content to Vertex AI parts
// Based on litellm's implementation: handles text, images, files, and audio
func buildVertexContentParts(msg *chatMessage) ([]*vertex.Part, error) {
	parts := make([]*vertex.Part, 0)
	// Parse multimodal content
	contentList := msg.ParseContent()
	for idx, c := range contentList {
		switch c.Type {
		case contentTypeText:
			// Text content
			if c.Text != "" {
				parts = append(parts, &vertex.Part{
					Text: c.Text,
				})
			}
		case contentTypeImageUrl:
			if c.ImageUrl != nil && c.ImageUrl.Url != "" {
				// Python extracts format from image_url dict, but Go struct doesn't have format field
				// So we pass empty string and let ProcessGeminiImage detect MIME type
				part, err := vertex.ProcessGeminiImage(c.ImageUrl.Url, "")
				if err != nil {
					log.Warnf("Failed to process image_url at index %d: %v", idx, err)
				} else {
					parts = append(parts, part)
				}
			}
		case contentTypeFile:
			if c.File != nil {
				// Following litellm's implementation: passed_file = file_id or file_data
				// Priority: file_id > file_data
				passedFile := c.File.FileId
				if passedFile == "" {
					passedFile = c.File.FileData
				}

				if passedFile == "" {
					return nil, fmt.Errorf("content type file at index %d has no file_id or file_data", idx)
				}

				format := c.File.Format
				part, err := vertex.ProcessGeminiImage(passedFile, format)
				if err != nil {
					return nil, fmt.Errorf("unable to process file at index %d: %w", idx, err)
				} else {
					parts = append(parts, part)
				}
			}
		case contentTypeInputAudio:
			if c.InputAudio != nil && c.InputAudio.Data != "" {
				part, err := vertex.ProcessGeminiImage(c.InputAudio.Data, c.InputAudio.Format)
				if err != nil {
					return nil, fmt.Errorf("unable to process audio at index %d: %w", idx, err)
				} else {
					parts = append(parts, part)
				}
			} else {
				log.Warnf("content type input_audio at index %d has no data: %v", idx, c.InputAudio)
			}
		}
	}
	return parts, nil
}

// buildVertexReqSystemInstruction extracts system messages and converts to Vertex AI format
// based on litellm's _transform_system_message implementation
func buildVertexReqSystemInstruction(req *chatCompletionRequest) (*vertex.SystemInstruction, []chatMessage) {
	var messages []chatMessage
	var systemParts []*vertex.Part
	for _, msg := range req.Messages {
		if msg.Role == roleSystem || msg.Role == roleDeveloper {
			// Extract system message content and add to parts
			content := msg.StringContent()
			if content == "" {
				continue
			}
			systemParts = append(systemParts, &vertex.Part{
				Text: content,
			})
		} else {
			messages = append(messages, msg)
		}
	}
	// If no system messages found, return nil
	if len(systemParts) == 0 {
		return nil, messages
	}

	// Return system instruction with all system message parts
	return &vertex.SystemInstruction{
		Role:  roleUser,
		Parts: systemParts,
	}, messages
}

// buildVertexReqGenerationConfig converts OpenAI request parameters to Vertex AI generation config
func buildVertexReqGenerationConfig(req *chatCompletionRequest, extendedParams *vertexExtendedParams) (*vertex.GenerationConfig, error) {
	out := &vertex.GenerationConfig{}
	isGemini3OrNewerModel := vertex.IsGemini3OrNewerModel(req.Model)
	// temperature -> generationConfig.temperature
	if req.Temperature != 0 {
		if req.Temperature < 1.0 && isGemini3OrNewerModel {
			out.Temperature = 1.0
		} else {
			out.Temperature = req.Temperature
		}
	}

	// topP -> generationConfig.topP
	if req.TopP != 0 {
		out.TopP = req.TopP
	}

	// set CandidateCount
	if extendedParams.CandidateCount > 0 {
		out.CandidateCount = extendedParams.CandidateCount
	} else if req.N != 0 {
		out.CandidateCount = req.N
	}

	// set StopSequences
	if extendedParams.StopSequences != nil {
		// 如果请求参数里同时有 "stop_sequences": [] 和 "stop": ["xxx"]，则以 "stop_sequences": [] 为准
		out.StopSequences = extendedParams.StopSequences
	} else if len(req.Stop) > 0 {
		out.StopSequences = req.Stop
	}

	// set MaxOutputTokens
	// priority：max_output_tokens > max_completion_tokens > max_tokens
	if extendedParams.MaxOutputTokens > 0 {
		out.MaxOutputTokens = extendedParams.MaxOutputTokens
	} else if req.MaxCompletionTokens != 0 {
		out.MaxOutputTokens = req.MaxCompletionTokens
	} else if req.MaxTokens != 0 {
		out.MaxOutputTokens = req.MaxTokens
	}

	// set ResponseModalities
	if len(extendedParams.ResponseModalities) > 0 {
		out.ResponseModalities = extendedParams.ResponseModalities
	} else {
		out.ResponseModalities = buildVertexResponseModalities(req.Modalities)
	}

	// presence_penalty -> generationConfig.presencePenalty
	if req.PresencePenalty != 0 {
		out.PresencePenalty = req.PresencePenalty
	}

	// frequency_penalty -> generationConfig.frequencyPenalty
	if req.FrequencyPenalty != 0 {
		out.FrequencyPenalty = req.FrequencyPenalty
	}

	err := applyResponseSchemaTransformation(out, req, extendedParams)
	if err != nil {
		return nil, fmt.Errorf("failed to apply response schema transformation: %w", err)
	}

	// seed -> generationConfig.seed
	if req.Seed != 0 {
		seed := req.Seed
		out.Seed = &seed
	}

	// set ResponseLogprobs
	if extendedParams.ResponseLogprobs != nil {
		out.ResponseLogprobs = extendedParams.ResponseLogprobs
	} else {
		// logprobs -> generationConfig.responseLogprobs
		if req.Logprobs {
			out.ResponseLogprobs = util.BoolPtr(true)
		}
	}

	// top_logprobs -> generationConfig.logprobs
	if req.TopLogprobs != 0 {
		out.Logprobs = &req.TopLogprobs
	}

	thinkingConfig, err := buildVertexReqThinkingConfig(req, extendedParams)
	if err != nil {
		return nil, fmt.Errorf("failed to build vertex thinking config: %w", err)
	}

	out.ThinkingConfig = thinkingConfig

	if extendedParams.ImageConfig != nil {
		out.ImageConfig = extendedParams.ImageConfig
	}

	if len(extendedParams.SpeechConfig) > 0 {
		out.SpeechConfig = extendedParams.SpeechConfig
	}

	return out, nil
}

var (
	modelsNotSupportThinking = map[string]bool{
		"gemini-3-pro-image-preview":     true,
		"gemini-2.5-flash-image-preview": true,
		"gemini-2.5-flash-image":         true,
		"gemini-2.0-flash-001":           true,
	}
)

func buildVertexReqThinkingConfig(req *chatCompletionRequest, extendedParams *vertexExtendedParams) (*vertex.ThinkingConfig, error) {
	passThroughThinkingConfig := extendedParams.ThinkingConfig
	if passThroughThinkingConfig != nil {
		return passThroughThinkingConfig, nil
	}

	model := req.Model
	if _, notSupport := modelsNotSupportThinking[model]; notSupport {
		return nil, nil
	}

	thinking := extendedParams.Thinking
	if thinking != nil {
		return mapThinkingParam(thinking), nil
	}

	// build think config from reasoning_effort
	if req.ReasoningEffort != "" {
		isGemini3OrNewer := vertex.IsGemini3OrNewerModel(req.Model)

		// For Gemini 3+ models, use thinking_level instead of thinking_budget
		if isGemini3OrNewer {
			thinkConfig, err := mapReasoningEffortToVertexThinkingLevel(req.ReasoningEffort, req.Model)
			if err != nil {
				return nil, fmt.Errorf("cannot map reasoning effort to thinking level: %w", err)
			}
			return thinkConfig, nil
		}

		// For older models, use thinking_budget
		thinkConfig, err := mapReasoningEffortToThinkingBudget(req.ReasoningEffort, req.Model)
		if err != nil {
			return nil, fmt.Errorf("cannot map reasoning effort to thinking budget: %w", err)
		}
		return thinkConfig, nil
	}

	return nil, nil
}

// buildVertexResponseModalities converts OpenAI modalities to Vertex AI responseModalities
// Mapping: text -> TEXT, image -> IMAGE, others -> MODALITY_UNSPECIFIED
func buildVertexResponseModalities(modalities []string) []string {
	if len(modalities) == 0 {
		return nil
	}
	out := make([]string, 0, len(modalities))
	for _, modality := range modalities {
		switch strings.ToLower(modality) {
		case "text":
			out = append(out, vertex.ModalityText)
		case "image":
			out = append(out, vertex.ModalityImage)
		default:
			out = append(out, vertex.ModalityUnspecified)
		}
	}
	return out
}

func applyResponseSchemaTransformation(out *vertex.GenerationConfig, req *chatCompletionRequest, extendedParams *vertexExtendedParams) error {
	// 透传 responseSchema 和 responseMimeType
	if extendedParams.ResponseSchema != nil {
		out.ResponseSchema = extendedParams.ResponseSchema
	}
	if extendedParams.ResponseMimeType != "" {
		out.ResponseMimeType = extendedParams.ResponseMimeType
	}
	if out.ResponseSchema != nil && out.ResponseMimeType != "" {
		// 如果已经有透传的 responseSchema 和 responseMimeType，不再解析 req.ResponseFormat
		return nil
	}

	// 从 req.ResponseFormat 解析 responseSchema 和 responseMimeType
	responseFormat := req.ResponseFormat
	if responseFormat == nil {
		return nil
	}

	// remove 'additionalProperties' from json schema
	responseFormat = vertex.RemoveAdditionalProperties(responseFormat)
	// remove 'strict' from json schema
	responseFormat = vertex.RemoveStrictFromSchema(responseFormat)

	responseType := ""
	if typeVal, ok := responseFormat["type"].(string); ok {
		responseType = typeVal
	}

	switch responseType {
	case "json_object":
		out.ResponseMimeType = "application/json"
	case "text":
		out.ResponseMimeType = "text/plain"
	default:
	}

	if responseFormat["response_schema"] != nil {
		if responseSchema, ok := responseFormat["response_schema"].(map[string]interface{}); ok {
			out.ResponseMimeType = "application/json"
			out.ResponseSchema = responseSchema
		}
	} else if responseType == "json_schema" {
		// extract schema from json_schema.schema
		if jsonSchema, ok1 := responseFormat["json_schema"].(map[string]interface{}); ok1 {
			if schema, ok2 := jsonSchema["schema"].(map[string]interface{}); ok2 {
				out.ResponseMimeType = "application/json"
				out.ResponseSchema = schema
			}
		}
	}

	if out.ResponseSchema != nil {
		var err error
		out.ResponseSchema, err = vertex.MapResponseSchema(out.ResponseSchema)
		if err != nil {
			return fmt.Errorf("failed to map response schema: %w", err)
		}
	}

	return nil
}

// truncateString truncates a string to the specified length for logging
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// getAssistantContentMessage extracts text content and reasoning content from Vertex AI response parts
// Returns: (content, reasoningContent)
// Based on litellm's get_assistant_content_message implementation
func getAssistantContentMessage(parts []*vertex.Part) (string, string) {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	hasContent := false
	hasReasoning := false

	for _, part := range parts {
		partContent := ""
		if part.Text != "" {
			partContent = part.Text
		} else if part.InlineData != nil {
			// base64 encoded data - include as data URI
			partContent = fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, part.InlineData.Data)
		}

		if len(partContent) > 0 {
			if part.Thought {
				reasoningBuilder.WriteString(partContent)
				hasReasoning = true
			} else {
				contentBuilder.WriteString(partContent)
				hasContent = true
			}
		}
	}

	content := ""
	if hasContent {
		content = contentBuilder.String()
	}

	reasoningContent := ""
	if hasReasoning {
		reasoningContent = reasoningBuilder.String()
	}

	return content, reasoningContent
}

func setVertexExtraFieldsToResponse(respBytes []byte,
	groundingMetadataList []*vertex.GroundingMetadata,
	urlContextMetadataList []*vertex.URLContextMetadata,
	safetyRatingsList [][]*vertex.SafetyRatings,
	citationMetadataList []*vertex.CitationMetadata) ([]byte, error) {

	var err error

	if groundingMetadataList == nil {
		groundingMetadataList = make([]*vertex.GroundingMetadata, 0)
	}
	if urlContextMetadataList == nil {
		urlContextMetadataList = make([]*vertex.URLContextMetadata, 0)
	}
	if safetyRatingsList == nil {
		safetyRatingsList = make([][]*vertex.SafetyRatings, 0)
	}
	if citationMetadataList == nil {
		citationMetadataList = make([]*vertex.CitationMetadata, 0)
	}

	// Add grounding metadata
	groundingMetadataBytes, marshalErr := json.Marshal(groundingMetadataList)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal grounding metadata: %v", marshalErr)
	}
	respBytes, err = sjson.SetRawBytes(respBytes, "vertex_ai_grounding_metadata", groundingMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to set grounding metadata: %v", err)
	}

	// Add URL context metadata
	urlContextMetadataBytes, marshalErr := json.Marshal(urlContextMetadataList)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal URL context metadata: %v", marshalErr)
	}
	respBytes, err = sjson.SetRawBytes(respBytes, "vertex_ai_url_context_metadata", urlContextMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to set URL context metadata: %v", err)
	}

	// Add safety ratings
	safetyRatingsBytes, marshalErr := json.Marshal(safetyRatingsList)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal safety ratings: %v", marshalErr)
	}
	respBytes, err = sjson.SetRawBytes(respBytes, "vertex_ai_safety_results", safetyRatingsBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to set safety ratings: %v", err)
	}

	// Add citation metadata
	citationMetadataBytes, marshalErr := json.Marshal(citationMetadataList)
	if marshalErr != nil {
		return nil, fmt.Errorf("failed to marshal citation metadata: %v", marshalErr)
	}
	respBytes, err = sjson.SetRawBytes(respBytes, "vertex_ai_citation_metadata", citationMetadataBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to set citation metadata: %v", err)
	}

	return respBytes, nil
}

func handleVertexBlockedResponse(ctx wrapper.HttpContext, response *vertexChatResponse) *chatCompletionResponse {
	finishReason := "content_filter"
	out := &chatCompletionResponse{
		Id:      response.ResponseId,
		Object:  objectChatCompletion,
		Created: time.Now().UnixMilli() / 1000,
		Model:   ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
		Choices: []chatCompletionChoice{
			{
				Index: 0,
				Message: &chatMessage{
					Role:    roleAssistant,
					Content: "",
				},
				FinishReason: &finishReason,
			},
		},
		Usage: &usage{
			PromptTokens:     response.UsageMetadata.PromptTokenCount,
			CompletionTokens: response.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      response.UsageMetadata.TotalTokenCount,
		},
	}
	return out
}

// setVertexUsageToResponse sets Vertex AI usage metadata to the response body using sjson
func setVertexUsageToResponse(respBytes []byte, usageMetadata *vertex.UsageMetadata) ([]byte, error) {
	if usageMetadata == nil || usageMetadata.TotalTokenCount == 0 {
		return respBytes, nil
	}

	openaiFormatUsage := vertex.ConvertVertexUsage(usageMetadata)
	openaiFormatUsageBytes, err := json.Marshal(openaiFormatUsage)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal usage: %w", err)
	}

	respBytes, err = sjson.SetRawBytes(respBytes, "usage", openaiFormatUsageBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to set usage field: %w", err)
	}

	return respBytes, nil
}

// mapThinkingParam maps Claude-style thinking parameter to Vertex AI ThinkingConfig.
// This is equivalent to Python's _map_thinking_param method.
func mapThinkingParam(thinking *vertex.ClaudeStyleThinking) *vertex.ThinkingConfig {
	if thinking == nil {
		return nil
	}

	out := &vertex.ThinkingConfig{
		ThinkingBudget: thinking.BudgetTokens,
	}
	if thinking.Type != nil && *thinking.Type == "enabled" {
		out.IncludeThoughts = util.BoolPtr(true)
	}
	return out
}

// mapReasoningEffortToThinkingBudget is equivalent to Python's _map_reasoning_effort_to_thinking_budget method.
func mapReasoningEffortToThinkingBudget(reasoningEffort string, model string) (*vertex.ThinkingConfig, error) {
	switch reasoningEffort {
	case "minimal":
		// Use model-specific minimum thinking budget or fallback
		// Check for exact matches first, then partial matches
		budget := defaultReasoningEffortMinimalThinkingBudget
		modelLower := strings.ToLower(model)

		if strings.Contains(modelLower, "gemini-2.5-flash-lite") {
			budget = defaultReasoningEffortMinimalThinkingBudgetGemini25FlashLite
		} else if strings.Contains(modelLower, "gemini-2.5-pro") {
			budget = defaultReasoningEffortMinimalThinkingBudgetGemini25Pro
		} else if strings.Contains(modelLower, "gemini-2.5-flash") {
			budget = defaultReasoningEffortMinimalThinkingBudgetGemini25Flash
		}

		return &vertex.ThinkingConfig{
			ThinkingBudget:  &budget,
			IncludeThoughts: util.BoolPtr(true),
		}, nil

	case "low":
		return &vertex.ThinkingConfig{
			ThinkingBudget:  &defaultReasoningEffortLowThinkingBudget,
			IncludeThoughts: util.BoolPtr(true),
		}, nil

	case "medium":
		return &vertex.ThinkingConfig{
			ThinkingBudget:  &defaultReasoningEffortMediumThinkingBudget,
			IncludeThoughts: util.BoolPtr(true),
		}, nil

	case "high":
		return &vertex.ThinkingConfig{
			ThinkingBudget:  &defaultReasoningEffortHighThinkingBudget,
			IncludeThoughts: util.BoolPtr(true),
		}, nil

	case "disable":
		return &vertex.ThinkingConfig{
			ThinkingBudget:  &defaultReasoningEffortDisableThinkingBudget,
			IncludeThoughts: util.BoolPtr(false),
		}, nil

	case "none":
		return &vertex.ThinkingConfig{
			ThinkingBudget:  &defaultReasoningEffortNoneThinkingBudget,
			IncludeThoughts: util.BoolPtr(false),
		}, nil

	default:
		return nil, util.BadRequest("unknown reasoning_effort: " + reasoningEffort)
	}
}

var (
	gemini3FlashSupportedThinkingLevels = map[string]bool{
		vertex.ThinkingLevelMinimal: true,
		vertex.ThinkingLevelLow:     true,
		vertex.ThinkingLevelMedium:  true,
		vertex.ThinkingLevelHigh:    true,
	}
)

// mapReasoningEffortToVertexThinkingLevel maps reasoning_effort to thinking_level for Gemini 3+ models.
// This is equivalent to Python's _map_reasoning_effort_to_thinking_level method.
func mapReasoningEffortToVertexThinkingLevel(reasoningEffort string, model string) (*vertex.ThinkingConfig, error) {
	if strings.Contains(model, "gemini-3-flash") {
		if gemini3FlashSupportedThinkingLevels[reasoningEffort] {
			return &vertex.ThinkingConfig{
				ThinkingLevel:   &reasoningEffort,
				IncludeThoughts: util.BoolPtr(true),
			}, nil
		}
	}

	switch reasoningEffort {
	case "minimal", "low":
		return &vertex.ThinkingConfig{
			ThinkingLevel:   &vertex.ThinkingLevelLow,
			IncludeThoughts: util.BoolPtr(true),
		}, nil
	case "medium":
		// medium is not out yet, so we use "high"
		return &vertex.ThinkingConfig{
			ThinkingLevel:   &vertex.ThinkingLevelHigh,
			IncludeThoughts: util.BoolPtr(true),
		}, nil
	case "high":
		return &vertex.ThinkingConfig{
			ThinkingLevel:   &vertex.ThinkingLevelHigh,
			IncludeThoughts: util.BoolPtr(true),
		}, nil
	case "disable", "none":
		// Gemini 3 cannot fully disable thinking, so we use "low" but hide thoughts
		return &vertex.ThinkingConfig{
			ThinkingLevel:   &vertex.ThinkingLevelLow,
			IncludeThoughts: util.BoolPtr(false),
		}, nil
	default:
		return nil, util.BadRequest("unknown reasoning effort: " + reasoningEffort)
	}
}

// geminiConvertMessagesWithHistory converts OpenAI format messages to Gemini format with history support.
// This is equivalent to Python's _gemini_convert_messages_with_history function.
func geminiConvertMessagesWithHistory(messages []chatMessage) ([]*vertex.Content, error) {
	userMessageTypes := map[string]bool{
		roleUser:   true,
		roleSystem: true,
	}

	contents := make([]*vertex.Content, 0)
	var lastMessageWithToolCalls *chatMessage

	msgIndex := 0
	toolCallResponses := make([]*vertex.Part, 0)

	for msgIndex < len(messages) {
		initMsgIndex := msgIndex
		userContent := make([]*vertex.Part, 0)

		// MERGE CONSECUTIVE USER/SYSTEM CONTENT
		for msgIndex < len(messages) && userMessageTypes[messages[msgIndex].Role] {
			msg := &messages[msgIndex]
			parts, err := buildVertexContentParts(msg)
			if err != nil {
				return nil, fmt.Errorf("failed to build vertex content parts: %w", err)
			}
			userContent = append(userContent, parts...)
			msgIndex += 1
		}

		if len(userContent) > 0 {
			// Check that user_content has 'text' parameter
			// Known Vertex Error: Unable to submit request because it must have a text parameter
			// Relevant Issue: https://github.com/BerriAI/litellm/issues/5515
			hasText := vertex.CheckTextInContent(userContent)
			if !hasText {
				log.Info("No text in user content. Adding a blank text to user content, to ensure Gemini doesn't fail the request.")
				userContent = append(userContent, &vertex.Part{Text: " "})
			}
			contents = append(contents, &vertex.Content{
				Role:  roleUser,
				Parts: userContent,
			})
		}

		// MERGE CONSECUTIVE ASSISTANT CONTENT
		assistantContent := make([]*vertex.Part, 0)
		for msgIndex < len(messages) && messages[msgIndex].Role == roleAssistant {
			msg := &messages[msgIndex]

			// Handle message content
			messageContent := msg.ParseContent()
			for _, c := range messageContent {
				if c.Type == contentTypeText && c.Text != "" {
					assistantContent = append(assistantContent, &vertex.Part{
						Text: c.Text,
					})
				}
			}

			// HANDLE ASSISTANT FUNCTION CALL / TOOL CALLS
			if len(msg.ToolCalls) > 0 || msg.FunctionCall != nil {
				toolCallParts := convertToGeminiToolCallInvoke(msg)
				assistantContent = append(assistantContent, toolCallParts...)
				lastMessageWithToolCalls = msg
			}

			msgIndex += 1
		}

		if len(assistantContent) > 0 {
			contents = append(contents, &vertex.Content{
				Role:  "model", // Map assistant to model
				Parts: assistantContent,
			})
		}

		// APPEND TOOL CALL MESSAGES
		toolCallMessageRoles := map[string]bool{
			"tool":     true,
			"function": true,
		}

		if msgIndex < len(messages) && toolCallMessageRoles[messages[msgIndex].Role] {
			part, err := convertToGeminiToolCallResult(&messages[msgIndex], lastMessageWithToolCalls) // TODO
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool call result: %w", err)
			}
			msgIndex++
			toolCallResponses = append(toolCallResponses, part)
		}

		if msgIndex < len(messages) && !toolCallMessageRoles[messages[msgIndex].Role] {
			if len(toolCallResponses) > 0 {
				contents = append(contents, &vertex.Content{
					Parts: toolCallResponses,
				})
				toolCallResponses = make([]*vertex.Part, 0)
			}
		}

		// Prevent infinite loops
		if msgIndex == initMsgIndex {
			return nil, fmt.Errorf("invalid message passed in: %+v", messages[msgIndex])
		}
	}

	// Append any remaining tool call responses
	if len(toolCallResponses) > 0 {
		contents = append(contents, &vertex.Content{
			Parts: toolCallResponses,
		})
	}

	return contents, nil
}

// convertToGeminiToolCallInvoke converts OpenAI tool calls to Gemini function call parts
// This is equivalent to Python's convert_to_gemini_tool_call_invoke
func convertToGeminiToolCallInvoke(msg *chatMessage) []*vertex.Part {
	parts := make([]*vertex.Part, 0)

	// Handle tool_calls (OpenAI format)
	if len(msg.ToolCalls) > 0 {
		for _, tc := range msg.ToolCalls {
			if tc.Function.Name == "" {
				continue
			}

			args := make(map[string]interface{})
			if tc.Function.Arguments != "" {
				// Try to parse JSON arguments
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					log.Warnf("Failed to parse tool call arguments: %v", err)
				}
			}

			fcPart := &vertex.Part{
				FunctionCall: &vertex.FunctionCall{
					Name: tc.Function.Name,
					Args: args,
				},
			}

			thoughtSignature := getThoughtSignatureFromProviderSpecificFields(tc.ProviderSpecificFields)
			if thoughtSignature != "" {
				fcPart.ThoughtSignature = thoughtSignature
				log.Debugf("tool call %s thought signature: %s", tc.Function.Name, thoughtSignature)
			}

			parts = append(parts, fcPart)
		}
	}

	// Handle function_call (legacy OpenAI format)
	if msg.FunctionCall != nil {
		args := make(map[string]interface{})
		if msg.FunctionCall.Arguments != "" {
			if err := json.Unmarshal([]byte(msg.FunctionCall.Arguments), &args); err != nil {
				log.Warnf("Failed to parse function call arguments: %v", err)
			}
		}

		fcPart := &vertex.Part{
			FunctionCall: &vertex.FunctionCall{
				Name: msg.FunctionCall.Name,
				Args: args,
			},
		}

		thoughtSignature := getThoughtSignatureFromProviderSpecificFields(msg.FunctionCall.ProviderSpecificFields)
		if thoughtSignature != "" {
			fcPart.ThoughtSignature = thoughtSignature
			log.Debugf("function call %s thought signature: %s", msg.FunctionCall.Name, thoughtSignature)
		}

		parts = append(parts, fcPart)
	}

	return parts
}

// convertToGeminiToolCallResult converts OpenAI tool/function response to Gemini function response part
// This is equivalent to Python's convert_to_gemini_tool_call_result
func convertToGeminiToolCallResult(msg *chatMessage, lastMessageWithToolCalls *chatMessage) (*vertex.Part, error) {
	name := msg.Name

	// Recover name from last message with tool calls
	if lastMessageWithToolCalls != nil {
		toolCalls := lastMessageWithToolCalls.ToolCalls
		msgToolCallId := msg.ToolCallId
		for _, tc := range toolCalls {
			prevToolCallId := tc.Id
			if msgToolCallId != "" && prevToolCallId != "" && msgToolCallId == prevToolCallId {
				name = tc.Function.Name
			}
		}
	}

	if name == "" {
		log.Warnf("Missing corresponding tool call for tool response message. Received - message=%+v, last_message_with_tool_calls=%+v",
			msg.StringContent(), lastMessageWithToolCalls)
		return nil, fmt.Errorf("failed to corresponding tool call for tool response message: %s", msg.ToolCallId)
	}

	return &vertex.Part{
		FunctionResponse: &vertex.FunctionResponse{
			Name: name,
			Response: map[string]interface{}{
				"content": msg.StringContent(),
			},
		},
	}, nil
}

func getThoughtSignatureFromProviderSpecificFields(providerSpecificFields map[string]interface{}) string {
	if providerSpecificFields == nil {
		return ""
	}

	if thoughtSig, ok1 := providerSpecificFields["thought_signature"]; ok1 {
		if sigStr, ok2 := thoughtSig.(string); ok2 {
			return sigStr
		}
	}
	return ""
}

// transformVertexLogprobs converts Vertex AI logprobs format to OpenAI format
// Based on Python's _transform_logprobs implementation
func transformVertexLogprobs(logProbsResult *vertex.LogprobsResult) map[string]interface{} {
	if logProbsResult == nil {
		return nil
	}
	if len(logProbsResult.ChosenCandidates) == 0 {
		return nil
	}

	logprobsList := make([]map[string]interface{}, 0, len(logProbsResult.ChosenCandidates))

	for index, candidate := range logProbsResult.ChosenCandidates {
		topLogprobs := make([]map[string]interface{}, 0)

		if index < len(logProbsResult.TopCandidates) {
			topCandidatesForIndex := logProbsResult.TopCandidates[index].Candidates

			for _, option := range topCandidatesForIndex {
				topLogprobs = append(topLogprobs, map[string]interface{}{
					"token":   option.Token,
					"logprob": option.LogProbability,
				})
			}
		}

		tokenLogprob := map[string]interface{}{
			"token":        candidate.Token,
			"logprob":      candidate.LogProbability,
			"top_logprobs": topLogprobs,
		}

		logprobsList = append(logprobsList, tokenLogprob)
	}

	return map[string]interface{}{
		"content": logprobsList,
	}
}
