package provider

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/higress-group/proxy-wasm-go-sdk/proxywasm/types"
	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/higress-group/wasm-go/pkg/wrapper"
	"github.com/tidwall/gjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/bedrock"
	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/util"
)

const (
	httpPostMethod = "POST"
	awsService     = "bedrock"
	// bedrock-runtime.{awsRegion}.amazonaws.com
	bedrockDefaultDomain = "bedrock-runtime.%s.amazonaws.com"
	// converse 路径 /model/{modelId}/converse
	bedrockChatCompletionPath = "/model/%s/converse"
	// converseStream 路径 /model/{modelId}/converse-stream
	bedrockStreamChatCompletionPath = "/model/%s/converse-stream"
	// invoke_model 路径 /model/{modelId}/invoke
	bedrockInvokeModelPath = "/model/%s/invoke"
	// invoke_model_with_response_stream 路径 /model/{modelId}/invoke-with-response-stream
	bedrockStreamInvokeModelPath = "/model/%s/invoke-with-response-stream"
	bedrockSignedHeaders         = "host;x-amz-date"
	requestIdHeader              = "X-Amzn-Requestid"
	bedrockAnthropicVersion      = "bedrock-2023-05-31"
	headerAnthropicVersion       = "anthropic-version"
	headerAnthropicBeta          = "anthropic-beta"
	headerXAPIKey                = "x-api-key"
	bedrockLogRequestBodyConfig  = "bedrockLogRequestBody"

	ctxKeyBedrockJsonMode            = "bedrockJsonMode"
	ctxKeyBedrockCurrentToolUseIndex = "bedrockCurrentToolUseIndex"
)

type bedrockProviderInitializer struct{}

func (b *bedrockProviderInitializer) ValidateConfig(config *ProviderConfig) error {
	if len(config.awsAccessKey) == 0 || len(config.awsSecretKey) == 0 {
		return errors.New("missing bedrock access authentication parameters")
	}
	if len(config.awsRegion) == 0 {
		return errors.New("missing bedrock region parameters")
	}
	return nil
}

func (b *bedrockProviderInitializer) DefaultCapabilities() map[string]string {
	return map[string]string{
		string(ApiNameCompletion):     bedrockChatCompletionPath,
		string(ApiNameChatCompletion): bedrockChatCompletionPath,
		// Bedrock 侧的 Claude /v1/messages 原生入口走 invoke-model，而不是 converse。
		string(ApiNameAnthropicMessages): bedrockInvokeModelPath,
		string(ApiNameImageGeneration):   bedrockInvokeModelPath,
	}
}

func (b *bedrockProviderInitializer) CreateProvider(config ProviderConfig) (Provider, error) {
	config.setDefaultCapabilities(b.DefaultCapabilities())
	return &bedrockProvider{
		config:       config,
		contextCache: createContextCache(&config),
	}, nil
}

type bedrockProvider struct {
	config       ProviderConfig
	contextCache *contextCache
}

func (b *bedrockProvider) OnStreamingResponseBody(ctx wrapper.HttpContext, apiName ApiName, chunk []byte, isLastChunk bool) ([]byte, error) {
	switch apiName {
	case ApiNameAnthropicMessages:
		return b.onAnthropicMessagesStreamingResponseBody(ctx, chunk, isLastChunk)
	case ApiNameChatCompletion, ApiNameCompletion:
		return b.onBedrockConverseStreamingResponseBody(ctx, apiName, chunk)
	default:
		return chunk, nil
	}
}

func (b *bedrockProvider) onBedrockConverseStreamingResponseBody(ctx wrapper.HttpContext, apiName ApiName, chunk []byte) ([]byte, error) {
	var responseBuilder strings.Builder
	events := extractAmazonEventStreamEvents(ctx, chunk)
	if len(events) == 0 {
		doneEvent := StreamEvent{Data: streamEndDataValue}
		responseBuilder.WriteString(doneEvent.ToHttpString())
		return []byte(responseBuilder.String()), nil
	}

	for _, event := range events {
		outputEvent, err := b.convertEventFromBedrockToOpenAI(ctx, apiName, event)
		if err != nil {
			log.Errorf("[onStreamingResponseBody] failed to process streaming event: %v\n%s", err, chunk)
			return chunk, err
		}
		responseBuilder.WriteString(string(outputEvent))
	}
	return []byte(responseBuilder.String()), nil
}

