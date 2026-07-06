package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestConvertChatCompletionRequestToResponses(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4.1-mini",
		"max_tokens":64,
		"response_format":{"type":"json_object"},
		"messages":[{"role":"user","content":"hello"}]
	}`)

	converted, err := ConvertChatCompletionRequestToResponses(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4.1-mini", gjson.GetBytes(converted, "model").String())
	require.Equal(t, int64(64), gjson.GetBytes(converted, "max_output_tokens").Int())
	require.Equal(t, `json_object`, gjson.GetBytes(converted, "text.format.type").String())
	require.Equal(t, "user", gjson.GetBytes(converted, "input.0.role").String())
	require.Equal(t, "hello", gjson.GetBytes(converted, "input.0.content").String())
	require.False(t, gjson.GetBytes(converted, "messages").Exists())
	require.False(t, gjson.GetBytes(converted, "max_tokens").Exists())
	require.False(t, gjson.GetBytes(converted, "response_format").Exists())
}

func TestConvertResponsesRequestToChatCompletion(t *testing.T) {
	body := []byte(`{
		"model":"gpt-4.1-mini",
		"max_output_tokens":64,
		"instructions":"be precise",
		"text":{"format":{"type":"json_object"}},
		"input":"hello"
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "gpt-4.1-mini", gjson.GetBytes(converted, "model").String())
	require.Equal(t, int64(64), gjson.GetBytes(converted, "max_tokens").Int())
	require.Equal(t, "json_object", gjson.GetBytes(converted, "response_format.type").String())
	require.Equal(t, "system", gjson.GetBytes(converted, "messages.0.role").String())
	require.Equal(t, "be precise", gjson.GetBytes(converted, "messages.0.content").String())
	require.Equal(t, "user", gjson.GetBytes(converted, "messages.1.role").String())
	require.Equal(t, "hello", gjson.GetBytes(converted, "messages.1.content").String())
	require.False(t, gjson.GetBytes(converted, "input").Exists())
	require.False(t, gjson.GetBytes(converted, "max_output_tokens").Exists())
}

func TestConvertResponsesRequestToChatCompletionRequestsStreamingUsage(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"stream":true,
		"include":[],
		"input":"hello"
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.True(t, gjson.GetBytes(converted, "stream").Bool())
	require.True(t, gjson.GetBytes(converted, "stream_options.include_usage").Bool())
	require.False(t, gjson.GetBytes(converted, "include").Exists())
}

