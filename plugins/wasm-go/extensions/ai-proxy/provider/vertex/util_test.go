package vertex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetImageMimeTypeFromUrl(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		// Images
		{name: "JPEG with .jpg", url: "https://example.com/image.jpg", expected: "image/jpeg"},
		{name: "JPEG with .jpeg", url: "https://example.com/image.jpeg", expected: "image/jpeg"},
		{name: "PNG", url: "https://example.com/image.png", expected: "image/png"},
		{name: "WebP", url: "https://example.com/image.webp", expected: "image/webp"},
		// Videos
		{name: "MP4", url: "https://example.com/video.mp4", expected: "video/mp4"},
		{name: "MOV", url: "https://example.com/video.mov", expected: "video/mov"},
		{name: "MPEG", url: "https://example.com/video.mpeg", expected: "video/mpeg"},
		{name: "MPG", url: "https://example.com/video.mpg", expected: "video/mpg"},
		{name: "AVI", url: "https://example.com/video.avi", expected: "video/avi"},
		{name: "WMV", url: "https://example.com/video.wmv", expected: "video/wmv"},
		{name: "MPEGPS", url: "https://example.com/video.mpegps", expected: "video/mpegps"},
		{name: "FLV", url: "https://example.com/video.flv", expected: "video/flv"},
		// Audio
		{name: "MP3", url: "https://example.com/audio.mp3", expected: "audio/mp3"},
		{name: "WAV", url: "https://example.com/audio.wav", expected: "audio/wav"},
		{name: "OGG", url: "https://example.com/audio.ogg", expected: "audio/ogg"},
		// Documents
		{name: "PDF", url: "https://example.com/document.pdf", expected: "application/pdf"},
		{name: "TXT", url: "https://example.com/document.txt", expected: "text/plain"},
		// Edge cases
		{name: "Upper case", url: "https://example.com/IMAGE.JPG", expected: "image/jpeg"},
		{name: "Mixed case", url: "https://example.com/Image.JpG", expected: "image/jpeg"},
		{name: "With query params", url: "https://example.com/image.png?size=large", expected: ""},
		{name: "Unknown extension", url: "https://example.com/file.xyz", expected: ""},
		{name: "No extension", url: "https://example.com/file", expected: ""},
		{name: "Empty string", url: "", expected: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetMimeTypeFromURL(tt.url)
			assert.Equal(t, tt.expected, result, "URL: %s", tt.url)
		})
	}
}

