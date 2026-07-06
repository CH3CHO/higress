package provider

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	// ctxKeyResponsesViaChatCompletionsBridge 保存流式响应转换器实例。
	// 它不是开关，而是用于在多个 response chunk 之间延续 output index、
	// reasoning、tool call 等 Responses 转换状态。
	ctxKeyResponsesViaChatCompletionsBridge = "responsesViaChatCompletionsBridge"
	// ctxKeyResponsesViaChatCompletionsBridgeActivated 表示当前请求已在 request headers
	// 阶段判定要走 Responses via Chat Completions 桥接。request body 阶段据此把
	// Responses 请求体转成 Chat Completions，response 阶段据此再转回 Responses。
	ctxKeyResponsesViaChatCompletionsBridgeActivated = "responsesViaChatCompletionsBridgeActivated"
	// ctxKeyResponsesBridgeStreamingBody 用于在 chunk 之间保存未完整接收的 SSE 尾部。
	// 它不能复用上游解析器的 ctxKeyStreamingBody: bridge 解析的是 ToHttpString
	// 重新序列化后的数据, 共享 buffer 会把上游解析器残留的尾部拼到干净事件前面。
	ctxKeyResponsesBridgeStreamingBody = "responsesBridgeStreamingBody"
	// ctxKeyChatCompletionBridgeStreamingBody 是反向 bridge 的独立 SSE 尾部 buffer。
	ctxKeyChatCompletionBridgeStreamingBody = "chatCompletionBridgeStreamingBody"
	ctxKeyChatCompletionToolNameMap         = "chatCompletionToolNameMap"
	ctxKeyChatCompletionToolContext         = "chatCompletionToolContext"
	customToolInputField                    = "input"
	toolSearchProxyName                     = "tool_search"
)

var validChatCompletionToolNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

func ResponsesViaChatCompletionsBridgeContextKey() string {
	return ctxKeyResponsesViaChatCompletionsBridge
}

func ResponsesViaChatCompletionsBridgeActivatedContextKey() string {
	return ctxKeyResponsesViaChatCompletionsBridgeActivated
}

func ChatCompletionToolNameMapContextKey() string {
	return ctxKeyChatCompletionToolNameMap
}

func ChatCompletionToolContextKey() string {
	return ctxKeyChatCompletionToolContext
}

func IsResponsesPath(path string) bool {
	trimmedPath := path
	if idx := strings.IndexByte(trimmedPath, '?'); idx >= 0 {
		trimmedPath = trimmedPath[:idx]
	}
	return strings.HasSuffix(trimmedPath, "/responses")
}

func IsChatCompletionsPath(path string) bool {
	trimmedPath := path
	if idx := strings.IndexByte(trimmedPath, '?'); idx >= 0 {
		trimmedPath = trimmedPath[:idx]
	}
	return strings.HasSuffix(trimmedPath, "/chat/completions")
}

func ConvertResponsesRequestToChatCompletion(body []byte) ([]byte, error) {
	convertedBody, _, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(body)
	return convertedBody, err
}

func ConvertResponsesRequestToChatCompletionWithToolNameMapping(body []byte) ([]byte, map[string]string, error) {
	convertedBody, _, toolNameMap, err := ConvertResponsesRequestToChatCompletionWithToolContext(body)
	return convertedBody, toolNameMap, err
}

func ConvertResponsesRequestToChatCompletionWithToolContext(body []byte) ([]byte, *ResponsesToolContext, map[string]string, error) {
	transformedBody := body
	var err error
	toolNameBridge := newResponsesToolNameBridge()
	toolContext := buildResponsesToolContextFromRequest(body, toolNameBridge)

	if input := gjson.GetBytes(transformedBody, "input"); input.Exists() {
		messages, err := convertResponsesInputToMessages(input, toolNameBridge, toolContext)
		if err != nil {
			return nil, nil, nil, err
		}
		transformedBody, err = sjson.SetRawBytes(transformedBody, "messages", messages)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set messages from input: %w", err)
		}
		transformedBody, err = sjson.DeleteBytes(transformedBody, "input")
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to delete input: %w", err)
		}
	}

	if instructions := gjson.GetBytes(transformedBody, "instructions"); instructions.Exists() && instructions.String() != "" {
		transformedBody, err = prependSystemMessageToChatCompletions(transformedBody, instructions.String())
		if err != nil {
			return nil, nil, nil, err
		}
		transformedBody, err = sjson.DeleteBytes(transformedBody, "instructions")
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to delete instructions: %w", err)
		}
	}

	if textFormat := gjson.GetBytes(transformedBody, "text.format"); textFormat.Exists() {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "response_format", []byte(textFormat.Raw))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set response_format from text.format: %w", err)
		}
	}

	if maxOutputTokens := gjson.GetBytes(transformedBody, "max_output_tokens"); maxOutputTokens.Exists() {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "max_tokens", []byte(maxOutputTokens.Raw))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set max_tokens from max_output_tokens: %w", err)
		}
	}

	if reasoningEffort := gjson.GetBytes(transformedBody, "reasoning.effort"); reasoningEffort.Exists() && reasoningEffort.String() != "" {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "reasoning_effort", []byte(reasoningEffort.Raw))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set reasoning_effort from reasoning.effort: %w", err)
		}
	}

	if gjson.GetBytes(transformedBody, "stream").Bool() {
		transformedBody, err = sjson.SetBytes(transformedBody, "stream_options.include_usage", true)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to set stream_options.include_usage: %w", err)
		}
	}

	if tools := gjson.GetBytes(transformedBody, "tools"); tools.Exists() && tools.IsArray() {
		normalizedTools, normalizeErr := normalizeResponsesToolsForChatCompletion(tools, toolNameBridge)
		if normalizeErr != nil {
			return nil, nil, nil, normalizeErr
		}
		transformedBody, err = sjson.SetRawBytes(transformedBody, "tools", normalizedTools)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to normalize tools: %w", err)
		}
	}

	if toolChoice := gjson.GetBytes(transformedBody, "tool_choice"); toolChoice.Exists() {
		normalizedToolChoice, normalizeErr := normalizeResponsesToolChoiceForChatCompletion(toolChoice, toolNameBridge, toolContext)
		if normalizeErr != nil {
			return nil, nil, nil, normalizeErr
		}
		if normalizedToolChoice != nil {
			transformedBody, err = sjson.SetRawBytes(transformedBody, "tool_choice", normalizedToolChoice)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed to normalize tool_choice: %w", err)
			}
		}
	}

	for _, field := range []string{
		"max_output_tokens",
		"text",
		"store",
		"client_metadata",
		"previous_response_id",
		"include",
		"reasoning",
		"truncation",
		"metadata",
		"prompt_cache_key",
	} {
		transformedBody, err = sjson.DeleteBytes(transformedBody, field)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to delete %s: %w", field, err)
		}
	}

	return transformedBody, toolContext, toolNameBridge.SanitizedToOriginal(), nil
}

func ConvertChatCompletionRequestToResponses(body []byte) ([]byte, error) {
	transformedBody := body
	var err error

	if messages := gjson.GetBytes(transformedBody, "messages"); messages.Exists() {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "input", []byte(messages.Raw))
		if err != nil {
			return nil, fmt.Errorf("failed to set input from messages: %w", err)
		}
		transformedBody, err = sjson.DeleteBytes(transformedBody, "messages")
		if err != nil {
			return nil, fmt.Errorf("failed to delete messages: %w", err)
		}
	}

	if responseFormat := gjson.GetBytes(transformedBody, "response_format"); responseFormat.Exists() {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "text.format", []byte(responseFormat.Raw))
		if err != nil {
			return nil, fmt.Errorf("failed to set text.format from response_format: %w", err)
		}
		transformedBody, err = sjson.DeleteBytes(transformedBody, "response_format")
		if err != nil {
			return nil, fmt.Errorf("failed to delete response_format: %w", err)
		}
	}

	maxOutputTokens := gjson.GetBytes(transformedBody, "max_completion_tokens")
	if !maxOutputTokens.Exists() {
		maxOutputTokens = gjson.GetBytes(transformedBody, "max_tokens")
	}
	if maxOutputTokens.Exists() {
		transformedBody, err = sjson.SetRawBytes(transformedBody, "max_output_tokens", []byte(maxOutputTokens.Raw))
		if err != nil {
			return nil, fmt.Errorf("failed to set max_output_tokens: %w", err)
		}
	}

	for _, field := range []string{
		"max_tokens",
		"max_completion_tokens",
		"stream_options",
		"n",
		"logprobs",
		"top_logprobs",
		"logit_bias",
		"presence_penalty",
		"frequency_penalty",
		"modalities",
		"prediction",
		"audio",
		"service_tier",
		"stop",
	} {
		transformedBody, err = sjson.DeleteBytes(transformedBody, field)
		if err != nil {
			return nil, fmt.Errorf("failed to delete %s: %w", field, err)
		}
	}

	return transformedBody, nil
}

func ConvertResponsesResponseToChatCompletion(body []byte) ([]byte, error) {
	response, err := buildChatCompletionResponseFromResponses(body)
	if err != nil {
		return nil, err
	}
	return json.Marshal(response)
}

func ConvertChatCompletionResponseToResponses(body []byte) ([]byte, error) {
	return ConvertChatCompletionResponseToResponsesWithToolContext(body, nil, nil)
}

func ConvertChatCompletionResponseToResponsesWithToolNameMapping(body []byte, toolNameMap map[string]string) ([]byte, error) {
	return ConvertChatCompletionResponseToResponsesWithToolContext(body, nil, toolNameMap)
}

func ConvertChatCompletionResponseToResponsesWithToolContext(body []byte, toolContext *ResponsesToolContext, toolNameMap map[string]string) ([]byte, error) {
	var response chatCompletionResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat completion response: %w", err)
	}
	return json.Marshal(buildResponsesResponseFromChatCompletion(&response, toolContext, toolNameMap))
}

