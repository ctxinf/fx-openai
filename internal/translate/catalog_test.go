package translate

import (
	"encoding/json"
	"testing"
)

func TestCatalogPassesIdsAndTagsToolUse(t *testing.T) {
	out, err := Catalog([]byte(`{
		"object":"list",
		"data":[
			{"id":"glm-5.2","object":"model"},
			{"id":"zai/glm-5.2","context_window":202752,"max_tokens":8192},
			{"object":"model"},
			{"id":"gpt-oss:120b","context_length":128000}
		]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Data []struct {
			ID            string   `json:"id"`
			Type          string   `json:"type"`
			Tags          []string `json:"tags"`
			ContextWindow int      `json:"context_window"`
			MaxTokens     int      `json:"max_tokens"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Data) != 3 {
		t.Fatalf("skipped empty id? got %d: %s", len(parsed.Data), out)
	}
	if parsed.Data[0].ID != "glm-5.2" || parsed.Data[0].ContextWindow != 0 {
		t.Fatalf("first %#v", parsed.Data[0])
	}
	if parsed.Data[1].ID != "zai/glm-5.2" {
		t.Fatalf("must not rewrite id, got %q", parsed.Data[1].ID)
	}
	if parsed.Data[1].ContextWindow != 202752 || parsed.Data[1].MaxTokens != 8192 {
		t.Fatalf("copied windows %#v", parsed.Data[1])
	}
	if parsed.Data[2].ContextWindow != 128000 {
		t.Fatalf("context_length fallback %#v", parsed.Data[2])
	}
	for _, m := range parsed.Data {
		if m.Type != "language" || len(m.Tags) != 1 || m.Tags[0] != "tool-use" {
			t.Fatalf("tags %#v", m)
		}
	}
}

func TestCatalogInvalidJSON(t *testing.T) {
	if _, err := Catalog([]byte(`[`)); err == nil {
		t.Fatal("expected error")
	}
}
