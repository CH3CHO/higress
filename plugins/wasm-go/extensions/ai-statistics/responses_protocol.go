package main

import (
	"fmt"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func getRequestBodyToLogForResponses(body []byte, limit int) string {
	originalBody := body

	if tools := gjson.GetBytes(body, "tools"); tools.Exists() {
		log.Debugf("redacting responses request body 'tools' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "tools", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact responses request body 'tools' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted responses request body 'tools' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if instructions := gjson.GetBytes(body, "instructions"); instructions.Exists() {
		log.Debugf("redacting responses request body 'instructions' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "instructions", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact responses request body 'instructions' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted responses request body 'instructions' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	if input := gjson.GetBytes(body, "input"); input.Exists() {
		log.Debugf("redacting responses request body 'input' field, current length %d", len(body))
		if redactedBody, err := sjson.SetBytesOptions(body, "input", bodyToLongPlaceholderBytes, bodyRedactingOptions); err != nil {
			log.Errorf("failed to redact responses request body 'input' field: %v", err)
		} else {
			body = redactedBody
			log.Debugf("redacted responses request body 'input' field, new length %d", len(body))
			if len(body) <= limit {
				return string(body)
			}
		}
	}

	request := gjson.ParseBytes(originalBody)
	if !request.Exists() || !request.IsObject() {
		return bodyToLongPlaceholder
	}

	summaryBuilder := newJSONObjectBuilder()
	for _, field := range []string{
		"model",
		"stream",
		"store",
		"truncation",
		"previous_response_id",
		"max_output_tokens",
		"max_tool_calls",
		"temperature",
		"top_p",
		"tool_choice",
		"parallel_tool_calls",
	} {
		summaryBuilder.AddResultFieldIfFits(field, request.Get(field), limit)
	}

	if input := request.Get("input"); input.Exists() {
		switch {
		case input.IsArray():
			summaryBuilder.AddRawField("input_count", fmt.Sprint(len(input.Array())))
		case input.IsObject():
			summaryBuilder.AddRawField("input_type", `"object"`)
		case input.Type == gjson.String:
			summaryBuilder.AddRawField("input_type", `"string"`)
		}
		appendTruncatedJSONResultFieldForLog(summaryBuilder, "input", input, limit)
	}
	if tools := request.Get("tools"); tools.Exists() && tools.IsArray() {
		summaryBuilder.AddRawField("tools_count", fmt.Sprint(len(tools.Array())))
	}

	appendTruncatedJSONResultFieldForLog(summaryBuilder, "instructions", request.Get("instructions"), limit)

	if summaryBuilder.ExtraValueCapacity("request_truncated", limit) >= len("true") {
		summaryBuilder.AddRawField("request_truncated", "true")
	}
	if !summaryBuilder.HasField() {
		return bodyToLongPlaceholder
	}

	summary := summaryBuilder.String()
	if len(summary) > limit {
		return bodyToLongPlaceholder
	}

	return summary
}

// aggregateResponsesStreamingEventsForLog 会把一批 Responses SSE event
// 折叠成用于最终日志的 ResponseBody。
// 目标是尽量还原成非流式 /v1/responses 的 body 形态，方便最终
// ai_log.response_body 保留一份可读的聚合结果。
func aggregateResponsesStreamingEventsForLog(ctx wrapper.HttpContext, config *AIStatisticsConfig, events []StreamEvent) {
	// TODO：这里与 Chat Completions、Anthropic 的流式日志路径骨架相同，
	// 后续可抽通用累加循环，统一处理 skipContent、mergeUsage 和超限 compact。
	responseBody := getStreamingResponseBodyForLogAccumulator(ctx)

	for _, event := range events {
		// 内容累加守卫：超限后跳过内容类 parse/累加（含 response.completed 整体覆盖），
		// 防止批内峰值放大与 CPU 回归。usage 走旁路提取，保证 body 里 usage 段始终落库。
		skipContent := ctx.GetBoolContext(SkipStreamingResponseBodyContentAccumulation, false)
		if skipContent {
			mergeResponsesUsageIntoBody(&responseBody, event.Data)
			continue
		}
		responseBody = applyResponsesStreamingEventForLog(responseBody, &event)
		mergeResponsesUsageIntoBody(&responseBody, event.Data)
		if len(responseBody) > config.responseBodyLengthLimit {
			responseBody = compactStreamingResponseBodyForLog(ctx, config, responseBody)
			if len(responseBody) > config.responseBodyLengthLimit {
				responseBody = boundedBodyToLongPlaceholder(config.responseBodyLengthLimit)
			}
			ctx.SetContext(SkipStreamingResponseBodyContentAccumulation, true)
		}
	}

	if responseBody == "" {
		return
	}

	setStreamingResponseBodyForLog(ctx, config, responseBody)
}

// mergeResponsesUsageIntoBody 在内容累加超限后，从 Responses 流式 event 中提取
// 稳定根字段（含 id、status、model、usage 等）合入 body，但不触碰 output 内容。
// 这样既避免重新塞回超长 content，又保证 id、usage 等关键字段落库。
func mergeResponsesUsageIntoBody(body *string, data string) {
	parsed := gjson.Parse(data)
	if !parsed.Exists() || !parsed.IsObject() {
		return
	}
	var response gjson.Result
	if r := parsed.Get("response"); r.Exists() && r.IsObject() {
		response = r
	} else if parsed.Get("id").Exists() || parsed.Get("usage").Exists() {
		response = parsed
	} else {
		return
	}
	*body = mergeResponsesRootForLog(*body, response)
}

// applyResponsesStreamingEventForLog 会把一个 Responses SSE event 应用到
// 当前聚合中的 body 上。这里不只是“追加”：
// - delta 类 event 会追加 text / arguments
// - added/done 类 event 会设置结构化字段
// - created/in_progress 类 event 会合并顶层元数据
// - completed/failed/incomplete 类 event 可能直接用最终 response 整体覆盖
func applyResponsesStreamingEventForLog(body string, event *StreamEvent) string {
	if event == nil || event.Data == "" {
		return body
	}

	data := gjson.Parse(event.Data)
	if !data.Exists() || !data.IsObject() {
		return body
	}

	eventType := data.Get("type").String()
	if eventType == "" {
		eventType = event.Event
	}

	switch eventType {
	case "response.created", "response.in_progress":
		body = mergeResponsesRootForLog(body, data.Get("response"))
	case "response.output_item.added", "response.output_item.done":
		body = setJSONRawField(body, fmt.Sprintf("output.%d", int(data.Get("output_index").Int())), firstExistingResult(data, "item", "output_item"))
	case "response.content_part.added", "response.content_part.done":
		body = setJSONRawField(body, fmt.Sprintf("output.%d.content.%d", int(data.Get("output_index").Int()), int(data.Get("content_index").Int())), firstExistingResult(data, "part", "content_part"))
	case "response.output_text.delta":
		body = updateResponsesOutputTextForLog(body, data, true)
	case "response.output_text.done":
		body = updateResponsesOutputTextForLog(body, data, false)
	case "response.function_call_arguments.delta":
		body = updateResponsesFunctionCallArgumentsForLog(body, data, true)
	case "response.function_call_arguments.done":
		body = updateResponsesFunctionCallArgumentsForLog(body, data, false)
	case "response.completed", "response.failed", "response.incomplete":
		if response := data.Get("response"); response.Exists() && response.IsObject() {
			body = response.Raw
		}
	default:
		if response := data.Get("response"); response.Exists() && response.IsObject() {
			body = mergeResponsesRootForLog(body, response)
		}
	}

	return body
}

// mergeResponsesRootForLog 只合并顶层稳定字段。
// 这些字段即使后续为了日志长度做压缩，也应该尽量保留下来。
func mergeResponsesRootForLog(body string, response gjson.Result) string {
	if !response.Exists() || !response.IsObject() {
		return body
	}

	for _, field := range []string{"id", "object", "created_at", "completed_at", "status", "background", "error", "incomplete_details", "model"} {
		body = setJSONRawField(body, field, response.Get(field))
	}
	body = setJSONRawField(body, "usage", response.Get("usage"))

	return body
}

func updateResponsesOutputTextForLog(body string, data gjson.Result, appendDelta bool) string {
	outputIndex := int(data.Get("output_index").Int())
	contentIndex := int(data.Get("content_index").Int())
	contentPath := fmt.Sprintf("output.%d.content.%d", outputIndex, contentIndex)

	body = setJSONFieldIfAbsent(body, contentPath+".type", "output_text")

	text := data.Get("text").String()
	if appendDelta {
		text = gjson.Get(body, contentPath+".text").String() + data.Get("delta").String()
	}
	if text == "" && !appendDelta {
		return body
	}
	return setJSONStringField(body, contentPath+".text", text)
}

func updateResponsesFunctionCallArgumentsForLog(body string, data gjson.Result, appendDelta bool) string {
	outputIndex := int(data.Get("output_index").Int())
	outputPath := fmt.Sprintf("output.%d", outputIndex)

	body = setJSONFieldIfAbsent(body, outputPath+".type", "function_call")
	body = setJSONRawField(body, outputPath+".id", data.Get("item_id"))
	body = setJSONRawField(body, outputPath+".call_id", data.Get("call_id"))
	body = setJSONRawField(body, outputPath+".name", data.Get("name"))

	args := data.Get("arguments")
	if appendDelta || !args.Exists() {
		args = data.Get("delta")
	}
	if !args.Exists() {
		return body
	}

	value := args.String()
	if appendDelta {
		value = gjson.Get(body, outputPath+".arguments").String() + value
	}
	return setJSONStringField(body, outputPath+".arguments", value)
}

// compactResponsesBodyForLog 会在裁剪超长 output 的同时保留根字段和 usage。
// 这里改成“按预算渐进构建摘要”：
// 先稳定保留根字段，再按剩余长度预算尽量追加 output，避免反复重建整段 JSON。
func compactResponsesBodyForLog(body string, limit int) string {
	if limit <= 0 || len(body) <= limit {
		return body
	}

	if tools := gjson.Get(body, "tools"); tools.Exists() {
		log.Debugf("redacting responses response body 'tools' field, current length %d", len(body))
		redactedBody, err := sjson.SetBytesOptions([]byte(body), "tools", bodyToLongPlaceholderBytes, bodyRedactingOptions)
		if err != nil {
			log.Errorf("failed to redact responses response body 'tools' field: %v", err)
		} else {
			body = string(redactedBody)
			log.Debugf("redacted responses response body 'tools' field, new length %d", len(body))
			if len(body) <= limit {
				return body
			}
		}
	}

	response := gjson.Parse(body)
	if !response.Exists() || !response.IsObject() {
		return bodyToLongPlaceholder
	}

	if summary := buildResponsesSummaryForLog(response, limit); summary != "" && len(summary) <= limit {
		return summary
	}

	return bodyToLongPlaceholder
}

// buildResponsesSummaryForLog 会先构造固定根字段，再按剩余 budget 逐步补 output。
// 这样只需要构造一次根摘要，后续是否保留 output 由剩余预算决定。
func buildResponsesSummaryForLog(response gjson.Result, limit int) string {
	summaryBuilder := newJSONObjectBuilder()
	for _, field := range []string{"id", "object", "created_at", "completed_at", "status", "background", "error", "incomplete_details", "model"} {
		summaryBuilder.AddResultField(field, response.Get(field))
	}
	summaryBuilder.AddResultField("usage", response.Get("usage"))
	summaryBuilder.AddRawField("output_truncated", "true")

	summary := summaryBuilder.String()
	if len(summary) > limit {
		return ""
	}

	output := response.Get("output")
	if !output.Exists() || !output.IsArray() {
		return summary
	}

	remainingBudget := limit - len(summary) - len(`,"output":`)
	if remainingBudget < 2 {
		return summary
	}

	outputSummary := buildJSONArrayForLog(output.Array(), remainingBudget, buildResponsesOutputItemForLog)
	if outputSummary == "" {
		return summary
	}

	return summary[:len(summary)-1] + `,"output":` + outputSummary + `}`
}

// buildResponsesOutputItemForLog 会优先保留 output item 的稳定元数据，
// 再用剩余预算尽量补 arguments 和 content。
func buildResponsesOutputItemForLog(item gjson.Result, budget int) string {
	if !item.Exists() || !item.IsObject() {
		return ""
	}

	summaryBuilder := newJSONObjectBuilder()
	for _, field := range []string{"id", "type", "status", "role", "phase", "call_id", "name"} {
		summaryBuilder.AddResultFieldIfFits(field, item.Get(field), budget)
	}

	appendTruncatedJSONResultFieldForLog(summaryBuilder, "arguments", item.Get("arguments"), budget)

	content := item.Get("content")
	if content.Exists() && content.IsArray() {
		contentSummary := buildJSONArrayForLog(content.Array(), summaryBuilder.ExtraValueCapacity("content", budget), buildResponsesContentPartForLog)
		if contentSummary != "" {
			summaryBuilder.AddRawField("content", contentSummary)
		}
	}

	if !summaryBuilder.HasField() {
		return ""
	}

	return summaryBuilder.String()
}

// buildJSONArrayForLog 负责在总 budget 内尽量多保留数组元素。
// 每个元素的具体摘要策略由 buildItem 控制；一旦下一个元素放不下就停止追加。
func buildJSONArrayForLog(items []gjson.Result, budget int, buildItem func(gjson.Result, int) string) string {
	if budget < 2 || len(items) == 0 {
		return ""
	}

	builder := newJSONArrayBuilder()
	for _, item := range items {
		if !item.Exists() || !item.IsObject() {
			continue
		}

		itemSummary := buildItem(item, builder.ExtraValueCapacity(budget))
		if itemSummary == "" {
			break
		}
		builder.AddRawValue(itemSummary)
	}

	if !builder.HasValue() {
		return ""
	}

	return builder.String()
}

func buildResponsesContentPartForLog(part gjson.Result, budget int) string {
	if !part.Exists() || !part.IsObject() {
		return ""
	}

	summaryBuilder := newJSONObjectBuilder()
	summaryBuilder.AddResultFieldIfFits("type", part.Get("type"), budget)
	if !summaryBuilder.HasField() {
		return ""
	}

	for _, field := range []string{"text", "refusal"} {
		appendTruncatedJSONResultFieldForLog(summaryBuilder, field, part.Get(field), budget)
	}

	return summaryBuilder.String()
}

func setJSONRawField(body string, path string, value gjson.Result) string {
	if !value.Exists() {
		return body
	}
	if newBody, err := sjson.SetRaw(body, path, value.Raw); err == nil {
		return newBody
	}
	return body
}

func firstExistingResult(data gjson.Result, paths ...string) gjson.Result {
	for _, path := range paths {
		value := data.Get(path)
		if value.Exists() {
			return value
		}
	}
	return gjson.Result{}
}

func setJSONStringField(body string, path string, value string) string {
	if newBody, err := sjson.Set(body, path, value); err == nil {
		return newBody
	}
	return body
}

// setJSONFieldIfAbsent 只在字段缺失时写入给定值，不会覆盖已有值。
func setJSONFieldIfAbsent(body string, path string, value interface{}) string {
	if gjson.Get(body, path).Exists() {
		return body
	}
	if newBody, err := sjson.Set(body, path, value); err == nil {
		return newBody
	}
	return body
}

func isResponsesBuiltInStreamingUsageAttribute(apiName ApiName, attribute Attribute) bool {
	return apiName == ApiNameResponses &&
		attribute.Key == Usage &&
		attribute.ValueSource == ResponseStreamingBody &&
		attribute.Value == "usage" &&
		attribute.Rule == RuleReplace
}

func extractLatestStreamingResultByJsonPath(events []StreamEvent, jsonPath string) gjson.Result {
	for i := len(events) - 1; i >= 0; i-- {
		result := gjson.Get(events[i].Data, jsonPath)
		if result.Exists() && result.Type != gjson.Null {
			return result
		}
	}
	return gjson.Result{}
}
