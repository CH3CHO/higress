package bedrock

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/google/uuid"
	"github.com/tidwall/sjson"
)

// Default reasoning effort thinking budget constants
// These correspond to the constants in Python's litellm/constants.py
const (
	DefaultReasoningEffortLowThinkingBudget    = 1024
	DefaultReasoningEffortMediumThinkingBudget = 2048
	DefaultReasoningEffortHighThinkingBudget   = 4096
)

// MapToolChoiceValues converts OpenAI-style tool_choice to Bedrock ToolChoiceValuesBlock.
// Returns nil if dropParams is true and tool_choice is "none".
// Returns error if tool_choice is unsupported.
//
// This function corresponds to map_tool_choice_values in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
func MapToolChoiceValues(toolChoice interface{}, dropParams bool) (*ToolChoiceValuesBlock, error) {
	if toolChoice == nil {
		return nil, nil
	}

	// Handle string values
	if strChoice, ok := toolChoice.(string); ok {
		switch strChoice {
		case "none":
			if dropParams {
				return nil, nil
			}
			return nil, util.BadRequest("bedrock doesn't support tool_choice='none'. To drop it from the call, set dropParams=true")

		case "required":
			return &ToolChoiceValuesBlock{
				Any: &map[string]any{},
			}, nil

		case "auto":
			return &ToolChoiceValuesBlock{
				Auto: &map[string]any{},
			}, nil

		default:
			return nil, util.BadRequest(fmt.Sprintf("bedrock doesn't support tool_choice='%s'. Supported values: 'auto', 'required', or JSON object", strChoice))
		}
	}

	// Handle dict/map values (specific tool choice)
	// Only supported for anthropic + mistral models
	// Reference: https://docs.aws.amazon.com/bedrock/latest/APIReference/API_runtime_ToolChoice.html
	if mapChoice, ok := toolChoice.(map[string]interface{}); ok {
		functionMap, ok := mapChoice["function"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid tool_choice format: missing 'function' field")
		}

		name, ok := functionMap["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("invalid tool_choice format: missing or invalid 'name' field")
		}

		return &ToolChoiceValuesBlock{
			Tool: &SpecificToolChoiceBlock{
				Name: name,
			},
		}, nil
	}

	return nil, util.BadRequest(fmt.Sprintf("bedrock doesn't support tool_choice of type %T. Supported values: 'auto', 'required', or JSON object", toolChoice))
}

var (
	reasoningEffortToThinkingConfig = map[string]map[string]any{
		"low": {
			"type":          "enabled",
			"budget_tokens": DefaultReasoningEffortLowThinkingBudget,
		},
		"medium": {
			"type":          "enabled",
			"budget_tokens": DefaultReasoningEffortMediumThinkingBudget,
		},
		"high": {
			"type":          "enabled",
			"budget_tokens": DefaultReasoningEffortHighThinkingBudget,
		},
	}
)

// toolNameCache is a specialized LRU cache for mapping normalized tool names to original names
var toolNameCache = util.NewLRUCache("bedrock-tool-name", 1024)

// NormalizeToolName replaces any invalid characters in the input tool name with underscores
// and ensures the resulting string is a valid identifier for Bedrock tools.
// Bedrock tool names only support alphanumeric characters and underscores.
//
// This function corresponds to make_valid_bedrock_tool_name in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func NormalizeToolName(toolName string) string {
	// If the string is empty, return as is
	if len(toolName) == 0 {
		return toolName
	}

	name := toolName

	// If it doesn't start with a letter (ASCII only), prepend 'a'
	firstChar := rune(name[0])
	if !((firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z')) {
		name = "a" + name
	}

	// Replace any invalid characters with underscores
	// Only ASCII letters, digits and underscores are valid
	var builder strings.Builder
	builder.Grow(len(name))

	for _, char := range name {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' {
			builder.WriteRune(char)
		} else {
			builder.WriteRune('_')
		}
	}

	result := builder.String()

	// Store mapping if the name was changed
	if toolName != result {
		toolNameCache.Set(result, toolName)
	}

	return result
}

// GetOriginalToolName retrieves the original tool name from the normalized name.
// If the normalized name was not transformed, it returns the input as is.
//
// This function corresponds to get_bedrock_tool_name in Python's litellm:
// litellm/llms/bedrock/common_utils.py
func GetOriginalToolName(name string) string {
	originalNameVal, exists := toolNameCache.Get(name)
	if exists {
		if originalName, ok := originalNameVal.(string); ok {
			return originalName
		}
	}
	return name
}

