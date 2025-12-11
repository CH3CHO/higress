package provider

import (
	"encoding/json"
	"fmt"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/vertex"
)

type vertexExtendedParams struct {
	ThinkingConfig     *vertex.ThinkingConfig
	tools              []*vertexExtendedTool
	functions          []*vertexFunction
	ImageConfig        *vertex.ImageConfig
	ResponseSchema     map[string]interface{}
	ResponseMimeType   string
	ResponseLogprobs   *bool
	MaxOutputTokens    int
	CandidateCount     int
	StopSequences      []string
	ResponseModalities []string
	Thinking           *vertex.ClaudeStyleThinking
	CachedContent      string
	SafetySettings     []*vertex.SafetySettingsConfig
}

func extractVertexExtendedParams(reqBody []byte) (*vertexExtendedParams, error) {
	tc, err := extractVertexThinkingConfig(reqBody)
	if err != nil {
		return nil, err
	}

	tools, err := extractVertexExtendedTools(reqBody)
	if err != nil {
		return nil, err
	}

	functions, err := extractVertexFunctions(reqBody)
	if err != nil {
		return nil, err
	}

	imageConfig, err := extractVertexImageConfig(reqBody)
	if err != nil {
		return nil, err
	}

	responseSchema, err := extractVertexResponseSchema(reqBody)
	if err != nil {
		return nil, err
	}

	stopSequences, err := extractVertexStopSequences(reqBody)
	if err != nil {
		return nil, err
	}

	responseModalities, err := extractVertexResponseModalities(reqBody)
	if err != nil {
		return nil, err
	}

	thinking, err := extractVertexThinking(reqBody)
	if err != nil {
		return nil, err
	}

	safetySettings, err := extractVertexSafetySettings(reqBody)
	if err != nil {
		return nil, err
	}

	return &vertexExtendedParams{
		ThinkingConfig:     tc,
		tools:              tools,
		functions:          functions,
		ImageConfig:        imageConfig,
		ResponseSchema:     responseSchema,
		ResponseMimeType:   extractVertexResponseMimeType(reqBody),
		ResponseLogprobs:   extractVertexResponseLogprobs(reqBody),
		MaxOutputTokens:    extractVertexMaxOutputTokens(reqBody),
		CandidateCount:     extractVertexCandidateCount(reqBody),
		StopSequences:      stopSequences,
		ResponseModalities: responseModalities,
		Thinking:           thinking,
		CachedContent:      extractVertexCachedContent(reqBody),
		SafetySettings:     safetySettings,
	}, nil
}

type vertexExtendedTool struct {
	vertex.SpecificTool
	Function *function `json:"function,omitempty"`
}

type vertexFunction struct {
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	vertex.SpecificTool
}

func extractVertexExtendedTools(reqBody []byte) ([]*vertexExtendedTool, error) {
	toolsJson := gjson.GetBytes(reqBody, "tools")
	if !toolsJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex tools: %s", toolsJson.Raw)
	var tools []*vertexExtendedTool
	if err := json.Unmarshal([]byte(toolsJson.Raw), &tools); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vertex tools: %v", err)
	}
	return tools, nil
}

func extractVertexFunctions(reqBody []byte) ([]*vertexFunction, error) {
	functionsJson := gjson.GetBytes(reqBody, "functions")
	if !functionsJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex functions: %s", functionsJson.Raw)
	var functions []*vertexFunction
	if err := json.Unmarshal([]byte(functionsJson.Raw), &functions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal vertex functions: %v", err)
	}
	return functions, nil
}

func extractVertexThinkingConfig(reqBody []byte) (*vertex.ThinkingConfig, error) {
	thinkingConfigJson := gjson.GetBytes(reqBody, "thinkingConfig")
	if !thinkingConfigJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex thinking config: %s", thinkingConfigJson.Raw)
	out := &vertex.ThinkingConfig{}
	if err := json.Unmarshal([]byte(thinkingConfigJson.Raw), &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal thinking config: %v", err)
	}
	return out, nil
}

func extractVertexImageConfig(reqBody []byte) (*vertex.ImageConfig, error) {
	imageConfigJson := gjson.GetBytes(reqBody, "imageConfig")
	if !imageConfigJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex image config: %s", imageConfigJson.Raw)
	out := &vertex.ImageConfig{}
	if err := json.Unmarshal([]byte(imageConfigJson.Raw), &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal image config: %v", err)
	}
	return out, nil
}

