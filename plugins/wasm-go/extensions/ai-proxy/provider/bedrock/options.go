package bedrock

// TransformResponseOptions contains options for response transformation
type TransformResponseOptions struct {
	Model     string
	JSONMode  bool
	RequestID string
}

// TransformMessagesOptions contains options for message transformation
type TransformMessagesOptions struct {
	UserContinueMessage string // Message to use when user continue is needed
	ModifyParams        bool   // Whether to automatically modify params (insert continue messages)
}

// TransformRequestOptions contains options for request transformation
type TransformRequestOptions struct {
	Model               string
	UserContinueMessage string
	ModifyParams        bool
}
