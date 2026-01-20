package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm"
	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/tokenusage"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func main() {}

func init() {
	wrapper.SetCtx(
		"ai-statistics",
		wrapper.ParseConfig(parseConfig),
		wrapper.ProcessRequestHeaders(onHttpRequestHeaders),
		wrapper.ProcessStreamingRequestBody(onHttpStreamingRequestBody),
		wrapper.ProcessRequestBody(onHttpRequestBody),
		wrapper.ProcessResponseHeaders(onHttpResponseHeaders),
		wrapper.ProcessStreamingResponseBody(onHttpStreamingResponseBody),
		wrapper.ProcessResponseBody(onHttpResponseBody),
		wrapper.WithRebuildAfterRequests[AIStatisticsConfig](1000),
		wrapper.WithRebuildMaxMemBytes[AIStatisticsConfig](200*1024*1024),
	)
}

type ApiName string

const (
	ApiNameUndetermined    ApiName = "undetermined"
	ApiNameChatCompletions         = "chat_completions"
	ApiNameCompletions             = "completions"
)

const (
	defaultMaxBodyBytes uint32 = 100 * 1024 * 1024

	// Context consts
	StatisticsRequestStartTime             = "ai-statistics-request-start-time"
	StatisticsFirstTokenTime               = "ai-statistics-first-token-time"
	RouteName                              = "route"
	ClusterName                            = "cluster"
	APIName                                = "api"
	ConsumerKey                            = "x-mse-consumer"
	RequestPath                            = "request_path"
	SkipProcessing                         = "skip_processing"
	LogRawResponseBody                     = "log_raw_response_body"
	StreamingResponseBodyLogged            = "streaming_response_body_logged"
	StreamEventFoundInResponseBody         = "stream_event_found_in_response_body"
	SkipStreamingRequestBodyLogProcessing  = "skip_streaming_request_body_log_processing"
	SkipStreamingResponseBodyLogProcessing = "skip_streaming_response_body_log_processing"
	IsSseResponse                          = "is_sse_response"
	SseStreamStopped                       = "sse_stream_stopped"
	NeedDropAdditionalUsageChunk           = "need_drop_additional_usage_chunk"
	RequestApiName                         = "request_api_name"

	// AI API Paths
	PathOpenAIChatCompletions       = "/chat/completions"
	PathOpenAICompletions           = "/completions"
	PathOpenAIEmbeddings            = "/embeddings"
	PathOpenAIModels                = "/models"
	PathGeminiGenerateContent       = "/generateContent"
	PathGeminiStreamGenerateContent = "/streamGenerateContent"

	// Source Type
	FixedValue            = "fixed_value"
	RequestHeader         = "request_header"
	RequestBody           = "request_body"
	ResponseHeader        = "response_header"
	ResponseStreamingBody = "response_streaming_body"
	ResponseBody          = "response_body"

	// Inner metric & log attributes
	LLMFirstTokenDuration  = "llm_first_token_duration"
	LLMServiceDuration     = "llm_service_duration"
	LLMDurationCount       = "llm_duration_count"
	LLMStreamDurationCount = "llm_stream_duration_count"
	ResponseType           = "response_type"
	ChatID                 = "chat_id"
	ChatRound              = "chat_round"
	RequestModelOriginal   = "request_model_original"
	RequestModelFinal      = "request_model_final"
	Usage                  = "usage"

	ResponseTypeNormal = "normal"
	ResponseTypeStream = "stream"

	// Inner span attributes
	ArmsSpanKind     = "gen_ai.span.kind"
	ArmsModelName    = "gen_ai.model_name"
	ArmsRequestModel = "gen_ai.request.model"
	ArmsInputToken   = "gen_ai.usage.input_tokens"
	ArmsOutputToken  = "gen_ai.usage.output_tokens"
	ArmsTotalToken   = "gen_ai.usage.total_tokens"

	// Extract Rule
	RuleFirst   = "first"
	RuleReplace = "replace"
	RuleAppend  = "append"

	defaultRequestBodyLengthLimit  = 10 * 1024 * 1024
	defaultResponseBodyLengthLimit = 10 * 1024 * 1024
)

var (
	builtInAttributes = []Attribute{
		{
			Key:         Usage,
			Value:       "usage",
			ValueSource: ResponseStreamingBody,
			Rule:        RuleReplace,
			ApplyToLog:  true,
		},
		{
			Key:         Usage,
			Value:       "usage",
			ValueSource: ResponseBody,
			ApplyToLog:  true,
		},
		{
			Key:         RequestModelOriginal,
			Value:       "x-higress-llm-model",
			ValueSource: RequestHeader,
			ApplyToLog:  true,
		},
		{
			Key:         RequestModelFinal,
			Value:       "model",
			ValueSource: RequestBody,
			ApplyToLog:  true,
		},
	}

	bodyToLongPlaceholder      = "too long"
	bodyToLongPlaceholderBytes = []byte(bodyToLongPlaceholder)
	bodyRedactingOptions       = &sjson.Options{Optimistic: true, ReplaceInPlace: false}

	pathSuffixesNeedDropAdditionalUsageChunk = []string{
		PathOpenAIChatCompletions,
	}
)

// TracingSpan is the tracing span configuration.
type Attribute struct {
	Key                string `json:"key"`
	ValueSource        string `json:"value_source"`
	Value              string `json:"value"`
	TraceSpanKey       string `json:"trace_span_key,omitempty"`
	DefaultValue       string `json:"default_value,omitempty"`
	Rule               string `json:"rule,omitempty"`
	ApplyToLog         bool   `json:"apply_to_log,omitempty"`
	ApplyToSpan        bool   `json:"apply_to_span,omitempty"`
	AsSeparateLogField bool   `json:"as_separate_log_field,omitempty"`
}

var (
	defaultEnablePathSuffixes = []string{
		"/completions",
		"/embeddings",
		"/images/generations",
		"/audio/speech",
		"/fine_tuning/jobs",
		"/moderations",
		"/image-synthesis",
		"/video-synthesis",
	}
	defaultEnablePathKeywords = []string{
		"/llm/",
	}
)

