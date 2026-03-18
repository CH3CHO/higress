package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestRouteConfig_FromJson(t *testing.T) {
	validJson := `{
		"id": "route1",
		"models": ["model1", "model2"],
		"services": [{"id": "service1", "weight": 10}],
		"ratelimit": {"rpm": 100},
		"fallback": {"statusCodes": ["500"], "services": [{"id": "fallback1"}]},
		"protocols": ["openai", "original"]
	}`

	validJsonWithDefaultProtocol := `{
		"id": "route1",
		"models": ["model1", "model2"],
		"services": [{"id": "service1", "weight": 10}],
		"ratelimit": {"rpm": 100},
		"fallback": {"statusCodes": ["500"], "services": [{"id": "fallback1"}]},
	}`

	invalidJson := `{"id": "route1"}`

	t.Run("valid json", func(t *testing.T) {
		c := &RouteConfig{}
		err := c.FromJson(gjson.Parse(validJson))
		assert.NoError(t, err)
		assert.Equal(t, "route1", c.ID)
		assert.Len(t, c.Models, 2)
		assert.Equal(t, "model1", c.Models[0])
		assert.Equal(t, "model2", c.Models[1])
		assert.Len(t, c.Protocols, 2)
		assert.Equal(t, ProtocolOpenAI, c.Protocols[0])
		assert.Equal(t, ProtocolOriginal, c.Protocols[1])
	})

	t.Run("valid json with default protocol", func(t *testing.T) {
		c := &RouteConfig{}
		err := c.FromJson(gjson.Parse(validJsonWithDefaultProtocol))
		assert.NoError(t, err)
		assert.Equal(t, "route1", c.ID)
		assert.Len(t, c.Models, 2)
		assert.Len(t, c.Protocols, 1)
		assert.Equal(t, ProtocolOpenAI, c.Protocols[0])
	})

	t.Run("invalid json", func(t *testing.T) {
		c := &RouteConfig{}
		err := c.FromJson(gjson.Parse(invalidJson))
		assert.Error(t, err)
	})
}

func TestRouteUpstreamConfig_SelectService(t *testing.T) {
	validJson := `[
		{"id": "service1", "weight": 10},
		{"id": "service2", "weight": 20}
	]`

	c := &RouteUpstreamConfig{}
	err := c.FromJson(gjson.Parse(validJson))
	assert.NoError(t, err)

	assert.Equal(t, 30, c.weightSum)

	service := c.SelectService()
	assert.NotNil(t, service)
}

func TestServiceConfig_FromJson(t *testing.T) {
	validJson := `{"id": "service1"}`
	invalidJson := `{"name": "service1"}`

	t.Run("valid json", func(t *testing.T) {
		c := &ServiceConfig{}
		err := c.FromJson(gjson.Parse(validJson))
		assert.NoError(t, err)
		assert.Equal(t, "service1", c.ID)
	})

	t.Run("invalid json", func(t *testing.T) {
		c := &ServiceConfig{}
		err := c.FromJson(gjson.Parse(invalidJson))
		assert.Error(t, err)
	})
}

func TestRatelimitConfig_Initialize(t *testing.T) {
	validJson := `{"rpm": 100, "tpm": 20000}`

	c := &RatelimitConfig{}
	err := c.FromJson(gjson.Parse(validJson))
	assert.NoError(t, err)

	c.Initialize("route1")

	assert.Equal(t, 100, c.RPM)
	assert.Equal(t, []byte{0x64, 0, 0, 0}, c.RPMData)
	assert.Equal(t, "higress-cluster-key-rate-limit:route1:global_threshold:100:60", string(c.RPMKey))
	assert.Equal(t, 20000, c.TPM)
	assert.Equal(t, []byte{0x20, 0x4e, 0, 0}, c.TPMData)
	assert.Equal(t, []byte("higress-token-ratelimit:route1:global_threshold:20000:60"), c.TPMKey)
}

func TestFallbackConfig_ShallFallback(t *testing.T) {
	validJson := `{
		"statusCodes": ["500", "4xx"],
		"services": [{"id": "fallback1"}]
	}`

	c := &FallbackConfig{}
	err := c.FromJson(gjson.Parse(validJson))
	assert.NoError(t, err)

	t.Run("exact match", func(t *testing.T) {
		assert.True(t, c.ShallFallback("500"))
	})

	t.Run("class match", func(t *testing.T) {
		assert.True(t, c.ShallFallback("404"))
		assert.True(t, c.ShallFallback("429"))
	})

	t.Run("no match", func(t *testing.T) {
		assert.False(t, c.ShallFallback("200"))
	})
}
