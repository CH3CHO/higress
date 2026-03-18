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
