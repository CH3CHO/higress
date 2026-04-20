// Copyright (c) 2024 Alibaba Group Holding Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/tidwall/gjson"
	"github.com/stretchr/testify/require"
)

// 测试配置：基本统计配置
var basicConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes": []map[string]interface{}{
			{
				"key":                   "request_id",
				"value_source":          "request_header",
				"value":                 "x-request-id",
				"apply_to_log":          true,
				"apply_to_span":         false,
				"as_separate_log_field": false,
			},
			{
				"key":                   "api_version",
				"value_source":          "fixed_value",
				"value":                 "v1",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
			{
				"key":                   "model",
				"value_source":          "request_body",
				"value":                 "model",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
			{
				"key":                   "input_token",
				"value_source":          "response_body",
				"value":                 "usage.prompt_tokens",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
			{
				"key":                   "output_token",
				"value_source":          "response_body",
				"value":                 "usage.completion_tokens",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
			{
				"key":                   "total_token",
				"value_source":          "response_body",
				"value":                 "usage.total_tokens",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
		},
		"disable_openai_usage": false,
	})
	return data
}()

// 测试配置：流式响应体属性配置
var streamingBodyConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes": []map[string]interface{}{
			{
				"key":                   "response_content",
				"value_source":          "response_streaming_body",
				"value":                 "choices.0.message.content",
				"rule":                  "first",
				"apply_to_log":          true,
				"apply_to_span":         false,
				"as_separate_log_field": false,
			},
			{
				"key":                   "model_name",
				"value_source":          "response_streaming_body",
				"value":                 "model",
				"rule":                  "replace",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
		},
		"disable_openai_usage": false,
	})
	return data
}()

// 测试配置：请求体属性配置
var requestBodyConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes": []map[string]interface{}{
			{
				"key":                   "user_message_count",
				"value_source":          "request_body",
				"value":                 "messages.#(role==\"user\")",
				"apply_to_log":          true,
				"apply_to_span":         false,
				"as_separate_log_field": false,
			},
			{
				"key":                   "request_model",
				"value_source":          "request_body",
				"value":                 "model",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
		},
		"disable_openai_usage": false,
	})
	return data
}()

// 测试配置：响应体属性配置
var responseBodyConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes": []map[string]interface{}{
			{
				"key":                   "response_status",
				"value_source":          "response_body",
				"value":                 "status",
				"apply_to_log":          true,
				"apply_to_span":         false,
				"as_separate_log_field": false,
			},
			{
				"key":                   "response_message",
				"value_source":          "response_body",
				"value":                 "message",
				"apply_to_log":          true,
				"apply_to_span":         true,
				"as_separate_log_field": false,
			},
		},
		"disable_openai_usage": false,
	})
	return data
}()

// 测试配置：禁用 OpenAI 使用统计
var disableOpenaiUsageConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes": []map[string]interface{}{
			{
				"key":                   "custom_attribute",
				"value_source":          "fixed_value",
				"value":                 "custom_value",
				"apply_to_log":          true,
				"apply_to_span":         false,
				"as_separate_log_field": false,
			},
		},
		"disable_openai_usage": true,
	})
	return data
}()

// 测试配置：空属性配置
var emptyAttributesConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"attributes":           []map[string]interface{}{},
		"disable_openai_usage": false,
	})
	return data
}()

