package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAzureApiVersionParsing(t *testing.T) {
	type testCase struct {
		apiVersion      string
		apiVersionYear  int
		apiVersionMonth int
	}
	tests := []testCase{
		{
			apiVersion:      "2023-05-15",
			apiVersionYear:  2023,
			apiVersionMonth: 5,
		},
		{
			apiVersion:      "2024-06-01",
			apiVersionYear:  2024,
			apiVersionMonth: 6,
		},
		{
			apiVersion:      "2024-10-21",
			apiVersionYear:  2024,
			apiVersionMonth: 10,
		},
		{
			apiVersion:      "2024-12-01-preview",
			apiVersionYear:  2024,
			apiVersionMonth: 12,
		},
		{
			apiVersion:      "2025-01-01-preview",
			apiVersionYear:  2025,
			apiVersionMonth: 1,
		},
		{
			apiVersion:      "2025-03-01-preview",
			apiVersionYear:  2025,
			apiVersionMonth: 3,
		},
		{
			apiVersion:      "2025-04-01-preview",
			apiVersionYear:  2025,
			apiVersionMonth: 4,
		},
	}
	for _, test := range tests {
		t.Run("parsing "+test.apiVersion, func(t *testing.T) {
			year, month, err := parseAzureApiVersion(test.apiVersion)
			assert.NoError(t, err)
			assert.Equal(t, test.apiVersionYear, year)
			assert.Equal(t, test.apiVersionMonth, month)
		})
	}
}

func TestIsEmptyChatCompletionChunk(t *testing.T) {
	cases := []struct {
		name  string
		data  string
		empty bool
	}{
		{
			name:  "no delta or provider specific fields",
			data:  `{}`,
			empty: true,
		},
		{
			name:  "empty delta object",
			data:  `{"choices":[{"delta":{}}]}`,
			empty: true,
		},
		{
			name:  "empty content string",
			data:  `{"choices":[{"delta":{"content":""}}]}`,
			empty: true,
		},
		{
			name:  "non empty content",
			data:  `{"choices":[{"delta":{"content":"hello"}}]}`,
			empty: false,
		},
		{
			name:  "tool calls present but empty",
			data:  `{"choices":[{"delta":{"tool_calls":[]}}]}`,
			empty: true,
		},
		{
			name:  "tool calls present",
			data:  `{"choices":[{"delta":{"tool_calls":[{"type":"function"}]}}]}`,
			empty: false,
		},
		{
			name:  "function call present but null",
			data:  `{"choices":[{"delta":{"function_call":null}}]}`,
			empty: true,
		},
		{
			name:  "function call present",
			data:  `{"choices":[{"delta":{"function_call":{"name":"fn"}}}]}`,
			empty: false,
		},
		{
			name:  "reasoning content present but empty",
			data:  `{"choices":[{"delta":{"reasoning_content":""}}]}`,
			empty: true,
		},
		{
			name:  "reasoning content present",
			data:  `{"choices":[{"delta":{"reasoning_content":"why"}}]}`,
			empty: false,
		},
		{
			name:  "delta provider specific fields present but null",
			data:  `{"choices":[{"delta":{"provider_specific_fields":null}}]}`,
			empty: true,
		},
		{
			name:  "delta provider specific fields present",
			data:  `{"choices":[{"delta":{"provider_specific_fields":{"foo":"bar"}}}]}`,
			empty: false,
		},
		{
			name:  "delta annotations present but null",
			data:  `{"choices":[{"delta":{"annotations":null}}]}`,
			empty: true,
		},
		{
			name:  "delta annotations present",
			data:  `{"choices":[{"delta":{"annotations":{"foo":"bar"}}}]}`,
			empty: false,
		},
		{
			name:  "root provider specific fields present",
			data:  `{"provider_specific_fields":{"foo":"bar"}}`,
			empty: false,
		},
		{
			name:  "root provider specific fields present but null",
			data:  `{"provider_specific_fields":null}`,
			empty: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			event := StreamEvent{Data: tt.data}
			assert.Equal(t, tt.empty, isEmptyChatCompletionChunk(event))
		})
	}
}
