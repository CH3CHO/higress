package bedrock

import (
	"encoding/json"
	"testing"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/stretchr/testify/require"
)

func TestMapToolChoiceValues_Dict(t *testing.T) {
	tests := []struct {
		name         string
		jsonInput    string
		dropParams   bool
		expectedTool string
	}{
		{
			name: "dict with valid function and name",
			jsonInput: `{
				"function": {
					"name": "get_weather"
				}
			}`,
			dropParams:   false,
			expectedTool: "get_weather",
		},
		{
			name: "dict with different tool name",
			jsonInput: `{
				"function": {
					"name": "search_database"
				}
			}`,
			dropParams:   false,
			expectedTool: "search_database",
		},
		{
			name: "dict with complex tool name",
			jsonInput: `{
				"function": {
					"name": "calculate_sum_total"
				}
			}`,
			dropParams:   false,
			expectedTool: "calculate_sum_total",
		},
		{
			name: "dict with type and function",
			jsonInput: `{
				"type": "function",
				"function": {
					"name": "query_api"
				}
			}`,
			dropParams:   false,
			expectedTool: "query_api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse JSON string into interface{}
			var toolChoice interface{}
			err := json.Unmarshal([]byte(tt.jsonInput), &toolChoice)
			require.NoError(t, err, "Failed to unmarshal JSON")

			result, err := MapToolChoiceValues(toolChoice, tt.dropParams)

			require.NoError(t, err, "MapToolChoiceValues() should not return error")
			require.NotNil(t, result, "MapToolChoiceValues() should not return nil")
			require.NotNil(t, result.Tool, "result.Tool should not be nil")
			require.Equal(t, tt.expectedTool, result.Tool.Name, "Tool name should match expected")
		})
	}
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "valid name with letters only",
			input:    "myFunction",
			expected: "myFunction",
		},
		{
			name:     "valid name with letters and underscores",
			input:    "get_weather",
			expected: "get_weather",
		},
		{
			name:     "valid name with letters, numbers and underscores",
			input:    "my_function_123",
			expected: "my_function_123",
		},
		{
			name:     "name starting with number",
			input:    "123_tool",
			expected: "a123_tool",
		},
		{
			name:     "name starting with underscore",
			input:    "_private_tool",
			expected: "a_private_tool",
		},
		{
			name:     "name with hyphens",
			input:    "get-weather-info",
			expected: "get_weather_info",
		},
		{
			name:     "name with spaces",
			input:    "get weather info",
			expected: "get_weather_info",
		},
		{
			name:     "name with mixed special characters",
			input:    "tool@name#123",
			expected: "tool_name_123",
		},
		{
			name:     "name starting with special character",
			input:    "$tool",
			expected: "a_tool",
		},
		{
			name:     "name with dots",
			input:    "my.function.name",
			expected: "my_function_name",
		},
		{
			name:     "name with unicode characters",
			input:    "tool 名称",
			expected: "tool___",
		},
		{
			name:     "complex name with mixed invalid characters",
			input:    "123-my@function.name#test",
			expected: "a123_my_function_name_test",
		},
		{
			name:     "single letter",
			input:    "a",
			expected: "a",
		},
		{
			name:     "single number",
			input:    "1",
			expected: "a1",
		},
		{
			name:     "single special character",
			input:    "@",
			expected: "a_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestGetOriginalToolName(t *testing.T) {
	toolNameCache = util.NewLRUCache("test-cache", 1024)

	tests := []struct {
		name             string
		inputToNorm      string
		normalizedName   string
		expectedOriginal string
	}{
		{
			name:             "get original name for transformed tool",
			inputToNorm:      "get-weather",
			normalizedName:   "get_weather",
			expectedOriginal: "get-weather",
		},
		{
			name:             "get original name for tool starting with number",
			inputToNorm:      "123tool",
			normalizedName:   "a123tool",
			expectedOriginal: "123tool",
		},
		{
			name:             "get original name for tool with special chars",
			inputToNorm:      "my@tool#name",
			normalizedName:   "my_tool_name",
			expectedOriginal: "my@tool#name",
		},
		{
			name:             "unchanged tool name returns itself",
			inputToNorm:      "validToolName",
			normalizedName:   "validToolName",
			expectedOriginal: "validToolName",
		},
		{
			name:             "unknown normalized name returns itself",
			inputToNorm:      "",
			normalizedName:   "unknownName",
			expectedOriginal: "unknownName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First normalize the tool name (except for the unknown case)
			if tt.inputToNorm != "" {
				normalized := NormalizeToolName(tt.inputToNorm)
				if normalized != tt.normalizedName {
					t.Fatalf("NormalizeToolName(%q) = %q, want %q", tt.inputToNorm, normalized, tt.normalizedName)
				}
			}

			// Then retrieve the original name
			original := GetOriginalToolName(tt.normalizedName)
			if original != tt.expectedOriginal {
				t.Errorf("GetOriginalToolName(%q) = %q, want %q", tt.normalizedName, original, tt.expectedOriginal)
			}
		})
	}
}