func TestParseConfig(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		// 测试基本统计配置解析
		t.Run("basic config", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		// 测试流式响应体属性配置解析
		t.Run("streaming body config", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		// 测试请求体属性配置解析
		t.Run("request body config", func(t *testing.T) {
			host, status := test.NewTestHost(requestBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		// 测试响应体属性配置解析
		t.Run("response body config", func(t *testing.T) {
			host, status := test.NewTestHost(responseBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		// 测试禁用 OpenAI 使用统计配置解析
		t.Run("disable openai usage config", func(t *testing.T) {
			host, status := test.NewTestHost(disableOpenaiUsageConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		// 测试空属性配置解析
		t.Run("empty attributes config", func(t *testing.T) {
			host, status := test.NewTestHost(emptyAttributesConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})
	})
}

func TestOnHttpRequestHeaders(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试基本请求头处理
		t.Run("basic request headers", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-request-id", "req-123"},
				{"x-mse-consumer", "consumer1"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试包含 consumer 的请求头处理
		t.Run("request headers with consumer", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-request-id", "req-456"},
				{"x-mse-consumer", "consumer2"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试不包含 consumer 的请求头处理
		t.Run("request headers without consumer", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-request-id", "req-789"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})
	})
}

func TestOnHttpRequestBody(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试基本请求体处理
		t.Run("basic request body", func(t *testing.T) {
			host, status := test.NewTestHost(requestBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置请求体
			requestBody := []byte(`{
				"model": "gpt-3.5-turbo",
				"messages": [
					{"role": "user", "content": "Hello"},
					{"role": "assistant", "content": "Hi there"},
					{"role": "user", "content": "How are you?"}
				]
			}`)
			action := host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试 Google Gemini 格式的请求体处理
		t.Run("gemini request body", func(t *testing.T) {
			host, status := test.NewTestHost(requestBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/models/gemini-pro:generateContent"},
				{":method", "POST"},
			})

			// 设置请求体
			requestBody := []byte(`{
				"contents": [
					{"role": "user", "parts": [{"text": "Hello"}]},
					{"parts": [{"text": "Hi there"}]}
				]
			}`)
			action := host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试不包含消息的请求体处理
		t.Run("request body without messages", func(t *testing.T) {
			host, status := test.NewTestHost(requestBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置请求体
			requestBody := []byte(`{
				"model": "gpt-3.5-turbo",
				"temperature": 0.7
			}`)
			action := host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})
	})
}

func TestOnHttpResponseHeaders(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试基本响应头处理
		t.Run("basic response headers", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置响应头
			action := host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试流式响应头处理
		t.Run("streaming response headers", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置流式响应头
			action := host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})
	})
}

func TestOnHttpStreamingBody(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试流式响应体处理
		t.Run("streaming response body", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置流式响应头
			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})

			// 处理第一个流式块
			firstChunk := []byte(`data: {"choices":[{"message":{"content":"Hello"}}],"model":"gpt-3.5-turbo"}`)
			action := host.CallOnHttpStreamingResponseBody(firstChunk, false)

			result := host.GetResponseBody()
			require.Equal(t, firstChunk, result)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			// 处理最后一个流式块
			lastChunk := []byte(`data: {"choices":[{"message":{"content":"How can I help you?"}}],"model":"gpt-3.5-turbo"}`)
			action = host.CallOnHttpStreamingResponseBody(lastChunk, true)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result = host.GetResponseBody()
			require.Equal(t, lastChunk, result)

			host.CompleteHttp()
		})

		// 测试不包含 token 统计的流式响应体处理
		t.Run("streaming body without token usage", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置流式响应头
			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})

			// 处理流式响应体
			chunk := []byte(`data: {"message": "Hello world"}`)
			action := host.CallOnHttpStreamingResponseBody(chunk, true)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result := host.GetResponseBody()
			require.Equal(t, chunk, result)

			host.CompleteHttp()
		})
	})
}

func TestOnHttpResponseBody(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试基本响应体处理
		t.Run("basic response body", func(t *testing.T) {
			host, status := test.NewTestHost(responseBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置响应头
			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json"},
			})

			// 设置响应体
			responseBody := []byte(`{
				"status": "success",
				"message": "Hello, how can I help you?",
				"choices": [{"message": {"content": "Hello"}}],
				"usage": {"prompt_tokens": 10, "completion_tokens": 15, "total_tokens": 25},
				"model": "gpt-3.5-turbo"
			}`)
			action := host.CallOnHttpResponseBody(responseBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试不包含 token 统计的响应体处理
		t.Run("response body without token usage", func(t *testing.T) {
			host, status := test.NewTestHost(responseBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先设置请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
			})

			// 设置响应头
			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json"},
			})

			// 设置响应体
			responseBody := []byte(`{
				"status": "success",
				"message": "Hello world"
			}`)
			action := host.CallOnHttpResponseBody(responseBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})
	})
}

func TestMetrics(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试指标收集
		t.Run("test token usage metrics", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置路由和集群名称
			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			// 1. 处理请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-mse-consumer", "user1"},
			})

			// 2. 处理请求体
			requestBody := []byte(`{
				"model": "gpt-3.5-turbo",
				"messages": [{"role": "user", "content": "Hello"}]
			}`)
			host.CallOnHttpRequestBody(requestBody)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 3. 处理响应头（此步骤非常重要，必须通过正确的响应头来告知插件选择使用流式处理方式还是非流式处理方式）
			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json; charset=utf-8"},
			})

			// 4. 处理响应体
			responseBody := []byte(`{
				"choices": [{"message": {"content": "Hello, how can I help you?"}}],
				"usage": {"prompt_tokens": 5, "completion_tokens": 8, "total_tokens": 13},
				"model": "gpt-3.5-turbo"
			}`)
			host.CallOnHttpResponseBody(responseBody)

			// 5. 完成请求
			host.CompleteHttp()

			// 6. 验证指标值
			// 检查输入 token 指标
			inputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.user1.metric.input_token"
			inputTokenValue, err := host.GetCounterMetric(inputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(5), inputTokenValue)

			// 检查输出 token 指标
			outputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.user1.metric.output_token"
			outputTokenValue, err := host.GetCounterMetric(outputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(8), outputTokenValue)

			// 检查总 token 指标
			totalTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.user1.metric.total_token"
			totalTokenValue, err := host.GetCounterMetric(totalTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(13), totalTokenValue)

			// 检查服务时长指标
			serviceDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.user1.metric.llm_service_duration"
			serviceDurationValue, err := host.GetCounterMetric(serviceDurationMetric)
			require.NoError(t, err)
			require.Greater(t, serviceDurationValue, uint64(0))

			// 检查请求计数指标
			durationCountMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.user1.metric.llm_duration_count"
			durationCountValue, err := host.GetCounterMetric(durationCountMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(1), durationCountValue)
		})

		// 测试流式响应指标
		t.Run("test streaming metrics", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置路由和集群名称
			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			// 1. 处理请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-mse-consumer", "user2"},
			})

			// 2. 处理请求体
			requestBody := []byte(`{
				"model": "gpt-4",
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`)
			action := host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 3. 处理流式响应头
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 4. 处理流式响应体 - 添加 usage 信息
			firstChunk := []byte(`data: {"choices":[{"message":{"content":"Hello"}}],"model":"gpt-4","usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
			action = host.CallOnHttpStreamingResponseBody(firstChunk, false)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result := host.GetResponseBody()
			require.Equal(t, firstChunk, result)

			// 5. 处理最后一个流式块 - 添加 usage 信息
			lastChunk := []byte(`data: {"choices":[{"message":{"content":"How can I help you?"}}],"model":"gpt-4","usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`)
			action = host.CallOnHttpStreamingResponseBody(lastChunk, true)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result = host.GetResponseBody()
			require.Equal(t, lastChunk, result)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 6. 完成请求
			host.CompleteHttp()

			// 7. 验证流式响应指标
			// 检查首 token 延迟指标
			firstTokenDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.llm_first_token_duration"
			firstTokenDurationValue, err := host.GetCounterMetric(firstTokenDurationMetric)
			require.NoError(t, err)
			require.Greater(t, firstTokenDurationValue, uint64(0))

			// 检查流式请求计数指标
			streamDurationCountMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.llm_stream_duration_count"
			streamDurationCountValue, err := host.GetCounterMetric(streamDurationCountMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(1), streamDurationCountValue)

			// 检查服务时长指标
			serviceDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.llm_service_duration"
			serviceDurationValue, err := host.GetCounterMetric(serviceDurationMetric)
			require.NoError(t, err)
			require.Greater(t, serviceDurationValue, uint64(0))

			// 检查 token 指标
			inputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.input_token"
			inputTokenValue, err := host.GetCounterMetric(inputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(5), inputTokenValue)

			outputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.output_token"
			outputTokenValue, err := host.GetCounterMetric(outputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(8), outputTokenValue)

			totalTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.user2.metric.total_token"
			totalTokenValue, err := host.GetCounterMetric(totalTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(13), totalTokenValue)
		})

		t.Run("anthropic_messages_metrics", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/v1/messages"},
				{":method", "POST"},
				{"x-mse-consumer", "user3"},
			})

			requestBody := []byte(`{
				"model": "claude-sonnet-4-5",
				"messages": [{"role": "user", "content": "Hello"}],
				"max_tokens": 128
			}`)
			host.CallOnHttpRequestBody(requestBody)

			time.Sleep(10 * time.Millisecond)

			host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json; charset=utf-8"},
			})

			responseBody := []byte(`{
				"id": "msg_123",
				"type": "message",
				"role": "assistant",
				"model": "claude-sonnet-4-5",
				"content": [{"type": "text", "text": "Hello back"}],
				"usage": {"input_tokens": 5, "output_tokens": 8, "cache_creation_input_tokens": 2, "cache_read_input_tokens": 1}
			}`)
			host.CallOnHttpResponseBody(responseBody)
			host.CompleteHttp()

			inputTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5.consumer.user3.metric.input_token"
			inputTokenValue, err := host.GetCounterMetric(inputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(5), inputTokenValue)

			outputTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5.consumer.user3.metric.output_token"
			outputTokenValue, err := host.GetCounterMetric(outputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(8), outputTokenValue)

			totalTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5.consumer.user3.metric.total_token"
			totalTokenValue, err := host.GetCounterMetric(totalTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(16), totalTokenValue)
		})

		t.Run("anthropic_messages_streaming_metrics", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/messages"},
				{":method", "POST"},
				{"x-mse-consumer", "user4"},
			})

			requestBody := []byte(`{
				"model": "claude-sonnet-4-5",
				"messages": [{"role": "user", "content": "Hello"}],
				"stream": true,
				"max_tokens": 128
			}`)
			action := host.CallOnHttpRequestBody(requestBody)
			require.Equal(t, types.ActionContinue, action)

			time.Sleep(10 * time.Millisecond)

			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})
			require.Equal(t, types.ActionContinue, action)

			firstChunk := []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-4-5-20250929\",\"id\":\"msg_bdrk_01UiSs8fzPz4UzPCLBZur21X\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":23,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0,\"cache_creation\":{\"ephemeral_5m_input_tokens\":0,\"ephemeral_1h_input_tokens\":0},\"output_tokens\":2}}}\n\n")
			action = host.CallOnHttpStreamingResponseBody(firstChunk, false)
			require.Equal(t, types.ActionContinue, action)
			require.Equal(t, firstChunk, host.GetResponseBody())

			lastChunk := []byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":23,\"cache_creation_input_tokens\":0,\"cache_read_input_tokens\":0,\"output_tokens\":21}}\n\n")
			action = host.CallOnHttpStreamingResponseBody(lastChunk, true)
			require.Equal(t, types.ActionContinue, action)
			require.Equal(t, lastChunk, host.GetResponseBody())

			time.Sleep(10 * time.Millisecond)
			host.CompleteHttp()

			firstTokenDurationMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5-20250929.consumer.user4.metric.llm_first_token_duration"
			firstTokenDurationValue, err := host.GetCounterMetric(firstTokenDurationMetric)
			require.NoError(t, err)
			require.Greater(t, firstTokenDurationValue, uint64(0))

			streamDurationCountMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5-20250929.consumer.user4.metric.llm_stream_duration_count"
			streamDurationCountValue, err := host.GetCounterMetric(streamDurationCountMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(1), streamDurationCountValue)

			inputTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5-20250929.consumer.user4.metric.input_token"
			inputTokenValue, err := host.GetCounterMetric(inputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(23), inputTokenValue)

			outputTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5-20250929.consumer.user4.metric.output_token"
			outputTokenValue, err := host.GetCounterMetric(outputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(21), outputTokenValue)

			totalTokenMetric := "route.api-v1.upstream.cluster-1.model.claude-sonnet-4-5-20250929.consumer.user4.metric.total_token"
			totalTokenValue, err := host.GetCounterMetric(totalTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(44), totalTokenValue)
		})
	})
}

