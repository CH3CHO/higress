package provider

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/higress-group/wasm-go/pkg/log"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/bedrock"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util/templates"
)

// transformToBedrockConverseRequest converts an OpenAI chat completion request to Bedrock Converse API format.
// This function corresponds to _transform_request in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
//
// It performs the following transformations:
// - Extracts and converts system messages
// - Transforms messages to Bedrock format
// - Converts tools and tool_choice
// - Builds inference configuration
//
// Returns:
//   - *bedrock.ConverseRequest: the transformed request
//   - bool: whether JSON mode is enabled
//   - error: any error that occurred during transformation
func transformToBedrockConverseRequest(request *chatCompletionRequest, opts *bedrock.TransformRequestOptions,
	extendedParams *bedrockExtendedParams) (*bedrock.ConverseRequest, bool, error) {
	isThinkingEnabled := isBedrockThinkingEnabled(extendedParams.Thinking, request.ReasoningEffort)

	// Handle response_format parameter
	jsonModeEnabled := handleBedrockResponseFormat(request, isThinkingEnabled)

	out := &bedrock.ConverseRequest{}
	// Extract system messages and remaining messages
	messages, systemContentBlocks := transformSystemMessage(request.Messages)

	out.System = systemContentBlocks

	fixChatMessages(messages)

	// VALIDATE REQUEST: Bedrock doesn't support tool calling without `tools=` param specified.
	if len(messages) > 0 && hasToolCallMessage(messages) && len(request.Tools) == 0 {
		return nil, false, util.BadRequest("bedrock doesn't support tool calling without `tools=` param specified. Pass `tools=` param")
	}

	// Build additional model request fields
	additionalModelRequestFields, err := buildBedrockAdditionalModelRequestFields(request, extendedParams)
	if err != nil {
		return nil, false, err
	}
	out.AdditionalModelRequestFields = additionalModelRequestFields

	inferenceConfig := buildBedrockInferenceConfig(request, extendedParams)
	if inferenceConfig != nil {
		out.InferenceConfig = inferenceConfig
	}

	// Transform messages to Bedrock format
	bedrockMessages, err := transformMessagesToBedrockMessageBlocks(messages, &bedrock.TransformMessagesOptions{
		UserContinueMessage: opts.UserContinueMessage,
		ModifyParams:        opts.ModifyParams,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to transform messages: %w", err)
	}
	out.Messages = bedrockMessages

	// Transform tools
	if len(request.Tools) > 0 {
		bedrockTools, err := transformBedrockTools(request.Tools)
		if err != nil {
			return nil, false, fmt.Errorf("failed to transform tools: %w", err)
		}

		toolConfig := &bedrock.ToolConfigBlock{
			Tools: bedrockTools,
		}

		// Transform tool_choice
		if request.ToolChoice != nil && !isThinkingEnabled {
			toolChoiceValues, err := bedrock.MapToolChoiceValues(request.ToolChoice, true)
			if err != nil {
				return nil, false, err
			}
			if toolChoiceValues != nil {
				toolConfig.ToolChoice = toolChoiceValues
			}
		}

		out.ToolConfig = toolConfig
	}

	return out, jsonModeEnabled, nil
}

// transformBedrockConverseResponse transforms a Bedrock Converse API response to OpenAI-compatible format.
// This function corresponds to _transform_response in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
//
// Parameters:
//   - responseBody: raw JSON response from Bedrock Converse API
//   - opts: transformation options including model name and JSON mode flag
//
// Returns:
//   - *chatCompletionResponse: OpenAI-compatible response (uses existing type from model.go)
//   - error: error if parsing or transformation fails
func transformBedrockConverseResponse(responseBody []byte, opts *bedrock.TransformResponseOptions) (*chatCompletionResponse, *bedrock.OpenAIFormatUsage, error) {
	// Parse the Bedrock response
	var converseResponse bedrock.ConverseResponseBlock
	if err := json.Unmarshal(responseBody, &converseResponse); err != nil {
		return nil, nil, fmt.Errorf("failed to parse Bedrock response: %w", err)
	}

	// Initialize the chat completion message
	chatMsg := &chatMessage{
		Role: roleAssistant,
	}

	var contentStr string
	var tools []toolCall
	var reasoningContentBlocks []*bedrock.ConverseReasoningContentBlock

	currentToolCallIndex := 0
	if converseResponse.Output != nil && converseResponse.Output.Message != nil {
		messageBlock := converseResponse.Output.Message
		for _, content := range messageBlock.Content {
			// Content is either a tool response or text

			if content.Text != nil {
				contentStr += *content.Text
			}

			if content.ToolUse != nil {
				// Get original tool name (reverse the normalization)
				responseToolName := bedrock.GetOriginalToolName(content.ToolUse.Name)

				// Serialize tool input to JSON string
				argsBytes, err := json.Marshal(content.ToolUse.Input)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to serialize content.ToolUse.Input: %w", err)
				}

				currentToolCallIndex++
				tc := toolCall{
					Index: currentToolCallIndex,
					Id:    content.ToolUse.ToolUseID,
					Type:  "function",
					Function: functionCall{
						Name:      responseToolName,
						Arguments: string(argsBytes),
					},
				}
				tools = append(tools, tc)
			}

			if content.ReasoningContent != nil {
				reasoningContentBlocks = append(reasoningContentBlocks, content.ReasoningContent)
			}
		}
	}

	// Transform reasoning content blocks to both reasoning_content (string) and thinking_blocks (structured)
	if len(reasoningContentBlocks) > 0 {
		// Store original reasoningContentBlocks in ProviderSpecificFields
		chatMsg.ProviderSpecificFields = map[string]interface{}{
			"reasoningContentBlocks": reasoningContentBlocks,
		}

		// Transform to string format
		chatMsg.ReasoningContent = bedrock.TransformReasoningContent(reasoningContentBlocks)

		// Transform to structured thinking blocks
		chatMsg.ThinkingBlocks = bedrock.TransformThinkingBlocks(reasoningContentBlocks)
	}

	chatMsg.Content = contentStr

	// Handle JSON mode: use tool call arguments as content
	if opts.JSONMode && len(tools) == 1 {
		// to support 'json_schema' logic on bedrock models
		chatMsg.Content = tools[0].Function.Arguments
	} else {
		chatMsg.ToolCalls = tools
	}

	responseUsage := bedrock.TransformUsageToOpenAIFormat(converseResponse.Usage)

	// Map finish reason using util.MapFinishReason
	finishReason := util.MapFinishReason(converseResponse.StopReason)

	// Build the response
	response := &chatCompletionResponse{
		Id:      opts.RequestID,
		Object:  objectChatCompletion,
		Created: time.Now().Unix(),
		Model:   opts.Model,
		Choices: []chatCompletionChoice{
			{
				Index:        0,
				Message:      chatMsg,
				FinishReason: &finishReason,
			},
		},
	}

	return response, responseUsage, nil
}

