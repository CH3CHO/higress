package main

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// jsonObjectBuilder/jsonArrayBuilder 用于按预算增量构造响应日志摘要，
// 避免在裁剪长 body 时多次重建整段 JSON 字符串。
const (
	// 先用一个有限的首选截断长度做快速尝试，避免对超长文本直接做大范围二分。
	maxPreferredTruncationTextLen = 2048
	// 二分时至少尝试保留 1 个字符；0 长度由上游直接视为不可保留。
	minTruncatedTextLen = 1
	// 预算里需要始终为末尾的 `}` / `]` 预留 1 个字符。
	jsonObjectClosingDelimiterLen = 1
	jsonArrayClosingDelimiterLen  = 1
	// JSON 对象字段名前缀固定是 `"name":`，除去字段名本身，还会多占 3 个字符。
	jsonObjectFieldSyntaxOverheadLen = 3
	// 数组或对象在追加后续元素/字段时，需要额外预留 1 个逗号。
	jsonValueSeparatorLen = 1
	// 截断后缀用于明确标识日志内容被裁剪。
	truncatedMarker = " [truncated]"
)

func appendTruncatedJSONResultFieldForLog(builder *jsonObjectBuilder, name string, value gjson.Result, budget int) {
	if builder == nil || !value.Exists() {
		return
	}

	maxEncodedLen := builder.ExtraValueCapacity(name, budget)
	if maxEncodedLen <= 0 {
		return
	}

	text := value.String()
	truncatedValue := buildTruncatedJSONStringForLog(text, minInt(len(text), maxPreferredTruncationTextLen), maxEncodedLen)
	if len(truncatedValue) == 0 {
		// 空字符串和预算不足都统一视为“可省略字段”：
		// 摘要日志优先保留有信息量的文本内容，不专门为 `""` 预留空间。
		return
	}

	builder.AddEncodedField(name, truncatedValue)
}

func buildTruncatedJSONStringForLog(value string, preferredLimit int, maxEncodedLen int) []byte {
	if preferredLimit <= 0 || maxEncodedLen <= 0 {
		return nil
	}

	if preferredLimit > len(value) {
		preferredLimit = len(value)
	}
	if preferredLimit <= 0 {
		return nil
	}

	if encodedValue := marshalTruncatedStringForLog(value, preferredLimit); len(encodedValue) > 0 && len(encodedValue) <= maxEncodedLen {
		return encodedValue
	}

	left, right := minTruncatedTextLen, preferredLimit
	var best []byte
	for left <= right {
		mid := left + (right-left)/2
		encodedValue := marshalTruncatedStringForLog(value, mid)
		if len(encodedValue) > 0 && len(encodedValue) <= maxEncodedLen {
			best = encodedValue
			left = mid + 1
		} else {
			right = mid - 1
		}
	}

	return best
}

func marshalTruncatedStringForLog(value string, limit int) []byte {
	truncatedValue := truncateStringForLog(value, limit)
	encodedValue, err := json.Marshal(truncatedValue)
	if err != nil {
		return nil
	}
	return encodedValue
}

func truncateStringForLog(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}

	if limit <= len(truncatedMarker) {
		return value[:limit]
	}

	return value[:limit-len(truncatedMarker)] + truncatedMarker
}

type jsonObjectBuilder struct {
	builder  strings.Builder
	hasField bool
}

func newJSONObjectBuilder() *jsonObjectBuilder {
	builder := &jsonObjectBuilder{}
	builder.builder.WriteByte('{')
	return builder
}

func (b *jsonObjectBuilder) AddResultField(name string, value gjson.Result) {
	if !value.Exists() {
		return
	}
	b.AddRawField(name, value.Raw)
}

func (b *jsonObjectBuilder) AddResultFieldIfFits(name string, value gjson.Result, budget int) bool {
	if !value.Exists() {
		return false
	}
	if b.ExtraValueCapacity(name, budget) < len(value.Raw) {
		return false
	}
	b.AddRawField(name, value.Raw)
	return true
}

func (b *jsonObjectBuilder) AddRawField(name string, raw string) {
	if b.hasField {
		b.builder.WriteByte(',')
	}
	b.builder.WriteByte('"')
	b.builder.WriteString(name)
	b.builder.WriteString(`":`)
	b.builder.WriteString(raw)
	b.hasField = true
}

func (b *jsonObjectBuilder) AddEncodedField(name string, encodedValue []byte) {
	if len(encodedValue) == 0 {
		return
	}
	if b.hasField {
		b.builder.WriteByte(',')
	}
	b.builder.WriteByte('"')
	b.builder.WriteString(name)
	b.builder.WriteString(`":`)
	b.builder.Write(encodedValue)
	b.hasField = true
}

func (b *jsonObjectBuilder) HasField() bool {
	return b.hasField
}

func (b *jsonObjectBuilder) ExtraValueCapacity(name string, budget int) int {
	remaining := budget - b.builder.Len() - jsonObjectClosingDelimiterLen - b.fieldPrefixLen(name)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (b *jsonObjectBuilder) String() string {
	return b.builder.String() + "}"
}

func (b *jsonObjectBuilder) fieldPrefixLen(name string) int {
	prefixLen := len(name) + jsonObjectFieldSyntaxOverheadLen
	if b.hasField {
		prefixLen += jsonValueSeparatorLen
	}
	return prefixLen
}

type jsonArrayBuilder struct {
	builder  strings.Builder
	hasValue bool
}

func newJSONArrayBuilder() *jsonArrayBuilder {
	builder := &jsonArrayBuilder{}
	builder.builder.WriteByte('[')
	return builder
}

func (b *jsonArrayBuilder) AddRawValue(raw string) {
	if b.hasValue {
		b.builder.WriteByte(',')
	}
	b.builder.WriteString(raw)
	b.hasValue = true
}

func (b *jsonArrayBuilder) HasValue() bool {
	return b.hasValue
}

func (b *jsonArrayBuilder) ExtraValueCapacity(budget int) int {
	remaining := budget - b.builder.Len() - jsonArrayClosingDelimiterLen
	if b.hasValue {
		remaining -= jsonValueSeparatorLen
	}
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (b *jsonArrayBuilder) String() string {
	return b.builder.String() + "]"
}
