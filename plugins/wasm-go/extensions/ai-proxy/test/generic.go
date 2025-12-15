package test

import (
	"encoding/json"
	"testing"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/test"
	"github.com/stretchr/testify/require"
)

var basicGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com/v1",
				"genericAuthType":   "BEARER",
			},
		},
		"activeProviderId": "g",
		"routePathPrefix":  "/ai/generic",
	})
	return data
}()

var withTrailingSlashGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com/v1/",
				"genericAuthType":   "BEARER",
			},
		},
		"activeProviderId": "g",
		"routePathPrefix":  "/ai/generic",
	})
	return data
}()

var rootGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com",
				"genericAuthType":   "BEARER",
			},
		},
		"activeProviderId": "g",
		"routePathPrefix":  "/ai/generic",
	})
	return data
}()

var noAuthGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com/v1/",
				"genericAuthType":   "NONE",
			},
		},
		"activeProviderId": "g",
		"routePathPrefix":  "/ai/generic",
	})
	return data
}()

var rootRewriteGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com/v1",
				"genericAuthType":   "NONE",
			},
		},
		"activeProviderId": "g",
		"routePathPrefix":  "/",
	})
	return data
}()

var noRewriteGenericConfig = func() json.RawMessage {
	data, _ := json.Marshal(map[string]interface{}{
		"providers": []map[string]interface{}{
			{
				"id":       "g",
				"type":     "generic",
				"protocol": "original",
				"apiTokens": []string{
					"sk-generic-test123456789",
				},
				"genericServiceUrl": "http://generic-llm-service.example.com/v1/",
				"genericAuthType":   "NONE",
			},
		},
		"activeProviderId": "g",
	})
	return data
}()

func RunGenericParseConfigTests(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		t.Run("basic generic config", func(t *testing.T) {
			host, status := test.NewTestHost(basicGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		t.Run("with trailing slash generic config", func(t *testing.T) {
			host, status := test.NewTestHost(withTrailingSlashGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		t.Run("root generic config", func(t *testing.T) {
			host, status := test.NewTestHost(rootGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		t.Run("no-auth generic config", func(t *testing.T) {
			host, status := test.NewTestHost(noAuthGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		t.Run("root rewrite generic config", func(t *testing.T) {
			host, status := test.NewTestHost(rootRewriteGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})

		t.Run("no rewrite generic config", func(t *testing.T) {
			host, status := test.NewTestHost(noRewriteGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			config, err := host.GetMatchConfig()
			require.NoError(t, err)
			require.NotNil(t, config)
		})
	})
}

func RunGenericOnHttpRequestHeadersTests(t *testing.T) {
	test.RunGoTest(t, func(t *testing.T) {
		t.Run("basic generic config", func(t *testing.T) {
			host, status := test.NewTestHost(basicGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/v1/text/generations", pathValue, "Path should be rewritten correctly")

			apiKeyValue, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.True(t, hasApiKey, "Authorization header should exist")
			require.Equal(t, "Bearer sk-generic-test123456789", apiKeyValue, "Authorization should contain API token")
		})

		t.Run("with trailing slash generic config", func(t *testing.T) {
			host, status := test.NewTestHost(withTrailingSlashGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/v1/text/generations", pathValue, "Path should be rewritten correctly")

			apiKeyValue, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.True(t, hasApiKey, "Authorization header should exist")
			require.Equal(t, "Bearer sk-generic-test123456789", apiKeyValue, "Authorization should contain API token")
		})

		t.Run("root generic config", func(t *testing.T) {
			host, status := test.NewTestHost(rootGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/text/generations", pathValue, "Path should be rewritten correctly")

			apiKeyValue, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.True(t, hasApiKey, "Authorization header should exist")
			require.Equal(t, "Bearer sk-generic-test123456789", apiKeyValue, "Authorization should contain API token")
		})

		t.Run("no-auth generic config", func(t *testing.T) {
			host, status := test.NewTestHost(noAuthGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/v1/text/generations", pathValue, "Path should be rewritten correctly")

			_, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.False(t, hasApiKey, "Authorization header should not exist")
		})

		t.Run("root rewrite generic config", func(t *testing.T) {
			host, status := test.NewTestHost(rootRewriteGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/ai/generic/text/generations", pathValue, "Path should be rewritten correctly")

			_, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.False(t, hasApiKey, "Authorization header should not exist")
		})

		t.Run("no rewrite generic config", func(t *testing.T) {
			host, status := test.NewTestHost(rootRewriteGenericConfig)
			defer host.Reset()
			require.Equal(t, types.OnPluginStartStatusOK, status)

			action := host.CallOnHttpRequestHeaders([][2]string{
				{":authority", "example.com"},
				{":path", "/ai/generic/text/generations"},
				{":method", "POST"},
				{"Content-Type", "application/json"},
			})
			require.Equal(t, types.HeaderStopIteration, action)

			requestHeaders := host.GetRequestHeaders()
			require.NotNil(t, requestHeaders)

			hostValue, hasHost := test.GetHeaderValue(requestHeaders, ":authority")
			require.True(t, hasHost, "Host header should exist")
			require.Equal(t, "generic-llm-service.example.com", hostValue, "Host should be changed to generic service domain")

			pathValue, hasPath := test.GetHeaderValue(requestHeaders, ":path")
			require.True(t, hasPath, "Path header should exist")
			require.Equal(t, "/ai/generic/text/generations", pathValue, "Path should be rewritten correctly")

			_, hasApiKey := test.GetHeaderValue(requestHeaders, "Authorization")
			require.False(t, hasApiKey, "Authorization header should not exist")
		})
	})
}
