package provider

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/bedrock"
	"github.com/higress-group/wasm-go/pkg/iface"
	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

type mockBedrockHTTPContext struct {
	values map[string]interface{}
}

func newMockBedrockHTTPContext() *mockBedrockHTTPContext {
	return &mockBedrockHTTPContext{
		values: make(map[string]interface{}),
	}
}

func (m *mockBedrockHTTPContext) Scheme() string { return "" }
func (m *mockBedrockHTTPContext) Host() string   { return "" }
func (m *mockBedrockHTTPContext) Path() string   { return "" }
func (m *mockBedrockHTTPContext) Method() string { return "" }
func (m *mockBedrockHTTPContext) SetContext(key string, value interface{}) {
	m.values[key] = value
}
func (m *mockBedrockHTTPContext) GetContext(key string) interface{} {
	return m.values[key]
}
func (m *mockBedrockHTTPContext) GetBoolContext(key string, defaultValue bool) bool {
	if v, ok := m.values[key].(bool); ok {
		return v
	}
	return defaultValue
}
func (m *mockBedrockHTTPContext) GetIntContext(key string, defaultValue int) int {
	if v, ok := m.values[key].(int); ok {
		return v
	}
	return defaultValue
}
func (m *mockBedrockHTTPContext) GetStringContext(key, defaultValue string) string {
	if v, ok := m.values[key].(string); ok {
		return v
	}
	return defaultValue
}
func (m *mockBedrockHTTPContext) GetByteSliceContext(key string, defaultValue []byte) []byte {
	if v, ok := m.values[key].([]byte); ok {
		return v
	}
	return defaultValue
}
func (m *mockBedrockHTTPContext) GetGlobalConfig(string) interface{} { return nil }
func (m *mockBedrockHTTPContext) GetBoolGlobalConfig(string, bool) bool {
	return false
}
func (m *mockBedrockHTTPContext) GetIntGlobalConfig(string, int) int {
	return 0
}
func (m *mockBedrockHTTPContext) GetStringGlobalConfig(_ string, defaultValue string) string {
	return defaultValue
}
func (m *mockBedrockHTTPContext) GetUserAttribute(string) interface{}        { return nil }
func (m *mockBedrockHTTPContext) SetUserAttribute(string, interface{})       {}
func (m *mockBedrockHTTPContext) SetUserAttributeMap(map[string]interface{}) {}
func (m *mockBedrockHTTPContext) GetUserAttributeMap() map[string]interface{} {
	return nil
}
func (m *mockBedrockHTTPContext) WriteUserAttributeToLog() error              { return nil }
func (m *mockBedrockHTTPContext) WriteUserAttributeToLogWithKey(string) error { return nil }
func (m *mockBedrockHTTPContext) WriteUserAttributeToTrace() error            { return nil }
func (m *mockBedrockHTTPContext) DontReadRequestBody()                        {}
func (m *mockBedrockHTTPContext) DontReadResponseBody()                       {}
func (m *mockBedrockHTTPContext) BufferRequestBody()                          {}
func (m *mockBedrockHTTPContext) BufferResponseBody()                         {}
func (m *mockBedrockHTTPContext) NeedPauseStreamingResponse()                 {}
func (m *mockBedrockHTTPContext) PushBuffer([]byte)                           {}
func (m *mockBedrockHTTPContext) PopBuffer() []byte                           { return nil }
func (m *mockBedrockHTTPContext) BufferQueueSize() int                        { return 0 }
func (m *mockBedrockHTTPContext) DisableReroute()                             {}
func (m *mockBedrockHTTPContext) SetRequestBodyBufferLimit(uint32)            {}
func (m *mockBedrockHTTPContext) SetResponseBodyBufferLimit(uint32)           {}
func (m *mockBedrockHTTPContext) RouteCall(string, string, [][2]string, []byte, iface.RouteResponseCallback) error {
	return nil
}
func (m *mockBedrockHTTPContext) GetExecutionPhase() iface.HTTPExecutionPhase { return 0 }
func (m *mockBedrockHTTPContext) HasRequestBody() bool                        { return true }
func (m *mockBedrockHTTPContext) HasResponseBody() bool                       { return true }
func (m *mockBedrockHTTPContext) IsWebsocket() bool                           { return false }
func (m *mockBedrockHTTPContext) IsBinaryRequestBody() bool                   { return false }
func (m *mockBedrockHTTPContext) IsBinaryResponseBody() bool                  { return false }

func TestSetBedrockAnthropicMessagesRequestDefaults(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-version", "2023-06-01")
	headers.Set("anthropic-beta", "beta-a, beta-b")
	headers.Set("x-api-key", "secret")

	body, err := setBedrockAnthropicMessagesRequestDefaults([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":64,
		"stream":true,
		"messages":[{"role":"user","content":"hi"}],
		"metadata":{"trace_id":"trace-1"},
		"anthropic_beta":["body-beta"]
	}`), headers)
	assert.NoError(t, err)
	assert.False(t, gjson.GetBytes(body, "model").Exists())
	assert.False(t, gjson.GetBytes(body, "stream").Exists())
	assert.False(t, gjson.GetBytes(body, "stream_options").Exists())
	assert.False(t, gjson.GetBytes(body, "metadata").Exists())
	assert.Equal(t, "hi", gjson.GetBytes(body, "messages.0.content").String())
	assert.Equal(t, bedrockAnthropicVersion, gjson.GetBytes(body, "anthropic_version").String())
	assert.Equal(t, []string{"beta-a", "beta-b"}, []string{
		gjson.GetBytes(body, "anthropic_beta.0").String(),
		gjson.GetBytes(body, "anthropic_beta.1").String(),
	})
	assert.Empty(t, headers.Get("anthropic-version"))
	assert.Empty(t, headers.Get("anthropic-beta"))
	assert.Empty(t, headers.Get("x-api-key"))
}

func TestSetBedrockAnthropicMessagesRequestDefaultsPreservesBodyAnthropicBetaWhenHeaderMissing(t *testing.T) {
	headers := http.Header{}

	body, err := setBedrockAnthropicMessagesRequestDefaults([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":64,
		"anthropic_beta":["body-beta"]
	}`), headers)
	assert.NoError(t, err)
	assert.Equal(t, bedrockAnthropicVersion, gjson.GetBytes(body, "anthropic_version").String())
	assert.Equal(t, "body-beta", gjson.GetBytes(body, "anthropic_beta.0").String())
}

