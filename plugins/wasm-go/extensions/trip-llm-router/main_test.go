package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"
)

var routerConfig = func() json.RawMessage {
	return []byte(`
{
	"consumers": [
		{
			"id": "consumer1",
			"routes": [
				{
					"id": "route1",
					"models": ["model1", "model2"],
					"services": [
						{
							"id": "service1",
							"weight": 100
						}
					],
					"ratelimit": {
						"tpm": 100000,
						"rpm": 200
					},
					"protocols": [
						"openai"
					],
					"fallback": {
						"statusCodes": ["429", "5xx"],
						"services": [
							{
								"id": "service2",
								"weight": 100
							}
						]
					}
				}
			]
		}
	]
}
`)
}()

func TestConfigParsing(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		t.Run("config parsing", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)

			pluginConfig, ok := config.(*PluginConfig)
			require.True(t, ok, "config should be of type *PluginConfig")
			require.NotNil(t, pluginConfig)

			// Validate the content of the config object
			consumers := pluginConfig.Consumers
			require.Len(t, consumers, 1)
			consumer1, exists := pluginConfig.Consumers["consumer1"]
			require.True(t, exists, "consumer1 should exist in the config")
			require.NotNil(t, consumer1)
			require.Equal(t, "consumer1", consumer1.ID)
			require.Len(t, consumer1.ModelRoutes, 2)

			model1Route := consumer1.ModelRoutes["model1"]
			model2Route := consumer1.ModelRoutes["model2"]
			require.NotNil(t, model1Route)
			require.Same(t, model1Route, model2Route)
			route := model1Route
			require.Equal(t, "route1", route.ID)
			require.ElementsMatch(t, []string{"model1", "model2"}, route.Models)
			services := route.Upstream.Services
			require.Len(t, services, 1)
			require.Equal(t, "service1", services[0].ID)
			require.Equal(t, 100, services[0].Weight)

			rat := route.Ratelimit
			require.NotNil(t, rat)
			require.Equal(t, 100000, rat.TPM)
			require.Equal(t, 200, rat.RPM)

			fallback := route.Fallback
			require.NotNil(t, fallback)
			require.ElementsMatch(t, []string{"429", "5xx"}, fallback.StatusCodes)
			require.Len(t, fallback.Services, 1)
			require.Equal(t, "service2", fallback.Services[0].ID)
		})
	})
}