// MapReasoningEffort converts OpenAI-style reasoning_effort to Anthropic-style thinking parameters.
// Returns nil if reasoningEffort is nil or empty.
// Returns error if reasoningEffort value is unsupported.
//
// This function corresponds to AnthropicConfig._map_reasoning_effort in Python's litellm:
// litellm/llms/anthropic/chat/transformation.py
func MapReasoningEffort(reasoningEffort string) (map[string]interface{}, error) {
	if reasoningEffort == "" {
		return nil, nil
	}

	if thinkingConfig, ok := reasoningEffortToThinkingConfig[reasoningEffort]; ok {
		return thinkingConfig, nil
	}

	return nil, util.BadRequest("unmapped reasoning effort: " + reasoningEffort)
}

func ParseThinkingBudgetTokens(thinking map[string]any) int {
	if budgetTokensVal, ok := thinking["budget_tokens"]; ok {
		var budgetTokens int
		switch v := budgetTokensVal.(type) {
		case int:
			budgetTokens = v
		case float64:
			budgetTokens = int(v)
		case int64:
			budgetTokens = int(v)
		}
		return budgetTokens
	}
	return 0
}

// TransformReasoningContent extracts the reasoning text from reasoning content blocks.
// It concatenates all reasoning text from blocks that contain reasoningText.
// This ensures DeepSeek reasoning content compatible output.
//
// This function corresponds to _transform_reasoning_content in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
func TransformReasoningContent(reasoningContentBlocks []*ConverseReasoningContentBlock) string {
	var builder strings.Builder

	for _, block := range reasoningContentBlocks {
		if block != nil && block.ReasoningText != nil {
			builder.WriteString(block.ReasoningText.Text)
		}
	}

	return builder.String()
}

// TransformThinkingBlocks returns a consistent format for thinking blocks between Anthropic and Bedrock.
// It converts ConverseReasoningContentBlock to either ThinkingBlock or RedactedThinkingBlock.
//
// This function corresponds to _transform_thinking_blocks in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
func TransformThinkingBlocks(thinkingBlocks []*ConverseReasoningContentBlock) []map[string]any {
	var result []map[string]any

	for _, block := range thinkingBlocks {
		if block == nil {
			continue
		}

		if block.ReasoningText != nil {
			thinkingBlock := map[string]any{
				"type": "thinking",
			}

			if block.ReasoningText.Text != "" {
				thinkingBlock["thinking"] = block.ReasoningText.Text
			}

			if block.ReasoningText.Signature != nil {
				thinkingBlock["signature"] = *block.ReasoningText.Signature
			}

			result = append(result, thinkingBlock)
		} else if block.RedactedContent != nil {
			redactedBlock := map[string]any{
				"type": "redacted_thinking",
				"data": *block.RedactedContent,
			}

			result = append(result, redactedBlock)
		}
	}

	return result
}

// TranslateThinkingBlocksToReasoningContentBlocks converts OpenAI thinking blocks to Bedrock reasoning content blocks.
// This function corresponds to BedrockConverseMessagesProcessor.translate_thinking_blocks_to_reasoning_content_blocks in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func TranslateThinkingBlocksToReasoningContentBlocks(thinkingBlocks []map[string]any) []*ContentBlock {
	var out []*ContentBlock
	for _, thinkingBlock := range thinkingBlocks {
		blockType, _ := thinkingBlock["type"].(string)

		if blockType == "thinking" {
			var reasoningText string
			var reasoningSignature *string

			if thinking, ok := thinkingBlock["thinking"].(string); ok {
				reasoningText = thinking
			}
			if signature, ok := thinkingBlock["signature"].(string); ok {
				reasoningSignature = &signature
			}

			out = append(out, &ContentBlock{
				ReasoningContent: &ConverseReasoningContentBlock{
					ReasoningText: &ConverseReasoningTextBlock{
						Text:      reasoningText,
						Signature: reasoningSignature,
					},
				},
			})
		} else if blockType == "redacted_thinking" {
			if data, ok := thinkingBlock["data"].(string); ok {
				out = append(out, &ContentBlock{
					ReasoningContent: &ConverseReasoningContentBlock{
						RedactedContent: &data,
					},
				})
			}
		}
	}
	return out
}