// buildBedrockAdditionalModelRequestFields builds the additionalModelRequestFields map for Bedrock Converse API.
// It handles:
// - TopK parameter (with both TopK and Top_K variants)
// - Thinking mode configuration (including budget tokens and max_tokens calculation)
//
// Parameters:
//   - request: The chat completion request
//   - extendedParams: Extended Bedrock-specific parameters
//   - isThinkingEnabled: Whether thinking mode is enabled
//
// Returns:
//   - map[string]interface{}: The additional model request fields
//   - error: Any error that occurred during processing
func buildBedrockAdditionalModelRequestFields(request *chatCompletionRequest, extendedParams *bedrockExtendedParams) (map[string]interface{}, error) {
	out := make(map[string]interface{})

	// Handle TopK parameter - support both TopK and Top_K variants
	// If both are provided, Top_K takes precedence
	if extendedParams.TopK > 0 && extendedParams.Top_K > 0 {
		out["top_k"] = extendedParams.Top_K
	} else {
		if extendedParams.TopK > 0 {
			out["top_k"] = extendedParams.TopK
		} else if extendedParams.Top_K > 0 {
			out["top_k"] = extendedParams.Top_K
		}
	}

	// Handle thinking parameter
	// If thinking is not explicitly provided, try to derive it from reasoning_effort
	thinking := extendedParams.Thinking
	if len(thinking) == 0 {
		thinkingFromReasoningEffort, err := bedrock.MapReasoningEffort(request.ReasoningEffort)
		if err != nil {
			return nil, err
		}
		thinking = thinkingFromReasoningEffort
	}

	if len(thinking) > 0 {
		out["thinking"] = thinking
		// Try to get budget_tokens from thinking config
		budgetTokens := bedrock.ParseThinkingBudgetTokens(thinking)
		if budgetTokens > 0 && request.MaxTokens == 0 {
			// Set max_tokens = budget_tokens + DEFAULT_MAX_TOKENS (256)
			// This matches Python's: optional_params["max_tokens"] = thinking_token_budget + DEFAULT_MAX_TOKENS
			out["max_tokens"] = budgetTokens + 256
			log.Debugf("[bedrock] thinking enabled with budget_tokens=%d, setting max_tokens=%d",
				budgetTokens, out["max_tokens"])
		}
	}

	if len(extendedParams.EnableThinking) > 0 {
		out["enable_thinking"] = extendedParams.EnableThinking
	}

	anthropicBeta := bedrock.GetAnthropicBetaFromHeaders(extendedParams.ExtraHeaders)
	if len(anthropicBeta) > 0 {
		out["anthropic_beta"] = anthropicBeta
	}

	return out, nil
}

