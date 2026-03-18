package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	consumerHeaderName      = "x-mse-consumer"
	modelHeaderName         = "x-higress-llm-model"
	llmServiceIdHeaderName  = "x-llm-service-id"
	authorityHostHeaderName = ":authority"
	originalHostHeaderName  = "x-envoy-original-host"
	debugHeaderName         = "x-trip-aigw-debug"
	debugHeaderValue        = "true"

	fallbackHeaderName  = "x-trip-llm-fallback"
	fallbackHeaderValue = "1"

	originalRequestPathKeyword = "/raw/"

	matchedRouteConfigKey = "routeConfig"
	consumerIdKey         = "consumerId"
	routeIdKey            = "routeId"
	serviceIdKey          = "serviceId"
	debugModeKey          = "debug"

	requestRatelimitDryRunGlobalKey                = "request-ratelimit.dry-run.global"
	requestRatelimitDryRunByConsumerKeyFormat      = "request-ratelimit.dry-run.consumer.%s"
	requestRatelimitDryRunByModelKeyFormat         = "request-ratelimit.dry-run.model.%s"
	requestRatelimitDryRunByConsumerModelKeyFormat = "request-ratelimit.dry-run.consumer.%s.model.%s"
	requestRatelimitDryRunDefaultValue             = false

	tokenRatelimitDryRunGlobalKey                = "token-ratelimit.dry-run.global"
	tokenRatelimitDryRunByConsumerKeyFormat      = "token-ratelimit.dry-run.consumer.%s"
	tokenRatelimitDryRunByModelKeyFormat         = "token-ratelimit.dry-run.model.%s"
	tokenRatelimitDryRunByConsumerModelKeyFormat = "token-ratelimit.dry-run.consumer.%s.model.%s"
	tokenRatelimitDryRunDefaultValue             = true

	defaultServiceRouteHost = "aigw.internal"
)

var (
	errorResponseHeaders = [][2]string{
		{"content-type", "application/json"},
	}

	routePathPrefixPropertyPath = []string{"route.path.prefix"}

	requestRatelimitKeyPropertyPath        = []string{"cluster-key-rate-limit.key"}
	requestRatelimitCountPropertyPath      = []string{"cluster-key-rate-limit.count"}
	requestRatelimitTimeWindowPropertyPath = []string{"cluster-key-rate-limit.time_window"}
	requestRatelimitDryRunPropertyPath     = []string{"cluster-key-rate-limit.dry_run"}
	tokenRatelimitKeyPropertyPath          = []string{"ai-token-ratelimit.key"}
	tokenRatelimitCountPropertyPath        = []string{"ai-token-ratelimit.count"}
	tokenRatelimitTimeWindowPropertyPath   = []string{"ai-token-ratelimit.time_window"}
	tokenRatelimitDryRunPropertyPath       = []string{"ai-token-ratelimit.dry_run"}

	dryRunBytes = uint32ToBytes(1)
)

type PluginConfig struct {
	Consumers             map[string]*ConsumerConfig
	ServiceRouteHost      string
	EnabledOnPathPrefixes []string
}

func (c *PluginConfig) FromJson(json gjson.Result) {
	c.Consumers = make(map[string]*ConsumerConfig)

	consumers := json.Get("consumers")
	if !consumers.Exists() {
		return
	}

	consumers.ForEach(func(_, value gjson.Result) bool {
		consumerConfig := &ConsumerConfig{}
		if err := consumerConfig.FromJson(value); err != nil {
			log.Errorf("Failed to parse consumer config: %v\n%s", err, value.Raw)
		} else {
			c.Consumers[consumerConfig.ID] = consumerConfig
		}
		return true
	})

	if host := json.Get("serviceRouteHost"); host.Exists() && host.Type == gjson.String {
		c.ServiceRouteHost = host.String()
	} else {
		c.ServiceRouteHost = defaultServiceRouteHost
	}

	if prefixes := json.Get("enabledOnPathPrefixes"); prefixes.Exists() && prefixes.IsArray() {
		c.EnabledOnPathPrefixes = make([]string, 0, len(prefixes.Array()))
		prefixes.ForEach(func(_, value gjson.Result) bool {
			c.EnabledOnPathPrefixes = append(c.EnabledOnPathPrefixes, value.String())
			return true
		})
	} else {
		c.EnabledOnPathPrefixes = defaultEnabledOnPathPrefixes
	}
}

