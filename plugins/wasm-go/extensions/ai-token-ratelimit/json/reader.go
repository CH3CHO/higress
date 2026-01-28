package json

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The whole implementation is a port of com.google.gson.stream.JsonReader

type peekedState int

const (
	peekedStateNone peekedState = iota
	peekedStateBeginObject
	peekedStateEndObject
	peekedStateBeginArray
	peekedStateEndArray
	peekedStateTrue
	peekedStateFalse
	peekedStateNull
	peekedStateDoubleQuoted
	// When this is returned, the string value is stored in peekedString.
	peekedStateBuffered
	peekedStateDoubleQuotedName
	// When this is returned, the integer value is stored in peekedLong.
	peekedStateLong
	peekedStateNumber
	peekedStateEOF
)

type numberParsingState int

const (
	numberCharNone numberParsingState = iota
	numberCharSign
	numberCharDigit
	numberCharDecimal
	numberCharFractionDigit
	numberCharExpE
	numberCharExpSign
	numberCharExpDigit
)

const (
	defaultNestingLimit = 255

	quote = byte('"')

	maxNumberLength      = 1024
	minIncompleteInteger = math.MinInt64 / 10
)

var (
	ErrEndOfBuffer     = errors.New("end of buffer")
	ErrEndOfFile       = errors.New("end of file")
	ErrSyntaxError     = errors.New("syntax error")
	ErrUnexpectedToken = errors.New("unexpected token")
)

type JsonReader interface {
	BeginArray() error
	EndArray() error
	BeginObject() error
	EndObject() error
	HasNext() bool
	Peek() (JsonToken, error)
	NextName() (string, error)
	NextString() (string, error)
	NextBoolean() (bool, error)
	NextNull() error
	NextFloat64() (float64, error)
	NextLong() (int64, error)
	NextInt() (int, error)
	SkipValue() error
	GetPath() string
	GetPreviousPath() string
}

func NewJsonReader(buffer ScrollableBuffer) JsonReader {
	reader := &jsonReader{
		buffer:       buffer,
		peeked:       peekedStateNone,
		stack:        make([]JsonScope, 32),
		stackSize:    0,
		nestingLimit: defaultNestingLimit,
		pathNames:    make([]string, 32),
		pathIndices:  make([]int, 32),
	}
	reader.stackSize++
	reader.stack[reader.stackSize-1] = JsonScopeEmptyDocument
	return reader
}

type jsonReader struct {
	buffer ScrollableBuffer

	peeked peekedState

	// A peeked value that was composed entirely of digits with an optional leading dash.
	// Positive values may not have a leading 0.
	peekedLong int64
	// The number of characters in a peeked number literal. Increment 'pos' by this after reading a
	// number.
	peekedNumberLength int
	// A peeked string that should be parsed on the next double, long or string. This is populated
	// before a numeric value is parsed and used if that parsing fails.
	peekedString string
	// Whether a peek operation was interrupted in order to fill the buffer.
	peekInterrupted bool

	// Whether a skipValue is in progress. This is used to make sure the progress won't be interrupted
	// due to end of buffer
	skipping       bool
	skippingPeeked peekedState
	skipDepth      int

	// The nesting stack.
	stack        []JsonScope
	stackSize    int
	nestingLimit int

	lineNumber int
	lineStart  int

	// The path members. It corresponds directly to stack: At indices where the
	// stack contains an object (scopeEmptyObject, scopeDanglingName or scopeNonEmptyObject),
	// pathNames contains the name at this jsonScope. Where it contains an array
	// (scopeEmptyArray, scopeNonEmptyArray) pathIndices contains the current index in
	// that array. Otherwise, the value is undefined, and we take advantage of that
	// by incrementing pathIndices when doing so isn't useful.
	pathNames   []string
	pathIndices []int
}

func (j *jsonReader) BeginArray() error {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return err
		} else {
			p = np
		}
	}
	if p == peekedStateBeginArray {
		_ = j.push(JsonScopeEmptyArray)
		j.pathIndices[j.stackSize-1] = 0
		j.peeked = peekedStateNone
		return nil
	}
	return j.unexpectedTokenError(JsonTokenBeginArray)
}

