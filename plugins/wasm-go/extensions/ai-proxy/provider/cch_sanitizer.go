package provider

import "strings"

const anthropicBillingHeaderPrefix = "x-anthropic-billing-header:"

// stripDynamicCCHField 按行清洗文本里的 billing header，只移除动态 cch 字段，
// 保留其余稳定元信息。返回值中的 bool 表示本次是否真的发生了修改。
func stripDynamicCCHField(text string) (string, bool) {
	if text == "" {
		return text, false
	}

	// billing header 可能混在多行 system prompt 中，因此按行处理更稳妥。
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if !strings.HasPrefix(strings.ToLower(trimmed), anthropicBillingHeaderPrefix) {
			out = append(out, line)
			continue
		}

		// 保留原始前导空白，尽量不破坏调用方依赖的文本格式。
		leadingWhitespaceLen := len(line) - len(strings.TrimLeft(line, " \t"))
		leadingWhitespace := line[:leadingWhitespaceLen]
		prefix := trimmed[:len(anthropicBillingHeaderPrefix)]
		rest := strings.TrimSpace(trimmed[len(anthropicBillingHeaderPrefix):])

		segments := strings.Split(rest, ";")
		kept := make([]string, 0, len(segments))
		removed := false
		for _, segment := range segments {
			segment = strings.TrimSpace(segment)
			if segment == "" {
				continue
			}

			// 只识别键名为 cch 的 segment，避免误删其他 cc_* 字段。
			key, _, hasValue := strings.Cut(segment, "=")
			if hasValue && strings.EqualFold(strings.TrimSpace(key), "cch") {
				removed = true
				continue
			}

			kept = append(kept, segment)
		}

		if !removed {
			out = append(out, line)
			continue
		}
		changed = true
		// 如果某一行删完后已经没有有效内容，就直接丢掉这一行。
		if len(kept) == 0 {
			continue
		}

		out = append(out, leadingWhitespace+prefix+" "+strings.Join(kept, "; ")+";")
	}

	if !changed {
		return text, false
	}
	return strings.Join(out, "\n"), true
}

// sanitizeChatCompletionRequestCCH 用于 typed chat.completions DTO 路径。
// 这里保留 OpenAI 风格 system/developer message 的清洗，因为 Claude Code 降级到
// /v1/chat/completions 后，动态 cch 会落在这类消息内容里。
func sanitizeChatCompletionRequestCCH(request *chatCompletionRequest) bool {
	if request == nil {
		return false
	}

	if len(request.Messages) == 0 {
		return false
	}

	out := make([]chatMessage, 0, len(request.Messages))
	changed := false
	for _, message := range request.Messages {
		// 只有 system / developer prompt 会参与这类清洗，普通 user 内容保持原样。
		if message.Role != roleSystem && message.Role != roleDeveloper {
			out = append(out, message)
			continue
		}

		content, contentChanged, keep := sanitizeStructuredTextContentCCH(message.Content)
		if !contentChanged {
			out = append(out, message)
			continue
		}

		changed = true
		if !keep {
			continue
		}

		message.Content = content
		out = append(out, message)
	}

	if !changed {
		return false
	}

	request.Messages = out
	return true
}

// sanitizeStructuredTextContentCCH 处理 OpenAI 风格 system/developer content。
// 它兼容 string 和 OpenAI text block array，返回值依次是：
// 清洗后的 content、是否发生修改、是否保留该内容。
func sanitizeStructuredTextContentCCH(content any) (any, bool, bool) {
	switch value := content.(type) {
	case string:
		sanitizedText, changed := stripDynamicCCHField(value)
		if !changed {
			return content, false, true
		}
		if strings.TrimSpace(sanitizedText) == "" {
			return nil, true, false
		}
		return sanitizedText, true, true
	case []any:
		out := make([]any, 0, len(value))
		changed := false
		for _, item := range value {
			contentMap, ok := item.(map[string]any)
			if !ok {
				out = append(out, item)
				continue
			}

			if mapStringValue(contentMap, "type") != contentTypeText {
				out = append(out, item)
				continue
			}

			// 只清洗 text block；图片、tool、thinking 等其他 block 原样保留。
			text, ok := contentMap[contentTypeText].(string)
			if !ok {
				out = append(out, item)
				continue
			}

			sanitizedText, itemChanged := stripDynamicCCHField(text)
			if itemChanged {
				changed = true
			}
			if strings.TrimSpace(sanitizedText) == "" {
				continue
			}

			contentMap[contentTypeText] = sanitizedText
			out = append(out, contentMap)
		}

		if !changed {
			return content, false, true
		}
		if len(out) == 0 {
			return nil, true, false
		}

		return out, true, true
	default:
		return content, false, true
	}
}

