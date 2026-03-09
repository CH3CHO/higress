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
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"ai-token-ratelimit/config"
	"ai-token-ratelimit/json"
	"ai-token-ratelimit/util"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/resp"
)

func main() {}

func init() {
	wrapper.SetCtx(
		"ai-token-ratelimit",
		wrapper.ParseOverrideConfig(parseGlobalConfig, parseOverrideRuleConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		wrapper.ProcessResponseHeaders(onHttpResponseHeaders),
		wrapper.ProcessStreamingResponseBody(onHttpStreamingBody),
		wrapper.ProcessResponseBody(onHttpResponseBody),
		wrapper.WithRebuildAfterRequests[config.AiTokenRateLimitConfig](1000),
		wrapper.WithRebuildMaxMemBytes[config.AiTokenRateLimitConfig](200*1024*1024),
	)
}

type ResponseType int

const (
	RedisKeyPrefix string = "higress-token-ratelimit"
	// AiTokenGlobalRateLimitFormat  全局限流模式 redis key 为 RedisKeyPrefix:限流规则名称:global_threshold:时间窗口:窗口内限流数
	AiTokenGlobalRateLimitFormat = RedisKeyPrefix + ":%s:global_threshold:%d:%d"
	// AiTokenRateLimitFormat 规则限流模式 redis key 为 RedisKeyPrefix:限流规则名称:限流类型:时间窗口:窗口内限流数:限流key名称:限流key对应的实际值
	AiTokenRateLimitFormat        = RedisKeyPrefix + ":%s:%s:%d:%d:%s:%s"
	RequestPhaseFixedWindowScript = `
	local ttl = redis.call('ttl', KEYS[1])
	if ttl < 0 then
	redis.call('set', KEYS[1], ARGV[1], 'EX', ARGV[2])
	return {ARGV[1], ARGV[1], ARGV[2]}
	end
	return {ARGV[1], redis.call('get', KEYS[1]), ttl}
	`
	ResponsePhaseFixedWindowScript = `
	local ttl = redis.call('ttl', KEYS[1])
	if ttl < 0 then
	redis.call('set', KEYS[1], ARGV[1]-ARGV[3], 'EX', ARGV[2])
	return {ARGV[1], ARGV[1]-ARGV[3], ARGV[2]}
	end
	return {ARGV[1], redis.call('decrby', KEYS[1], ARGV[3]), ttl}
	`

	Skip                   = "skip"
	LimitRedisContextKey   = "LimitRedisContext"
	ResponseTypeKey        = "ResponseType"
	ResponseUsageFoundKey  = "ResponseUsageFound"
	ResponseNeedBuffer     = "ResponseNeedBuffer"
	ResponseEndOfStreamKey = "ResponseEndOfStream"

	ResponseTypeJson ResponseType = 1
	ResponseTypeSse  ResponseType = 2

	CookieHeader = "cookie"

	RateLimitResetHeader = "X-TokenRateLimit-Reset" // 限流重置时间（触发限流时返回）

	TokenRateLimitCount         = "token_ratelimit_count" // metric name
	StreamingBodyBufferKey      = "streaming_body_buffer"
	StreamingJsonReaderKey      = "streaming_json_reader"
	SkipStreamingJsonProcessing = "skip_streaming_json_processing"
	UsageObject                 = "usage_object"

	consumerHeader = "x-mse-consumer"
	modelHeader    = "x-higress-llm-model"

	LogKey               = "rate_limit_log"
	UserAttrKeyKey       = "tpx_key"
	UserAttrThresholdKey = "tpx_threshold"
	UserAttrIntervalKey  = "tpx_interval"
	UserAttrResultKey    = "tpx_result"
	UserAttrRemainingKey = "tpx_remaining"
	UserAttrUsageKey     = "tpx_usage"

	defaultMaxBodyBytes uint32 = 100 * 1024 * 1024

	enabledGlobalKey                = "enabled.global"
	enabledByConsumerKeyFormat      = "enabled.consumer.%s"
	enabledByModelKeyFormat         = "enabled.model.%s"
	enabledByConsumerModelKeyFormat = "enabled.consumer.%s.model.%s"
	enabledDefaultValue             = false
)

var (
	tokenRatelimitKeyPropertyPath        = []string{"ai-token-ratelimit.key"}
	tokenRatelimitCountPropertyPath      = []string{"ai-token-ratelimit.count"}
	tokenRatelimitTimeWindowPropertyPath = []string{"ai-token-ratelimit.time_window"}
	tokenRatelimitDryRunPropertyPath     = []string{"ai-token-ratelimit.dry_run"}
)

type LimitContext struct {
	count     int
	remaining int
	reset     int
}

type LimitRedisContext struct {
	key    string
	count  int64
	window int64
}

func parseGlobalConfig(json gjson.Result, cfg *config.AiTokenRateLimitConfig) error {
	*cfg = config.AiTokenRateLimitConfig{}
	return parseConfig(json, cfg)
}

func parseOverrideRuleConfig(json gjson.Result, global config.AiTokenRateLimitConfig, cfg *config.AiTokenRateLimitConfig) error {
	*cfg = global
	if err := parseConfig(json, cfg); err != nil {
		return err
	}
	if cfg.RedisClient == nil {
		return fmt.Errorf("redis client is not configured properly")
	}
	return nil
}

func parseConfig(json gjson.Result, cfg *config.AiTokenRateLimitConfig) error {
	if config.IsRedisConfigured(json) {
		if err := config.InitRedisClusterClient(json, cfg); err != nil {
			return err
		}
	}
	if config.IsRuleNameConfigured(json) {
		if err := config.ParseAiTokenRateLimitConfig(json, cfg); err != nil {
			return err
		}
	}
	// Metric settings
	cfg.CounterMetrics = make(map[string]proxywasm.MetricCounter)
	return nil
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, cfg config.AiTokenRateLimitConfig) types.Action {
	ctx.DisableReroute()
	limitKey, count, timeWindow := "", int64(0), int64(0)
	dryRun := false

	if !isEnabled(ctx) {
		log.Debugf("plugin is not enabled for this request, skip processing")
		ctx.SetContext(Skip, true)
		ctx.DontReadRequestBody()
		ctx.DontReadResponseBody()
		return types.ActionContinue
	}

	if limitKeyFromProperty, countFromProperty, timeWindowFromProperty, dryRunFromProperty, ok := getContextValuesFromProperty(); ok {
		log.Debugf("got context values from properties: limitKey=%s count=%d timeWindow=%d dryRun=%t", limitKeyFromProperty, countFromProperty, timeWindowFromProperty, dryRunFromProperty)
		limitKey = limitKeyFromProperty
		count = countFromProperty
		timeWindow = timeWindowFromProperty
		dryRun = dryRunFromProperty
	} else if cfg.GlobalThreshold != nil {
		// 全局限流模式
		limitKey = fmt.Sprintf(AiTokenGlobalRateLimitFormat, cfg.RuleName, cfg.GlobalThreshold.TimeWindow, cfg.GlobalThreshold.Count)
		count = cfg.GlobalThreshold.Count
		timeWindow = cfg.GlobalThreshold.TimeWindow
	} else {
		// 规则限流模式
		val, ruleItem, configItem := checkRequestAgainstLimitRule(ctx, cfg.RuleItems)
		if ruleItem == nil || configItem == nil {
			// 没有匹配到限流规则直接返回
			ctx.SetContext(Skip, true)
			ctx.DontReadRequestBody()
			ctx.DontReadResponseBody()
			return types.ActionContinue
		}

		limitKey = fmt.Sprintf(AiTokenRateLimitFormat, cfg.RuleName, ruleItem.LimitType, configItem.TimeWindow, configItem.Count, ruleItem.Key, val)
		count = configItem.Count
		timeWindow = configItem.TimeWindow
	}

	ctx.SetContext(LimitRedisContextKey, LimitRedisContext{
		key:    limitKey,
		count:  count,
		window: timeWindow,
	})

	ctx.SetUserAttribute(UserAttrKeyKey, limitKey)
	ctx.SetUserAttribute(UserAttrThresholdKey, count)
	ctx.SetUserAttribute(UserAttrIntervalKey, timeWindow)

	defer func() {
		_ = ctx.WriteUserAttributeToLogWithKey(LogKey)
	}()

	// 执行限流逻辑
	keys := []interface{}{limitKey}
	args := []interface{}{count, timeWindow}
	err := cfg.RedisClient.Eval(RequestPhaseFixedWindowScript, 1, keys, args, func(response resp.Value) {
		defer func() {
			_ = ctx.WriteUserAttributeToLogWithKey(LogKey)
		}()

		resultArray := response.Array()
		if len(resultArray) != 3 {
			// Server connection error is also possible to reach here, so we log it as error level.
			// response = "cannot connect to redis cluster"

			ctx.SetUserAttribute(UserAttrResultKey, "resp_error")

			log.Errorf("redis response parse error, response: %v", response)
			_ = proxywasm.ResumeHttpRequest()
			return
		}
		context := LimitContext{
			count:     resultArray[0].Integer(),
			remaining: resultArray[1].Integer(),
			reset:     resultArray[2].Integer(),
		}
		ctx.SetUserAttribute(UserAttrRemainingKey, context.remaining)
		if context.remaining < 0 {
			if dryRun {
				ctx.SetUserAttribute(UserAttrResultKey, "reject-dryrun")
			} else {
				ctx.SetUserAttribute(UserAttrResultKey, "reject")
				rejected(cfg, context)
			}
		} else {
			ctx.SetUserAttribute(UserAttrResultKey, "pass")
		}
		_ = proxywasm.ResumeHttpRequest()
	})
	if err != nil {
		ctx.SetUserAttribute(UserAttrResultKey, "call_error")
		log.Errorf("redis call failed: %v", err)
		return types.ActionContinue
	}
	return types.HeaderStopAllIterationAndWatermark
}

func isEnabled(ctx wrapper.HttpContext) bool {
	consumerId, _ := proxywasm.GetHttpRequestHeader(consumerHeader)
	model, _ := proxywasm.GetHttpRequestHeader(modelHeader)

	enabledKeys := []string{}
	if consumerId != "" {
		if model != "" {
			enabledKeys = append(enabledKeys, fmt.Sprintf(enabledByConsumerModelKeyFormat, consumerId, model))
		}
		enabledKeys = append(enabledKeys, fmt.Sprintf(enabledByConsumerKeyFormat, consumerId))
	}
	if model != "" {
		enabledKeys = append(enabledKeys, fmt.Sprintf(enabledByModelKeyFormat, model))
	}
	enabledKeys = append(enabledKeys, enabledGlobalKey)

	for _, key := range enabledKeys {
		if enabled, ok := ctx.GetGlobalConfig(key).(bool); ok {
			log.Debugf("got enabled value from global config with key %s: %t", key, enabled)
			return enabled
		}
	}

	return enabledDefaultValue
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, cfg config.AiTokenRateLimitConfig) types.Action {
	if ctx.GetBoolContext(Skip, false) {
		ctx.DontReadResponseBody()
		return types.ActionContinue
	}

	if status, err := proxywasm.GetHttpResponseHeader(":status"); err != nil || status != "200" {
		if err != nil {
			log.Errorf("unable to load :status header from response: %v", err)
		}
		ctx.DontReadResponseBody()
		return types.ActionContinue
	}

	contentType, _ := proxywasm.GetHttpResponseHeader("content-type")

	if strings.Contains(contentType, "application/json") {
		ctx.SetContext(ResponseTypeKey, ResponseTypeJson)
		// TODO: add a config option to control whether to buffer the response body for json content type or use the streaming parsing approach.
		ctx.BufferResponseBody()
		ctx.SetResponseBodyBufferLimit(defaultMaxBodyBytes)
	} else if strings.Contains(contentType, "text/event-stream") {
		ctx.SetContext(ResponseTypeKey, ResponseTypeSse)
	} else {
		// Other content types, no need to read response body
		ctx.DontReadResponseBody()
	}

	return types.ActionContinue
}

func onHttpStreamingBody(ctx wrapper.HttpContext, cfg config.AiTokenRateLimitConfig, data []byte, endOfStream bool) (ret []byte) {
	ret = data

	if ctx.GetBoolContext(Skip, false) {
		ctx.DontReadResponseBody()
		return
	}

	if !ctx.GetBoolContext(ResponseUsageFoundKey, false) {
		usage := int64(0)

		if responseType, ok := ctx.GetContext(ResponseTypeKey).(ResponseType); ok {
			switch responseType {
			case ResponseTypeJson:
				usage = getUsageFromStreamingBodyJson(ctx, data, endOfStream)
			case ResponseTypeSse:
				usage = getUsageFromStreamingBodySse(ctx, data, endOfStream)
			default:
				// Other unsupported content types, no need to read response body
				ctx.DontReadResponseBody()
				return
			}
		}

		if usage <= 0 {
			return
		}

		ctx.SetContext(ResponseUsageFoundKey, true)

		ctx.SetUserAttribute(UserAttrUsageKey, usage)
		_ = ctx.WriteUserAttributeToLogWithKey(LogKey)

		if limitRedisContext, ok := ctx.GetContext(LimitRedisContextKey).(LimitRedisContext); ok {
			key := limitRedisContext.key
			args := []interface{}{limitRedisContext.count, limitRedisContext.window, usage}
			if err := cfg.RedisClient.Eval(ResponsePhaseFixedWindowScript, 1, []interface{}{key}, args, func(response resp.Value) {
				log.Debugf("=== redis response in response phase: %v ===", response)
				if array := response.Array(); len(array) == 3 {
					tokenRemaining := response.Array()[1].Integer()
					ctx.SetUserAttribute(UserAttrRemainingKey, tokenRemaining)
					_ = ctx.WriteUserAttributeToLogWithKey(LogKey)
				}

				// Inject all cached response body data into the filter chain and resume response processing.
				buffer := getJoinedBufferData(ctx)
				endOfStream := ctx.GetBoolContext(ResponseEndOfStreamKey, false)
				_ = proxywasm.InjectEncodedDataToFilterChain(buffer, endOfStream)
				_ = proxywasm.ResumeHttpResponse()

				// We are done. There is no need to read response body anymore.
				ctx.SetContext(ResponseNeedBuffer, false)
				ctx.DontReadResponseBody()
			}); err != nil {
				log.Errorf("redis call in response phase failed with key %s and usage %d: %v", key, usage, err)
			} else {
				ctx.SetContext(ResponseNeedBuffer, true)
			}
		}
	}

	if ctx.GetBoolContext(ResponseNeedBuffer, false) {
		log.Debugf("=== buffering response data chunk of size %d with eof %t ===", len(data), endOfStream)

		// It is used when resuming the process.
		ctx.SetContext(ResponseEndOfStreamKey, endOfStream)

		ctx.PushBuffer(data)
		ctx.NeedPauseStreamingResponse()

		// Response data is cached inside the plugin. Clear the envoy stream cache.
		ret = []byte{}
	}

	return
}

func onHttpResponseBody(ctx wrapper.HttpContext, cfg config.AiTokenRateLimitConfig, body []byte) types.Action {
	var usage int64
	if totalTokens := gjson.GetBytes(body, "usage.total_tokens"); totalTokens.Exists() && totalTokens.Type == gjson.Number {
		usage = totalTokens.Int()
	} else {
		log.Debugf("usage.total_tokens field not found in response body or is not a number, skipping usage extraction")
		return types.ActionContinue
	}

	ctx.SetUserAttribute(UserAttrUsageKey, usage)
	_ = ctx.WriteUserAttributeToLogWithKey(LogKey)

	if limitRedisContext, ok := ctx.GetContext(LimitRedisContextKey).(LimitRedisContext); !ok {
		return types.ActionContinue
	} else {
		key := limitRedisContext.key
		args := []interface{}{limitRedisContext.count, limitRedisContext.window, usage}
		if err := cfg.RedisClient.Eval(ResponsePhaseFixedWindowScript, 1, []interface{}{key}, args, func(response resp.Value) {
			log.Debugf("=== redis response in response phase: %v ===", response)
			if array := response.Array(); len(array) == 3 {
				tokenRemaining := response.Array()[1].Integer()
				ctx.SetUserAttribute(UserAttrRemainingKey, tokenRemaining)
				_ = ctx.WriteUserAttributeToLogWithKey(LogKey)
			}
			_ = proxywasm.ResumeHttpResponse()
		}); err != nil {
			log.Errorf("redis call in response phase failed with key %s and usage %d: %v", key, usage, err)
			return types.ActionContinue
		} else {
			return types.ActionPause
		}
	}
}

func getUsageFromStreamingBodyJson(ctx wrapper.HttpContext, data []byte, endOfStream bool) int64 {
	if ctx.GetBoolContext(SkipStreamingJsonProcessing, false) {
		ctx.DontReadResponseBody()
		return -1
	}

	buffer, _ := ctx.GetContext(StreamingBodyBufferKey).(json.ScrollableBuffer)
	if buffer == nil {
		buffer = json.NewScrollableBuffer()
		ctx.SetContext(StreamingBodyBufferKey, buffer)
	}

	reader, _ := ctx.GetContext(StreamingJsonReaderKey).(json.JsonReader)
	if reader == nil {
		reader = json.NewJsonReader(buffer)
		ctx.SetContext(StreamingJsonReaderKey, reader)
	}

	_ = buffer.Append(data, endOfStream)

	usageObject, _ := ctx.GetContext(UsageObject).(map[string]interface{})
	if usageObject == nil {
		usageObject = make(map[string]interface{})
		ctx.SetContext(UsageObject, usageObject)
	}

	var jsonToken json.JsonToken
	var err error
	for docEnded := false; !docEnded; {
		jsonToken, err = reader.Peek()
		if err != nil {
			if !errors.Is(err, json.ErrEndOfBuffer) {
				log.Errorf("error peeking json token: %v", err)
				ctx.SetContext(SkipStreamingJsonProcessing, true)
			}
			return -1
		}
		switch jsonToken {
		case json.JsonTokenBeginObject:
			log.Tracef("==== Beginning JSON object at path %s ====", reader.GetPath())
			_ = reader.BeginObject()
		case json.JsonTokenEndObject:
			log.Tracef("==== Ending JSON object at path %s ====", reader.GetPath())
			_ = reader.EndObject()
		case json.JsonTokenBeginArray:
			log.Tracef("==== Beginning JSON array at path %s ====", reader.GetPath())
			_ = reader.BeginArray()
		case json.JsonTokenEndArray:
			log.Tracef("==== Ending JSON array at path %s ====", reader.GetPath())
			_ = reader.EndArray()
		case json.JsonTokenName:
			if _, err := reader.NextName(); err != nil {
				log.Errorf("error reading json name: %v", err)
				if !errors.Is(err, json.ErrEndOfBuffer) {
					ctx.SetContext(SkipStreamingJsonProcessing, true)
				}
				return -1
			} else if !strings.HasPrefix(reader.GetPath(), "$.usage") {
				log.Tracef("skipping json token name: %s", reader.GetPath())
				_ = reader.SkipValue()
			}
		case json.JsonTokenNumber:
			if reader.GetPath() != "$.usage.total_tokens" {
				log.Tracef("==== Skipping non-total_tokens numeric value at path %s ====", reader.GetPath())
				_ = reader.SkipValue()
			} else if value, err := reader.NextLong(); err != nil {
				if !errors.Is(err, json.ErrEndOfBuffer) {
					log.Errorf("error reading total_tokens value: %v", err)
					ctx.SetContext(SkipStreamingJsonProcessing, true)
				}
				log.Tracef("error reading total_tokens value: %v", err)
				return -1
			} else {
				log.Tracef("==== Extracted total_tokens from JSON response: %d ====", value)
				return value
			}
		case json.JsonTokenString, json.JsonTokenNull, json.JsonTokenBoolean:
			log.Tracef("==== Skipping non-numeric usage value at path %s ====", reader.GetPath())
			_ = reader.SkipValue()
		case json.JsonTokenEndDocument:
			log.Tracef("==== End of JSON document reached ====")
			docEnded = true
		default:
			// Unknown token, skip processing further
		}
	}

	return 0
}

func getUsageFromStreamingBodySse(ctx wrapper.HttpContext, data []byte, endOfStream bool) int64 {
	streamEvents := ExtractStreamingEvents(ctx, data)

	for _, event := range streamEvents {
		usage := gjson.Get(event.Data, "usage")
		if !usage.Exists() || !usage.IsObject() {
			continue
		}
		if totalTokens := usage.Get("total_tokens"); totalTokens.Exists() && totalTokens.Type == gjson.Number {
			return totalTokens.Int()
		}
	}

	return -1
}

func getJoinedBufferData(ctx wrapper.HttpContext) []byte {
	joinedBuffer := make([]byte, 0)
	for ctx.BufferQueueSize() > 0 {
		buffer := ctx.PopBuffer()
		joinedBuffer = append(joinedBuffer, buffer...)
	}
	return joinedBuffer
}

func checkRequestAgainstLimitRule(ctx wrapper.HttpContext, ruleItems []config.LimitRuleItem) (string, *config.LimitRuleItem, *config.LimitConfigItem) {
	if len(ruleItems) > 0 {
		for _, rule := range ruleItems {
			val, ruleItem, configItem := hitRateRuleItem(ctx, rule)
			if ruleItem != nil && configItem != nil {
				return val, ruleItem, configItem
			}
		}
	}
	return "", nil, nil
}

func hitRateRuleItem(ctx wrapper.HttpContext, rule config.LimitRuleItem) (string, *config.LimitRuleItem, *config.LimitConfigItem) {
	switch rule.LimitType {
	// 根据HTTP请求头限流
	case config.LimitByHeaderType, config.LimitByPerHeaderType:
		val, err := proxywasm.GetHttpRequestHeader(rule.Key)
		if err != nil {
			return logDebugAndReturnEmpty("failed to get request header %s: %v", rule.Key, err)
		}
		return val, &rule, findMatchingItem(rule.LimitType, rule.ConfigItems, val)
	// 根据HTTP请求参数限流
	case config.LimitByParamType, config.LimitByPerParamType:
		parse, err := url.Parse(ctx.Path())
		if err != nil {
			return logDebugAndReturnEmpty("failed to parse request path: %v", err)
		}
		query, err := url.ParseQuery(parse.RawQuery)
		if err != nil {
			return logDebugAndReturnEmpty("failed to parse query params: %v", err)
		}
		val, ok := query[rule.Key]
		if !ok {
			return logDebugAndReturnEmpty("request param %s is empty", rule.Key)
		}
		return val[0], &rule, findMatchingItem(rule.LimitType, rule.ConfigItems, val[0])
	// 根据consumer限流
	case config.LimitByConsumerType, config.LimitByPerConsumerType:
		val, err := proxywasm.GetHttpRequestHeader(util.ConsumerHeader)
		if err != nil {
			return logDebugAndReturnEmpty("failed to get request header %s: %v", util.ConsumerHeader, err)
		}
		return val, &rule, findMatchingItem(rule.LimitType, rule.ConfigItems, val)
	// 根据cookie中key值限流
	case config.LimitByCookieType, config.LimitByPerCookieType:
		cookie, err := proxywasm.GetHttpRequestHeader(CookieHeader)
		if err != nil {
			return logDebugAndReturnEmpty("failed to get request cookie : %v", err)
		}
		val := util.ExtractCookieValueByKey(cookie, rule.Key)
		if val == "" {
			return logDebugAndReturnEmpty("cookie key '%s' extracted from cookie '%s' is empty.", rule.Key, cookie)
		}
		return val, &rule, findMatchingItem(rule.LimitType, rule.ConfigItems, val)
	// 根据客户端IP限流
	case config.LimitByPerIpType:
		realIp, err := getDownStreamIp(rule)
		if err != nil {
			log.Warnf("failed to get down stream ip: %v", err)
			return "", &rule, nil
		}
		for _, item := range rule.ConfigItems {
			if _, found, _ := item.IpNet.Get(realIp); !found {
				continue
			}
			return realIp.String(), &rule, &item
		}
	}
	return "", nil, nil
}

func logDebugAndReturnEmpty(errMsg string, args ...interface{}) (string, *config.LimitRuleItem, *config.LimitConfigItem) {
	log.Debugf(errMsg, args...)
	return "", nil, nil
}

func findMatchingItem(limitType config.LimitRuleItemType, items []config.LimitConfigItem, key string) *config.LimitConfigItem {
	for _, item := range items {
		// per类型,检查allType和regexpType
		if limitType == config.LimitByPerHeaderType ||
			limitType == config.LimitByPerParamType ||
			limitType == config.LimitByPerConsumerType ||
			limitType == config.LimitByPerCookieType {
			if item.ConfigType == config.AllType || (item.ConfigType == config.RegexpType && item.Regexp.MatchString(key)) {
				return &item
			}
		}
		// 其他类型,直接比较key
		if item.Key == key {
			return &item
		}
	}
	return nil
}

func getDownStreamIp(rule config.LimitRuleItem) (net.IP, error) {
	var (
		realIpStr string
		err       error
	)
	if rule.LimitByPerIp.SourceType == config.HeaderSourceType {
		realIpStr, err = proxywasm.GetHttpRequestHeader(rule.LimitByPerIp.HeaderName)
		if err == nil {
			realIpStr = strings.Split(strings.Trim(realIpStr, " "), ",")[0]
		}
	} else {
		var bs []byte
		bs, err = proxywasm.GetProperty([]string{"source", "address"})
		realIpStr = string(bs)
	}
	if err != nil {
		return nil, err
	}
	ip := util.ParseIP(realIpStr)
	realIP := net.ParseIP(ip)
	if realIP == nil {
		return nil, fmt.Errorf("invalid ip[%s]", ip)
	}
	return realIP, nil
}

func generateMetricName(route, cluster, model, consumer, metricName string) string {
	return fmt.Sprintf("route.%s.upstream.%s.model.%s.consumer.%s.metric.%s", route, cluster, model, consumer, metricName)
}

func rejected(cfg config.AiTokenRateLimitConfig, context LimitContext) {
	headers := make(map[string][]string)
	headers[RateLimitResetHeader] = []string{strconv.Itoa(context.reset)}
	rejectedCode := cfg.RejectedCode
	if rejectedCode == 0 {
		rejectedCode = config.DefaultRejectedCode
	}
	rejectedMsg := cfg.RejectedMsg
	if rejectedMsg == "" {
		rejectedMsg = config.DefaultRejectedMsg
	}
	_ = proxywasm.SendHttpResponseWithDetail(
		rejectedCode, "ai-token-ratelimit.rejected", util.ReconvertHeaders(headers), []byte(rejectedMsg), -1)

	//route, _ := util.GetRouteName()
	//cluster, _ := util.GetClusterName()
	//consumer, _ := util.GetConsumer()
	//cfg.IncrementCounter(generateMetricName(route, cluster, "none", consumer, TokenRateLimitCount), 1)
}

func getContextValuesFromProperty() (limitKey string, count int64, timeWindow int64, dryRun bool, succ bool) {
	limitKey = ""
	count = 0
	timeWindow = 0
	succ = false

	if limitKeyBytes, err := proxywasm.GetProperty(tokenRatelimitKeyPropertyPath); err == nil && limitKeyBytes != nil {
		limitKey = string(limitKeyBytes)
	} else {
		if err != nil && !errors.Is(err, types.ErrorStatusNotFound) {
			log.Warnf("failed to get limit key from property: %v", err)
		}
		return
	}

	if countBytes, err := proxywasm.GetProperty(tokenRatelimitCountPropertyPath); err == nil && countBytes != nil {
		count = int64(bytesToUint32(countBytes))
	} else {
		if err != nil && !errors.Is(err, types.ErrorStatusNotFound) {
			log.Warnf("failed to get count from property: %v", err)
		}
		return
	}

	if timeWindowBytes, err := proxywasm.GetProperty(tokenRatelimitTimeWindowPropertyPath); err == nil && timeWindowBytes != nil {
		timeWindow = int64(bytesToUint32(timeWindowBytes))
	} else {
		if err != nil && !errors.Is(err, types.ErrorStatusNotFound) {
			log.Warnf("failed to get time window from property: %v", err)
		}
		return
	}

	if dryRunBytes, err := proxywasm.GetProperty(tokenRatelimitDryRunPropertyPath); err == nil && dryRunBytes != nil {
		dryRun = bytesToUint32(dryRunBytes) != 0
	} else {
		if err != nil && !errors.Is(err, types.ErrorStatusNotFound) {
			log.Warnf("failed to get dry-run flag from property: %v", err)
		}
		// dry-run flag is optional, we can proceed even if it's not found
		dryRun = false
	}

	succ = true
	return
}

func bytesToUint32(bs []byte) uint32 {
	if len(bs) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(bs)
}
