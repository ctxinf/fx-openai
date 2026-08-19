package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"fx-openai/internal/openai"
)

// Smallest Ollama Cloud free-tier id we have actually called successfully.
// Override with FX_MODEL. Do not default to 70B+/subscription models in tests.
const liveDefaultModel = "gpt-oss:20b"

func liveHandler(t *testing.T) http.Handler {
	t.Helper()
	key := openai.APIKeyFromEnv(os.Getenv)
	if key == "" {
		t.Skip("set OPENAI_API_KEY or OLLAMA_API_KEY to run live tests")
	}
	base := os.Getenv("OPENAI_BASE_URL")
	if base == "" {
		base = "https://ollama.com/v1"
	}
	return New(openai.Config{BaseURL: base, APIKey: key})
}

func liveModel() string {
	if id := os.Getenv("FX_MODEL"); id != "" {
		return id
	}
	return liveDefaultModel
}

func TestLiveCatalog(t *testing.T) {
	h := liveHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/coding-agent/v1/models", nil)
	req.Header.Set("Authorization", "Bearer should-not-matter")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	var cat struct {
		Data []struct {
			ID   string   `json:"id"`
			Type string   `json:"type"`
			Tags []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.Data) == 0 {
		t.Fatal("empty catalog")
	}
	for _, m := range cat.Data {
		if m.ID == "" || m.Type != "language" || len(m.Tags) == 0 || m.Tags[0] != "tool-use" {
			t.Fatalf("bad entry %#v", m)
		}
	}
}

func TestLiveStreamSmoke(t *testing.T) {
	h := liveHandler(t)
	model := liveModel()
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{
		"prompt":[{"role":"user","content":[{"type":"text","text":"hi"}]}],
		"maxOutputTokens": 8
	}`))
	req.Header.Set(ModelIDHeader, model)
	req.Header.Set(StreamingHeader, "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"type":"finish"`) {
		t.Fatalf("no finish from %s: %s", model, body)
	}
	if !strings.Contains(body, `"type":"text-delta"`) && !strings.Contains(body, `"type":"reasoning-delta"`) {
		t.Fatalf("no deltas from %s: %s", model, body)
	}
}
