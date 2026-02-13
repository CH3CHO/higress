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
	"fmt"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"

	"ai-token-ratelimit/config"
	"ai-token-ratelimit/util"
)

// 测试配置：全局限流配置
var globalThresholdConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-global-limit",
		"global_threshold": map[string]interface{}{
			"token_per_minute": 1000,
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
			"timeout":      1000,
		},
		"rejected_code": 429,
		"rejected_msg":  "Too many AI token requests",
	})
	return data
}()

// 测试配置：基于请求头的限流配置
var headerLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-header-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_header": "x-api-key",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "test-key-123",
						"token_per_minute": 100,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "API key rate limit exceeded",
	})
	return data
}()

// 测试配置：基于请求参数的限流配置
var paramLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-param-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_param": "apikey",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "param-key-456",
						"token_per_minute": 50,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "Parameter rate limit exceeded",
	})
	return data
}()

// 测试配置：基于 Consumer 的限流配置
var consumerLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-consumer-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_consumer": "",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "consumer1",
						"token_per_minute": 200,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "Consumer rate limit exceeded",
	})
	return data
}()

// 测试配置：基于 Cookie 的限流配置
var cookieLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-cookie-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_cookie": "session-id",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "session-789",
						"token_per_minute": 75,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "Session rate limit exceeded",
	})
	return data
}()

// 测试配置：基于 IP 的限流配置
var ipLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-ip-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_per_ip": "from-remote-addr",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "192.168.1.0/24",
						"token_per_minute": 300,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "IP rate limit exceeded",
	})
	return data
}()

// 测试配置：正则表达式限流配置
var regexpLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-regexp-limit",
		"rule_items": []map[string]interface{}{
			{
				"limit_by_per_header": "x-user-id",
				"limit_keys": []map[string]interface{}{
					{
						"key":              "regexp:^user-\\d+$",
						"token_per_minute": 150,
					},
				},
			},
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
		},
		"rejected_code": 429,
		"rejected_msg":  "User ID rate limit exceeded",
	})
	return data
}()

// 测试配置：全局路由混合限流配置
var globalRouteMixedLimitConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-global-limit",
		"global_threshold": map[string]interface{}{
			"token_per_minute": 1000,
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
			"timeout":      1000,
		},
		"rejected_code": 429,
		"rejected_msg":  "Too many AI token requests",
		"_rules_": []map[string]interface{}{
			{
				"_match_route_": "routeA",
				"rule_name":     "routeA-ai-token-limit",
				"global_threshold": map[string]interface{}{
					"token_per_minute": 2000,
				},
				"rejected_code": 430,
				"rejected_msg":  "Too many AI token requests for route A",
			},
		},
	})
	return data
}()

// 测试配置：全局路由混合限流配置，路由配置覆盖 Redis 配置
var globalRouteMixedLimitWithRedisConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"rule_name": "ai-token-global-limit",
		"global_threshold": map[string]interface{}{
			"token_per_minute": 1000,
		},
		"redis": map[string]interface{}{
			"service_name": "redis.static",
			"service_port": 6379,
			"timeout":      1000,
		},
		"rejected_code": 429,
		"rejected_msg":  "Too many AI token requests",
		"_rules_": []map[string]interface{}{
			{
				"_match_route_": "routeA",
				"rule_name":     "routeA-ai-token-limit",
				"global_threshold": map[string]interface{}{
					"token_per_minute": 2000,
				},
				"rejected_code": 430,
				"rejected_msg":  "Too many AI token requests for route A",
				"redis": map[string]interface{}{
					"service_name": "redis-2.static",
					"service_port": 16379,
					"timeout":      2000,
				},
			},
		},
	})
	return data
}()