type ResponsesChatCompletionBridge struct {
	id                  string
	model               string
	created             int64
	started             bool
	completed           bool
	nextOutput          int
	ToolNameMap         map[string]string
	ToolContext         *ResponsesToolContext
	reasoning           responsesStreamingReasoningState
	inlineThink         responsesStreamingInlineThinkState
	text                responsesStreamingTextState
	toolCalls           map[int]*responsesStreamingToolCallState
	outputItems         []responsesStreamingOutputItem
	outputText          strings.Builder
	usageData           *usage
	pendingFinishReason string
}

type responsesStreamingReasoningState struct {
	outputIndex int
	itemID      string
	text        strings.Builder
	added       bool
	done        bool
}

type responsesStreamingInlineThinkState struct {
	mode   string
	buffer strings.Builder
}

type responsesStreamingTextState struct {
	outputIndex int
	itemID      string
	added       bool
	done        bool
}

type responsesStreamingToolCallState struct {
	outputIndex int
	itemID      string
	callID      string
	chatName    string
	name        string
	arguments   strings.Builder
	// reasoningContent carries the per-tool-call reasoning text accumulated from
	// the upstream choice-level reasoning_content delta. Mirrors cc-switch's
	// ToolCallState.reasoning_content so chat-completions clients that read the
	// tool call item's reasoning_content (DeepSeek-style) still get it, while the
	// independent reasoning output item serves Responses-schema clients.
	reasoningContent strings.Builder
	added            bool
	done             bool
}

type responsesStreamingOutputItem struct {
	outputIndex int
	item        map[string]any
}

type responsesToolKind string

const (
	responsesToolKindFunction   responsesToolKind = "function"
	responsesToolKindNamespace  responsesToolKind = "namespace"
	responsesToolKindCustom     responsesToolKind = "custom"
	responsesToolKindToolSearch responsesToolKind = "tool_search"
)

type responsesToolSpec struct {
	kind      responsesToolKind
	name      string
	namespace string
}

type ResponsesToolContext struct {
	chatNameToSpec map[string]responsesToolSpec
}

func newResponsesToolContext() *ResponsesToolContext {
	return &ResponsesToolContext{
		chatNameToSpec: map[string]responsesToolSpec{},
	}
}

func (c *ResponsesToolContext) add(chatName string, spec responsesToolSpec) {
	if c == nil || chatName == "" {
		return
	}
	if c.chatNameToSpec == nil {
		c.chatNameToSpec = map[string]responsesToolSpec{}
	}
	if _, exists := c.chatNameToSpec[chatName]; exists {
		return
	}
	c.chatNameToSpec[chatName] = spec
}

func (c *ResponsesToolContext) lookup(chatName string) (responsesToolSpec, bool) {
	if c == nil {
		return responsesToolSpec{}, false
	}
	spec, ok := c.chatNameToSpec[chatName]
	return spec, ok
}

func (c *ResponsesToolContext) isCustomToolChatName(chatName string) bool {
	spec, ok := c.lookup(chatName)
	return ok && spec.kind == responsesToolKindCustom
}

func (c *ResponsesToolContext) hasSpecs() bool {
	return c != nil && len(c.chatNameToSpec) > 0
}

func (b *ResponsesChatCompletionBridge) ConvertStreamingChatCompletionToResponses(ctx wrapper.HttpContext, data []byte, isLastChunk bool) ([]byte, error) {
	events := extractStreamingEventsWithBuffer(ctx, ctxKeyResponsesBridgeStreamingBody, data)

	var out strings.Builder
	for _, event := range events {
		// 一旦发出 terminal 事件，后续上游残余 chunk 直接丢弃，避免 response.completed 后继续吐 delta。
		if b.completed {
			break
		}
		if event.IsEndData() {
			continue
		}

		if event.Event == "error" {
			failedEvent, err := b.buildFailedEventFromEventData([]byte(event.Data))
			if err != nil {
				return nil, err
			}
			out.WriteString(failedEvent.ToHttpString())
			continue
		}

		chunkData := []byte(event.Data)
		if len(chunkData) == 0 {
			continue
		}
		if gjson.GetBytes(chunkData, "error").Exists() {
			failedEvent, err := b.buildFailedEventFromEventData(chunkData)
			if err != nil {
				return nil, err
			}
			out.WriteString(failedEvent.ToHttpString())
			continue
		}

		b.captureChatCompletionMetadata(chunkData)

		startEvents, err := b.buildStartEvents()
		if err != nil {
			return nil, err
		}
		for _, streamEvent := range startEvents {
			out.WriteString(streamEvent.ToHttpString())
		}

		// 先把推理摘要独立成 reasoning item，再决定是否切到文本/tool call item。
		if reasoningDelta := b.extractReasoningDelta(chunkData); reasoningDelta != "" {
			reasoningEvents, err := b.buildReasoningDeltaEvents(reasoningDelta)
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range reasoningEvents {
				out.WriteString(streamEvent.ToHttpString())
			}
			// 推理正文已走独立 reasoning item；同时按 cc-switch 的做法把增量累积到每个
			// 仍在建的 tool call，使其后续 output item 能回挂 reasoning_content 字段。
			b.appendReasoningToActiveToolCalls(reasoningDelta)
		}

		if delta := gjson.GetBytes(chunkData, "choices.0.delta.content").String(); delta != "" {
			contentEvents, err := b.buildContentDeltaEvents(delta)
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range contentEvents {
				out.WriteString(streamEvent.ToHttpString())
			}
		}

		if toolCalls := gjson.GetBytes(chunkData, "choices.0.delta.tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
			reasoningDoneEvents, err := b.buildReasoningDoneEvents()
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range reasoningDoneEvents {
				out.WriteString(streamEvent.ToHttpString())
			}

			for _, toolCall := range toolCalls.Array() {
				toolCallEvents, err := b.buildToolCallDeltaEvents(toolCall)
				if err != nil {
					return nil, err
				}
				for _, streamEvent := range toolCallEvents {
					out.WriteString(streamEvent.ToHttpString())
				}
			}
		}

		usageData := extractChatCompletionUsage(chunkData)
		if usageData != nil {
			b.usageData = usageData
		}

		if b.pendingFinishReason != "" && usageData != nil {
			finalEvents, err := b.buildFinalEvents(b.pendingFinishReason)
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range finalEvents {
				out.WriteString(streamEvent.ToHttpString())
			}
			continue
		}

		finishReason := gjson.GetBytes(chunkData, "choices.0.finish_reason").String()
		if finishReason == "" {
			continue
		}

		if b.usageData == nil {
			b.pendingFinishReason = finishReason
			continue
		}

		finalEvents, err := b.buildFinalEvents(finishReason)
		if err != nil {
			return nil, err
		}
		for _, streamEvent := range finalEvents {
			out.WriteString(streamEvent.ToHttpString())
		}
	}

	if isLastChunk && !b.completed && b.started {
		if b.pendingFinishReason != "" {
			finalEvents, err := b.buildFinalEvents(b.pendingFinishReason)
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range finalEvents {
				out.WriteString(streamEvent.ToHttpString())
			}
			return []byte(out.String()), nil
		}

		// 非规范上游如果没显式 finish_reason，这里按 cc-switch 的思路兜底：
		// 有输出就补一个 incomplete completed；完全没产出就转成 response.failed。
		if b.hasSubstantiveOutput() {
			finalEvents, err := b.buildFinalEvents(finishReasonLength)
			if err != nil {
				return nil, err
			}
			for _, streamEvent := range finalEvents {
				out.WriteString(streamEvent.ToHttpString())
			}
		} else {
			failedEvent, err := b.buildFailedEvent("chat completion stream truncated", "stream_truncated")
			if err != nil {
				return nil, err
			}
			out.WriteString(failedEvent.ToHttpString())
		}
	}

	return []byte(out.String()), nil
}

func (b *ResponsesChatCompletionBridge) captureChatCompletionMetadata(body []byte) {
	if id := gjson.GetBytes(body, "id").String(); id != "" {
		b.id = id
	}
	if model := gjson.GetBytes(body, "model").String(); model != "" {
		b.model = model
	}
	if created := gjson.GetBytes(body, "created").Int(); created > 0 {
		b.created = created
	}
}

func (b *ResponsesChatCompletionBridge) buildStartEvents() ([]*StreamEvent, error) {
	if b.started {
		return nil, nil
	}
	b.started = true

	createdEvent, err := b.buildCreatedEvent()
	if err != nil {
		return nil, err
	}
	inProgressEvent, err := b.buildInProgressEvent()
	if err != nil {
		return nil, err
	}
	return []*StreamEvent{createdEvent, inProgressEvent}, nil
}

func (b *ResponsesChatCompletionBridge) extractReasoningDelta(body []byte) string {
	for _, path := range []string{
		"choices.0.delta.reasoning_content",
		"choices.0.delta.reasoning",
	} {
		if delta := gjson.GetBytes(body, path).String(); delta != "" {
			return delta
		}
	}
	return ""
}

func (b *ResponsesChatCompletionBridge) buildReasoningDeltaEvents(delta string) ([]*StreamEvent, error) {
	events := make([]*StreamEvent, 0, 3)
	if !b.reasoning.added {
		// reasoning 和 message/tool call 走不同 output item，避免客户端把摘要误当正文。
		b.reasoning.outputIndex = b.allocateOutputIndex()
		b.reasoning.itemID = fmt.Sprintf("rs_%s", firstNonEmpty(b.id, "resp"))
		b.reasoning.added = true

		itemAddedEvent, err := b.buildReasoningOutputItemAddedEvent()
		if err != nil {
			return nil, err
		}
		partAddedEvent, err := b.buildReasoningSummaryPartAddedEvent()
		if err != nil {
			return nil, err
		}
		events = append(events, itemAddedEvent, partAddedEvent)
	}

	b.reasoning.text.WriteString(delta)
	deltaEvent, err := b.buildReasoningSummaryTextDeltaEvent(delta)
	if err != nil {
		return nil, err
	}
	events = append(events, deltaEvent)
	return events, nil
}