// transformBedrockTools converts OpenAI-style tools to Bedrock ToolBlock format.
//
// This function corresponds to _bedrock_tools_pt in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func transformBedrockTools(tools []tool) ([]*bedrock.ToolBlock, error) {
	out := make([]*bedrock.ToolBlock, 0, len(tools))

	for _, t := range tools {
		// Get parameters from the function definition
		parameters := t.Function.Parameters
		if parameters == nil {
			parameters = map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}
		}

		// Get the tool name
		name := t.Function.Name

		// Normalize the tool name to satisfy Bedrock requirements
		// Tool names must match pattern: [a-zA-Z][a-zA-Z0-9_]*
		// Related issue: https://github.com/BerriAI/litellm/issues/5007
		name = bedrock.NormalizeToolName(name)

		// Get description from function definition
		// Bedrock Converse API requires a description, use name as fallback
		description := t.Function.Description
		if description == "" {
			description = name
		}

		// Extract and remove $defs from parameters
		defs := make(map[string]interface{})
		if defsVal, ok := parameters["$defs"].(map[string]interface{}); ok {
			defs = defsVal
			delete(parameters, "$defs")
		}

		// Deep copy defs for processing
		defsCopy := bedrock.DeepCopyMap(defs)

		// Flatten the defs - unpack nested references
		for _, value := range defsCopy {
			if valueMap, ok := value.(map[string]interface{}); ok {
				templates.UnpackDefs(valueMap, defsCopy)
			}
		}

		// Unpack references in the main parameters schema
		templates.UnpackDefs(parameters, defsCopy)

		// Build the Bedrock tool structure
		out = append(out, &bedrock.ToolBlock{
			ToolSpec: &bedrock.ToolSpecBlock{
				InputSchema: bedrock.ToolInputSchemaBlock{
					JSON: parameters,
				},
				Name:        name,
				Description: &description,
			},
		})
		if cachePoint := newBedrockDefaultCachePointBlock(t.CacheControl); cachePoint != nil {
			out = append(out, &bedrock.ToolBlock{
				CachePoint: cachePoint,
			})
		}
	}

	return out, nil
}

// Default continue messages for Bedrock
const (
	defaultUserContinueMessage = "Please continue."
)