func TestParseConfig(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		// 测试全局限流配置解析
		t.Run("global threshold config", func(t *testing.T) {
			host, status := test.NewTestHost(globalThresholdConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-global-limit", parsedConfig.RuleName)
			require.NotNil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, int64(1000), parsedConfig.GlobalThreshold.Count)
			require.Equal(t, int64(60), parsedConfig.GlobalThreshold.TimeWindow)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Too many AI token requests", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Nil(t, parsedConfig.RuleItems)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试基于请求头的限流配置解析
		t.Run("header limit config", func(t *testing.T) {
			host, status := test.NewTestHost(headerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-header-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "API key rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByHeaderType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, "x-api-key", parsedConfig.RuleItems[0].Key)
			require.Len(t, parsedConfig.RuleItems[0].ConfigItems, 1)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.ExactType, item.ConfigType)
			require.Equal(t, "test-key-123", item.Key)
			require.Equal(t, int64(100), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试基于请求参数的限流配置解析
		t.Run("param limit config", func(t *testing.T) {
			host, status := test.NewTestHost(paramLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-param-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Parameter rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByParamType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, "apikey", parsedConfig.RuleItems[0].Key)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.ExactType, item.ConfigType)
			require.Equal(t, "param-key-456", item.Key)
			require.Equal(t, int64(50), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试基于 Consumer 的限流配置解析
		t.Run("consumer limit config", func(t *testing.T) {
			host, status := test.NewTestHost(consumerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-consumer-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Consumer rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByConsumerType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, util.ConsumerHeader, parsedConfig.RuleItems[0].Key)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.ExactType, item.ConfigType)
			require.Equal(t, "consumer1", item.Key)
			require.Equal(t, int64(200), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试基于 Cookie 的限流配置解析
		t.Run("cookie limit config", func(t *testing.T) {
			host, status := test.NewTestHost(cookieLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-cookie-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Session rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByCookieType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, "session-id", parsedConfig.RuleItems[0].Key)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.ExactType, item.ConfigType)
			require.Equal(t, "session-789", item.Key)
			require.Equal(t, int64(75), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试基于 IP 的限流配置解析
		t.Run("ip limit config", func(t *testing.T) {
			host, status := test.NewTestHost(ipLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-ip-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "IP rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByPerIpType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, "from-remote-addr", parsedConfig.RuleItems[0].Key)
			require.Equal(t, config.RemoteAddrSourceType, parsedConfig.RuleItems[0].LimitByPerIp.SourceType)
			require.Equal(t, "", parsedConfig.RuleItems[0].LimitByPerIp.HeaderName)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.IpNetType, item.ConfigType)
			require.Equal(t, "192.168.1.0/24", item.Key)
			require.NotNil(t, item.IpNet)
			require.Equal(t, int64(300), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试正则表达式限流配置解析
		t.Run("regexp limit config", func(t *testing.T) {
			host, status := test.NewTestHost(regexpLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			rawCfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, rawCfg)
			parsedConfig := rawCfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-regexp-limit", parsedConfig.RuleName)
			require.Nil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "User ID rate limit exceeded", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Len(t, parsedConfig.RuleItems, 1)
			require.Equal(t, config.LimitByPerHeaderType, parsedConfig.RuleItems[0].LimitType)
			require.Equal(t, "x-user-id", parsedConfig.RuleItems[0].Key)
			item := parsedConfig.RuleItems[0].ConfigItems[0]
			require.Equal(t, config.RegexpType, item.ConfigType)
			require.Equal(t, "regexp:^user-\\d+$", item.Key)
			require.NotNil(t, item.Regexp)
			require.Equal(t, int64(150), item.Count)
			require.Equal(t, int64(60), item.TimeWindow)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
		})

		// 测试全局配置和路由配置混合解析
		t.Run("global+route mixed threshold config", func(t *testing.T) {
			host, status := test.NewTestHost(globalRouteMixedLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			cfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg)

			// 验证配置内容
			parsedConfig := cfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-global-limit", parsedConfig.RuleName)
			require.NotNil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, int64(1000), parsedConfig.GlobalThreshold.Count)
			require.Equal(t, int64(60), parsedConfig.GlobalThreshold.TimeWindow)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Too many AI token requests", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			globalConfig := parsedConfig

			err = host.SetRouteName("routeA")
			require.NoError(t, err)

			cfg, err = host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg)

			// 验证配置内容
			parsedConfig = cfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "routeA-ai-token-limit", parsedConfig.RuleName)
			require.NotNil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, int64(2000), parsedConfig.GlobalThreshold.Count)
			require.Equal(t, int64(60), parsedConfig.GlobalThreshold.TimeWindow)
			require.Equal(t, uint32(430), parsedConfig.RejectedCode)
			require.Equal(t, "Too many AI token requests for route A", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.Equal(t, globalConfig.RedisClient, parsedConfig.RedisClient)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
			require.NotSame(t, globalConfig.CounterMetrics, parsedConfig.CounterMetrics)
		})

		// 测试全局配置和路由配置混合解析
		t.Run("global+route mixed with redis threshold config", func(t *testing.T) {
			host, status := test.NewTestHost(globalRouteMixedLimitWithRedisConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			cfg, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg)

			// 验证配置内容
			parsedConfig := cfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "ai-token-global-limit", parsedConfig.RuleName)
			require.NotNil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, int64(1000), parsedConfig.GlobalThreshold.Count)
			require.Equal(t, int64(60), parsedConfig.GlobalThreshold.TimeWindow)
			require.Equal(t, uint32(429), parsedConfig.RejectedCode)
			require.Equal(t, "Too many AI token requests", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			globalConfig := parsedConfig

			err = host.SetRouteName("routeA")
			require.NoError(t, err)

			cfg, err = host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg)

			// 验证配置内容
			parsedConfig = cfg.(*config.AiTokenRateLimitConfig)
			require.Equal(t, "routeA-ai-token-limit", parsedConfig.RuleName)
			require.NotNil(t, parsedConfig.GlobalThreshold)
			require.Equal(t, int64(2000), parsedConfig.GlobalThreshold.Count)
			require.Equal(t, int64(60), parsedConfig.GlobalThreshold.TimeWindow)
			require.Equal(t, uint32(430), parsedConfig.RejectedCode)
			require.Equal(t, "Too many AI token requests for route A", parsedConfig.RejectedMsg)
			require.NotNil(t, parsedConfig.RedisClient)
			require.NotEqual(t, globalConfig.RedisClient, parsedConfig.RedisClient)
			require.NotNil(t, parsedConfig.CounterMetrics)
			require.Equal(t, 0, len(parsedConfig.CounterMetrics))
			require.NotSame(t, globalConfig.CounterMetrics, parsedConfig.CounterMetrics)
		})
	})
}

func TestOnHttpRequestHeaders(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试全局限流请求头处理
		t.Run("global threshold request headers", func(t *testing.T) {
			host, status := test.NewTestHost(globalThresholdConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（允许请求）
			// 返回 [count, remaining, ttl] 格式
			resp := test.CreateRedisRespArray([]interface{}{1000, 999, 60})
			host.CallOnRedisCall(0, resp)

			host.CompleteHttp()
		})

		// 测试基于请求头的限流请求头处理
		t.Run("header limit request headers", func(t *testing.T) {
			host, status := test.NewTestHost(headerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头，包含限流键
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
				{"x-api-key", "test-key-123"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（允许请求）
			resp := test.CreateRedisRespArray([]interface{}{100, 99, 60})
			host.CallOnRedisCall(0, resp)

			host.CompleteHttp()
		})

		// 测试基于请求参数的限流请求头处理
		t.Run("param limit request headers", func(t *testing.T) {
			host, status := test.NewTestHost(paramLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头，包含查询参数
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test?apikey=param-key-456"},
				{":method", "POST"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（允许请求）
			resp := test.CreateRedisRespArray([]interface{}{50, 49, 60})
			host.CallOnRedisCall(0, resp)

			host.CompleteHttp()
		})

		// 测试基于 Consumer 的限流请求头处理
		t.Run("consumer limit request headers", func(t *testing.T) {
			host, status := test.NewTestHost(consumerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头，包含 consumer 信息
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
				{"x-mse-consumer", "consumer1"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（允许请求）
			resp := test.CreateRedisRespArray([]interface{}{200, 199, 60})
			host.CallOnRedisCall(0, resp)

			host.CompleteHttp()
		})

		// 测试基于 Cookie 的限流请求头处理
		t.Run("cookie limit request headers", func(t *testing.T) {
			host, status := test.NewTestHost(cookieLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头，包含 cookie
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
				{"cookie", "session-id=session-789; other=value"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（允许请求）
			resp := test.CreateRedisRespArray([]interface{}{75, 74, 60})
			host.CallOnRedisCall(0, resp)

			host.CompleteHttp()
		})

		// 测试限流触发的情况
		t.Run("rate limit exceeded", func(t *testing.T) {
			host, status := test.NewTestHost(globalThresholdConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 模拟 Redis 调用响应（触发限流）
			// 返回 [count, remaining, ttl] 格式，remaining < 0 表示触发限流
			resp := test.CreateRedisRespArray([]interface{}{1000, -1, 60})
			host.CallOnRedisCall(0, resp)

			// 检查是否发送了限流响应
			localResponse := host.GetLocalResponse()
			require.NotNil(t, localResponse)
			require.Equal(t, uint32(429), localResponse.StatusCode)
			require.Contains(t, string(localResponse.Data), "Too many AI token requests")

			host.CompleteHttp()
		})

		// 测试没有匹配到限流规则的情况
		t.Run("no matching limit rule", func(t *testing.T) {
			host, status := test.NewTestHost(headerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 设置请求头，但不包含限流键
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
				// 不包含 x-api-key 头
			})

			// 应该返回 ActionContinue，因为没有匹配到限流规则
			require.Equal(t, types.ActionContinue, action)
		})
	})
}

func TestOnHttpStreamingBody(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试流式响应体处理（包含 token 统计）
		t.Run("streaming body with token usage", func(t *testing.T) {
			host, status := test.NewTestHost(globalThresholdConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先处理请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
			})

			// 模拟 Redis 调用响应
			resp := test.CreateRedisRespArray([]interface{}{1000, 999, 60})
			host.CallOnRedisCall(0, resp)

			// 处理流式响应体
			// 模拟包含 token 统计信息的响应体
			responseBody := []byte(`{"choices":[{"message":{"content":"Hello, how can I help you?"}}],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`)
			action := host.CallOnHttpStreamingRequestBody(responseBody, false) // 不是最后一个块

			result := host.GetRequestBody()
			require.Equal(t, responseBody, result)
			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			// 处理最后一个块
			lastChunk := []byte(`{"choices":[{"message":{"content":"How can I help you?"}}],"usage":{"prompt_tokens":10,"completion_tokens":15,"total_tokens":25}}`)
			action = host.CallOnHttpStreamingRequestBody(lastChunk, true) // 最后一个块

			result = host.GetRequestBody()
			require.Equal(t, lastChunk, result)

			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})

		// 测试流式响应体处理（不包含 token 统计）
		t.Run("streaming body without token usage", func(t *testing.T) {
			host, status := test.NewTestHost(globalThresholdConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 先处理请求头
			host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
			})

			// 模拟 Redis 调用响应
			resp := test.CreateRedisRespArray([]interface{}{1000, 999, 60})
			host.CallOnRedisCall(0, resp)

			// 处理流式响应体
			// 模拟不包含 token 统计信息的响应体
			responseBody := []byte(`{"message": "Hello, world!"}`)
			action := host.CallOnHttpStreamingRequestBody(responseBody, true) // 最后一个块

			result := host.GetRequestBody()
			require.Equal(t, responseBody, result)
			// 应该返回 ActionContinue
			require.Equal(t, types.ActionContinue, action)

			host.CompleteHttp()
		})
	})
}

func TestCompleteFlow(t *testing.T) {
	test.RunTest(t, func(t *testing.T) {
		// 测试完整的限流流程
		t.Run("complete rate limit flow", func(t *testing.T) {
			host, status := test.NewTestHost(headerLimitConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			// 1. 处理请求头
			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/api/test"},
				{":method", "POST"},
				{"x-api-key", "test-key-123"},
			})

			// 由于需要调用 Redis，应该返回 HeaderStopAllIterationAndWatermark
			require.Equal(t, types.HeaderStopAllIterationAndWatermark, action)

			// 2. 模拟 Redis 调用响应
			resp := test.CreateRedisRespArray([]interface{}{100, 99, 60})
			host.CallOnRedisCall(0, resp)

			action = host.CallOnHttpResponseHeaders([][2]string{
				{":status", "200"},
				{"content-type", "application/json"},
			})
			require.Equal(t, types.ActionContinue, action)

			// 3. 处理流式响应体
			responseBody := []byte(`{"choices":[{"message":{"content":"AI response"}}],"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`)
			responseBodyLength := len(responseBody)
			bufferedResponseBody := make([]byte, 0)
			for i := 0; i < responseBodyLength; {
				end := i + 1
				if end > responseBodyLength {
					end = responseBodyLength
				}
				chunk := responseBody[i:end]
				isLast := end == responseBodyLength
				action = host.CallOnHttpStreamingResponseBody(chunk, isLast)
				if action != types.ActionContinue {
					// 读取到 usage 数据后需要返回 DataStopIterationNoBuffer 等待 Redis 调用完成
					require.Equal(t, types.DataStopIterationNoBuffer, action)
					bufferedResponseBody = append(bufferedResponseBody, chunk...)
				}
				i = end
			}

			// 此时应该返回 DataStopIterationNoBuffer，等待 Redis 调用完成
			require.Equal(t, types.DataStopIterationNoBuffer, action)

			// 模拟 Redis 调用响应，更新 token 统计
			host.CallOnRedisCall(0, resp)

			// 继续处理剩余的响应体
			require.Equal(t, types.ActionContinue, host.GetHttpStreamAction())

			// 验证最终的响应体内容，即缓存的响应体
			result := host.GetResponseBody()
			fmt.Println("Final response body:", string(result))
			require.Equal(t, bufferedResponseBody, result)

			// 4. 完成请求
			host.CompleteHttp()
		})
	})
}
