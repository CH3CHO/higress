package main

import (
	"fmt"
	"math/rand"
	"strings"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"
)

type Protocol string

const (
	ProtocolOpenAI   Protocol = "openai"
	ProtocolOriginal Protocol = "original"

	statusCodeClassSuffix = "xx"

	// To be consistent with the rate limit key format used in the ratelimit plugins,
	// we use the same format for the request rate limit keys.
	requestRatelimitKeyFormat = "higress-cluster-key-rate-limit:%s:global_threshold:%d:%d"
	tokenRatelimitKeyFormat   = "higress-token-ratelimit:%s:global_threshold:%d:%d"

	ratelimitTimeWindow = 60
)

var (
	defaultProtocols = []Protocol{ProtocolOpenAI, ProtocolOriginal}
	knownProtocols   = map[Protocol]bool{
		ProtocolOpenAI:   true,
		ProtocolOriginal: true,
	}
	defaultEnabledOnPathPrefixes = []string{"/llm/"}

	ratelimitTimeWindowBytes = uint32ToBytes(uint32(ratelimitTimeWindow))
)

type RouteConfig struct {
	ID        string
	Models    []string
	Upstream  *RouteUpstreamConfig
	Ratelimit *RatelimitConfig
	Fallback  *FallbackConfig
	Protocols []Protocol
}

func (c *RouteConfig) FromJson(json gjson.Result) error {
	if id := json.Get("id"); id.Exists() && id.Type == gjson.String {
		c.ID = id.String()
	} else {
		return fmt.Errorf("bad route config: missing or invalid id: %s", json.Raw)
	}

	if models := json.Get("models"); models.Exists() && models.IsArray() {
		c.Models = make([]string, 0, len(models.Array()))
		models.ForEach(func(_, value gjson.Result) bool {
			c.Models = append(c.Models, value.String())
			return true
		})
	} else {
		return fmt.Errorf("bad route config: missing or invalid models: %s", json.Raw)
	}

	if services := json.Get("services"); services.Exists() && services.IsArray() {
		c.Upstream = &RouteUpstreamConfig{}
		if err := c.Upstream.FromJson(services); err != nil {
			return fmt.Errorf("bad route config: invalid services: %v\n%s", err, services.Raw)
		}
	}

	if ratelimit := json.Get("ratelimit"); ratelimit.Exists() && ratelimit.IsObject() {
		ratelimitConfig := &RatelimitConfig{}
		if err := ratelimitConfig.FromJson(ratelimit); err != nil {
			return fmt.Errorf("bad route config: invalid ratelimit: %v\n%s", err, ratelimit.Raw)
		}
		ratelimitConfig.Initialize(c.ID)
		c.Ratelimit = ratelimitConfig
	}

	if fallback := json.Get("fallback"); fallback.Exists() && fallback.IsObject() {
		fallbackConfig := &FallbackConfig{}
		if err := fallbackConfig.FromJson(fallback); err != nil {
			return fmt.Errorf("bad route config: invalid fallback: %v\n%s", err, fallback.Raw)
		}
		c.Fallback = fallbackConfig
	}

	if protocols := json.Get("protocols"); protocols.Exists() && protocols.IsArray() {
		c.Protocols = make([]Protocol, 0, len(protocols.Array()))
		protocols.ForEach(func(_, value gjson.Result) bool {
			protocol := Protocol(value.String())
			if knownProtocols[protocol] {
				c.Protocols = append(c.Protocols, protocol)
			} else {
				log.Warnf("Unknown protocol in route config: %s\n%s", value.String(), value.Raw)
			}
			return true
		})
	} else {
		c.Protocols = defaultProtocols
	}

	return nil
}

type RouteUpstreamConfig struct {
	Services  []*WeightedServiceConfig
	weightSum int
}

func (c *RouteUpstreamConfig) FromJson(json gjson.Result) error {
	if !json.IsArray() {
		return fmt.Errorf("bad route upstream config: invalid services: %s", json.Raw)
	}

	weightSum := 0
	c.Services = make([]*WeightedServiceConfig, 0, len(json.Array()))
	json.ForEach(func(_, value gjson.Result) bool {
		serviceConfig := &WeightedServiceConfig{}
		if err := serviceConfig.FromJson(value); err != nil {
			log.Errorf("Failed to parse route upstream service config: %v\n%s", err, value.Raw)
		} else {
			c.Services = append(c.Services, serviceConfig)
			if serviceConfig.Weight > 0 {
				weightSum += serviceConfig.Weight
			}
		}
		return true
	})
	c.weightSum = weightSum

	if len(c.Services) == 0 {
		return fmt.Errorf("bad route upstream config: no valid services: %s", json.Raw)
	}
	if c.weightSum <= 0 {
		return fmt.Errorf("bad route upstream config: non-positive total weight: %d: %s", c.weightSum, json.Raw)
	}

	return nil
}