func (b *ResponsesChatCompletionBridge) buildReasoningOutputItemAddedEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.added",
		"output_index": b.reasoning.outputIndex,
		"item": map[string]any{
			"id":      b.reasoning.itemID,
			"type":    "reasoning",
			"status":  "in_progress",
			"summary": []any{},
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_item.added", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildReasoningSummaryPartAddedEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.reasoning_summary_part.added",
		"item_id":       b.reasoning.itemID,
		"output_index":  b.reasoning.outputIndex,
		"summary_index": 0,
		"part": map[string]any{
			"type": "summary_text",
			"text": "",
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.reasoning_summary_part.added", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildReasoningSummaryTextDeltaEvent(delta string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.reasoning_summary_text.delta",
		"item_id":       b.reasoning.itemID,
		"output_index":  b.reasoning.outputIndex,
		"summary_index": 0,
		"delta":         delta,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.reasoning_summary_text.delta", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildReasoningDoneEvents() ([]*StreamEvent, error) {
	if !b.reasoning.added || b.reasoning.done {
		return nil, nil
	}

	text := b.reasoning.text.String()
	textDonePayload, err := json.Marshal(map[string]any{
		"type":          "response.reasoning_summary_text.done",
		"item_id":       b.reasoning.itemID,
		"output_index":  b.reasoning.outputIndex,
		"summary_index": 0,
		"text":          text,
	})
	if err != nil {
		return nil, err
	}
	partDonePayload, err := json.Marshal(map[string]any{
		"type":          "response.reasoning_summary_part.done",
		"item_id":       b.reasoning.itemID,
		"output_index":  b.reasoning.outputIndex,
		"summary_index": 0,
		"part": map[string]any{
			"type": "summary_text",
			"text": text,
		},
	})
	if err != nil {
		return nil, err
	}
	item := map[string]any{
		"id":   b.reasoning.itemID,
		"type": "reasoning",
		"summary": []any{
			map[string]any{
				"type": "summary_text",
				"text": text,
			},
		},
	}
	itemDonePayload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.done",
		"output_index": b.reasoning.outputIndex,
		"item":         item,
	})
	if err != nil {
		return nil, err
	}

	b.reasoning.done = true
	b.outputItems = append(b.outputItems, responsesStreamingOutputItem{
		outputIndex: b.reasoning.outputIndex,
		item:        item,
	})

	return []*StreamEvent{
		{Event: "response.reasoning_summary_text.done", Data: string(textDonePayload)},
		{Event: "response.reasoning_summary_part.done", Data: string(partDonePayload)},
		{Event: "response.output_item.done", Data: string(itemDonePayload)},
	}, nil
}

func (b *ResponsesChatCompletionBridge) buildCreatedEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":     "response.created",
		"response": b.buildResponseEnvelope("in_progress", []any{}, ""),
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.created", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildInProgressEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":     "response.in_progress",
		"response": b.buildResponseEnvelope("in_progress", []any{}, ""),
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.in_progress", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildContentDeltaEvents(delta string) ([]*StreamEvent, error) {
	switch b.inlineThink.mode {
	case "text":
		reasoningDoneEvents, err := b.buildReasoningDoneEvents()
		if err != nil {
			return nil, err
		}
		textEvents, err := b.buildTextDeltaEvents(delta)
		if err != nil {
			return nil, err
		}
		return append(reasoningDoneEvents, textEvents...), nil
	case "reasoning":
		b.inlineThink.buffer.WriteString(delta)
		return b.flushInlineThinkBuffer()
	default:
		b.inlineThink.buffer.WriteString(delta)
		buffered := b.inlineThink.buffer.String()
		if strings.HasPrefix(buffered, "<think>") {
			b.inlineThink.mode = "reasoning"
			return b.flushInlineThinkBuffer()
		}
		if isIncompleteThinkPrefix(buffered) {
			return nil, nil
		}
		b.inlineThink.mode = "text"
		b.inlineThink.buffer.Reset()
		reasoningDoneEvents, err := b.buildReasoningDoneEvents()
		if err != nil {
			return nil, err
		}
		textEvents, err := b.buildTextDeltaEvents(buffered)
		if err != nil {
			return nil, err
		}
		return append(reasoningDoneEvents, textEvents...), nil
	}
}

func (b *ResponsesChatCompletionBridge) flushInlineThinkBuffer() ([]*StreamEvent, error) {
	buffered := b.inlineThink.buffer.String()
	closeIndex := strings.Index(buffered, "</think>")
	if closeIndex < 0 {
		return nil, nil
	}

	reasoningText := strings.TrimPrefix(buffered[:closeIndex], "<think>")
	reasoningText = strings.TrimPrefix(reasoningText, "\n")
	answerText := buffered[closeIndex+len("</think>"):]
	answerText = strings.TrimPrefix(answerText, "\n\n")
	answerText = strings.TrimPrefix(answerText, "\n")

	b.inlineThink.mode = "text"
	b.inlineThink.buffer.Reset()

	events := make([]*StreamEvent, 0, 6)
	if reasoningText != "" {
		reasoningEvents, err := b.buildReasoningDeltaEvents(reasoningText)
		if err != nil {
			return nil, err
		}
		events = append(events, reasoningEvents...)
	}
	reasoningDoneEvents, err := b.buildReasoningDoneEvents()
	if err != nil {
		return nil, err
	}
	events = append(events, reasoningDoneEvents...)
	if answerText != "" {
		textEvents, err := b.buildTextDeltaEvents(answerText)
		if err != nil {
			return nil, err
		}
		events = append(events, textEvents...)
	}
	return events, nil
}

func isIncompleteThinkPrefix(value string) bool {
	return strings.HasPrefix("<think>", value)
}

func (b *ResponsesChatCompletionBridge) buildTextDeltaEvents(delta string) ([]*StreamEvent, error) {
	events := make([]*StreamEvent, 0, 3)
	if !b.text.added {
		// 文本 delta 前先补 output_item.added + content_part.added，匹配 Codex 客户端的 item 生命周期预期。
		outputIndex := b.allocateOutputIndex()
		b.text.outputIndex = outputIndex
		b.text.itemID = fmt.Sprintf("%s_msg", firstNonEmpty(b.id, "resp"))
		b.text.added = true

		itemAddedEvent, err := b.buildTextOutputItemAddedEvent()
		if err != nil {
			return nil, err
		}
		events = append(events, itemAddedEvent)

		contentPartAddedEvent, err := b.buildContentPartAddedEvent()
		if err != nil {
			return nil, err
		}
		events = append(events, contentPartAddedEvent)
	}

	b.outputText.WriteString(delta)
	textEvent, err := b.buildOutputTextDeltaEvent(delta)
	if err != nil {
		return nil, err
	}
	events = append(events, textEvent)
	return events, nil
}