func (j *jsonReader) EndArray() error {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return err
		} else {
			p = np
		}
	}
	if p == peekedStateEndArray {
		j.stackSize--
		j.pathIndices[j.stackSize-1]++
		j.peeked = peekedStateNone
		return nil
	}
	return j.unexpectedTokenError(JsonTokenEndArray)
}

func (j *jsonReader) BeginObject() error {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return err
		} else {
			p = np
		}
	}
	if p == peekedStateBeginObject {
		_ = j.push(JsonScopeEmptyObject)
		j.peeked = peekedStateNone
		return nil
	}
	return j.unexpectedTokenError(JsonTokenBeginObject)
}

func (j *jsonReader) EndObject() error {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return err
		} else {
			p = np
		}
	}
	if p == peekedStateEndObject {
		j.stackSize--
		j.pathNames[j.stackSize] = "" // Free the last path name so that it can be garbage collected!
		j.pathIndices[j.stackSize-1]++
		j.peeked = peekedStateNone
		return nil
	}
	return j.unexpectedTokenError(JsonTokenEndObject)
}

func (j *jsonReader) HasNext() bool {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return false
		} else {
			p = np
		}
	}
	return p != peekedStateEndObject && p != peekedStateEndArray && p != peekedStateEOF
}

func (j *jsonReader) Peek() (JsonToken, error) {
	if j.skipping {
		// If we are skipping, we should not return any tokens; just skip the value
		if err := j.SkipValue(); err != nil {
			return JsonTokenNone, err
		}
	}

	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return JsonTokenNone, err
		} else {
			p = np
		}
	}

	switch p {
	case peekedStateBeginObject:
		return JsonTokenBeginObject, nil
	case peekedStateEndObject:
		return JsonTokenEndObject, nil
	case peekedStateBeginArray:
		return JsonTokenBeginArray, nil
	case peekedStateEndArray:
		return JsonTokenEndArray, nil
	case peekedStateDoubleQuotedName:
		return JsonTokenName, nil
	case peekedStateTrue, peekedStateFalse:
		return JsonTokenBoolean, nil
	case peekedStateNull:
		return JsonTokenNull, nil
	case peekedStateDoubleQuoted, peekedStateBuffered:
		return JsonTokenString, nil
	case peekedStateLong, peekedStateNumber:
		return JsonTokenNumber, nil
	case peekedStateEOF:
		return JsonTokenEndDocument, nil
	default:
		return JsonTokenNone, fmt.Errorf("unexpected peeked state: %d", p)
	}
}

func (j *jsonReader) NextName() (string, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return "", err
		} else {
			p = np
		}
	}
	result := ""
	var err error
	if p == peekedStateDoubleQuotedName {
		result, err = j.nextQuotedValue()
	} else {
		return "", j.unexpectedTokenErrorWithString("a name")
	}
	if errors.Is(err, ErrEndOfBuffer) {
		return "", err
	}

	j.peeked = peekedStateNone
	j.pathNames[j.stackSize-1] = result
	return result, nil
}

func (j *jsonReader) NextString() (string, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return "", err
		} else {
			p = np
		}
	}
	result := ""
	var err error
	if p == peekedStateDoubleQuoted {
		result, err = j.nextQuotedValue()
	} else if p == peekedStateBuffered {
		result = j.peekedString
		j.peekedString = ""
	} else if p == peekedStateLong {
		result = fmt.Sprintf("%d", j.peekedLong)
	} else if p == peekedStateNumber {
		result = string(j.buffer.ReadBytes(j.peekedNumberLength))
	} else {
		return "", j.unexpectedTokenErrorWithString("a string")
	}
	if errors.Is(err, ErrEndOfBuffer) {
		return "", err
	}

	j.peeked = peekedStateNone
	j.pathIndices[j.stackSize-1]++
	return result, nil
}

func (j *jsonReader) NextBoolean() (bool, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return false, err
		} else {
			p = np
		}
	}
	if p == peekedStateTrue {
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return true, nil
	} else if p == peekedStateFalse {
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return false, nil
	} else {
		return false, j.unexpectedTokenErrorWithString("a boolean")
	}
}

func (j *jsonReader) NextNull() error {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return err
		} else {
			p = np
		}
	}
	if p == peekedStateNull {
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return nil
	} else {
		return j.unexpectedTokenErrorWithString("null")
	}
}

