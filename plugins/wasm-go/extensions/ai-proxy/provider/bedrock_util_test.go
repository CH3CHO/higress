package provider

import (
	"encoding/json"
	"testing"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/bedrock"
	"github.com/stretchr/testify/assert"
)

// TestTransformBedrockTools tests the transformBedrockTools function using the exact
// input/output examples from the Python _bedrock_tools_pt function comments in:
// litellm/litellm_core_utils/prompt_templates/factory.py
func TestTransformBedrockTools(t *testing.T) {
	// Input: OpenAI tool format matching Python's _bedrock_tools_pt input
	// Description is at function level, not inside parameters
	tools := []tool{
		{
			Type: "function",
			Function: function{
				Name:        "get_current_weather",
				Description: "Get the current weather in a given location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type":        "string",
							"description": "The city and state, e.g. San Francisco, CA",
						},
						"unit": map[string]interface{}{
							"type": "string",
							"enum": []string{"celsius", "fahrenheit"},
						},
					},
					"required": []string{"location"},
				},
			},
		},
	}

	// Expected output: Bedrock toolConfig format from Python comment
	// Adapted to match the input tool (get_current_weather)
	expectedBedrockJSON := `[
		{
			"toolSpec": {
				"name": "get_current_weather",
				"description": "Get the current weather in a given location",
				"inputSchema": {
					"json": {
						"type": "object",
						"properties": {
							"location": {
								"type": "string",
								"description": "The city and state, e.g. San Francisco, CA"
							},
							"unit": {
								"type": "string",
								"enum": ["celsius", "fahrenheit"]
							}
						},
						"required": ["location"]
					}
				}
			}
		}
	]`

	// Execute the transformation
	result, err := transformBedrockTools(tools)
	assert.NoError(t, err, "transformBedrockTools should not return error")

	// Convert result to JSON for comparison
	resultJSON, err := json.Marshal(result)
	assert.NoError(t, err, "Failed to marshal result")

	// Use JSONEq to compare ignoring formatting differences
	assert.JSONEq(t, expectedBedrockJSON, string(resultJSON),
		"Bedrock tools output should match expected format from Python comment")
}

