package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStripDynamicCCHField(t *testing.T) {
	// 这里集中覆盖 stripDynamicCCHField 的核心边界，确保这个纯函数的行为
	// 在已知输入范围内保持稳定，不会因为后续简化而误改。
	tests := []struct {
		name        string
		text        string
		want        string
		wantChanged bool
	}{
		// 常规场景：只移除动态 cch，其余 billing header 元信息保留。
		{
			name:        "removes only cch segment",
			text:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode; cch=123tsdaf;",
			want:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;",
			wantChanged: true,
		},
		// 不含 cch 字段时应保持原样，且 changed=false。
		{
			name:        "keeps text without cch field unchanged",
			text:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;",
			want:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;",
			wantChanged: false,
		},
		// 如果整行只有 cch，清洗后没有有效 segment，应直接删掉这一行。
		{
			name:        "drops line when billing header only contains cch",
			text:        "x-anthropic-billing-header: cch=123tsdaf;",
			want:        "",
			wantChanged: true,
		},
		// 裸 cch（没有等号）不再视为已知合法输入，当前实现保持原样。
		{
			name:        "keeps bare cch segment without equals",
			text:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cch;",
			want:        "x-anthropic-billing-header: cc_version=2.1.84.c8e; cch;",
			wantChanged: false,
		},
		// header key 允许大小写混合命中，输出时保留原始大小写。
		{
			name:        "matches mixed case header key",
			text:        "X-Anthropic-Billing-Header: cc_version=2.1.84.c8e; cch=123tsdaf;",
			want:        "X-Anthropic-Billing-Header: cc_version=2.1.84.c8e;",
			wantChanged: true,
		},
		// 多行文本里只清洗命中的 header 行，其余普通 prompt 行保持不变。
		{
			name:        "sanitizes only matching lines in multiline text",
			text:        "You are helpful.\nx-anthropic-billing-header: cc_version=2.1.84.c8e; cch=123tsdaf;\nAnswer briefly.",
			want:        "You are helpful.\nx-anthropic-billing-header: cc_version=2.1.84.c8e;\nAnswer briefly.",
			wantChanged: true,
		},
		// 清洗时要尽量保持原始文本格式，尤其是行首空白。
		{
			name:        "preserves leading whitespace",
			text:        "\t  x-anthropic-billing-header: cc_version=2.1.84.c8e; cch=123tsdaf;",
			want:        "\t  x-anthropic-billing-header: cc_version=2.1.84.c8e;",
			wantChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized, changed := stripDynamicCCHField(tt.text)

			// 文本结果和 changed 标记都要一起校验，避免只改了字符串而漏改状态位。
			assert.Equal(t, tt.wantChanged, changed)
			assert.Equal(t, tt.want, sanitized)
		})
	}
}

func TestDecodeChatCompletionRequestStripsDynamicCCHFromSystemMessages(t *testing.T) {
	// 验证 OpenAI typed chat.completions 解码路径会清洗 system text block。
	body := []byte(`{
		"model":"claude-sonnet-4-5",
		"messages":[
			{
				"role":"system",
				"content":[
					{
						"type":"text",
						"text":"x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode; cch=123tsdaf;"
					},
					{
						"type":"text",
						"text":"You are helpful."
					}
				]
			},
			{
				"role":"user",
				"content":"hi"
			}
		]
	}`)

	var request chatCompletionRequest
	err := decodeChatCompletionRequest(body, &request)
	require.NoError(t, err)

	systemContent, ok := request.Messages[0].Content.([]any)
	require.True(t, ok)
	require.Len(t, systemContent, 2)
	firstBlock, ok := systemContent[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;", firstBlock["text"])
}

func TestSanitizeAnthropicMessagesRequestCCHStripsDynamicCCHFromSystemAndMessages(t *testing.T) {
	// 验证 Anthropic typed request 会同时清洗顶层 system 和 messages.content 中的 text block。
	var request anthropicMessagesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-sonnet-4-5",
		"system":[
			{
				"type":"text",
				"text":"x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode; cch=123tsdaf;"
			},
			{
				"type":"text",
				"text":"You are helpful."
			}
		],
		"messages":[
			{
				"role":"user",
				"content":[
					{
						"type":"text",
						"text":"x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode; cch=ttffc;"
					},
					{
						"type":"text",
						"text":"hi"
					}
				]
			}
		]
	}`), &request)
	require.NoError(t, err)

	changed := sanitizeAnthropicMessagesRequestCCH(&request)
	assert.True(t, changed)
	require.True(t, request.System.IsArray)
	require.Len(t, request.System.ArrayValue, 2)
	assert.Equal(t, "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;", request.System.ArrayValue[0].Text)
	assert.Equal(t, "You are helpful.", request.System.ArrayValue[1].Text)
	messageContents := request.Messages[0].Content.GetArrayValue()
	require.Len(t, messageContents, 2)
	assert.Equal(t, "x-anthropic-billing-header: cc_version=2.1.84.c8e; cc_entrypoint=claude-vscode;", messageContents[0].Text)
	assert.Equal(t, "hi", messageContents[1].Text)
}