func extractVertexResponseSchema(reqBody []byte) (map[string]interface{}, error) {
	responseSchemaJson := gjson.GetBytes(reqBody, "response_schema")
	if !responseSchemaJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex response schema: %s", responseSchemaJson.Raw)
	var responseSchema map[string]interface{}
	if err := json.Unmarshal([]byte(responseSchemaJson.Raw), &responseSchema); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response schema: %v", err)
	}
	return responseSchema, nil
}

func extractVertexResponseMimeType(reqBody []byte) string {
	responseMimeType := gjson.GetBytes(reqBody, "response_mime_type")
	if !responseMimeType.Exists() {
		return ""
	}
	log.Tracef("[vertex]: vertex response mime type: %s", responseMimeType.String())
	return responseMimeType.String()
}

func extractVertexResponseLogprobs(reqBody []byte) *bool {
	responseLogprobs := gjson.GetBytes(reqBody, "response_logprobs")
	if !responseLogprobs.Exists() {
		return nil
	}
	log.Tracef("[vertex]: vertex response logprobs: %t", responseLogprobs.Bool())
	val := responseLogprobs.Bool()
	return &val
}

func extractVertexMaxOutputTokens(reqBody []byte) int {
	maxOutputTokens := gjson.GetBytes(reqBody, "max_output_tokens")
	if !maxOutputTokens.Exists() {
		return 0
	}
	log.Tracef("[vertex]: vertex max output tokens: %d", maxOutputTokens.Int())
	return int(maxOutputTokens.Int())
}

func extractVertexCandidateCount(reqBody []byte) int {
	candidateCount := gjson.GetBytes(reqBody, "candidate_count")
	if !candidateCount.Exists() {
		return 0
	}
	log.Tracef("[vertex]: vertex candidate count: %d", candidateCount.Int())
	return int(candidateCount.Int())
}

func extractVertexStopSequences(reqBody []byte) ([]string, error) {
	stopSequencesJson := gjson.GetBytes(reqBody, "stop_sequences")
	if !stopSequencesJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex stop sequences: %s", stopSequencesJson.Raw)
	var stopSequences []string
	if err := json.Unmarshal([]byte(stopSequencesJson.Raw), &stopSequences); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stop sequences: %v", err)
	}
	return stopSequences, nil
}

func extractVertexResponseModalities(reqBody []byte) ([]string, error) {
	responseModalitiesJson := gjson.GetBytes(reqBody, "response_modalities")
	if !responseModalitiesJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex response modalities: %s", responseModalitiesJson.Raw)
	var responseModalities []string
	if err := json.Unmarshal([]byte(responseModalitiesJson.Raw), &responseModalities); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response modalities: %v", err)
	}
	return responseModalities, nil
}

func extractVertexThinking(reqBody []byte) (*vertex.ClaudeStyleThinking, error) {
	thinkingJson := gjson.GetBytes(reqBody, "thinking")
	if !thinkingJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex thinking: %s", thinkingJson.Raw)
	out := &vertex.ClaudeStyleThinking{}
	if err := json.Unmarshal([]byte(thinkingJson.Raw), &out); err != nil {
		return nil, fmt.Errorf("failed to unmarshal thinking: %v", err)
	}
	return out, nil
}

func extractVertexCachedContent(reqBody []byte) string {
	cachedContent := gjson.GetBytes(reqBody, "cached_content")
	if !cachedContent.Exists() {
		return ""
	}
	log.Tracef("[vertex]: vertex cached content: %s", cachedContent.String())
	return cachedContent.String()
}

func extractVertexSafetySettings(reqBody []byte) ([]*vertex.SafetySettingsConfig, error) {
	safetySettingsJson := gjson.GetBytes(reqBody, "safety_settings")
	if !safetySettingsJson.Exists() {
		return nil, nil
	}
	log.Tracef("[vertex]: vertex safety settings: %s", safetySettingsJson.Raw)
	var safetySettings []*vertex.SafetySettingsConfig
	if err := json.Unmarshal([]byte(safetySettingsJson.Raw), &safetySettings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal safety settings: %v", err)
	}
	return safetySettings, nil
}
