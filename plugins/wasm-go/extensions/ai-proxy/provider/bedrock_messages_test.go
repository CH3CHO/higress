package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"strings"
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

func TestSetBedrockAnthropicMessagesRequestDefaultsPreservesThinkingBlocks(t *testing.T) {
	headers := http.Header{}

	body, err := setBedrockAnthropicMessagesRequestDefaults([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":64,
		"messages":[
			{"role":"user","content":"hi"},
			{"role":"assistant","content":[
				{"type":"thinking","thinking":"internal reasoning","signature":"sig-1"},
				{"type":"text","text":"final answer"}
			]}
		]
	}`), headers)
	assert.NoError(t, err)
	assert.Equal(t, "thinking", gjson.GetBytes(body, "messages.1.content.0.type").String())
	assert.Equal(t, "internal reasoning", gjson.GetBytes(body, "messages.1.content.0.thinking").String())
	assert.Equal(t, "sig-1", gjson.GetBytes(body, "messages.1.content.0.signature").String())
	assert.Equal(t, "text", gjson.GetBytes(body, "messages.1.content.1.type").String())
	assert.Equal(t, "final answer", gjson.GetBytes(body, "messages.1.content.1.text").String())
}

func TestSetBedrockAnthropicMessagesRequestDefaultsPreservesRicherToolUnion(t *testing.T) {
	headers := http.Header{}

	body, err := setBedrockAnthropicMessagesRequestDefaults([]byte(`{
		"model":"claude-sonnet-4-5",
		"max_tokens":64,
		"tools":[
			{
				"type":"web_search_20250305",
				"name":"web_search",
				"max_uses":5
			}
		],
		"tool_choice":{
			"type":"tool",
			"name":"web_search",
			"disable_parallel_tool_use":true
		},
		"messages":[{"role":"user","content":"hi"}]
	}`), headers)
	assert.NoError(t, err)
	assert.Equal(t, "web_search_20250305", gjson.GetBytes(body, "tools.0.type").String())
	assert.Equal(t, "web_search", gjson.GetBytes(body, "tools.0.name").String())
	assert.Equal(t, float64(5), gjson.GetBytes(body, "tools.0.max_uses").Float())
	assert.Equal(t, "tool", gjson.GetBytes(body, "tool_choice.type").String())
	assert.Equal(t, "web_search", gjson.GetBytes(body, "tool_choice.name").String())
	assert.True(t, gjson.GetBytes(body, "tool_choice.disable_parallel_tool_use").Bool())
}

func TestDecodeBedrockAnthropicStreamPayload(t *testing.T) {
	inner := `{"type":"message_stop"}`
	wrapped := `{"bytes":"` + base64.StdEncoding.EncodeToString([]byte(inner)) + `","p":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO"}`

	decoded, err := decodeBedrockAnthropicStreamPayload([]byte(wrapped))
	assert.NoError(t, err)
	assert.JSONEq(t, inner, string(decoded))
}

func TestDecodeAmazonEventStreamMessageSeparatesHeadersAndPayload(t *testing.T) {
	payload := mustMarshalJSON(t, map[string]any{
		"contentBlockIndex": 0,
		"p":                 "abcdefghijklmnopqrstuvwxyzABCDEFG",
	})
	frame := encodeAmazonEventStreamMessage(t, "contentBlockDelta", payload)

	msg, err := decodeMessage(bytes.NewReader(frame), make([]byte, 1024))
	assert.NoError(t, err)

	headerMap := make(map[string]string, len(msg.Headers))
	for _, h := range msg.Headers {
		headerMap[h.Name] = h.Value.Get().(string)
	}
	t.Logf("headers=%#v", headerMap)
	t.Logf("payload=%s", string(msg.Payload))

	var payloadMap map[string]any
	err = json.Unmarshal(msg.Payload, &payloadMap)
	assert.NoError(t, err)
	t.Logf("payload_map=%#v", payloadMap)

	ctx := newMockBedrockHTTPContext()
	events := extractAmazonEventStreamEvents(ctx, frame)
	t.Logf("events=%#v", events)

	assert.Equal(t, "contentBlockDelta", headerMap[":event-type"])
	assert.Equal(t, "application/json", headerMap[":content-type"])
	assert.Equal(t, "event", headerMap[":message-type"])
	assert.JSONEq(t, string(payload), string(msg.Payload))
	assert.Equal(t, float64(0), payloadMap["contentBlockIndex"])
	assert.Equal(t, "abcdefghijklmnopqrstuvwxyzABCDEFG", payloadMap["p"])
	assert.Len(t, events, 1)
	assert.Equal(t, 0, events[0].ContentBlockIndex)
	assert.Nil(t, events[0].Delta)
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

func TestOnBedrockConverseStreamingResponseBodyDoesNotEmitDoneForIncompleteChunk(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	provider := &bedrockProvider{}

	out, err := provider.onBedrockConverseStreamingResponseBody(ctx, ApiNameChatCompletion, []byte("partial-eventstream-frame"), false)
	assert.NoError(t, err)
	assert.Equal(t, []byte(""), out)

	buffered, _ := ctx.GetContext(ctxKeyStreamingBody).([]byte)
	assert.Equal(t, []byte("partial-eventstream-frame"), buffered)
}

func TestOnBedrockConverseStreamingResponseBodyDoesNotLeakRawChunkWhenEventStreamFrameSplitAcrossChunks(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	ctx.SetContext(requestIdHeader, "req-1")
	ctx.SetContext(ctxKeyFinalRequestModel, "claude-sonnet-4-5")
	provider := &bedrockProvider{}

	reasoningTextPayload := mustMarshalJSON(t, map[string]any{
		"contentBlockIndex": 0,
		"delta": map[string]any{
			"reasoningContent": map[string]any{
				"text": "程。",
			},
		},
	})
	signaturePayload := mustMarshalJSON(t, map[string]any{
		"contentBlockIndex": 0,
		"delta": map[string]any{
			"reasoningContent": map[string]any{
				"signature": "sig-11111111111111111111111112222222222222222222222222222222222222222333333333333333333333333333333333333333",
			},
		},
	})
	textPayload := mustMarshalJSON(t, map[string]any{
		"contentBlockIndex": 1,
		"delta": map[string]any{
			"text": "文",
		},
	})

	reasoningTextFrame := encodeAmazonEventStreamMessage(t, "contentBlockDelta", reasoningTextPayload)
	signatureFrame := encodeAmazonEventStreamMessage(t, "contentBlockDelta", signaturePayload)
	textFrame := encodeAmazonEventStreamMessage(t, "contentBlockDelta", textPayload)

	_, decodeErr := decodeMessage(bytes.NewReader(reasoningTextFrame), make([]byte, 1024))
	assert.NoError(t, decodeErr)

	sanityCtx := newMockBedrockHTTPContext()
	reasoningTextEvents := extractAmazonEventStreamEvents(sanityCtx, reasoningTextFrame)
	assert.Len(t, reasoningTextEvents, 1)
	assert.NotNil(t, reasoningTextEvents[0].Delta)
	assert.NotNil(t, reasoningTextEvents[0].Delta.ReasoningContent)
	assert.Equal(t, "程。", reasoningTextEvents[0].Delta.ReasoningContent.Text)

	reasoningOut, err := provider.OnStreamingResponseBody(ctx, ApiNameChatCompletion, reasoningTextFrame, false)
	assert.NoError(t, err)
	assert.Contains(t, string(reasoningOut), `"reasoning_content":"程。"`)
	assert.NotContains(t, string(reasoningOut), ":content-typeapplication/json")
	assert.NotContains(t, string(reasoningOut), ":message-typeevent")

	splitAt1 := len(signatureFrame) / 3
	splitAt2 := splitAt1 * 2
	firstPart := signatureFrame[:splitAt1]
	secondPart := signatureFrame[splitAt1:splitAt2]
	thirdPart := signatureFrame[splitAt2:]

	firstOut, err := provider.OnStreamingResponseBody(ctx, ApiNameChatCompletion, firstPart, false)
	assert.NoError(t, err)
	t.Logf("chunk1 输入%s\nchunk1 输出%s", string(firstPart), string(firstOut))
	assert.Equal(t, []byte(""), firstOut)

	secondOut, err := provider.OnStreamingResponseBody(ctx, ApiNameChatCompletion, secondPart, false)
	assert.NoError(t, err)
	t.Logf("chunk2 输入%s\nchunk2 输出%s", string(secondPart), string(secondOut))
	assert.Equal(t, []byte(""), secondOut)

	thirdOut, err := provider.OnStreamingResponseBody(ctx, ApiNameChatCompletion, thirdPart, false)
	assert.NoError(t, err)
	t.Logf("chunk3 输入%s\nchunk3 输出%s", string(thirdPart), string(thirdOut))
	assert.Contains(t, string(thirdOut), `"signature":"sig-11111111111111111111111112222222222222222222222222222222222222222333333333333333333333333333333333333333"`)
	assert.NotContains(t, string(thirdOut), ":content-typeapplication/json")
	assert.NotContains(t, string(thirdOut), ":message-typeevent")
	assert.NotContains(t, string(thirdOut), ":event-typecontentBlockDelta")

	finalOut, err := provider.OnStreamingResponseBody(ctx, ApiNameChatCompletion, textFrame, true)
	assert.NoError(t, err)
	assert.Contains(t, string(finalOut), `"content":"文"`)
	assert.True(t, strings.HasSuffix(string(finalOut), ssePrefix+streamEndDataValue+"\n\n"))
	assert.NotContains(t, string(finalOut), ":content-typeapplication/json")
	assert.NotContains(t, string(finalOut), ":message-typeevent")
}

func TestOnBedrockConverseStreamingResponseBodyEmitsDoneOnLastChunkWithoutBufferedFrame(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	provider := &bedrockProvider{}

	out, err := provider.onBedrockConverseStreamingResponseBody(ctx, ApiNameChatCompletion, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, "data: [DONE]\n\n", string(out))
}

func TestOnBedrockConverseStreamingResponseBodyEmitsDoneOnLastChunkWithBufferedFrame(t *testing.T) {
	ctx := newMockBedrockHTTPContext()
	ctx.SetContext(ctxKeyStreamingBody, []byte("partial-eventstream-frame"))
	provider := &bedrockProvider{}

	out, err := provider.onBedrockConverseStreamingResponseBody(ctx, ApiNameChatCompletion, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, "data: [DONE]\n\n", string(out))
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

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	out, err := json.Marshal(value)
	assert.NoError(t, err)
	return out
}

func encodeAmazonEventStreamMessage(t *testing.T, eventType string, payload []byte) []byte {
	t.Helper()

	headers := encodeAmazonEventStreamHeaders(t, map[string]string{
		":event-type":   eventType,
		":content-type": "application/json",
		":message-type": "event",
	})

	headersLength := uint32(len(headers))
	totalLength := uint32(16 + len(headers) + len(payload))

	prelude := make([]byte, 8)
	binary.BigEndian.PutUint32(prelude[0:4], totalLength)
	binary.BigEndian.PutUint32(prelude[4:8], headersLength)

	preludeCRC := crc32.ChecksumIEEE(prelude)

	messageWithoutCRC := make([]byte, 12+len(headers)+len(payload))
	copy(messageWithoutCRC[0:8], prelude)
	binary.BigEndian.PutUint32(messageWithoutCRC[8:12], preludeCRC)
	copy(messageWithoutCRC[12:12+len(headers)], headers)
	copy(messageWithoutCRC[12+len(headers):], payload)

	messageCRC := crc32.ChecksumIEEE(messageWithoutCRC)

	fullMessage := make([]byte, len(messageWithoutCRC)+4)
	copy(fullMessage, messageWithoutCRC)
	binary.BigEndian.PutUint32(fullMessage[len(messageWithoutCRC):], messageCRC)
	return fullMessage
}

func encodeAmazonEventStreamHeaders(t *testing.T, values map[string]string) []byte {
	t.Helper()

	order := []string{":event-type", ":content-type", ":message-type"}
	var out []byte
	for _, key := range order {
		value, ok := values[key]
		if !ok {
			continue
		}
		assert.LessOrEqual(t, len(key), 255)
		header := make([]byte, 0, 1+len(key)+1+2+len(value))
		header = append(header, byte(len(key)))
		header = append(header, []byte(key)...)
		header = append(header, byte(stringValueType))
		valueLen := make([]byte, 2)
		binary.BigEndian.PutUint16(valueLen, uint16(len(value)))
		header = append(header, valueLen...)
		header = append(header, []byte(value)...)
		out = append(out, header...)
	}
	return out
}