type AIStatisticsConfig struct {
	// Metrics
	// TODO: add more metrics in Gauge and Histogram format
	counterMetrics map[string]proxywasm.MetricCounter
	// Attributes to be recorded in log & span
	attributes []Attribute
	// If disableOpenaiUsage is true, model/input_token/output_token logs will be skipped
	disableOpenaiUsage bool
	valueLengthLimit   int
	// The max length of request body to be logged
	requestBodyLengthLimit int
	// The max length of response body to be logged
	responseBodyLengthLimit int
	// Path suffixes to enable the plugin on
	enablePathSuffixes []string
	// Path keywords to enable the plugin on
	enablePathKeywords []string
	// Content types to enable response body buffering
	enableContentTypes []string
}

func generateMetricName(route, cluster, model, consumer, metricName string) string {
	return fmt.Sprintf("route.%s.upstream.%s.model.%s.consumer.%s.metric.%s", route, cluster, model, consumer, metricName)
}

func getRouteName() (string, error) {
	if raw, err := proxywasm.GetProperty([]string{"route_name"}); err != nil {
		return "-", err
	} else {
		return string(raw), nil
	}
}

func getAPIName() (string, error) {
	if raw, err := proxywasm.GetProperty([]string{"route_name"}); err != nil {
		return "-", err
	} else {
		parts := strings.Split(string(raw), "@")
		if len(parts) != 5 {
			return "-", errors.New("not api type")
		} else {
			return strings.Join(parts[:3], "@"), nil
		}
	}
}

func getClusterName() (string, error) {
	if raw, err := proxywasm.GetProperty([]string{"cluster_name"}); err != nil {
		return "-", err
	} else {
		return string(raw), nil
	}
}

func (config *AIStatisticsConfig) incrementCounter(metricName string, inc uint64) {
	if inc == 0 {
		return
	}
	counter, ok := config.counterMetrics[metricName]
	if !ok {
		counter = proxywasm.DefineCounterMetric(metricName)
		config.counterMetrics[metricName] = counter
	}
	counter.Increment(inc)
}

// isPathEnabled checks if the request path matches any of the configured criteria
func isPathEnabled(pathWithoutQuery string, config *AIStatisticsConfig) bool {
	enabledSuffixes := config.enablePathSuffixes
	if len(enabledSuffixes) == 0 {
		return true // If no path suffixes configured, enable for all
	}

	// Check if path ends with any enabled suffix
	for _, suffix := range enabledSuffixes {
		if strings.HasSuffix(pathWithoutQuery, suffix) {
			return true
		}
	}

	// Check if path contains any enabled keyword
	enabledKeywords := config.enablePathKeywords
	if len(enabledKeywords) != 0 {
		for _, keyword := range enabledKeywords {
			if strings.Contains(pathWithoutQuery, keyword) {
				return true
			}
		}
	}

	return false
}

// isContentTypeEnabled checks if the content type matches any of the enabled content types
func isContentTypeEnabled(contentType string, enabledContentTypes []string) bool {
	if len(enabledContentTypes) == 0 {
		return true // If no content types configured, enable for all
	}

	for _, enabledType := range enabledContentTypes {
		if strings.Contains(contentType, enabledType) {
			return true
		}
	}
	return false
}

func parseConfig(configJson gjson.Result, config *AIStatisticsConfig) error {
	// Parse tracing span attributes setting.
	attributeConfigs := configJson.Get("attributes").Array()
	if configJson.Get("value_length_limit").Exists() {
		config.valueLengthLimit = int(configJson.Get("value_length_limit").Int())
	} else {
		config.valueLengthLimit = 4000
	}
	if requestBodyLengthLimit := configJson.Get("request_body_length_limit"); requestBodyLengthLimit.Exists() {
		config.requestBodyLengthLimit = int(requestBodyLengthLimit.Int())
	} else {
		config.requestBodyLengthLimit = defaultRequestBodyLengthLimit
	}
	if responseBodyLengthLimit := configJson.Get("response_body_length_limit"); responseBodyLengthLimit.Exists() {
		config.responseBodyLengthLimit = int(responseBodyLengthLimit.Int())
	} else {
		config.responseBodyLengthLimit = defaultResponseBodyLengthLimit
	}
	config.attributes = make([]Attribute, len(attributeConfigs))
	for i, attributeConfig := range attributeConfigs {
		attribute := Attribute{}
		err := json.Unmarshal([]byte(attributeConfig.Raw), &attribute)
		if err != nil {
			log.Errorf("parse config failed, %v", err)
			return err
		}
		if attribute.Rule != "" && attribute.Rule != RuleFirst && attribute.Rule != RuleReplace && attribute.Rule != RuleAppend {
			return errors.New("value of rule must be one of [nil, first, replace, append]")
		}
		config.attributes[i] = attribute
	}
	config.attributes = addBuiltInAttributes(config.attributes)
	// Metric settings
	config.counterMetrics = make(map[string]proxywasm.MetricCounter)

	// Parse openai usage config setting.
	config.disableOpenaiUsage = configJson.Get("disable_openai_usage").Bool()

	// Parse path suffix configuration
	pathSuffixes := configJson.Get("enable_path_suffixes").Array()
	config.enablePathSuffixes = make([]string, 0, len(defaultEnablePathSuffixes)+len(pathSuffixes))
	config.enablePathSuffixes = append(config.enablePathSuffixes, defaultEnablePathSuffixes...)

	for _, suffix := range pathSuffixes {
		suffixStr := suffix.String()
		if suffixStr == "*" {
			// Clear the suffixes list since * means all paths are enabled
			config.enablePathSuffixes = make([]string, 0)
			break
		}
		config.enablePathSuffixes = append(config.enablePathSuffixes, suffixStr)
	}

	// Parse path keyword configuration
	pathKeywords := configJson.Get("enable_path_keywords").Array()
	config.enablePathKeywords = make([]string, 0, len(defaultEnablePathKeywords)+len(pathKeywords))
	config.enablePathKeywords = append(config.enablePathKeywords, defaultEnablePathKeywords...)

	for _, keyword := range pathKeywords {
		config.enablePathKeywords = append(config.enablePathKeywords, keyword.String())
	}

	// Parse content type configuration
	contentTypes := configJson.Get("enable_content_types").Array()
	config.enableContentTypes = make([]string, 0, len(contentTypes))

	for _, contentType := range contentTypes {
		contentTypeStr := contentType.String()
		if contentTypeStr == "*" {
			// Clear the content types list since * means all content types are enabled
			config.enableContentTypes = make([]string, 0)
			break
		}
		config.enableContentTypes = append(config.enableContentTypes, contentTypeStr)
	}

	return nil
}