func TestE2E(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		t.Run("e2e", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			inputRequestHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			}
			inputRequestHeaderMap := make(map[string]string)
			for _, header := range inputRequestHeaders {
				inputRequestHeaderMap[strings.ToLower(header[0])] = header[1]
			}
			require.Len(t, inputRequestHeaderMap, len(inputRequestHeaders), "Request headers should be unique")

			action := host.CallOnHttpRequestHeaders(inputRequestHeaders)
			require.Equal(t, types.ActionContinue, action)

			outputRequestHeaders := host.GetRequestHeaders()
			require.NotNil(t, outputRequestHeaders)
			require.Len(t, outputRequestHeaders, len(inputRequestHeaders)+2)
			for _, header := range outputRequestHeaders {
				name := header[0]
				value := header[1]

				switch name {
				case originalHostHeaderName:
					require.Equal(t, "example.com", value, "Original host should be preserved in header")
				case authorityHostHeaderName:
					require.Equal(t, "aigw.internal", value, "Authority should be rewritten to service route host")
				case llmServiceIdHeaderName:
					require.Equal(t, "service1", value, "LLM Service ID header should be set to the matched service")
				default:
					inputHeaderValue, ok := inputRequestHeaderMap[name]
					require.True(t, ok, "No new request header shall be created: %s", name)
					require.Equal(t, value, inputHeaderValue, "Existing headers should be preserved")
				}
			}

			requestRatelimitKey, _ := host.GetProperty(requestRatelimitKeyPropertyPath)
			requestRatelimitCount, _ := host.GetProperty(requestRatelimitCountPropertyPath)
			requestRatelimitTimeWindow, _ := host.GetProperty(requestRatelimitTimeWindowPropertyPath)
			require.Equal(t, []byte("higress-cluster-key-rate-limit:route1:global_threshold:200:60"), requestRatelimitKey, "Request rate limit key should be set to route ID")
			require.Equal(t, uint32ToBytes(200), requestRatelimitCount, "Request rate limit count should be set to route RPM")
			require.Equal(t, uint32ToBytes(60), requestRatelimitTimeWindow, "Request rate limit time window should be 60 seconds")

			tokenRatelimitKey, _ := host.GetProperty(tokenRatelimitKeyPropertyPath)
			tokenRatelimitCount, _ := host.GetProperty(tokenRatelimitCountPropertyPath)
			tokenRatelimitTimeWindow, _ := host.GetProperty(tokenRatelimitTimeWindowPropertyPath)
			require.Equal(t, []byte("higress-token-ratelimit:route1:global_threshold:100000:60"), tokenRatelimitKey, "Token rate limit key should be set to route ID")
			require.Equal(t, uint32ToBytes(100000), tokenRatelimitCount, "Token rate limit count should be set to route TPM")
			require.Equal(t, uint32ToBytes(60), tokenRatelimitTimeWindow, "Token rate limit time window should be 60 seconds")

			inputResponseHeaders := [][2]string{
				{":status", "200"},
				{"Content-Type", "application/json"},
			}
			inputResponseHeaderMap := make(map[string]string)
			for _, header := range inputResponseHeaders {
				inputResponseHeaderMap[strings.ToLower(header[0])] = header[1]
			}

			action = host.CallOnHttpResponseHeaders(inputResponseHeaders)
			require.Equal(t, types.ActionContinue, action)

			outputResponseHeaders := host.GetResponseHeaders()
			require.Len(t, outputResponseHeaders, len(inputResponseHeaders))
			require.NotNil(t, outputResponseHeaders)
			for _, header := range outputResponseHeaders {
				name := header[0]
				value := header[1]

				inputHeaderValue, ok := inputResponseHeaderMap[name]
				require.True(t, ok, "No new response header shall be created: %s", name)
				require.Equal(t, value, inputHeaderValue, "Existing response headers should be preserved")
			}
		})

		t.Run("e2e with fallback", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			inputRequestHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			}
			inputRequestHeaderMap := make(map[string]string)
			for _, header := range inputRequestHeaders {
				inputRequestHeaderMap[strings.ToLower(header[0])] = header[1]
			}
			require.Len(t, inputRequestHeaderMap, len(inputRequestHeaders), "Request headers should be unique")

			action := host.CallOnHttpRequestHeaders(inputRequestHeaders)
			require.Equal(t, types.ActionContinue, action)

			outputRequestHeaders := host.GetRequestHeaders()
			require.NotNil(t, outputRequestHeaders)
			require.Len(t, outputRequestHeaders, len(inputRequestHeaders)+2)
			for _, header := range outputRequestHeaders {
				name := header[0]
				value := header[1]

				switch name {
				case originalHostHeaderName:
					require.Equal(t, "example.com", value, "Original host should be preserved in header")
				case authorityHostHeaderName:
					require.Equal(t, "aigw.internal", value, "Authority should be rewritten to service route host")
				case llmServiceIdHeaderName:
					require.Equal(t, "service1", value, "LLM Service ID header should be set to the matched service")
				default:
					inputHeaderValue, ok := inputRequestHeaderMap[name]
					require.True(t, ok, "No new request header shall be created: %s", name)
					require.Equal(t, value, inputHeaderValue, "Existing headers should be preserved")
				}
			}

			requestRatelimitKey, _ := host.GetProperty(requestRatelimitKeyPropertyPath)
			requestRatelimitCount, _ := host.GetProperty(requestRatelimitCountPropertyPath)
			requestRatelimitTimeWindow, _ := host.GetProperty(requestRatelimitTimeWindowPropertyPath)
			require.Equal(t, []byte("higress-cluster-key-rate-limit:route1:global_threshold:200:60"), requestRatelimitKey, "Request rate limit key should be set to route ID")
			require.Equal(t, uint32ToBytes(200), requestRatelimitCount, "Request rate limit count should be set to route RPM")
			require.Equal(t, uint32ToBytes(60), requestRatelimitTimeWindow, "Request rate limit time window should be 60 seconds")

			tokenRatelimitKey, _ := host.GetProperty(tokenRatelimitKeyPropertyPath)
			tokenRatelimitCount, _ := host.GetProperty(tokenRatelimitCountPropertyPath)
			tokenRatelimitTimeWindow, _ := host.GetProperty(tokenRatelimitTimeWindowPropertyPath)
			require.Equal(t, []byte("higress-token-ratelimit:route1:global_threshold:100000:60"), tokenRatelimitKey, "Token rate limit key should be set to route ID")
			require.Equal(t, uint32ToBytes(100000), tokenRatelimitCount, "Token rate limit count should be set to route TPM")
			require.Equal(t, uint32ToBytes(60), tokenRatelimitTimeWindow, "Token rate limit time window should be 60 seconds")

			inputResponseHeaders := [][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			}
			inputResponseHeaderMap := make(map[string]string)
			for _, header := range inputResponseHeaders {
				inputResponseHeaderMap[strings.ToLower(header[0])] = header[1]
			}

			action = host.CallOnHttpResponseHeaders(inputResponseHeaders)
			require.Equal(t, types.ActionContinue, action)

			outputResponseHeaders := host.GetResponseHeaders()
			require.NotNil(t, outputResponseHeaders)
			require.Len(t, outputResponseHeaders, len(inputResponseHeaders)+1)
			for _, header := range outputResponseHeaders {
				name := header[0]
				value := header[1]

				switch name {
				case fallbackHeaderName:
					require.Equal(t, fallbackHeaderValue, value, "Fallback header should be set when response status code matches fallback criteria")
				default:
					inputHeaderValue, ok := inputResponseHeaderMap[name]
					require.True(t, ok, "No new response header shall be created: %s", name)
					require.Equal(t, value, inputHeaderValue, "Existing response headers should be preserved")
				}
			}
		})

		t.Run("unauthorized model", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			inputRequestHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model3"},
			}
			inputRequestHeaderMap := make(map[string]string)
			for _, header := range inputRequestHeaders {
				inputRequestHeaderMap[strings.ToLower(header[0])] = header[1]
			}
			require.Len(t, inputRequestHeaderMap, len(inputRequestHeaders), "Request headers should be unique")

			action := host.CallOnHttpRequestHeaders(inputRequestHeaders)
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse)
			require.Equal(t, uint32(403), localResponse.StatusCode, "Response status code should be 403 for unknown consumer")
			require.Equal(t, "trip-llm-router.unauthorized-model", localResponse.StatusCodeDetail)
			require.Equal(t, [][2]string{{"content-type", "application/json"}}, localResponse.Headers)
			require.Equal(t, `{"code":403,"message":"Consumer consumer1 is not authorized to access model model3"}`, string(localResponse.Data))
		})

		t.Run("unknown consumer", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			inputRequestHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer2/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer2"},
				{"x-higress-llm-model", "model1"},
			}
			inputRequestHeaderMap := make(map[string]string)
			for _, header := range inputRequestHeaders {
				inputRequestHeaderMap[strings.ToLower(header[0])] = header[1]
			}
			require.Len(t, inputRequestHeaderMap, len(inputRequestHeaders), "Request headers should be unique")

			action := host.CallOnHttpRequestHeaders(inputRequestHeaders)
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse)
			require.Equal(t, uint32(403), localResponse.StatusCode, "Response status code should be 403 for unknown consumer")
			require.Equal(t, "trip-llm-router.unknown-consumer", localResponse.StatusCodeDetail)
			require.Equal(t, [][2]string{{"content-type", "application/json"}}, localResponse.Headers)
			require.Equal(t, `{"code":403,"message":"Consumer not recognized: consumer2"}`, string(localResponse.Data))
		})

		t.Run("unenabled protocol", func(t *testing.T) {
			host, status := test.NewTestHost(routerConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			inputRequestHeaders := [][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/raw/aigc/text-generation/generation"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			}
			inputRequestHeaderMap := make(map[string]string)
			for _, header := range inputRequestHeaders {
				inputRequestHeaderMap[strings.ToLower(header[0])] = header[1]
			}
			require.Len(t, inputRequestHeaderMap, len(inputRequestHeaders), "Request headers should be unique")

			action := host.CallOnHttpRequestHeaders(inputRequestHeaders)
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse)
			require.Equal(t, uint32(403), localResponse.StatusCode, "Response status code should be 403 for unknown consumer")
			require.Equal(t, "trip-llm-router.unenabled-protocol", localResponse.StatusCodeDetail)
			require.Equal(t, [][2]string{{"content-type", "application/json"}}, localResponse.Headers)
			require.Equal(t, `{"code":403,"message":"Protocol original is not enabled for model model1"}`, string(localResponse.Data))
		})
	})
}