func (b *bedrockProvider) onAnthropicMessagesStreamingResponseBody(ctx wrapper.HttpContext, chunk []byte, isLastChunk bool) ([]byte, error) {
	payloads := extractAmazonEventStreamPayloads(ctx, chunk)
	if len(payloads) == 0 {
		return nil, nil
	}

	var responseBuilder strings.Builder
	for _, payload := range payloads {
		decodedPayload, err := decodeBedrockAnthropicStreamPayload(payload)
		if err != nil {
			log.Warnf("bedrock failed to decode anthropic stream payload: %v", err)
			continue
		}
		logBedrockBodyDebug(ctx, "[bedrock] raw anthropic messages stream payload from bedrock: %s", decodedPayload)

		eventType := gjson.GetBytes(decodedPayload, "type").String()
		if eventType != "" {
			responseBuilder.WriteString("event: ")
			responseBuilder.WriteString(eventType)
			responseBuilder.WriteString("\n")
		}
		responseBuilder.WriteString("data: ")
		responseBuilder.Write(decodedPayload)
		responseBuilder.WriteString("\n\n")
	}
	return []byte(responseBuilder.String()), nil
}

func decodeBedrockAnthropicStreamPayload(payload []byte) ([]byte, error) {
	encodedBytes := gjson.GetBytes(payload, "bytes").String()
	if encodedBytes == "" {
		return payload, nil
	}
	return base64.StdEncoding.DecodeString(encodedBytes)
}

func (b *bedrockProvider) convertEventFromBedrockToOpenAI(ctx wrapper.HttpContext, apiName ApiName, bedrockEvent ConverseStreamEvent) ([]byte, error) {
	choices := make([]chatCompletionChoice, 0)
	chatChoice := &chatCompletionChoice{
		Delta: &chatMessage{},
	}
	if bedrockEvent.Role != nil {
		chatChoice.Delta.Role = *bedrockEvent.Role
	}

	// Handle start event
	if bedrockEvent.Start != nil {
		chatChoice.Delta.Content = nil
		// Handle tool use start
		if bedrockEvent.Start.ToolUse != nil {
			currentToolUseIndex, _ := ctx.GetContext(ctxKeyBedrockCurrentToolUseIndex).(int)
			currentToolUseIndex++
			ctx.SetContext(ctxKeyBedrockCurrentToolUseIndex, currentToolUseIndex)
			toolIndex := currentToolUseIndex

			// Restore original tool name (reverse normalization done during request transformation)
			responseToolName := bedrock.GetOriginalToolName(bedrockEvent.Start.ToolUse.Name)
			chatChoice.Delta.ToolCalls = []toolCall{
				{
					Index: toolIndex,
					Id:    bedrockEvent.Start.ToolUse.ToolUseID,
					Type:  "function",
					Function: functionCall{
						Name:      responseToolName,
						Arguments: "",
					},
				},
			}
		}

		fillBedrockDeltaReasoningContentToChoice(chatChoice, bedrockEvent.Start.ReasoningContent)
	}

	// Handle delta event
	if bedrockEvent.Delta != nil {
		chatChoice.Delta = &chatMessage{Content: bedrockEvent.Delta.Text}
		// Handle tool use delta
		if bedrockEvent.Delta.ToolUse != nil {
			currentToolUseIndex, _ := ctx.GetContext(ctxKeyBedrockCurrentToolUseIndex).(int)
			chatChoice.Delta.ToolCalls = []toolCall{
				{
					Index: currentToolUseIndex,
					Type:  "function",
					Function: functionCall{
						Arguments: bedrockEvent.Delta.ToolUse.Input,
					},
				},
			}
		}
		// Handle reasoning content delta (thinking blocks)
		fillBedrockDeltaReasoningContentToChoice(chatChoice, bedrockEvent.Delta.ReasoningContent)
	}

	if bedrockEvent.StopReason != nil {
		chatChoice.FinishReason = util.Ptr(util.MapFinishReason(*bedrockEvent.StopReason))
	}
	choices = append(choices, *chatChoice)
	requestId := ctx.GetStringContext(requestIdHeader, "")
	openAIFormattedChunk := &chatCompletionResponse{
		Id:                requestId,
		Created:           time.Now().UnixMilli() / 1000,
		Model:             ctx.GetStringContext(ctxKeyFinalRequestModel, ""),
		SystemFingerprint: "",
		Object:            objectChatCompletionChunk,
		Choices:           choices,
	}
	if bedrockEvent.Usage != nil {
		openAIFormattedChunk.Choices = choices[:0]
	}

	openAIFormattedChunkBytes, _ := json.Marshal(openAIFormattedChunk)
	if bedrockEvent.Usage != nil {
		var err error
		openAIFormattedChunkBytes, err = bedrock.SetBedrockUsageFieldsToResponse(
			openAIFormattedChunkBytes, bedrock.TransformUsageToOpenAIFormat(bedrockEvent.Usage))
		if err != nil {
			log.Errorf("[onStreamingResponseBody] failed to set Bedrock usage fields: %v", err)
			return nil, err
		}
	}

	chunkData := string(openAIFormattedChunkBytes)

	if apiName == ApiNameCompletion {
		chunkData = transformCompletionsStreamingData(chunkData)
	}

	var openAIChunk strings.Builder
	openAIChunk.WriteString(ssePrefix)
	openAIChunk.WriteString(chunkData)
	openAIChunk.WriteString("\n\n")
	return []byte(openAIChunk.String()), nil
}