func TestToolNameCacheEviction(t *testing.T) {
	toolNameCache = util.NewLRUCache("test-cache", 5)

	// Add entries that will cause eviction
	// Each entry needs to be transformed to ensure it's stored in the cache
	testCases := []struct {
		input      string // original tool name
		normalized string // expected normalized name
	}{
		{"test@1", "test_1"}, // @ will be converted to _
		{"test@2", "test_2"}, // @ will be converted to _
		{"test@3", "test_3"}, // @ will be converted to _
		{"test@4", "test_4"}, // @ will be converted to _
		{"test@5", "test_5"}, // @ will be converted to _
		{"test@6", "test_6"}, // This should trigger eviction of "test_1"
		{"test@7", "test_7"}, // This should trigger eviction of "test_2"
	}

	for _, tc := range testCases {
		normalized := NormalizeToolName(tc.input)
		if normalized != tc.normalized {
			t.Fatalf("NormalizeToolName(%q) = %q, expected %q", tc.input, normalized, tc.normalized)
		}
	}

	// Verify cache size doesn't exceed maximum
	cacheSize := toolNameCache.Size()
	maxSize := toolNameCache.MaxSize()

	if cacheSize > maxSize {
		t.Errorf("Cache size %d exceeds maximum %d", cacheSize, maxSize)
	}

	// Verify the oldest entries were evicted
	// test_1 and test_2 should NOT be in the cache
	test1Value, test1Exists := toolNameCache.Exists("test_1")
	if test1Exists {
		t.Errorf("test_1 should have been evicted from cache, but still maps to: %v", test1Value)
	}

	test2Value, test2Exists := toolNameCache.Exists("test_2")
	if test2Exists {
		t.Errorf("test_2 should have been evicted from cache, but still maps to: %v", test2Value)
	}

	// Verify newer entries still exist in cache
	test6Value, test6Exists := toolNameCache.Exists("test_6")
	if !test6Exists {
		t.Errorf("test_6 should exist in cache")
	} else if str, ok := test6Value.(string); !ok || str != "test@6" {
		t.Errorf("test_6 should exist in cache with original 'test@6', got: %v", test6Value)
	}

	test7Value, test7Exists := toolNameCache.Exists("test_7")
	if !test7Exists {
		t.Errorf("test_7 should exist in cache")
	} else if str, ok := test7Value.(string); !ok || str != "test@7" {
		t.Errorf("test_7 should exist in cache with original 'test@7', got: %v", test7Value)
	}
}

func TestTransformReasoningContent(t *testing.T) {
	tests := []struct {
		name      string
		jsonInput string
		expected  string
	}{
		{
			name:      "empty blocks list",
			jsonInput: `[]`,
			expected:  "",
		},
		{
			name: "single block with reasoning text",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "Let me think about this problem."
					}
				}
			]`,
			expected: "Let me think about this problem.",
		},
		{
			name: "multiple blocks with reasoning text",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "First, I need to understand the question. "
					}
				},
				{
					"reasoningText": {
						"text": "Then I'll analyze the data. "
					}
				},
				{
					"reasoningText": {
						"text": "Finally, I'll provide the answer."
					}
				}
			]`,
			expected: "First, I need to understand the question. Then I'll analyze the data. Finally, I'll provide the answer.",
		},
		{
			name: "block with redacted content only",
			jsonInput: `[
				{
					"redactedContent": "hidden"
				}
			]`,
			expected: "",
		},
		{
			name: "mixed blocks with reasoning and redacted content",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "This is visible. "
					}
				},
				{
					"redactedContent": "hidden"
				},
				{
					"reasoningText": {
						"text": "This is also visible."
					}
				}
			]`,
			expected: "This is visible. This is also visible.",
		},
		{
			name: "reasoning text with signature",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "Analyzing the problem step by step.",
						"signature": "sig123"
					}
				}
			]`,
			expected: "Analyzing the problem step by step.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse JSON string into []ConverseReasoningContentBlock
			var blocks []*ConverseReasoningContentBlock
			err := json.Unmarshal([]byte(tt.jsonInput), &blocks)
			require.NoError(t, err, "Failed to unmarshal JSON")

			result := TransformReasoningContent(blocks)
			require.Equal(t, tt.expected, result, "Result should match expected")
		})
	}
}