// TranslateConverseReasoningContentBlockDelta translates Bedrock reasoning content delta to OpenAI thinking blocks.
// This function is used in streaming responses to convert reasoning content from Bedrock format to OpenAI format.
// This corresponds to translate_thinking_blocks in Python's litellm:
// litellm/llms/bedrock/chat/invoke_handler.py
func TranslateConverseReasoningContentBlockDelta(reasoningContent *ConverseReasoningContentBlockDelta) []map[string]any {
	var out []map[string]any
	if reasoningContent.Text != "" {
		thinkingBlock := map[string]any{
			"type":     "thinking",
			"thinking": reasoningContent.Text,
		}
		out = append(out, thinkingBlock)
	} else if reasoningContent.Signature != "" {
		thinkingBlock := map[string]any{
			"type":      "thinking",
			"thinking":  "", // consistent with anthropic response
			"signature": reasoningContent.Signature,
		}
		out = append(out, thinkingBlock)
	} else if reasoningContent.RedactedContent != "" {
		redactedBlock := map[string]any{
			"type": "redacted_thinking",
			"data": reasoningContent.RedactedContent,
		}
		out = append(out, redactedBlock)
	}
	if len(out) > 0 {
		return out
	}
	return nil
}

var (
	supportedDocumentTypes = []string{"application", "text"}
)

// isDocumentMimeType checks if a MIME type is a document based on prefix.
// This corresponds to the document type check in Python's litellm BedrockImageProcessor.
func isDocumentMimeType(mimeType string) bool {
	for _, docType := range supportedDocumentTypes {
		if strings.HasPrefix(mimeType, docType) {
			return true
		}
	}
	return false
}

// validateFormat validates image format and mime type for both images and documents.
//
// This function corresponds to BedrockImageProcessor._validate_format in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func validateFormat(mimeType string, imageFormat string) (string, error) {
	supportedImageFormats := []string{"png", "jpeg", "gif", "webp"}
	supportedDocFormats := []string{"pdf", "csv", "doc", "docx", "xls", "xlsx", "html", "txt", "md"}

	if isDocumentMimeType(mimeType) {
		ext, err := util.GetFileExtensionFromMimeType(mimeType)
		if err != nil {
			return "", fmt.Errorf("no supported extensions for MIME type: %s. Supported formats: %v", mimeType, supportedDocFormats)
		}

		// Validate extension is in supported formats
		isSupported := false
		for _, supportedFormat := range supportedDocFormats {
			if ext == supportedFormat {
				isSupported = true
				break
			}
		}

		if !isSupported {
			return "", fmt.Errorf("no supported extensions for MIME type: %s. Supported formats: %v", mimeType, supportedDocFormats)
		}

		// Use the extension from mime type instead of provided imageFormat
		return ext, nil
	}

	// For images, validate against supported formats
	isSupported := false
	for _, format := range supportedImageFormats {
		if imageFormat == format {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return "", fmt.Errorf("unsupported image format: %s. Supported formats: %v", imageFormat, supportedImageFormats)
	}

	return imageFormat, nil
}

// createBedrockBlock creates appropriate Bedrock content block based on mime type.
//
// This function corresponds to BedrockImageProcessor._create_bedrock_block in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
func createBedrockBlock(imageBytes string, mimeType string, imageFormat string) *ContentBlock {
	blob := SourceBlock{Bytes: &imageBytes}

	if isDocumentMimeType(mimeType) {
		return &ContentBlock{
			Document: &DocumentBlock{
				Format: imageFormat,
				Name:   fmt.Sprintf("DocumentPDFmessages_%s", uuid.New().String()),
				Source: blob,
			},
		}
	}

	// Default to image
	return &ContentBlock{
		Image: &ImageBlock{
			Format: imageFormat,
			Source: blob,
		},
	}
}

// ProcessImage is the image processing function.
//
// This function corresponds to BedrockImageProcessor.process_image_sync in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
//
// Supported URL formats:
//   - Base64 data URLs: data:[<media-type>];base64,<data>
//   - HTTP/HTTPS URLs: Not yet supported
//
// Returns error if the URL format is unsupported or parsing fails.
func ProcessImage(imageURL string, format string) (*ContentBlock, error) {
	var imgBytes string
	var mimeType string
	var imageFormat string

	// Check if it's a base64 data URL (matching Python's: if "base64" in image_url)
	if strings.Contains(imageURL, "base64") {
		info := util.ParseBase64Image(imageURL)
		if info == nil {
			return nil, fmt.Errorf("failed to parse base64 image data URL: %s", imageURL)
		}
		imgBytes = info.Data
		mimeType = info.MimeType
		imageFormat = info.Format
	} else if strings.Contains(imageURL, "http://") || strings.Contains(imageURL, "https://") {
		return nil, fmt.Errorf("HTTP/HTTPS image URLs are not yet supported")
	} else {
		// Unsupported format
		return nil, fmt.Errorf("unsupported image type. Expected either image url or base64 encoded string")
	}

	// Override with user-defined format if provided
	if format != "" {
		parts := strings.Split(imageURL, "/")
		if len(parts) > 1 {
			mimeType = format
			// Extract format from mime type (e.g., "image/jpeg" -> "jpeg")
			imageFormat = parts[1]
		}
	}

	// Validate format
	validatedFormat, err := validateFormat(mimeType, imageFormat)
	if err != nil {
		return nil, err
	}
	imageFormat = validatedFormat

	// Create and return the Bedrock block
	return createBedrockBlock(imgBytes, mimeType, imageFormat), nil
}