func TestConvertResponsesRequestToChatCompletionWithCodexStyleInput(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"instructions":"You are a coding agent",
		"client_metadata":{"session_id":"s1"},
		"prompt_cache_key":"thread-1",
		"parallel_tool_calls":false,
		"reasoning":{"effort":"high"},
		"input":[
			{
				"type":"message",
				"role":"developer",
				"content":[{"type":"input_text","text":"developer prompt"}]
			},
			{
				"type":"message",
				"role":"user",
				"content":[{"type":"input_text","text":"hello"}]
			},
			{
				"type":"reasoning",
				"summary":[{"type":"summary_text","text":"internal"}]
			},
			{
				"type":"message",
				"role":"assistant",
				"content":[{"type":"output_text","text":"previous answer"}]
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "You are a coding agent", gjson.GetBytes(converted, "messages.0.content").String())
	require.Equal(t, "system", gjson.GetBytes(converted, "messages.1.role").String())
	require.Equal(t, "developer prompt", gjson.GetBytes(converted, "messages.1.content").String())
	require.Equal(t, "user", gjson.GetBytes(converted, "messages.2.role").String())
	require.Equal(t, "hello", gjson.GetBytes(converted, "messages.2.content").String())
	require.Equal(t, "assistant", gjson.GetBytes(converted, "messages.3.role").String())
	require.Equal(t, "previous answer", gjson.GetBytes(converted, "messages.3.content").String())
	require.Equal(t, "high", gjson.GetBytes(converted, "reasoning_effort").String())
	require.False(t, gjson.GetBytes(converted, "client_metadata").Exists())
	require.False(t, gjson.GetBytes(converted, "prompt_cache_key").Exists())
	require.False(t, gjson.GetBytes(converted, "reasoning").Exists())
	require.False(t, gjson.GetBytes(converted, "messages.4").Exists())
	require.True(t, gjson.GetBytes(converted, "parallel_tool_calls").Exists())
	require.False(t, gjson.GetBytes(converted, "parallel_tool_calls").Bool())
}

func TestConvertResponsesRequestToChatCompletionWithPrimitiveContentParts(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"input":[
			{
				"type":"message",
				"role":"user",
				"content":["hello",123,true]
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "hello\n123\ntrue", gjson.GetBytes(converted, "messages.0.content").String())
}

func TestConvertResponsesRequestToChatCompletionWithMixedPrimitiveAndImageContentParts(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"input":[
			{
				"type":"message",
				"role":"user",
				"content":[
					{"type":"input_image","image_url":"https://example.com/a.png"},
					"caption",
					123
				]
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "image_url", gjson.GetBytes(converted, "messages.0.content.0.type").String())
	require.Equal(t, "https://example.com/a.png", gjson.GetBytes(converted, "messages.0.content.0.image_url.url").String())
	require.Equal(t, "text", gjson.GetBytes(converted, "messages.0.content.1.type").String())
	require.Equal(t, "caption", gjson.GetBytes(converted, "messages.0.content.1.text").String())
	require.Equal(t, "text", gjson.GetBytes(converted, "messages.0.content.2.type").String())
	require.Equal(t, "123", gjson.GetBytes(converted, "messages.0.content.2.text").String())
}

func TestConvertResponsesRequestToChatCompletionWithToolItems(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"input":[
			{
				"type":"function_call",
				"id":"fc_1",
				"call_id":"call_1",
				"name":"exec_command",
				"arguments":"{\"cmd\":\"pwd\"}"
			},
			{
				"type":"function_call_output",
				"call_id":"call_1",
				"output":"{\"stdout\":\"/tmp\"}"
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "assistant", gjson.GetBytes(converted, "messages.0.role").String())
	require.Equal(t, "call_1", gjson.GetBytes(converted, "messages.0.tool_calls.0.id").String())
	require.Equal(t, "exec_command", gjson.GetBytes(converted, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, "{\"cmd\":\"pwd\"}", gjson.GetBytes(converted, "messages.0.tool_calls.0.function.arguments").String())
	require.Equal(t, "tool", gjson.GetBytes(converted, "messages.1.role").String())
	require.Equal(t, "call_1", gjson.GetBytes(converted, "messages.1.tool_call_id").String())
	require.Equal(t, "{\"stdout\":\"/tmp\"}", gjson.GetBytes(converted, "messages.1.content").String())
}

func TestConvertResponsesRequestToChatCompletionWithToolMessageCallID(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"input":[
			{
				"type":"function_call",
				"call_id":"exec_command:0",
				"name":"exec_command",
				"arguments":"{\"cmd\":\"pwd\"}"
			},
			{
				"type":"message",
				"role":"tool",
				"call_id":"exec_command:0",
				"content":[{"type":"output_text","text":"{\"stdout\":\"/tmp\"}"}]
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "exec_command:0", gjson.GetBytes(converted, "messages.0.tool_calls.0.id").String())
	require.Equal(t, "exec_command:0", gjson.GetBytes(converted, "messages.1.tool_call_id").String())
	require.Equal(t, "{\"stdout\":\"/tmp\"}", gjson.GetBytes(converted, "messages.1.content").String())
}

func TestConvertResponsesRequestToChatCompletionWithToolCallOutputAlias(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"input":[
			{
				"type":"function_call",
				"call_id":"exec_command:0",
				"name":"exec_command",
				"arguments":"{\"cmd\":\"pwd\"}"
			},
			{
				"type":"tool_call_output",
				"call_id":"exec_command:0",
				"output":"{\"stdout\":\"/tmp\"}"
			}
		]
	}`)

	converted, err := ConvertResponsesRequestToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, "exec_command:0", gjson.GetBytes(converted, "messages.1.tool_call_id").String())
	require.Equal(t, "{\"stdout\":\"/tmp\"}", gjson.GetBytes(converted, "messages.1.content").String())
}

func TestConvertResponsesRequestToChatCompletionFlattensNamespaceTools(t *testing.T) {
	body := []byte(`{
		"model":"kimi-k2.5",
		"tool_choice":{"type":"function","function":{"name":"multi_agent_v1.close_agent"}},
		"tools":[
			{
				"type":"namespace",
				"name":"multi_agent_v1",
				"tools":[
					{
						"type":"function",
						"name":"close_agent",
						"description":"close agent",
						"parameters":{"type":"object"}
					}
				]
			}
		],
		"input":[
			{
				"type":"function_call",
				"call_id":"call_1",
				"name":"multi_agent_v1.close_agent",
				"arguments":"{\"target\":\"a1\"}"
			}
		]
	}`)

	converted, toolNameMap, err := ConvertResponsesRequestToChatCompletionWithToolNameMapping(body)
	require.NoError(t, err)
	require.Equal(t, "multi_agent_v1.close_agent", toolNameMap["multi_agent_v1_close_agent"])
	require.Equal(t, "function", gjson.GetBytes(converted, "tools.0.type").String())
	require.Equal(t, "multi_agent_v1_close_agent", gjson.GetBytes(converted, "tools.0.function.name").String())
	require.Equal(t, "multi_agent_v1_close_agent", gjson.GetBytes(converted, "tool_choice.function.name").String())
	require.Equal(t, "multi_agent_v1_close_agent", gjson.GetBytes(converted, "messages.0.tool_calls.0.function.name").String())
}

func TestConvertResponsesRequestToChatCompletionSupportsCustomAndToolSearchItems(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.4",
		"tools":[
			{"type":"custom","name":"apply_patch"},
			{"type":"tool_search"}
		],
		"input":[
			{"type":"custom_tool_call","call_id":"call_patch","name":"apply_patch","input":"*** Begin Patch\n*** End Patch"},
			{"type":"tool_search_call","call_id":"call_search","arguments":{"query":"gmail","limit":3}},
			{"type":"custom_tool_call_output","call_id":"call_patch","output":"ok"},
			{"type":"tool_search_output","call_id":"call_search","tools":[{"type":"function","name":"gmail_search","description":"search","parameters":{"type":"object"}}]}
		]
	}`)

	converted, toolContext, toolNameMap, err := ConvertResponsesRequestToChatCompletionWithToolContext(body)
	require.NoError(t, err)
	require.NotNil(t, toolContext)
	require.Equal(t, "apply_patch", toolNameMap["apply_patch"])
	require.Equal(t, "apply_patch", gjson.GetBytes(converted, "tools.0.function.name").String())
	require.Equal(t, toolSearchProxyName, gjson.GetBytes(converted, "tools.1.function.name").String())
	require.Equal(t, "apply_patch", gjson.GetBytes(converted, "messages.0.tool_calls.0.function.name").String())
	require.Equal(t, "{\"input\":\"*** Begin Patch\\n*** End Patch\"}", gjson.GetBytes(converted, "messages.0.tool_calls.0.function.arguments").String())
	require.Equal(t, toolSearchProxyName, gjson.GetBytes(converted, "messages.1.tool_calls.0.function.name").String())
	require.Equal(t, "{\"limit\":3,\"query\":\"gmail\"}", gjson.GetBytes(converted, "messages.1.tool_calls.0.function.arguments").String())
	require.Equal(t, "{\"call_id\":\"call_patch\",\"output\":\"ok\",\"type\":\"custom_tool_call_output\"}", gjson.GetBytes(converted, "messages.2.content").String())
	require.Equal(t, "{\"call_id\":\"call_search\",\"tools\":[{\"description\":\"search\",\"name\":\"gmail_search\",\"parameters\":{\"type\":\"object\"},\"type\":\"function\"}],\"type\":\"tool_search_output\"}", gjson.GetBytes(converted, "messages.3.content").String())
}

func TestBuildResponsesToolCallItemFallsBackForUnknownToolSpecKind(t *testing.T) {
	toolContext := newResponsesToolContext()
	toolContext.add("exec_command", responsesToolSpec{name: "exec_command"})

	item := buildResponsesToolCallItem(
		"exec_command:0",
		"exec_command:0",
		"exec_command",
		"exec_command",
		`{"cmd":"Get-PSDrive D"}`,
		"need to inspect disk usage",
		"completed",
		toolContext,
	)

	require.Equal(t, "function_call", item["type"])
	require.Equal(t, "exec_command:0", item["id"])
	require.Equal(t, "exec_command", item["name"])
	require.Equal(t, `{"cmd":"Get-PSDrive D"}`, item["arguments"])
	require.Equal(t, "need to inspect disk usage", item["reasoning_content"])
}

func TestConvertResponsesResponseToChatCompletion(t *testing.T) {
	body := []byte(`{
		"id":"resp_123",
		"object":"response",
		"created_at":1710000000,
		"model":"gpt-4.1-mini",
		"output":[
			{
				"type":"message",
				"role":"assistant",
				"content":[
					{"type":"output_text","text":"Hello from responses"}
				]
			}
		],
		"usage":{
			"input_tokens":11,
			"output_tokens":7,
			"total_tokens":18
		}
	}`)

	converted, err := ConvertResponsesResponseToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, objectChatCompletion, gjson.GetBytes(converted, "object").String())
	require.Equal(t, "assistant", gjson.GetBytes(converted, "choices.0.message.role").String())
	require.Equal(t, "Hello from responses", gjson.GetBytes(converted, "choices.0.message.content").String())
	require.Equal(t, int64(11), gjson.GetBytes(converted, "usage.prompt_tokens").Int())
	require.Equal(t, int64(7), gjson.GetBytes(converted, "usage.completion_tokens").Int())
	require.Equal(t, int64(18), gjson.GetBytes(converted, "usage.total_tokens").Int())
}

func TestConvertChatCompletionResponseToResponses(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_123",
		"object":"chat.completion",
		"created":1710000000,
		"model":"qwen-plus",
		"choices":[
			{
				"index":0,
				"message":{"role":"assistant","content":"hello back"},
				"finish_reason":"stop"
			}
		],
		"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":5}}
	}`)

	converted, err := ConvertChatCompletionResponseToResponses(body)
	require.NoError(t, err)
	require.Equal(t, "response", gjson.GetBytes(converted, "object").String())
	require.Equal(t, "hello back", gjson.GetBytes(converted, "output_text").String())
	require.Equal(t, "assistant", gjson.GetBytes(converted, "output.0.role").String())
	require.Equal(t, "output_text", gjson.GetBytes(converted, "output.0.content.0.type").String())
	require.Equal(t, int64(11), gjson.GetBytes(converted, "usage.input_tokens").Int())
	require.Equal(t, int64(7), gjson.GetBytes(converted, "usage.output_tokens").Int())
	require.Equal(t, int64(18), gjson.GetBytes(converted, "usage.total_tokens").Int())
	require.Equal(t, int64(5), gjson.GetBytes(converted, "usage.input_tokens_details.cached_tokens").Int())
}

func TestConvertChatCompletionResponseToResponsesLengthBecomesIncomplete(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_124",
		"object":"chat.completion",
		"created":1710000000,
		"model":"qwen-plus",
		"choices":[
			{
				"index":0,
				"message":{"role":"assistant","content":"partial"},
				"finish_reason":"length"
			}
		]
	}`)

	converted, err := ConvertChatCompletionResponseToResponses(body)
	require.NoError(t, err)
	require.Equal(t, "incomplete", gjson.GetBytes(converted, "status").String())
	require.Equal(t, "max_output_tokens", gjson.GetBytes(converted, "incomplete_details.reason").String())
}