func (b *ResponsesChatCompletionBridge) buildOutputTextDeltaEvent(delta string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.output_text.delta",
		"item_id":       b.text.itemID,
		"delta":         delta,
		"output_index":  b.text.outputIndex,
		"content_index": 0,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_text.delta", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildTextOutputItemAddedEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.added",
		"output_index": b.text.outputIndex,
		"item": map[string]any{
			"id":      b.text.itemID,
			"type":    "message",
			"role":    roleAssistant,
			"status":  "in_progress",
			"content": []any{},
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_item.added", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildContentPartAddedEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.content_part.added",
		"item_id":       b.text.itemID,
		"output_index":  b.text.outputIndex,
		"content_index": 0,
		"part": map[string]any{
			"type":        "output_text",
			"text":        "",
			"annotations": []any{},
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.content_part.added", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildOutputTextDoneEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.output_text.done",
		"item_id":       b.text.itemID,
		"output_index":  b.text.outputIndex,
		"content_index": 0,
		"text":          b.outputText.String(),
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_text.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildContentPartDoneEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":          "response.content_part.done",
		"item_id":       b.text.itemID,
		"output_index":  b.text.outputIndex,
		"content_index": 0,
		"part": map[string]any{
			"type":        "output_text",
			"text":        b.outputText.String(),
			"annotations": []any{},
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.content_part.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildOutputItemDoneEvent() (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.done",
		"output_index": b.text.outputIndex,
		"item": map[string]any{
			"id":     b.text.itemID,
			"type":   "message",
			"role":   roleAssistant,
			"status": "completed",
			"content": []any{
				map[string]any{
					"type":        "output_text",
					"text":        b.outputText.String(),
					"annotations": []any{},
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_item.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildToolCallDeltaEvents(toolCall gjson.Result) ([]*StreamEvent, error) {
	toolCallIndex := int(toolCall.Get("index").Int())
	state := b.ensureToolCallState(toolCallIndex)
	if id := toolCall.Get("id").String(); id != "" {
		state.callID = id
	}
	if chatName := toolCall.Get("function.name").String(); chatName != "" {
		state.chatName = chatName
	}
	if name := restoreToolName(toolCall.Get("function.name").String(), b.ToolNameMap); name != "" {
		state.name = name
	}
	if arguments := toolCall.Get("function.arguments").String(); arguments != "" {
		state.arguments.WriteString(arguments)
	}

	events := make([]*StreamEvent, 0, 2)
	if !state.added && state.name != "" {
		// chat.completions 的 tool_call 可能先给 name，再分多段给 arguments，所以先建 item 再吃参数 delta。
		if state.callID == "" {
			state.callID = fmt.Sprintf("call_%d", toolCallIndex)
		}
		// 对齐 cc-switch 的 push_tool_call_delta：tool call 首次建项时，若该 tool 尚无
		// reasoning_content，则用当前已累积的 choice 级推理正文兜底回填，覆盖"reasoning
		// 早于 tool_calls 到达"的场景（appendReasoningToActiveToolCalls 当时无活跃 tool 可挂）。
		if state.reasoningContent.Len() == 0 {
			if reasoning := b.currentReasoningText(); reasoning != "" {
				state.reasoningContent.WriteString(reasoning)
			}
		}
		state.outputIndex = b.allocateOutputIndex()
		state.itemID = b.buildToolCallItemID(state)
		state.added = true

		itemAddedEvent, err := b.buildToolOutputItemAddedEvent(state)
		if err != nil {
			return nil, err
		}
		events = append(events, itemAddedEvent)
	}

	if state.added {
		argumentsDelta := toolCall.Get("function.arguments").String()
		if argumentsDelta != "" && !b.isCustomToolCall(state) {
			deltaEvent, err := b.buildFunctionCallArgumentsDeltaEvent(state, argumentsDelta)
			if err != nil {
				return nil, err
			}
			events = append(events, deltaEvent)
		}
	}
	return events, nil
}

func (b *ResponsesChatCompletionBridge) buildToolOutputItemAddedEvent(state *responsesStreamingToolCallState) (*StreamEvent, error) {
	item := b.buildToolCallItem(state, "", "in_progress")
	payload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.added",
		"output_index": state.outputIndex,
		"item":         item,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_item.added", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildFunctionCallArgumentsDeltaEvent(state *responsesStreamingToolCallState, arguments string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.function_call_arguments.delta",
		"item_id":      state.itemID,
		"output_index": state.outputIndex,
		"delta":        arguments,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.function_call_arguments.delta", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildFunctionCallArgumentsDoneEvent(state *responsesStreamingToolCallState) (*StreamEvent, error) {
	arguments := canonicalizeStreamingToolArguments(state.arguments.String())
	payload, err := json.Marshal(map[string]any{
		"type":         "response.function_call_arguments.done",
		"item_id":      state.itemID,
		"output_index": state.outputIndex,
		"arguments":    arguments,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.function_call_arguments.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildCustomToolCallInputDeltaEvent(state *responsesStreamingToolCallState, input string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.custom_tool_call_input.delta",
		"item_id":      state.itemID,
		"output_index": state.outputIndex,
		"delta":        input,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.custom_tool_call_input.delta", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildCustomToolCallInputDoneEvent(state *responsesStreamingToolCallState, input string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":         "response.custom_tool_call_input.done",
		"item_id":      state.itemID,
		"output_index": state.outputIndex,
		"input":        input,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.custom_tool_call_input.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildToolCallArgumentsDoneEvents(state *responsesStreamingToolCallState) ([]*StreamEvent, error) {
	if b.isCustomToolCall(state) {
		input := customToolInputFromChatArguments(canonicalizeStreamingToolArguments(state.arguments.String()))
		events := make([]*StreamEvent, 0, 2)
		if input != "" {
			deltaEvent, err := b.buildCustomToolCallInputDeltaEvent(state, input)
			if err != nil {
				return nil, err
			}
			events = append(events, deltaEvent)
		}
		doneEvent, err := b.buildCustomToolCallInputDoneEvent(state, input)
		if err != nil {
			return nil, err
		}
		events = append(events, doneEvent)
		return events, nil
	}
	doneEvent, err := b.buildFunctionCallArgumentsDoneEvent(state)
	if err != nil {
		return nil, err
	}
	return []*StreamEvent{doneEvent}, nil
}

func (b *ResponsesChatCompletionBridge) buildToolOutputItemDoneEvent(state *responsesStreamingToolCallState) (*StreamEvent, error) {
	// TODO：后续可抽一个 SSE event builder，统一 json.Marshal + StreamEvent 样板。
	item := b.buildToolCallItem(state, canonicalizeStreamingToolArguments(state.arguments.String()), "completed")
	payload, err := json.Marshal(map[string]any{
		"type":         "response.output_item.done",
		"output_index": state.outputIndex,
		"item":         item,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.output_item.done", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildFinalEvents(finishReason string) ([]*StreamEvent, error) {
	if b.completed {
		return nil, nil
	}
	b.completed = true

	// finalize 顺序尽量贴近 cc-switch：reasoning -> text -> tools -> response.completed。
	// TODO：message output_item.done 的载荷应复用 buildOutputItemDoneEvent，避免两处手动拼装分歧。
	events := make([]*StreamEvent, 0, 8)
	reasoningDoneEvents, err := b.buildReasoningDoneEvents()
	if err != nil {
		return nil, err
	}
	events = append(events, reasoningDoneEvents...)

	if b.text.added && !b.text.done {
		textDoneEvent, err := b.buildOutputTextDoneEvent()
		if err != nil {
			return nil, err
		}
		contentPartDoneEvent, err := b.buildContentPartDoneEvent()
		if err != nil {
			return nil, err
		}
		outputItemDoneEvent, err := b.buildOutputItemDoneEvent()
		if err != nil {
			return nil, err
		}
		events = append(events, textDoneEvent, contentPartDoneEvent, outputItemDoneEvent)
		b.text.done = true
		b.outputItems = append(b.outputItems, responsesStreamingOutputItem{
			outputIndex: b.text.outputIndex,
			item: map[string]any{
				"id":     b.text.itemID,
				"type":   "message",
				"role":   roleAssistant,
				"status": "completed",
				"content": []any{
					map[string]any{
						"type":        "output_text",
						"text":        b.outputText.String(),
						"annotations": []any{},
					},
				},
			},
		})
	}

	toolCallIndexes := make([]int, 0, len(b.toolCalls))
	for index := range b.toolCalls {
		toolCallIndexes = append(toolCallIndexes, index)
	}
	sort.Ints(toolCallIndexes)
	for _, index := range toolCallIndexes {
		state := b.toolCalls[index]
		if state == nil || state.done || state.name == "" {
			continue
		}
		if !state.added {
			if state.callID == "" {
				state.callID = fmt.Sprintf("call_%d", index)
			}
			state.outputIndex = b.allocateOutputIndex()
			state.itemID = b.buildToolCallItemID(state)
			state.added = true
			itemAddedEvent, err := b.buildToolOutputItemAddedEvent(state)
			if err != nil {
				return nil, err
			}
			events = append(events, itemAddedEvent)
		}

		finalArgumentEvents, err := b.buildToolCallArgumentsDoneEvents(state)
		if err != nil {
			return nil, err
		}
		itemDoneEvent, err := b.buildToolOutputItemDoneEvent(state)
		if err != nil {
			return nil, err
		}
		events = append(events, finalArgumentEvents...)
		events = append(events, itemDoneEvent)
		state.done = true
		b.outputItems = append(b.outputItems, responsesStreamingOutputItem{
			outputIndex: state.outputIndex,
			item:        b.buildToolCallItem(state, canonicalizeStreamingToolArguments(state.arguments.String()), "completed"),
		})
	}

	completedEvent, err := b.buildCompletedEvent(finishReason)
	if err != nil {
		return nil, err
	}
	events = append(events, completedEvent)
	return events, nil
}

func (b *ResponsesChatCompletionBridge) buildCompletedEvent(finishReason string) (*StreamEvent, error) {
	payload, err := json.Marshal(map[string]any{
		"type":     "response.completed",
		"response": b.buildResponseEnvelope(responseStatusFromFinishReason(finishReason), b.completedOutputItems(), finishReason),
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.completed", Data: string(payload)}, nil
}

func (b *ResponsesChatCompletionBridge) buildResponseEnvelope(status string, output []any, finishReason string) map[string]any {
	response := map[string]any{
		"id":         b.id,
		"object":     "response",
		"created_at": b.created,
		"model":      b.model,
		"status":     status,
		"output":     output,
	}
	if text := b.outputText.String(); text != "" {
		response["output_text"] = text
	}
	if b.usageData != nil {
		response["usage"] = buildResponsesUsage(b.usageData)
	}
	if finishReason == finishReasonLength {
		response["incomplete_details"] = map[string]any{
			"reason": "max_output_tokens",
		}
	}
	return response
}

func (b *ResponsesChatCompletionBridge) completedOutputItems() []any {
	if len(b.outputItems) == 0 {
		return []any{}
	}
	sort.Slice(b.outputItems, func(i, j int) bool {
		return b.outputItems[i].outputIndex < b.outputItems[j].outputIndex
	})
	items := make([]any, 0, len(b.outputItems))
	for _, outputItem := range b.outputItems {
		items = append(items, outputItem.item)
	}
	return items
}

func (b *ResponsesChatCompletionBridge) buildToolCallItem(state *responsesStreamingToolCallState, arguments, status string) map[string]any {
	return buildResponsesToolCallItem(state.itemID, state.callID, state.chatName, state.name, arguments, state.reasoningContent.String(), status, b.ToolContext)
}

func (b *ResponsesChatCompletionBridge) buildToolCallItemID(state *responsesStreamingToolCallState) string {
	if b.isCustomToolCall(state) {
		return "ctc_" + state.callID
	}
	return state.callID
}

func (b *ResponsesChatCompletionBridge) isCustomToolCall(state *responsesStreamingToolCallState) bool {
	if state == nil {
		return false
	}
	if state.chatName != "" && b.ToolContext != nil {
		return b.ToolContext.isCustomToolChatName(state.chatName)
	}
	if state.name != "" && b.ToolContext != nil {
		return b.ToolContext.isCustomToolChatName(state.name)
	}
	return false
}

func (b *ResponsesChatCompletionBridge) ensureToolCallState(index int) *responsesStreamingToolCallState {
	if b.toolCalls == nil {
		b.toolCalls = map[int]*responsesStreamingToolCallState{}
	}
	state, ok := b.toolCalls[index]
	if !ok {
		state = &responsesStreamingToolCallState{}
		b.toolCalls[index] = state
	}
	return state
}

func (b *ResponsesChatCompletionBridge) allocateOutputIndex() int {
	index := b.nextOutput
	b.nextOutput++
	return index
}

func (b *ResponsesChatCompletionBridge) hasSubstantiveOutput() bool {
	if b.text.added || b.reasoning.added || len(b.outputItems) > 0 {
		return true
	}
	for _, state := range b.toolCalls {
		if state != nil && (state.added || state.callID != "" || state.name != "" || state.arguments.Len() > 0 || state.reasoningContent.Len() > 0) {
			return true
		}
	}
	return false
}

// currentReasoningText mirrors cc-switch's current_reasoning_text: return the trimmed
// accumulated choice-level reasoning text, used to seed a tool call's
// reasoning_content when the tool is first created after reasoning already arrived.
func (b *ResponsesChatCompletionBridge) currentReasoningText() string {
	return strings.TrimSpace(b.reasoning.text.String())
}

// appendReasoningToActiveToolCalls mirrors cc-switch's append_reasoning_to_active_tools:
// accumulate the upstream choice-level reasoning delta onto every tool call that is
// still in flight (not done), so the tool call item can later surface its own
// reasoning_content for chat-completions clients. Already-finalized tools are skipped.
func (b *ResponsesChatCompletionBridge) appendReasoningToActiveToolCalls(delta string) {
	if strings.TrimSpace(delta) == "" {
		return
	}
	for _, state := range b.toolCalls {
		if state == nil || state.done {
			continue
		}
		state.reasoningContent.WriteString(delta)
	}
}

func (b *ResponsesChatCompletionBridge) buildFailedEventFromEventData(body []byte) (*StreamEvent, error) {
	message := firstNonEmpty(
		gjson.GetBytes(body, "error.message").String(),
		gjson.GetBytes(body, "message").String(),
		"chat completion stream failed",
	)
	errorType := firstNonEmpty(
		gjson.GetBytes(body, "error.type").String(),
		gjson.GetBytes(body, "error.code").String(),
		gjson.GetBytes(body, "type").String(),
	)
	return b.buildFailedEvent(message, errorType)
}

func (b *ResponsesChatCompletionBridge) buildFailedEvent(message, errorType string) (*StreamEvent, error) {
	b.completed = true
	response := map[string]any{
		"id":         b.id,
		"object":     "response",
		"created_at": b.created,
		"model":      b.model,
		"status":     "failed",
		"error": map[string]any{
			"message": message,
		},
	}
	if errorType != "" {
		response["error"].(map[string]any)["type"] = errorType
	}
	payload, err := json.Marshal(map[string]any{
		"type":     "response.failed",
		"response": response,
	})
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Event: "response.failed", Data: string(payload)}, nil
}

func responseStatusFromFinishReason(finishReason string) string {
	if finishReason == finishReasonLength {
		return "incomplete"
	}
	return "completed"
}

func canonicalizeStreamingToolArguments(arguments string) string {
	if arguments == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err != nil {
		return arguments
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return arguments
	}
	return string(normalized)
}

func buildResponsesToolContextFromRequest(body []byte, toolNameBridge *responsesToolNameBridge) *ResponsesToolContext {
	request := gjson.ParseBytes(body)
	context := newResponsesToolContext()

	if tools := request.Get("tools"); tools.Exists() && tools.IsArray() {
		for _, tool := range tools.Array() {
			addResponsesToolContextTool(context, tool, "", toolNameBridge)
		}
	}
	collectResponsesToolSearchOutputTools(context, request.Get("input"), toolNameBridge)

	if !context.hasSpecs() {
		return nil
	}
	return context
}

func collectResponsesToolSearchOutputTools(context *ResponsesToolContext, value gjson.Result, toolNameBridge *responsesToolNameBridge) {
	if !value.Exists() {
		return
	}
	if value.IsArray() {
		for _, item := range value.Array() {
			collectResponsesToolSearchOutputTools(context, item, toolNameBridge)
		}
		return
	}
	if !value.IsObject() {
		return
	}
	if value.Get("type").String() == "tool_search_output" {
		for _, tool := range value.Get("tools").Array() {
			addResponsesToolContextTool(context, tool, "", toolNameBridge)
		}
	}
	for _, entry := range value.Map() {
		collectResponsesToolSearchOutputTools(context, entry, toolNameBridge)
	}
}

func addResponsesToolContextTool(context *ResponsesToolContext, tool gjson.Result, namespace string, toolNameBridge *responsesToolNameBridge) {
	if context == nil || !tool.Exists() {
		return
	}
	toolType := tool.Get("type").String()
	switch toolType {
	case "function":
		name := firstNonEmpty(tool.Get("function.name").String(), tool.Get("name").String())
		if name == "" {
			return
		}
		chatName := resolveResponseToolChatName(name, namespace, toolNameBridge, nil)
		specKind := responsesToolKindFunction
		if namespace != "" {
			specKind = responsesToolKindNamespace
		}
		context.add(chatName, responsesToolSpec{
			kind:      specKind,
			name:      name,
			namespace: namespace,
		})
	case "custom":
		name := firstNonEmpty(tool.Get("function.name").String(), tool.Get("name").String())
		if name == "" {
			return
		}
		chatName := resolveResponseToolChatName(name, "", toolNameBridge, nil)
		context.add(chatName, responsesToolSpec{
			kind: responsesToolKindCustom,
			name: name,
		})
	case "tool_search":
		context.add(toolSearchProxyName, responsesToolSpec{
			kind: responsesToolKindToolSearch,
			name: toolSearchProxyName,
		})
	case "namespace":
		namespaceName := firstNonEmpty(tool.Get("name").String(), namespace)
		for _, child := range tool.Get("tools").Array() {
			addResponsesToolContextTool(context, child, namespaceName, toolNameBridge)
		}
	}
}

func convertResponsesInputToMessages(input gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) ([]byte, error) {
	if input.Type == gjson.String {
		return json.Marshal([]map[string]any{
			{
				"role":    roleUser,
				"content": input.String(),
			},
		})
	}
	if input.IsObject() {
		input = gjson.ParseBytes([]byte("[" + input.Raw + "]"))
	}
	if !input.IsArray() {
		return nil, fmt.Errorf("unsupported responses input type")
	}

	messages := make([]map[string]any, 0, len(input.Array()))
	for _, item := range input.Array() {
		itemMessages := convertResponsesInputItemToMessages(item, toolNameBridge, toolContext)
		if len(itemMessages) == 0 {
			continue
		}
		messages = append(messages, itemMessages...)
	}
	return json.Marshal(messages)
}

func convertResponsesInputItemToMessages(item gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) []map[string]any {
	if item.Type == gjson.String {
		return []map[string]any{{
			"role":    roleUser,
			"content": item.String(),
		}}
	}
	if !item.Exists() || (!item.IsObject() && !item.IsArray()) {
		return nil
	}

	itemType := item.Get("type").String()
	switch itemType {
	case "reasoning", "summary":
		return nil
	case "function_call":
		callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
		chatName := responsesFunctionChatName(item, toolNameBridge, toolContext)
		if chatName == "" {
			return nil
		}
		return []map[string]any{{
			"role":    roleAssistant,
			"content": nil,
			"tool_calls": []map[string]any{{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      chatName,
					"arguments": stringifyResponsesValue(item.Get("arguments")),
				},
			}},
		}}
	case "custom_tool_call":
		callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
		name := item.Get("name").String()
		if name == "" {
			return nil
		}
		return []map[string]any{{
			"role":    roleAssistant,
			"content": nil,
			"tool_calls": []map[string]any{{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      toolNameBridge.Sanitize(name),
					"arguments": buildCustomToolChatArguments(item.Get(customToolInputField)),
				},
			}},
		}}
	case "tool_search_call":
		callID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String())
		return []map[string]any{{
			"role":    roleAssistant,
			"content": nil,
			"tool_calls": []map[string]any{{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      toolSearchProxyName,
					"arguments": stringifyResponsesToolSearchArguments(item.Get("arguments")),
				},
			}},
		}}
	case "function_call_output", "tool_call_output":
		message := map[string]any{
			"role":    roleTool,
			"content": responsesContentToText(item.Get("output")),
		}
		if toolCallID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String()); toolCallID != "" {
			message["tool_call_id"] = toolCallID
		}
		return []map[string]any{message}
	case "custom_tool_call_output", "tool_search_output":
		message := map[string]any{
			"role":    roleTool,
			"content": canonicalizeJSONIfPossible(item.Raw),
		}
		if toolCallID := firstNonEmpty(item.Get("call_id").String(), item.Get("id").String()); toolCallID != "" {
			message["tool_call_id"] = toolCallID
		}
		return []map[string]any{message}
	}

	role := normalizeResponsesMessageRole(firstNonEmpty(item.Get("role").String(), roleUser))
	if itemType != "" && itemType != "message" && !item.Get("role").Exists() {
		switch itemType {
		case "input_text", "output_text", "text", "summary_text":
			return []map[string]any{{
				"role":    roleUser,
				"content": responsesContentToText(item),
			}}
		default:
			return nil
		}
	}

	content := normalizeResponsesInputContent(item.Get("content"))
	message := map[string]any{
		"role":    role,
		"content": content,
	}
	if role == roleTool {
		if toolCallID := firstNonEmpty(item.Get("tool_call_id").String(), item.Get("call_id").String(), item.Get("id").String()); toolCallID != "" {
			message["tool_call_id"] = toolCallID
		}
	}
	if toolCalls := item.Get("tool_calls"); toolCalls.Exists() && toolCalls.IsArray() {
		message["tool_calls"] = normalizeMessageToolCalls(toolCalls, toolNameBridge, toolContext)
	}
	if functionCall := item.Get("function_call"); functionCall.Exists() && functionCall.IsObject() {
		message["function_call"] = normalizeMessageFunctionCall(functionCall, toolNameBridge, toolContext)
	}
	if name := item.Get("name").String(); name != "" {
		message["name"] = name
	}

	return []map[string]any{message}
}

func normalizeResponsesInputContent(content gjson.Result) any {
	if content.Type == gjson.String {
		return content.String()
	}
	if content.IsObject() {
		if text := responsesContentToText(content); text != "" {
			return text
		}
		return content.Value()
	}
	if !content.IsArray() {
		if !content.Exists() {
			return ""
		}
		return content.Value()
	}

	allText := true
	textParts := make([]string, 0, len(content.Array()))
	items := make([]map[string]any, 0, len(content.Array()))
	for _, part := range content.Array() {
		partType := part.Get("type").String()
		switch partType {
		case "input_text", "output_text", "text", "summary_text":
			text := responsesContentToText(part)
			textParts = append(textParts, text)
			items = append(items, map[string]any{
				"type": "text",
				"text": text,
			})
		case "refusal":
			text := firstNonEmpty(part.Get("refusal").String(), part.Get("text").String())
			textParts = append(textParts, text)
			items = append(items, map[string]any{
				"type": "text",
				"text": text,
			})
		case "input_image", "image_url":
			allText = false
			imageURL := part.Get("image_url").String()
			if imageURL == "" {
				imageURL = part.Get("image_url.url").String()
			}
			items = append(items, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": imageURL,
				},
			})
		default:
			if part.IsObject() {
				allText = false
				if item, ok := part.Value().(map[string]any); ok {
					items = append(items, item)
				}
				continue
			}
			// 有些客户端会发送原始类型的 content 数组元素, 例如 ["hello"] 或 [123]。
			// 这里按文本处理, 避免裸 map[string]any 断言导致插件 panic。
			text := stringifyResponsesValue(part)
			textParts = append(textParts, text)
			items = append(items, map[string]any{
				"type": "text",
				"text": text,
			})
		}
	}
	if allText {
		return strings.Join(textParts, "\n")
	}
	return items
}

func normalizeResponsesMessageRole(role string) string {
	switch role {
	case roleDeveloper:
		return roleSystem
	case roleSystem, roleAssistant, roleTool:
		return role
	default:
		return roleUser
	}
}

func responsesContentToText(content gjson.Result) string {
	if !content.Exists() {
		return ""
	}
	if content.Type == gjson.String {
		return content.String()
	}
	if !content.IsArray() {
		return firstNonEmpty(
			content.Get("text").String(),
			content.Get("output").String(),
			content.Get("refusal").String(),
			content.Get("summary").String(),
		)
	}

	textParts := make([]string, 0, len(content.Array()))
	for _, part := range content.Array() {
		text := responsesContentToText(part)
		if text != "" {
			textParts = append(textParts, text)
		}
	}
	return strings.Join(textParts, "\n")
}

func stringifyResponsesValue(value gjson.Result) string {
	if !value.Exists() {
		return ""
	}
	if value.Type == gjson.String {
		return value.String()
	}
	return value.Raw
}

func normalizeResponsesToolsForChatCompletion(tools gjson.Result, toolNameBridge *responsesToolNameBridge) ([]byte, error) {
	normalizedTools := make([]map[string]any, 0, len(tools.Array()))
	for _, tool := range tools.Array() {
		switch tool.Get("type").String() {
		case "function", "custom":
			normalizedTool := normalizeFlatToolForChatCompletion(tool, "", toolNameBridge)
			if normalizedTool != nil {
				normalizedTools = append(normalizedTools, normalizedTool)
			}
		case "namespace":
			namespaceName := tool.Get("name").String()
			for _, child := range tool.Get("tools").Array() {
				normalizedTool := normalizeFlatToolForChatCompletion(child, namespaceName, toolNameBridge)
				if normalizedTool != nil {
					normalizedTools = append(normalizedTools, normalizedTool)
				}
			}
		case "tool_search":
			normalizedTools = append(normalizedTools, buildToolSearchChatTool())
		}
	}
	return json.Marshal(normalizedTools)
}

func normalizeFlatToolForChatCompletion(tool gjson.Result, namespace string, toolNameBridge *responsesToolNameBridge) map[string]any {
	toolType := tool.Get("type").String()
	functionName := tool.Get("function.name").String()
	if functionName == "" {
		functionName = tool.Get("name").String()
	}
	if functionName == "" {
		return nil
	}
	originalName := functionName
	if namespace != "" {
		originalName = namespace + "." + functionName
	}
	normalized := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": toolNameBridge.Sanitize(originalName),
		},
	}
	functionSpec := normalized["function"].(map[string]any)
	if toolType == "custom" {
		functionSpec["description"] = firstNonEmpty(tool.Get("description").String(), tool.Get("function.description").String(), "Custom tool input.")
		functionSpec["parameters"] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				customToolInputField: map[string]any{
					"type": "string",
				},
			},
			"required": []string{customToolInputField},
		}
		return normalized
	}
	if description := firstNonEmpty(tool.Get("function.description").String(), tool.Get("description").String()); description != "" {
		functionSpec["description"] = description
	}
	if parameters := tool.Get("function.parameters"); parameters.Exists() && parameters.IsObject() {
		functionSpec["parameters"] = parameters.Value()
	} else if parameters := tool.Get("parameters"); parameters.Exists() && parameters.IsObject() {
		functionSpec["parameters"] = parameters.Value()
	} else if inputSchema := tool.Get("input_schema"); inputSchema.Exists() && inputSchema.IsObject() {
		functionSpec["parameters"] = inputSchema.Value()
	} else if schema := tool.Get("schema"); schema.Exists() && schema.IsObject() {
		functionSpec["parameters"] = schema.Value()
	}
	if strict := tool.Get("strict"); strict.Exists() {
		functionSpec["strict"] = strict.Value()
	}
	return normalized
}

func normalizeResponsesToolChoiceForChatCompletion(toolChoice gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) ([]byte, error) {
	if !toolChoice.Exists() {
		return nil, nil
	}
	if toolChoice.Type == gjson.String {
		return []byte(toolChoice.Raw), nil
	}
	if !toolChoice.IsObject() {
		return []byte(toolChoice.Raw), nil
	}

	normalized := toolChoice.Value().(map[string]any)
	if functionData := toolChoice.Get("function"); functionData.Exists() && functionData.IsObject() {
		name := functionData.Get("name").String()
		namespace := functionData.Get("namespace").String()
		if name != "" {
			function := functionData.Value().(map[string]any)
			function["name"] = resolveResponseToolChatName(name, namespace, toolNameBridge, toolContext)
			normalized["function"] = function
		}
	} else if name := toolChoice.Get("name").String(); name != "" {
		normalized["name"] = resolveResponseToolChatName(name, toolChoice.Get("namespace").String(), toolNameBridge, toolContext)
	}
	return json.Marshal(normalized)
}

func normalizeMessageToolCalls(toolCalls gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) []map[string]any {
	normalizedToolCalls := make([]map[string]any, 0, len(toolCalls.Array()))
	for _, toolCall := range toolCalls.Array() {
		normalizedToolCall := map[string]any{
			"id":   toolCall.Get("id").String(),
			"type": firstNonEmpty(toolCall.Get("type").String(), "function"),
		}
		functionName := responsesFunctionChatName(toolCall.Get("function"), toolNameBridge, toolContext)
		function := map[string]any{
			"name":      functionName,
			"arguments": stringifyResponsesValue(toolCall.Get("function.arguments")),
		}
		normalizedToolCall["function"] = function
		normalizedToolCalls = append(normalizedToolCalls, normalizedToolCall)
	}
	return normalizedToolCalls
}

func normalizeMessageFunctionCall(functionCall gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) map[string]any {
	return map[string]any{
		"name":      responsesFunctionChatName(functionCall, toolNameBridge, toolContext),
		"arguments": stringifyResponsesValue(functionCall.Get("arguments")),
	}
}

func responsesFunctionChatName(item gjson.Result, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) string {
	name := firstNonEmpty(item.Get("name").String(), item.Get("function.name").String())
	if name == "" {
		return ""
	}
	namespace := firstNonEmpty(item.Get("namespace").String(), item.Get("function.namespace").String())
	return resolveResponseToolChatName(name, namespace, toolNameBridge, toolContext)
}

func resolveResponseToolChatName(name, namespace string, toolNameBridge *responsesToolNameBridge, toolContext *ResponsesToolContext) string {
	if name == "" {
		return ""
	}
	if name == toolSearchProxyName {
		return toolSearchProxyName
	}
	if toolContext != nil {
		for chatName, spec := range toolContext.chatNameToSpec {
			if spec.name == name && spec.namespace == namespace {
				return chatName
			}
		}
	}
	if namespace != "" {
		return toolNameBridge.Sanitize(namespace + "." + name)
	}
	return toolNameBridge.Sanitize(name)
}

func buildCustomToolChatArguments(input gjson.Result) string {
	value := ""
	if input.Exists() {
		if input.Type == gjson.String {
			value = input.String()
		} else {
			value = stringifyResponsesValue(input)
		}
	}
	raw, err := json.Marshal(map[string]any{
		customToolInputField: value,
	})
	if err != nil {
		return fmt.Sprintf(`{"%s":%q}`, customToolInputField, value)
	}
	return string(raw)
}

func stringifyResponsesToolSearchArguments(arguments gjson.Result) string {
	if !arguments.Exists() {
		return "{}"
	}
	if arguments.Type == gjson.String {
		return canonicalizeJSONIfPossible(arguments.String())
	}
	return canonicalizeJSONIfPossible(arguments.Raw)
}

func canonicalizeJSONIfPossible(raw string) string {
	if raw == "" {
		return raw
	}
	var value any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return raw
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return raw
	}
	return string(normalized)
}

func buildToolSearchChatTool() map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        toolSearchProxyName,
			"description": "Search and load tools for the current task.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type": "string",
					},
					"limit": map[string]any{
						"type": "integer",
					},
				},
				"required": []string{"query"},
			},
		},
	}
}