func addBuiltInAttributes(attributes []Attribute) []Attribute {
	for _, builtInAttr := range builtInAttributes {
		if !hasAttribute(attributes, builtInAttr.Key, builtInAttr.ValueSource) {
			attributes = append(attributes, builtInAttr)
		} else {
			log.Debugf("Skip adding built-in attribute %s from source %s as it already exists in config", builtInAttr.Key, builtInAttr.ValueSource)
		}
	}
	return attributes
}

func hasAttribute(attributes []Attribute, key string, source string) bool {
	if len(attributes) == 0 {
		return false
	}
	for _, attribute := range attributes {
		if attribute.Key == key && (source == "" || (strings.HasPrefix(attribute.ValueSource, "response_") && attribute.ValueSource == source)) {
			return true
		}
	}
	return false
}

func onHttpRequestHeaders(ctx wrapper.HttpContext, config AIStatisticsConfig) types.Action {
	// Check if request path matches enabled suffixes
	requestPath, _ := proxywasm.GetHttpRequestHeader(":path")
	requestPath = strings.SplitN(requestPath, "?", 2)[0]
	if !isPathEnabled(requestPath, &config) {
		log.Debugf("ai-statistics: skipping request for path %s (not in enabled suffixes)", requestPath)
		// Set skip processing flag and avoid reading request/response body
		ctx.SetContext(SkipProcessing, true)
		ctx.DontReadRequestBody()
		ctx.DontReadResponseBody()
		return types.ActionContinue
	}

	apiName := ApiNameUndetermined
	if strings.HasSuffix(requestPath, PathOpenAIChatCompletions) {
		apiName = ApiNameChatCompletions
	} else if strings.HasSuffix(requestPath, PathOpenAICompletions) {
		apiName = ApiNameCompletions
	}
	ctx.SetContext(RequestApiName, apiName)

	ctx.DisableReroute()
	route, _ := getRouteName()
	cluster, _ := getClusterName()
	api, apiError := getAPIName()
	if apiError == nil {
		route = api
	}
	ctx.SetContext(RouteName, route)
	ctx.SetContext(ClusterName, cluster)
	ctx.SetUserAttribute(APIName, apiName)
	ctx.SetContext(StatisticsRequestStartTime, time.Now().UnixMilli())
	if requestPath, _ := proxywasm.GetHttpRequestHeader(":path"); requestPath != "" {
		ctx.SetContext(RequestPath, requestPath)
	}
	if consumer, _ := proxywasm.GetHttpRequestHeader(ConsumerKey); consumer != "" {
		ctx.SetContext(ConsumerKey, consumer)
	}

	ctx.SetRequestBodyBufferLimit(defaultMaxBodyBytes)

	// Set span attributes for ARMS.
	setSpanAttribute(ArmsSpanKind, "LLM")
	// Set user defined log & span attributes which type is fixed_value
	setAttributeBySource(ctx, config, FixedValue, nil, nil)
	// Set user defined log & span attributes which type is request_header
	setAttributeBySource(ctx, config, RequestHeader, nil, nil)

	if ctx.HasRequestBody() {
		if contentType, _ := proxywasm.GetHttpRequestHeader("content-type"); strings.Contains(contentType, "json") {
			// For non-JSON content types, we don't buffer the body here.
			// Just process it in onHttpStreamingRequestBody.
			ctx.BufferRequestBody()
		}
		// Delay the header flushing to the next filter and buffer the body for once.
		// Because if we don't do this, following plugins may just return an error when processing the header,
		// causing us unable to log the request body at all.
		return types.HeaderStopIteration
	}

	return types.ActionContinue
}

func onHttpStreamingRequestBody(ctx wrapper.HttpContext, config AIStatisticsConfig, chunk []byte, isLastChunk bool) (modifiedChunk []byte) {
	log.Debugf("processing streaming request body, len(chunk)=%d isLastChunk=%t", len(chunk), isLastChunk)
	// We don't modify chunk data here. Just log it.
	modifiedChunk = chunk

	// Check if processing should be skipped
	if ctx.GetBoolContext(SkipProcessing, false) {
		return
	}
	if ctx.GetBoolContext(SkipStreamingRequestBodyLogProcessing, false) {
		return
	}

	cachedRequestBody := ctx.GetByteSliceContext(RequestBody, nil)
	if cachedRequestBody == nil {
		cachedRequestBody = chunk
	} else if len(cachedRequestBody)+len(chunk) < config.requestBodyLengthLimit {
		cachedRequestBody = append(cachedRequestBody, chunk...)
	} else {
		bytesToBeAdded := config.requestBodyLengthLimit - len(cachedRequestBody)
		if bytesToBeAdded > 0 {
			cachedRequestBody = append(cachedRequestBody, chunk[:bytesToBeAdded]...)
		}
		ctx.SetContext(SkipStreamingRequestBodyLogProcessing, true)
	}
	ctx.SetContext(RequestBody, cachedRequestBody)

	// Both header and received body will be passed to the following filter in the chain after return.
	// If following plugins may just return an error when processing the header or the first chunk of body,
	// breaking the filter chain, we won't be able to log the request body at all.
	// So we perform the logging here when processing both the first chunk and the last chunk,
	// especially for route_not_found cases.
	isFirstChunk := ctx.GetUserAttribute(RequestBody) == nil
	if isFirstChunk || isLastChunk {
		ctx.SetUserAttribute(RequestBody, string(cachedRequestBody))
		_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)
	}

	return
}