func TestRemoveAdditionalProperties(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "Remove additionalProperties when false",
			input: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
				"additionalProperties": false,
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			name: "Keep additionalProperties when true",
			input: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
			expected: map[string]interface{}{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
		{
			name: "Keep additionalProperties when object",
			input: map[string]interface{}{
				"type": "object",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
			expected: map[string]interface{}{
				"type": "object",
				"additionalProperties": map[string]interface{}{
					"type": "string",
				},
			},
		},
		{
			name: "Nested schema with false additionalProperties",
			input: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"address": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"street": map[string]interface{}{"type": "string"},
						},
						"additionalProperties": false,
					},
				},
				"additionalProperties": false,
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"address": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"street": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
		{
			name: "Array of schemas",
			input: []interface{}{
				map[string]interface{}{
					"type":                 "object",
					"additionalProperties": false,
				},
				map[string]interface{}{
					"type":                 "string",
					"additionalProperties": false,
				},
			},
			expected: []interface{}{
				map[string]interface{}{
					"type": "object",
				},
				map[string]interface{}{
					"type": "string",
				},
			},
		},
		{
			name: "No additionalProperties field",
			input: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			name:     "Empty map",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name:     "Empty array",
			input:    []interface{}{},
			expected: []interface{}{},
		},
		{
			name:     "Primitive value",
			input:    "string value",
			expected: "string value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeAdditionalProperties(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRemoveStrictFromSchema(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "Remove strict field",
			input: map[string]interface{}{
				"type":   "object",
				"strict": true,
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			name: "Remove strict when false",
			input: map[string]interface{}{
				"type":   "object",
				"strict": false,
			},
			expected: map[string]interface{}{
				"type": "object",
			},
		},
		{
			name: "Nested schema with strict",
			input: map[string]interface{}{
				"type":   "object",
				"strict": true,
				"properties": map[string]interface{}{
					"address": map[string]interface{}{
						"type":   "object",
						"strict": true,
						"properties": map[string]interface{}{
							"street": map[string]interface{}{
								"type":   "string",
								"strict": false,
							},
						},
					},
				},
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"address": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"street": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
			},
		},
		{
			name: "Array of schemas with strict",
			input: []interface{}{
				map[string]interface{}{
					"type":   "object",
					"strict": true,
				},
				map[string]interface{}{
					"type":   "string",
					"strict": false,
				},
			},
			expected: []interface{}{
				map[string]interface{}{
					"type": "object",
				},
				map[string]interface{}{
					"type": "string",
				},
			},
		},
		{
			name: "No strict field",
			input: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
			expected: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			name: "Mixed with other fields",
			input: map[string]interface{}{
				"type":        "object",
				"strict":      true,
				"required":    []string{"name", "age"},
				"description": "User schema",
			},
			expected: map[string]interface{}{
				"type":        "object",
				"required":    []string{"name", "age"},
				"description": "User schema",
			},
		},
		{
			name:     "Empty map",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name:     "Empty array",
			input:    []interface{}{},
			expected: []interface{}{},
		},
		{
			name:     "Primitive value",
			input:    "string value",
			expected: "string value",
		},
		{
			name:     "Nil value",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeStrictFromSchema(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// parseJSON parses a JSON string into map[string]interface{}
func parseJSON(t *testing.T, jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	require.NoError(t, err, "Failed to parse JSON")
	return result
}

// toJSON converts an interface{} to a JSON string
func toJSON(t *testing.T, v interface{}) string {
	data, err := json.Marshal(v)
	require.NoError(t, err, "Failed to marshal to JSON")
	return string(data)
}

func assertSchemaEqual(t *testing.T, expected, actual map[string]interface{}, msgAndArgs ...interface{}) {
	// Deep copy to avoid modifying the original
	expectedCopy := deepCopyValue(expected).(map[string]interface{})
	actualCopy := deepCopyValue(actual).(map[string]interface{})
	assert.JSONEq(t, toJSON(t, expectedCopy), toJSON(t, actualCopy), msgAndArgs...)
}

// deepCopyValue creates a deep copy of any value using JSON marshal/unmarshal
func deepCopyValue(v interface{}) interface{} {
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return v
	}
	return result
}

func TestFilterSchemaFields_BasicFiltering(t *testing.T) {
	// Test basic field filtering
	validFields := map[string]bool{
		"type":        true,
		"properties":  true,
		"items":       true,
		"description": true,
		"nullable":    true,
	}

	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "User name"
			}
		},
		"$defs": {
			"User": {
				"type": "object"
			}
		},
		"additionalProperties": false
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "User name"
			}
		}
	}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_NestedProperties(t *testing.T) {
	validFields := map[string]bool{
		"type":       true,
		"properties": true,
	}

	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					}
				},
				"$defs": {
					"Internal": {}
				}
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					}
				}
			}
		}
	}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_WithItems(t *testing.T) {
	validFields := map[string]bool{
		"type":  true,
		"items": true,
	}

	input := parseJSON(t, `{
		"type": "array",
		"items": {
			"type": "string",
			"maxLength": 100
		},
		"minItems": 1
	}`)

	expected := `{
		"type": "array",
		"items": {
			"type": "string"
		}
	}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_WithAnyOf(t *testing.T) {
	validFields := map[string]bool{
		"type":     true,
		"anyOf":    true,
		"nullable": true,
	}

	input := parseJSON(t, `{
		"anyOf": [
			{
				"type": "string",
				"nullable": true,
				"maxLength": 50
			},
			{
				"type": "number",
				"nullable": true,
				"maximum": 100
			}
		],
		"description": "Either string or number"
	}`)

	expected := `{
		"anyOf": [
			{
				"type": "string",
				"nullable": true
			},
			{
				"type": "number",
				"nullable": true
			}
		]
	}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_ComplexSchema(t *testing.T) {
	validFields := map[string]bool{
		"type":       true,
		"properties": true,
		"items":      true,
		"anyOf":      true,
		"nullable":   true,
	}

	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"status": {
							"anyOf": [
								{"type": "string", "nullable": true},
								{"type": "null"}
							]
						}
					}
				}
			}
		},
		"$defs": {
			"User": {"type": "object"}
		},
		"additionalProperties": false
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"status": {
							"anyOf": [
								{"type": "string", "nullable": true},
								{"type": "null"}
							]
						}
					}
				}
			}
		}
	}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_EmptyValidFields(t *testing.T) {
	validFields := map[string]bool{}

	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`)

	expected := `{}`

	result := FilterSchemaFields(input, validFields)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestFilterSchemaFields_NilInput(t *testing.T) {
	validFields := map[string]bool{
		"type": true,
	}

	result := FilterSchemaFields(nil, validFields)
	assert.Nil(t, result)
}

func Test_addObjectType_Basic(t *testing.T) {
	// Test basic object type addition
	input := parseJSON(t, `{
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		}
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_RemovesNilRequired(t *testing.T) {
	// Test that nil required field is removed
	input := map[string]interface{}{
		"properties": map[string]interface{}{
			"name": map[string]interface{}{
				"type": "string",
			},
		},
		"required": nil,
	}

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			}
		}
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_PreservesNonNilRequired(t *testing.T) {
	// Test that non-nil required field is preserved
	input := parseJSON(t, `{
		"properties": {
			"name": {
				"type": "string"
			}
		},
		"required": ["name"]
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			}
		},
		"required": ["name"]
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_NestedObjects(t *testing.T) {
	// Test nested objects
	input := parseJSON(t, `{
		"properties": {
			"user": {
				"properties": {
					"name": {
						"type": "string"
					},
					"address": {
						"properties": {
							"street": {
								"type": "string"
							}
						}
					}
				}
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					},
					"address": {
						"type": "object",
						"properties": {
							"street": {
								"type": "string"
							}
						}
					}
				}
			}
		}
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_WithItems(t *testing.T) {
	// Test with array items
	input := parseJSON(t, `{
		"type": "array",
		"items": {
			"properties": {
				"id": {
					"type": "integer"
				},
				"name": {
					"type": "string"
				}
			}
		}
	}`)

	expected := `{
		"type": "array",
		"items": {
			"type": "object",
			"properties": {
				"id": {
					"type": "integer"
				},
				"name": {
					"type": "string"
				}
			}
		}
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_NoProperties(t *testing.T) {
	// Test schema without properties (should not add type)
	input := parseJSON(t, `{
		"type": "string"
	}`)

	expected := `{
		"type": "string"
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_addObjectType_ComplexNested(t *testing.T) {
	// Test complex nested structure with both properties and items
	input := parseJSON(t, `{
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"properties": {
						"name": {
							"type": "string"
						},
						"addresses": {
							"type": "array",
							"items": {
								"properties": {
									"street": {
										"type": "string"
									},
									"city": {
										"type": "string"
									}
								}
							}
						}
					}
				}
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"addresses": {
							"type": "array",
							"items": {
								"type": "object",
								"properties": {
									"street": {
										"type": "string"
									},
									"city": {
										"type": "string"
									}
								}
							}
						}
					}
				}
			}
		}
	}`

	addObjectType(input)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_Basic(t *testing.T) {
	// Test basic conversion of anyOf with null
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "string"},
			{"type": "null"}
		]
	}`)

	expected := `{
		"anyOf": [
			{"type": "string", "nullable": true}
		]
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_MultipleTypes(t *testing.T) {
	// Test anyOf with multiple types and null
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "string"},
			{"type": "integer"},
			{"type": "null"}
		]
	}`)

	expected := `{
		"anyOf": [
			{"type": "string", "nullable": true},
			{"type": "integer", "nullable": true}
		]
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_NoNull(t *testing.T) {
	// Test anyOf without null (should not modify)
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "string"},
			{"type": "integer"}
		]
	}`)

	expected := `{
		"anyOf": [
			{"type": "string"},
			{"type": "integer"}
		]
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_OnlyNull(t *testing.T) {
	// Test anyOf with only null type (should return error)
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "null"}
		]
	}`)

	err := convertAnyOfNullToNullable(input, 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only null type is not supported")
}

func Test_convertAnyOfNullToNullable_NestedProperties(t *testing.T) {
	// Test nested properties with anyOf containing null
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"status": {
				"anyOf": [
					{"type": "string"},
					{"type": "null"}
				]
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"status": {
				"anyOf": [
					{"type": "string", "nullable": true}
				]
			}
		}
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_WithItems(t *testing.T) {
	// Test array items with anyOf containing null
	input := parseJSON(t, `{
		"type": "array",
		"items": {
			"anyOf": [
				{"type": "string"},
				{"type": "integer"},
				{"type": "null"}
			]
		}
	}`)

	expected := `{
		"type": "array",
		"items": {
			"anyOf": [
				{"type": "string", "nullable": true},
				{"type": "integer", "nullable": true}
			]
		}
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_ComplexNested(t *testing.T) {
	// Test complex nested structure
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"anyOf": [
								{"type": "string"},
								{"type": "null"}
							]
						},
						"age": {
							"anyOf": [
								{"type": "integer"},
								{"type": "null"}
							]
						}
					}
				}
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"anyOf": [
								{"type": "string", "nullable": true}
							]
						},
						"age": {
							"anyOf": [
								{"type": "integer", "nullable": true}
							]
						}
					}
				}
			}
		}
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_NoAnyOf(t *testing.T) {
	// Test schema without anyOf (should not modify)
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			}
		}
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			}
		}
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func Test_convertAnyOfNullToNullable_MaxDepth(t *testing.T) {
	// Test max depth protection
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "string"},
			{"type": "null"}
		]
	}`)

	// Should return error when depth exceeds max
	err := convertAnyOfNullToNullable(input, 11)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "max depth")
}

func Test_convertAnyOfNullToNullable_NullWithOtherFields(t *testing.T) {
	// Test that null type with other fields is NOT removed
	input := parseJSON(t, `{
		"anyOf": [
			{"type": "string"},
			{"type": "null", "description": "no value"}
		]
	}`)

	// The null type should NOT be removed because it has additional fields
	expected := `{
		"anyOf": [
			{"type": "string"},
			{"type": "null", "description": "no value"}
		]
	}`

	err := convertAnyOfNullToNullable(input, 0)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, input))
}

func TestBuildVertexSchema_Basic(t *testing.T) {
	// Test basic schema building without property ordering
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		},
		"additionalProperties": false,
		"$schema": "http://json-schema.org/draft-07/schema#"
	}`)

	expected := `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		}
	}`

	result, err := BuildVertexSchema(input, false)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestBuildVertexSchema_WithDefs(t *testing.T) {
	// Test schema with $defs that need to be unpacked
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"user": {
				"$ref": "#/$defs/User"
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					}
				}
			}
		}
	}`)

	// Expected output after unpacking $defs and removing $ref
	expected := `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {
						"type": "string"
					}
				}
			}
		}
	}`

	result, err := BuildVertexSchema(input, false)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestBuildVertexSchema_WithAnyOfNull(t *testing.T) {
	// Test schema with anyOf containing null
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"status": {
				"anyOf": [
					{"type": "string"},
					{"type": "null"}
				]
			}
		}
	}`)

	// Expected output: null removed from anyOf, remaining types marked as nullable
	expected := `{
		"type": "object",
		"properties": {
			"status": {
				"anyOf": [
					{"type": "string", "nullable": true}
				]
			}
		}
	}`

	result, err := BuildVertexSchema(input, false)
	require.NoError(t, err)
	assert.JSONEq(t, expected, toJSON(t, result))
}

