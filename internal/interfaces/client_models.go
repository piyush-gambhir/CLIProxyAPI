package interfaces

type Content struct {
	Role string `json:"role"`

	Parts []Part `json:"parts"`
}

type Part struct {
	Thought bool `json:"thought,omitempty"`

	Text string `json:"text,omitempty"`

	InlineData *InlineData `json:"inlineData,omitempty"`

	ThoughtSignature string `json:"thoughtSignature,omitempty"`

	FunctionCall *FunctionCall `json:"functionCall,omitempty"`

	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

type InlineData struct {
	MimeType string `json:"mime_type,omitempty"`

	Data string `json:"data,omitempty"`
}

type FunctionCall struct {
	ID string `json:"id,omitempty"`

	Name string `json:"name"`

	Args map[string]interface{} `json:"args"`
}

type FunctionResponse struct {
	ID string `json:"id,omitempty"`

	Name string `json:"name"`

	Response map[string]interface{} `json:"response"`
}

type GenerateContentRequest struct {
	SystemInstruction *Content `json:"systemInstruction,omitempty"`

	Contents []Content `json:"contents"`

	Tools []ToolDeclaration `json:"tools,omitempty"`

	GenerationConfig `json:"generationConfig"`
}

type GenerationConfig struct {
	ThinkingConfig GenerationConfigThinkingConfig `json:"thinkingConfig,omitempty"`

	Temperature float64 `json:"temperature,omitempty"`

	TopP float64 `json:"topP,omitempty"`

	TopK float64 `json:"topK,omitempty"`
}

type GenerationConfigThinkingConfig struct {
	IncludeThoughts bool `json:"include_thoughts,omitempty"`
}

type ToolDeclaration struct {
	FunctionDeclarations []interface{} `json:"functionDeclarations"`
}