func fillBedrockDeltaReasoningContentToChoice(choice *chatCompletionChoice, reasoningContent *bedrock.ConverseReasoningContentBlockDelta) {
	if reasoningContent == nil {
		return
	}

	choice.Delta.ReasoningContent = reasoningContent.Text
	choice.Delta.ProviderSpecificFields = map[string]any{
		// NOTE: use `reasoningContent` instead of `reasoningContentBlocks`(non-stream use this field name)
		// to keep consistency with litellm's implementation
		"reasoningContent": reasoningContent,
	}
	thinkingBlocks := bedrock.TranslateConverseReasoningContentBlockDelta(reasoningContent)
	if thinkingBlocks != nil {
		choice.Delta.ThinkingBlocks = thinkingBlocks
	}
}

type ConverseStreamEvent struct {
	ContentBlockIndex int                             `json:"contentBlockIndex,omitempty"`
	Delta             *bedrock.ContentBlockDeltaEvent `json:"delta,omitempty"`
	Role              *string                         `json:"role,omitempty"`
	StopReason        *string                         `json:"stopReason,omitempty"`
	Usage             *bedrock.TokenUsageBlock        `json:"usage,omitempty"`
	Start             *bedrock.ContentBlockStartEvent `json:"start,omitempty"`
}

type bedrockImageGenerationResponse struct {
	Images []string `json:"images"`
	Error  string   `json:"error"`
}

type bedrockImageGenerationTextToImageParams struct {
	Text            string  `json:"text"`
	NegativeText    string  `json:"negativeText,omitempty"`
	ConditionImage  string  `json:"conditionImage,omitempty"`
	ControlMode     string  `json:"controlMode,omitempty"`
	ControlStrength float32 `json:"controlLength,omitempty"`
}

type bedrockImageGenerationConfig struct {
	Width          int     `json:"width"`
	Height         int     `json:"height"`
	Quality        string  `json:"quality,omitempty"`
	CfgScale       float32 `json:"cfgScale,omitempty"`
	Seed           int     `json:"seed,omitempty"`
	NumberOfImages int     `json:"numberOfImages"`
}

type bedrockImageGenerationColorGuidedGenerationParams struct {
	Colors         []string `json:"colors"`
	ReferenceImage string   `json:"referenceImage"`
	Text           string   `json:"text"`
	NegativeText   string   `json:"negativeText,omitempty"`
}

type bedrockImageGenerationImageVariationParams struct {
	Images             []string `json:"images"`
	SimilarityStrength float32  `json:"similarityStrength"`
	Text               string   `json:"text"`
	NegativeText       string   `json:"negativeText,omitempty"`
}

type bedrockImageGenerationInPaintingParams struct {
	Image        string `json:"image"`
	MaskPrompt   string `json:"maskPrompt"`
	MaskImage    string `json:"maskImage"`
	Text         string `json:"text"`
	NegativeText string `json:"negativeText,omitempty"`
}

type bedrockImageGenerationOutPaintingParams struct {
	Image           string `json:"image"`
	MaskPrompt      string `json:"maskPrompt"`
	MaskImage       string `json:"maskImage"`
	OutPaintingMode string `json:"outPaintingMode"`
	Text            string `json:"text"`
	NegativeText    string `json:"negativeText,omitempty"`
}

type bedrockImageGenerationBackgroundRemovalParams struct {
	Image string `json:"image"`
}

type bedrockImageGenerationRequest struct {
	TaskType                    string                                             `json:"taskType"`
	ImageGenerationConfig       *bedrockImageGenerationConfig                      `json:"imageGenerationConfig"`
	TextToImageParams           *bedrockImageGenerationTextToImageParams           `json:"textToImageParams,omitempty"`
	ColorGuidedGenerationParams *bedrockImageGenerationColorGuidedGenerationParams `json:"colorGuidedGenerationParams,omitempty"`
	ImageVariationParams        *bedrockImageGenerationImageVariationParams        `json:"imageVariationParams,omitempty"`
	InPaintingParams            *bedrockImageGenerationInPaintingParams            `json:"inPaintingParams,omitempty"`
	OutPaintingParams           *bedrockImageGenerationOutPaintingParams           `json:"outPaintingParams,omitempty"`
	BackgroundRemovalParams     *bedrockImageGenerationBackgroundRemovalParams     `json:"backgroundRemovalParams,omitempty"`
}