func (c *PluginConfig) GetConsumerConfig(consumerId string) *ConsumerConfig {
	if c.Consumers == nil {
		return nil
	}
	return c.Consumers[consumerId]
}

func main() {}

func init() {
	wrapper.SetCtx(
		"trip-llm-router",
		wrapper.ParseConfig(parseConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		wrapper.ProcessResponseHeaders(onHttpResponseHeaders),
	)
}

func parseConfig(json gjson.Result, config *PluginConfig) (err error) {
	config.FromJson(json)
	return nil
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config PluginConfig) types.Action {
	if !shallProcessRequest(ctx, &config) {
		return types.ActionContinue
	}

	consumerId, _ := proxywasm.GetHttpRequestHeader(consumerHeaderName)
	if consumerId == "" {
		_ = sendErrorResponse(401, "trip-llm-router.missing-consumer-id", "No consumer ID found after authentication")
		return types.ActionContinue
	}

	consumerConfig := config.GetConsumerConfig(consumerId)
	if consumerConfig == nil {
		_ = sendErrorResponse(403, "trip-llm-router.unknown-consumer", "Consumer not recognized: "+consumerId)
		return types.ActionContinue
	}

	ctx.SetContext(consumerIdKey, consumerId)

	path := ctx.Path()
	consumerPathKeyword := fmt.Sprintf("/%s/", consumerId)
	if index := strings.Index(path, consumerPathKeyword); index == -1 {
		_ = sendErrorResponse(400, "trip-llm-router.bad-path", "Request path doesn't match the authenticated consumer ID: "+path)
		return types.ActionContinue
	} else {
		routePathPrefix := path[:index+len(consumerPathKeyword)-1] // Remove the trailing slash
		log.Debugf("route path prefix: %s", routePathPrefix)
		_ = proxywasm.SetProperty(routePathPrefixPropertyPath, []byte(routePathPrefix))
	}

	model, _ := proxywasm.GetHttpRequestHeader(modelHeaderName)
	if model == "" {
		_ = sendErrorResponse(400, "trip-llm-router.missing-model", "No model specified in the request")
		return types.ActionContinue
	}

	routeConfig := consumerConfig.GetRouteConfig(model)
	if routeConfig == nil {
		_ = sendErrorResponse(403, "trip-llm-router.unauthorized-model", "Consumer "+consumerId+" is not authorized to access model "+model)
		return types.ActionContinue
	}

	requestProtocol := getRequestProtocol(ctx)
	if routeConfig.Protocols != nil && !slices.Contains(routeConfig.Protocols, requestProtocol) {
		_ = sendErrorResponse(403, "trip-llm-router.unenabled-protocol", "Protocol "+string(requestProtocol)+" is not enabled for model "+model)
		return types.ActionContinue
	}

	ctx.SetContext(matchedRouteConfigKey, routeConfig)

	if err := executeRoute(ctx, &config, consumerId, model, routeConfig); err != nil {
		var httpErr httpResponseError
		if errors.As(err, &httpErr) {
			_ = sendErrorResponse(httpErr.statusCode, httpErr.statusCodeDetails, httpErr.message)
		} else {
			_ = sendErrorResponse(500, "trip-llm-router.route-execution-failed", "Failed to execute routing logic: "+err.Error())
		}
	}

	if debugValue, _ := proxywasm.GetHttpRequestHeader(debugHeaderName); debugValue == debugHeaderValue {
		ctx.SetContext(debugModeKey, true)
	}

	return types.ActionContinue
}

func getRequestProtocol(ctx wrapper.HttpContext) Protocol {
	if strings.Contains(ctx.Path(), originalRequestPathKeyword) {
		return ProtocolOriginal
	}
	return ProtocolOpenAI
}

func executeRoute(ctx wrapper.HttpContext, pluginConfig *PluginConfig, consumerId, model string, routeConfig *RouteConfig) error {
	var targetService *ServiceConfig
	routeId := routeConfig.ID

	defer func() {
		ctx.SetUserAttribute("routeId", routeId)
		ctx.SetContext(routeIdKey, routeId)
		if targetService != nil {
			ctx.SetContext(serviceIdKey, targetService.ID)
		}
		_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)
	}()

	fallbackRequest := isFallbackRequest()
	if !fallbackRequest {
		targetService = routeConfig.Upstream.SelectService()

		// Only set rate limit properties for the original request, not for fallback requests,=
		// to avoid double counting
		setRatelimitProperties(ctx, consumerId, model, routeConfig.Ratelimit)
	} else if routeConfig.Fallback == nil {
		return httpResponseError{
			statusCode:        400,
			statusCodeDetails: "trip-llm-router.unexpected-fallback",
			message:           "Fallback is not enabled for the request",
		}
	} else {
		targetService = routeConfig.Fallback.SelectFallbackService()
		routeId = routeId + ".fallback"
	}

	if targetService == nil {
		return fmt.Errorf("no available service for routeConfig %s", routeConfig.ID)
	}

	_ = proxywasm.ReplaceHttpRequestHeader(llmServiceIdHeaderName, targetService.ID)
	if pluginConfig.ServiceRouteHost != "" {
		if originalHost, _ := proxywasm.GetHttpRequestHeader(originalHostHeaderName); originalHost == "" {
			if err := proxywasm.ReplaceHttpRequestHeader(originalHostHeaderName, ctx.Host()); err != nil {
				return fmt.Errorf("failed to set original host header: %w", err)
			}
		}
		if err := proxywasm.ReplaceHttpRequestHeader(authorityHostHeaderName, pluginConfig.ServiceRouteHost); err != nil {
			return fmt.Errorf("failed to set authority header: %w", err)
		}
	}

	return nil
}