func TestCompleteFlow(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试完整的统计流程
		t.Run("complete statistics flow", func(t *testing.T) {
			host, status := test.NewTestHost(basicConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置路由和集群名称
			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			// 1. 处理请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-request-id", "req-123"},
				{"x-mse-consumer", "consumer1"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 2. 处理请求体
			requestBody := []byte(`{
				"model": "gpt-3.5-turbo",
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`)
			action = host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 3. 处理响应头
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 4. 处理响应体
			responseBody := []byte(`{
				"choices": [{"message": {"content": "Hello, how can I help you?"}}],
				"usage": {"prompt_tokens": 5, "completion_tokens": 8, "total_tokens": 13},
				"model": "gpt-3.5-turbo"
			}`)
			action = host.CallOnHttpResponseBody(responseBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 5. 完成请求
			host.CompleteHttp()

			// 6. 验证指标值
			// 检查输入 token 指标
			inputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.consumer1.metric.input_token"
			inputTokenValue, err := host.GetCounterMetric(inputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(5), inputTokenValue)

			// 检查输出 token 指标
			outputTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.consumer1.metric.output_token"
			outputTokenValue, err := host.GetCounterMetric(outputTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(8), outputTokenValue)

			// 检查总 token 指标
			totalTokenMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.consumer1.metric.total_token"
			totalTokenValue, err := host.GetCounterMetric(totalTokenMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(13), totalTokenValue)

			// 检查服务时长指标
			serviceDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.consumer1.metric.llm_service_duration"
			serviceDurationValue, err := host.GetCounterMetric(serviceDurationMetric)
			require.NoError(t, err)
			require.Greater(t, serviceDurationValue, uint64(0))

			// 检查请求计数指标
			durationCountMetric := "route.api-v1.upstream.cluster-1.model.gpt-3.5-turbo.consumer.consumer1.metric.llm_duration_count"
			durationCountValue, err := host.GetCounterMetric(durationCountMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(1), durationCountValue)
		})

		// 测试流式响应的完整流程
		t.Run("complete streaming flow", func(t *testing.T) {
			host, status := test.NewTestHost(streamingBodyConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置路由和集群名称
			host.SetRouteName("api-v1")
			host.SetClusterName("cluster-1")

			// 1. 处理请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/chat"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer2"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 2. 处理请求体
			requestBody := []byte(`{
				"model": "gpt-4",
				"messages": [
					{"role": "user", "content": "Hello"}
				]
			}`)
			action = host.CallOnHttpRequestBody(requestBody)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 3. 处理流式响应头
			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "text/event-stream"},
			})

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 4. 处理流式响应体 - 添加 usage 信息
			firstChunk := []byte(`data: {"choices":[{"message":{"content":"Hello"}}],"model":"gpt-4","usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
			action = host.CallOnHttpStreamingResponseBody(firstChunk, false)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result := host.GetResponseBody()
			require.Equal(t, firstChunk, result)

			// 5. 处理最后一个流式块 - 添加 usage 信息
			lastChunk := []byte(`data: {"choices":[{"message":{"content":"How can I help you?"}}],"model":"gpt-4","usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`)
			action = host.CallOnHttpStreamingResponseBody(lastChunk, true)

			// 应该返回原始数据
			require.Equal(t, types.ActionContinue, action)

			result = host.GetResponseBody()
			require.Equal(t, lastChunk, result)

			// 添加延迟，确保有足够的时间间隔来计算 llm_service_duration
			time.Sleep(10 * time.Millisecond)

			// 6. 完成请求
			host.CompleteHttp()

			// 7. 验证流式响应指标
			// 检查首 token 延迟指标
			firstTokenDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.consumer2.metric.llm_first_token_duration"
			firstTokenDurationValue, err := host.GetCounterMetric(firstTokenDurationMetric)
			require.NoError(t, err)
			require.Greater(t, firstTokenDurationValue, uint64(0))

			// 检查流式请求计数指标
			streamDurationCountMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.consumer2.metric.llm_stream_duration_count"
			streamDurationCountValue, err := host.GetCounterMetric(streamDurationCountMetric)
			require.NoError(t, err)
			require.Equal(t, uint64(1), streamDurationCountValue)

			// 检查服务时长指标
			serviceDurationMetric := "route.api-v1.upstream.cluster-1.model.gpt-4.consumer.consumer2.metric.llm_service_duration"
			serviceDurationValue, err := host.GetCounterMetric(serviceDurationMetric)
			require.NoError(t, err)
			require.Greater(t, serviceDurationValue, uint64(0))
		})
	})
}

func TestExtractStreamingBodyByJsonPath(t *testing.T) {
	type testCase struct {
		name     string
		events   []StreamEvent
		jsonPath string
		rule     string
		current  interface{}
		expect   interface{}
	}

	events := []StreamEvent{
		{Data: `{"val": "first"}`},
		{Data: `{"val": "second"}`},
		{Data: `{"val": "third"}`},
		{Data: `{"val": null}`},
		{Data: `{"another-val": "new"}`},
	}

	tests := []testCase{
		{
			name:     "RuleFirst with nil currentValue",
			events:   events,
			jsonPath: "val",
			rule:     RuleFirst,
			current:  nil,
			expect:   "first",
		},
		{
			name:     "RuleFirst with non-nil currentValue",
			events:   events,
			jsonPath: "val",
			rule:     RuleFirst,
			current:  "existing",
			expect:   "existing",
		},
		{
			name:     "RuleReplace (should get last non-null)",
			events:   events,
			jsonPath: "val",
			rule:     RuleReplace,
			current:  "existing",
			expect:   "third",
		},
		{
			name:     "RuleAppend",
			events:   events,
			jsonPath: "val",
			rule:     RuleAppend,
			current:  "",
			expect:   "firstsecondthird",
		},
		{
			name:     "Empty events returns currentValue",
			events:   nil,
			jsonPath: "val",
			rule:     RuleReplace,
			current:  "existing",
			expect:   "existing",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extractStreamingBodyByJsonPath(tc.events, tc.jsonPath, tc.rule, tc.current)
			require.Equal(t, tc.expect, got)
		})
	}
}