func (j *jsonReader) NextFloat64() (float64, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return 0, err
		} else {
			p = np
		}
	}

	if p == peekedStateLong {
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return float64(j.peekedLong), nil
	}

	var err error
	if p == peekedStateNumber {
		j.peekedString = string(j.buffer.ReadBytes(j.peekedNumberLength))
	} else if p == peekedStateDoubleQuoted {
		j.peekedString, err = j.nextQuotedValue()
	} else if p != peekedStateBuffered {
		return 0, j.unexpectedTokenErrorWithString("a float64")
	}
	if errors.Is(err, ErrEndOfBuffer) {
		return 0, err
	}

	j.peeked = peekedStateBuffered
	result, err := strconv.ParseFloat(j.peekedString, 64) // don't catch this NumberFormatException.
	if err != nil {
		return 0, j.syntaxError("invalid double: " + j.peekedString)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, j.syntaxError(fmt.Sprintf("JSON forbids NaN and infinities: %f", result))
	}
	j.peekedString = ""
	j.peeked = peekedStateNone
	j.pathIndices[j.stackSize-1]++
	return result, nil
}

func (j *jsonReader) NextLong() (int64, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return 0, err
		} else {
			p = np
		}
	}

	if p == peekedStateLong {
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return j.peekedLong, nil
	}

	var err error
	if p == peekedStateNumber {
		j.peekedString = string(j.buffer.ReadBytes(j.peekedNumberLength))
	} else if p == peekedStateDoubleQuoted {
		j.peekedString, err = j.nextQuotedValue()
	} else if p != peekedStateBuffered {
		return 0, j.unexpectedTokenErrorWithString("a long")
	}
	if errors.Is(err, ErrEndOfBuffer) {
		return 0, err
	}

	j.peeked = peekedStateBuffered
	asDouble, err := strconv.ParseFloat(j.peekedString, 64) // don't catch this NumberFormatException.
	if err != nil {
		return 0, j.syntaxError("invalid double: " + j.peekedString)
	}
	result := int64(asDouble)
	if float64(result) != asDouble {
		return 0, fmt.Errorf("expected a long but was %s %s", j.peekedString, j.locationString())
	}
	j.peekedString = ""
	j.peeked = peekedStateNone
	j.pathIndices[j.stackSize-1]++
	return result, nil
}

func (j *jsonReader) NextInt() (int, error) {
	p := j.peeked
	if p == peekedStateNone {
		if np, err := j.doPeek(); err != nil {
			return 0, err
		} else {
			p = np
		}
	}

	var result int
	if p == peekedStateLong {
		result = int(j.peekedLong)
		if int64(result) != j.peekedLong {
			return 0, fmt.Errorf("expected an int but was %d %s", j.peekedLong, j.locationString())
		}
		j.peeked = peekedStateNone
		j.pathIndices[j.stackSize-1]++
		return result, nil
	}

	var err error
	if p == peekedStateNumber {
		j.peekedString = string(j.buffer.ReadBytes(j.peekedNumberLength))
	} else if p == peekedStateDoubleQuoted {
		j.peekedString, err = j.nextQuotedValue()
	} else if p != peekedStateBuffered {
		return 0, j.unexpectedTokenErrorWithString("a long")
	}
	if errors.Is(err, ErrEndOfBuffer) {
		return 0, err
	}

	j.peeked = peekedStateBuffered
	asDouble, err := strconv.ParseFloat(j.peekedString, 64) // don't catch this NumberFormatException.
	if err != nil {
		return 0, j.syntaxError("invalid double: " + j.peekedString)
	}
	result = int(asDouble)
	if float64(result) != asDouble {
		return 0, fmt.Errorf("expected an int but was %s %s", j.peekedString, j.locationString())
	}
	j.peekedString = ""
	j.peeked = peekedStateNone
	j.pathIndices[j.stackSize-1]++
	return result, nil
}