func TestMapReasoningEffort(t *testing.T) {
	tests := []struct {
		name            string
		reasoningEffort string
		expectedType    string
		expectedTokens  int
		expectError     bool
		expectNil       bool
	}{
		{
			name:            "empty string returns nil",
			reasoningEffort: "",
			expectNil:       true,
		},
		{
			name:            "low reasoning effort",
			reasoningEffort: "low",
			expectedType:    "enabled",
			expectedTokens:  DefaultReasoningEffortLowThinkingBudget,
		},
		{
			name:            "medium reasoning effort",
			reasoningEffort: "medium",
			expectedType:    "enabled",
			expectedTokens:  DefaultReasoningEffortMediumThinkingBudget,
		},
		{
			name:            "high reasoning effort",
			reasoningEffort: "high",
			expectedType:    "enabled",
			expectedTokens:  DefaultReasoningEffortHighThinkingBudget,
		},
		{
			name:            "unsupported reasoning effort",
			reasoningEffort: "minimal",
			expectError:     true,
		},
		{
			name:            "invalid reasoning effort",
			reasoningEffort: "invalid",
			expectError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MapReasoningEffort(tt.reasoningEffort)

			if tt.expectError {
				require.Error(t, err, "MapReasoningEffort(%q) expected error", tt.reasoningEffort)
				return
			}

			require.NoError(t, err, "MapReasoningEffort(%q) unexpected error", tt.reasoningEffort)

			if tt.expectNil {
				require.Nil(t, result, "MapReasoningEffort(%q) should return nil", tt.reasoningEffort)
				return
			}

			require.NotNil(t, result, "MapReasoningEffort(%q) should return non-nil", tt.reasoningEffort)
			require.Equal(t, tt.expectedType, result["type"], "MapReasoningEffort(%q).type mismatch", tt.reasoningEffort)
			require.Equal(t, tt.expectedTokens, result["budget_tokens"], "MapReasoningEffort(%q).budget_tokens mismatch", tt.reasoningEffort)
		})
	}
}

