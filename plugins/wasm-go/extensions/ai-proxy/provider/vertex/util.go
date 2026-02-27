package vertex

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util/templates"
)

const (
	defaultMaxRecurseDepth = 10 // DEFAULT_MAX_RECURSE_DEPTH from Python
)

// GetMimeTypeFromURL gets mime type for common image/video/audio URLs from file extension
func GetMimeTypeFromURL(url string) string {
	url = strings.ToLower(url)
	lastDot := strings.LastIndex(url, ".")
	if lastDot == -1 {
		return ""
	}
	ext := url[lastDot:]
	if mimeType, ok := extensionToMimeType[ext]; ok {
		return mimeType
	}
	return ""
}

// extensionToMimeType maps file extensions to MIME types
// Supported by Gemini: https://cloud.google.com/vertex-ai/generative-ai/docs/multimodal/image-understanding#image-requirements
var extensionToMimeType = map[string]string{
	// Images
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	// Videos
	".mp4":    "video/mp4",
	".mov":    "video/mov",
	".mpeg":   "video/mpeg",
	".mpg":    "video/mpg",
	".avi":    "video/avi",
	".wmv":    "video/wmv",
	".mpegps": "video/mpegps",
	".flv":    "video/flv",
	// Audio
	".mp3": "audio/mp3",
	".wav": "audio/wav",
	".ogg": "audio/ogg",
	// Documents
	".pdf": "application/pdf",
	".txt": "text/plain",
}

func RemoveAdditionalProperties(schema map[string]interface{}) map[string]interface{} {
	modifiedSchema := removeAdditionalProperties(schema)
	result, ok := modifiedSchema.(map[string]interface{})
	if ok {
		return result
	}
	log.Warnf("remove additional properties from schema failed: %v", schema)
	// Return original schema if type assertion fails
	return schema
}

// removeAdditionalProperties clean out 'additionalProperties = False'.
// Causes vertexai/gemini OpenAI API Schema errors - https://github.com/langchain-ai/langchainjs/issues/5240
//
// Relevant Issues: https://github.com/BerriAI/litellm/issues/6136, https://github.com/BerriAI/litellm/issues/6088
//
// Based on litellm _remove_additional_properties.
func removeAdditionalProperties(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		// Remove the 'additionalProperties' key if it exists and is set to False
		if additionalProps, ok := v["additionalProperties"]; ok {
			if boolVal, isBool := additionalProps.(bool); isBool && !boolVal {
				delete(v, "additionalProperties")
			}
		}

		// Recursively process all dictionary values
		for key, value := range v {
			v[key] = removeAdditionalProperties(value)
		}

	case []interface{}:
		// Recursively process all items in the list
		for i, item := range v {
			v[i] = removeAdditionalProperties(item)
		}
	}

	return schema
}

func RemoveStrictFromSchema(schema map[string]interface{}) map[string]interface{} {
	modifiedSchema := removeStrictFromSchema(schema)
	result, ok := modifiedSchema.(map[string]interface{})
	if ok {
		return result
	}
	log.Warnf("remove strict from schema failed: %v", schema)
	// Return original schema if type assertion fails
	return schema
}

// removeStrictFromSchema Relevant Issues: https://github.com/BerriAI/litellm/issues/6136, https://github.com/BerriAI/litellm/issues/6088
// Based on litellm _remove_strict_from_schema.
func removeStrictFromSchema(schema interface{}) interface{} {
	switch v := schema.(type) {
	case map[string]interface{}:
		// Remove the 'strict' key if it exists
		if _, ok := v["strict"]; ok {
			delete(v, "strict")
		}

		// Recursively process all dictionary values
		for key, value := range v {
			v[key] = removeStrictFromSchema(value)
		}

	case []interface{}:
		// Recursively process all items in the list
		for i, item := range v {
			v[i] = removeStrictFromSchema(item)
		}
	}

	return schema
}

