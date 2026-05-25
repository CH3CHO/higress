package main

import (
	"encoding/json"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"
)

// TestBuildModelsListResponse 测试 buildModelsListResponse 函数
func TestBuildModelsListResponse(t *testing.T) {
	t.Run("empty models list", func(t *testing.T) {
		models := []string{}
		result := buildModelsListResponse(models)

		require.Equal(t, "list", result.Object)
		require.Empty(t, result.Data)
	})

	t.Run("single model", func(t *testing.T) {
		models := []string{"gpt-4"}
		result := buildModelsListResponse(models)

		require.Equal(t, "list", result.Object)
		require.Len(t, result.Data, 1)
		require.Equal(t, "gpt-4", result.Data[0].ID)
		require.Equal(t, "model", result.Data[0].Object)
		require.Equal(t, int64(modelsListCreatedAt), result.Data[0].Created)
		require.Equal(t, "system", result.Data[0].OwnedBy)
	})

	t.Run("multiple models", func(t *testing.T) {
		models := []string{"gpt-4", "gpt-3.5-turbo", "text-embedding-ada-002"}
		result := buildModelsListResponse(models)

		require.Equal(t, "list", result.Object)
		require.Len(t, result.Data, 3)

		modelIDs := make([]string, len(result.Data))
		for i, m := range result.Data {
			modelIDs[i] = m.ID
			require.Equal(t, "model", m.Object)
			require.Equal(t, int64(modelsListCreatedAt), m.Created)
			require.Equal(t, "system", m.OwnedBy)
		}
		require.ElementsMatch(t, models, modelIDs)
	})
}

// TestModelsListEndpoint 测试 /v1/models 端点
func TestModelsListEndpoint(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		t.Run("models list endpoint enabled - returns local response", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4", "gpt-3.5-turbo"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
							{
								"id":     "route2",
								"models": []string{"text-embedding-ada-002"},
								"services": []map[string]interface{}{
									{"id": "service2", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证返回了本地响应
			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return local response for models list endpoint")
			require.Equal(t, uint32(200), localResponse.StatusCode)
			require.Equal(t, [][2]string{{"content-type", "application/json"}}, localResponse.Headers)

			var result ModelsListResponse
			err := json.Unmarshal(localResponse.Data, &result)
			require.NoError(t, err)
			require.Equal(t, "list", result.Object)
			require.Len(t, result.Data, 3)

			modelIDs := make([]string, len(result.Data))
			for i, m := range result.Data {
				modelIDs[i] = m.ID
			}
			require.ElementsMatch(t, []string{"gpt-4", "gpt-3.5-turbo", "text-embedding-ada-002"}, modelIDs)
		})

		t.Run("models list endpoint enabled with /models path - returns local response", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4", "gpt-3.5-turbo"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证返回了本地响应
			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return local response for /models path")
			require.Equal(t, uint32(200), localResponse.StatusCode)
			require.Equal(t, [][2]string{{"content-type", "application/json"}}, localResponse.Headers)

			var result ModelsListResponse
			err := json.Unmarshal(localResponse.Data, &result)
			require.NoError(t, err)
			require.Equal(t, "list", result.Object)
			require.Len(t, result.Data, 2)
		})

		t.Run("models list endpoint disabled - returns error", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": false,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证返回了 400 错误响应（models list 被禁用）
			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return error response when models list is disabled")
			require.Equal(t, uint32(400), localResponse.StatusCode)
			require.Equal(t, "trip-llm-router.models-not-enabled", localResponse.StatusCodeDetail)
		})

		t.Run("models list endpoint not matching path - passes to backend", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
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
				{"x-higress-llm-model", "gpt-4"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有返回本地响应，请求继续转发到 chat completions
			localResponse := host.GetLocalResponse()
			require.Nil(t, localResponse, "Should not return local response for non-models endpoint")
		})

		t.Run("models list endpoint original protocol - passes to backend", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai", "original"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 使用 original 协议路径（包含 /raw/），需要提供 model header 才能继续转发
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/raw/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
				{"x-higress-llm-model", "gpt-4"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 验证没有返回本地响应（original 协议不支持 models list 自定义响应）
			localResponse := host.GetLocalResponse()
			require.Nil(t, localResponse, "Should not return local response for original protocol")
		})

		t.Run("models list endpoint enabled by consumer", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
					{
						"id": "consumer2",
						"routes": []map[string]interface{}{
							{
								"id":     "route2",
								"models": []string{"gpt-3.5-turbo"},
								"services": []map[string]interface{}{
									{"id": "service2", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": false,
					"models-list.enabled.consumer.consumer1": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// consumer1 启用
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return local response for consumer1")
			require.Equal(t, uint32(200), localResponse.StatusCode)

			// consumer2 未启用（使用全局默认值 false），应返回 400 错误
			host.Reset()
			host, _ = test.NewTestHost(pluginConfig)
			action = host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer2/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer2"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse = host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return error response for consumer2 when models list is disabled")
			require.Equal(t, uint32(400), localResponse.StatusCode)
			require.Equal(t, "trip-llm-router.models-not-enabled", localResponse.StatusCodeDetail)
		})

		t.Run("models list endpoint case insensitive - uppercase path works", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 测试大写路径 /V1/MODELS
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/V1/MODELS"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return local response for uppercase /V1/MODELS path")
			require.Equal(t, uint32(200), localResponse.StatusCode)

			// 测试大小写混合路径 /Models
			host.Reset()
			host, _ = test.NewTestHost(pluginConfig)
			action = host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer1/Models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer1"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse = host.GetLocalResponse()
			require.NotNil(t, localResponse, "Should return local response for mixed case /Models path")
			require.Equal(t, uint32(200), localResponse.StatusCode)
		})

		t.Run("models list endpoint unknown consumer", func(t *testing.T) {
			pluginConfig, _ := json.Marshal(map[string]interface{}{
				"consumers": []map[string]interface{}{
					{
						"id": "consumer1",
						"routes": []map[string]interface{}{
							{
								"id":     "route1",
								"models": []string{"gpt-4"},
								"services": []map[string]interface{}{
									{"id": "service1", "weight": 100},
								},
								"protocols": []string{"openai"},
							},
						},
					},
				},
				"_config_": map[string]interface{}{
					"models-list.enabled.global": true,
				},
			})
			host, status := test.NewTestHost(pluginConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/llm/consumer2/v1/models"},
				{":method", "GET"},
				{"Content-Type", "application/json"},
				{"x-mse-consumer", "consumer2"},
			})
			require.Equal(t, types.ActionContinue, action)

			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse)
			require.Equal(t, uint32(403), localResponse.StatusCode)
			require.Equal(t, "trip-llm-router.unknown-consumer", localResponse.StatusCodeDetail)
		})
	})
}