// transformMessagesToBedrockMessageBlocks converts OpenAI-style messages to Bedrock Converse API format.
// This function corresponds to _bedrock_converse_messages_pt in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
//
// Key behaviors:
// - Roles must alternate between 'user' and 'assistant'
// - Consecutive messages with the same role are merged
// - Tool results are sent as 'user' role messages
// - System messages should be handled separately (not included here)
func transformMessagesToBedrockMessageBlocks(messages []chatMessage, opts *bedrock.TransformMessagesOptions) ([]*bedrock.MessageBlock, error) {
	// Base case: empty messages
	if len(messages) == 0 {
		return nil, util.BadRequest("bedrock requires at least one non-system message")
	}

	// Set default user continue message if not provided
	userContinue := opts.UserContinueMessage
	if userContinue == "" {
		userContinue = defaultUserContinueMessage
	}

	// If first message is assistant, insert user continue message
	if len(messages) > 0 && messages[0].Role == roleAssistant {
		if opts.ModifyParams {
			userMsg := chatMessage{Role: roleUser, Content: userContinue}
			messages = append([]chatMessage{userMsg}, messages...)
		}
	}

	// If last message is assistant, append user continue message
	if len(messages) > 0 && messages[len(messages)-1].Role == roleAssistant {
		if opts.ModifyParams {
			userMsg := chatMessage{Role: roleUser, Content: userContinue}
			messages = append(messages, userMsg)
		}
	}

	var contents []*bedrock.MessageBlock
	msgIdx := 0

	for msgIdx < len(messages) {
		initMsgIdx := msgIdx

		// MERGE CONSECUTIVE USER CONTENT
		var userContentBlocks []*bedrock.ContentBlock
		for msgIdx < len(messages) && messages[msgIdx].Role == roleUser {
			contentBlocks, err := convertUserMessageToContentBlocks(messages[msgIdx])
			if err != nil {
				return nil, fmt.Errorf("failed to convert user message: %w", err)
			}
			userContentBlocks = append(userContentBlocks, contentBlocks...)
			msgIdx++
		}
		if len(userContentBlocks) > 0 {
			if len(contents) > 0 && contents[len(contents)-1].Role == roleUser {
				if opts.ModifyParams {
					// if last message was a 'user' message, then add a dummy assistant message (bedrock requires alternating roles)
					contents = bedrock.AppendDefaultAssistantContinueMessage(contents)
					contents = append(contents, &bedrock.MessageBlock{
						Role:    roleUser,
						Content: userContentBlocks,
					})
				} else {
					// Potential consecutive user/tool blocks. Trying to merge.
					contents[len(contents)-1].Content = append(contents[len(contents)-1].Content, userContentBlocks...)
				}
			} else {
				contents = append(contents, &bedrock.MessageBlock{
					Role:    roleUser,
					Content: userContentBlocks,
				})
			}
		}

		// MERGE CONSECUTIVE TOOL CALL MESSAGES
		var toolContentBlocks []*bedrock.ContentBlock
		for msgIdx < len(messages) && messages[msgIdx].Role == roleTool {
			currentMsg := messages[msgIdx]
			toolResult, err := convertToBedrockToolCallResult(currentMsg)
			if err != nil {
				return nil, fmt.Errorf("failed to convert to bedrock tool call result: %w", err)
			}
			toolContentBlocks = append(toolContentBlocks, toolResult)
			if shouldAddCachePointForToolResult(currentMsg) {
				toolContentBlocks = append(toolContentBlocks, newBedrockCachePointContentBlock(getToolResultCacheControl(currentMsg)))
			}
			msgIdx++
		}
		if len(toolContentBlocks) > 0 {
			// if last message was a 'user' message, then add a blank assistant message (bedrock requires alternating roles)
			if len(contents) > 0 && contents[len(contents)-1].Role == roleUser {
				if opts.ModifyParams {
					// if last message was a 'user' message, then add a dummy assistant message (bedrock requires alternating roles)
					contents = bedrock.AppendDefaultAssistantContinueMessage(contents)
					contents = append(contents, &bedrock.MessageBlock{
						Role:    roleUser,
						Content: toolContentBlocks,
					})
				} else {
					// Potential consecutive user/tool blocks. Trying to merge.
					contents[len(contents)-1].Content = append(contents[len(contents)-1].Content, toolContentBlocks...)
				}
			} else {
				contents = append(contents, &bedrock.MessageBlock{
					Role:    roleUser,
					Content: toolContentBlocks,
				})
			}
		}

		// MERGE CONSECUTIVE ASSISTANT CONTENT
		var assistantContentBlocks []*bedrock.ContentBlock
		for msgIdx < len(messages) && messages[msgIdx].Role == roleAssistant {
			contentBlocks, err := convertAssistantMessageToContentBlocks(messages[msgIdx])
			if err != nil {
				return nil, fmt.Errorf("failed to convert assistant message: %w", err)
			}
			assistantContentBlocks = append(assistantContentBlocks, contentBlocks...)
			msgIdx++
		}
		if len(assistantContentBlocks) > 0 {
			contents = append(contents, &bedrock.MessageBlock{
				Role:    roleAssistant,
				Content: assistantContentBlocks,
			})
		}

		// Prevent infinite loops
		if msgIdx == initMsgIdx {
			return nil, fmt.Errorf("unable to process message at index %d: %+v", msgIdx, messages[msgIdx])
		}
	}

	return contents, nil
}

