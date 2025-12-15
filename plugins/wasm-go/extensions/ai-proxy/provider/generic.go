package provider

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	genericAuthTypeNone   = "NONE"
	genericAuthTypeBearer = "BEARER"
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
	if m.runtimeConfig != nil && m.runtimeConfig.routePathPrefix != "" {
		// It is assumed that routePathPrefix doesn't have trailing slash and serviceUrl.Path has leading slash.
		if len(path) == len(m.runtimeConfig.routePathPrefix) {
			path = m.serviceUrl.Path
		} else if strings.HasPrefix(path, m.runtimeConfig.routePathPrefix+"/") {
			path = m.serviceUrl.Path + path[len(m.runtimeConfig.routePathPrefix)+1:]
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