func (j *jsonReader) SkipValue() error {
	j.skipping = true
	for {
		if j.skippingPeeked == peekedStateNone {
			j.skippingPeeked = j.peeked
			if j.skippingPeeked == peekedStateNone {
				if np, err := j.doPeek(); err != nil {
					return err
				} else {
					j.skippingPeeked = np
				}
			}
		}

		switch j.skippingPeeked {
		case peekedStateBeginArray:
			_ = j.push(JsonScopeEmptyArray)
			j.skipDepth++
			break
		case peekedStateBeginObject:
			_ = j.push(JsonScopeEmptyObject)
			j.skipDepth++
			break
		case peekedStateEndArray:
			j.stackSize--
			j.skipDepth--
			break
		case peekedStateEndObject:
			// Only update when object end is explicitly skipped, otherwise stack is not updated
			// anyway
			if j.skipDepth == 0 {
				// Free the last path name so that it can be garbage collected
				j.pathNames[j.stackSize-1] = ""
			}
			j.stackSize--
			j.skipDepth--
			break
		case peekedStateDoubleQuoted:
			if err := j.skipQuotedValue(); err != nil {
				return err
			}
			break
		case peekedStateDoubleQuotedName:
			if err := j.skipQuotedValue(); err != nil {
				return err
			}
			// Only update when name is explicitly skipped, otherwise stack is not updated anyway
			if j.skipDepth == 0 {
				j.pathNames[j.stackSize-1] = "<skipped>"
			}
			break
		case peekedStateNumber:
			_ = j.buffer.Advance(j.peekedNumberLength)
			break
		case peekedStateEOF:
			// Do nothing
			return nil
		default:
			// For all other tokens there is nothing to do; token has already been consumed from
			// underlying reader
		}
		j.skippingPeeked = peekedStateNone
		j.peeked = peekedStateNone

		if j.skipDepth <= 0 {
			break
		}
	}

	j.skipping = false
	j.skippingPeeked = peekedStateNone
	j.skipDepth = 0
	j.pathIndices[j.stackSize-1]++
	return nil
}

func (j *jsonReader) GetPath() string {
	return j.getPath(false)
}

func (j *jsonReader) GetPreviousPath() string {
	return j.getPath(true)
}

func (j *jsonReader) getPath(usePreviousPath bool) string {
	var result strings.Builder
	result.WriteRune('$')
	for i := 0; i < j.stackSize; i++ {
		scope := j.stack[i]
		switch scope {
		case JsonScopeEmptyArray, JsonScopeNonEmptyArray:
			pathIndex := j.pathIndices[i]
			// If index is last path element it points to next array element; have to decrement
			if usePreviousPath && pathIndex > 0 && i == j.stackSize-1 {
				pathIndex--
			}
			result.WriteString(fmt.Sprintf("[%d]", pathIndex))
		case JsonScopeEmptyObject, JsonScopeNonEmptyObject, JsonScopeDanglingName:
			if j.pathNames[i] != "" {
				result.WriteRune('.')
				result.WriteString(j.pathNames[i])
			}
		case JsonScopeNonEmptyDocument, JsonScopeEmptyDocument:
			break
		default:
			// unexpected json scope
			return ""
		}
	}
	return result.String()
}

