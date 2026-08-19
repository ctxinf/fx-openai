package translate

import "encoding/json"

// Catalog rewrites an OpenAI /v1/models body into fx's coding-agent catalog.
// Ids are passed through unchanged. Missing windows stay 0. Every language
// model is tagged tool-use so fx advertises tools.
func Catalog(raw []byte) ([]byte, error) {
	var in struct {
		Data []struct {
			ID              string `json:"id"`
			ContextWindow   int    `json:"context_window"`
			ContextLength   int    `json:"context_length"`
			MaxTokens       int    `json:"max_tokens"`
			MaxOutputTokens int    `json:"max_output_tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}

	type outModel struct {
		ID            string   `json:"id"`
		Type          string   `json:"type"`
		Tags          []string `json:"tags"`
		ContextWindow int      `json:"context_window"`
		MaxTokens     int      `json:"max_tokens"`
	}
	out := struct {
		Data []outModel `json:"data"`
	}{Data: make([]outModel, 0, len(in.Data))}

	for _, m := range in.Data {
		if m.ID == "" {
			continue
		}
		window := m.ContextWindow
		if window == 0 {
			window = m.ContextLength
		}
		maxTok := m.MaxTokens
		if maxTok == 0 {
			maxTok = m.MaxOutputTokens
		}
		out.Data = append(out.Data, outModel{
			ID:            m.ID,
			Type:          "language",
			Tags:          []string{"tool-use"},
			ContextWindow: window,
			MaxTokens:     maxTok,
		})
	}
	return json.Marshal(out)
}
