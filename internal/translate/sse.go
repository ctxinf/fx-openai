package translate

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Stream converts OpenAI Chat Completions SSE payloads into Gateway events.
// Each Consume/Close return JSON objects; the HTTP layer adds "data: " framing.
type Stream struct {
	tools    map[int]*toolAcc
	order    []int
	finished bool
}

type toolAcc struct {
	id      string
	name    string
	args    strings.Builder
	started bool
}

func NewStream() *Stream {
	return &Stream{tools: map[int]*toolAcc{}}
}

func (s *Stream) Consume(data []byte) [][]byte {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "[DONE]" {
		return nil
	}

	var chunk openaiChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil
	}

	var events [][]byte
	if len(chunk.Error) > 0 && string(chunk.Error) != "null" {
		events = append(events, mustJSON(map[string]any{
			"type":  "error",
			"error": json.RawMessage(chunk.Error),
		}))
	}

	if len(chunk.Choices) == 0 {
		return events
	}
	choice := chunk.Choices[0]

	if content := choice.Delta.Content; content != "" {
		events = append(events, mustJSON(map[string]any{
			"type":  "text-delta",
			"delta": content,
		}))
	}
	reasoning := choice.Delta.Reasoning
	if reasoning == "" {
		reasoning = choice.Delta.ReasoningContent
	}
	if reasoning != "" {
		events = append(events, mustJSON(map[string]any{
			"type":  "reasoning-delta",
			"delta": reasoning,
		}))
	}

	for _, call := range choice.Delta.ToolCalls {
		acc := s.tool(call.Index)
		if call.ID != "" {
			acc.id = call.ID
		}
		if call.Function.Name != "" {
			acc.name = call.Function.Name
		}
		if !acc.started && acc.id != "" && acc.name != "" {
			acc.started = true
			events = append(events, mustJSON(map[string]any{
				"type":     "tool-input-start",
				"id":       acc.id,
				"toolName": acc.name,
			}))
		}
		if call.Function.Arguments != "" {
			acc.args.WriteString(call.Function.Arguments)
			if acc.started {
				events = append(events, mustJSON(map[string]any{
					"type":  "tool-input-delta",
					"id":    acc.id,
					"delta": call.Function.Arguments,
				}))
			}
		}
	}

	if choice.FinishReason != "" {
		events = append(events, s.finalize(choice.FinishReason, chunk.Usage)...)
	}
	return events
}

// Close emits any pending tool finals and a stop finish if the stream ended
// without a finish_reason (OpenAI's [DONE] after the last token).
func (s *Stream) Close() [][]byte {
	if s.finished {
		return nil
	}
	return s.finalize("stop", usage{})
}

// Fail marks a truncated or broken upstream stream. It does not emit a
// successful stop, and it does not finalize incomplete tool calls.
func (s *Stream) Fail(msg string) [][]byte {
	if msg == "" {
		msg = "upstream stream interrupted"
	}
	errEv := mustJSON(map[string]any{"type": "error", "error": msg})
	if s.finished {
		return [][]byte{errEv}
	}
	s.finished = true
	return [][]byte{
		errEv,
		mustJSON(map[string]any{
			"type": "finish",
			"finishReason": map[string]string{
				"unified": "error",
			},
		}),
	}
}

func (s *Stream) finalize(reason string, usage usage) [][]byte {
	if s.finished {
		return nil
	}
	s.finished = true

	var events [][]byte
	for _, idx := range s.order {
		acc := s.tools[idx]
		if acc == nil || acc.id == "" {
			continue
		}
		if acc.started {
			events = append(events, mustJSON(map[string]any{
				"type": "tool-input-end",
				"id":   acc.id,
			}))
		}
		args := acc.args.String()
		if args == "" {
			args = "{}"
		}
		if json.Valid([]byte(args)) {
			events = append(events, mustJSON(map[string]any{
				"type":       "tool-call",
				"toolCallId": acc.id,
				"toolName":   acc.name,
				"input":      json.RawMessage(args),
			}))
		} else {
			events = append(events, mustJSON(map[string]any{
				"type":  "error",
				"error": "tool arguments are not valid JSON",
			}))
		}
	}

	finish := map[string]any{
		"type": "finish",
		"finishReason": map[string]string{
			"unified": unifiedFinish(reason),
		},
	}
	if usage.PromptTokens > 0 || usage.CompletionTokens > 0 {
		finish["usage"] = map[string]any{
			"inputTokens":  map[string]int{"total": usage.PromptTokens},
			"outputTokens": map[string]int{"total": usage.CompletionTokens},
		}
	}
	events = append(events, mustJSON(finish))
	return events
}

func (s *Stream) tool(index int) *toolAcc {
	if acc, ok := s.tools[index]; ok {
		return acc
	}
	acc := &toolAcc{}
	s.tools[index] = acc
	s.order = append(s.order, index)
	return acc
}

func unifiedFinish(reason string) string {
	switch reason {
	case "stop":
		return "stop"
	case "length":
		return "length"
	case "content_filter":
		return "content-filter"
	case "tool_calls":
		return "tool-calls"
	case "error":
		return "error"
	default:
		return "other"
	}
}

type openaiChunk struct {
	Error   json.RawMessage `json:"error"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usage `json:"usage"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error","error":"internal marshal"}`)
	}
	return b
}

// WriteSSE writes one Gateway event with standard SSE framing.
func WriteSSE(buf *bytes.Buffer, event []byte) {
	buf.WriteString("data: ")
	buf.Write(event)
	buf.WriteString("\n\n")
}
