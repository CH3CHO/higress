package vertex

const (
	ModalityText        = "TEXT"
	ModalityAudio       = "AUDIO"
	ModalityImage       = "IMAGE"
	ModalityUnspecified = "MODALITY_UNSPECIFIED"
)

// Use var instead of const because ThinkingConfig.ThinkingLevel is a pointer to string
var (
	ThinkingLevelMinimal = "minimal"
	ThinkingLevelLow     = "low"
	ThinkingLevelMedium  = "medium"
	ThinkingLevelHigh    = "high"
)

type GenerationConfig struct {
	Temperature        float64                `json:"temperature,omitempty"`
	TopP               float64                `json:"topP,omitempty"`
	TopK               int                    `json:"topK,omitempty"`
	CandidateCount     int                    `json:"candidateCount,omitempty"`
	MaxOutputTokens    int                    `json:"maxOutputTokens,omitempty"`
	StopSequences      []string               `json:"stopSequences,omitempty"`
	ResponseModalities []string               `json:"responseModalities,omitempty"`
	PresencePenalty    float64                `json:"presencePenalty,omitempty"`
	FrequencyPenalty   float64                `json:"frequencyPenalty,omitempty"`
	ResponseMimeType   string                 `json:"responseMimeType,omitempty"`
	ResponseSchema     map[string]interface{} `json:"responseSchema,omitempty"`
	Seed               *int                   `json:"seed,omitempty"`
	Logprobs           *int                   `json:"logprobs,omitempty"`
	ResponseLogprobs   *bool                  `json:"responseLogprobs,omitempty"`
	ThinkingConfig     *ThinkingConfig        `json:"thinkingConfig,omitempty"`
	ImageConfig        *ImageConfig           `json:"imageConfig,omitempty"`
	SpeechConfig       map[string]any         `json:"speechConfig,omitempty"`
}

type Candidate struct {
	Content        *Content        `json:"content"`
	FinishReason   string          `json:"finishReason"`
	Index          int             `json:"index"`
	LogprobsResult *LogprobsResult `json:"logprobsResult"`

	SafetyRatings      []*SafetyRatings    `json:"safetyRatings"`
	GroundingMetadata  *GroundingMetadata  `json:"groundingMetadata"`
	CitationMetadata   *CitationMetadata   `json:"citationMetadata"`
	UrlContextMetadata *URLContextMetadata `json:"urlContextMetadata"`
}

type Content struct {
	Role  string  `json:"role,omitempty"` // "user" or "model"
	Parts []*Part `json:"parts"`          // Required field
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *Blob             `json:"inlineData,omitempty"`
	FileData         *FileData         `json:"fileData,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"` // For Gemini 3 models, thought signature from tool definition
}

type UsageMetadata struct {
	PromptTokenCount        int                   `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount    int                   `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount         int                   `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount      int                   `json:"thoughtsTokenCount,omitempty"`
	CachedContentTokenCount int                   `json:"cachedContentTokenCount,omitempty"`
	ResponseTokenCount      int                   `json:"responseTokenCount,omitempty"`
	PromptTokensDetails     []*ModalityTokenCount `json:"promptTokensDetails,omitempty"`
	ResponseTokensDetails   []*ModalityTokenCount `json:"responseTokensDetails,omitempty"`
	CandidatesTokensDetails []*ModalityTokenCount `json:"candidatesTokensDetails,omitempty"`
	TrafficType             string                `json:"trafficType,omitempty"`
}

type ModalityTokenCount struct {
	Modality   string `json:"modality,omitempty"`
	TokenCount int    `json:"tokenCount,omitempty"`
}

type FunctionCall struct {
	Name             string                 `json:"name"`
	Args             map[string]interface{} `json:"args"`
	ThoughtSignature string                 `json:"thoughtSignature,omitempty"`
}

type ThinkingConfig struct {
	IncludeThoughts *bool   `json:"includeThoughts,omitempty"`
	ThinkingBudget  *int    `json:"thinkingBudget,omitempty"` // If thinkingBudget is explicitly set to 0, the field should be preserved during serialization
	ThinkingLevel   *string `json:"thinkingLevel,omitempty"`
}

type ClaudeStyleThinking struct {
	Type         *string `json:"type,omitempty"`
	BudgetTokens *int    `json:"budget_tokens,omitempty"`
}

type Tools struct {
	FunctionDeclarations []*FunctionDeclaration `json:"functionDeclarations,omitempty"`
	SpecificTool
}
type FunctionDeclaration struct {
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
	Response    map[string]interface{} `json:"response,omitempty"`
}

type SpecificTool struct {
	GoogleSearch          *map[string]interface{} `json:"googleSearch,omitempty"` // TODO litellm supports both camelCase and snake_case
	GoogleSearchRetrieval *map[string]interface{} `json:"googleSearchRetrieval,omitempty"`
	EnterpriseWebSearch   *map[string]interface{} `json:"enterpriseWebSearch,omitempty"`
	URLContext            *map[string]interface{} `json:"urlContext,omitempty"`
	CodeExecution         *map[string]interface{} `json:"codeExecution,omitempty"`
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type FunctionCallingConfig struct {
	// Mode specifies the function calling mode. Valid values: "ANY", "AUTO", "NONE"
	Mode string `json:"mode,omitempty"`

	AllowedFunctionNames []string `json:"allowed_function_names,omitempty"`
}

type SafetyRatings struct {
	Category         string  `json:"category"`
	Probability      string  `json:"probability"`
	ProbabilityScore float64 `json:"probabilityScore"`
	Severity         string  `json:"severity"`
	Blocked          bool    `json:"blocked"`
}

type URLMetadata map[string]any

type CitationMetadata map[string]any

type URLContextMetadata map[string]any

type GroundingMetadata map[string]any

type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Date  int `json:"date"`
}

type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

type SafetySettingsConfig struct {
	Category            string `json:"category,omitempty"`
	Threshold           string `json:"threshold,omitempty"`
	MaxInfluentialTerms int    `json:"max_influential_terms,omitempty"`
	Method              string `json:"method,omitempty"`
}

type SystemInstruction struct {
	Role  string  `json:"role,omitempty"`
	Parts []*Part `json:"parts"`
}

type Blob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type FileData struct {
	MimeType string `json:"mimeType"`
	FileURI  string `json:"fileUri"` // the cloud storage uri of storing this file
}

type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response,omitempty"`
}

type LogprobsCandidate struct {
	Token          string  `json:"token"`
	TokenID        int     `json:"tokenId"`
	LogProbability float64 `json:"logProbability"`
}

type LogprobsTopCandidate struct {
	Candidates []*LogprobsCandidate `json:"candidates"`
}

type LogprobsResult struct {
	TopCandidates    []*LogprobsTopCandidate `json:"topCandidates,omitempty"`
	ChosenCandidates []*LogprobsCandidate    `json:"chosenCandidates,omitempty"`
}
