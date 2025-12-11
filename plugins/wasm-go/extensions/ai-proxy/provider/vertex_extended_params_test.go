package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractVertexExtendedParams(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		validate    func(t *testing.T, params *vertexExtendedParams)
	}{
		{
			name:        "empty json",
			input:       `{}`,
			expectError: false,
			validate: func(t *testing.T, params *vertexExtendedParams) {
				assert.Nil(t, params.ThinkingConfig)
				assert.Nil(t, params.tools)
			},
		},
		{
			name: "with thinking config",
			input: `{
    "thinkingConfig": {
     "thinkingBudget": 1000,
     "includeThoughts": true
    }
   }`,
			expectError: false,
			validate: func(t *testing.T, params *vertexExtendedParams) {
				assert.NotNil(t, params.ThinkingConfig)
				assert.Nil(t, params.tools)
			},
		},
		{
			name: "with tools - googleSearch",
			input: `{
    "tools": [
     {
      "googleSearch": {"key": "value1"}
     }
    ]
   }`,
			expectError: false,
			validate: func(t *testing.T, params *vertexExtendedParams) {
				assert.Nil(t, params.ThinkingConfig)
				assert.NotNil(t, params.tools)
				assert.Len(t, params.tools, 1)
				assert.NotNil(t, params.tools[0].GoogleSearch)
			},
		},
		{
			name: "with function declaration",
			input: `{
    "tools": [
     {
      "function": {
       "name": "testFunc",
       "description": "A test function"
      }
     }
    ]
   }`,
			expectError: false,
			validate: func(t *testing.T, params *vertexExtendedParams) {
				assert.Nil(t, params.ThinkingConfig)
				assert.NotNil(t, params.tools)
				assert.Len(t, params.tools, 1)
				assert.NotNil(t, params.tools[0].Function)
				assert.Equal(t, "testFunc", params.tools[0].Function.Name)
			},
		},
		{
			name: "with both thinking config and tools",
			input: `{
    "thinkingConfig": {
     "thinkingBudget": 500,
     "includeThoughts": false
    },
    "tools": [
     {
      "googleSearchRetrieval": {"key": "value2"}
     },
     {
      "code_execution": {"key": "value3"}
     }
    ]
   }`,
			expectError: false,
			validate: func(t *testing.T, params *vertexExtendedParams) {
				assert.NotNil(t, params.ThinkingConfig)
				assert.NotNil(t, params.tools)
				assert.Len(t, params.tools, 2)
				assert.NotNil(t, params.tools[0].GoogleSearchRetrieval)
				assert.NotNil(t, params.tools[1].CodeExecution)
			},
		},
		{
			name: "invalid thinking config json",
			input: `{
    "thinkingConfig": "invalid"
   }`,
			expectError: true,
			validate:    nil,
		},
		{
			name: "invalid tools json",
			input: `{
    "tools": "invalid"
   }`,
			expectError: true,
			validate:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params, err := extractVertexExtendedParams([]byte(tt.input))
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, params)
				if tt.validate != nil {
					tt.validate(t, params)
				}
			}
		})
	}
}