func (j *jsonReader) doPeek() (peekedState, error) {
	peekStack := j.stack[j.stackSize-1]
	if j.peekInterrupted {
		// We have partially read a token before the buffer ran out. Skip back to where we were.
	} else if peekStack == JsonScopeEmptyArray {
		j.stack[j.stackSize-1] = JsonScopeNonEmptyArray
	} else if peekStack == JsonScopeNonEmptyArray {
		// Look for a comma before the next element.
		c, err := j.nextNonWhitespace()
		if err != nil {
			return peekedStateNone, err
		}
		switch c {
		case ']':
			j.peeked = peekedStateEndArray
			return j.peeked, nil
		case ';':
			return peekedStateNone, j.syntaxError("Unexpected semicolon in array")
		case ',':
			break
		default:
			return peekedStateNone, j.syntaxError("Unterminated array")
		}
	} else if peekStack == JsonScopeEmptyObject || peekStack == JsonScopeNonEmptyObject {
		// Look for a comma before the next element.
		startPos := j.buffer.GetPos()
		if peekStack == JsonScopeNonEmptyObject {
			c, err := j.nextNonWhitespace()
			if err != nil {
				return peekedStateNone, err
			}
			switch c {
			case '}':
				j.peeked = peekedStateEndObject
				return j.peeked, nil
			case ';':
				return peekedStateNone, j.syntaxError("Unexpected semicolon in object")
			case ',':
				break
			default:
				return peekedStateNone, j.syntaxError("Unterminated object")
			}
		}

		c, err := j.nextNonWhitespace()
		if err != nil {
			if errors.Is(err, ErrEndOfBuffer) {
				// Reset the buffer position so that we can do this again after buffer is filled.
				_ = j.buffer.SetPos(startPos)
			}
			return peekedStateNone, err
		}
		j.stack[j.stackSize-1] = JsonScopeDanglingName
		switch c {
		case '"':
			j.peeked = peekedStateDoubleQuotedName
			return j.peeked, nil
		case '\'':
			return peekedStateNone, j.syntaxError("Unexpected single quote in object name")
		case '}':
			if peekStack != JsonScopeNonEmptyObject {
				j.peeked = peekedStateEndObject
				return j.peeked, nil
			} else {
				return peekedStateNone, j.syntaxError("Expected name")
			}
		default:
			return peekedStateNone, j.syntaxError("Unquoted name")
		}
	} else if peekStack == JsonScopeDanglingName {
		c, err := j.nextNonWhitespace()
		if err != nil {
			return peekedStateNone, err
		}
		// Look for a colon before the value.
		j.stack[j.stackSize-1] = JsonScopeNonEmptyObject
		switch c {
		case ':':
			break
		case '=':
			return peekedStateNone, j.syntaxError("Expected ':' instead of '='")
		default:
			return peekedStateNone, j.syntaxError("Expected ':' instead of '" + string(c) + "'")
		}
	} else if peekStack == JsonScopeEmptyDocument {
		j.stack[j.stackSize-1] = JsonScopeNonEmptyDocument
	} else if peekStack == JsonScopeNonEmptyDocument {
		if _, err := j.nextNonWhitespace(); err != nil {
			if errors.Is(err, ErrEndOfFile) {
				j.peeked = peekedStateEOF
				return j.peeked, nil
			}
			return peekedStateNone, err
		}
		_ = j.buffer.Rewind(1)
	}

	pos := j.buffer.GetPos()

	c, err := j.nextNonWhitespace()
	if err != nil {
		j.peekInterrupted = true
		return peekedStateNone, err
	}
	j.peekInterrupted = false
	switch c {
	case ']':
		if peekStack == JsonScopeEmptyArray {
			j.peeked = peekedStateEndArray
			return j.peeked, nil
		}
		fallthrough
	// fall-through to handle ",]"
	case ';':
	case ',':
		// In lenient mode, a 0-length literal in an array means 'null'.
		if peekStack == JsonScopeEmptyArray || peekStack == JsonScopeNonEmptyArray {
			return peekedStateNone, j.syntaxError("Expected value")
		} else {
			return peekedStateNone, j.syntaxError("Unexpected value")
		}
	case '\'':
		return peekedStateNone, j.syntaxError("Unexpected single quote")
	case '"':
		j.peeked = peekedStateDoubleQuoted
		return j.peeked, nil
	case '[':
		j.peeked = peekedStateBeginArray
		return j.peeked, nil
	case '{':
		j.peeked = peekedStateBeginObject
		return j.peeked, nil
	default:
		_ = j.buffer.Rewind(1) // Don't consume the first character in a literal value.
	}

	if result, err := j.peekKeyword(); result != peekedStateNone {
		return result, nil
	} else if errors.Is(err, ErrEndOfBuffer) {
		j.peekInterrupted = true
		_ = j.buffer.SetPos(pos)
		return peekedStateNone, err
	}

	if result, err := j.peekNumber(); result != peekedStateNone {
		return result, nil
	} else if errors.Is(err, ErrEndOfBuffer) {
		j.peekInterrupted = true
		_ = j.buffer.SetPos(pos)
		return peekedStateNone, err
	}

	if !isLiteral(j.buffer.PeekByte()) {
		return peekedStateNone, j.syntaxError("Expected value")
	}

	return peekedStateNone, j.syntaxError("Unquoted value")
}