// FilterSchemaFields recursively filters a schema dictionary to keep only valid fields.
// This is a Go implementation of Python's filter_schema_fields function.
//
// Parameters:
//   - schemaDict: The schema dictionary to filter
//   - validFields: Set of valid field names to keep
//
// Returns:
//   - Filtered schema dictionary containing only valid fields
func FilterSchemaFields(schemaDict map[string]interface{}, validFields map[string]bool) map[string]interface{} {
	processed := make(map[uintptr]bool)
	return filterSchemaFields(schemaDict, validFields, processed)
}

// filterSchemaFields is the internal implementation of FilterSchemaFields
func filterSchemaFields(schemaDict map[string]interface{}, validFields map[string]bool, processed map[uintptr]bool) map[string]interface{} {
	if schemaDict == nil {
		return nil
	}

	// Handle circular references by tracking the address of the map
	// Note: In Go, we use a simplified approach compared to Python's id()
	// This checks if we've seen this exact map reference before
	schemaID := getMapID(schemaDict)
	if processed[schemaID] {
		return schemaDict
	}
	processed[schemaID] = true

	result := make(map[string]interface{})

	for key, value := range schemaDict {
		// Skip fields that are not in the valid fields set
		if !validFields[key] {
			continue
		}

		// Special handling for specific keys
		switch key {
		case "properties":
			if propsMap, ok := value.(map[string]interface{}); ok {
				filteredProps := make(map[string]interface{})
				for k, v := range propsMap {
					if vMap, ok := v.(map[string]interface{}); ok {
						filteredProps[k] = filterSchemaFields(vMap, validFields, processed)
					} else {
						filteredProps[k] = v
					}
				}
				result[key] = filteredProps
			} else {
				result[key] = value
			}

		case "items":
			if itemsMap, ok := value.(map[string]interface{}); ok {
				result[key] = filterSchemaFields(itemsMap, validFields, processed)
			} else {
				result[key] = value
			}

		case "anyOf":
			if anyOfList, ok := value.([]interface{}); ok {
				filteredAnyOf := make([]interface{}, 0, len(anyOfList))
				for _, item := range anyOfList {
					if itemMap, ok := item.(map[string]interface{}); ok {
						filteredAnyOf = append(filteredAnyOf, filterSchemaFields(itemMap, validFields, processed))
					} else {
						filteredAnyOf = append(filteredAnyOf, item)
					}
				}
				result[key] = filteredAnyOf
			} else {
				result[key] = value
			}

		default:
			result[key] = value
		}
	}

	return result
}

// getMapID returns a unique ID for a map based on its pointer value
// This is used for detecting circular references in schema filtering
func getMapID(m map[string]interface{}) uintptr {
	// Use reflect to get the pointer value of the map
	// Maps in Go are reference types, so this gives us a unique identifier
	return reflect.ValueOf(m).Pointer()
}

// validSchemaFields are the set of valid schema fields supported by Vertex AI.
// This is equivalent to Python's `set(get_type_hints(Schema).keys())`.
//
// Based on the Schema TypedDict definition in vertex_ai.py:
//
// class Schema(TypedDict, total=False):
//
//	type: Literal["STRING", "INTEGER", "BOOLEAN", "NUMBER", "ARRAY", "OBJECT"]
//	format: str
//	title: str
//	description: str
//	nullable: bool
//	default: Any
//	items: "Schema"
//	minItems: str
//	maxItems: str
//	enum: List[str]
//	properties: Dict[str, "Schema"]
//	propertyOrdering: List[str]
//	required: List[str]
//	minProperties: str
//	maxProperties: str
//	minimum: float
//	maximum: float
//	minLength: str
//	maxLength: str
//	pattern: str
//	example: Any
//	anyOf: List["Schema"]
//
// Returns:
//   - A map[string]bool representing the set of valid field names
var validSchemaFields = map[string]bool{
	// All fields from Schema TypedDict in vertex_ai.py
	"type":             true, // Literal["STRING", "INTEGER", "BOOLEAN", "NUMBER", "ARRAY", "OBJECT"]
	"format":           true, // str
	"title":            true, // str
	"description":      true, // str
	"nullable":         true, // bool
	"default":          true, // Any
	"items":            true, // "Schema"
	"minItems":         true, // str
	"maxItems":         true, // str
	"enum":             true, // List[str]
	"properties":       true, // Dict[str, "Schema"]
	"propertyOrdering": true, // List[str] - Vertex AI specific for structured outputs
	"required":         true, // List[str]
	"minProperties":    true, // str
	"maxProperties":    true, // str
	"minimum":          true, // float
	"maximum":          true, // float
	"minLength":        true, // str
	"maxLength":        true, // str
	"pattern":          true, // str
	"example":          true, // Any
	"anyOf":            true, // List["Schema"]
}