func onHttpRequestBody(ctx wrapper.HttpContext, config AIStatisticsConfig, body []byte) types.Action {
	// Check if processing should be skipped
	if ctx.GetBoolContext(SkipProcessing, false) {
		return types.ActionContinue
	}

	ctx.SetUserAttribute(RequestBody, getRequestBodyToLog(ctx, body, &config))

	// Set user defined log & span attributes.
	setAttributeBySource(ctx, config, RequestBody, body, nil)
	// Set span attributes for ARMS.
	requestModel := "UNKNOWN"
	if model := gjson.GetBytes(body, "model"); model.Exists() {
		requestModel = model.String()
	} else {
		requestPath := ctx.GetStringContext(RequestPath, "")
		if strings.Contains(requestPath, "generateContent") || strings.Contains(requestPath, "streamGenerateContent") { // Google Gemini GenerateContent
			reg := regexp.MustCompile(`^.*/(?P<api_version>[^/]+)/models/(?P<model>[^:]+):\w+Content$`)
			matches := reg.FindStringSubmatch(requestPath)
			if len(matches) == 3 {
				requestModel = matches[2]
			}
		}
	}
	setSpanAttribute(ArmsRequestModel, requestModel)
	// Set the number of conversation rounds

	userPromptCount := 0
	if messages := gjson.GetBytes(body, "messages"); messages.Exists() && messages.IsArray() {
		for _, msg := range messages.Array() {
			if msg.Get("role").String() == "user" {
				userPromptCount += 1
			}
		}
	} else if contents := gjson.GetBytes(body, "contents"); contents.Exists() && contents.IsArray() { // Google Gemini GenerateContent
		for _, content := range contents.Array() {
			if !content.Get("role").Exists() || content.Get("role").String() == "user" {
				userPromptCount += 1
			}
		}
	}
	ctx.SetUserAttribute(ChatRound, userPromptCount)

	responseType := ResponseTypeNormal
	if stream := gjson.GetBytes(body, "stream"); stream.Exists() && stream.IsBool() {
		if stream.Bool() {
			responseType = ResponseTypeStream
		}
	}
	setResponseType(ctx, responseType, true)

	// Write log
	_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)

	markForResponsePostProcesses(ctx, body)

	return types.ActionContinue
}

func getRequestBodyToLog(ctx wrapper.HttpContext, body []byte, config *AIStatisticsConfig) string {
	requestBodyLengthLimit := config.requestBodyLengthLimit
	if len(body) <= requestBodyLengthLimit {
		return string(body)
	}

	apiName, _ := ctx.GetContext(RequestApiName).(ApiName)
	log.Debugf("request body length %d exceeds limit %d, start redacting for api %s", len(body), config.requestBodyLengthLimit, apiName)
	switch apiName {
	case ApiNameChatCompletions:
		return getRequestBodyToLogForChatCompletions(body, requestBodyLengthLimit)
	case ApiNameCompletions:
		return getRequestBodyToLogForCompletions(body, requestBodyLengthLimit)
	default:
		return bodyToLongPlaceholder
	}
}

