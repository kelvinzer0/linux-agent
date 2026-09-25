package protocol

// ToolDefinition defines the structure of a tool exposed by linux-agent.
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// ToolContent represents a content element returned by a tool execution.
type ToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// ToolResult represents the output of a tool call.
type ToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// Helper to create a single text result
func TextResult(text string) ToolResult {
	return ToolResult{
		Content: []ToolContent{
			{
				Type: "text",
				Text: text,
			},
		},
		IsError: false,
	}
}

// Helper to create an error result
func ErrorResult(errMsg string) ToolResult {
	return ToolResult{
		Content: []ToolContent{
			{
				Type: "text",
				Text: errMsg,
			},
		},
		IsError: true,
	}
}

// ExtensionMessage represents an inbound WebSocket payload sent to the bridge.
type ExtensionMessage struct {
	Type   string           `json:"type"`
	Tools  []ToolDefinition `json:"tools,omitempty"`
	Names  []string         `json:"names,omitempty"`
	CallID string           `json:"callId,omitempty"`
	Result *ToolResult      `json:"result,omitempty"`
}

// CallToolMessage represents an outbound tool call dispatched from the bridge.
type CallToolMessage struct {
	Type   string                 `json:"type"`
	CallID string                 `json:"callId"`
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}
