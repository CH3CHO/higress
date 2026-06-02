package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tidwall/gjson"
)

func TestClaudeTextGenContentPreservesEmptyToolUseInput(t *testing.T) {
	body, err := json.Marshal(claudeTextGenContent{
		Type:  "tool_use",
		Id:    "call_123",
		Name:  "fetch_doc",
		Input: map[string]interface{}{},
	})
	assert.NoError(t, err)
	assert.True(t, gjson.GetBytes(body, "input").Exists())
	assert.Equal(t, "{}", gjson.GetBytes(body, "input").Raw)
}

func TestClaudeTextGenContentOmitsInputForNonToolUseBlock(t *testing.T) {
	body, err := json.Marshal(claudeTextGenContent{
		Type: "text",
		Text: "hello",
	})
	assert.NoError(t, err)
	assert.False(t, gjson.GetBytes(body, "input").Exists())
}

func TestClaudeTextGenContentOmitsMissingToolUseInput(t *testing.T) {
	body, err := json.Marshal(claudeTextGenContent{
		Type: "tool_use",
		Id:   "call_123",
		Name: "fetch_doc",
	})
	assert.NoError(t, err)
	assert.False(t, gjson.GetBytes(body, "input").Exists())
}
