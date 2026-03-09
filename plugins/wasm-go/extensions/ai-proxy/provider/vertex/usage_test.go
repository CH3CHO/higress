package vertex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildModalityTokensDetails(t *testing.T) {
	tests := []struct {
		name     string
		input    []*ModalityTokenCount
		expected ModalityTokensDetails
	}{
		{
			name:     "known modality text",
			input:    []*ModalityTokenCount{{Modality: ModalityText, TokenCount: 10}},
			expected: ModalityTokensDetails{"text_tokens": 10},
		},
		{
			name:     "known modality image",
			input:    []*ModalityTokenCount{{Modality: ModalityImage, TokenCount: 20}},
			expected: ModalityTokensDetails{"image_tokens": 20},
		},
		{
			name:     "future unknown modality 1",
			input:    []*ModalityTokenCount{{Modality: "HOLOGRAM", TokenCount: 99}},
			expected: ModalityTokensDetails{"hologram_tokens": 99},
		},
		{
			name:     "future unknown modality 2",
			input:    []*ModalityTokenCount{{Modality: "COMPUTER_USE", TokenCount: 99}},
			expected: ModalityTokensDetails{"computer_use_tokens": 99},
		},
		{
			name: "mixed known and future modalities",
			input: []*ModalityTokenCount{
				{Modality: ModalityText, TokenCount: 5},
				{Modality: "COMPUTER_USE", TokenCount: 15},
			},
			expected: ModalityTokensDetails{
				"text_tokens":         5,
				"computer_use_tokens": 15,
			},
		},
		{
			name:     "nil entry skipped",
			input:    []*ModalityTokenCount{nil, {Modality: ModalityAudio, TokenCount: 30}},
			expected: ModalityTokensDetails{"audio_tokens": 30},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, buildModalityTokensDetails(tt.input))
		})
	}
}

func TestConvertVertexUsage(t *testing.T) {
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
		"completion_tokens": 1120,
		"total_tokens": 1356,
		"reasoning_tokens": 232,
		"prompt_tokens_details": {
			"text_tokens": 4
		},
		"completion_tokens_details": {
			"image_tokens": 1120
		},
		"traffic_type": "ON_DEMAND"
	}`

	// Parse input JSON to vertex.UsageMetadata
	var usageMetadata UsageMetadata
	err := json.Unmarshal([]byte(inputJSON), &usageMetadata)
	require.NoError(t, err, "Failed to parse input JSON")

	// Call the function
	result := ConvertVertexUsage(&usageMetadata)
	require.NotNil(t, result, "Result should not be nil")

	// Marshal result to JSON string
	resultJSON, err := json.Marshal(result)
	require.NoError(t, err, "Failed to marshal result")

	// Compare JSON strings
	assert.JSONEq(t, expectedJSON, string(resultJSON))
}
