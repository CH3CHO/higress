package json

type JsonToken int

const (
	// No token has been peeked yet.
	JsonTokenNone JsonToken = iota
	// Beginning of a JSON array.
	JsonTokenBeginArray
	// End of a JSON array.
	JsonTokenEndArray
	// Beginning of a JSON object.
	JsonTokenBeginObject
	// End of a JSON object.
	JsonTokenEndObject
	// A JSON object name (key).
	JsonTokenName
	// A JSON string value.
	JsonTokenString
	// A JSON boolean value.
	JsonTokenBoolean
	// A JSON null value.
	JsonTokenNull
	// A JSON number value.
	JsonTokenNumber
	// End of the JSON document.
	JsonTokenEndDocument
)

func (j JsonToken) String() string {
	switch j {
	case JsonTokenNone:
		return "NONE"
	case JsonTokenBeginArray:
		return "BEGIN_ARRAY"
	case JsonTokenEndArray:
		return "END_ARRAY"
	case JsonTokenBeginObject:
		return "BEGIN_OBJECT"
	case JsonTokenEndObject:
		return "END_OBJECT"
	case JsonTokenName:
		return "NAME"
	case JsonTokenString:
		return "STRING"
	case JsonTokenBoolean:
		return "BOOLEAN"
	case JsonTokenNull:
		return "NULL"
	case JsonTokenNumber:
		return "NUMBER"
	case JsonTokenEndDocument:
		return "END_DOCUMENT"
	default:
		return "UNKNOWN"
	}
}