type bedrockAnthropicMessagesRequest struct {
	AnthropicVersion string            `json:"anthropic_version"`
	AnthropicBeta    []string          `json:"anthropic_beta,omitempty"`
	anthropicMessagesCommonFields
}

func extractAmazonEventStreamEvents(ctx wrapper.HttpContext, chunk []byte) []ConverseStreamEvent {
	payloads := extractAmazonEventStreamPayloads(ctx, chunk)
	var events []ConverseStreamEvent
	for _, payload := range payloads {
		var event ConverseStreamEvent
		if err := json.Unmarshal(payload, &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}

func extractAmazonEventStreamPayloads(ctx wrapper.HttpContext, chunk []byte) [][]byte {
	body := chunk
	if bufferedStreamingBody, has := ctx.GetContext(ctxKeyStreamingBody).([]byte); has {
		body = append(bufferedStreamingBody, chunk...)
	}

	r := bytes.NewReader(body)
	var payloads [][]byte
	var lastRead int64 = 0
	messageBuffer := make([]byte, 1024)
	defer func() {
		log.Debugf("extractAmazonEventStreamEvents: lastRead=%d, r.Size=%d", lastRead, r.Size())
	}()

	for {
		msg, err := decodeMessage(r, messageBuffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Warnf("bedrock failed to decode message: %v, lastRead=%d, r.Size=%d", err, lastRead, r.Size())
			break
		}
		payloads = append(payloads, append([]byte(nil), msg.Payload...))
		lastRead = r.Size() - int64(r.Len())
	}
	if lastRead < int64(len(body)) {
		ctx.SetContext(ctxKeyStreamingBody, body[lastRead:])
	} else {
		ctx.SetContext(ctxKeyStreamingBody, nil)
	}
	return payloads
}

type bedrockStreamMessage struct {
	Headers headers
	Payload []byte
}

type EventFrame struct {
	TotalLength   uint32
	HeadersLength uint32
	PreludeCRC    uint32
	Headers       map[string]interface{}
	Payload       []byte
	PayloadCRC    uint32
}

type headers []header

type header struct {
	Name  string
	Value Value
}

func (hs *headers) Set(name string, value Value) {
	var i int
	for ; i < len(*hs); i++ {
		if (*hs)[i].Name == name {
			(*hs)[i].Value = value
			return
		}
	}

	*hs = append(*hs, header{
		Name: name, Value: value,
	})
}

func decodeMessage(reader io.Reader, payloadBuf []byte) (m bedrockStreamMessage, err error) {
	crc := crc32.New(crc32.MakeTable(crc32.IEEE))
	hashReader := io.TeeReader(reader, crc)

	prelude, err := decodePrelude(hashReader, crc)
	if err != nil {
		return bedrockStreamMessage{}, err
	}

	if prelude.HeadersLen > 0 {
		lr := io.LimitReader(hashReader, int64(prelude.HeadersLen))
		m.Headers, err = decodeHeaders(lr)
		if err != nil {
			return bedrockStreamMessage{}, err
		}
	}

	if payloadLen := prelude.PayloadLen(); payloadLen > 0 {
		buf, err := decodePayload(payloadBuf, io.LimitReader(hashReader, int64(payloadLen)))
		if err != nil {
			return bedrockStreamMessage{}, err
		}
		m.Payload = buf
	}

	msgCRC := crc.Sum32()
	if err := validateCRC(reader, msgCRC); err != nil {
		return bedrockStreamMessage{}, err
	}

	return m, nil
}

func decodeHeaders(r io.Reader) (headers, error) {
	hs := headers{}

	for {
		name, err := decodeHeaderName(r)
		if err != nil {
			if err == io.EOF {
				// EOF while getting header name means no more headers
				break
			}
			return nil, err
		}

		value, err := decodeHeaderValue(r)
		if err != nil {
			return nil, err
		}

		hs.Set(name, value)
	}

	return hs, nil
}

func decodeHeaderValue(r io.Reader) (Value, error) {
	var raw rawValue

	typ, err := decodeUint8(r)
	if err != nil {
		return nil, err
	}
	raw.Type = valueType(typ)

	var v Value

	switch raw.Type {
	case stringValueType:
		var tv StringValue
		err = tv.decode(r)
		v = tv
	default:
		log.Errorf("unknown value type %d", raw.Type)
	}

	// Error could be EOF, let caller deal with it
	return v, err
}

type Value interface {
	Get() interface{}
}

type StringValue string

func (v StringValue) Get() interface{} {
	return string(v)
}

func (v *StringValue) decode(r io.Reader) error {
	s, err := decodeStringValue(r)
	if err != nil {
		return err
	}

	*v = StringValue(s)
	return nil
}

func decodeBytesValue(r io.Reader) ([]byte, error) {
	var raw rawValue
	var err error
	raw.Len, err = decodeUint16(r)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, raw.Len)
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return nil, err
	}

	return buf, nil
}

