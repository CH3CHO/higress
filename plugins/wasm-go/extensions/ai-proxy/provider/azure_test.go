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
