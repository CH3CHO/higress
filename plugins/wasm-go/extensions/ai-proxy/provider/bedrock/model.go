package bedrock

// Bedrock type definitions
// These types correspond to the classes in Python's litellm:
// litellm/types/llms/bedrock.py

// Tool-related types

// SourceBlock represents base64 encoded content source
type SourceBlock struct {
	Bytes *string `json:"bytes,omitempty"`
}

// ImageBlock represents an image in Bedrock format
type ImageBlock struct {
	Format string      `json:"format"`
	Source SourceBlock `json:"source"`
}

// DocumentBlock represents a document in Bedrock format
type DocumentBlock struct {
	Format string      `json:"format"`
	Name   string      `json:"name"`
	Source SourceBlock `json:"source"`
}

// ToolResultContentBlock represents content in a tool result
type ToolResultContentBlock struct {
	Image    *ImageBlock    `json:"image,omitempty"`
	Document *DocumentBlock `json:"document,omitempty"`
	JSON     map[string]any `json:"json,omitempty"`
	Text     *string        `json:"text,omitempty"`
}

// ToolResultBlock represents a tool execution result
type ToolResultBlock struct {
	Content   []*ToolResultContentBlock `json:"content"`
	ToolUseID string                    `json:"toolUseId"`
	Status    *string                   `json:"status,omitempty"` // "success" or "error"
}

// ToolUseBlock represents a tool invocation
type ToolUseBlock struct {
	Input     map[string]any `json:"input"`
	Name      string         `json:"name"`
	ToolUseID string         `json:"toolUseId"`
}

// ToolInputSchemaBlock represents the schema for tool input parameters
type ToolInputSchemaBlock struct {
	JSON map[string]any `json:"json,omitempty"`
}

// ToolSpecBlock represents the specification of a tool
type ToolSpecBlock struct {
	InputSchema ToolInputSchemaBlock `json:"inputSchema"`
	Name        string               `json:"name"`
	Description *string              `json:"description,omitempty"`
}

// ToolBlock wraps a tool specification
type ToolBlock struct {
	ToolSpec *ToolSpecBlock `json:"toolSpec,omitempty"`
}

// SpecificToolChoiceBlock specifies a particular tool to use
type SpecificToolChoiceBlock struct {
	Name string `json:"name"`
}

// ToolChoiceValuesBlock represents different tool choice options
type ToolChoiceValuesBlock struct {
	Any  *map[string]any          `json:"any,omitempty"`
	Auto *map[string]any          `json:"auto,omitempty"`
	Tool *SpecificToolChoiceBlock `json:"tool,omitempty"`
}

// ToolConfigBlock represents the tool configuration for a request
type ToolConfigBlock struct {
	Tools      []*ToolBlock `json:"tools"`
	ToolChoice any          `json:"toolChoice,omitempty"` // string or ToolChoiceValuesBlock
}

// ToolBlockDeltaEvent represents a streaming update for tool input
type ToolBlockDeltaEvent struct {
	Input string `json:"input"`
}

// ToolUseBlockStartEvent represents the start of a tool use in streaming
type ToolUseBlockStartEvent struct {
	Name      string `json:"name"`
	ToolUseID string `json:"toolUseId"`
}

// ContentBlockDeltaEvent represents a content block delta in streaming
// This can contain text, toolUse, or reasoningContent updates
type ContentBlockDeltaEvent struct {
	Text             *string                             `json:"text,omitempty"`
	ToolUse          *ToolBlockDeltaEvent                `json:"toolUse,omitempty"`
	ReasoningContent *ConverseReasoningContentBlockDelta `json:"reasoningContent,omitempty"`
}

// ConverseReasoningContentBlockDelta represents a delta for reasoning content in streaming
type ConverseReasoningContentBlockDelta struct {
	Text            string `json:"text,omitempty"`
	Signature       string `json:"signature,omitempty"`
	RedactedContent string `json:"redactedContent,omitempty"`
}

// ContentBlockStartEvent represents the start of a content block in streaming
// This corresponds to ContentBlockStartEvent in Python's litellm
type ContentBlockStartEvent struct {
	ToolUse          *ToolUseBlockStartEvent             `json:"toolUse,omitempty"`
	ReasoningContent *ConverseReasoningContentBlockDelta `json:"reasoningContent,omitempty"`
}

// Reasoning-related types

// ConverseReasoningTextBlock represents the reasoning text with optional signature
type ConverseReasoningTextBlock struct {
	Text      string  `json:"text"`
	Signature *string `json:"signature,omitempty"`
}

// ConverseReasoningContentBlock represents a reasoning content block
type ConverseReasoningContentBlock struct {
	ReasoningText   *ConverseReasoningTextBlock `json:"reasoningText,omitempty"`
	RedactedContent *string                     `json:"redactedContent,omitempty"`
}

// Usage-related types

// TokenUsageBlock represents token usage information from Bedrock
// This corresponds to ConverseTokenUsageBlock in Python's litellm:
// litellm/types/llms/bedrock.py
type TokenUsageBlock struct {
	InputTokens               int `json:"inputTokens"`
	OutputTokens              int `json:"outputTokens"`
	TotalTokens               int `json:"totalTokens"`
	CacheReadInputTokenCount  int `json:"cacheReadInputTokenCount,omitempty"`
	CacheReadInputTokens      int `json:"cacheReadInputTokens,omitempty"`
	CacheWriteInputTokenCount int `json:"cacheWriteInputTokenCount,omitempty"`
	CacheWriteInputTokens     int `json:"cacheWriteInputTokens,omitempty"`
}

type PromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

type OpenAIFormatUsage struct {
	PromptTokens             int                  `json:"prompt_tokens,omitempty"`
	CompletionTokens         int                  `json:"completion_tokens,omitempty"`
	TotalTokens              int                  `json:"total_tokens,omitempty"`
	PromptTokensDetails      *PromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	CacheCreationInputTokens int                  `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int                  `json:"cache_read_input_tokens,omitempty"`
}

// Response-related types for Bedrock Converse API

// ConverseResponseBlock represents the complete response from Bedrock Converse API
// This corresponds to ConverseResponseBlock in Python's litellm:
// litellm/types/llms/bedrock.py
type ConverseResponseBlock struct {
	Output     *OutputBlock     `json:"output,omitempty"`
	StopReason string           `json:"stopReason,omitempty"`
	Usage      *TokenUsageBlock `json:"usage,omitempty"`
	Metrics    *MetricsBlock    `json:"metrics,omitempty"`
	Trace      interface{}      `json:"trace,omitempty"` // Optional trace from Bedrock guardrails
}

// OutputBlock wraps the message in the response
type OutputBlock struct {
	Message *MessageBlock `json:"message,omitempty"`
}

// MessageBlock represents a message in the Bedrock request/response
// This corresponds to MessageBlock in Python's litellm:
// litellm/types/llms/bedrock.py
type MessageBlock struct {
	Role    string          `json:"role,omitempty"`
	Content []*ContentBlock `json:"content,omitempty"`
}

// ContentBlock represents a content block in a message
// Content can contain text, image, document, toolUse, toolResult, reasoningContent, or cachePoint
// This corresponds to ContentBlock in Python's litellm:
// litellm/types/llms/bedrock.py
type ContentBlock struct {
	Text             *string                        `json:"text,omitempty"`
	Image            *ImageBlock                    `json:"image,omitempty"`
	Document         *DocumentBlock                 `json:"document,omitempty"`
	ToolUse          *ToolUseBlock                  `json:"toolUse,omitempty"`
	ToolResult       *ToolResultBlock               `json:"toolResult,omitempty"`
	ReasoningContent *ConverseReasoningContentBlock `json:"reasoningContent,omitempty"`
	CachePoint       *CachePointBlock               `json:"cachePoint,omitempty"` // For prompt caching
}

// MetricsBlock represents metrics information from Bedrock response
type MetricsBlock struct {
	LatencyMs int `json:"latencyMs,omitempty"`
}

// CachePointBlock represents a cache point for prompt caching
// This is used in Bedrock's prompt caching feature
type CachePointBlock struct {
	Type string `json:"type"` // "default"
}

// Request-related types for Bedrock Converse API

// SystemContentBlock represents a system message content block
// This corresponds to SystemContentBlock in Python's litellm:
// litellm/types/llms/bedrock.py
type SystemContentBlock struct {
	Text       *string          `json:"text,omitempty"`
	CachePoint *CachePointBlock `json:"cachePoint,omitempty"`
}

// InferenceConfig represents the inference configuration for Bedrock
// This corresponds to InferenceConfig in Python's litellm:
// litellm/types/llms/bedrock.py
type InferenceConfig struct {
	MaxTokens     *int     `json:"maxTokens,omitempty"`
	StopSequences []string `json:"stopSequences,omitempty"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"topP,omitempty"`
	TopK          *float64 `json:"topK,omitempty"`
}

// GuardrailConfigBlock represents guardrail configuration
type GuardrailConfigBlock struct {
	GuardrailIdentifier string `json:"guardrailIdentifier"`
	GuardrailVersion    string `json:"guardrailVersion"`
	Trace               string `json:"trace,omitempty"` // "enabled" or "disabled"
}

// PerformanceConfigBlock represents performance configuration
type PerformanceConfigBlock struct {
	Latency string `json:"latency,omitempty"` // "standard" or "optimized"
}

// ConverseRequest represents the full request body for Bedrock Converse API
// This corresponds to RequestObject in Python's litellm:
// litellm/types/llms/bedrock.py
type ConverseRequest struct {
	CommonRequestObject
	Messages []*MessageBlock `json:"messages"`
}

// CommonRequestObject represents common request object across sync + async flows
// This corresponds to CommonRequestObject in Python's litellm:
// litellm/litellm_core_utils/prompt_templates/factory.py
type CommonRequestObject struct {
	AdditionalModelRequestFields      map[string]interface{}  `json:"additionalModelRequestFields,omitempty"`
	AdditionalModelResponseFieldPaths []string                `json:"additionalModelResponseFieldPaths,omitempty"`
	InferenceConfig                   *InferenceConfig        `json:"inferenceConfig,omitempty"`
	System                            []*SystemContentBlock   `json:"system,omitempty"`
	ToolConfig                        *ToolConfigBlock        `json:"toolConfig,omitempty"`
	GuardrailConfig                   *GuardrailConfigBlock   `json:"guardrailConfig,omitempty"`
	PerformanceConfig                 *PerformanceConfigBlock `json:"performanceConfig,omitempty"`
}
