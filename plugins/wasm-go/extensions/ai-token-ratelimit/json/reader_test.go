package json

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	data = `
{
	"name": "John Doe",
	"age": 30,
	"is_student": true,
	"interests": [
		"reading",
		"traveling",
		"coding"
	],
	"projects": null,
	"address": {
		"street": "123 Main St",
		"city": "Anytown",
		"zip": "12345"
	},
	"nested_object": {
		"user": {
			"details": {
				"hobbies": [
					{
						"title": "hiking",
						"gear": ["boots", "backpack"],
						"history": {"years": 5}
					},
					{
						"title": "photography",
						"gear": ["camera", "tripod"]
					}
				]
			}
		}
	},
	"nested_array": [
		[
			{"type": "matrix", "values": [1, 2, 3]},
			[{"step": 1}, {"step": 2, "notes": [{"text": "deep"}]}]
		],
		{"summary": {"count": 2}}
	]
}
`

	expectedParseResults = map[string]interface{}{
		"$.name":           "John Doe",
		"$.age":            30,
		"$.is_student":     true,
		"$.interests[0]":   "reading",
		"$.interests[1]":   "traveling",
		"$.interests[2]":   "coding",
		"$.projects":       nil,
		"$.address.street": "123 Main St",
		"$.address.city":   "Anytown",
		"$.address.zip":    "12345",
		"$.nested_object.user.details.hobbies[0].title":         "hiking",
		"$.nested_object.user.details.hobbies[0].gear[0]":       "boots",
		"$.nested_object.user.details.hobbies[0].gear[1]":       "backpack",
		"$.nested_object.user.details.hobbies[0].history.years": 5,
		"$.nested_object.user.details.hobbies[1].title":         "photography",
		"$.nested_object.user.details.hobbies[1].gear[0]":       "camera",
		"$.nested_object.user.details.hobbies[1].gear[1]":       "tripod",
		"$.nested_array[0][0].type":                             "matrix",
		"$.nested_array[0][0].values[0]":                        1,
		"$.nested_array[0][0].values[1]":                        2,
		"$.nested_array[0][0].values[2]":                        3,
		"$.nested_array[0][1][0].step":                          1,
		"$.nested_array[0][1][1].step":                          2,
		"$.nested_array[0][1][1].notes[0].text":                 "deep",
		"$.nested_array[1].summary.count":                       2,
	}

	expectedSkipResults = map[string]bool{
		"$.name":          true,
		"$.age":           true,
		"$.is_student":    true,
		"$.interests":     true,
		"$.projects":      true,
		"$.address":       true,
		"$.nested_object": true,
		"$.nested_array":  true,
	}
)

func TestFullJsonReading(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fullMode bool
		skipMode bool
	}{
		{
			name:     "Full mode without skip",
			fullMode: true,
			skipMode: false,
		},
		{
			name:     "Full mode with skip",
			fullMode: true,
			skipMode: true,
		},
		{
			name:     "Incremental mode without skip",
			fullMode: false,
			skipMode: false,
		},
		{
			name:     "Incremental mode with skip",
			fullMode: false,
			skipMode: true,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			buffer := NewScrollableBuffer()
			reader := NewJsonReader(buffer)

			observedParseResults := map[string]interface{}{}
			observedSkipResults := map[string]bool{}

			if tc.fullMode {
				_ = buffer.Append([]byte(data), true)
			}

			nextIndex := 0
			feedMoreData := func() {
				if tc.fullMode {
					panic("no more data should be needed in full mode")
				}
				if nextIndex >= len(data) {
					panic("No more data to feed")
				}
				newData := []byte{data[nextIndex]}
				nextIndex++
				_ = buffer.Append(newData, nextIndex >= len(data))
			}

			maxBufferLength := 0

			var jsonToken JsonToken
			var err error
			for docEnded := false; !docEnded; {
				bufferLength := buffer.GetForwardLimit() - buffer.GetBackwardLimit()
				if bufferLength > maxBufferLength {
					maxBufferLength = bufferLength
				}
				jsonToken, err = reader.Peek()
				if err != nil {
					if errors.Is(err, ErrEndOfFile) {
						assert.Fail(t, "Unexpected end of file")
					}
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						continue
					}
					t.Fatalf("Error peeking token: %v", err)
				}
				switch jsonToken {
				case JsonTokenBeginObject:
					fmt.Println("Begin Object")
					assert.NoError(t, reader.BeginObject())
				case JsonTokenEndObject:
					fmt.Println("End Object")
					assert.NoError(t, reader.EndObject())
				case JsonTokenBeginArray:
					fmt.Println("Begin Array")
					assert.NoError(t, reader.BeginArray())
				case JsonTokenEndArray:
					fmt.Println("End Array")
					assert.NoError(t, reader.EndArray())
				case JsonTokenName:
					name, err := reader.NextName()
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						break
					}
					assert.NoError(t, err)
					path := reader.GetPath()
					fmt.Printf("Name: %s Path :%s\n", name, path)
					if tc.skipMode {
						observedSkipResults[path] = true
						_ = reader.SkipValue()
					}
				case JsonTokenString:
					if tc.skipMode {
						assert.Fail(t, "Should not reach string token in skip mode")
					}
					path := reader.GetPath()
					s, err := reader.NextString()
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						break
					}
					assert.NoError(t, err)
					observedParseResults[path] = s
					fmt.Printf("StringValue of %s: %s\n", path, s)
				case JsonTokenNumber:
					if tc.skipMode {
						assert.Fail(t, "Should not reach number token in skip mode")
					}
					path := reader.GetPath()
					n, err := reader.NextInt()
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						break
					}
					assert.NoError(t, err)
					observedParseResults[path] = n
					fmt.Printf("NumberValue of %s: %d\n", path, n)
				case JsonTokenNull:
					if tc.skipMode {
						assert.Fail(t, "Should not reach null token in skip mode")
					}
					path := reader.GetPath()
					err := reader.NextNull()
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						break
					}
					assert.NoError(t, err)
					observedParseResults[path] = nil
					fmt.Printf("NullValue of path: %s\n", path)
				case JsonTokenBoolean:
					if tc.skipMode {
						assert.Fail(t, "Should not reach boolean token in skip mode")
					}
					path := reader.GetPath()
					b, err := reader.NextBoolean()
					if errors.Is(err, ErrEndOfBuffer) {
						feedMoreData()
						break
					}
					assert.NoError(t, err)
					observedParseResults[path] = b
					fmt.Printf("BooleanValue of path %s: %t\n", path, b)
				case JsonTokenEndDocument:
					fmt.Println("End of document reached")
					docEnded = true
				default:
					t.Fatalf("Unhandled token: %v", jsonToken)
				}
			}

			if tc.skipMode {
				assert.Equal(t, 0, len(observedParseResults), "No parse results should be recorded in skip mode")
				assert.Equal(t, expectedSkipResults, observedSkipResults, "Skipped results do not match expected results")
			} else {
				assert.Equal(t, expectedParseResults, observedParseResults, "Parsed results do not match expected results")
				assert.Equal(t, 0, len(observedSkipResults), "No skip results should be recorded in parse mode")
			}

			fmt.Printf("Max buffer length used: %d\n", maxBufferLength)
			if !tc.fullMode {
				assert.Less(t, maxBufferLength, 20, "Buffer length should be small in incremental mode")
			}
		})
	}
}