func getRequestBodyToLogForChatCompletions(body []byte, limit int) string {
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		log.Debugf("redacting request body 'tools' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "tools", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact request body 'tools' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted request body 'tools' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if responseFormat := gjson.GetBytes(body, "response_format"); responseFormat.Exists() {
		log.Debugf("redacting request body 'response_format' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "response_format", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact request body 'response_format' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted request body 'response_format' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if messagesLength := int(gjson.GetBytes(body, "messages.#").Int()); messagesLength > 0 {
		for i := 0; i < messagesLength; i++ {
			itemPath := fmt.Sprintf("messages.%d", i)
			log.Debugf("redacting request body at %s, current length %d", itemPath, len(body))
			if redactedBody, err := sjson.SetBytesOptions(body, itemPath, bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
				log.Errorf("failed to redact request body at %s: %v", itemPath, err)
			} else {
				body = redactedBody
				log.Debugf("request body redacted at %s, new length %d", itemPath, len(redactedBody))
				if len(redactedBody) <= limit {
					return string(body)
				}
			}
		}
	}

	return bodyToLongPlaceholder
}

func getRequestBodyToLogForCompletions(body []byte, limit int) string {
	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		log.Debugf("redacting request body 'tools' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "tools", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact request body 'tools' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted request body 'tools' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if responseFormat := gjson.GetBytes(body, "response_format"); responseFormat.Exists() {
		log.Debugf("redacting request body 'response_format' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "response_format", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact request body 'response_format' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted request body 'response_format' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if prompt := gjson.GetBytes(body, "prompt"); prompt.Exists() {
		log.Debugf("redacting request body 'prompt' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "prompt", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact request body 'prompt' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted request body 'prompt' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	return bodyToLongPlaceholder
}

func onHttpResponseHeaders(ctx wrapper.HttpContext, config AIStatisticsConfig) types.Action {
	contentType, _ := proxywasm.GetHttpResponseHeader("content-type")

	// Comment by Trip.com: disable content type filtering for response body processing since we only support JSON and event-stream now.
	//if !isContentTypeEnabled(contentType, config.enableContentTypes) {
	//	log.Debugf("ai-statistics: skipping response for content type %s (not in enabled content types)", contentType)
	//	// Set skip processing flag and avoid reading response body
	//	ctx.SetContext(SkipProcessing, true)
	//	ctx.DontReadResponseBody()
	//	return types.ActionContinue
	//}

	if strings.Contains(contentType, "application/json") {
		// Non-streaming JSON response body will be processed in onHttpResponseBody
		ctx.BufferResponseBody()
	} else if strings.Contains(contentType, "text/event-stream") {
		// Do nothing, streaming body will be processed in onHttpStreamingResponseBody
		setResponseType(ctx, ResponseTypeStream, false)
		ctx.SetContext(IsSseResponse, true)
	} else {
		// Other content types, just log the raw response body
		ctx.SetContext(LogRawResponseBody, true)
	}

	// Set user defined log & span attributes.
	setAttributeBySource(ctx, config, ResponseHeader, nil, nil)

	return types.ActionContinue
}

func onHttpStreamingResponseBody(ctx wrapper.HttpContext, config AIStatisticsConfig, data []byte, endOfStream bool) []byte {
	// Check if processing should be skipped
	if ctx.GetBoolContext(SkipProcessing, false) {
		return data
	}

	// Get requestStartTime from http context
	requestStartTime, _ := ctx.GetContext(StatisticsRequestStartTime).(int64)

	if !ctx.GetBoolContext(LogRawResponseBody, false) {
		// there is no need for attribute extraction when raw logging is enabled
		if chatID := wrapper.GetValueFromBody(data, []string{
			"id",
			"response.id",
			"responseId", // Gemini generateContent
			"message.id", // anthropic messages
		}); chatID != nil {
			ctx.SetUserAttribute(ChatID, chatID.String())
		}

		// If this is the first chunk, record first token duration metric and span attribute
		if requestStartTime > 0 && ctx.GetContext(StatisticsFirstTokenTime) == nil {
			firstTokenTime := time.Now().UnixMilli()
			ctx.SetContext(StatisticsFirstTokenTime, firstTokenTime)
			ctx.SetUserAttribute(LLMFirstTokenDuration, firstTokenTime-requestStartTime)
		}

		// Set information about this request
		if !config.disableOpenaiUsage {
			if usage := tokenusage.GetTokenUsage(ctx, data); usage.TotalToken > 0 {
				// Set span attributes for ARMS.
				setSpanAttribute(ArmsTotalToken, usage.TotalToken)
				setSpanAttribute(ArmsModelName, usage.Model)
				setSpanAttribute(ArmsInputToken, usage.InputToken)
				setSpanAttribute(ArmsOutputToken, usage.OutputToken)
			}
		}
	}

	var streamEvents []StreamEvent
	if ctx.GetBoolContext(IsSseResponse, false) {
		streamEvents = ExtractStreamingEvents(ctx, data)
	}
	setAttributeBySource(ctx, config, ResponseStreamingBody, data, streamEvents)

	data = postProcessResponseData(ctx, config, data, streamEvents)

	// If the end of the stream is reached, record metrics/logs/spans.
	if endOfStream {
		if requestStartTime > 0 {
			responseEndTime := time.Now().UnixMilli()
			ctx.SetUserAttribute(LLMServiceDuration, responseEndTime-requestStartTime)
		}

		// Write log
		_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)

		// Write metrics
		writeMetric(ctx, config)
	} else if !ctx.GetBoolContext(StreamingResponseBodyLogged, false) {
		// For streaming response, we only log once when the first non-empty streaming body chunk is received,
		// to avoid missing logs in case of stream interruption.
		ctx.SetContext(StreamingResponseBodyLogged, true)
		_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)
	}

	return data
}

func onHttpResponseBody(ctx wrapper.HttpContext, config AIStatisticsConfig, body []byte) types.Action {
	// Check if processing should be skipped
	if ctx.GetBoolContext(SkipProcessing, false) {
		return types.ActionContinue
	}

	// Get requestStartTime from http context
	requestStartTime, _ := ctx.GetContext(StatisticsRequestStartTime).(int64)

	responseEndTime := time.Now().UnixMilli()
	ctx.SetUserAttribute(LLMServiceDuration, responseEndTime-requestStartTime)

	setResponseType(ctx, ResponseTypeNormal, false)
	if chatID := wrapper.GetValueFromBody(body, []string{
		"id",
		"response.id",
		"responseId", // Gemini generateContent
		"message.id", // anthropic messages
	}); chatID != nil {
		ctx.SetUserAttribute(ChatID, chatID.String())
	}

	// Set information about this request
	if !config.disableOpenaiUsage {
		if usage := tokenusage.GetTokenUsage(ctx, body); usage.TotalToken > 0 {
			// Set span attributes for ARMS.
			setSpanAttribute(ArmsModelName, usage.Model)
			setSpanAttribute(ArmsInputToken, usage.InputToken)
			setSpanAttribute(ArmsOutputToken, usage.OutputToken)
			setSpanAttribute(ArmsTotalToken, usage.TotalToken)
		}
	}

	ctx.SetUserAttribute(ResponseBody, getResponseBodyToLog(ctx, body, &config))

	// Set user defined log & span attributes.
	setAttributeBySource(ctx, config, ResponseBody, body, nil)

	// Write log
	_ = ctx.WriteUserAttributeToLogWithKey(wrapper.AILogKey)

	// Write metrics
	writeMetric(ctx, config)

	return types.ActionContinue
}

func getResponseBodyToLog(ctx wrapper.HttpContext, body []byte, config *AIStatisticsConfig) string {
	responseBodyLengthLimit := config.responseBodyLengthLimit
	if len(body) <= responseBodyLengthLimit {
		return string(body)
	}

	apiName, _ := ctx.GetContext(RequestApiName).(ApiName)
	log.Debugf("response body length %d exceeds limit %d, start redacting for api %s", len(body), config.requestBodyLengthLimit, apiName)
	switch apiName {
	case ApiNameChatCompletions, ApiNameCompletions:
		return getResponseBodyToLogForCompletions(body, responseBodyLengthLimit)
	default:
		return bodyToLongPlaceholder
	}
}

func getResponseBodyToLogForCompletions(body []byte, limit int) string {
	if choicesLength := int(gjson.GetBytes(body, "choices.#").Int()); choicesLength > 0 {
		for i := 0; i < choicesLength; i++ {
			itemPath := fmt.Sprintf("choices.%d", i)
			log.Debugf("redacting response body at %s, current length %d", itemPath, len(body))
			if redactedBody, err := sjson.SetBytesOptions(body, itemPath, bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
				log.Errorf("failed to redact response body at %s: %v", itemPath, err)
			} else {
				body = redactedBody
				log.Debugf("response body redacted at %s, new length %d", itemPath, len(redactedBody))
				if len(redactedBody) <= limit {
					return string(redactedBody)
				}
			}
		}
	}

	newBody := ""

	if id := gjson.GetBytes(body, "id"); id.Exists() {
		newBody, _ = sjson.SetRaw(newBody, "id", id.Raw)
	} else {
		newBody, _ = sjson.Set(newBody, "id", "")
	}
	if usage := gjson.GetBytes(body, "usage"); usage.Exists() {
		newBody, _ = sjson.SetRaw(newBody, "usage", usage.Raw)
	} else {
		newBody, _ = sjson.SetRaw(newBody, "usage", "{}")
	}

	return newBody
}

func setResponseType(ctx wrapper.HttpContext, responseType string, overwrite bool) {
	if overwrite || ctx.GetUserAttribute(ResponseType) == nil {
		ctx.SetUserAttribute(ResponseType, responseType)
	}
}

// fetches the tracing span value from the specified source.

func setAttributeBySource(ctx wrapper.HttpContext, config AIStatisticsConfig, source string, body []byte, streamEvents []StreamEvent) {
	if source == ResponseStreamingBody {
		logRawResponseBody := ctx.GetBoolContext(LogRawResponseBody, false)

		if !logRawResponseBody {
			// for a potential streaming body, we need to extract streaming events first for attribute extraction
			log.Debugf("parsing streaming body chunks for attribute extraction")
			// get buffered streaming body chunks for one-time to reduce memory footprint.
			combineStreamingEventsForLog(ctx, &config, streamEvents)
		}

		if !ctx.GetBoolContext(StreamEventFoundInResponseBody, false) {
			// as long as no streaming event hasn't been found in the response body,
			// we can directly use the raw response body for logging just in case that the body isn't a streaming one.
			appendRawStreamingResponseBodyToLog(ctx, &config, body)
		}

		if logRawResponseBody {
			// skip attribute extraction from streaming body when just logging raw response body
			return
		}
	}

	for _, attribute := range config.attributes {
		var key string
		var value interface{}
		if source == attribute.ValueSource {
			key = attribute.Key
			switch source {
			case FixedValue:
				value = attribute.Value
			case RequestHeader:
				value, _ = proxywasm.GetHttpRequestHeader(attribute.Value)
			case RequestBody:
				value = gjson.GetBytes(body, attribute.Value).Value()
			case ResponseHeader:
				value, _ = proxywasm.GetHttpResponseHeader(attribute.Value)
			case ResponseStreamingBody:
				value = extractStreamingBodyByJsonPath(streamEvents, attribute.Value, attribute.Rule, ctx.GetUserAttribute(key))
			case ResponseBody:
				value = gjson.GetBytes(body, attribute.Value).Value()
			default:
			}
			if (value == nil || value == "") && attribute.DefaultValue != "" {
				value = attribute.DefaultValue
			}
			if len(fmt.Sprint(value)) > config.valueLengthLimit {
				value = fmt.Sprint(value)[:config.valueLengthLimit/2] + " [truncated] " + fmt.Sprint(value)[len(fmt.Sprint(value))-config.valueLengthLimit/2:]
			}
			log.Debugf("[attribute] source type: %s, key: %s, value: %+v", source, key, value)
			if attribute.ApplyToLog {
				if attribute.AsSeparateLogField {
					marshalledJsonStr := wrapper.MarshalStr(fmt.Sprint(value))
					if err := proxywasm.SetProperty([]string{key}, []byte(marshalledJsonStr)); err != nil {
						log.Warnf("failed to set %s in filter state, raw is %s, err is %v", key, marshalledJsonStr, err)
					}
				} else {
					ctx.SetUserAttribute(key, value)
				}
			}
			// for metrics
			if key == tokenusage.CtxKeyModel || key == tokenusage.CtxKeyInputToken || key == tokenusage.CtxKeyOutputToken || key == tokenusage.CtxKeyTotalToken {
				ctx.SetContext(key, value)
			}
			if attribute.ApplyToSpan {
				if attribute.TraceSpanKey != "" {
					key = attribute.TraceSpanKey
				}
				setSpanAttribute(key, value)
			}
		}
	}
}

func appendRawStreamingResponseBodyToLog(ctx wrapper.HttpContext, config *AIStatisticsConfig, body []byte) {
	if ctx.GetContext(SkipStreamingResponseBodyLogProcessing) != nil {
		log.Debugf("skipping streaming response body log processing as per context flag")
		return
	}
	responseBody, _ := ctx.GetUserAttribute(ResponseBody).(string)
	responseBody += string(body)
	// use a smaller limit for raw appending since it may contain binary data which needs more space after encoding
	responseBodyLengthLimit := config.responseBodyLengthLimit / 5
	if len(responseBody) > responseBodyLengthLimit {
		responseBody = responseBody[:responseBodyLengthLimit] + "[truncated]"
		ctx.SetContext(SkipStreamingResponseBodyLogProcessing, true)
	}
	ctx.SetUserAttribute(ResponseBody, responseBody)
}

func combineStreamingEventsForLog(ctx wrapper.HttpContext, config *AIStatisticsConfig, events []StreamEvent) {
	log.Debugf("adding %d streaming events to the full response body for log", len(events))
	if events == nil || len(events) == 0 {
		return
	}

	if !ctx.GetBoolContext(StreamEventFoundInResponseBody, false) {
		ctx.SetContext(StreamEventFoundInResponseBody, true)
		// reset response body for combining
		ctx.SetUserAttribute(ResponseBody, "")
	}

	if ctx.GetContext(SkipStreamingResponseBodyLogProcessing) != nil {
		log.Debugf("skipping streaming response body log processing as per context flag")
		return
	}

	responseBody, _ := ctx.GetUserAttribute(ResponseBody).(string)

	processedFields := make(map[string]bool)
	for i, event := range events {
		log.Debugf("processing streaming event %d: %s", i, event.Data)
		dataJson := gjson.Parse(event.Data)
		if !dataJson.Exists() || !dataJson.IsObject() {
			log.Debugf("streaming event %d's data does not contain a valid JSON object", i)
			continue
		}
		dataJson.ForEach(func(key, value gjson.Result) bool {
			log.Debugf("processing key %s in streaming event %d", key.Str, i)
			if key.Str == "choices" && value.IsArray() {
				// Only support the default choice or choice with index 0 for now
				choice := value.Get("0")
				if index := choice.Get("index"); !index.Exists() || index.Type == gjson.Number && index.Int() == 0 {
					responseBody = combineStreamingChoice(responseBody, 0, &choice)
				}
			} else if !processedFields[key.Str] {
				if newBody, err := sjson.SetRaw(responseBody, key.Str, value.Raw); err == nil {
					responseBody = newBody
					if value.Type != gjson.Null {
						// only mark non-null fields as processed to allow it to be updated later
						processedFields[key.Str] = true
					}
				} else {
					log.Errorf("failed to combine streaming body for log at key %s: %v", key.Str, err)
				}
			}
			return true
		})
		if len(responseBody) > config.responseBodyLengthLimit {
			log.Debugf("combined response body length for log exceeds limit %d, stop processing further streaming events", config.responseBodyLengthLimit)
			ctx.SetContext(SkipStreamingResponseBodyLogProcessing, true)
		} else {
			ctx.SetUserAttribute(ResponseBody, responseBody)
		}
	}
}

func combineStreamingChoice(body string, choiceIndex int, choice *gjson.Result) string {
	choicePath := fmt.Sprintf("choices.%d", choiceIndex)
	if currentChoice := gjson.Get(body, choicePath); !currentChoice.Exists() {
		// initialize the choice object
		if newBody, err := sjson.SetRaw(body, choicePath, choice.Raw); err == nil {
			body = newBody
		} else {
			log.Errorf("failed to init streaming choice object for log at index %d: %v", choiceIndex, err)
		}
		return body
	}

	body = combineStreamingStringField(body, choicePath+".delta.content", choice, "delta.content")
	body = combineStreamingStringField(body, choicePath+".delta.reasoning_content", choice, "delta.reasoning_content")
	body = combineStreamingStringField(body, choicePath+".text", choice, "text") // for /v1/completions api
	body = combineStreamingChoiceToolCalls(body, choiceIndex, choice)
	return body
}

func combineStreamingChoiceToolCalls(body string, choiceIndex int, choice *gjson.Result) string {
	inputToolCalls := choice.Get("delta.tool_calls")
	if !inputToolCalls.Exists() || !inputToolCalls.IsArray() {
		return body
	}

	// TODO: Only support one tool call for now
	inputToolCall := inputToolCalls.Get("0")

	bodyFieldPath := fmt.Sprintf("choices.%d.delta.tool_calls.0", choiceIndex)
	bodyToolCall := gjson.Get(body, bodyFieldPath).String()
	log.Debugf("current streaming choice tool call for log at key %s: %s", bodyFieldPath, bodyToolCall)
	if bodyToolCall == "" {
		bodyToolCall = inputToolCall.Raw
	} else {
		bodyToolCall = combineStreamingStringField(bodyToolCall, "function.name", &inputToolCall, "function.name")
		bodyToolCall = combineStreamingStringField(bodyToolCall, "function.arguments", &inputToolCall, "function.arguments")
	}
	log.Debugf("combined streaming choice delta tool call for log at key %s: %s", bodyFieldPath, bodyToolCall)
	if newBody, err := sjson.SetRaw(body, bodyFieldPath, bodyToolCall); err == nil {
		body = newBody
	} else {
		log.Errorf("failed to combine streaming choice delta value for log at key %s: %v", bodyFieldPath, err)
	}
	return body
}

func combineStreamingStringField(body string, bodyFieldPath string, json *gjson.Result, jsonFieldPath string) string {
	currentValue := gjson.Get(body, bodyFieldPath).String()
	log.Debugf("current streaming value for log at key %s: %s", bodyFieldPath, currentValue)
	newValue := currentValue + json.Get(jsonFieldPath).String()
	log.Debugf("combined streaming value for log at key %s: %s", bodyFieldPath, newValue)
	if newBody, err := sjson.Set(body, bodyFieldPath, newValue); err == nil {
		body = newBody
	} else {
		log.Errorf("failed to combine streaming value for log at key %s: %v", bodyFieldPath, err)
	}
	return body
}

func extractStreamingBodyByJsonPath(events []StreamEvent, jsonPath string, rule string, currentValue interface{}) interface{} {
	if events == nil || len(events) == 0 {
		// no events found, return current value directly
		return currentValue
	}
	// always start with current value
	value := currentValue
	if rule == RuleFirst {
		if currentValue == nil {
			// only extract when current value is nil
			for _, event := range events {
				jsonObj := gjson.Get(event.Data, jsonPath)
				if jsonObj.Exists() {
					value = jsonObj.Value()
					break
				}
			}
		}
	} else if rule == RuleReplace {
		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]
			jsonObj := gjson.Get(event.Data, jsonPath)
			// use the first non-null value from the end
			if jsonObj.Exists() && jsonObj.Type != gjson.Null {
				value = jsonObj.Value()
				break
			}
		}
	} else if rule == RuleAppend {
		// extract llm response
		var strValue string
		if currentStrValue, isStr := currentValue.(string); isStr {
			strValue = currentStrValue
		}
		for _, event := range events {
			jsonObj := gjson.Get(event.Data, jsonPath)
			if jsonObj.Exists() {
				strValue += jsonObj.String()
			}
		}
		value = strValue
	} else {
		log.Errorf("unsupported rule type: %s", rule)
	}
	return value
}