func TestCombineAnthropicStreamingEventForLog(t *testing.T) {
	body := ""
	events := []StreamEvent{
		{
			Event: "message_start",
			Data:  `{"type":"message_start","message":{"model":"claude-haiku-4-5-20251001","id":"msg_bdrk_01EF5Y5ZVQRtKNfh3kUW1NqZ","type":"message","role":"assistant","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":23,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":0},"output_tokens":8}}}`,
		},
		{
			Event: "content_block_start",
			Data:  `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		},
		{
			Event: "content_block_delta",
			Data:  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"我是Claude，一个由"}}`,
		},
		{
			Event: "content_block_delta",
			Data:  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Anthropic公"}}`,
		},
		{
			Event: "content_block_delta",
			Data:  `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"司开发的AI助手。"}}`,
		},
		{
			Event: "content_block_stop",
			Data:  `{"type":"content_block_stop","index":0}`,
		},
		{
			Event: "message_delta",
			Data:  `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"input_tokens":23,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":23}}`,
		},
		{
			Event: "message_stop",
			Data:  `{"type":"message_stop","amazon-bedrock-invocationMetrics":{"inputTokenCount":23,"outputTokenCount":20,"invocationLatency":3101,"firstByteLatency":2948}}`,
		},
	}

	for _, event := range events {
		body = combineAnthropicStreamingEventForLog(body, &event)
	}

	require.Equal(t, "msg_bdrk_01EF5Y5ZVQRtKNfh3kUW1NqZ", gjson.Get(body, "id").String())
	require.Equal(t, "claude-haiku-4-5-20251001", gjson.Get(body, "model").String())
	require.Equal(t, "message", gjson.Get(body, "type").String())
	require.Equal(t, "assistant", gjson.Get(body, "role").String())
	require.Equal(t, "我是Claude，一个由Anthropic公司开发的AI助手。", gjson.Get(body, "content.0.text").String())
	require.Equal(t, "text", gjson.Get(body, "content.0.type").String())
	require.Equal(t, "end_turn", gjson.Get(body, "stop_reason").String())
	require.True(t, gjson.Get(body, "stop_sequence").Exists())
	require.Equal(t, int64(23), gjson.Get(body, "usage.input_tokens").Int())
	require.Equal(t, int64(23), gjson.Get(body, "usage.output_tokens").Int())
	require.Equal(t, int64(3101), gjson.Get(body, "amazon-bedrock-invocationMetrics.invocationLatency").Int())
	require.Equal(t, int64(2948), gjson.Get(body, "amazon-bedrock-invocationMetrics.firstByteLatency").Int())
}