func decodeUint16(r io.Reader) (uint16, error) {
	var b [2]byte
	bs := b[:]
	_, err := io.ReadFull(r, bs)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(bs), nil
}

func decodeStringValue(r io.Reader) (string, error) {
	v, err := decodeBytesValue(r)
	return string(v), err
}

type rawValue struct {
	Type  valueType
	Len   uint16 // Only set for variable length slices
	Value []byte // byte representation of value, BigEndian encoding.
}

type valueType uint8

const (
	trueValueType valueType = iota
	falseValueType
	int8ValueType  // Byte
	int16ValueType // Short
	int32ValueType // Integer
	int64ValueType // Long
	bytesValueType
	stringValueType
	timestampValueType
	uuidValueType
)

func decodeHeaderName(r io.Reader) (string, error) {
	var n headerName

	var err error
	n.Len, err = decodeUint8(r)
	if err != nil {
		return "", err
	}

	name := n.Name[:n.Len]
	if _, err := io.ReadFull(r, name); err != nil {
		return "", err
	}

	return string(name), nil
}

func decodeUint8(r io.Reader) (uint8, error) {
	type byteReader interface {
		ReadByte() (byte, error)
	}

	if br, ok := r.(byteReader); ok {
		v, err := br.ReadByte()
		return v, err
	}

	var b [1]byte
	_, err := io.ReadFull(r, b[:])
	return b[0], err
}

const maxHeaderNameLen = 255

type headerName struct {
	Len  uint8
	Name [maxHeaderNameLen]byte
}

func decodePayload(buf []byte, r io.Reader) ([]byte, error) {
	w := bytes.NewBuffer(buf[0:0])

	_, err := io.Copy(w, r)
	return w.Bytes(), err
}

type messagePrelude struct {
	Length     uint32
	HeadersLen uint32
	PreludeCRC uint32
}

func (p messagePrelude) ValidateLens() error {
	if p.Length == 0 {
		return fmt.Errorf("message prelude want: 16, have: %v", int(p.Length))
	}
	return nil
}

func (p messagePrelude) PayloadLen() uint32 {
	return p.Length - p.HeadersLen - 16
}

func decodePrelude(r io.Reader, crc hash.Hash32) (messagePrelude, error) {
	var p messagePrelude

	var err error
	p.Length, err = decodeUint32(r)
	if err != nil {
		return messagePrelude{}, err
	}

	p.HeadersLen, err = decodeUint32(r)
	if err != nil {
		return messagePrelude{}, err
	}

	if err := p.ValidateLens(); err != nil {
		return messagePrelude{}, err
	}

	preludeCRC := crc.Sum32()
	if err := validateCRC(r, preludeCRC); err != nil {
		return messagePrelude{}, err
	}

	p.PreludeCRC = preludeCRC

	return p, nil
}

func decodeUint32(r io.Reader) (uint32, error) {
	var b [4]byte
	bs := b[:]
	_, err := io.ReadFull(r, bs)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(bs), nil
}

func validateCRC(r io.Reader, expect uint32) error {
	msgCRC, err := decodeUint32(r)
	if err != nil {
		return err
	}

	if msgCRC != expect {
		return fmt.Errorf("message checksum mismatch")
	}

	return nil
}

func (b *bedrockProvider) TransformResponseHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	ctx.SetContext(requestIdHeader, headers.Get(requestIdHeader))
	if headers.Get("Content-Type") == "application/vnd.amazon.eventstream" {
		headers.Set("Content-Type", "text/event-stream; charset=utf-8")
	}
	headers.Del("Content-Length")

	if util.IsFakeStream(ctx) {
		headers.Set("Content-Type", "text/event-stream; charset=utf-8")
		log.Debugf("[bedrock] set response content-type to text/event-stream for fake streaming")
	}
}

func (b *bedrockProvider) GetProviderType() string {
	return providerTypeBedrock
}

func (b *bedrockProvider) OnRequestHeaders(ctx wrapper.HttpContext, apiName ApiName) error {
	b.config.handleRequestHeaders(b, ctx, apiName)
	return nil
}

func (b *bedrockProvider) TransformRequestHeaders(ctx wrapper.HttpContext, apiName ApiName, headers http.Header) {
	util.OverwriteRequestHostHeader(headers, fmt.Sprintf(bedrockDefaultDomain, b.config.awsRegion))
}

func (b *bedrockProvider) OnRequestBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) (types.Action, error) {
	if !b.config.isSupportedAPI(apiName) {
		return types.ActionContinue, errUnsupportedApiName
	}
	return b.config.handleRequestBody(b, b.contextCache, ctx, apiName, body)
}

