package main

import (
	"fmt"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"
)

type ConsumerConfig struct {
	ID          string
	ModelRoutes map[string]*RouteConfig
}

func (c *ConsumerConfig) FromJson(json gjson.Result) error {
	if id := json.Get("id"); id.Exists() && id.Type == gjson.String {
		c.ID = id.String()
	} else {
		return fmt.Errorf("bad consumer config: missing or invalid id: %s", json.Raw)
	}

	routes := json.Get("routes")
	if !routes.Exists() {
		return nil
	}
	if !routes.IsArray() {
		return fmt.Errorf("bad consumer config: invalid routes: %s", json.Raw)
	}

	c.ModelRoutes = make(map[string]*RouteConfig)
	routes.ForEach(func(_, value gjson.Result) bool {
		routeConfig := &RouteConfig{}
		if err := routeConfig.FromJson(value); err != nil {
			log.Errorf("Failed to parse route config: %v\n%s", err, value.Raw)
		} else if len(routeConfig.Models) != 0 {
			for _, model := range routeConfig.Models {
				c.ModelRoutes[model] = routeConfig
			}
		}
		return true
	})

	return nil
}

func (c *ConsumerConfig) GetRouteConfig(model string) *RouteConfig {
	return c.ModelRoutes[model]
}