func isFallbackRequest() bool {
	fallbackValue, _ := proxywasm.GetHttpRequestHeader(fallbackHeaderName)
	return fallbackValue == fallbackHeaderValue
}

func setRatelimitProperties(ctx wrapper.HttpContext, consumerId, model string, ratelimit *RatelimitConfig) {
	if ratelimit == nil {
		return
	}
	if ratelimit.RPM > 0 {
		_ = proxywasm.SetProperty(requestRatelimitKeyPropertyPath, ratelimit.RPMKey)
		_ = proxywasm.SetProperty(requestRatelimitCountPropertyPath, ratelimit.RPMData)
		_ = proxywasm.SetProperty(requestRatelimitTimeWindowPropertyPath, ratelimitTimeWindowBytes)
		if needDryRunRequestRatelimit(ctx, consumerId, model) {
			_ = proxywasm.SetProperty(requestRatelimitDryRunPropertyPath, dryRunBytes)
		}

		log.Debugf("Set request rate limit properties: key=%s, count=%s", ratelimit.RPMKey, ratelimit.RPMData)
	}
	if ratelimit.TPM > 0 {
		_ = proxywasm.SetProperty(tokenRatelimitKeyPropertyPath, ratelimit.TPMKey)
		_ = proxywasm.SetProperty(tokenRatelimitCountPropertyPath, ratelimit.TPMData)
		_ = proxywasm.SetProperty(tokenRatelimitTimeWindowPropertyPath, ratelimitTimeWindowBytes)
		if needDryRunTokenRatelimit(ctx, consumerId, model) {
			_ = proxywasm.SetProperty(tokenRatelimitDryRunPropertyPath, dryRunBytes)
		}

		log.Debugf("Set token rate limit properties: key=%s, count=%s", ratelimit.TPMKey, ratelimit.TPMData)
	}
}

func needDryRunRequestRatelimit(ctx wrapper.HttpContext, consumerId, model string) bool {
	var enabledKeys []string
	if consumerId != "" {
		if model != "" {
			enabledKeys = append(enabledKeys, fmt.Sprintf(requestRatelimitDryRunByConsumerModelKeyFormat, consumerId, model))
		}
		enabledKeys = append(enabledKeys, fmt.Sprintf(requestRatelimitDryRunByConsumerKeyFormat, consumerId))
	}
	if model != "" {
		enabledKeys = append(enabledKeys, fmt.Sprintf(requestRatelimitDryRunByModelKeyFormat, model))
	}
	enabledKeys = append(enabledKeys, requestRatelimitDryRunGlobalKey)

	for _, key := range enabledKeys {
		if enabled, ok := ctx.GetGlobalConfig(key).(bool); ok {
			log.Debugf("got request ratelimit dry-run value from global config with key %s: %t", key, enabled)
			return enabled
		}
	}

	return requestRatelimitDryRunDefaultValue
}

