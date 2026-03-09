package vertex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