// convertUserMessageToContentBlocks converts a user message to Bedrock content blocks
func convertUserMessageToContentBlocks(msg chatMessage) ([]*bedrock.ContentBlock, error) {
	var blocks []*bedrock.ContentBlock

	// Handle string content
	if msg.IsStringContent() {
		content := msg.StringContent()
		if content != "" {
			blocks = append(blocks, &bedrock.ContentBlock{Text: &content})
			// Add cache point if cache_control is present at message level
			if msg.CacheControl != nil {
				blocks = append(blocks, newBedrockCachePointContentBlock(msg.CacheControl))
			}
		}
		return blocks, nil
	}

	// Handle array content
	parsedContent := msg.ParseContent()
	for _, part := range parsedContent {
		switch part.Type {
		case contentTypeText:
			text := part.Text
			blocks = append(blocks, &bedrock.ContentBlock{Text: &text})
		case contentTypeImageUrl:
			// Handle image URL - convert to Bedrock image format
			if part.ImageUrl != nil && part.ImageUrl.Url != "" {
				bedrockImageBlock, err := bedrock.ProcessImage(part.ImageUrl.Url, "") // TODO: format
				if err != nil {
					return nil, fmt.Errorf("failed to process image URL: %w", err)
				}
				blocks = append(blocks, bedrockImageBlock)
			}
		case contentTypeFile:
			// Handle file content - convert to Bedrock image/document format
			bedrockContentBlock, err := bedrock.ProcessFileContent(
				part.File.FileData,
				part.File.FileId,
				part.File.Format,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to process file content: %w", err)
			}
			blocks = append(blocks, bedrockContentBlock)
		}

		// Add cache point if cache_control is present
		if part.CacheControl != nil {
			blocks = append(blocks, newBedrockCachePointContentBlock(part.CacheControl))
		}
	}

	return blocks, nil
}

// convertAssistantMessageToContentBlocks converts an assistant message to Bedrock content blocks.
// This corresponds to the assistant message processing in Python's _bedrock_converse_messages_pt:
// litellm/litellm_core_utils/prompt_templates/factory.py
func convertAssistantMessageToContentBlocks(msg chatMessage) ([]*bedrock.ContentBlock, error) {
	var blocks []*bedrock.ContentBlock

	// Handle thinking_blocks if present
	if len(msg.ThinkingBlocks) > 0 {
		// Translate thinking blocks to reasoning content blocks
		reasoningBlocks := bedrock.TranslateThinkingBlocksToReasoningContentBlocks(msg.ThinkingBlocks)
		blocks = append(blocks, reasoningBlocks...)
	}

	if msg.IsStringContent() {
		content := msg.StringContent()
		if content != "" {
			blocks = append(blocks, &bedrock.ContentBlock{Text: &content})
			if msg.CacheControl != nil {
				blocks = append(blocks, newBedrockCachePointContentBlock(msg.CacheControl))
			}
		}
	} else {
		assistantContent := msg.ParseContent()
		for _, element := range assistantContent {
			if element.Type == contentTypeThinking {
				thinkingBlock := map[string]any{
					"type":      element.Type,
					"thinking":  element.Thinking,
					"signature": element.Signature,
				}
				thinkingBlocks := bedrock.TranslateThinkingBlocksToReasoningContentBlocks([]map[string]any{thinkingBlock})
				blocks = append(blocks, thinkingBlocks...)
			} else if element.Type == contentTypeText {
				text := element.Text
				if text != "" {
					blocks = append(blocks, &bedrock.ContentBlock{Text: &text})
				}
			} else if element.Type == contentTypeImageUrl {
				imageURL := element.ImageUrl.Url
				bedrockImageBlock, err := bedrock.ProcessImage(imageURL, "") // TODO provide format
				if err == nil && bedrockImageBlock != nil {
					blocks = append(blocks, bedrockImageBlock)
				} else {
					return nil, err
				}
			}
			if element.CacheControl != nil {
				blocks = append(blocks, newBedrockCachePointContentBlock(element.CacheControl))
			}
		}
	}

	// Handle tool calls
	if len(msg.ToolCalls) > 0 {
		toolBlocks := convertToBedrockToolCallInvoke(msg.ToolCalls)
		blocks = append(blocks, toolBlocks...)
	}

	return blocks, nil
}