func TestConvertChatCompletionResponseToResponsesRestoresToolNames(t *testing.T) {
	body := []byte(`{
		"id":"chatcmpl_123",
		"object":"chat.completion",
		"created":1710000000,
		"model":"qwen-plus",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"tool_calls":[
						{
							"id":"call_1",
							"type":"function",
							"function":{"name":"multi_agent_v1_close_agent","arguments":"{\"target\":\"a1\"}"}
						}
					]
				},
				"finish_reason":"tool_calls"
			}
		]
	}`)

	converted, err := ConvertChatCompletionResponseToResponsesWithToolNameMapping(body, map[string]string{
		"multi_agent_v1_close_agent": "multi_agent_v1.close_agent",
	})
	require.NoError(t, err)
	require.Equal(t, "multi_agent_v1.close_agent", gjson.GetBytes(converted, "output.0.name").String())
}

func TestConvertChatCompletionResponseToResponsesWithFunctionToolContextAndReasoning(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"tools":[{"type":"function","name":"read_file"}]
	}`)
	_, toolContext, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	body := []byte(`{
		"id":"chatcmpl_function_reasoning",
		"object":"chat.completion",
		"created":1710000000,
		"model":"gpt-5.4",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"reasoning_content":"Need the file.",
					"tool_calls":[
						{
							"id":"call_read",
							"type":"function",
							"function":{"name":"read_file","arguments":"{\"path\":\"README.md\"}"}
						}
					]
				},
				"finish_reason":"tool_calls"
			}
		]
	}`)

	converted, err := ConvertChatCompletionResponseToResponsesWithToolContext(body, toolContext, nil)
	require.NoError(t, err)
	require.Equal(t, "function_call", gjson.GetBytes(converted, "output.0.type").String())
	require.Equal(t, "read_file", gjson.GetBytes(converted, "output.0.name").String())
	require.Equal(t, "Need the file.", gjson.GetBytes(converted, "output.0.reasoning_content").String())
}

func TestConvertChatCompletionResponseToResponsesRestoresCustomToolCall(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"tools":[{"type":"custom","name":"apply_patch"}]
	}`)
	_, toolContext, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	body := []byte(`{
		"id":"chatcmpl_custom",
		"object":"chat.completion",
		"created":1710000000,
		"model":"gpt-5.4",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"tool_calls":[
						{
							"id":"call_patch",
							"type":"function",
							"function":{"name":"apply_patch","arguments":"{\"input\":\"*** Begin Patch\\n*** End Patch\"}"}
						}
					]
				},
				"finish_reason":"tool_calls"
			}
		]
	}`)

	converted, err := ConvertChatCompletionResponseToResponsesWithToolContext(body, toolContext, nil)
	require.NoError(t, err)
	require.Equal(t, "custom_tool_call", gjson.GetBytes(converted, "output.0.type").String())
	require.Equal(t, "ctc_call_patch", gjson.GetBytes(converted, "output.0.id").String())
	require.Equal(t, "*** Begin Patch\n*** End Patch", gjson.GetBytes(converted, "output.0.input").String())
}