type responsesToolNameBridge struct {
	originalToSanitized map[string]string
	sanitizedToOriginal map[string]string
}

func newResponsesToolNameBridge() *responsesToolNameBridge {
	return &responsesToolNameBridge{
		originalToSanitized: map[string]string{},
		sanitizedToOriginal: map[string]string{},
	}
}

func (b *responsesToolNameBridge) Sanitize(name string) string {
	if name == "" {
		return ""
	}
	if sanitized, ok := b.originalToSanitized[name]; ok {
		return sanitized
	}

	sanitized := sanitizeToolName(name)
	if existing, ok := b.sanitizedToOriginal[sanitized]; ok && existing != name {
		for suffix := 2; ; suffix++ {
			candidate := fmt.Sprintf("%s_%d", sanitized, suffix)
			if _, exists := b.sanitizedToOriginal[candidate]; !exists {
				sanitized = candidate
				break
			}
		}
	}

	b.originalToSanitized[name] = sanitized
	b.sanitizedToOriginal[sanitized] = name
	return sanitized
}

func (b *responsesToolNameBridge) SanitizedToOriginal() map[string]string {
	if len(b.sanitizedToOriginal) == 0 {
		return nil
	}
	result := make(map[string]string, len(b.sanitizedToOriginal))
	for sanitized, original := range b.sanitizedToOriginal {
		result[sanitized] = original
	}
	return result
}