// convertToBedrockToolCallInvoke converts OpenAI tool calls to Bedrock format.
// This function corresponds to _convert_to_bedrock_tool_call_invoke in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func convertToBedrockToolCallInvoke(toolCalls []toolCall) []*bedrock.ContentBlock {
	var blocks []*bedrock.ContentBlock

	for _, tc := range toolCalls {
		// Parse arguments JSON to map
		var argsMap map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &argsMap); err != nil {
				// If parsing fails, use empty map
				argsMap = make(map[string]any)
			}
		} else {
			argsMap = make(map[string]any)
		}

		// Normalize tool name for Bedrock
		name := bedrock.NormalizeToolName(tc.Function.Name)

		block := &bedrock.ContentBlock{
			ToolUse: &bedrock.ToolUseBlock{
				Input:     argsMap,
				Name:      name,
				ToolUseID: tc.Id,
			},
		}
		blocks = append(blocks, block)
		if tc.CacheControl != nil {
			blocks = append(blocks, newBedrockCachePointContentBlock(tc.CacheControl))
		}
	}

	return blocks
}

// convertToBedrockToolCallResult converts an OpenAI tool result message to Bedrock format.
// This function corresponds to _convert_to_bedrock_tool_call_result in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func convertToBedrockToolCallResult(msg chatMessage) (*bedrock.ContentBlock, error) {
	var contentBlocks []*bedrock.ToolResultContentBlock

	var contentStr string
	if msg.IsStringContent() {
		contentStr = msg.StringContent()
		contentBlocks = append(contentBlocks, &bedrock.ToolResultContentBlock{
			Text: &contentStr,
		})
	} else {
		// Handle array content
		parsedContent := msg.ParseContent()
		for _, part := range parsedContent {
			switch part.Type {
			case contentTypeText:
				contentBlocks = append(contentBlocks, &bedrock.ToolResultContentBlock{
					Text: &part.Text,
				})
			case contentTypeImageUrl:
				imageBlock, err := bedrock.ProcessImage(part.ImageUrl.Url, "")
				if err != nil {
					return nil, fmt.Errorf("failed to process image URL: %w", err)
				}
				if imageBlock.Image != nil {
					contentBlocks = append(contentBlocks, &bedrock.ToolResultContentBlock{
						Image: imageBlock.Image,
					})
				} else {
					log.Debugf("[bedrock] imageBlock.Image is nil for URL: %s", part.ImageUrl.Url)
				}
			default:

			}
		}
	}

	// Get tool call ID
	toolUseID := msg.ToolCallId
	if toolUseID == "" {
		// Generate a unique ID if not provided
		toolUseID = fmt.Sprintf("tool_%d", time.Now().UnixNano())
	}

	return &bedrock.ContentBlock{
		ToolResult: &bedrock.ToolResultBlock{
			Content:   contentBlocks,
			ToolUseID: toolUseID,
		},
	}, nil
}

func shouldAddCachePointForToolResult(msg chatMessage) bool {
	if msg.CacheControl != nil {
		return true
	}

	for _, part := range msg.ParseContent() {
		if part.CacheControl != nil {
			return true
		}
	}

	return false
}

func getToolResultCacheControl(msg chatMessage) *cacheControl {
	if msg.CacheControl != nil {
		return msg.CacheControl
	}
	for _, part := range msg.ParseContent() {
		if part.CacheControl != nil {
			return part.CacheControl
		}
	}
	return nil
}