func (j *jsonReader) peekKeyword() (peekedState, error) {
	// Figure out which keyword we're matching against by its first character.
	c := j.buffer.PeekByte()
	var keyword string
	var keywordUpper string
	peeking := peekedStateNone

	// Look at the first letter to determine what keyword we are trying to match.
	if c == 't' || c == 'T' {
		keyword = "true"
		keywordUpper = "TRUE"
		peeking = peekedStateTrue
	} else if c == 'f' || c == 'F' {
		keyword = "false"
		keywordUpper = "FALSE"
		peeking = peekedStateFalse
	} else if c == 'n' || c == 'N' {
		keyword = "null"
		keywordUpper = "NULL"
		peeking = peekedStateNull
	} else {
		return peekedStateNone, nil
	}

	// Uppercased keywords are not allowed in STRICT mode

	// Confirm that chars [0..length) match the keyword.
	length := len(keyword)
	if !j.buffer.HasBytesAvailable(length) {
		if j.buffer.HasMoreDataComing() {
			return peekedStateNone, ErrEndOfBuffer
		}
		return peekedStateNone, nil
	}
	pos := j.buffer.GetPos()
	for i := 0; i < length; i++ {
		kc := j.buffer.ByteAt(pos + i)
		if kc != keyword[i] && kc != keywordUpper[i] {
			return peekedStateNone, nil
		}
	}

	if !j.buffer.HasBytesAvailable(length + 1) {
		if j.buffer.HasMoreDataComing() {
			// We need to be able to read the next character to see if the keyword is
			return peekedStateNone, ErrEndOfBuffer
		}
		// We've reached the end of the buffer. So if the keyword matches, we are done.
	} else if isLiteral(j.buffer.ByteAt(pos + length)) {
		// Don't match trues, falsey or nullsoft!
		return peekedStateNone, nil
	}

	// We've found the keyword followed either by EOF or by a non-literal character.

	_ = j.buffer.Advance(length)
	j.peeked = peeking
	return j.peeked, nil
}

func (j *jsonReader) peekNumber() (peekedState, error) {
	pos := j.buffer.GetPos()
	limit := j.buffer.GetForwardLimit()

	var value int64 // Negative to accommodate Long.MIN_VALUE more easily.
	negative := false
	fitsInInt64 := true
	last := numberCharNone

	i := 0

charactersOfNumber:
	for ; true; i++ {
		currentPos := pos + i
		if currentPos == limit {
			if i >= maxNumberLength {
				// Though this looks like a well-formed number, it's too long to continue reading. Give up
				// and let the application handle this as an unquoted literal.
				return peekedStateNone, nil
			}
			if j.buffer.HasMoreDataComing() {
				_ = j.buffer.SetPos(pos)
				return peekedStateNone, ErrEndOfBuffer
			} else {
				break
			}
		}

		c := j.buffer.ByteAt(currentPos)
		switch c {
		case '-':
			if last == numberCharNone {
				negative = true
				last = numberCharSign
				continue
			} else if last == numberCharExpE {
				last = numberCharExpSign
				continue
			}
			return peekedStateNone, nil
		case '+':
			if last == numberCharExpE {
				last = numberCharExpSign
				continue
			}
			return peekedStateNone, nil
		case 'e', 'E':
			if last == numberCharDigit || last == numberCharFractionDigit {
				last = numberCharExpE
				continue
			}
			return peekedStateNone, nil
		case '.':
			if last == numberCharDigit {
				last = numberCharDecimal
				continue
			}
			return peekedStateNone, nil
		default:
			if c < '0' || c > '9' {
				if !isLiteral(c) {
					break charactersOfNumber
				}
				return peekedStateNone, nil
			}
			if last == numberCharSign || last == numberCharNone {
				value = -int64(c - '0')
				last = numberCharDigit
			} else if last == numberCharDigit {
				if value == 0 {
					return peekedStateNone, nil // Leading '0' prefix is not allowed (since it could be octal).
				}
				newValue := value*10 - int64(c-'0')
				fitsInInt64 = fitsInInt64 && (value > minIncompleteInteger || (value == minIncompleteInteger && newValue < value))
				value = newValue
			} else if last == numberCharDecimal {
				last = numberCharFractionDigit
			} else if last == numberCharExpE || last == numberCharExpSign {
				last = numberCharExpDigit
			}
		}
	}

	// We've read a complete number. Decide if it's a PEEKED_LONG or a PEEKED_NUMBER.
	// Don't store -0 as long; user might want to read it as double -0.0
	// Don't try to convert Long.MIN_VALUE to positive long; it would overflow MAX_VALUE
	if last == numberCharDigit && fitsInInt64 && (value != math.MinInt64 || negative) && (value != 0 || !negative) {
		if negative {
			j.peekedLong = value
		} else {
			j.peekedLong = -value
		}
		_ = j.buffer.Advance(i)
		j.peeked = peekedStateLong
		return j.peeked, nil
	} else if last == numberCharDigit || last == numberCharFractionDigit || last == numberCharExpDigit {
		j.peekedNumberLength = i
		j.peeked = peekedStateNumber
		return j.peeked, nil
	}

	return peekedStateNone, nil
}