func TestConvertChatCompletionResponseToResponsesRestoresToolSearchCall(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"tools":[{"type":"tool_search"}]
	}`)
	_, toolContext, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	body := []byte(`{
		"id":"chatcmpl_tool_search",
		"object":"chat.completion",
		"created":1710000000,
		"model":"gpt-5.4",
		"choices":[
			{
				"index":0,
				"message":{
					"role":"assistant",
					"tool_calls":[
						{
							"id":"call_search",
							"type":"function",
							"function":{"name":"tool_search","arguments":"{\"query\":\"gmail\",\"limit\":10}"}
						}
					]
				},
				"finish_reason":"tool_calls"
			}
		]
	}`)

	converted, err := ConvertChatCompletionResponseToResponsesWithToolContext(body, toolContext, nil)
	require.NoError(t, err)
	require.Equal(t, "tool_search_call", gjson.GetBytes(converted, "output.0.type").String())
	require.Equal(t, "client", gjson.GetBytes(converted, "output.0.execution").String())
	require.Equal(t, "gmail", gjson.GetBytes(converted, "output.0.arguments.query").String())
	require.Equal(t, int64(10), gjson.GetBytes(converted, "output.0.arguments.limit").Int())
}

func TestConvertResponsesResponseToChatCompletionWithToolCalls(t *testing.T) {
	body := []byte(`{
		"id":"resp_456",
		"object":"response",
		"created_at":1710000001,
		"model":"gpt-4.1-mini",
		"output":[
			{
				"type":"function_call",
				"id":"fc_1",
				"call_id":"call_1",
				"name":"weather",
				"arguments":"{\"city\":\"shanghai\"}"
			}
		]
	}`)

	converted, err := ConvertResponsesResponseToChatCompletion(body)
	require.NoError(t, err)
	require.Equal(t, finishReasonToolCall, gjson.GetBytes(converted, "choices.0.finish_reason").String())
	require.Equal(t, "call_1", gjson.GetBytes(converted, "choices.0.message.tool_calls.0.id").String())
	require.Equal(t, "weather", gjson.GetBytes(converted, "choices.0.message.tool_calls.0.function.name").String())
	require.Equal(t, "{\"city\":\"shanghai\"}", gjson.GetBytes(converted, "choices.0.message.tool_calls.0.function.arguments").String())
}

func TestConvertStreamingChatCompletionToResponsesTextLifecycle(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":1,\"total_tokens\":12}}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 9)
	require.Equal(t, "response.created", events[0].Event)
	require.Equal(t, "response.in_progress", events[1].Event)
	require.Equal(t, "response.output_item.added", events[2].Event)
	require.Equal(t, "response.content_part.added", events[3].Event)
	require.Equal(t, "response.output_text.delta", events[4].Event)
	require.Equal(t, "response.output_text.done", events[5].Event)
	require.Equal(t, "response.content_part.done", events[6].Event)
	require.Equal(t, "response.output_item.done", events[7].Event)
	require.Equal(t, "response.completed", events[8].Event)

	require.Equal(t, "chatcmpl_123_msg", gjson.Get(events[4].Data, "item_id").String())
	require.Equal(t, "pong", gjson.Get(events[4].Data, "delta").String())
	require.Equal(t, "pong", gjson.Get(events[8].Data, "response.output_text").String())
	require.Equal(t, "message", gjson.Get(events[8].Data, "response.output.0.type").String())
	require.Equal(t, int64(11), gjson.Get(events[8].Data, "response.usage.input_tokens").Int())
	require.Equal(t, int64(1), gjson.Get(events[8].Data, "response.usage.output_tokens").Int())
}

func TestConvertStreamingChatCompletionToResponsesTextLifecycleAcrossChunks(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}

	chunk1 := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"po\"}}]}\n\n")
	converted1, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk1, false)
	require.NoError(t, err)

	events1 := ExtractStreamingEvents(newMockHttpContext(), converted1)
	require.NoError(t, err)
	require.Len(t, events1, 5)
	require.Equal(t, "response.created", events1[0].Event)
	require.Equal(t, "response.in_progress", events1[1].Event)
	require.Equal(t, "response.output_item.added", events1[2].Event)
	require.Equal(t, "response.content_part.added", events1[3].Event)
	require.Equal(t, "response.output_text.delta", events1[4].Event)
	require.Equal(t, "po", gjson.Get(events1[4].Data, "delta").String())

	chunk2 := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"ng\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":1,\"total_tokens\":12}}\n\n")
	converted2, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk2, false)
	require.NoError(t, err)

	events2 := ExtractStreamingEvents(newMockHttpContext(), converted2)
	require.NoError(t, err)
	require.Len(t, events2, 5)
	require.Equal(t, "response.output_text.delta", events2[0].Event)
	require.Equal(t, "ng", gjson.Get(events2[0].Data, "delta").String())
	require.Equal(t, "response.output_text.done", events2[1].Event)
	require.Equal(t, "response.content_part.done", events2[2].Event)
	require.Equal(t, "response.output_item.done", events2[3].Event)
	require.Equal(t, "response.completed", events2[4].Event)
	require.Equal(t, "pong", gjson.Get(events2[1].Data, "text").String())
	require.Equal(t, "pong", gjson.Get(events2[4].Data, "response.output_text").String())
	require.Equal(t, int64(11), gjson.Get(events2[4].Data, "response.usage.input_tokens").Int())
	require.Equal(t, int64(1), gjson.Get(events2[4].Data, "response.usage.output_tokens").Int())

	converted3, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), []byte("data: [DONE]\n\n"), true)
	require.NoError(t, err)
	require.Empty(t, converted3)
}

func TestConvertStreamingChatCompletionToResponsesUsageInChoice(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\",\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":1,\"total_tokens\":12,\"cached_tokens\":5}}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 9)
	require.Equal(t, "response.completed", events[8].Event)
	require.Equal(t, int64(11), gjson.Get(events[8].Data, "response.usage.input_tokens").Int())
	require.Equal(t, int64(1), gjson.Get(events[8].Data, "response.usage.output_tokens").Int())
	require.Equal(t, int64(12), gjson.Get(events[8].Data, "response.usage.total_tokens").Int())
	require.Equal(t, int64(5), gjson.Get(events[8].Data, "response.usage.input_tokens_details.cached_tokens").Int())
}

func TestConvertStreamingChatCompletionToResponsesWaitsForUsageChunk(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}

	chunk1 := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\n")
	converted1, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk1, false)
	require.NoError(t, err)

	events1 := ExtractStreamingEvents(newMockHttpContext(), converted1)
	require.NoError(t, err)
	require.Len(t, events1, 5)
	require.Equal(t, "response.output_text.delta", events1[4].Event)
	require.NotContains(t, string(converted1), "response.completed")

	chunk2 := []byte("data: {\"id\":\"chatcmpl_123\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":1,\"total_tokens\":12,\"prompt_tokens_details\":{\"cached_tokens\":5}}}\n\n")
	converted2, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk2, false)
	require.NoError(t, err)

	events2 := ExtractStreamingEvents(newMockHttpContext(), converted2)
	require.NoError(t, err)
	require.Len(t, events2, 4)
	require.Equal(t, "response.output_text.done", events2[0].Event)
	require.Equal(t, "response.content_part.done", events2[1].Event)
	require.Equal(t, "response.output_item.done", events2[2].Event)
	require.Equal(t, "response.completed", events2[3].Event)
	require.Equal(t, int64(11), gjson.Get(events2[3].Data, "response.usage.input_tokens").Int())
	require.Equal(t, int64(1), gjson.Get(events2[3].Data, "response.usage.output_tokens").Int())
	require.Equal(t, int64(12), gjson.Get(events2[3].Data, "response.usage.total_tokens").Int())
	require.Equal(t, int64(5), gjson.Get(events2[3].Data, "response.usage.input_tokens_details.cached_tokens").Int())
}

func TestConvertStreamingChatCompletionToResponsesRestoresToolNameLifecycle(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{
		ToolNameMap: map[string]string{
			"multi_agent_v1_close_agent": "multi_agent_v1.close_agent",
		},
	}
	stream := []byte("data: {\"id\":\"chatcmpl_456\",\"created\":1710000001,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"multi_agent_v1_close_agent\",\"arguments\":\"{\\\"target\\\":\\\"a1\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 7)
	require.Equal(t, "response.created", events[0].Event)
	require.Equal(t, "response.in_progress", events[1].Event)
	require.Equal(t, "response.output_item.added", events[2].Event)
	require.Equal(t, "response.function_call_arguments.delta", events[3].Event)
	require.Equal(t, "response.function_call_arguments.done", events[4].Event)
	require.Equal(t, "response.output_item.done", events[5].Event)
	require.Equal(t, "response.completed", events[6].Event)

	require.Equal(t, "multi_agent_v1.close_agent", gjson.Get(events[2].Data, "item.name").String())
	require.Equal(t, "call_1", gjson.Get(events[3].Data, "item_id").String())
	require.Equal(t, "{\"target\":\"a1\"}", gjson.Get(events[4].Data, "arguments").String())
	require.Equal(t, "multi_agent_v1.close_agent", gjson.Get(events[6].Data, "response.output.0.name").String())
	require.Equal(t, "completed", gjson.Get(events[6].Data, "response.status").String())
}

func TestConvertStreamingChatCompletionToResponsesRestoresToolNameLifecycleAcrossChunks(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{
		ToolNameMap: map[string]string{
			"multi_agent_v1_close_agent": "multi_agent_v1.close_agent",
		},
	}

	chunk1 := []byte("data: {\"id\":\"chatcmpl_456\",\"created\":1710000001,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"multi_agent_v1_close_agent\"}}]}}]}\n\n")
	converted1, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk1, false)
	require.NoError(t, err)

	events1 := ExtractStreamingEvents(newMockHttpContext(), converted1)
	require.NoError(t, err)
	require.Len(t, events1, 3)
	require.Equal(t, "response.created", events1[0].Event)
	require.Equal(t, "response.in_progress", events1[1].Event)
	require.Equal(t, "response.output_item.added", events1[2].Event)
	require.Equal(t, "multi_agent_v1.close_agent", gjson.Get(events1[2].Data, "item.name").String())

	chunk2 := []byte("data: {\"id\":\"chatcmpl_456\",\"created\":1710000001,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"target\\\":\\\"a1\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
	converted2, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), chunk2, false)
	require.NoError(t, err)

	events2 := ExtractStreamingEvents(newMockHttpContext(), converted2)
	require.NoError(t, err)
	require.Len(t, events2, 1)
	require.Equal(t, "response.function_call_arguments.delta", events2[0].Event)
	require.Equal(t, "call_1", gjson.Get(events2[0].Data, "item_id").String())
	require.Equal(t, "{\"target\":\"a1\"}", gjson.Get(events2[0].Data, "delta").String())
	require.NotContains(t, string(converted2), "response.completed")

	converted3, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), []byte("data: [DONE]\n\n"), true)
	require.NoError(t, err)
	events3 := ExtractStreamingEvents(newMockHttpContext(), converted3)
	require.NoError(t, err)
	require.Len(t, events3, 3)
	require.Equal(t, "response.function_call_arguments.done", events3[0].Event)
	require.Equal(t, "response.output_item.done", events3[1].Event)
	require.Equal(t, "response.completed", events3[2].Event)
	require.Equal(t, "{\"target\":\"a1\"}", gjson.Get(events3[0].Data, "arguments").String())
	require.Equal(t, "multi_agent_v1.close_agent", gjson.Get(events3[1].Data, "item.name").String())
	require.Equal(t, "multi_agent_v1.close_agent", gjson.Get(events3[2].Data, "response.output.0.name").String())
}

func TestConvertStreamingChatCompletionToResponsesReasoningLifecycle(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_reason\",\"created\":1710000002,\"model\":\"deepseek-reasoner\",\"choices\":[{\"delta\":{\"reasoning_content\":\"Need context. \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_reason\",\"created\":1710000002,\"model\":\"deepseek-reasoner\",\"choices\":[{\"delta\":{\"reasoning\":\"Now answer. \"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_reason\",\"created\":1710000002,\"model\":\"deepseek-reasoner\",\"choices\":[{\"delta\":{\"content\":\"Done\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":6,\"total_tokens\":10}}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Equal(t, "response.output_item.added", events[2].Event)
	require.Equal(t, "response.reasoning_summary_part.added", events[3].Event)
	require.Equal(t, "response.reasoning_summary_text.delta", events[4].Event)
	require.Equal(t, "response.reasoning_summary_text.delta", events[5].Event)
	require.Equal(t, "response.reasoning_summary_text.done", events[6].Event)
	require.Equal(t, "response.output_text.delta", events[11].Event)
	require.Equal(t, "Need context. ", gjson.Get(events[4].Data, "delta").String())
	require.Equal(t, "Now answer. ", gjson.Get(events[5].Data, "delta").String())
	require.Equal(t, "Need context. Now answer. ", gjson.Get(events[6].Data, "text").String())
	require.Equal(t, "reasoning", gjson.Get(events[8].Data, "item.type").String())
	require.Equal(t, "Done", gjson.Get(events[len(events)-1].Data, "response.output_text").String())
}

func TestConvertStreamingChatCompletionToResponsesInlineThinkLifecycle(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_minimax\",\"created\":1710000004,\"model\":\"MiniMax-M2.7\",\"choices\":[{\"delta\":{\"content\":\"<think>\\nNeed\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_minimax\",\"created\":1710000004,\"model\":\"MiniMax-M2.7\",\"choices\":[{\"delta\":{\"content\":\" context.</think>\\n\\npong\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":4,\"completion_tokens\":6,\"total_tokens\":10}}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	text := string(converted)
	require.Contains(t, text, "event: response.reasoning_summary_text.delta")
	require.Contains(t, text, "Need context.")
	require.Contains(t, text, "\"text\":\"pong\"")
	require.NotContains(t, text, "<think>")
	require.NotContains(t, text, "</think>")
}

func TestConvertStreamingChatCompletionToResponsesPreservesLateReasoningOnToolCall(t *testing.T) {
	// 对齐 cc-switch 的 preserves_late_reasoning_content_on_streamed_tool_call_items：
	// reasoning_content 在 tool_call 之后才到，仍要回挂到该 tool call item。
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_tool_late_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_late_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_late_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"reasoning_content\":\"Need file.\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_late_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Equal(t, "response.output_item.done", events[len(events)-2].Event)
	require.Equal(t, "function_call", gjson.Get(events[len(events)-2].Data, "item.type").String())
	require.Equal(t, "Need file.", gjson.Get(events[len(events)-2].Data, "item.reasoning_content").String())
	require.Contains(t, string(converted), "event: response.reasoning_summary_text.delta")
}

func TestConvertStreamingChatCompletionToResponsesCanonicalizesToolArgumentsOnDone(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_args\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"lookup\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_args\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{ \\\"b\\\": 2,\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_args\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\" \\\"a\\\": 1 }\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1,\"b\":2}", gjson.Get(events[len(events)-3].Data, "arguments").String())
	require.Equal(t, "{\"a\":1,\"b\":2}", gjson.Get(events[len(events)-2].Data, "item.arguments").String())
}

func TestConvertStreamingChatCompletionToResponsesRestoresCustomToolLifecycle(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"tools":[{"type":"custom","name":"apply_patch"}]
	}`)
	_, toolContext, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	bridge := &ResponsesChatCompletionBridge{
		ToolContext: toolContext,
	}
	stream := []byte("data: {\"id\":\"chatcmpl_custom\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_patch\",\"type\":\"function\",\"function\":{\"name\":\"apply_patch\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_custom\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"input\\\":\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_custom\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"*** Begin Patch\\\\n*** End Patch\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	text := string(converted)
	require.Contains(t, text, "event: response.custom_tool_call_input.delta")
	require.Contains(t, text, "event: response.custom_tool_call_input.done")
	require.NotContains(t, text, "event: response.function_call_arguments.delta")
	require.NotContains(t, text, "event: response.function_call_arguments.done")
	require.Contains(t, text, "\"type\":\"custom_tool_call\"")
	require.Contains(t, text, "\"id\":\"ctc_call_patch\"")
	require.Contains(t, text, "\"input\":\"*** Begin Patch\\n*** End Patch\"")
}

