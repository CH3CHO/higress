package test

import (
	"encoding/json"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"
)

// 测试配置：用于测试内置参数的基本配置
var fullBundledRequestParamsConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"provider": map[string]interface{}{
			"type": "azure",
			"apiTokens": []string{
				"sk-1234567890",
			},
			"azureServiceUrl": "https://YOUR_RESOURCE_NAME.openai.azure.com/openai/deployments/YOUR_DEPLOYMENT_NAME/chat/completions?api-version=2024-10-21",
			"bundledRequestParams": []map[string]interface{}{
				{
					"target":      0,
					"name":        "x-test-header-1",
					"value":       "my-test-value",
					"valueSource": 0,
				},
				{
					"target":      0,
					"name":        "x-test-header-2",
					"value":       "user-agent",
					"valueSource": 1,
				},
				{
					"target":      0,
					"name":        "x-test-header-3",
					"value":       "model",
					"valueSource": 2,
				},
				{
					"target":      1,
					"name":        "testParam1",
					"value":       "value1",
					"valueSource": 0,
				},
				{
					"target":      1,
					"name":        "testParam2",
					"value":       "connection",
					"valueSource": 1,
				},
				{
					"target":      1,
					"name":        "testParam3",
					"value":       "name",
					"valueSource": 2,
				},
			},
		},
	})
	return data
}()

func RunBundledRequestParamsTests(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		t.Run("full bundled request params config with no body", func(t *testing.T) {
			host, status := test.NewTestHost(fullBundledRequestParamsConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)

			inputHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions?model=gpt-4&name=trip"},
				{":method", "POST"},
				{"Connection", "keep-alive"},
				{"Content-Type", "application/json"},
				{"User-Agent", "test-browser"},
			}

			action := host.CallOnHttpRequestHeaders(inputHeaders, test.WithEndOfStream(true))

			require.Equal(t, types.ActionContinue, action)

			outputHeaders := host.GetRequestHeaders()
			require.NotNil(t, outputHeaders)

			expectedOutputHeaders := make(map[string]string)
			for _, header := range inputHeaders {
				expectedOutputHeaders[header[0]] = header[1]
			}
			expectedOutputHeaders["x-test-header-1"] = "my-test-value"
			expectedOutputHeaders["x-test-header-2"] = "test-browser"
			expectedOutputHeaders["x-test-header-3"] = "gpt-4"
			expectedOutputHeaders[":path"] = "/openai/deployments/YOUR_DEPLOYMENT_NAME/chat/completions?api-version=2024-10-21&testParam1=value1&testParam2=keep-alive&testParam3=trip"
			expectedOutputHeaders[":authority"] = "YOUR_RESOURCE_NAME.openai.azure.com"
			for name, value := range expectedOutputHeaders {
				outputHeaderValue, found := test.GetHeaderValue(outputHeaders, name)
				require.True(t, found, "header %s not found in output headers", name)
				require.Equal(t, value, outputHeaderValue)
			}
		})

		t.Run("full bundled request params config with body", func(t *testing.T) {
			host, status := test.NewTestHost(fullBundledRequestParamsConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)

			inputHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/v1/chat/completions?model=gpt-4&name=trip"},
				{":method", "POST"},
				{"Connection", "keep-alive"},
				{"Content-Type", "application/json"},
				{"User-Agent", "test-browser"},
			}

			action := host.CallOnHttpRequestHeaders(inputHeaders, test.WithEndOfStream(false))

			require.Equal(t, types.HeaderStopIteration, action)

			outputHeaders := host.GetRequestHeaders()
			require.NotNil(t, outputHeaders)

			expectedOutputHeaders := make(map[string]string)
			for _, header := range inputHeaders {
				expectedOutputHeaders[header[0]] = header[1]
			}
			expectedOutputHeaders[":path"] = "/openai/deployments/YOUR_DEPLOYMENT_NAME/chat/completions?api-version=2024-10-21"
			expectedOutputHeaders[":authority"] = "YOUR_RESOURCE_NAME.openai.azure.com"
			for name, value := range expectedOutputHeaders {
				outputHeaderValue, found := test.GetHeaderValue(outputHeaders, name)
				require.True(t, found, "header %s not found in output headers", name)
				require.Equal(t, value, outputHeaderValue)
			}

			expectedOutputHeaders["x-test-header-1"] = "my-test-value"
			expectedOutputHeaders["x-test-header-2"] = "test-browser"
			expectedOutputHeaders["x-test-header-3"] = "gpt-4"
			expectedOutputHeaders[":path"] = "/openai/deployments/YOUR_DEPLOYMENT_NAME/chat/completions?api-version=2024-10-21&testParam1=value1&testParam2=keep-alive&testParam3=trip"

			action = host.CallOnHttpRequestBody([]byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}`))
			require.Equal(t, types.ActionContinue, action)

			outputHeaders = host.GetRequestHeaders()
			require.NotNil(t, outputHeaders)
			for name, value := range expectedOutputHeaders {
				outputHeaderValue, found := test.GetHeaderValue(outputHeaders, name)
				require.True(t, found, "header %s not found in output headers", name)
				require.Equal(t, value, outputHeaderValue)
			}
		})
	})
}