func (j *jsonReader) push(newTop JsonScope) error {
	// - 1 because stack contains as first element either EMPTY_DOCUMENT or NONEMPTY_DOCUMENT
	if j.stackSize-1 >= j.nestingLimit {
		return fmt.Errorf("nesting limit %d reached %s", j.nestingLimit, j.locationString())
	}

	if j.stackSize == len(j.stack) {
		j.stack = append(j.stack, make([]JsonScope, j.stackSize)...)
		j.pathIndices = append(j.pathIndices, make([]int, j.stackSize)...)
		j.pathNames = append(j.pathNames, make([]string, j.stackSize)...)
	}
	j.stack[j.stackSize] = newTop
	j.stackSize++

	return nil
}

func (j *jsonReader) skipQuotedValue() error {
	// Like nextNonWhitespace, this uses locals 'p' and 'l' to save inner-loop field access.
	for {
		p := j.buffer.GetPos()
		l := j.buffer.GetForwardLimit()
		/* the index of the first character not yet appended to the builder. */
		for p < l {
			c := j.buffer.ByteAt(p)
			p++
			if c == quote {
				_ = j.buffer.SetPos(p)
				return nil
			} else if c == '\\' {
				_ = j.buffer.SetPos(p)
				if _, err := j.readEscapeCharacter(); err != nil {
					return err
				}
				p = j.buffer.GetPos()
				l = j.buffer.GetForwardLimit()
			} else if c == '\n' {
				j.lineNumber++
				j.lineStart = p
			}
		}
		_ = j.buffer.SetPos(p)

		if !j.buffer.HasMoreDataComing() {
			return j.syntaxError("Unterminated string")
		} else {
			return ErrEndOfBuffer
		}
	}
}

// Returns the string up to but not including the closing quote, unescaping any character escape
// sequences encountered along the way. The opening quote should have already been read. This
// consumes the closing quote, but does not include it in the returned string.
func (j *jsonReader) nextQuotedValue() (string, error) {
	// Like nextNonWhitespace, this uses locals 'p' and 'l' to save inner-loop field access.
	var builder *strings.Builder
	startPos := j.buffer.GetPos()
	for {
		pos := j.buffer.GetPos()
		limit := j.buffer.GetForwardLimit()
		/* the index of the first character not yet appended to the builder. */
		start := pos
		for pos < limit {
			c := j.buffer.ByteAt(pos)
			pos++

			if c == quote {
				length := pos - start - 1
				s := string(j.buffer.ReadBytes(length))
				_ = j.buffer.SetPos(pos)
				if builder == nil {
					return s, nil
				} else {
					builder.WriteString(s)
					return builder.String(), nil
				}
			} else if c == '\\' {
				length := pos - start - 1
				if builder == nil {
					builder = &strings.Builder{}
					estimatedLength := (length + 1) * 2
					builder.Grow(int(math.Max(float64(estimatedLength), 16)))
				}
				builder.WriteString(string(j.buffer.ReadBytes(length)))
				c, _ := j.readEscapeCharacter()
				builder.WriteRune(c)
				start = j.buffer.GetPos()
			} else if c == '\n' {
				j.lineNumber++
				j.lineStart = pos
			}
		}

		if builder == nil {
			builder = &strings.Builder{}
			estimatedLength := (pos - start) * 2
			builder.Grow(int(math.Max(float64(estimatedLength), 16)))
		}
		builder.WriteString(string(j.buffer.ReadBytes(pos - start)))
		if !j.buffer.HasBytesAvailable(1) {
			if j.buffer.HasMoreDataComing() {
				// We need more data to complete the string. So we reset the buffer position to
				// the start of the string so that when more data is available we can read
				_ = j.buffer.SetPos(startPos)
			}
			return "", j.getEOXError()
		}
	}
}

