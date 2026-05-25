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

func TestConsumerConfig_GetAvailableModels(t *testing.T) {
	t.Run("all models without protocol filter", func(t *testing.T) {
		c := &ConsumerConfig{
			ID: "consumer1",
			ModelRoutes: map[string]*RouteConfig{
				"gpt-4":     {ID: "route1"},
				"gpt-3.5":   {ID: "route1"},
				"embedding": {ID: "route2"},
			},
		}

		models := c.GetAvailableModels("")
		assert.Len(t, models, 3)
		assert.Contains(t, models, "gpt-4")
		assert.Contains(t, models, "gpt-3.5")
		assert.Contains(t, models, "embedding")
	})

	t.Run("filter by openai protocol", func(t *testing.T) {
		c := &ConsumerConfig{
			ID: "consumer1",
			ModelRoutes: map[string]*RouteConfig{
				"gpt-4": {
					ID:        "route1",
					Protocols: []Protocol{ProtocolOpenAI},
				},
				"original-model": {
					ID:        "route2",
					Protocols: []Protocol{ProtocolOriginal},
				},
				"both-protocols": {
					ID:        "route3",
					Protocols: []Protocol{ProtocolOpenAI, ProtocolOriginal},
				},
			},
		}

		models := c.GetAvailableModels(ProtocolOpenAI)
		assert.Len(t, models, 2)
		assert.Contains(t, models, "gpt-4")
		assert.Contains(t, models, "both-protocols")
		assert.NotContains(t, models, "original-model")
	})

	t.Run("filter by original protocol", func(t *testing.T) {
		c := &ConsumerConfig{
			ID: "consumer1",
			ModelRoutes: map[string]*RouteConfig{
				"gpt-4": {
					ID:        "route1",
					Protocols: []Protocol{ProtocolOpenAI},
				},
				"original-model": {
					ID:        "route2",
					Protocols: []Protocol{ProtocolOriginal},
				},
				"both-protocols": {
					ID:        "route3",
					Protocols: []Protocol{ProtocolOpenAI, ProtocolOriginal},
				},
			},
		}

		models := c.GetAvailableModels(ProtocolOriginal)
		assert.Len(t, models, 2)
		assert.Contains(t, models, "original-model")
		assert.Contains(t, models, "both-protocols")
		assert.NotContains(t, models, "gpt-4")
	})

	t.Run("empty model routes", func(t *testing.T) {
		c := &ConsumerConfig{
			ID:          "consumer2",
			ModelRoutes: map[string]*RouteConfig{},
		}

		models := c.GetAvailableModels(ProtocolOpenAI)
		assert.Empty(t, models)
	})

	t.Run("nil model routes", func(t *testing.T) {
		c := &ConsumerConfig{
			ID:          "consumer3",
			ModelRoutes: nil,
		}

		models := c.GetAvailableModels(ProtocolOpenAI)
		assert.Empty(t, models)
	})

	t.Run("route with nil protocols", func(t *testing.T) {
		c := &ConsumerConfig{
			ID: "consumer1",
			ModelRoutes: map[string]*RouteConfig{
				"model-with-nil-protocols": {
					ID:        "route1",
					Protocols: nil,
				},
			},
		}

		// When protocols is nil, the model should be included (no filter applied)
		models := c.GetAvailableModels(ProtocolOpenAI)
		assert.Len(t, models, 1)
		assert.Contains(t, models, "model-with-nil-protocols")
	})
}