func TestConvertStreamingChatCompletionToResponsesRestoresNamespaceLifecycle(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{
				"type":"tool_search_output",
				"call_id":"call_tool_search_1",
				"tools":[
					{
						"type":"namespace",
						"name":"mcp__codex_apps__gmail",
						"tools":[
							{
								"type":"function",
								"name":"_search_emails",
								"description":"Search Gmail.",
								"parameters":{"type":"object"}
							}
						]
					}
				]
			}
		]
	}`)
	_, toolContext, toolNameMap, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	bridge := &ResponsesChatCompletionBridge{
		ToolContext: toolContext,
		ToolNameMap: toolNameMap,
	}
	stream := []byte("data: {\"id\":\"chatcmpl_gmail\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_gmail\",\"type\":\"function\",\"function\":{\"name\":\"mcp__codex_apps__gmail__search_emails\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_gmail\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"query\\\":\\\"in:inbox\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)
	require.Contains(t, string(converted), "\"namespace\":\"mcp__codex_apps__gmail\"")
	require.Contains(t, string(converted), "\"name\":\"_search_emails\"")
}

func TestConvertStreamingChatCompletionToResponsesRestoresToolSearchLifecycle(t *testing.T) {
	request := []byte(`{
		"model":"gpt-5.4",
		"tools":[{"type":"tool_search"}]
	}`)
	_, toolContext, _, err := ConvertResponsesRequestToChatCompletionWithToolContext(request)
	require.NoError(t, err)

	bridge := &ResponsesChatCompletionBridge{
		ToolContext: toolContext,
	}
	stream := []byte("data: {\"id\":\"chatcmpl_tool_search\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_tool_search_1\",\"type\":\"function\",\"function\":{\"name\":\"tool_search\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_search\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"query\\\":\\\"Gmail search emails\\\",\\\"limit\\\":10}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)
	require.Contains(t, string(converted), "\"type\":\"tool_search_call\"")
	require.Contains(t, string(converted), "\"execution\":\"client\"")
	require.Contains(t, string(converted), "\"query\":\"Gmail search emails\"")
}

func TestConvertStreamingChatCompletionToResponsesPreservesEarlyReasoningOnToolCall(t *testing.T) {
	// 对齐 cc-switch 的 preserves_reasoning_content_on_streamed_tool_call_items：
	// 先到的 reasoning_content 应回挂到后续 tool call item 的 reasoning_content 字段。
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"reasoning_content\":\"Need file.\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Equal(t, "response.output_item.done", events[len(events)-2].Event)
	require.Equal(t, "function_call", gjson.Get(events[len(events)-2].Data, "item.type").String())
	require.Equal(t, "Need file.", gjson.Get(events[len(events)-2].Data, "item.reasoning_content").String())
	require.Contains(t, string(converted), "event: response.reasoning_summary_text.delta")
}

func TestConvertStreamingChatCompletionToResponsesAttachesReasoningToEachToolCall(t *testing.T) {
	// 对齐 cc-switch：单份 choice 级 reasoning 同时进独立 reasoning item 与每个 tool call
	// item 的 reasoning_content（各 tool 各持一份正文副本，独立 reasoning item 另存一份）。
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_multi_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"reasoning_content\":\"Need both tools.\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_multi_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\"}},{\"index\":1,\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"list_dir\"}}]}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_multi_tool_reasoning\",\"model\":\"deepseek-v4-flash\",\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}},{\"index\":1,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"plugins\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	var completedData string
	for _, event := range events {
		if event.Event == "response.completed" {
			completedData = event.Data
		}
	}
	require.NotEmpty(t, completedData)

	var reasoningItems int
	var toolCallItems int
	for _, item := range gjson.Get(completedData, "response.output").Array() {
		switch item.Get("type").String() {
		case "reasoning":
			reasoningItems++
		case "function_call":
			toolCallItems++
			require.Equal(t, "Need both tools.", item.Get("reasoning_content").String())
		}
	}
	require.Equal(t, 1, reasoningItems)
	require.Equal(t, 2, toolCallItems)
}

func TestConvertStreamingChatCompletionToResponsesTruncatedStreamBecomesIncomplete(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_partial\",\"created\":1710000003,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Equal(t, "response.completed", events[len(events)-1].Event)
	require.Equal(t, "incomplete", gjson.Get(events[len(events)-1].Data, "response.status").String())
	require.Equal(t, "max_output_tokens", gjson.Get(events[len(events)-1].Data, "response.incomplete_details.reason").String())
}

func TestConvertStreamingChatCompletionToResponsesTruncatedWithoutOutputBecomesFailed(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"id\":\"chatcmpl_truncated\",\"model\":\"gpt-5.4\",\"choices\":[{\"delta\":{}}]}\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, "response.failed", events[len(events)-1].Event)
	require.Equal(t, "stream_truncated", gjson.Get(events[len(events)-1].Data, "response.error.type").String())
}

func TestConvertStreamingChatCompletionToResponsesErrorEventBecomesFailed(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("event: error\ndata: {\"error\":{\"message\":\"bad request\",\"type\":\"invalid_request_error\"}}\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "response.failed", events[0].Event)
	require.Equal(t, "bad request", gjson.Get(events[0].Data, "response.error.message").String())
	require.Equal(t, "invalid_request_error", gjson.Get(events[0].Data, "response.error.type").String())
}

func TestConvertStreamingChatCompletionToResponsesDataOnlyErrorBecomesFailed(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	stream := []byte("data: {\"error\":{\"message\":\"quota exceeded\",\"code\":\"rate_limit_exceeded\"}}\n\n")

	converted, err := bridge.ConvertStreamingChatCompletionToResponses(newMockHttpContext(), stream, true)
	require.NoError(t, err)

	events := ExtractStreamingEvents(newMockHttpContext(), converted)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, "response.failed", events[0].Event)
	require.Equal(t, "quota exceeded", gjson.Get(events[0].Data, "response.error.message").String())
	require.Equal(t, "rate_limit_exceeded", gjson.Get(events[0].Data, "response.error.type").String())
}

// TestConvertStreamingChatCompletionToResponsesHandlesEventsSplitAcrossChunks
// verifies that an SSE event whose `data:` line is split across two chunks is
// reassembled via the per-bridge cross-chunk buffer instead of being parsed as
// truncated JSON (which gjson would silently drop).
func TestConvertStreamingChatCompletionToResponsesHandlesEventsSplitAcrossChunks(t *testing.T) {
	bridge := &ResponsesChatCompletionBridge{}
	ctx := newMockHttpContext()

	// Split a single chat-completion delta event mid-JSON across two chunks.
	chunk1 := []byte("data: {\"id\":\"chatcmpl_split\",\"created\":1710000000,\"model\":\"kimi-k2.5\",\"choices\":[{\"delta\":{\"content\":\"hel")
	chunk2 := []byte("lo\"}}]}\n\n")

	converted1, err := bridge.ConvertStreamingChatCompletionToResponses(ctx, chunk1, false)
	require.NoError(t, err)
	// The incomplete tail must be buffered, so no complete event is emitted yet.
	require.Empty(t, converted1)

	converted2, err := bridge.ConvertStreamingChatCompletionToResponses(ctx, chunk2, false)
	require.NoError(t, err)
	require.NotEmpty(t, converted2)

	events := ExtractStreamingEvents(newMockHttpContext(), converted2)
	require.NotEmpty(t, events)

	var delta string
	for _, event := range events {
		if event.Event == "response.output_text.delta" {
			delta = gjson.Get(event.Data, "delta").String()
		}
	}
	require.Equal(t, "hello", delta, "split payload should be reassembled into a single delta")

	_, err = bridge.ConvertStreamingChatCompletionToResponses(ctx, []byte("data: [DONE]\n\n"), true)
	require.NoError(t, err)
}

// TestExtractStreamingEventsConcatenatesMultiLineData verifies that multiple
// `data:` lines within one SSE event are concatenated with a newline per the
// SSE specification, instead of only the last line surviving.
func TestExtractStreamingEventsConcatenatesMultiLineData(t *testing.T) {
	ctx := newMockHttpContext()
	stream := []byte("data: {\"a\":1}\ndata: {\"b\":2}\n\n")

	events := ExtractStreamingEvents(ctx, stream)
	require.Len(t, events, 1)
	require.Equal(t, "{\"a\":1}\n{\"b\":2}", events[0].Data)
}

// TestExtractStreamingEventsBuffersTailAcrossChunks verifies the shared parser
// carries an incomplete event tail forward across chunks via the buffer key.
func TestExtractStreamingEventsBuffersTailAcrossChunks(t *testing.T) {
	ctx := newMockHttpContext()

	// First chunk ends mid-event (no trailing blank line).
	first := ExtractStreamingEvents(ctx, []byte("data: {\"partial\":"))
	require.Empty(t, first)

	// Second chunk completes the event.
	second := ExtractStreamingEvents(ctx, []byte("true}\n\n"))
	require.Len(t, second, 1)
	require.Equal(t, "{\"partial\":true}", second[0].Data)

	// Buffer must be cleared after a complete event.
	require.Nil(t, ctx.GetContext(ctxKeyStreamingBody))
}