func (b *bedrockProvider) TransformRequestBodyHeaders(ctx wrapper.HttpContext, apiName ApiName, body []byte, headers http.Header) ([]byte, error) {
	switch apiName {
	case ApiNameCompletion:
		var err error
		body, err = translateCompletionRequestToChatCompletionRequest(body)
		if err != nil {
			return nil, err
		}
		return b.onChatCompletionRequestBody(ctx, body, headers)
	case ApiNameChatCompletion:
		return b.onChatCompletionRequestBody(ctx, body, headers)
	case ApiNameAnthropicMessages:
		return b.onAnthropicMessagesRequestBody(ctx, body, headers)
	case ApiNameImageGeneration:
		return b.onImageGenerationRequestBody(ctx, body, headers)
	default:
		return b.config.defaultTransformRequestBody(ctx, apiName, body)
	}
}

func (b *bedrockProvider) TransformResponseBody(ctx wrapper.HttpContext, apiName ApiName, body []byte) ([]byte, error) {
	switch apiName {
	case ApiNameCompletion:
		bodyBytes, err := b.onChatCompletionResponseBody(ctx, body)
		if err != nil {
			return nil, err
		}
		// transform to completion response format
		return transformCompletionsResponseFields(ctx, bodyBytes)
	case ApiNameChatCompletion:
		return b.onChatCompletionResponseBody(ctx, body)
	case ApiNameAnthropicMessages:
		logBedrockBodyDebug(ctx, "[bedrock] raw anthropic messages response body from bedrock: %s", body)
		return body, nil
	case ApiNameImageGeneration:
		return b.onImageGenerationResponseBody(ctx, body)
	}
	return nil, errUnsupportedApiName
}

func (b *bedrockProvider) onAnthropicMessagesRequestBody(ctx wrapper.HttpContext, body []byte, headers http.Header) ([]byte, error) {
	model := gjson.GetBytes(body, "model").String()
	if model == "" {
		return nil, util.BadRequest("missing model")
	}
	ctx.SetContext(ctxKeyOriginalRequestModel, model)

	mappedModel := getMappedModel(model, b.config.modelMapping)
	ctx.SetContext(ctxKeyFinalRequestModel, mappedModel)

	streaming := gjson.GetBytes(body, "stream").Bool()
	ctx.SetContext(ctxKeyIsStreaming, streaming)

	var err error
	body, err = setBedrockAnthropicMessagesRequestDefaults(body, headers)
	if err != nil {
		return nil, err
	}

	// Bedrock 原生 Messages 入口要求使用 invoke / invoke-with-response-stream 路径，
	// 并且请求体里使用 anthropic_version，而不是直接保留 model / stream 字段。
	headers.Set("Accept", "*/*")
	if streaming {
		b.overwriteRequestPathHeader(headers, bedrockStreamInvokeModelPath, mappedModel)
	} else {
		b.overwriteRequestPathHeader(headers, bedrockInvokeModelPath, mappedModel)
	}
	logBedrockBodyDebug(ctx, "[bedrock] anthropic messages request body: %s", body)
	b.setAuthHeaders(body, headers)
	return body, nil
}

func setBedrockAnthropicMessagesRequestDefaults(body []byte, headers http.Header) ([]byte, error) {
	var request anthropicMessagesRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return nil, err
	}

	// 这里显式构造 Bedrock native messages request，
	// 只保留这条协议真正需要的字段，避免 map[string]any 带来的 key 漏改/漏删问题。
	rebuilt := bedrockAnthropicMessagesRequest{
		anthropicMessagesCommonFields: anthropicMessagesCommonFields{
			Messages:      request.Messages,
			System:        request.System,
			MaxTokens:     request.MaxTokens,
			StopSequences: request.StopSequences,
			Temperature:   request.Temperature,
			TopP:          request.TopP,
			TopK:          request.TopK,
			ToolChoice:    request.ToolChoice,
			Tools:         request.Tools,
			ServiceTier:   request.ServiceTier,
			Thinking:      request.Thinking,
		},
		AnthropicVersion: bedrockAnthropicVersion,
		AnthropicBeta:    request.AnthropicBeta,
	}

	if betaHeader := headers.Get(headerAnthropicBeta); betaHeader != "" {
		betas := make([]string, 0)
		for _, value := range strings.Split(betaHeader, ",") {
			value = strings.TrimSpace(value)
			if value != "" {
				betas = append(betas, value)
			}
		}
		if len(betas) > 0 {
			rebuilt.AnthropicBeta = betas
		}
	}

	headers.Del(headerAnthropicVersion)
	headers.Del(headerAnthropicBeta)
	headers.Del(headerXAPIKey)
	return json.Marshal(&rebuilt)
}