// Set the tracing span with value.
func setSpanAttribute(key string, value interface{}) {
	if value != "" {
		traceSpanTag := wrapper.TraceSpanTagPrefix + key
		if e := proxywasm.SetProperty([]string{traceSpanTag}, []byte(fmt.Sprint(value))); e != nil {
			log.Warnf("failed to set %s in filter state: %v", traceSpanTag, e)
		}
	} else {
		log.Debugf("failed to write span attribute [%s], because it's value is empty", key)
	}
}

func markForResponsePostProcesses(ctx wrapper.HttpContext, requestBody []byte) {
	path := ctx.Path()
	for _, suffix := range pathSuffixesNeedDropAdditionalUsageChunk {
		if !strings.HasSuffix(path, suffix) {
			continue
		}
		if includeUsage := gjson.GetBytes(requestBody, "stream_options.include_usage").Bool(); !includeUsage {
			// If the request doesn't have stream_options.include_usage enabled,
			// we shall drop the additional usage chunk in response streaming body
			// to prevent potential errors on client side caused by the empty choices array.
			ctx.SetContext(NeedDropAdditionalUsageChunk, true)
		}
		break
	}
}

func postProcessResponseData(ctx wrapper.HttpContext, config AIStatisticsConfig, data []byte, events []StreamEvent) (outputData []byte) {
	outputData = data

	// Drop additional usage chunk if needed
	if ctx.GetBoolContext(NeedDropAdditionalUsageChunk, false) && len(events) > 0 {
		changed := false
		for i := 0; i < len(events); i++ {
			event := events[i]
			log.Debugf("response streaming event %d: %s", i, event.Data)
			if !ctx.GetBoolContext(SseStreamStopped, false) {
				if isStopEvent(&event) {
					log.Debugf("found stop event in response streaming body: %s", event.Data)
					ctx.SetContext(SseStreamStopped, true)
				}
			} else if isAdditionalUsageChunk(&event) {
				// Remove the event
				log.Debugf("dropping additional usage chunk in response streaming body: %s", event.Data)
				events = append(events[:i], events[i+1:]...)
				i-- // adjust index after removal
				changed = true
			}

		}
		if changed {
			// reconstruct the data
			var reconstructedData strings.Builder
			for _, event := range events {
				reconstructedData.WriteString(event.ToHttpString())
			}
			outputData = []byte(reconstructedData.String())
		}
	}

	return
}

