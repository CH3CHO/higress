package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// anthropicMessagesRequest is the shared user-facing DTO for Claude-compatible
// /v1/messages requests accepted by the gateway.
type anthropicMessagesRequest struct {
	Model         string   `json:"model"`
	Stream        bool     `json:"stream,omitempty"`
	AnthropicBeta []string `json:"anthropic_beta,omitempty"`
	anthropicMessagesCommonFields
}

type claudeTextGenRequest = anthropicMessagesRequest

type claudeTool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"input_schema,omitempty"`
}

type claudeToolChoice struct {
	Type                   string `json:"type"`
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

type claudeChatMessage struct {
	Role    string                        `json:"role"`
	Content claudeChatMessageContentUnion `json:"content"`
}

type claudeChatMessageContentValue interface {
	claudeChatMessageContentValue()
}

type claudeChatMessageStringValue struct {
	Value string
}

func (claudeChatMessageStringValue) claudeChatMessageContentValue() {}

type claudeChatMessageArrayValue struct {
	Value []claudeChatMessageContent
}

func (claudeChatMessageArrayValue) claudeChatMessageContentValue() {}

// claudeChatMessageContentUnion represents the content field that can be either
// a plain string or an array of structured content blocks.
type claudeChatMessageContentUnion struct {
	value claudeChatMessageContentValue
}

type claudeChatMessageContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	Url       string `json:"url,omitempty"`
	FileId    string `json:"file_id,omitempty"`
}

type claudeChatMessageContent struct {
	Type         string                          `json:"type"`
	Text         string                          `json:"text,omitempty"`
	Source       *claudeChatMessageContentSource `json:"source,omitempty"`
	CacheControl map[string]interface{}          `json:"cache_control,omitempty"`
	Id           string                          `json:"id,omitempty"`
	Name         string                          `json:"name,omitempty"`
	Input        map[string]interface{}          `json:"input,omitempty"`
	ToolUseId    string                          `json:"tool_use_id,omitempty"`
	Content      string                          `json:"content,omitempty"`
}

func (ccw *claudeChatMessageContentUnion) UnmarshalJSON(data []byte) error {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		ccw.value = claudeChatMessageStringValue{Value: stringValue}
		return nil
	}

	var arrayValue []claudeChatMessageContent
	if err := json.Unmarshal(data, &arrayValue); err == nil {
		ccw.value = claudeChatMessageArrayValue{Value: arrayValue}
		return nil
	}

	return fmt.Errorf("content field must be either a string or an array of content blocks")
}

func (ccw *claudeChatMessageContentUnion) MarshalJSON() ([]byte, error) {
	if ccw == nil || ccw.value == nil {
		return json.Marshal(nil)
	}
	switch value := ccw.value.(type) {
	case claudeChatMessageStringValue:
		return json.Marshal(value.Value)
	case claudeChatMessageArrayValue:
		return json.Marshal(value.Value)
	default:
		return nil, fmt.Errorf("unsupported content value type %T", value)
	}
}

func (ccw *claudeChatMessageContentUnion) IsString() bool {
	if ccw == nil {
		return false
	}
	_, ok := ccw.value.(claudeChatMessageStringValue)
	return ok
}

func (ccw *claudeChatMessageContentUnion) GetStringValue() string {
	if ccw == nil {
		return ""
	}
	if value, ok := ccw.value.(claudeChatMessageStringValue); ok {
		return value.Value
	}
	return ""
}

func (ccw *claudeChatMessageContentUnion) GetArrayValue() []claudeChatMessageContent {
	if ccw == nil {
		return nil
	}
	if value, ok := ccw.value.(claudeChatMessageArrayValue); ok {
		return value.Value
	}
	return nil
}

func newStringContent(content string) claudeChatMessageContentUnion {
	return claudeChatMessageContentUnion{
		value: claudeChatMessageStringValue{Value: content},
	}
}

func newArrayContent(content []claudeChatMessageContent) claudeChatMessageContentUnion {
	return claudeChatMessageContentUnion{
		value: claudeChatMessageArrayValue{Value: content},
	}
}

// claudeSystemPrompt represents the system field which can be either a string or an array of text blocks
type claudeSystemPrompt struct {
	StringValue string
	ArrayValue  []claudeTextGenContent
	IsArray     bool
}

func (csp *claudeSystemPrompt) UnmarshalJSON(data []byte) error {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		csp.StringValue = stringValue
		csp.IsArray = false
		return nil
	}

	var arrayValue []claudeTextGenContent
	if err := json.Unmarshal(data, &arrayValue); err == nil {
		csp.ArrayValue = arrayValue
		csp.IsArray = true
		return nil
	}

	return fmt.Errorf("system field must be either a string or an array of text blocks")
}

func (csp *claudeSystemPrompt) MarshalJSON() ([]byte, error) {
	if csp.IsArray {
		return json.Marshal(csp.ArrayValue)
	}
	return json.Marshal(csp.StringValue)
}

func (csp *claudeSystemPrompt) String() string {
	if csp.IsArray {
		var parts []string
		for _, block := range csp.ArrayValue {
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return csp.StringValue
}

type claudeThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type anthropicMessagesCommonFields struct {
	Messages      []claudeChatMessage   `json:"messages"`
	System        claudeSystemPrompt    `json:"system,omitempty"`
	MaxTokens     int                   `json:"max_tokens,omitempty"`
	StopSequences []string              `json:"stop_sequences,omitempty"`
	Temperature   float64               `json:"temperature,omitempty"`
	TopP          float64               `json:"top_p,omitempty"`
	TopK          int                   `json:"top_k,omitempty"`
	ToolChoice    *claudeToolChoice     `json:"tool_choice,omitempty"`
	Tools         []claudeTool          `json:"tools,omitempty"`
	ServiceTier   string                `json:"service_tier,omitempty"`
	Thinking      *claudeThinkingConfig `json:"thinking,omitempty"`
}
