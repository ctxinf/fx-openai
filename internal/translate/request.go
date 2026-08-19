package translate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"fx-openai/internal/openai"
)

var (
	ErrInvalidBody  = errors.New("invalid gateway request body")
	ErrImageOnly    = errors.New("image-only user message is not supported")
	ErrMissingModel = errors.New("missing ai-language-model-id")
)

// ChatRequest maps a Gateway language-model JSON body to an OpenAI chat request.
// model comes from the ai-language-model-id header, not the body.
func ChatRequest(model string, stream bool, body []byte) (openai.ChatRequest, error) {
	if strings.TrimSpace(model) == "" {
		return openai.ChatRequest{}, ErrMissingModel
	}
	var in gatewayRequest
	if err := json.Unmarshal(body, &in); err != nil {
		return openai.ChatRequest{}, fmt.Errorf("%w: %v", ErrInvalidBody, err)
	}

	out := openai.ChatRequest{
		Model:     model,
		Stream:    stream,
		MaxTokens: in.MaxOutputTokens,
	}

	var lastUserImageOnly bool
	for _, msg := range in.Prompt {
		converted, imageOnly, err := convertMessage(msg)
		if err != nil {
			return openai.ChatRequest{}, err
		}
		if msg.Role == "user" {
			lastUserImageOnly = imageOnly
		}
		out.Messages = append(out.Messages, converted...)
	}
	if lastUserImageOnly {
		return openai.ChatRequest{}, ErrImageOnly
	}

	for _, tool := range in.Tools {
		if tool.Name == "" {
			continue
		}
		spec := openai.Tool{
			Type: "function",
			Function: openai.ToolSpec{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		}
		if len(bytes.TrimSpace(spec.Function.Parameters)) == 0 {
			spec.Function.Parameters = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out.Tools = append(out.Tools, spec)
	}

	if choice, ok := toolChoice(in.ToolChoice); ok {
		out.ToolChoice = choice
	}
	return out, nil
}

type gatewayRequest struct {
	Prompt          []gatewayMessage `json:"prompt"`
	Tools           []gatewayTool    `json:"tools"`
	ToolChoice      json.RawMessage  `json:"toolChoice"`
	MaxOutputTokens *int             `json:"maxOutputTokens"`
}

type gatewayMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type gatewayTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

type gatewayPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	Input      json.RawMessage `json:"input"`
	Output     json.RawMessage `json:"output"`
}

type toolResult struct {
	ID      string
	Name    string
	Content string
}

func convertMessage(msg gatewayMessage) ([]openai.ChatMessage, bool, error) {
	raw := bytes.TrimSpace(msg.Content)
	if len(raw) == 0 || string(raw) == "null" {
		if msg.Role == "" {
			return nil, false, nil
		}
		return []openai.ChatMessage{{Role: msg.Role}}, false, nil
	}

	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, false, fmt.Errorf("%w: content: %v", ErrInvalidBody, err)
		}
		return []openai.ChatMessage{{Role: msg.Role, Content: text}}, false, nil
	}

	if raw[0] != '[' {
		return nil, false, fmt.Errorf("%w: content must be a string or array", ErrInvalidBody)
	}

	var parts []gatewayPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		return nil, false, fmt.Errorf("%w: content parts: %v", ErrInvalidBody, err)
	}

	var text strings.Builder
	var calls []openai.ToolCall
	var results []toolResult
	var hadNonText bool
	for _, part := range parts {
		switch part.Type {
		case "text", "":
			text.WriteString(part.Text)
		case "tool-call":
			calls = append(calls, openai.ToolCall{
				ID:   part.ToolCallID,
				Type: "function",
				Function: openai.ToolCallFunction{
					Name:      part.ToolName,
					Arguments: rawToArguments(part.Input),
				},
			})
		case "tool-result":
			results = append(results, toolResult{
				ID:      part.ToolCallID,
				Name:    part.ToolName,
				Content: toolOutputText(part.Output),
			})
		default:
			hadNonText = true
		}
	}

	if msg.Role == "tool" || len(results) > 0 && msg.Role != "assistant" {
		out := make([]openai.ChatMessage, 0, len(results))
		for _, result := range results {
			out = append(out, openai.ChatMessage{
				Role:       "tool",
				ToolCallID: result.ID,
				Content:    result.Content,
			})
		}
		if len(out) == 0 {
			out = append(out, openai.ChatMessage{Role: "tool", Content: text.String()})
		}
		return out, false, nil
	}

	imageOnly := hadNonText && text.Len() == 0 && len(calls) == 0
	out := openai.ChatMessage{
		Role:      msg.Role,
		Content:   text.String(),
		ToolCalls: calls,
	}
	return []openai.ChatMessage{out}, imageOnly, nil
}

func rawToArguments(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return "{}"
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	return string(raw)
}

func toolOutputText(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s
		}
	}
	var obj struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Value != "" {
		return obj.Value
	}
	return string(raw)
}

func toolChoice(raw json.RawMessage) (any, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil && s != "" {
			return s, true
		}
		return nil, false
	}
	var obj struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil || obj.Type == "" {
		return nil, false
	}
	switch obj.Type {
	case "auto", "required", "none":
		return obj.Type, true
	default:
		return nil, false
	}
}
