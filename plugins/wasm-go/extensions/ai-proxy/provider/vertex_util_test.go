package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/vertex"
)

// TestGeminiConvertMessagesWithHistory_ToolCallWeather tests the conversion of messages with tool calls
func TestGeminiConvertMessagesWithHistory_ToolCallWeather(t *testing.T) {
	inputJSON := `[
		{
			"role": "system",
			"content": "Your name is Litellm Bot, you are a helpful assistant"
		},
		{
			"role": "user",
			"content": "Hello, what is your name and can you tell me the weather?"
		},
		{
			"role": "assistant",
			"content": "",
			"tool_calls": [
				{
					"id": "call_123",
					"type": "function",
					"index": 0,
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\":\"San Francisco, CA\"}"
					}
				}
			]
		},
		{
			"role": "tool",
			"tool_call_id": "call_123",
			"content": "27 degrees celsius and clear in San Francisco, CA"
		}
	]`

	// Note: System message is merged with user message in Go implementation
	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "Your name is Litellm Bot, you are a helpful assistant"
				},
				{
					"text": "Hello, what is your name and can you tell me the weather?"
				}
			]
		},
		{
			"role": "model",
			"parts": [
				{
					"functionCall": {
						"name": "get_weather",
						"args": {
							"location": "San Francisco, CA"
						}
					}
				}
			]
		},
		{
			"parts": [
				{
					"functionResponse": {
						"name": "get_weather",
						"response": {
							"content": "27 degrees celsius and clear in San Francisco, CA"
						}
					}
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// TestGeminiConvertMessagesWithHistory_NestedContent tests nested content arrays
func TestGeminiConvertMessagesWithHistory_NestedContent(t *testing.T) {
	inputJSON := `[
		{
			"role": "user",
			"content": [
				{
					"type": "text",
					"text": "hi"
				}
			]
		},
		{
			"role": "assistant",
			"content": [
				{
					"type": "text",
					"text": "Hi! 👋 \n\nHow can I help you today? 😊 \n"
				}
			]
		},
		{
			"role": "user",
			"content": [
				{
					"type": "text",
					"text": "hi 2nd time"
				}
			]
		}
	]`

	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "hi"
				}
			]
		},
		{
			"role": "model",
			"parts": [
				{
					"text": "Hi! 👋 \n\nHow can I help you today? 😊 \n"
				}
			]
		},
		{
			"role": "user",
			"parts": [
				{
					"text": "hi 2nd time"
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// TestGeminiConvertMessagesWithHistory_FunctionCall tests function_call (legacy format)
func TestGeminiConvertMessagesWithHistory_FunctionCall(t *testing.T) {
	inputJSON := `[
		{
			"role": "user",
			"content": "search for weather in boston (use ` + "`search`" + `)"
		},
		{
			"role": "assistant",
			"content": "Sure, let me check.",
			"function_call": {
				"name": "search",
				"arguments": "{\"queries\": [\"weather in boston\"]}"
			}
		},
		{
			"role": "function",
			"name": "search",
			"content": "The weather in Boston is 100 degrees."
		}
	]`

	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "search for weather in boston (use ` + "`search`" + `)"
				}
			]
		},
		{
			"role": "model",
			"parts": [
				{
					"text": "Sure, let me check."
				},
				{
					"functionCall": {
						"name": "search",
						"args": {
							"queries": ["weather in boston"]
						}
					}
				}
			]
		},
		{
			"parts": [
				{
					"functionResponse": {
						"name": "search",
						"response": {
							"content": "The weather in Boston is 100 degrees."
						}
					}
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// TestGeminiConvertMessagesWithHistory_SimpleUserAssistant tests simple user-assistant conversation
func TestGeminiConvertMessagesWithHistory_SimpleUserAssistant(t *testing.T) {
	inputJSON := `[
		{
			"role": "user",
			"content": "Hello, how are you?"
		},
		{
			"role": "assistant",
			"content": "I'm doing well, thank you!"
		},
		{
			"role": "user",
			"content": "What's the weather like?"
		}
	]`

	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "Hello, how are you?"
				}
			]
		},
		{
			"role": "model",
			"parts": [
				{
					"text": "I'm doing well, thank you!"
				}
			]
		},
		{
			"role": "user",
			"parts": [
				{
					"text": "What's the weather like?"
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// TestGeminiConvertMessagesWithHistory_SystemMessage tests system message handling
func TestGeminiConvertMessagesWithHistory_SystemMessage(t *testing.T) {
	// Input messages with system message
	inputJSON := `[
		{
			"role": "system",
			"content": "You are a helpful assistant"
		},
		{
			"role": "user",
			"content": "Hello"
		}
	]`

	// Expected output - system messages are merged into user messages
	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "You are a helpful assistant"
				},
				{
					"text": "Hello"
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// TestGeminiConvertMessagesWithHistory_MultipleToolCalls tests multiple tool calls in one message
func TestGeminiConvertMessagesWithHistory_MultipleToolCalls(t *testing.T) {
	// Input messages with multiple tool calls
	inputJSON := `[
		{
			"role": "user",
			"content": "What's the weather in SF and Boston?"
		},
		{
			"role": "assistant",
			"content": "",
			"tool_calls": [
				{
					"id": "call_1",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\":\"San Francisco\"}"
					}
				},
				{
					"id": "call_2",
					"type": "function",
					"function": {
						"name": "get_weather",
						"arguments": "{\"location\":\"Boston\"}"
					}
				}
			]
		},
		{
			"role": "tool",
			"tool_call_id": "call_1",
			"content": "Sunny, 70F"
		},
		{
			"role": "tool",
			"tool_call_id": "call_2",
			"content": "Rainy, 50F"
		}
	]`

	expectedJSON := `[
		{
			"role": "user",
			"parts": [
				{
					"text": "What's the weather in SF and Boston?"
				}
			]
		},
		{
			"role": "model",
			"parts": [
				{
					"functionCall": {
						"name": "get_weather",
						"args": {
							"location": "San Francisco"
						}
					}
				},
				{
					"functionCall": {
						"name": "get_weather",
						"args": {
							"location": "Boston"
						}
					}
				}
			]
		},
		{
			"parts": [
				{
					"functionResponse": {
						"name": "get_weather",
						"response": {
							"content": "Sunny, 70F"
						}
					}
				},
				{
					"functionResponse": {
						"name": "get_weather",
						"response": {
							"content": "Rainy, 50F"
						}
					}
				}
			]
		}
	]`

	result := testGeminiConvertMessages(t, inputJSON)
	assert.JSONEq(t, expectedJSON, result)
}

// Helper function to convert messages and marshal result to JSON string
func testGeminiConvertMessages(t *testing.T, inputJSON string) string {
	var messages []chatMessage
	err := json.Unmarshal([]byte(inputJSON), &messages)
	require.NoError(t, err, "Failed to parse input JSON")

	result, err := geminiConvertMessagesWithHistory(messages)
	require.NoError(t, err, "geminiConvertMessagesWithHistory failed")

	resultJSON, err := json.Marshal(result)
	require.NoError(t, err, "Failed to marshal result")

	return string(resultJSON)
}

func TestCalculateVertexUsage(t *testing.T) {
	inputJSON := `{
		"promptTokenCount": 4,
		"candidatesTokenCount": 1120,
		"totalTokenCount": 1356,
		"thoughtsTokenCount": 232,
		"promptTokensDetails": [
			{
				"modality": "TEXT",
				"tokenCount": 4
			}
		],
		"candidatesTokensDetails": [
			{
				"modality": "IMAGE",
				"tokenCount": 1120
			}
		],
		"trafficType": "ON_DEMAND"
	}`

	expectedJSON := `{
		"prompt_tokens": 4,
		"completion_tokens": 1352,
		"total_tokens": 1356,
		"prompt_tokens_details": {
			"text_tokens": 4
		},
		"completion_tokens_details": {
			"reasoning_tokens": 232,
			"text_tokens": 1120,
			"image_tokens": 1120
		},
		"traffic_type": "ON_DEMAND"
	}`

	// Parse input JSON to vertex.UsageMetadata
	var usageMetadata vertex.UsageMetadata
	err := json.Unmarshal([]byte(inputJSON), &usageMetadata)
	require.NoError(t, err, "Failed to parse input JSON")

	// Call the function
	result := calculateVertexUsage(&usageMetadata)
	require.NotNil(t, result, "Result should not be nil")

	// Marshal result to JSON string
	resultJSON, err := json.Marshal(result)
	require.NoError(t, err, "Failed to marshal result")

	// Compare JSON strings
	assert.JSONEq(t, expectedJSON, string(resultJSON))
}
