package provider

import (
	"github.com/higress-group/wasm-go/pkg/iface"
)

// mockHttpContext 是 responses bridge 流式测试使用的最小 HttpContext。
// 这些用例只需要 GetContext/SetContext 来验证跨 chunk SSE buffer,
// 其余接口按 mockBedrockHTTPContext 的方式做空实现。
type mockHttpContext struct {
	values map[string]interface{}
}

func newMockHttpContext() *mockHttpContext {
	return &mockHttpContext{values: make(map[string]interface{})}
}

func (m *mockHttpContext) Scheme() string { return "" }
func (m *mockHttpContext) Host() string   { return "" }
func (m *mockHttpContext) Path() string   { return "" }
func (m *mockHttpContext) Method() string { return "" }
func (m *mockHttpContext) SetContext(key string, value interface{}) {
	if value == nil {
		delete(m.values, key)
		return
	}
	m.values[key] = value
}
func (m *mockHttpContext) GetContext(key string) interface{} { return m.values[key] }
func (m *mockHttpContext) GetBoolContext(key string, defaultValue bool) bool {
	if v, ok := m.values[key].(bool); ok {
		return v
	}
	return defaultValue
}
func (m *mockHttpContext) GetIntContext(key string, defaultValue int) int {
	if v, ok := m.values[key].(int); ok {
		return v
	}
	return defaultValue
}
func (m *mockHttpContext) GetStringContext(key, defaultValue string) string {
	if v, ok := m.values[key].(string); ok {
		return v
	}
	return defaultValue
}
func (m *mockHttpContext) GetByteSliceContext(key string, defaultValue []byte) []byte {
	if v, ok := m.values[key].([]byte); ok {
		return v
	}
	return defaultValue
}
func (m *mockHttpContext) GetGlobalConfig(string) interface{}    { return nil }
func (m *mockHttpContext) GetBoolGlobalConfig(string, bool) bool { return false }
func (m *mockHttpContext) GetIntGlobalConfig(string, int) int    { return 0 }
func (m *mockHttpContext) GetStringGlobalConfig(_ string, defaultValue string) string {
	return defaultValue
}
func (m *mockHttpContext) GetUserAttribute(string) interface{}        { return nil }
func (m *mockHttpContext) SetUserAttribute(string, interface{})       {}
func (m *mockHttpContext) SetUserAttributeMap(map[string]interface{}) {}
func (m *mockHttpContext) GetUserAttributeMap() map[string]interface{} {
	return nil
}
func (m *mockHttpContext) WriteUserAttributeToLog() error              { return nil }
func (m *mockHttpContext) WriteUserAttributeToLogWithKey(string) error { return nil }
func (m *mockHttpContext) WriteUserAttributeToTrace() error            { return nil }
func (m *mockHttpContext) DontReadRequestBody()                        {}
func (m *mockHttpContext) DontReadResponseBody()                       {}
func (m *mockHttpContext) BufferRequestBody()                          {}
func (m *mockHttpContext) BufferResponseBody()                         {}
func (m *mockHttpContext) NeedPauseStreamingResponse()                 {}
func (m *mockHttpContext) PushBuffer([]byte)                           {}
func (m *mockHttpContext) PopBuffer() []byte                           { return nil }
func (m *mockHttpContext) BufferQueueSize() int                        { return 0 }
func (m *mockHttpContext) DisableReroute()                             {}
func (m *mockHttpContext) SetRequestBodyBufferLimit(uint32)            {}
func (m *mockHttpContext) SetResponseBodyBufferLimit(uint32)           {}
func (m *mockHttpContext) RouteCall(string, string, [][2]string, []byte, iface.RouteResponseCallback) error {
	return nil
}
func (m *mockHttpContext) GetExecutionPhase() iface.HTTPExecutionPhase { return 0 }
func (m *mockHttpContext) HasRequestBody() bool                        { return true }
func (m *mockHttpContext) HasResponseBody() bool                       { return true }
func (m *mockHttpContext) IsWebsocket() bool                           { return false }
func (m *mockHttpContext) IsBinaryRequestBody() bool                   { return false }
func (m *mockHttpContext) IsBinaryResponseBody() bool                  { return false }