// transformSystemMessage extracts system messages from the message list and converts them to SystemContentBlocks.
// This function corresponds to _transform_system_message in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
//
// Returns:
// - remaining messages (without system messages)
// - list of system content blocks
func transformSystemMessage(messages []chatMessage) ([]chatMessage, []*bedrock.SystemContentBlock) {
	var remaining []chatMessage
	var systemBlocks []*bedrock.SystemContentBlock

	for _, msg := range messages {
		if msg.Role == roleSystem || msg.Role == roleDeveloper {
			// Extract system content
			if msg.IsStringContent() {
				content := msg.StringContent()
				if content != "" {
					systemBlocks = append(systemBlocks, &bedrock.SystemContentBlock{
						Text: &content,
					})
					// Add cache point if cache_control is present at message level
					if msg.CacheControl != nil {
						systemBlocks = append(systemBlocks, &bedrock.SystemContentBlock{
							CachePoint: newBedrockDefaultCachePointBlock(msg.CacheControl),
						})
					}
				}
			} else {
				// Handle array content
				parsedContent := msg.ParseContent()
				for _, part := range parsedContent {
					if part.Type == contentTypeText && part.Text != "" {
						text := part.Text
						systemBlocks = append(systemBlocks, &bedrock.SystemContentBlock{
							Text: &text,
						})
						// Add cache point if cache_control is present on content block
						if part.CacheControl != nil {
							systemBlocks = append(systemBlocks, &bedrock.SystemContentBlock{
								CachePoint: newBedrockDefaultCachePointBlock(part.CacheControl),
							})
						}
					}
				}
			}
		} else {
			remaining = append(remaining, msg)
		}
	}

	return remaining, systemBlocks
}

func newBedrockDefaultCachePointBlock(cacheControl *cacheControl) *bedrock.CachePointBlock {
	if cacheControl == nil {
		return nil
	}
	cachePoint := &bedrock.CachePointBlock{Type: "default"}
	if cacheControl.TTL != "" {
		cachePoint.TTL = cacheControl.TTL
	}
	return cachePoint
}

func newBedrockCachePointContentBlock(cacheControl *cacheControl) *bedrock.ContentBlock {
	cachePoint := newBedrockDefaultCachePointBlock(cacheControl)
	if cachePoint == nil {
		return nil
	}
	return &bedrock.ContentBlock{CachePoint: cachePoint}
}

// buildBedrockInferenceConfig converts OpenAI request parameters to Bedrock InferenceConfig.
// This function corresponds to _transform_inference_params in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
func buildBedrockInferenceConfig(request *chatCompletionRequest, extendParams *bedrockExtendedParams) *bedrock.InferenceConfig {
	out := &bedrock.InferenceConfig{}
	hasValue := false

	maxTokens := bedrock.GetMaxTokens(extendParams.MaxTokens, request.MaxCompletionTokens, request.MaxTokens)
	if maxTokens > 0 {
		out.MaxTokens = &maxTokens
		hasValue = true
	}

	// Temperature
	if request.Temperature > 0 {
		out.Temperature = &request.Temperature
		hasValue = true
	}

	// Top P
	topP := extendParams.TopP
	if topP > 0 {
		out.TopP = &topP
		hasValue = true
	}

	// Top K
	topK := extendParams.TopK
	top_K := extendParams.Top_K
	if topK > 0 && top_K > 0 {
		// both provided: use topK, and top_k will put into additionalModelRequestFields
		out.TopK = &topK
		hasValue = true
	}

	// Stop sequences
	if len(extendParams.StopSequences) > 0 {
		out.StopSequences = extendParams.StopSequences
		hasValue = true
	} else {
		if len(request.Stop) > 0 {
			out.StopSequences = request.Stop
			hasValue = true
		}
	}

	if !hasValue {
		return nil
	}

	return out
}

func isBedrockThinkingEnabled(thinking map[string]interface{}, reasoningEffort string) bool {
	return thinking["type"] == "enabled" || (reasoningEffort != "" && reasoningEffort != "none")
}

// createBedrockJsonToolForResponseFormat handles creating a tool call for getting responses in JSON format.
// This corresponds to _create_json_tool_call_for_response_format in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
//
// Parameters:
//   - jsonSchema: The JSON schema the response should be in (optional)
//   - schemaName: Name for the schema (defaults to "json_tool_call" if empty)
//   - description: Optional description for the tool
//
// Returns:
//   - tool: The tool in OpenAI ChatCompletionToolParam format
func createBedrockJsonToolForResponseFormat(jsonSchema map[string]interface{}, schemaName string, description string) tool {
	var inputSchema map[string]interface{}

	if jsonSchema == nil {
		// Anthropic raises a 400 BadRequest error if properties is passed as None
		// see usage with additionalProperties (Example 5) https://github.com/anthropics/anthropic-cookbook/blob/main/tool_use/extracting_structured_json.ipynb
		inputSchema = map[string]interface{}{
			"type":                 "object",
			"additionalProperties": true,
			"properties":           map[string]interface{}{},
		}
	} else {
		inputSchema = jsonSchema
	}

	if schemaName == "" {
		schemaName = "json_tool_call"
	}

	f := function{
		Name:       schemaName,
		Parameters: inputSchema,
	}

	if description != "" {
		f.Description = description
	}

	return tool{
		Type:     "function",
		Function: f,
	}
}