// addObjectType adds "type": "object" to schemas that have properties.
// This is a Go implementation of Python's add_object_type function.
//
// The function recursively processes the schema:
// - If schema has "properties", it sets "type" to "object" and removes "required" if it's nil
// - Recursively processes all nested properties and items
//
// Based on Python's add_object_type from common_utils.py
func addObjectType(schema map[string]interface{}) {
	// Check if schema has properties
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		// If "required" exists and is nil, remove it
		if required, exists := schema["required"]; exists && required == nil {
			delete(schema, "required")
		}

		// Set type to "object"
		schema["type"] = "object"

		// Recursively process each property
		for _, value := range properties {
			if propSchema, ok := value.(map[string]interface{}); ok {
				addObjectType(propSchema)
			}
		}
	}

	// Recursively process items if they exist
	if items, ok := schema["items"].(map[string]interface{}); ok {
		addObjectType(items)
	}
}

// convertAnyOfNullToNullable converts null objects within anyOf by removing them and adding nullable to all remaining objects.
// This is a Go implementation of Python's convert_anyof_null_to_nullable function.
//
// The function processes anyOf schemas following Vertex AI requirements:
// - Removes {"type": "null"} from anyOf arrays
// - Adds "nullable": true to all remaining types in anyOf
// - Recursively processes properties and items
//
// Based on Python's convert_anyof_null_to_nullable from common_utils.py
// Reference: https://cloud.google.com/vertex-ai/generative-ai/docs/samples/generativeaionvertexai-gemini-controlled-generation-response-schema-3
func convertAnyOfNullToNullable(schema map[string]interface{}, depth int) error {
	if depth > defaultMaxRecurseDepth {
		return fmt.Errorf("max depth of %d exceeded while processing schema. Please check the schema for excessive nesting", defaultMaxRecurseDepth)
	}

	// Check if schema has anyOf
	if anyOf, ok := schema["anyOf"].([]interface{}); ok {
		containsNull := false
		// Create a new slice to store non-null types
		newAnyOf := make([]interface{}, 0, len(anyOf))

		// Check for null types and filter them out
		for _, atype := range anyOf {
			if atypeMap, ok := atype.(map[string]interface{}); ok {
				// Check if this is {"type": "null"}
				if typeVal, hasType := atypeMap["type"]; hasType {
					if typeStr, isString := typeVal.(string); isString && typeStr == "null" {
						// Check if this is exactly {"type": "null"} with no other fields
						if len(atypeMap) == 1 {
							containsNull = true
							continue // Skip adding this to newAnyOf
						}
					}
				}
			}
			newAnyOf = append(newAnyOf, atype)
		}

		// Edge case: response schema with only null type present is invalid in Vertex AI
		if len(newAnyOf) == 0 {
			return fmt.Errorf("invalid input: AnyOf schema with only null type is not supported. Please provide a non-null type")
		}

		// If we found null type, add nullable to all remaining types
		if containsNull {
			for _, atype := range newAnyOf {
				if atypeMap, ok := atype.(map[string]interface{}); ok {
					atypeMap["nullable"] = true
				}
			}
		}

		// Update the anyOf array with filtered types
		schema["anyOf"] = newAnyOf
	}

	// Recursively process properties
	if properties, ok := schema["properties"].(map[string]interface{}); ok {
		for _, value := range properties {
			if propSchema, ok := value.(map[string]interface{}); ok {
				if err := convertAnyOfNullToNullable(propSchema, depth+1); err != nil {
					return err
				}
			}
		}
	}

	// Recursively process items
	if items, ok := schema["items"].(map[string]interface{}); ok {
		if err := convertAnyOfNullToNullable(items, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// BuildVertexSchema builds a Vertex AI compatible schema from input parameters.
// This is a Go implementation of Python's _build_vertex_schema function.
//
// This is a modified version of the schema building logic from:
// https://github.com/google-gemini/generative-ai-python/blob/8f77cc6ac99937cd3a81299ecf79608b91b06bbb/google/generativeai/types/content_types.py#L419
//
// The function updates the input parameters by:
// - Removing extraneous fields
// - Adjusting types
// - Unwinding $defs
// - Adding propertyOrdering if specified
//
// Parameters:
//   - parameters: The JSON schema to build from (will be modified in place)
//   - addPropertyOrdering: Whether to add propertyOrdering to the schema.
//     This is only applicable to schemas for structured outputs.
//     See setSchemaPropertyOrdering for more details.
//
// Returns:
//   - The modified schema (also modified in place)
//   - Error if schema processing fails
//
// Based on Python's _build_vertex_schema from common_utils.py
func BuildVertexSchema(parameters map[string]interface{}, addPropertyOrdering bool) (map[string]interface{}, error) {
	// Step 1: Extract and process $defs
	var defs map[string]interface{}
	if defsVal, ok := parameters["$defs"]; ok {
		if defsMap, ok := defsVal.(map[string]interface{}); ok {
			defs = defsMap
		}
		delete(parameters, "$defs")
	} else {
		defs = make(map[string]interface{})
	}

	// Flatten the defs
	for _, value := range defs {
		if defMap, ok := value.(map[string]interface{}); ok {
			templates.UnpackDefs(defMap, defs)
		}
	}
	templates.UnpackDefs(parameters, defs)

	// Step 2: Convert anyOf null to nullable
	// Nullable fields handling:
	// * https://github.com/pydantic/pydantic/issues/1270
	// * https://stackoverflow.com/a/58841311
	// * https://github.com/pydantic/pydantic/discussions/4872
	if err := convertAnyOfNullToNullable(parameters, 0); err != nil {
		return nil, err
	}

	// Step 3: Add object type
	addObjectType(parameters)

	// Step 4: Postprocessing - Filter out fields that don't exist in Schema
	parameters = FilterSchemaFields(parameters, validSchemaFields)

	// Step 5: Optionally add property ordering
	if addPropertyOrdering {
		// do nothing because unmarshalling from JSON already lost ordering
	}

	return parameters, nil
}

func MapResponseSchema(value map[string]interface{}) (map[string]interface{}, error) {
	mappedSchema, err := mapResponseSchema(value)
	if err != nil {
		return nil, err
	}
	return mappedSchema.(map[string]interface{}), nil
}

// mapResponseSchema maps a response schema value by applying BuildVertexSchema transformation.
// This is a Go implementation of Python's _map_response_schema function.
//
// The function handles both single schema objects and arrays of schemas:
// - For a single schema dict, applies BuildVertexSchema with addPropertyOrdering=true
// - For an array of schemas, applies BuildVertexSchema to each dict item
//
// Parameters:
//   - value: The response schema to map (can be a map or array)
//
// Returns:
//   - The mapped schema with BuildVertexSchema transformations applied
//   - Error if schema processing fails
//
// Based on Python's _map_response_schema from vertex_and_google_ai_studio_gemini.py
func mapResponseSchema(value interface{}) (interface{}, error) {
	// Deep copy the value to avoid modifying the original
	// Note: In Python uses deepcopy, in Go we use JSON marshal/unmarshal
	valueCopy := deepCopyInterface(value)

	switch v := valueCopy.(type) {
	case []interface{}:
		// If it's a list, process each dict item
		for i, item := range v {
			if itemDict, ok := item.(map[string]interface{}); ok {
				processed, err := BuildVertexSchema(itemDict, true)
				if err != nil {
					return nil, fmt.Errorf("failed to process schema at index %d: %w", i, err)
				}
				v[i] = processed
			}
		}
		return v, nil

	case map[string]interface{}:
		// If it's a dict, process it directly
		processed, err := BuildVertexSchema(v, true)
		if err != nil {
			return nil, fmt.Errorf("failed to process schema: %w", err)
		}
		return processed, nil

	default:
		// Return as-is if not a dict or list
		return valueCopy, nil
	}
}

// deepCopyInterface creates a deep copy of any interface value using JSON marshal/unmarshal
func deepCopyInterface(v interface{}) interface{} {
	if v == nil {
		return nil
	}

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

// IsGemini3OrNewerModel checks if the model is Gemini 3 Pro or newer.
//
// Gemini 3 models include:
// - gemini-3-pro-preview
// - Any future Gemini 3.x models
//
// This is equivalent to Python's _is_gemini_3_or_newer method.
func IsGemini3OrNewerModel(model string) bool {
	// Check for Gemini 3 models
	return strings.Contains(model, "gemini-3")
}

// CheckTextInContent check that user_content has 'text' parameter.
//   - Known Vertex Error: Unable to submit request because it must have a text parameter.
//   - 'text' param needs to be len > 0
//   - Relevant Issue: https://github.com/BerriAI/litellm/issues/5515
func CheckTextInContent(parts []*Part) bool {
	for _, part := range parts {
		if part.Text != "" {
			return true
		}
	}
	return false
}

// ProcessGeminiImage processes an image URL and returns the appropriate Part for Gemini.
// This is a Go implementation of Python's _process_gemini_image function.
//
// The function handles three types of image URLs:
// 1. GCS URIs (gs://) - Returns FileData with the GCS URI
// 2. Base64 images (data:image/...) - Returns InlineData with base64 data
// 3. HTTP/HTTPS URLs - Returns FileData with the URL
//
// Parameters:
//   - imageURL: The image URL to process
//   - format: Optional MIME type format (e.g., "image/jpeg", "image/png")
//
// Returns:
//   - *Part: A Part structure with either FileData or InlineData populated
//   - error: Error if the image URL is invalid or empty
//
// Based on Python's _process_gemini_image from transformation.py
func ProcessGeminiImage(imageURL string, format string) (*Part, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("invalid image url: empty URL")
	}

	// Handle GCS URIs (gs://)
	if strings.Contains(imageURL, "gs://") {
		// Figure out file type from extension
		// Python: extension_with_dot = os.path.splitext(image_url)[-1]
		extensionWithDot := filepath.Ext(imageURL)             // Ex: ".png"
		extension := strings.TrimPrefix(extensionWithDot, ".") // Ex: "png"

		mimeType := format
		if mimeType == "" && extension != "" {
			// Python: file_type = get_file_type_from_extension(extension)
			fileType, err := util.GetFileTypeFromExtension(extension)
			if err != nil {
				return nil, fmt.Errorf("unknown file type for extension %s: %w", extension, err)
			}

			// Validate the file type is supported by Gemini
			if !util.IsGemini15AcceptedFileType(fileType) {
				return nil, fmt.Errorf("file type not supported by gemini: %s", fileType)
			}

			// Python: mime_type = get_file_mime_type_for_file_type(file_type)
			mimeType = util.GetFileMimeTypeForFileType(fileType)
		}

		return &Part{
			FileData: &FileData{
				MimeType: mimeType,
				FileURI:  imageURL,
			},
		}, nil
	}

	// Handle base64 images (data:image/...)
	if strings.Contains(imageURL, "base64") {
		// Extract MIME type and base64 data from data URL
		// Format: data:image/jpeg;base64,/9j/4AAQ...
		mimeType := format
		data := ""

		if strings.HasPrefix(imageURL, "data:") {
			// Split by comma to separate metadata from data
			parts := strings.SplitN(imageURL, ",", 2)
			if len(parts) == 2 {
				data = parts[1]

				// Extract MIME type from metadata if format not provided
				if mimeType == "" {
					// Format: data:image/jpeg;base64
					metadata := parts[0]
					// Remove "data:" prefix
					metadata = strings.TrimPrefix(metadata, "data:")
					// Split by semicolon to get MIME type
					mimeParts := strings.Split(metadata, ";")
					if len(mimeParts) > 0 {
						mimeType = mimeParts[0]
					}
				}
			}
		} else {
			// If it's just base64 data without data URL prefix
			data = imageURL
		}

		// Default MIME type if not determined
		if mimeType == "" {
			mimeType = "image/jpeg"
		}

		return &Part{
			InlineData: &Blob{
				MimeType: mimeType,
				Data:     data,
			},
		}, nil
	}

	// Handle HTTP/HTTPS URLs
	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		mimeType := format
		if mimeType == "" {
			// Try to determine MIME type from URL extension
			mimeType = GetMimeTypeFromURL(imageURL)
			if mimeType == "" {
				// Default to image/jpeg if cannot determine
				mimeType = "image/jpeg"
			}
		}

		return &Part{
			FileData: &FileData{
				MimeType: mimeType,
				FileURI:  imageURL,
			},
		}, nil
	}

	return nil, fmt.Errorf("invalid image url: %s", imageURL)
}

func DeleteInvalidRequestFields(body []byte) ([]byte, error) {
	toolsJson := gjson.GetBytes(body, "tools")
	if !toolsJson.Exists() {
		return body, nil
	}
	if !toolsJson.IsArray() {
		log.Debugf("delete invalid vertex tools field: %s", toolsJson.Raw)
		newBody, err := sjson.DeleteBytes(body, "tools")
		if err != nil {
			return nil, errors.New("[vertex]: unable to delete invalid tools field: " + err.Error())
		}
		body = newBody
		log.Debugf("[vertex]: delete invalid tools field successfully: %s", body)
	}

	return body, nil
}

// ExtractGoogleMapsRetrievalConfig extracts location configuration from googleMaps tool for Vertex AI toolConfig.
//
// Input (flat structure):
//
//	{"enableWidget": "...", "latitude": ..., "longitude": ..., "languageCode": "..."}
//
// Output:
//   - cleanedConfig: {"enableWidget": true} (enableWidget normalized to boolean)
//   - retrievalConfig: {"latLng": {"latitude": ..., "longitude": ...}, "languageCode": "..."}
//
// Parameters:
//   - googleMapsConfig: The googleMaps tool configuration
//
// Returns:
//   - cleanedGoogleMapsConfig: googleMaps config without location fields, with enableWidget normalized
//   - retrievalConfig: Location config for toolConfig.retrievalConfig or nil
//
// Based on Python's _extract_google_maps_retrieval_config from vertex_ai.py
func ExtractGoogleMapsRetrievalConfig(googleMapsConfig map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	var retrievalConfig map[string]interface{}

	// Extract location fields
	latitude := googleMapsConfig["latitude"]
	longitude := googleMapsConfig["longitude"]
	languageCode := googleMapsConfig["languageCode"]

	// Build retrieval config if both latitude and longitude are present
	if latitude != nil && longitude != nil {
		retrievalConfig = map[string]interface{}{
			"latLng": map[string]interface{}{
				"latitude":  latitude,
				"longitude": longitude,
			},
		}

		// Add language code if present
		if languageCode != nil {
			retrievalConfig["languageCode"] = languageCode
		}
	}

	googleMaps := make(map[string]interface{})
	if googleMapsConfig["enableWidget"] != nil {
		googleMaps["enableWidget"] = normalizeBool(googleMapsConfig["enableWidget"])
	}

	return googleMaps, retrievalConfig
}

func normalizeBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	}
	return false
}
