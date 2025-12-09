package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestTranslatePromptToMessages(t *testing.T) {
	tests := []struct {
		name             string
		promptJSON       string
		expectedMessages string
		expectError      bool
		errorMessage     string
		skipIfNotExists  bool
	}{
		{
			name:       "simple string prompt",
			promptJSON: `"写一个快速排序算法"`,
			expectedMessages: `[
				{
					"role": "user",
					"content": "写一个快速排序算法"
				}
			]`,
			expectError: false,
		},
		{
			name:       "array of string prompts",
			promptJSON: `["Hello", "World", "Test"]`,
			expectedMessages: `[
				{
					"role": "user",
					"content": "Hello"
				},
				{
					"role": "user",
					"content": "World"
				},
				{
					"role": "user",
					"content": "Test"
				}
			]`,
			expectError: false,
		},
		{
			name:       "array with single array element",
			promptJSON: `[["token1", "token2", "token3"]]`,
			expectedMessages: `[
				{
					"role": "user",
					"content": ["token1", "token2", "token3"]
				}
			]`,
			expectError: false,
		},
		{
			name:         "empty prompt array",
			promptJSON:   `[]`,
			expectError:  true,
			errorMessage: "empty prompt array",
		},
		{
			name:         "unsupported prompt type - number",
			promptJSON:   `123`,
			expectError:  true,
			errorMessage: "unsupported prompt type",
		},
		{
			name:         "unsupported prompt item type - object in array",
			promptJSON:   `[{"key": "value"}]`,
			expectError:  true,
			errorMessage: "unsupported prompt item type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the prompt using gjson
			prompt := gjson.Parse(tt.promptJSON)

			// Call the function
			result, err := translatePromptToMessages(prompt)

			// Check error expectation
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
				return
			}

			// If no error expected
			assert.NoError(t, err)
			assert.NotNil(t, result)

			// Compare the result with expected messages
			assert.JSONEq(t, tt.expectedMessages, string(result), "result should match expected messages")
		})
	}
}