func TestDecodeBedrockAnthropicStreamPayload(t *testing.T) {
	inner := `{"type":"message_stop"}`
	wrapped := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(inner)) + `","p":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO"}`

	decoded, err := decodeBedrockAnthropicStreamPayload([]byte(wrapped))
	assert.NoError(t, err)
	assert.JSONEq(t, inner, string(decoded))
}

func TestOnAnthropicMessagesRequestBodyKeepsNativePathForSupportedModel(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	headers := http.Header{}
	provider := &bedrockProvider{
		config: ProviderConfig{
			awsAccessKey: "ak",
			awsSecretKey: "sk",
			awsRegion:    "us-west-2",
		},
	}

	body, err := provider.onAnthropicMessagesRequestBody(ctx, []byte(`{
		"model":"global.anthropic.claude-sonnet-4-5-20250929-v1:0",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hi"}]
	}`), headers)
	assert.NoError(t, err)
	assert.Nil(t, ctx.GetContext(CtxKeyApiName))
	assert.Nil(t, ctx.GetContext("needClaudeResponseConversion"))
	assert.Equal(t, "/model/global.anthropic.claude-sonnet-4-5-20250929-v1%3A0/invoke", headers.Get(":path"))
	assert.Equal(t, bedrockAnthropicVersion, gjson.GetBytes(body, "anthropic_version").String())
	assert.False(t, gjson.GetBytes(body, "model").Exists())
}

func TestOnAnthropicMessagesRequestBodyUsesNativePathForPreviouslyFallbackModel(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	headers := http.Header{}
	provider := &bedrockProvider{
		config: ProviderConfig{
			awsAccessKey: "ak",
			awsSecretKey: "sk",
			awsRegion:    "us-west-2",
		},
	}

	body, err := provider.onAnthropicMessagesRequestBody(ctx, []byte(`{
		"model":"global.anthropic.claude-sonnet-4-5",
		"max_tokens":64,
		"messages":[{"role":"user","content":"hi"}]
	}`), headers)
	assert.NoError(t, err)
	assert.Nil(t, ctx.GetContext(CtxKeyApiName))
	assert.Nil(t, ctx.GetContext("needClaudeResponseConversion"))
	assert.Equal(t, "/model/global.anthropic.claude-sonnet-4-5/invoke", headers.Get(":path"))
	assert.Equal(t, "user", gjson.GetBytes(body, "messages.0.role").String())
	assert.Equal(t, "hi", gjson.GetBytes(body, "messages.0.content").String())
	assert.Equal(t, bedrockAnthropicVersion, gjson.GetBytes(body, "anthropic_version").String())
}

func TestConvertEventFromBedrockToOpenAIUsesOneBasedSequentialToolIndex(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	ctx.SetContext(requestIdHeader, "req-1")
	ctx.SetContext(ctxKeyFinalRequestModel, "amazon.nova-lite-v1:0")
	provider := &bedrockProvider{}

	startOne, err := provider.convertEventFromBedrockToOpenAI(ctx, ApiNameChatCompletion, ConverseStreamEvent{
		Start: &bedrock.ContentBlockStartEvent{
			ToolUse: &bedrock.ToolUseBlockStartEvent{
				Name:      "tool_one",
				ToolUseID: "toolu_1",
			},
		},
	})
	assert.NoError(t, err)

	deltaOne, err := provider.convertEventFromBedrockToOpenAI(ctx, ApiNameChatCompletion, ConverseStreamEvent{
		Delta: &bedrock.ContentBlockDeltaEvent{
			ToolUse: &bedrock.ToolBlockDeltaEvent{
				Input: "{\"a\":1}",
			},
		},
	})
	assert.NoError(t, err)

	startTwo, err := provider.convertEventFromBedrockToOpenAI(ctx, ApiNameChatCompletion, ConverseStreamEvent{
		Start: &bedrock.ContentBlockStartEvent{
			ToolUse: &bedrock.ToolUseBlockStartEvent{
				Name:      "tool_two",
				ToolUseID: "toolu_2",
			},
		},
	})
	assert.NoError(t, err)

	assert.Equal(t, 1, extractFirstToolCallIndexFromSSE(t, startOne))
	assert.Equal(t, 1, extractFirstToolCallIndexFromSSE(t, deltaOne))
	assert.Equal(t, 2, extractFirstToolCallIndexFromSSE(t, startTwo))
}

func extractFirstToolCallIndexFromSSE(t *testing.T, sse []byte) int {
	t.Helper()
	resp := extractChatCompletionChunkFromSSE(t, sse)
	return resp.Choices[0].Delta.ToolCalls[0].Index
}

func extractChatCompletionChunkFromSSE(t *testing.T, sse []byte) *chatCompletionResponse {
	t.Helper()
	const prefix = "data: "
	payload := string(sse)
	assert.Contains(t, payload, prefix)
	payload = payload[len(prefix):]
	payload = payload[:len(payload)-2]
	var resp chatCompletionResponse
	err := json.Unmarshal([]byte(payload), &resp)
	assert.NoError(t, err)
	return &resp
}
