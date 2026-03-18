package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestConsumerConfig_FromJson(t *testing.T) {
	validJson := `{
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
				]
			}
		]
	}`

	invalidJson := `{
		"routes": "not an array"
	}`

	missingIdJson := `{
		"routes": []
	}`

	t.Run("valid json", func(t *testing.T) {
		c := &ConsumerConfig{}
		err := c.FromJson(gjson.Parse(validJson))
		assert.NoError(t, err)
		assert.Equal(t, "consumer1", c.ID)
		assert.Len(t, c.ModelRoutes, 2)

		// Validate parsed route content
		route, exists := c.ModelRoutes["model1"]
		assert.True(t, exists)
		assert.Equal(t, "route1", route.ID)
		assert.Len(t, route.Models, 2)
		assert.Equal(t, "model1", route.Models[0])
		assert.Equal(t, "model2", route.Models[1])
		assert.NotNil(t, route.Upstream)
		assert.Len(t, route.Upstream.Services, 1)
		assert.Equal(t, "service1", route.Upstream.Services[0].ID)
		assert.Equal(t, 100, route.Upstream.Services[0].Weight)
	})

	t.Run("invalid routes", func(t *testing.T) {
		c := &ConsumerConfig{}
		err := c.FromJson(gjson.Parse(invalidJson))
		assert.Error(t, err)
	})

	t.Run("missing id", func(t *testing.T) {
		c := &ConsumerConfig{}
		err := c.FromJson(gjson.Parse(missingIdJson))
		assert.Error(t, err)
	})
}

func TestConsumerConfig_GetRouteConfig(t *testing.T) {
	c := &ConsumerConfig{
		ModelRoutes: map[string]*RouteConfig{
			"model1": {ID: "route1"},
		},
	}

	t.Run("existing model", func(t *testing.T) {
		route := c.GetRouteConfig("model1")
		assert.NotNil(t, route)
		assert.Equal(t, "route1", route.ID)
	})

	t.Run("non-existing model", func(t *testing.T) {
		route := c.GetRouteConfig("model2")
		assert.Nil(t, route)
	})
}
