package templates

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseJSON parses a JSON string into map[string]interface{}
func parseJSON(t *testing.T, jsonStr string) map[string]interface{} {
	var result map[string]interface{}
	err := json.Unmarshal([]byte(jsonStr), &result)
	require.NoError(t, err, "Failed to parse JSON")
	return result
}

// assertJSONEqual compares two values by JSON serialization
func assertJSONEqual(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	expectedJSON, err := json.Marshal(expected)
	require.NoError(t, err, "Failed to marshal expected value")

	actualJSON, err := json.Marshal(actual)
	require.NoError(t, err, "Failed to marshal actual value")

	// Parse back to interface{} to normalize the comparison
	var expectedNormalized, actualNormalized interface{}
	require.NoError(t, json.Unmarshal(expectedJSON, &expectedNormalized))
	require.NoError(t, json.Unmarshal(actualJSON, &actualNormalized))

	assert.Equal(t, expectedNormalized, actualNormalized, msgAndArgs...)
}

func TestUnpackDefs_SimpleRef(t *testing.T) {
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
					"name": {"type": "string"},
					"age": {"type": "integer"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"age": {"type": "integer"}
				}
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"age": {"type": "integer"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_NestedRefs(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"company": {
				"$ref": "#/$defs/Company"
			}
		},
		"$defs": {
			"Company": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"employee": {"$ref": "#/$defs/Employee"}
				}
			},
			"Employee": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"position": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"company": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"employee": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"position": {"type": "string"}
						}
					}
				}
			}
		},
		"$defs": {
			"Company": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"employee": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"position": {"type": "string"}
						}
					}
				}
			},
			"Employee": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"position": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_ArrayItems(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"$ref": "#/$defs/User"
				}
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"users": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string"}
					}
				}
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_AllOf(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"admin": {
				"allOf": [
					{"$ref": "#/$defs/User"},
					{
						"properties": {
							"role": {"type": "string"}
						}
					}
				]
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"admin": {
				"allOf": [
					{
						"type": "object",
						"properties": {
							"name": {"type": "string"}
						}
					},
					{
						"properties": {
							"role": {"type": "string"}
						}
					}
				]
			}
		},
		"$defs": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_CircularRef(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"node": {
				"$ref": "#/$defs/Node"
			}
		},
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {
					"value": {"type": "string"},
					"next": {"$ref": "#/$defs/Node"}
				}
			}
		}
	}`)

	// Expected: first level resolved, $defs also resolved one level, but nested circular ref remains
	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"node": {
				"type": "object",
				"properties": {
					"value": {"type": "string"},
					"next": {"$ref": "#/$defs/Node"}
				}
			}
		},
		"$defs": {
			"Node": {
				"type": "object",
				"properties": {
					"value": {"type": "string"},
					"next": {
						"type": "object",
						"properties": {
							"value": {"type": "string"},
							"next": {"$ref": "#/$defs/Node"}
						}
					}
				}
			}
		}
	}`)

	// This should not panic or hang
	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_ExternalDefs(t *testing.T) {
	externalDefs := parseJSON(t, `{
		"Address": {
			"type": "object",
			"properties": {
				"street": {"type": "string"},
				"city": {"type": "string"}
			}
		}
	}`)

	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"address": {
				"$ref": "#/$defs/Address"
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"address": {
				"type": "object",
				"properties": {
					"street": {"type": "string"},
					"city": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, externalDefs)
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_DefinitionsKeyword(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"user": {
				"$ref": "#/definitions/User"
			}
		},
		"definitions": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"user": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		},
		"definitions": {
			"User": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_UnknownRef(t *testing.T) {
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"unknown": {
				"$ref": "#/$defs/UnknownType"
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"unknown": {
				"$ref": "#/$defs/UnknownType"
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestDeepCopy(t *testing.T) {
	original := parseJSON(t, `{
		"name": "test",
		"nested": {
			"value": 42
		}
	}`)

	copied := deepCopy(original)

	// Modify the copy
	copiedMap := copied.(map[string]interface{})
	copiedMap["name"] = "modified"
	copiedNested := copiedMap["nested"].(map[string]interface{})
	copiedNested["value"] = float64(99)

	// Verify original is unchanged
	expectedOriginal := parseJSON(t, `{
		"name": "test",
		"nested": {
			"value": 42
		}
	}`)
	assertJSONEqual(t, expectedOriginal, original)
}

func TestUnpackDefs_ComplexRealWorld(t *testing.T) {
	// Test a more complex real-world scenario with nested definitions
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"tools": {
				"type": "array",
				"items": {
					"$ref": "#/$defs/Tool"
				}
			}
		},
		"$defs": {
			"Tool": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"parameters": {"$ref": "#/$defs/Parameters"}
				}
			},
			"Parameters": {
				"type": "object",
				"properties": {
					"type": {"type": "string"},
					"properties": {
						"type": "object",
						"additionalProperties": {"$ref": "#/$defs/Property"}
					}
				}
			},
			"Property": {
				"type": "object",
				"properties": {
					"type": {"type": "string"},
					"description": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"tools": {
				"type": "array",
				"items": {
					"type": "object",
					"properties": {
						"name": {"type": "string"},
						"parameters": {
							"type": "object",
							"properties": {
								"type": {"type": "string"},
								"properties": {
									"type": "object",
									"additionalProperties": {
										"type": "object",
										"properties": {
											"type": {"type": "string"},
											"description": {"type": "string"}
										}
									}
								}
							}
						}
					}
				}
			}
		},
		"$defs": {
			"Tool": {
				"type": "object",
				"properties": {
					"name": {"type": "string"},
					"parameters": {
						"type": "object",
						"properties": {
							"type": {"type": "string"},
							"properties": {
								"type": "object",
								"additionalProperties": {
									"type": "object",
									"properties": {
										"type": {"type": "string"},
										"description": {"type": "string"}
									}
								}
							}
						}
					}
				}
			},
			"Parameters": {
				"type": "object",
				"properties": {
					"type": {"type": "string"},
					"properties": {
						"type": "object",
						"additionalProperties": {
							"type": "object",
							"properties": {
								"type": {"type": "string"},
								"description": {"type": "string"}
							}
						}
					}
				}
			},
			"Property": {
				"type": "object",
				"properties": {
					"type": {"type": "string"},
					"description": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_NestedDefsInDefs(t *testing.T) {
	// Test case where $defs contains definitions with their own nested $defs
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"container": {
				"$ref": "#/$defs/Container"
			}
		},
		"$defs": {
			"Container": {
				"type": "object",
				"properties": {
					"item": {"$ref": "#/$defs/Item"}
				},
				"$defs": {
					"Item": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"details": {"$ref": "#/$defs/Details"}
						}
					},
					"Details": {
						"type": "object",
						"properties": {
							"description": {"type": "string"}
						}
					}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"container": {
				"type": "object",
				"properties": {
					"item": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"details": {
								"type": "object",
								"properties": {
									"description": {"type": "string"}
								}
							}
						}
					}
				},
				"$defs": {
					"Item": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"details": {
								"type": "object",
								"properties": {
									"description": {"type": "string"}
								}
							}
						}
					},
					"Details": {
						"type": "object",
						"properties": {
							"description": {"type": "string"}
						}
					}
				}
			}
		},
		"$defs": {
			"Container": {
				"type": "object",
				"properties": {
					"item": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"details": {
								"type": "object",
								"properties": {
									"description": {"type": "string"}
								}
							}
						}
					}
				},
				"$defs": {
					"Item": {
						"type": "object",
						"properties": {
							"name": {"type": "string"},
							"details": {
								"type": "object",
								"properties": {
									"description": {"type": "string"}
								}
							}
						}
					},
					"Details": {
						"type": "object",
						"properties": {
							"description": {"type": "string"}
						}
					}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_DefsInProperties(t *testing.T) {
	// Test case where $defs appears inside properties, not just at the top level
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"person": {
				"type": "object",
				"properties": {
					"address": {"$ref": "#/$defs/Address"}
				},
				"$defs": {
					"Address": {
						"type": "object",
						"properties": {
							"street": {"type": "string"},
							"city": {"type": "string"}
						}
					}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"person": {
				"type": "object",
				"properties": {
					"address": {
						"type": "object",
						"properties": {
							"street": {"type": "string"},
							"city": {"type": "string"}
						}
					}
				},
				"$defs": {
					"Address": {
						"type": "object",
						"properties": {
							"street": {"type": "string"},
							"city": {"type": "string"}
						}
					}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}

func TestUnpackDefs_DefsAtMultipleLevels(t *testing.T) {
	// Test case where $defs appears at multiple levels with scoping
	input := parseJSON(t, `{
		"type": "object",
		"properties": {
			"outer": {"$ref": "#/$defs/OuterType"},
			"section": {
				"type": "object",
				"properties": {
					"inner": {"$ref": "#/$defs/InnerType"}
				},
				"$defs": {
					"InnerType": {
						"type": "object",
						"properties": {
							"value": {"type": "number"}
						}
					}
				}
			}
		},
		"$defs": {
			"OuterType": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	expected := parseJSON(t, `{
		"type": "object",
		"properties": {
			"outer": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			},
			"section": {
				"type": "object",
				"properties": {
					"inner": {
						"type": "object",
						"properties": {
							"value": {"type": "number"}
						}
					}
				},
				"$defs": {
					"InnerType": {
						"type": "object",
						"properties": {
							"value": {"type": "number"}
						}
					}
				}
			}
		},
		"$defs": {
			"OuterType": {
				"type": "object",
				"properties": {
					"name": {"type": "string"}
				}
			}
		}
	}`)

	UnpackDefs(input, map[string]interface{}{})
	assertJSONEqual(t, expected, input)
}
