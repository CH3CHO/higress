package util

// MapFinishReason maps different AI provider finish_reason values to OpenAI standard format.
// OpenAI supports 5 stop sequences: 'stop', 'length', 'function_call', 'content_filter', 'null'
func MapFinishReason(finishReason string) string {
	switch finishReason {
	// Anthropic mapping
	case "stop_sequence", "end_turn":
		return "stop"

	// Cohere mapping - https://docs.cohere.com/reference/generate
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS": // cohere + vertex ai
		return "length"
	case "ERROR_TOXIC":
		return "content_filter"
	case "ERROR": // openai currently doesn't support an 'error' finish reason
		return "stop"

	// HuggingFace mapping - https://huggingface.github.io/text-generation-inference/#/Text%20Generation%20Inference/generate_stream
	case "eos_token":
		return "stop"

	// Vertex AI mapping
	case "FINISH_REASON_UNSPECIFIED", "STOP":
		return "stop"
	case "SAFETY", "RECITATION":
		return "content_filter"

	// Anthropic specific
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"

	// Generic mapping
	case "content_filtered":
		return "content_filter"

	default:
		return finishReason
	}
}