func needDryRunTokenRatelimit(ctx wrapper.HttpContext, consumerId, model string) bool {
	var enabledKeys []string
	if consumerId != "" {
		if model != "" {
			enabledKeys = append(enabledKeys, fmt.Sprintf(tokenRatelimitDryRunByConsumerModelKeyFormat, consumerId, model))
		}
		enabledKeys = append(enabledKeys, fmt.Sprintf(tokenRatelimitDryRunByConsumerKeyFormat, consumerId))
	}
	if model != "" {
		enabledKeys = append(enabledKeys, fmt.Sprintf(tokenRatelimitDryRunByModelKeyFormat, model))
	}
	enabledKeys = append(enabledKeys, tokenRatelimitDryRunGlobalKey)

	for _, key := range enabledKeys {
		if enabled, ok := ctx.GetGlobalConfig(key).(bool); ok {
			log.Debugf("got token ratelimit dry-run value from global config with key %s: %t", key, enabled)
			return enabled
		}
	}

	return tokenRatelimitDryRunDefaultValue
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config PluginConfig) types.Action {
	routeConfig, _ := ctx.GetContext(matchedRouteConfigKey).(*RouteConfig)
	if routeConfig == nil {
		return types.ActionContinue
	}

	defer func() {
		if debug := ctx.GetBoolContext(debugModeKey, false); debug {
			_ = proxywasm.ReplaceHttpResponseHeader("x-trip-llm-consumer", ctx.GetStringContext(consumerIdKey, ""))
			_ = proxywasm.ReplaceHttpResponseHeader("x-trip-llm-route", ctx.GetStringContext(routeIdKey, ""))
			_ = proxywasm.ReplaceHttpResponseHeader("x-trip-llm-service", ctx.GetStringContext(serviceIdKey, ""))
		}
	}()

	status, _ := proxywasm.GetHttpResponseHeader(":status")
	if status == "200" {
		return types.ActionContinue
	}

	if status == "429" {
		// If the ratelimit error is triggered by another wasm plugin, we should skip the fallback.
		// e.g.: via_wasm::cluster-key-rate-limit::cluster-key-rate-limit.rejected
		// Prefix defined in envoy/envoy/source/extensions/common/wasm/context.cc
		responseCodeDetails, _ := proxywasm.GetProperty([]string{"response", "code_details"})
		if strings.HasPrefix(string(responseCodeDetails), "via_wasm") {
			log.Debugf("skip fallback for ratelimit triggered by other wasm plugin, code_details=%s", string(responseCodeDetails))
			return types.ActionContinue
		}
	}

	if routeConfig.Fallback != nil && routeConfig.Fallback.ShallFallback(status) {
		_ = proxywasm.ReplaceHttpResponseHeader(fallbackHeaderName, fallbackHeaderValue)
	}

	return types.ActionContinue
}

func shallProcessRequest(ctx wrapper.HttpContext, config *PluginConfig) bool {
	if config.EnabledOnPathPrefixes == nil {
		return true
	}
	path := ctx.Path()
	for _, prefix := range config.EnabledOnPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func sendErrorResponse(statusCode uint32, statusCodeDetails, message string) error {
	data, _ := json.Marshal(map[string]interface{}{
		"code":    statusCode,
		"message": message,
	})
	return proxywasm.SendHttpResponseWithDetail(statusCode, statusCodeDetails, errorResponseHeaders, data, -1)
}

type httpResponseError struct {
	statusCode        uint32
	statusCodeDetails string
	message           string
}

func (e httpResponseError) Error() string {
	return fmt.Sprintf("HTTP %d %s - %s", e.statusCode, e.statusCodeDetails, e.message)
}