func TestBuildVertexSchema_ComplexWithAllFeatures(t *testing.T) {
	// Test complex schema with all features
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"properties": {
						"name": {
							"type": "string"
						},
						"email": {
							"anyOf": [
								{"type": "string"},
								{"type": "null"}
							]
						}
					}
				}
			}
		},
		"$defs": {
			"User": {
				"type": "object"
			}
		},
		"additionalProperties": false,
		"$schema": "http://json-schema.org/draft-07/schema#"
	}`)

	// Expected output:
	// - $defs, additionalProperties, $schema removed
	// - type added to objects with properties
	// - anyOf null converted to nullable
	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"email": {
							"anyOf": [
								{"type": "string", "nullable": true}
							]
						}
					}
				}
			}
		}
	}`)

	result, err := BuildVertexSchema(input, true)
	require.NoError(t, err)
	assertSchemaEqual(t, expected, result)
}

func Test_mapResponseSchema_SingleDict(t *testing.T) {
	// Test mapping a single schema dict
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		},
		"additionalProperties": false
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"name": {
				"type": "string"
			},
			"age": {
				"type": "integer"
			}
		}
	}`)

	result, err := mapResponseSchema(input)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok, "Result should be a map")
	assertSchemaEqual(t, expected, resultMap)
}

func Test_mapResponseSchema_ArrayOfDicts(t *testing.T) {
	// Test mapping an array of schema dicts
	input := []interface{}{
		parseJSON(t, `{
			"type": "object",
			"properties": {
				"name": {
					"type": "string"
				}
			}
		}`),
		parseJSON(t, `{
			"type": "object",
			"properties": {
				"email": {
					"type": "string"
				}
			}
		}`),
	}

	result, err := mapResponseSchema(input)
	require.NoError(t, err)

	resultArray, ok := result.([]interface{})
	require.True(t, ok, "Result should be an array")
	require.Equal(t, 2, len(resultArray))

	// Check first item
	firstItem, ok := resultArray[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "object", firstItem["type"])

	// Check second item
	secondItem, ok := resultArray[1].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "object", secondItem["type"])
}

func Test_mapResponseSchema_WithAnyOfNull(t *testing.T) {
	// Test mapping with anyOf null conversion
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"status": {
				"anyOf": [
					{"type": "string"},
					{"type": "null"}
				]
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"status": {
				"anyOf": [
					{"type": "string", "nullable": true}
				]
			}
		}
	}`)

	result, err := mapResponseSchema(input)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assertSchemaEqual(t, expected, resultMap)
}

