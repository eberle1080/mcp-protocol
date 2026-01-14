package schema

// Message represents a message in a conversation for sampling
type Message struct {
	Role    string      `json:"role"`    // "user", "assistant", or "system"
	Content interface{} `json:"content"` // string or object for multi-modal
}

// SamplingCreateMessageRequest represents a request to create an LLM completion
type SamplingCreateMessageRequest struct {
	Messages         []Message         `json:"messages"`                   // Array of conversation messages
	ModelPreferences *ModelPreferences `json:"modelPreferences,omitempty"` // Optional model selection preferences
	SystemPrompt     *string           `json:"systemPrompt,omitempty"`     // Optional system prompt
	MaxTokens        *int              `json:"maxTokens,omitempty"`        // Optional maximum tokens to generate
	Temperature      *float64          `json:"temperature,omitempty"`      // Optional temperature (0-1)
	Tools            []Tool            `json:"tools,omitempty"`            // Optional tools for tool-enabled sampling
}

// TokenUsage represents token usage statistics
type TokenUsage struct {
	InputTokens  int `json:"inputTokens"`  // Number of tokens in the input
	OutputTokens int `json:"outputTokens"` // Number of tokens in the output
}

// SamplingCreateMessageResponse represents the response from a sampling request
type SamplingCreateMessageResponse struct {
	Content    string     `json:"content"`    // The generated text content
	Model      string     `json:"model"`      // The model used for generation
	StopReason string     `json:"stopReason"` // Why generation stopped (e.g., "end_turn", "max_tokens")
	Usage      TokenUsage `json:"usage"`      // Token usage statistics
}

// SamplingCapability describes the client's sampling support
type SamplingCapability struct {
	Enabled       bool `json:"enabled"`       // Whether sampling is supported
	SupportsTools bool `json:"supportsTools"` // Whether tool-enabled sampling is supported
}