// readEscapeCharacter unescapes the character identified by the character or characters that immediately follow a
// backslash. The backslash '\' should have already been read. This supports both Unicode escapes
// "u000A" and two-character escapes "\n".
func (j *jsonReader) readEscapeCharacter() (rune, error) {
	if !j.buffer.HasBytesAvailable(1) {
		return 0, j.getEOXError()
	}

	startPos := j.buffer.GetPos()

	escaped := j.buffer.ReadByte()
	switch escaped {
	case 'u':
		hexStart := j.buffer.GetPos()
		hexEnd := hexStart + 4
		if hexEnd > j.buffer.GetForwardLimit() && !j.buffer.HasBytesAvailable(4) {
			if j.buffer.HasMoreDataComing() {
				_ = j.buffer.SetPos(startPos)
				return 0, ErrEndOfBuffer
			}
			return 0, j.syntaxError("Unterminated escape sequence")
		}
		// Equivalent to Integer.parseInt(stringPool.get(buffer, pos, 4), 16);
		result := 0
		for i := hexStart; i < hexEnd; i++ {
			c := j.buffer.ByteAt(i)
			result <<= 4
			if c >= '0' && c <= '9' {
				result += int(c - '0')
			} else if c >= 'a' && c <= 'f' {
				result += int(c - 'a' + 10)
			} else if c >= 'A' && c <= 'F' {
				result += int(c - 'A' + 10)
			} else {
				return 0, j.syntaxError("Malformed Unicode escape \\u" + string(j.buffer.PeekBytes(4)))
			}
		}
		_ = j.buffer.SetPos(hexEnd)
		return rune(result), nil
	case 't':
		return '\t', nil
	case 'b':
		return '\b', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 'f':
		return '\f', nil
	case '\n':
		j.lineNumber++
		j.lineStart = j.buffer.GetPos()
		fallthrough
	case '\'', '"', '\\', '/':
		return rune(escaped), nil
	default:
		// throw error when none of the above cases are matched
		return 0, j.syntaxError("Invalid escape sequence")
	}
}

// nextNonWhitespace returns the next character in the stream that is neither whitespace nor a part of a comment.
// When this returns, the returned character is always at {@code buffer[pos-1]}; this means the
// caller can always push back the returned character by decrementing {@code pos}.
func (j *jsonReader) nextNonWhitespace() (byte, error) {
	for {
		if !j.buffer.HasBytesAvailable(1) {
			if j.buffer.HasMoreDataComing() {
				return 0, ErrEndOfBuffer
			}
			break
		}

		c := j.buffer.ReadByte()
		if c == '\n' {
			j.lineNumber++
			j.lineStart = j.buffer.GetPos()
			continue
		} else if c == ' ' || c == '\r' || c == '\t' {
			continue
		}

		if c == '/' {
			if !j.buffer.HasBytesAvailable(1) {
				if j.buffer.HasMoreDataComing() {
					_ = j.buffer.Rewind(1) // push back '/' so it's still in the buffer when this method returns
					return 0, ErrEndOfBuffer
				}
				return c, nil
			}
			return 0, j.syntaxError("comment is not supported")
		} else if c == '#' {
			return 0, j.syntaxError("comment is not supported")
		} else {
			return c, nil
		}
	}
	return 0, j.getEOXError()
}

func (j *jsonReader) getEOXError() error {
	if j.buffer.HasMoreDataComing() {
		return ErrEndOfBuffer
	}
	return ErrEndOfFile
}

func (j *jsonReader) locationString() string {
	line := j.lineNumber + 1
	column := j.buffer.GetPos() - j.lineStart + 1
	return fmt.Sprintf("at line %d column %d path %s", line, column, j.GetPath())
}

func (j *jsonReader) unexpectedTokenError(expectedToken JsonToken) error {
	return j.unexpectedTokenErrorWithString(expectedToken.String())
}

func (j *jsonReader) unexpectedTokenErrorWithString(expectedToken string) error {
	p, _ := j.Peek()
	return fmt.Errorf("%w: expected %s but was %s %s", ErrUnexpectedToken, expectedToken, p, j.locationString())
}

func (j *jsonReader) syntaxError(message string) error {
	return fmt.Errorf("%w: %s %s", ErrSyntaxError, message, j.locationString())
}

var (
	nonLiteralChars = map[byte]bool{
		'{':  true,
		'}':  true,
		'[':  true,
		']':  true,
		':':  true,
		',':  true,
		' ':  true,
		'\t': true,
		'\f': true,
		'\r': true,
		'\n': true,
	}
)

func isLiteral(r byte) bool {
	return !nonLiteralChars[r]
}