func sanitizeToolName(name string) string {
	if validChatCompletionToolNamePattern.MatchString(name) {
		return name
	}

	var sanitized strings.Builder
	for index, r := range name {
		valid := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '-' || (index > 0 && r >= '0' && r <= '9')
		if valid {
			sanitized.WriteRune(r)
			continue
		}
		sanitized.WriteByte('_')
	}
	result := sanitized.String()
	if result == "" {
		return "tool"
	}
	first := result[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		result = "tool_" + result
	}
	return result
}

func restoreToolName(name string, toolNameMap map[string]string) string {
	if toolNameMap == nil {
		return name
	}
	if restored, ok := toolNameMap[name]; ok {
		return restored
	}
	return name
}

func prependSystemMessageToChatCompletions(body []byte, instructions string) ([]byte, error) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() {
		return sjson.SetRawBytes(body, "messages", []byte(fmt.Sprintf(`[{"role":"system","content":%q}]`, instructions)))
	}
	prefix := fmt.Sprintf(`[{"role":"system","content":%q},`, instructions)
	return sjson.SetRawBytes(body, "messages", []byte(prefix+strings.TrimPrefix(messages.Raw, "[")))
}

func extractChatCompletionUsage(body []byte) *usage {
	usageData := gjson.GetBytes(body, "usage")
	if !usageData.Exists() || !usageData.IsObject() {
		usageData = gjson.GetBytes(body, "choices.0.usage")
		if !usageData.Exists() || !usageData.IsObject() {
			return nil
		}
	}
	result := &usage{
		PromptTokens:     int(usageData.Get("prompt_tokens").Int()),
		CompletionTokens: int(usageData.Get("completion_tokens").Int()),
		TotalTokens:      int(usageData.Get("total_tokens").Int()),
	}
	if cachedTokens := usageData.Get("prompt_tokens_details.cached_tokens").Int(); cachedTokens > 0 {
		result.PromptTokensDetails = &promptTokensDetails{
			CachedTokens: int(cachedTokens),
		}
	} else if cachedTokens := usageData.Get("cached_tokens").Int(); cachedTokens > 0 {
		result.PromptTokensDetails = &promptTokensDetails{
			CachedTokens: int(cachedTokens),
		}
	}
	if reasoningTokens := usageData.Get("completion_tokens_details.reasoning_tokens").Int(); reasoningTokens > 0 {
		result.CompletionTokensDetails = &completionTokensDetails{
			ReasoningTokens: int(reasoningTokens),
		}
	}
	return result
}

