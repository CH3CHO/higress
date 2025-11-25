package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/higress-group/wasm-go/pkg/wrapper"
)

const (
	streamEventIdItemKey        = "id:"
	streamEventNameItemKey      = "event:"
	streamBuiltInItemKey        = ":"
	streamHttpStatusValuePrefix = "HTTP_STATUS/"
	streamDataItemKey           = "data:"
	streamEndDataValue          = "[DONE]"

	ctxKeyStreamingBody = "streamingBody"
)

type StreamEvent struct {
	Id         string `json:"id"`
	Event      string `json:"event"`
	Data       string `json:"data"`
	HttpStatus string `json:"http_status"`
}

func (e *StreamEvent) IsEndData() bool {
	return e.Data == streamEndDataValue
}

func (e *StreamEvent) SetValue(key, value string) {
	switch key {
	case streamEventIdItemKey:
		e.Id = value
	case streamEventNameItemKey:
		e.Event = value
	case streamDataItemKey:
		e.Data = value
	case streamBuiltInItemKey:
		if strings.HasPrefix(value, streamHttpStatusValuePrefix) {
			e.HttpStatus = value[len(streamHttpStatusValuePrefix):]
		}
	}
}

func (e *StreamEvent) ToHttpString() string {
	return fmt.Sprintf("%s %s\n\n", streamDataItemKey, e.Data)
}

func ExtractStreamingEvents(ctx wrapper.HttpContext, chunk []byte) []StreamEvent {
	body := chunk
	if bufferedStreamingBody, has := ctx.GetContext(ctxKeyStreamingBody).([]byte); has {
		body = append(bufferedStreamingBody, chunk...)
	}
	body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
	body = bytes.ReplaceAll(body, []byte("\r"), []byte("\n"))

	eventStartIndex, lineStartIndex, valueStartIndex := -1, -1, -1

	defer func() {
		if eventStartIndex >= 0 && eventStartIndex < len(body) {
			// Just in case the received chunk is not a complete event.
			ctx.SetContext(ctxKeyStreamingBody, body[eventStartIndex:])
		} else {
			ctx.SetContext(ctxKeyStreamingBody, nil)
		}
	}()

	// Sample Qwen event response:
	//
	// event:result
	// :HTTP_STATUS/200
	// data:{"output":{"choices":[{"message":{"content":"你好！","role":"assistant"},"finish_reason":"null"}]},"usage":{"total_tokens":116,"input_tokens":114,"output_tokens":2},"request_id":"71689cfc-1f42-9949-86e8-9563b7f832b1"}
	//
	// event:error
	// :HTTP_STATUS/400
	// data:{"code":"InvalidParameter","message":"Preprocessor error","request_id":"0cbe6006-faec-9854-bf8b-c906d75c3bd8"}
	//

	var events []StreamEvent

	currentKey := ""
	currentEvent := &StreamEvent{}
	i, length := 0, len(body)
	for i = 0; i < length; i++ {
		ch := body[i]
		if ch != '\n' {
			if lineStartIndex == -1 {
				if eventStartIndex == -1 {
					eventStartIndex = i
				}
				lineStartIndex = i
				valueStartIndex = -1
			}
			if valueStartIndex == -1 {
				if ch == ':' {
					valueStartIndex = i + 1
					currentKey = string(body[lineStartIndex:valueStartIndex])
				}
			} else if valueStartIndex == i && ch == ' ' {
				// Skip leading spaces in data.
				valueStartIndex = i + 1
			}
			continue
		}

		if lineStartIndex != -1 {
			if valueStartIndex == -1 {
				// Malformed line, skip it.
			} else {
				value := string(body[valueStartIndex:i])
				currentEvent.SetValue(currentKey, value)
			}
		} else {
			// Extra new line. The current event is complete.
			events = append(events, *currentEvent)
			// Reset event parsing state.
			eventStartIndex = -1
			currentEvent = &StreamEvent{}
		}

		// Reset line parsing state.
		lineStartIndex = -1
		valueStartIndex = -1
		currentKey = ""
	}

	return events
}