func isStopEvent(event *StreamEvent) bool {
	if event == nil {
		return false
	}
	finishReason := gjson.Get(event.Data, "choices.0.finish_reason")
	return finishReason.Exists() && finishReason.String() != ""
}

func isAdditionalUsageChunk(event *StreamEvent) bool {
	if event == nil {
		return false
	}
	if choiceCount := gjson.Get(event.Data, "choices.#").Int(); choiceCount != 0 {
		return false
	}
	return gjson.Get(event.Data, "usage").Exists()
}

func writeMetric(ctx wrapper.HttpContext, config AIStatisticsConfig) {
	// Generate usage metrics
	var ok bool
	var route, cluster, model string
	consumer := ctx.GetStringContext(ConsumerKey, "none")
	route, ok = ctx.GetContext(RouteName).(string)
	if !ok {
		log.Info("RouteName type assert failed, skip metric record")
		return
	}
	cluster, ok = ctx.GetContext(ClusterName).(string)
	if !ok {
		log.Info("ClusterName type assert failed, skip metric record")
		return
	}

	if config.disableOpenaiUsage {
		return
	}

	if ctx.GetUserAttribute(tokenusage.CtxKeyModel) == nil || ctx.GetUserAttribute(tokenusage.CtxKeyInputToken) == nil || ctx.GetUserAttribute(tokenusage.CtxKeyOutputToken) == nil || ctx.GetUserAttribute(tokenusage.CtxKeyTotalToken) == nil {
		log.Info("get usage information failed, skip metric record")
		return
	}
	model, ok = ctx.GetUserAttribute(tokenusage.CtxKeyModel).(string)
	if !ok {
		log.Info("Model type assert failed, skip metric record")
		return
	}
	if inputToken, ok := convertToUInt(ctx.GetUserAttribute(tokenusage.CtxKeyInputToken)); ok {
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, tokenusage.CtxKeyInputToken), inputToken)
	} else {
		log.Info("InputToken type assert failed, skip metric record")
	}
	if outputToken, ok := convertToUInt(ctx.GetUserAttribute(tokenusage.CtxKeyOutputToken)); ok {
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, tokenusage.CtxKeyOutputToken), outputToken)
	} else {
		log.Info("OutputToken type assert failed, skip metric record")
	}
	if totalToken, ok := convertToUInt(ctx.GetUserAttribute(tokenusage.CtxKeyTotalToken)); ok {
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, tokenusage.CtxKeyTotalToken), totalToken)
	} else {
		log.Info("TotalToken type assert failed, skip metric record")
	}

	// Generate duration metrics
	var llmFirstTokenDuration, llmServiceDuration uint64
	// Is stream response
	if ctx.GetUserAttribute(LLMFirstTokenDuration) != nil {
		llmFirstTokenDuration, ok = convertToUInt(ctx.GetUserAttribute(LLMFirstTokenDuration))
		if !ok {
			log.Info("LLMFirstTokenDuration type assert failed")
			return
		}
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, LLMFirstTokenDuration), llmFirstTokenDuration)
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, LLMStreamDurationCount), 1)
	}
	if ctx.GetUserAttribute(LLMServiceDuration) != nil {
		llmServiceDuration, ok = convertToUInt(ctx.GetUserAttribute(LLMServiceDuration))
		if !ok {
			log.Warnf("LLMServiceDuration type assert failed")
			return
		}
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, LLMServiceDuration), llmServiceDuration)
		config.incrementCounter(generateMetricName(route, cluster, model, consumer, LLMDurationCount), 1)
	}
}

func convertToUInt(val interface{}) (uint64, bool) {
	switch v := val.(type) {
	case float32:
		return uint64(v), true
	case float64:
		return uint64(v), true
	case int32:
		return uint64(v), true
	case int64:
		return uint64(v), true
	case uint32:
		return uint64(v), true
	case uint64:
		return v, true
	default:
		return 0, false
	}
}