func buildResponsesResponseFromChatCompletion(response *chatCompletionResponse, toolContext *ResponsesToolContext, toolNameMap map[string]string) map[string]any {
	status := "completed"
	result := map[string]any{
		"id":         response.Id,
		"object":     "response",
		"created_at": response.Created,
		"model":      response.Model,
		"status":     status,
	}

	output := make([]any, 0)
	outputText := ""
	if len(response.Choices) > 0 {
		choice := response.Choices[0]
		if choice.Message != nil {
			msg := choice.Message
			// 与 cc-switch 的 chat_reasoning_text 对齐：提取消息级推理文本，回挂到每个
			// tool call item 的 reasoning_content 字段，供 chat-completions 下游客户端读取。
			messageReasoning := firstNonEmpty(msg.ReasoningContent, msg.Reasoning)
			if content := msg.StringContent(); content != "" {
				outputText = content
				output = append(output, map[string]any{
					"type": "message",
					"role": firstNonEmpty(msg.Role, roleAssistant),
					"content": []any{
						map[string]any{
							"type": "output_text",
							"text": content,
						},
					},
				})
			}
			for _, toolCall := range msg.ToolCalls {
				restoredName := restoreToolName(toolCall.Function.Name, toolNameMap)
				itemID := buildResponsesToolCallItemID(toolCall.Id, toolCall.Function.Name, toolContext)
				output = append(output, buildResponsesToolCallItem(
					itemID,
					toolCall.Id,
					toolCall.Function.Name,
					restoredName,
					toolCall.Function.Arguments,
					messageReasoning,
					"completed",
					toolContext,
				))
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason == finishReasonLength {
			status = "incomplete"
			result["status"] = status
			result["incomplete_details"] = map[string]any{
				"reason": "max_output_tokens",
			}
		}
	}
	result["output"] = output
	result["output_text"] = outputText
	if response.Usage != nil {
		result["usage"] = buildResponsesUsage(response.Usage)
	}
	return result
}

func buildResponsesToolCallItemID(callID, chatName string, toolContext *ResponsesToolContext) string {
	if spec, ok := toolContext.lookup(chatName); ok && spec.kind == responsesToolKindCustom {
		return "ctc_" + callID
	}
	return callID
}

func buildResponsesToolCallItem(itemID, callID, chatName, restoredName, arguments, reasoning, status string, toolContext *ResponsesToolContext) map[string]any {
	// 与 cc-switch 对齐：tool call item 可携带 reasoning_content 字段，供读取该字段的
	// chat-completions 下游客户端使用；推理正文同时走独立 reasoning output item 供
	// Responses-schema 客户端使用。reasoning 为空时不挂该字段。
	var item map[string]any
	if spec, ok := toolContext.lookup(chatName); ok {
		switch spec.kind {
		case responsesToolKindFunction:
			item = buildResponsesFunctionCallItem(itemID, callID, spec.name, arguments, status)
		case responsesToolKindCustom:
			item = map[string]any{
				"id":      itemID,
				"type":    "custom_tool_call",
				"call_id": callID,
				"name":    spec.name,
				"input":   customToolInputFromChatArguments(arguments),
				"status":  status,
			}
		case responsesToolKindToolSearch:
			item = map[string]any{
				"type":      "tool_search_call",
				"call_id":   callID,
				"status":    status,
				"execution": "client",
				"arguments": parseToolSearchArguments(arguments),
			}
		case responsesToolKindNamespace:
			item = map[string]any{
				"id":        itemID,
				"type":      "function_call",
				"call_id":   callID,
				"name":      spec.name,
				"namespace": spec.namespace,
				"arguments": arguments,
				"status":    status,
			}
		default:
			item = buildResponsesFunctionCallItem(itemID, callID, restoredName, arguments, status)
		}
	} else {
		item = buildResponsesFunctionCallItem(itemID, callID, restoredName, arguments, status)
	}
	attachReasoningContentField(item, reasoning)
	return item
}

func buildResponsesFunctionCallItem(itemID, callID, name, arguments, status string) map[string]any {
	return map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      name,
		"arguments": arguments,
		"status":    status,
	}
}

// attachReasoningContentField mirrors cc-switch's attach_optional_reasoning_content_field:
// only stamp reasoning_content onto the tool call item when non-empty (after trim), so the
// field is absent rather than empty for tool calls without reasoning.
func attachReasoningContentField(item map[string]any, reasoning string) {
	if item == nil || strings.TrimSpace(reasoning) == "" {
		return
	}
	item["reasoning_content"] = reasoning
}

func customToolInputFromChatArguments(arguments string) string {
	if arguments == "" {
		return ""
	}
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err != nil {
		return arguments
	}
	object, ok := value.(map[string]any)
	if !ok {
		return arguments
	}
	input, ok := object[customToolInputField]
	if !ok {
		return arguments
	}
	if text, ok := input.(string); ok {
		return text
	}
	normalized, err := json.Marshal(input)
	if err != nil {
		return arguments
	}
	return string(normalized)
}

func parseToolSearchArguments(arguments string) any {
	if arguments == "" {
		return map[string]any{}
	}
	var value any
	if err := json.Unmarshal([]byte(arguments), &value); err != nil {
		return map[string]any{
			"query": arguments,
		}
	}
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{
		"query": arguments,
	}
}

func buildResponsesUsage(usageData *usage) map[string]any {
	if usageData == nil {
		return nil
	}
	result := map[string]any{
		"input_tokens":  usageData.PromptTokens,
		"output_tokens": usageData.CompletionTokens,
		"total_tokens":  usageData.TotalTokens,
	}
	if usageData.PromptTokensDetails != nil && usageData.PromptTokensDetails.CachedTokens > 0 {
		result["input_tokens_details"] = map[string]any{
			"cached_tokens": usageData.PromptTokensDetails.CachedTokens,
		}
	}
	if usageData.CompletionTokensDetails != nil && usageData.CompletionTokensDetails.ReasoningTokens > 0 {
		result["output_tokens_details"] = map[string]any{
			"reasoning_tokens": usageData.CompletionTokensDetails.ReasoningTokens,
		}
	}
	return result
}

type ChatCompletionResponsesBridge struct {
	id              string
	model           string
	created         int64
	rolePushed      bool
	textPushed      bool
	toolCallsPushed bool
	toolCallNames   map[string]string
	IncludeUsage    bool
}

func (b *ChatCompletionResponsesBridge) ConvertStreamingResponseToChatCompletion(ctx wrapper.HttpContext, data []byte) ([]byte, error) {
	events := extractStreamingEventsWithBuffer(ctx, ctxKeyChatCompletionBridgeStreamingBody, data)

	var out strings.Builder
	for _, event := range events {
		switch event.Event {
		case "response.created":
			responseData := extractResponsesPayload([]byte(event.Data))
			b.captureResponseMetadata(responseData)
			if !b.rolePushed {
				chunk, err := b.buildRoleChunk()
				if err != nil {
					return nil, err
				}
				out.WriteString(chunk.ToHttpString())
				b.rolePushed = true
			}
		case "response.output_text.delta":
			if !b.rolePushed {
				chunk, err := b.buildRoleChunk()
				if err != nil {
					return nil, err
				}
				out.WriteString(chunk.ToHttpString())
				b.rolePushed = true
			}
			delta := gjson.GetBytes([]byte(event.Data), "delta").String()
			if delta == "" {
				continue
			}
			chunk, err := b.buildContentChunk(delta)
			if err != nil {
				return nil, err
			}
			out.WriteString(chunk.ToHttpString())
			b.textPushed = true
		case "response.function_call_arguments.delta":
			callID := gjson.GetBytes([]byte(event.Data), "call_id").String()
			name := gjson.GetBytes([]byte(event.Data), "name").String()
			if name != "" {
				if b.toolCallNames == nil {
					b.toolCallNames = map[string]string{}
				}
				b.toolCallNames[callID] = name
			}
			delta := gjson.GetBytes([]byte(event.Data), "delta").String()
			chunk, err := b.buildToolCallChunk(callID, b.toolCallNames[callID], delta)
			if err != nil {
				return nil, err
			}
			out.WriteString(chunk.ToHttpString())
			b.toolCallsPushed = true
		case "response.completed":
			responseData := extractResponsesPayload([]byte(event.Data))
			b.captureResponseMetadata(responseData)

			chatResponse, err := buildChatCompletionResponseFromResponses(responseData)
			if err != nil {
				return nil, err
			}

			if len(chatResponse.Choices) > 0 {
				choice := chatResponse.Choices[0]
				if choice.Message != nil {
					if !b.rolePushed {
						roleChunk, err := b.buildRoleChunk()
						if err != nil {
							return nil, err
						}
						out.WriteString(roleChunk.ToHttpString())
						b.rolePushed = true
					}
					if !b.textPushed {
						if content := choice.Message.StringContent(); content != "" {
							contentChunk, err := b.buildContentChunk(content)
							if err != nil {
								return nil, err
							}
							out.WriteString(contentChunk.ToHttpString())
						}
					}
					if !b.toolCallsPushed && len(choice.Message.ToolCalls) > 0 {
						for _, toolCall := range choice.Message.ToolCalls {
							toolCallChunk, err := b.buildToolCallChunk(toolCall.Id, toolCall.Function.Name, toolCall.Function.Arguments)
							if err != nil {
								return nil, err
							}
							out.WriteString(toolCallChunk.ToHttpString())
						}
					}
				}

				finishReason := finishReasonStop
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					finishReason = *choice.FinishReason
				}
				var usageData *usage
				if b.IncludeUsage {
					usageData = chatResponse.Usage
				}
				finishChunk, err := b.buildFinishChunk(finishReason, usageData)
				if err != nil {
					return nil, err
				}
				out.WriteString(finishChunk.ToHttpString())
			}

			out.WriteString((&StreamEvent{Data: streamEndDataValue}).ToHttpString())
		}
	}

	return []byte(out.String()), nil
}