func logBedrockBodyDebug(ctx wrapper.HttpContext, format string, body []byte) {
	if !ctx.GetBoolGlobalConfig(bedrockLogRequestBodyConfig, false) {
		return
	}
	log.Debugf(format, string(util.CompactJSONBytes(body)))
}

func (b *bedrockProvider) onImageGenerationResponseBody(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	bedrockResponse := &bedrockImageGenerationResponse{}
	if err := json.Unmarshal(body, bedrockResponse); err != nil {
		log.Errorf("unable to unmarshal bedrock image gerneration response: %v", err)
		return nil, fmt.Errorf("unable to unmarshal bedrock image generation response: %v", err)
	}
	response := b.buildBedrockImageGenerationResponse(ctx, bedrockResponse)
	return json.Marshal(response)
}

func (b *bedrockProvider) onImageGenerationRequestBody(ctx wrapper.HttpContext, body []byte, headers http.Header) ([]byte, error) {
	request := &imageGenerationRequest{}
	err := b.config.parseRequestAndMapModel(ctx, request, body)
	if err != nil {
		return nil, err
	}
	headers.Set("Accept", "*/*")
	b.overwriteRequestPathHeader(headers, bedrockInvokeModelPath, request.Model)
	return b.buildBedrockImageGenerationRequest(request, headers)
}

func (b *bedrockProvider) buildBedrockImageGenerationRequest(origRequest *imageGenerationRequest, headers http.Header) ([]byte, error) {
	width, height := 1024, 1024
	pairs := strings.Split(origRequest.Size, "x")
	if len(pairs) == 2 {
		width, _ = strconv.Atoi(pairs[0])
		height, _ = strconv.Atoi(pairs[1])
	}

	request := &bedrockImageGenerationRequest{
		TaskType: "TEXT_IMAGE",
		TextToImageParams: &bedrockImageGenerationTextToImageParams{
			Text: origRequest.Prompt,
		},
		ImageGenerationConfig: &bedrockImageGenerationConfig{
			NumberOfImages: origRequest.N,
			Width:          width,
			Height:         height,
			Quality:        origRequest.Quality,
		},
	}
	requestBytes, err := json.Marshal(request)
	b.setAuthHeaders(requestBytes, headers)
	return requestBytes, err
}

func (b *bedrockProvider) buildBedrockImageGenerationResponse(ctx wrapper.HttpContext, bedrockResponse *bedrockImageGenerationResponse) *imageGenerationResponse {
	data := make([]imageGenerationData, len(bedrockResponse.Images))
	for i, image := range bedrockResponse.Images {
		data[i] = imageGenerationData{
			B64: image,
		}
	}
	return &imageGenerationResponse{
		Created: time.Now().UnixMilli() / 1000,
		Data:    data,
	}
}

func (b *bedrockProvider) onChatCompletionResponseBody(ctx wrapper.HttpContext, body []byte) ([]byte, error) {
	logBedrockBodyDebug(ctx, "[onChatCompletionResponseBody] bedrock converse response body before transformation: %s", body)
	jsonMode, _ := ctx.GetContext(ctxKeyBedrockJsonMode).(bool)
	fakeStream := util.IsFakeStream(ctx)
	log.Debugf("[onChatCompletionResponseBody] bedrock jsonMode: %v, fakeStream: %v", jsonMode, fakeStream)

	requestId := ctx.GetStringContext(requestIdHeader, "")
	modelId, _ := url.QueryUnescape(ctx.GetStringContext(ctxKeyFinalRequestModel, ""))
	response, responseUsage, err := transformBedrockConverseResponse(body,
		&bedrock.TransformResponseOptions{JSONMode: jsonMode, RequestID: requestId, Model: modelId})
	if err != nil {
		log.Errorf("failed to transform bedrock converse response: %v", err)
		return nil, fmt.Errorf("failed to transform bedrock converse response: %v", err)
	}

	if fakeStream {
		return toBedrockFakeStreamResponse(response, responseUsage)
	}

	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Errorf("failed to marshal transformed bedrock converse response: %v", err)
		return nil, fmt.Errorf("failed to marshal transformed bedrock converse response: %v", err)
	}

	responseBytes, err = bedrock.SetBedrockUsageFieldsToResponse(responseBytes, responseUsage)
	if err != nil {
		return nil, err
	}

	logBedrockBodyDebug(ctx, "bedrock converse response body after transformation: %s", responseBytes)
	return responseBytes, nil
}

