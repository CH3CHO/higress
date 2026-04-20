package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEscapeForJsonString(t *testing.T) {
	var tests = []struct {
		input, output string
	}{
		{"hello", "hello"},
		{"hello\"world", "hello\\\"world"},
		{"h\be\vl\tlo\rworld\n", "h\\be\\vl\\tlo\\rworld\\n"},
	}

	for _, tt := range tests {
		// t.Run enables running "subtests", one for each
		// table entry. These are shown separately
		// when executing `go test -v`.
		testName := tt.input
		t.Run(testName, func(t *testing.T) {
			output := EscapeStringForJson(tt.input)
			assert.Equal(t, tt.output, output)
		})
	}
}

func TestCompactJSONBytes(t *testing.T) {
	t.Run("compact valid json", func(t *testing.T) {
		input := []byte("{\n  \"a\": 1,\n  \"b\": {\"c\": 2}\n}")
		assert.Equal(t, `{"a":1,"b":{"c":2}}`, string(CompactJSONBytes(input)))
	})

	t.Run("fallback for invalid json", func(t *testing.T) {
		input := []byte("{invalid")
		assert.Equal(t, input, CompactJSONBytes(input))
	})
}