// TestFallbackEnabledConfig 测试 fallback 动态开关配置
func TestFallbackEnabledConfig(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		t.Run("fallback global enabled", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 模拟 fallback 请求（带有 fallback header）
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否路由到了 fallback service
			requestHeaders := host.GetRequestHeaders()
			found := false
			for _, header := range requestHeaders {
				if header[0] == llmServiceIdHeaderName && header[1] == "fallback-service1" {
					found = true
					break
				}
			}
			require.True(t, found, "Should route to fallback service when fallback is enabled")
		})

		t.Run("fallback global disabled", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled": false,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 模拟 fallback 请求（带有 fallback header）
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否返回了禁用 fallback 的错误
			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return error when fallback is disabled")
			require.Equal(t, uint32(400), localResponse.StatusCode)
			require.Equal(t, "trip-llm-router.fallback-disabled", localResponse.StatusCodeDetail)
		})

		t.Run("fallback disabled by consumer", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
					{
						"id": "consumer2",
						"routes": []map[string]interface{}{
							{
								"id":     "route2",
								"models": []string{"model2"},
								"services": []map[string]interface{}{
									{"id": "service2", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service2"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled":           true,
					"fallback.enabled.consumer1": false,
					"fallback.enabled.consumer2": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// consumer1 的 fallback 被禁用
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return error for consumer1 when fallback is disabled")
			require.Equal(t, uint32(400), localResponse.StatusCode)

			// consumer2 的 fallback 仍然启用
			host.Reset()
			host, _ = test.NewTestHost(pluginConfig)
			action = host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer2/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer2"},
				{"x-higress-llm-model", "model2"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否路由到了 fallback service
			requestHeaders := host.GetRequestHeaders()
			found := false
			for _, header := range requestHeaders {
				if header[0] == llmServiceIdHeaderName && header[1] == "fallback-service2" {
					found = true
					break
				}
			}
			require.True(t, found, "consumer2 should route to fallback service")
		})

		t.Run("fallback disabled by consumer and model", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1", "model2"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled":                  true,
					"fallback.enabled.consumer1":        true,
					"fallback.enabled.consumer1.model1": false,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// consumer1 + model1 的 fallback 被禁用
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return error for consumer1+model1 when fallback is disabled")
			require.Equal(t, uint32(400), localResponse.StatusCode)

			// consumer1 + model2 的 fallback 仍然启用
			host.Reset()
			host, _ = test.NewTestHost(pluginConfig)
			action = host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model2"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否路由到了 fallback service
			requestHeaders := host.GetRequestHeaders()
			found := false
			for _, header := range requestHeaders {
				if header[0] == llmServiceIdHeaderName && header[1] == "fallback-service1" {
					found = true
					break
				}
			}
			require.True(t, found, "consumer1+model2 should route to fallback service")
		})

		t.Run("fallback response header not set when disabled", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled": false,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 首先发送正常请求
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟返回 429 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有设置 fallback header，且原始状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should not set fallback header when fallback is disabled")
			require.Equal(t, "429", statusCode, "Original error status code should be preserved when fallback is disabled")
		})

		t.Run("fallback response header set when enabled", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 首先发送正常请求
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟返回 429 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否设置了 fallback header
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName && header[1] == fallbackHeaderValue {
					found = true
					break
				}
			}
			require.True(t, found, "Should set fallback header when fallback is enabled and status matches")
		})

		t.Run("no config - fallback enabled by default", func(t *testing.T) {
			// 没有 _config_ 配置，fallback 应该默认启用
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 模拟 fallback 请求
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
				{"x-trip-llm-fallback", "1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 检查是否路由到了 fallback service
			requestHeaders := host.GetRequestHeaders()
			found := false
			for _, header := range requestHeaders {
				if header[0] == llmServiceIdHeaderName && header[1] == "fallback-service1" {
					found = true
					break
				}
			}
			require.True(t, found, "Should route to fallback service by default when no config is set")
		})

		// 测试各种异常状态码触发 fallback header 的情况
		t.Run("fallback header set on 429 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 429 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证设置了 fallback header
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName && header[1] == fallbackHeaderValue {
					found = true
					break
				}
			}
			require.True(t, found, "Should set fallback header on 429 status")
		})

		t.Run("fallback header set on 500 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 500 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "500"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证设置了 fallback header
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName && header[1] == fallbackHeaderValue {
					found = true
					break
				}
			}
			require.True(t, found, "Should set fallback header on 500 status (5xx match)")
		})

		t.Run("fallback header set on 503 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 503 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "503"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证设置了 fallback header
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName && header[1] == fallbackHeaderValue {
					found = true
					break
				}
			}
			require.True(t, found, "Should set fallback header on 503 status (5xx match)")
		})

		t.Run("no fallback header on 200 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 200 响应
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有设置 fallback header（200状态码不应触发fallback）
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
					break
				}
			}
			require.False(t, found, "Should NOT set fallback header on 200 status")
		})

		t.Run("no fallback header on 400 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 400 响应（不在 fallback 配置中）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "400"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有设置 fallback header，且原始状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should NOT set fallback header on 400 status (not in fallback statusCodes)")
			require.Equal(t, "400", statusCode, "Original error status code should be preserved")
		})

		t.Run("no fallback header when fallback config is nil", func(t *testing.T) {
			// 没有配置 fallback 的路由
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								// 没有 fallback 配置
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 429 响应（即使有错误码，但没有 fallback 配置）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有设置 fallback header，且原始状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should NOT set fallback header when fallback config is nil")
			require.Equal(t, "429", statusCode, "Original error status code should be preserved")
		})

		// 场景1：没有配置 fallback.enabled 相关配置，但配置了 fallback，
		// 接到满足 fallbackconfig 要求的响应时应该触发 fallback header
		t.Run("fallback header set without fallback.enabled config", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				// 注意：没有 _config_ 字段，即没有 fallback.enabled 配置
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 发送请求
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 429 响应（满足 fallback 条件）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证设置了 fallback header（默认启用）
			responseHeaders := host.GetResponseHeaders()
			found := false
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName && header[1] == fallbackHeaderValue {
					found = true
					break
				}
			}
			require.True(t, found, "Should set fallback header when no fallback.enabled config (default to enabled)")
		})

		// 场景2：各种不触发降级的场景验证
		t.Run("no fallback header when consumer disabled fallback", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled":           true,
					"fallback.enabled.consumer1": false, // consumer1 禁用 fallback
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 503 响应（满足 fallback 状态码条件）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "503"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证响应中不包含 fallback header，且状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should NOT set fallback header when consumer has disabled fallback")
			require.Equal(t, "503", statusCode, "Original error status code should be preserved")
		})

		t.Run("no fallback header on 404 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 404 响应（不在 fallback statusCodes 中）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "404"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证响应中不包含 fallback header，且状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should NOT set fallback header on 404 status")
			require.Equal(t, "404", statusCode, "Original error status code should be preserved")
		})

		t.Run("no fallback header on 401 status", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429", "5xx"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 401 响应（不在 fallback statusCodes 中）
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "401"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证响应中不包含 fallback header，且状态码被保留
			responseHeaders := host.GetResponseHeaders()
			found := false
			statusCode := ""
			for _, header := range responseHeaders {
				if header[0] == fallbackHeaderName {
					found = true
				}
				if header[0] == ":status" {
					statusCode = header[1]
				}
			}
			require.False(t, found, "Should NOT set fallback header on 401 status")
			require.Equal(t, "401", statusCode, "Original error status code should be preserved")
		})

		t.Run("verify response headers preserved when no fallback", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"model1"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"fallback": map[string]interface{}{
									"statusCodes": []string{"429"},
									"services": []map[string]interface{}{
										{"id": "fallback-service1"},
									},
								},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"fallback.enabled": false, // 全局禁用 fallback
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/chat/completions"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "model1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 模拟 429 响应，带有额外的响应头
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "429"},
				{"Content-Type", "application/json"},
				{"X-RateLimit-Limit", "100"},
				{"X-RateLimit-Remaining", "0"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证原始响应头被保留
			responseHeaders := host.GetResponseHeaders()
			headerMap := make(map[string]string)
			for _, header := range responseHeaders {
				headerMap[header[0]] = header[1]
			}

			// 验证没有 fallback header
			_, hasFallback := headerMap[fallbackHeaderName]
			require.False(t, hasFallback, "Should NOT have fallback header when disabled")

			// 验证原始响应头仍然存在
			require.Equal(t, "application/json", headerMap["content-type"])
			require.Equal(t, "100", headerMap["x-ratelimit-limit"])
			require.Equal(t, "0", headerMap["x-ratelimit-remaining"])
		})
	})
}