func (b *bedrockProvider) onChatCompletionRequestBody(ctx wrapper.HttpContext, body []byte, headers http.Header) ([]byte, error) {
	request := &chatCompletionRequest{}
	err := b.config.parseRequestAndMapModel(ctx, request, body)
	if err != nil {
		return nil, err
	}

	streaming := request.Stream
	headers.Set("Accept", "*/*")
	if streaming {
		b.overwriteRequestPathHeader(headers, bedrockStreamChatCompletionPath, request.Model)
	} else {
		b.overwriteRequestPathHeader(headers, bedrockChatCompletionPath, request.Model)
	}

	extendedParams, err := extractBedrockExtendedParams(body)
	if err != nil {
		return nil, err
	}
	return b.buildBedrockConverseRequest(ctx, request, extendedParams, headers)
}

func (b *bedrockProvider) buildBedrockConverseRequest(ctx wrapper.HttpContext,
	origRequest *chatCompletionRequest, extendedParams *bedrockExtendedParams, headers http.Header) ([]byte, error) {
	request, jsonMode, err := transformToBedrockConverseRequest(origRequest, &bedrock.TransformRequestOptions{}, extendedParams)
	if err != nil {
		return nil, err
	}

	// Store JSON mode status in context for response processing
	ctx.SetContext(ctxKeyBedrockJsonMode, jsonMode)

	if jsonMode && origRequest.Stream {
		util.SetFakeStream(ctx)
		b.overwriteRequestPathHeader(headers, bedrockChatCompletionPath, origRequest.Model)
		log.Debugf("[bedrock] use fake streaming")
	}

	requestBytes, err := json.Marshal(request)

	logBedrockBodyDebug(ctx, "[bedrock] converse request body: %s", requestBytes)

	if extendedParams.ExtraHeaders != nil {
		setBedrockExtraHeaders(headers, extendedParams.ExtraHeaders)
		log.Debugf("set extra headers on request: %+v", extendedParams.ExtraHeaders)
	}

	b.setAuthHeaders(requestBytes, headers)
	return requestBytes, err
}

func setBedrockExtraHeaders(headers http.Header, extraHeaders map[string]string) {
	for k, v := range extraHeaders {
		headers.Set(k, v)
	}
}

func (b *bedrockProvider) overwriteRequestPathHeader(headers http.Header, format, model string) {
	modelInPath := model
	// Just in case the model name has already been URL-escaped, we shouldn't escape it again.
	if !strings.ContainsRune(model, '%') {
		modelInPath = url.QueryEscape(model)
	}
	path := fmt.Sprintf(format, modelInPath)
	log.Debugf("overwriting bedrock request path: %s", path)
	util.OverwriteRequestPathHeader(headers, path)
}

func (b *bedrockProvider) setAuthHeaders(body []byte, headers http.Header) {
	t := time.Now().UTC()
	amzDate := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")
	path := headers.Get(":path")
	signature := b.generateSignature(path, amzDate, dateStamp, body)
	headers.Set("X-Amz-Date", amzDate)
	util.OverwriteRequestAuthorizationHeader(headers, fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s/%s/%s/aws4_request, SignedHeaders=%s, Signature=%s", b.config.awsAccessKey, dateStamp, b.config.awsRegion, awsService, bedrockSignedHeaders, signature))
}

func (b *bedrockProvider) generateSignature(path, amzDate, dateStamp string, body []byte) string {
	path = encodeSigV4Path(path)
	hashedPayload := sha256Hex(body)

	endpoint := fmt.Sprintf(bedrockDefaultDomain, b.config.awsRegion)
	canonicalHeaders := fmt.Sprintf("host:%s\nx-amz-date:%s\n", endpoint, amzDate)
	canonicalRequest := fmt.Sprintf("%s\n%s\n\n%s\n%s\n%s",
		httpPostMethod, path, canonicalHeaders, bedrockSignedHeaders, hashedPayload)

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, b.config.awsRegion, awsService)
	hashedCanonReq := sha256Hex([]byte(canonicalRequest))
	stringToSign := fmt.Sprintf("AWS4-HMAC-SHA256\n%s\n%s\n%s",
		amzDate, credentialScope, hashedCanonReq)

	signingKey := getSignatureKey(b.config.awsSecretKey, dateStamp, b.config.awsRegion, awsService)
	signature := hmacHex(signingKey, stringToSign)
	return signature
}

func encodeSigV4Path(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

func getSignatureKey(key, dateStamp, region, service string) []byte {
	kDate := hmacSha256([]byte("AWS4"+key), dateStamp)
	kRegion := hmacSha256(kDate, region)
	kService := hmacSha256(kRegion, service)
	kSigning := hmacSha256(kService, "aws4_request")
	return kSigning
}

func hmacSha256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}

func sha256Hex(data []byte) string {
	h := sha256.New()
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func hmacHex(key []byte, data string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return hex.EncodeToString(h.Sum(nil))
}