// sanitizeAnthropicMessagesRequestCCH 用于 typed Anthropic Messages DTO 路径。
// 当前只处理已知问题对应的两处文本来源：顶层 system 和 messages.content 里的 text。
func sanitizeAnthropicMessagesRequestCCH(request *anthropicMessagesRequest) bool {
	if request == nil {
		return false
	}

	changed := sanitizeClaudeSystemPromptCCH(&request.System)
	if len(request.Messages) == 0 {
		return changed
	}

	out := make([]claudeChatMessage, 0, len(request.Messages))
	for _, message := range request.Messages {
		if message.Content.IsString() {
			sanitizedText, contentChanged := stripDynamicCCHField(message.Content.GetStringValue())
			if !contentChanged {
				out = append(out, message)
				continue
			}

			changed = true
			if strings.TrimSpace(sanitizedText) == "" {
				continue
			}

			message.Content = newStringContent(sanitizedText)
			out = append(out, message)
			continue
		}

		contents := message.Content.GetArrayValue()
		if len(contents) == 0 {
			out = append(out, message)
			continue
		}

		sanitizedContents := make([]claudeChatMessageContent, 0, len(contents))
		contentChanged := false
		for _, content := range contents {
			if content.Type != contentTypeText || content.Text == "" {
				sanitizedContents = append(sanitizedContents, content)
				continue
			}

			sanitizedText, textChanged := stripDynamicCCHField(content.Text)
			if !textChanged {
				sanitizedContents = append(sanitizedContents, content)
				continue
			}

			contentChanged = true
			if strings.TrimSpace(sanitizedText) == "" {
				continue
			}

			content.Text = sanitizedText
			sanitizedContents = append(sanitizedContents, content)
		}

		if !contentChanged {
			out = append(out, message)
			continue
		}

		changed = true
		if len(sanitizedContents) == 0 {
			continue
		}

		message.Content = newArrayContent(sanitizedContents)
		out = append(out, message)
	}

	if changed {
		request.Messages = out
	}
	return changed
}

// sanitizeClaudeSystemPromptCCH 处理 Claude 独立 system 字段。
// Claude 的 system 同时支持 string 和 text block array 两种表示。
func sanitizeClaudeSystemPromptCCH(prompt *claudeSystemPrompt) bool {
	if prompt == nil {
		return false
	}

	if prompt.IsArray {
		out := make([]claudeTextGenContent, 0, len(prompt.ArrayValue))
		changed := false
		for _, block := range prompt.ArrayValue {
			if block.Text == "" {
				out = append(out, block)
				continue
			}

			sanitizedText, blockChanged := stripDynamicCCHField(block.Text)
			if blockChanged {
				changed = true
			}
			if strings.TrimSpace(sanitizedText) == "" {
				continue
			}

			block.Text = sanitizedText
			out = append(out, block)
		}

		if !changed {
			return false
		}
		if len(out) == 0 {
			// 数组里的文本块如果都被清空，退化成空字符串表示“没有 system”。
			prompt.IsArray = false
			prompt.ArrayValue = nil
			prompt.StringValue = ""
			return true
		}

		prompt.ArrayValue = out
		return true
	}

	sanitizedText, changed := stripDynamicCCHField(prompt.StringValue)
	if !changed {
		return false
	}

	prompt.StringValue = sanitizedText
	if strings.TrimSpace(prompt.StringValue) == "" {
		prompt.StringValue = ""
	}
	return true
}