func TestTransformThinkingBlocks(t *testing.T) {
	tests := []struct {
		name               string
		jsonInput          string
		expectedThinking   []string
		expectedRedacted   []string
		expectedSignatures []bool
	}{
		{
			name:             "empty blocks list",
			jsonInput:        `[]`,
			expectedThinking: []string{},
			expectedRedacted: []string{},
		},
		{
			name: "single reasoning text block",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "Let me analyze this step by step."
					}
				}
			]`,
			expectedThinking: []string{"Let me analyze this step by step."},
			expectedRedacted: []string{},
		},
		{
			name: "single redacted content block",
			jsonInput: `[
				{
					"redactedContent": "sensitive_info"
				}
			]`,
			expectedThinking: []string{},
			expectedRedacted: []string{"sensitive_info"},
		},
		{
			name: "multiple mixed blocks",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "First, let me think."
					}
				},
				{
					"redactedContent": "hidden_data"
				},
				{
					"reasoningText": {
						"text": "Now the conclusion."
					}
				}
			]`,
			expectedThinking: []string{"First, let me think.", "Now the conclusion."},
			expectedRedacted: []string{"hidden_data"},
		},
		{
			name: "reasoning text with signature",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "Verified thinking content.",
						"signature": "sig123abc"
					}
				}
			]`,
			expectedThinking:   []string{"Verified thinking content."},
			expectedRedacted:   []string{},
			expectedSignatures: []bool{true},
		},
		{
			name: "empty reasoning text",
			jsonInput: `[
				{
					"reasoningText": {
						"text": ""
					}
				}
			]`,
			expectedThinking: []string{""},
			expectedRedacted: []string{},
		},
		{
			name: "mixed blocks with signature",
			jsonInput: `[
				{
					"reasoningText": {
						"text": "First thought."
					}
				},
				{
					"reasoningText": {
						"text": "Signed thought.",
						"signature": "signature_value"
					}
				},
				{
					"redactedContent": "redacted_info"
				}
			]`,
			expectedThinking:   []string{"First thought.", "Signed thought."},
			expectedRedacted:   []string{"redacted_info"},
			expectedSignatures: []bool{false, true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse JSON string into []ConverseReasoningContentBlock
			var blocks []ConverseReasoningContentBlock
			err := json.Unmarshal([]byte(tt.jsonInput), &blocks)
			require.NoError(t, err, "Failed to unmarshal JSON")

			// Convert to pointer slice
			var blockPointers []*ConverseReasoningContentBlock
			for i := range blocks {
				blockPointers = append(blockPointers, &blocks[i])
			}

			result := TransformThinkingBlocks(blockPointers)

			// Count thinking and redacted blocks
			thinkingCount := 0
			redactedCount := 0
			for _, block := range result {
				blockType, _ := block["type"].(string)
				if blockType == "thinking" {
					thinkingCount++
				} else if blockType == "redacted_thinking" {
					redactedCount++
				}
			}

			require.Equal(t, len(tt.expectedThinking), thinkingCount, "Number of thinking blocks should match")
			require.Equal(t, len(tt.expectedRedacted), redactedCount, "Number of redacted blocks should match")

			// Verify thinking blocks
			thinkingIdx := 0
			redactedIdx := 0
			for i, block := range result {
				blockType, _ := block["type"].(string)

				if blockType == "thinking" {
					require.Equal(t, "thinking", blockType, "Thinking block type should be 'thinking'")
					if tt.expectedThinking[thinkingIdx] == "" {
						_, hasThinking := block["thinking"]
						require.False(t, hasThinking, "Empty text should not have thinking field")
					} else {
						thinking, hasThinking := block["thinking"].(string)
						require.True(t, hasThinking, "Non-empty text should have thinking field")
						require.Equal(t, tt.expectedThinking[thinkingIdx], thinking, "Thinking text should match")
					}

					// Check signature if expectedSignatures is provided
					if len(tt.expectedSignatures) > thinkingIdx {
						_, hasSignature := block["signature"]
						if tt.expectedSignatures[thinkingIdx] {
							require.True(t, hasSignature, "Signature should be present")
						} else {
							require.False(t, hasSignature, "Signature should not be present")
						}
					}

					thinkingIdx++
				}

				if blockType == "redacted_thinking" {
					require.Equal(t, "redacted_thinking", blockType, "Redacted block type should be 'redacted_thinking'")
					data, hasData := block["data"].(string)
					require.True(t, hasData, "Redacted block should have data field")
					require.Equal(t, tt.expectedRedacted[redactedIdx], data, "Redacted data should match at index %d", i)
					redactedIdx++
				}
			}
		})
	}
}

func TestSetBedrockUsageFieldsToResponse(t *testing.T) {
	tests := []struct {
		name         string
		inputJSON    string
		bedrockUsage *OpenAIFormatUsage
		expectedJSON string
		shouldError  bool
	}{
		{
			name:      "set usage fields successfully",
			inputJSON: `{"id":"test-id","choices":[]}`,
			bedrockUsage: &OpenAIFormatUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			expectedJSON: `{
				"id": "test-id",
				"choices": [],
				"usage": {
					"prompt_tokens": 100,
					"completion_tokens": 50,
					"total_tokens": 150
				}
			}`,
			shouldError: false,
		},
		{
			name:      "set usage with prompt tokens details",
			inputJSON: `{"id":"test-id","choices":[]}`,
			bedrockUsage: &OpenAIFormatUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
				PromptTokensDetails: &PromptTokensDetails{
					CachedTokens: 20,
				},
			},
			expectedJSON: `{
				"id": "test-id",
				"choices": [],
				"usage": {
					"prompt_tokens": 100,
					"completion_tokens": 50,
					"total_tokens": 150,
					"prompt_tokens_details": {
						"cached_tokens": 20
					}
				}
			}`,
			shouldError: false,
		},
		{
			name:      "set usage with cache fields",
			inputJSON: `{"id":"test-id","choices":[]}`,
			bedrockUsage: &OpenAIFormatUsage{
				PromptTokens:             100,
				CompletionTokens:         50,
				TotalTokens:              150,
				CacheCreationInputTokens: 10,
				CacheReadInputTokens:     5,
			},
			expectedJSON: `{
				"id": "test-id",
				"choices": [],
				"usage": {
					"prompt_tokens": 100,
					"completion_tokens": 50,
					"total_tokens": 150,
					"cache_creation_input_tokens": 10,
					"cache_read_input_tokens": 5
				}
			}`,
			shouldError: false,
		},
		{
			name:         "nil usage should not modify body",
			inputJSON:    `{"id":"test-id","choices":[]}`,
			bedrockUsage: nil,
			expectedJSON: `{
				"id": "test-id",
				"choices": []
			}`,
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := SetBedrockUsageFieldsToResponse([]byte(tt.inputJSON), tt.bedrockUsage)

			if tt.shouldError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.JSONEq(t, tt.expectedJSON, string(result))
		})
	}
}
