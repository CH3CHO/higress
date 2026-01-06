package provider

import (
	"encoding/json"
	"fmt"

	"github.com/higress-group/wasm-go/pkg/log"
	"github.com/tidwall/gjson"

	"github.com/alibaba/higress/plugins/wasm-go/extensions/ai-proxy/provider/bedrock"
)

type bedrockExtendedParams struct {
	MaxTokens              int
	ExtraHeaders           map[string]string
	TopP                   float64 // we use separate fields here instead of chatCompletionRequest.topP because both top_p and topP are used
	TopK                   float64
	Top_K                  float64 // separate fields here because top_k need put into additionalModelRequestFields
	StopSequences          []string
	PerformanceConfigBlock *bedrock.PerformanceConfigBlock
	GuardrailConfigBlock   *bedrock.GuardrailConfigBlock
	Thinking               map[string]interface{}
	EnableThinking         map[string]any
}

func extractBedrockExtendedParams(reqBody []byte) (*bedrockExtendedParams, error) {
	extraHeaders, err := extractBedrockExtraHeaders(reqBody)
	if err != nil {
		return nil, err
	}

	topP, err := extractBedrockTopP(reqBody)
	if err != nil {
		return nil, err
	}

	topK, err := extractBedrockTopK(reqBody)
	if err != nil {
		return nil, err
	}

	top_K, err := extractBedrockTop_K(reqBody)
	if err != nil {
		return nil, err
	}

	stopSequences, err := extractBedrockStopSequences(reqBody)
	if err != nil {
		return nil, err
	}

	performanceConfigBlock, err := extractBedrockPerformanceConfigBlock(reqBody)
	if err != nil {
		return nil, err
	}

	guardrailConfigBlock, err := extractBedrockGuardrailConfigBlock(reqBody)
	if err != nil {
		return nil, err
	}

	thinking, err := extractBedrockThinkingParams(reqBody)
	if err != nil {
		return nil, err
	}

	enableThinking, err := extractBedrockEnableThinkingParams(reqBody)
	if err != nil {
		return nil, err
	}

	return &bedrockExtendedParams{
		MaxTokens:              extractBedrockMaxTokens(reqBody),
		ExtraHeaders:           extraHeaders,
		TopP:                   topP,
		TopK:                   topK,
		Top_K:                  top_K,
		StopSequences:          stopSequences,
		PerformanceConfigBlock: performanceConfigBlock,
		GuardrailConfigBlock:   guardrailConfigBlock,
		Thinking:               thinking,
		EnableThinking:         enableThinking,
	}, nil
}

func extractBedrockMaxTokens(reqBody []byte) int {
	maxTokensJson := gjson.GetBytes(reqBody, "maxTokens") // NOTE: bedrock take "maxTokens" precedence over "max_tokens"
	if !maxTokensJson.Exists() {
		return 0
	}
	log.Tracef("[bedrock] maxTokens: %s", maxTokensJson.Raw)
	return int(maxTokensJson.Int())
}

func extractBedrockExtraHeaders(reqBody []byte) (map[string]string, error) {
	extraHeadersJson := gjson.GetBytes(reqBody, "extra_headers")
	if !extraHeadersJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] extra headers: %s", extraHeadersJson.Raw)
	var extraHeaders map[string]string
	if err := json.Unmarshal([]byte(extraHeadersJson.Raw), &extraHeaders); err != nil {
		return nil, fmt.Errorf("failed to unmarshal extra headers: %v", err)
	}
	return extraHeaders, nil
}

func extractBedrockTopP(reqBody []byte) (float64, error) {
	var topPJson gjson.Result
	topPJson = gjson.GetBytes(reqBody, "topP")
	if !topPJson.Exists() {
		topPJson = gjson.GetBytes(reqBody, "top_p")
	}
	if !topPJson.Exists() {
		return 0, nil
	}
	log.Tracef("[bedrock] top p: %s", topPJson.Raw)
	return topPJson.Float(), nil
}

func extractBedrockTopK(reqBody []byte) (float64, error) {
	var topKJson gjson.Result
	topKJson = gjson.GetBytes(reqBody, "topK")
	if !topKJson.Exists() {
		return 0, nil
	}
	log.Tracef("[bedrock] topK: %s", topKJson.Raw)
	return topKJson.Float(), nil
}

func extractBedrockTop_K(reqBody []byte) (float64, error) {
	var top_KJson gjson.Result
	top_KJson = gjson.GetBytes(reqBody, "top_k")
	if !top_KJson.Exists() {
		return 0, nil
	}
	log.Tracef("[bedrock] top_k: %s", top_KJson.Raw)
	return top_KJson.Float(), nil
}

func extractBedrockStopSequences(reqBody []byte) ([]string, error) {
	stopSequencesJson := gjson.GetBytes(reqBody, "stop_sequences")
	if !stopSequencesJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] stop sequences: %s", stopSequencesJson.Raw)
	var stopSequences []string
	if err := json.Unmarshal([]byte(stopSequencesJson.Raw), &stopSequences); err != nil {
		return nil, fmt.Errorf("failed to unmarshal stop sequences: %v", err)
	}
	return stopSequences, nil
}

func extractBedrockPerformanceConfigBlock(reqBody []byte) (*bedrock.PerformanceConfigBlock, error) {
	perfConfigJson := gjson.GetBytes(reqBody, "performanceConfig")
	if !perfConfigJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] performance config: %s", perfConfigJson.Raw)
	var perfConfig bedrock.PerformanceConfigBlock
	if err := json.Unmarshal([]byte(perfConfigJson.Raw), &perfConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal performance config: %v", err)
	}
	return &perfConfig, nil
}

func extractBedrockGuardrailConfigBlock(reqBody []byte) (*bedrock.GuardrailConfigBlock, error) {
	guardrailConfigJson := gjson.GetBytes(reqBody, "guardrailConfig")
	if !guardrailConfigJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] guardrail config: %s", guardrailConfigJson.Raw)
	var guardrailConfig bedrock.GuardrailConfigBlock
	if err := json.Unmarshal([]byte(guardrailConfigJson.Raw), &guardrailConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal guardrail config: %v", err)
	}
	return &guardrailConfig, nil
}

func extractBedrockThinkingParams(reqBody []byte) (map[string]interface{}, error) {
	thinkingJson := gjson.GetBytes(reqBody, "thinking")
	if !thinkingJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] thinking params: %s", thinkingJson.Raw)
	var thinkingParams map[string]interface{}
	if err := json.Unmarshal([]byte(thinkingJson.Raw), &thinkingParams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal thinking params: %v", err)
	}
	return thinkingParams, nil
}

func extractBedrockEnableThinkingParams(reqBody []byte) (map[string]any, error) {
	enableThinkingJson := gjson.GetBytes(reqBody, "enable_thinking")
	if !enableThinkingJson.Exists() {
		return nil, nil
	}
	log.Tracef("[bedrock] enable thinking params: %s", enableThinkingJson.Raw)
	var enableThinkingParams map[string]any
	if err := json.Unmarshal([]byte(enableThinkingJson.Raw), &enableThinkingParams); err != nil {
		return nil, fmt.Errorf("failed to unmarshal enable thinking params: %v", err)
	}
	return enableThinkingParams, nil
}