func Test_mapResponseSchema_NonDictOrArray(t *testing.T) {
	// Test that non-dict/array values are returned as-is
	input := "just a string"

	result, err := mapResponseSchema(input)
	require.NoError(t, err)
	assert.Equal(t, "just a string", result)
}

func Test_mapResponseSchema_NilInput(t *testing.T) {
	// Test nil input
	result, err := mapResponseSchema(nil)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func Test_mapResponseSchema_ArrayWithMixedTypes(t *testing.T) {
	// Test array with mixed types (dict and non-dict)
	input := []interface{}{
		parseJSON(t, `{
			"type": "object",
			"properties": {
				"name": {
					"type": "string"
				}
			}
		}`),
		"not a dict",
		123,
	}

	result, err := mapResponseSchema(input)
	require.NoError(t, err)

	resultArray, ok := result.([]interface{})
	require.True(t, ok)
	require.Equal(t, 3, len(resultArray))

	// First item should be processed
	_, ok = resultArray[0].(map[string]interface{})
	require.True(t, ok)

	// Other items should be unchanged
	assert.Equal(t, "not a dict", resultArray[1])
	assert.Equal(t, float64(123), resultArray[2])
}

func Test_mapResponseSchema_ComplexNestedSchema(t *testing.T) {
	// Test complex nested schema with all transformations
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"properties": {
						"name": {
							"type": "string"
						},
						"email": {
							"anyOf": [
								{"type": "string"},
								{"type": "null"}
							]
						}
					}
				}
			}
		},
		"$defs": {
			"User": {
				"type": "object"
			}
		},
		"additionalProperties": false
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {
							"type": "string"
						},
						"email": {
							"anyOf": [
								{"type": "string", "nullable": true}
							]
						}
					}
				}
			}
		}
	}`)

	result, err := mapResponseSchema(input)
	require.NoError(t, err)

	resultMap, ok := result.(map[string]interface{})
	require.True(t, ok)
	assertSchemaEqual(t, expected, resultMap)
}

func Test_mapResponseSchema_ErrorPropagation(t *testing.T) {
	// Test that errors from BuildVertexSchema are propagated
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"value": {
				"anyOf": [
					{"type": "null"}
				]
			}
		}
	}`)

	_, err := mapResponseSchema(input)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only null type is not supported")
}