func (c *RouteUpstreamConfig) SelectService() *ServiceConfig {
	if len(c.Services) == 0 {
		return nil
	}

	if len(c.Services) == 1 {
		return &c.Services[0].ServiceConfig
	}

	if c.weightSum <= 0 {
		return nil
	}

	r := rand.Intn(c.weightSum)
	for _, service := range c.Services {
		if service.Weight > 0 {
			r -= service.Weight
			if r < 0 {
				return &service.ServiceConfig
			}
		}
	}

	return nil
}

type ServiceConfig struct {
	ID string
}

func (c *ServiceConfig) FromJson(json gjson.Result) error {
	if id := json.Get("id"); id.Exists() && id.Type == gjson.String {
		c.ID = id.String()
	} else {
		return fmt.Errorf("bad service config: missing or invalid id: %s", json.Raw)
	}
	return nil
}

type WeightedServiceConfig struct {
	ServiceConfig
	Weight int
}

func (c *WeightedServiceConfig) FromJson(json gjson.Result) error {
	if err := c.ServiceConfig.FromJson(json); err != nil {
		return err
	}
	if weight := json.Get("weight"); weight.Exists() && weight.Type == gjson.Number {
		c.Weight = int(weight.Int())
	} else {
		return fmt.Errorf("bad service config: missing or invalid weight: %s", json.Raw)
	}
	return nil
}

type RatelimitConfig struct {
	RPM     int
	RPMData []byte
	RPMKey  []byte
	TPM     int
	TPMData []byte
	TPMKey  []byte
}

func (c *RatelimitConfig) FromJson(json gjson.Result) error {
	if rpm := json.Get("rpm"); rpm.Exists() && rpm.Type == gjson.Number {
		c.RPM = int(rpm.Int())
	}
	if tpm := json.Get("tpm"); tpm.Exists() && tpm.Type == gjson.Number {
		c.TPM = int(tpm.Int())
	}
	return nil
}

func (c *RatelimitConfig) Initialize(routeId string) {
	if c.RPM > 0 {
		c.RPMData = uint32ToBytes(uint32(c.RPM))
		c.RPMKey = []byte(fmt.Sprintf(requestRatelimitKeyFormat, routeId, c.RPM, ratelimitTimeWindow))
	}
	if c.TPM > 0 {
		c.TPMData = uint32ToBytes(uint32(c.TPM))
		c.TPMKey = []byte(fmt.Sprintf(tokenRatelimitKeyFormat, routeId, c.TPM, ratelimitTimeWindow))
	}
}

type FallbackConfig struct {
	StatusCodes []string
	Services    []ServiceConfig

	exactMatchStatusCodes map[string]bool
	statusCodeClasses     []string
}

func (c *FallbackConfig) FromJson(json gjson.Result) error {
	c.exactMatchStatusCodes = make(map[string]bool)

	if statusCodes := json.Get("statusCodes"); statusCodes.Exists() && statusCodes.IsArray() {
		c.StatusCodes = make([]string, 0, len(statusCodes.Array()))
		statusCodes.ForEach(func(_, value gjson.Result) bool {
			statusCode := value.String()
			if strings.HasSuffix(statusCode, statusCodeClassSuffix) {
				c.statusCodeClasses = append(c.statusCodeClasses, statusCode[:len(statusCode)-len(statusCodeClassSuffix)])
			} else {
				c.exactMatchStatusCodes[statusCode] = true
			}
			c.StatusCodes = append(c.StatusCodes, statusCode)
			return true
		})
	} else {
		return fmt.Errorf("bad fallback config: missing or invalid statusCodes: %s", json.Raw)
	}

	if services := json.Get("services"); services.Exists() && services.IsArray() {
		c.Services = make([]ServiceConfig, 0, len(services.Array()))
		services.ForEach(func(_, value gjson.Result) bool {
			serviceConfig := ServiceConfig{}
			if err := serviceConfig.FromJson(value); err != nil {
				log.Errorf("Failed to parse fallback service config: %v\n%s", err, value.Raw)
			} else {
				c.Services = append(c.Services, serviceConfig)
			}
			return true
		})
	} else {
		return fmt.Errorf("bad fallback config: missing or invalid services: %s", json.Raw)
	}

	return nil
}

func (c *FallbackConfig) ShallFallback(statusCode string) bool {
	if c.exactMatchStatusCodes != nil && c.exactMatchStatusCodes[statusCode] {
		return true
	}
	if c.statusCodeClasses != nil {
		for _, class := range c.statusCodeClasses {
			if strings.HasPrefix(statusCode, class) {
				return true
			}
		}
	}
	return false
}

func (c *FallbackConfig) SelectFallbackService() *ServiceConfig {
	if len(c.Services) == 0 {
		return nil
	}

	if len(c.Services) == 1 {
		return &c.Services[0]
	}

	// For simplicity, we select a random fallback service. More sophisticated strategies can be implemented as needed.
	index := rand.Intn(len(c.Services))
	return &c.Services[index]
}