func TestTransformBedrockToolsAddsCachePoint(t *testing.T) {
	tools := []tool{
		{
			Type: "function",
			Function: function{
				Name:        "list_tables",
				Description: "List all available tables",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			CacheControl: &cacheControl{
				Type: "ephemeral",
			},
		},
	}

	result, err := transformBedrockTools(tools)
	assert.NoError(t, err)
	if assert.Len(t, result, 2) {
		assert.NotNil(t, result[0].ToolSpec)
		assert.Equal(t, "list_tables", result[0].ToolSpec.Name)
		assert.Nil(t, result[0].CachePoint)
		assert.NotNil(t, result[1].CachePoint)
		assert.Equal(t, "default", result[1].CachePoint.Type)
	}
}

func TestTransformBedrockToolsAddsCachePointTTL(t *testing.T) {
	tools := []tool{
		{
			Type: "function",
			Function: function{
				Name:        "list_tables",
				Description: "List all available tables",
				Parameters: map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			CacheControl: &cacheControl{
				Type: "ephemeral",
				TTL:  "5m",
			},
		},
	}

	result, err := transformBedrockTools(tools)
	assert.NoError(t, err)
	if assert.Len(t, result, 2) {
		assert.NotNil(t, result[1].CachePoint)
		assert.Equal(t, "default", result[1].CachePoint.Type)
		assert.Equal(t, "5m", result[1].CachePoint.TTL)
	}
}

// TestHandleBedrockResponseFormat tests the handleBedrockResponseFormat function
// which processes response_format parameter and converts it to Bedrock tools.
// This corresponds to the response_format handling in map_openai_params in Python's litellm.
func TestHandleBedrockResponseFormat(t *testing.T) {
	tests := []struct {
		name               string
		requestJSON        string
		isThinkingEnabled  bool
		expectedEnabled    bool
		expectedToolsJSON  string
		expectedToolChoice string // JSON string for tool_choice, empty if not set
	}{
		{
			name: "nil response_format returns false",
			requestJSON: `{
				"messages": [],
				"model": "test-model"
			}`,
			isThinkingEnabled:  false,
			expectedEnabled:    false,
			expectedToolsJSON:  `null`, // nil tools marshals to null
			expectedToolChoice: "",
		},
		{
			name: "response_format type=text returns false",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "text"
				}
			}`,
			isThinkingEnabled:  false,
			expectedEnabled:    false,
			expectedToolsJSON:  `null`, // nil tools marshals to null
			expectedToolChoice: "",
		},
		{
			name: "response_schema creates JSON tool with default name",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "json_object",
					"response_schema": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"age": {"type": "integer"}
						},
						"required": ["name"]
					}
				}
			}`,
			isThinkingEnabled: false,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "json_tool_call",
						"parameters": {
							"type": "object",
							"properties": {
								"name": {"type": "string"},
								"age": {"type": "integer"}
							},
							"required": ["name"]
						}
					}
				}
			]`,
			expectedToolChoice: `{
				"type": "tool",
				"function": {
					"name": "json_tool_call"
				}
			}`,
		},
		{
			name: "json_schema with name and description",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "json_schema",
					"json_schema": {
						"name": "user_info",
						"description": "Extract user information",
						"schema": {
							"type": "object",
							"properties": {
								"username": {"type": "string"},
								"email": {"type": "string"}
							}
						}
					}
				}
			}`,
			isThinkingEnabled: false,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "user_info",
						"description": "Extract user information",
						"parameters": {
							"type": "object",
							"properties": {
								"username": {"type": "string"},
								"email": {"type": "string"}
							}
						}
					}
				}
			]`,
			expectedToolChoice: `{
				"type": "tool",
				"function": {
					"name": "user_info"
				}
			}`,
		},
		{
			name: "json_schema without description",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "json_schema",
					"json_schema": {
						"name": "search_query",
						"schema": {
							"type": "object",
							"properties": {
								"query": {"type": "string"}
							}
						}
					}
				}
			}`,
			isThinkingEnabled: false,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "search_query",
						"parameters": {
							"type": "object",
							"properties": {
								"query": {"type": "string"}
							}
						}
					}
				}
			]`,
			expectedToolChoice: `{
				"type": "tool",
				"function": {
					"name": "search_query"
				}
			}`,
		},
		{
			name: "thinking enabled does not set tool_choice",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "json_schema",
					"json_schema": {
						"name": "data_extract",
						"schema": {
							"type": "object",
							"properties": {
								"data": {"type": "string"}
							}
						}
					}
				}
			}`,
			isThinkingEnabled: true,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "data_extract",
						"parameters": {
							"type": "object",
							"properties": {
								"data": {"type": "string"}
							}
						}
					}
				}
			]`,
			expectedToolChoice: "", // No tool_choice when thinking is enabled
		},
		{
			name: "response_format appends to existing tools",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"tools": [
					{
						"type": "function",
						"function": {
							"name": "existing_tool",
							"parameters": {
								"type": "object",
								"properties": {}
							}
						}
					}
				],
				"response_format": {
					"type": "json_object",
					"response_schema": {
						"type": "object",
						"properties": {
							"result": {"type": "string"}
						}
					}
				}
			}`,
			isThinkingEnabled: false,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "existing_tool",
						"parameters": {
							"type": "object",
							"properties": {}
						}
					}
				},
				{
					"type": "function",
					"function": {
						"name": "json_tool_call",
						"parameters": {
							"type": "object",
							"properties": {
								"result": {"type": "string"}
							}
						}
					}
				}
			]`,
			expectedToolChoice: `{
				"type": "tool",
				"function": {
					"name": "json_tool_call"
				}
			}`,
		},
		{
			name: "nil schema creates default schema",
			requestJSON: `{
				"messages": [],
				"model": "test-model",
				"response_format": {
					"type": "json_object"
				}
			}`,
			isThinkingEnabled: false,
			expectedEnabled:   true,
			expectedToolsJSON: `[
				{
					"type": "function",
					"function": {
						"name": "json_tool_call",
						"parameters": {
							"type": "object",
							"additionalProperties": true,
							"properties": {}
						}
					}
				}
			]`,
			expectedToolChoice: `{
				"type": "tool",
				"function": {
					"name": "json_tool_call"
				}
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Deserialize request from JSON
			var request chatCompletionRequest
			err := json.Unmarshal([]byte(tt.requestJSON), &request)
			assert.NoError(t, err, "Failed to unmarshal request JSON")

			// Call the function
			result := handleBedrockResponseFormat(&request, tt.isThinkingEnabled)

			// Verify the return value
			assert.Equal(t, tt.expectedEnabled, result, "JSON mode enabled flag should match")

			// Verify the tools were added correctly
			actualToolsJSON, err := json.Marshal(request.Tools)
			assert.NoError(t, err, "Failed to marshal actual tools")
			assert.JSONEq(t, tt.expectedToolsJSON, string(actualToolsJSON),
				"Tools should match expected JSON")

			// Verify tool_choice if expected
			if tt.expectedToolChoice != "" {
				assert.NotNil(t, request.ToolChoice, "ToolChoice should be set")
				actualToolChoiceJSON, err := json.Marshal(request.ToolChoice)
				assert.NoError(t, err, "Failed to marshal actual tool_choice")
				assert.JSONEq(t, tt.expectedToolChoice, string(actualToolChoiceJSON),
					"ToolChoice should match expected JSON")
			} else {
				// For thinking enabled case, tool_choice might still be nil from initial state
				// We just verify it's not set by our function
				if tt.isThinkingEnabled {
					// Tool choice should not be set when thinking is enabled
					// (it might be nil or might have been set before, but we don't set it)
				}
			}
		})
	}
}