// ProcessFileContent converts file content to Bedrock ContentBlock format.
//
// This function corresponds to BedrockImageProcessor._process_file_message in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
//
// Parameters:
//   - fileData: base64 encoded file data or data URL
//   - fileId: file ID (used if fileData is empty)
//   - format: optional format hint (e.g., "pdf", "jpeg")
//
// Returns error if both fileData and fileId are empty, or if parsing fails.
func ProcessFileContent(fileData string, fileId string, format string) (*ContentBlock, error) {
	// Check if both fileData and fileId are empty (matching litellm's behavior)
	if fileData == "" && fileId == "" {
		return nil, util.BadRequest("file_data and file_id cannot both be empty")
	}

	data := fileId
	if data == "" {
		data = fileData
	}
	return ProcessImage(data, format)
}

// DeepCopyMap creates a deep copy of a map using JSON marshal/unmarshal
func DeepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}

	data, err := json.Marshal(src)
	if err != nil {
		return make(map[string]interface{})
	}

	var dst map[string]interface{}
	if err := json.Unmarshal(data, &dst); err != nil {
		return make(map[string]interface{})
	}

	return dst
}

// TransformUsageToOpenAIFormat converts Bedrock token usage to OpenAI-compatible usage format.
// This function corresponds to _transform_usage in Python's litellm:
// litellm/llms/bedrock/chat/converse_transformation.py
func TransformUsageToOpenAIFormat(bedrockUsage *TokenUsageBlock) *OpenAIFormatUsage {
	if bedrockUsage == nil {
		return nil
	}

	inputTokens := bedrockUsage.InputTokens
	outputTokens := bedrockUsage.OutputTokens
	totalTokens := bedrockUsage.TotalTokens
	cacheCreationInputTokens := 0
	cacheReadInputTokens := 0

	// Add cache read tokens to input tokens
	if bedrockUsage.CacheReadInputTokens > 0 {
		cacheReadInputTokens = bedrockUsage.CacheReadInputTokens
		inputTokens += cacheReadInputTokens
	}
	if bedrockUsage.CacheWriteInputTokens > 0 {
		// Do not increment prompt_tokens with cacheWriteInputTokens
		cacheCreationInputTokens = bedrockUsage.CacheWriteInputTokens
	}

	out := &OpenAIFormatUsage{
		PromptTokens:     inputTokens,
		CompletionTokens: outputTokens,
		TotalTokens:      totalTokens,
	}
	if cacheReadInputTokens > 0 {
		out.PromptTokensDetails = &PromptTokensDetails{
			CachedTokens: cacheReadInputTokens,
		}
	}

	if cacheCreationInputTokens > 0 || cacheReadInputTokens > 0 {
		out.CacheCreationInputTokens = cacheCreationInputTokens
		out.CacheReadInputTokens = cacheReadInputTokens
	}
	return out
}

func SetBedrockUsageFieldsToResponse(body []byte, bedrockUsage *OpenAIFormatUsage) ([]byte, error) {
	if bedrockUsage != nil {
		transformedBody, err := sjson.SetBytes(body, "usage", bedrockUsage)
		if err != nil {
			return nil, err
		}
		body = transformedBody
	}
	return body, nil
}

// AppendDefaultAssistantContinueMessage add dummy message between user/tool result blocks.
// Conversation blocks and tool result blocks cannot be provided in the same turn. Issue: https://github.com/BerriAI/litellm/issues/6053
func AppendDefaultAssistantContinueMessage(messages []*MessageBlock) []*MessageBlock {
	text := "Please continue."
	messages = append(messages, &MessageBlock{
		Role: "assistant",
		Content: []*ContentBlock{
			{Text: &text},
		},
	})
	return messages
}

// GetMaxTokens return max tokens with precedence: maxTokens > max_completion_tokens > max_tokens
func GetMaxTokens(maxTokensCamelCase, maxCompletionTokensSnakeCase, maxTokensSnakeCase int) int {
	if maxTokensCamelCase > 0 {
		return maxTokensCamelCase
	}
	if maxCompletionTokensSnakeCase > 0 {
		return maxCompletionTokensSnakeCase
	}
	return maxTokensSnakeCase
}