func (b *ChatCompletionResponsesBridge) captureResponseMetadata(body []byte) {
	if id := gjson.GetBytes(body, "id").String(); id != "" {
		b.id = id
	}
	if model := gjson.GetBytes(body, "model").String(); model != "" {
		b.model = model
	}
	if created := gjson.GetBytes(body, "created_at").Int(); created > 0 {
		b.created = created
	}
}

func (b *ChatCompletionResponsesBridge) buildRoleChunk() (*StreamEvent, error) {
	payload, err := buildChatCompletionChunkPayload(b.id, b.model, b.created, &chatMessage{
		Role: roleAssistant,
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Data: string(payload)}, nil
}

func (b *ChatCompletionResponsesBridge) buildContentChunk(content string) (*StreamEvent, error) {
	payload, err := buildChatCompletionChunkPayload(b.id, b.model, b.created, &chatMessage{
		Content: content,
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Data: string(payload)}, nil
}

func (b *ChatCompletionResponsesBridge) buildToolCallChunk(callID, name, arguments string) (*StreamEvent, error) {
	payload, err := buildChatCompletionChunkPayload(b.id, b.model, b.created, &chatMessage{
		ToolCalls: []toolCall{
			{
				Index: 0,
				Id:    callID,
				Type:  "function",
				Function: functionCall{
					Name:      name,
					Arguments: arguments,
				},
			},
		},
	}, nil, nil)
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Data: string(payload)}, nil
}

func (b *ChatCompletionResponsesBridge) buildFinishChunk(finishReason string, usageData *usage) (*StreamEvent, error) {
	payload, err := buildChatCompletionChunkPayload(b.id, b.model, b.created, &chatMessage{}, &finishReason, usageData)
	if err != nil {
		return nil, err
	}
	return &StreamEvent{Data: string(payload)}, nil
}

func buildChatCompletionChunkPayload(id, model string, created int64, delta *chatMessage, finishReason *string, usageData *usage) ([]byte, error) {
	response := &chatCompletionResponse{
		Id:      id,
		Object:  objectChatCompletionChunk,
		Created: created,
		Model:   model,
		Choices: []chatCompletionChoice{
			{
				Index:        0,
				Delta:        delta,
				FinishReason: finishReason,
			},
		},
		Usage: usageData,
	}
	return json.Marshal(response)
}

func buildChatCompletionResponseFromResponses(body []byte) (*chatCompletionResponse, error) {
	if gjson.GetBytes(body, "object").String() == objectChatCompletion {
		response := &chatCompletionResponse{}
		if err := json.Unmarshal(body, response); err != nil {
			return nil, err
		}
		return response, nil
	}

	message := &chatMessage{
		Role: roleAssistant,
	}

	var contentBuilder strings.Builder
	var toolCalls []toolCall
	output := gjson.GetBytes(body, "output")
	if output.Exists() && output.IsArray() {
		for _, item := range output.Array() {
			switch item.Get("type").String() {
			case "message":
				if role := item.Get("role").String(); role != "" {
					message.Role = role
				}
				for _, contentItem := range item.Get("content").Array() {
					switch contentItem.Get("type").String() {
					case "output_text", "text":
						contentBuilder.WriteString(contentItem.Get("text").String())
					case "refusal":
						message.Refusal = contentItem.Get("refusal").String()
					}
				}
			case "function_call":
				toolCalls = append(toolCalls, toolCall{
					Id:   firstNonEmpty(item.Get("call_id").String(), item.Get("id").String()),
					Type: "function",
					Function: functionCall{
						Name:      item.Get("name").String(),
						Arguments: item.Get("arguments").String(),
					},
				})
			}
		}
	}

	if contentBuilder.Len() == 0 {
		contentBuilder.WriteString(gjson.GetBytes(body, "output_text").String())
	}
	message.Content = contentBuilder.String()
	if len(toolCalls) > 0 {
		message.ToolCalls = toolCalls
	}

	finishReason := inferResponsesFinishReason(body, len(toolCalls) > 0)
	response := &chatCompletionResponse{
		Id:      gjson.GetBytes(body, "id").String(),
		Object:  objectChatCompletion,
		Created: firstNonZero(gjson.GetBytes(body, "created_at").Int(), gjson.GetBytes(body, "created").Int()),
		Model:   gjson.GetBytes(body, "model").String(),
		Choices: []chatCompletionChoice{
			{
				Index:        0,
				Message:      message,
				FinishReason: &finishReason,
			},
		},
		Usage: extractResponsesUsage(body),
	}
	return response, nil
}

func extractResponsesUsage(body []byte) *usage {
	usageData := gjson.GetBytes(body, "usage")
	if !usageData.Exists() || !usageData.IsObject() {
		return nil
	}

	result := &usage{
		PromptTokens:     int(usageData.Get("input_tokens").Int()),
		CompletionTokens: int(usageData.Get("output_tokens").Int()),
		TotalTokens:      int(usageData.Get("total_tokens").Int()),
	}

	if reasoningTokens := usageData.Get("output_tokens_details.reasoning_tokens").Int(); reasoningTokens > 0 {
		result.CompletionTokensDetails = &completionTokensDetails{
			ReasoningTokens: int(reasoningTokens),
		}
	}
	if cachedTokens := usageData.Get("input_tokens_details.cached_tokens").Int(); cachedTokens > 0 {
		result.PromptTokensDetails = &promptTokensDetails{
			CachedTokens: int(cachedTokens),
		}
	}

	return result
}

func inferResponsesFinishReason(body []byte, hasToolCalls bool) string {
	if hasToolCalls {
		return finishReasonToolCall
	}
	switch gjson.GetBytes(body, "incomplete_details.reason").String() {
	case "max_output_tokens":
		return finishReasonLength
	default:
		return finishReasonStop
	}
}

func extractResponsesPayload(body []byte) []byte {
	if response := gjson.GetBytes(body, "response"); response.Exists() && response.IsObject() {
		return []byte(response.Raw)
	}
	return body
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