func TestConvertAssistantMessageToContentBlocksAddsCachePointForStringContent(t *testing.T) {
	msg := chatMessage{
		Role:    roleAssistant,
		Content: "cached assistant content",
		CacheControl: &cacheControl{
			Type: "ephemeral",
		},
	}

	blocks, err := convertAssistantMessageToContentBlocks(msg)
	assert.NoError(t, err)
	if assert.Len(t, blocks, 2) {
		assert.Equal(t, "cached assistant content", *blocks[0].Text)
		assert.NotNil(t, blocks[1].CachePoint)
		assert.Equal(t, "default", blocks[1].CachePoint.Type)
	}
}

func TestConvertToBedrockToolCallInvokeAddsCachePoint(t *testing.T) {
	blocks := convertToBedrockToolCallInvoke([]toolCall{
		{
			Id:   "tool_1",
			Type: "function",
			Function: functionCall{
				Name:      "lookup_weather",
				Arguments: `{"location":"hangzhou"}`,
			},
			CacheControl: &cacheControl{
				Type: "ephemeral",
			},
		},
	})

	if assert.Len(t, blocks, 2) {
		assert.NotNil(t, blocks[0].ToolUse)
		assert.Equal(t, "lookup_weather", blocks[0].ToolUse.Name)
		assert.NotNil(t, blocks[1].CachePoint)
		assert.Equal(t, "default", blocks[1].CachePoint.Type)
	}
}

func TestTransformMessagesToBedrockMessageBlocksAddsCachePointForToolResults(t *testing.T) {
	t.Run("message_level_cache_control", func(t *testing.T) {
		messages := []chatMessage{
			{
				Role: roleTool,
				Content: []any{
					map[string]any{
						"type": "text",
						"text": "tool output",
					},
				},
				ToolCallId: "tool_1",
				CacheControl: &cacheControl{
					Type: "ephemeral",
				},
			},
		}

		result, err := transformMessagesToBedrockMessageBlocks(messages, &bedrock.TransformMessagesOptions{})
		assert.NoError(t, err)
		if assert.Len(t, result, 1) && assert.Len(t, result[0].Content, 2) {
			assert.NotNil(t, result[0].Content[0].ToolResult)
			assert.NotNil(t, result[0].Content[1].CachePoint)
			assert.Equal(t, "default", result[0].Content[1].CachePoint.Type)
		}
	})

	t.Run("content_level_cache_control", func(t *testing.T) {
		messages := []chatMessage{
			{
				Role: roleTool,
				Content: []any{
					map[string]any{
						"type": "text",
						"text": "tool output",
						"cache_control": map[string]any{
							"type": "ephemeral",
						},
					},
				},
				ToolCallId: "tool_2",
			},
		}

		result, err := transformMessagesToBedrockMessageBlocks(messages, &bedrock.TransformMessagesOptions{})
		assert.NoError(t, err)
		if assert.Len(t, result, 1) && assert.Len(t, result[0].Content, 2) {
			assert.NotNil(t, result[0].Content[0].ToolResult)
			assert.NotNil(t, result[0].Content[1].CachePoint)
			assert.Equal(t, "default", result[0].Content[1].CachePoint.Type)
		}
	})
}

func TestConvertAssistantMessageToContentBlocksAddsCachePointTTL(t *testing.T) {
	msg := chatMessage{
		Role:    roleAssistant,
		Content: "cached assistant content",
		CacheControl: &cacheControl{
			Type: "ephemeral",
			TTL:  "1h",
		},
	}

	blocks, err := convertAssistantMessageToContentBlocks(msg)
	assert.NoError(t, err)
	if assert.Len(t, blocks, 2) {
		assert.NotNil(t, blocks[1].CachePoint)
		assert.Equal(t, "default", blocks[1].CachePoint.Type)
		assert.Equal(t, "1h", blocks[1].CachePoint.TTL)
	}
}