// handleBedrockResponseFormat processes the response_format parameter and converts it to Bedrock tools.
// This corresponds to the response_format handling in map_openai_params in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
//
// Parameters:
//   - request: The chat completion request (will be modified to add JSON tool and tool_choice)
//   - isThinkingEnabled: Whether thinking mode is enabled (affects tool_choice setting)
//
// Returns:
//   - bool: Whether JSON mode is enabled
func handleBedrockResponseFormat(request *chatCompletionRequest, isThinkingEnabled bool) bool {
	if request.ResponseFormat == nil {
		return false
	}

	responseFormat := request.ResponseFormat

	// Check if type is "text" - if so, skip (it's a no-op)
	if formatType, ok := responseFormat["type"].(string); ok && formatType == "text" {
		return false
	}

	// Extract json schema, schema name, and description
	var jsonSchema map[string]interface{}
	var schemaName string
	var description string

	if responseSchema, ok := responseFormat["response_schema"].(map[string]interface{}); ok {
		jsonSchema = responseSchema
		schemaName = "json_tool_call"
	} else if jsonSchemaField, ok := responseFormat["json_schema"].(map[string]interface{}); ok {
		if schema, ok := jsonSchemaField["schema"].(map[string]interface{}); ok {
			jsonSchema = schema
		}
		if name, ok := jsonSchemaField["name"].(string); ok {
			schemaName = name
		}
		if desc, ok := jsonSchemaField["description"].(string); ok {
			description = desc
		}
	} else {
		schemaName = "json_tool_call"
	}

	// Create the JSON tool for response format
	// Follow similar approach to anthropic - translate to a single tool call
	// Reference: https://docs.anthropic.com/en/docs/build-with-claude/tool-use#json-mode
	jsonTool := createBedrockJsonToolForResponseFormat(jsonSchema, schemaName, description)

	// Add the JSON tool to the request (append to the end)
	request.Tools = append(request.Tools, jsonTool)
	log.Debugf("[bedrock] transform response_format to json tool")

	// Set tool_choice if supported and thinking is not enabled
	if !isThinkingEnabled {
		if schemaName == "" {
			schemaName = "json_tool_call"
		}

		if request.ToolChoice == nil {
			request.ToolChoice = map[string]interface{}{
				"type": "tool",
				"function": map[string]interface{}{
					"name": schemaName,
				},
			}
		}
	}

	// Mark JSON mode as enabled
	return true
}

func toBedrockFakeStreamResponse(response *chatCompletionResponse, bedrockUsage *bedrock.OpenAIFormatUsage) ([]byte, error) {
	var responseBuilder strings.Builder

	// first chunk
	firstChunk := response
	firstChunk.Usage = nil
	firstChunkBytes, err := json.Marshal(firstChunk)
	if err != nil {
		return nil, err
	}
	firstEvent := StreamEvent{Data: string(firstChunkBytes)}
	firstStr := firstEvent.ToHttpString()
	responseBuilder.WriteString(firstStr)
	log.Debugf("[bedrock] response body first chunk: %s", firstStr)

	// second chunk: usage
	secondChunk := response
	secondChunk.Choices = nil
	secondChunkBytes, err := json.Marshal(secondChunk)
	if err != nil {
		return nil, err
	}
	secondChunkBytes, err = bedrock.SetBedrockUsageFieldsToResponse(secondChunkBytes, bedrockUsage)
	if err != nil {
		return nil, err
	}
	secondEvent := StreamEvent{Data: string(secondChunkBytes)}
	secondStr := secondEvent.ToHttpString()
	responseBuilder.WriteString(secondStr)
	log.Debugf("[bedrock] response body second chunk (usage): %s", secondStr)

	// last chunk
	lastEvent := StreamEvent{Data: streamEndDataValue}
	responseBuilder.WriteString(lastEvent.ToHttpString())
	return []byte(responseBuilder.String()), nil
}
