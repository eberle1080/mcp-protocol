package schema

// ElicitationCreateRequest represents a request for user input
type ElicitationCreateRequest struct {
	Message    string                 `json:"message"`              // Explanation of why input is needed
	Mode       string                 `json:"mode"`                 // "form" or "url"
	FormSchema map[string]interface{} `json:"formSchema,omitempty"` // JSON schema for form mode
	URL        string                 `json:"url,omitempty"`        // URL for url mode
}

// ElicitationCreateResponse represents the response from an elicitation request
type ElicitationCreateResponse struct {
	Action  string                 `json:"action"`  // "accept", "decline", or "cancel"
	Content map[string]interface{} `json:"content"` // Form data (form mode) or result (url mode)
}

// ElicitationCapability describes the client's elicitation support
type ElicitationCapability struct {
	Enabled        bool     `json:"enabled"`        // Whether elicitation is supported
	SupportedModes []string `json:"supportedModes"` // Supported modes: "form", "url"
}
