package vertex

import (
	"strings"
)

// OpenAIUsage represents the OpenAI-compatible usage structure for Vertex AI responses
type OpenAIUsage struct {
	PromptTokens               int                   `json:"prompt_tokens,omitempty"`
	CompletionTokens           int                   `json:"completion_tokens,omitempty"`
	TotalTokens                int                   `json:"total_tokens,omitempty"`
	ReasoningTokens            int                   `json:"reasoning_tokens,omitempty"`
	CachedTokens               int                   `json:"cached_tokens,omitempty"`
	ToolUsePromptTokens        int                   `json:"tool_use_prompt_tokens,omitempty"`
	CompletionTokensDetails    ModalityTokensDetails `json:"completion_tokens_details,omitempty"`
	PromptTokensDetails        ModalityTokensDetails `json:"prompt_tokens_details,omitempty"`
	CacheTokensDetails         ModalityTokensDetails `json:"cache_tokens_details,omitempty"`
	ToolUsePromptTokensDetails ModalityTokensDetails `json:"tool_use_prompt_tokens_details,omitempty"`
	TrafficType                string                `json:"traffic_type,omitempty"`
}

// ModalityTokensDetails represents token counts broken down by modality
type ModalityTokensDetails map[string]int

// ConvertVertexUsage transforms Vertex AI UsageMetadata to OpenAI-compatible usage format
func ConvertVertexUsage(usageMetadata *UsageMetadata) *OpenAIUsage {
	if usageMetadata == nil {
		return nil
	}

	// Build prompt tokens details
	var promptDetails ModalityTokensDetails
	if len(usageMetadata.PromptTokensDetails) > 0 {
		promptDetails = buildModalityTokensDetails(usageMetadata.PromptTokensDetails)
	}

	// Build completion tokens details from candidatesTokensDetails
	var completionDetails ModalityTokensDetails
	if len(usageMetadata.CandidatesTokensDetails) > 0 {
		completionDetails = buildModalityTokensDetails(usageMetadata.CandidatesTokensDetails)
	}

	// Build cache tokens details
	var cacheDetails ModalityTokensDetails
	if len(usageMetadata.CacheTokensDetails) > 0 {
		cacheDetails = buildModalityTokensDetails(usageMetadata.CacheTokensDetails)
	}

	// Build tool use prompt tokens details
	var toolUseDetails ModalityTokensDetails
	if len(usageMetadata.ToolUsePromptTokensDetails) > 0 {
		toolUseDetails = buildModalityTokensDetails(usageMetadata.ToolUsePromptTokensDetails)
	}

	return &OpenAIUsage{
		PromptTokens:               usageMetadata.PromptTokenCount,
		CompletionTokens:           usageMetadata.CandidatesTokenCount,
		TotalTokens:                usageMetadata.TotalTokenCount,
		ReasoningTokens:            usageMetadata.ThoughtsTokenCount,
		CachedTokens:               usageMetadata.CachedContentTokenCount,
		ToolUsePromptTokens:        usageMetadata.ToolUsePromptTokenCount,
		PromptTokensDetails:        promptDetails,
		CompletionTokensDetails:    completionDetails,
		CacheTokensDetails:         cacheDetails,
		ToolUsePromptTokensDetails: toolUseDetails,
		TrafficType:                usageMetadata.TrafficType,
	}
}

// buildModalityTokensDetails converts a slice of ModalityTokenCount into ModalityTokensDetails
func buildModalityTokensDetails(details []*ModalityTokenCount) ModalityTokensDetails {
	result := make(ModalityTokensDetails)
	for _, detail := range details {
		if detail == nil {
			continue
		}
		result[strings.ToLower(detail.Modality)+"_tokens"] = detail.TokenCount
	}
	return result
}
