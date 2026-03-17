package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	genericAuthTypeNone   = "NONE"
	genericAuthTypeBearer = "BEARER"
)

var (
	routePathPrefixPropertyPath = []string{"route.path.prefix"}
)

// genericProvider is the provider for generic LLM services.

type genericProviderInitializer struct {
}

func (m *genericProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if config.genericServiceUrl == "" {
		return errors.New("missing genericServiceUrl in provider config")
	}
	switch config.genericAuthType {
	case "", genericAuthTypeNone, genericAuthTypeBearer:
	default:
		return fmt.Errorf("unsupported genericAuthType %s in provider config", config.genericAuthType)
	}
	return nil
}

func (m *genericProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{}
}

func (m *genericProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	serviceUrl, err := url.Parse(config.genericServiceUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid genericServiceUrl %s: %v", config.genericServiceUrl, err)
	}
	config.setDefaultCapabilities(m.DefaultCapabilities())
	return &genericProvider{
		config:     config,
		serviceUrl: serviceUrl,
	}, nil
}

type genericProvider struct {
	config        ProviderConfig
	serviceUrl    *url.URL
	runtimeConfig *ProviderRuntimeConfig
}

func (m *genericProvider) GetProviderType() string {
	return providerTypeGeneric
}

func (m *genericProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	m.config.handleRequestHeaders(m, ctx, apiName)
	return nil
}

func (m *genericProvider) SetRuntimeConfig(config *ProviderRuntimeConfig) error {
	m.runtimeConfig = config
	return nil
}

func (m *genericProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	path := ctx.Path()
	if routePathPrefix := m.getRoutePathPrefix(); routePathPrefix != "" {
		// It is assumed that serviceUrl.Path has leading slash,
		// and we trim the trailing slash of routePathPrefix for easier matching and concatenation.
		routePathPrefix = strings.TrimSuffix(routePathPrefix, "/")
		if len(path) == len(routePathPrefix) {
			path = m.serviceUrl.Path
		} else if strings.HasPrefix(path, routePathPrefix+"/") {
			path = m.serviceUrl.Path + path[len(routePathPrefix)+1:]
		}
	}
	util.OverwriteRequestPathHeader(headers, path)
	util.OverwriteRequestHostHeader(headers, m.serviceUrl.Host)

	if len(m.config.apiTokens) > 0 {
		switch m.config.genericAuthType {
		case genericAuthTypeNone:
		case "", genericAuthTypeBearer:
			util.OverwriteRequestAuthorizationHeader(headers, "Bearer "+m.config.GetApiTokenInUse(ctx))
		}
	}
}

func (m *genericProvider) getRoutePathPrefix() string {
	if m.runtimeConfig != nil && m.runtimeConfig.routePathPrefix != "" {
		return m.runtimeConfig.routePathPrefix
	}
	rawRoutePathPrefix, _ := proxywasm.GetProperty(routePathPrefixPropertyPath)
	if rawRoutePathPrefix != nil {
		log.Debugf("got route path prefix from property: %s", rawRoutePathPrefix)
		return string(rawRoutePathPrefix)
	}
	return ""
}
