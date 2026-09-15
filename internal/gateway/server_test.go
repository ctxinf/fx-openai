package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fx-openai/internal/openai"
)

func TestHealthzAndCredits(t *testing.T) {
	h := New(openai.Config{BaseURL: "http://127.0.0.1:1/v1", APIKey: "x"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != "ok" {
		t.Fatalf("%d %q", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/coding-agent/v1/credits", nil))
	if rr.Code != 404 {
		t.Fatalf("credits %d", rr.Code)
	}
}

func TestModelsRewritesCatalogAndDoesNotForwardFxKey(t *testing.T) {
	var sawAuth, sawUA string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		sawUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"data":[{"id":"glm-5.2"},{"id":"zai/glm-5.2"}]}`))
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1", APIKey: "upstream-secret"})
	req := httptest.NewRequest(http.MethodGet, "/coding-agent/v1/models", nil)
	req.Header.Set("Authorization", "Bearer fx-dummy")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if sawAuth != "Bearer upstream-secret" {
		t.Fatalf("forwarded fx key? %q", sawAuth)
	}
	if !strings.HasPrefix(sawUA, "fx-openai/") {
		t.Fatalf("user-agent %q", sawUA)
	}
	var cat struct {
		Data []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	if len(cat.Data) != 2 || cat.Data[0].ID != "glm-5.2" || cat.Data[1].ID != "zai/glm-5.2" {
		t.Fatalf("ids rewritten? %#v", cat.Data)
	}
	if cat.Data[0].Tags[0] != "tool-use" {
		t.Fatalf("tags %#v", cat.Data[0].Tags)
	}
}

func TestLanguageModelMissingHeader(t *testing.T) {
	h := New(openai.Config{BaseURL: "http://127.0.0.1:1/v1", APIKey: "x"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{"prompt":[]}`))
	h.ServeHTTP(rr, req)
	if rr.Code != 400 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestLanguageModelStreamTranslatesSSE(t *testing.T) {
	var gotBody map[string]any
	var sawAuth string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		sawAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1", APIKey: "upstream-secret"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{
		"prompt":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))
	req.Header.Set(ModelIDHeader, "glm-5.2")
	req.Header.Set(StreamingHeader, "true")
	req.Header.Set("Authorization", "Bearer fx-dummy")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if sawAuth != "Bearer upstream-secret" {
		t.Fatalf("auth %q", sawAuth)
	}
	if gotBody["model"] != "glm-5.2" {
		t.Fatalf("upstream model %#v", gotBody["model"])
	}
	if gotBody["stream"] != true {
		t.Fatalf("stream %#v", gotBody["stream"])
	}
	msgs := gotBody["messages"].([]any)
	if msgs[0].(map[string]any)["content"] != "hi" {
		t.Fatalf("messages %#v", msgs)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"type":"text-delta"`) || !strings.Contains(body, `"delta":"Hi"`) {
		t.Fatalf("sse %s", body)
	}
	if !strings.Contains(body, `"unified":"stop"`) {
		t.Fatalf("finish %s", body)
	}
	if strings.Contains(body, "choices") {
		t.Fatalf("leaked OpenAI shape: %s", body)
	}
}

func TestLanguageModelNonStreamPassthrough(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"finish_reason":"stop","message":{"content":"pong"}}]}`)
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1", APIKey: "k"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{"prompt":[{"role":"user","content":"ping"}]}`))
	req.Header.Set(ModelIDHeader, "any/id")
	req.Header.Set(StreamingHeader, "false")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"content":"pong"`) {
		t.Fatalf("%s", rr.Body.String())
	}
}

func TestLanguageModelUpstreamErrorStatus(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad key"}}`)
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1", APIKey: "nope"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{"prompt":[{"role":"user","content":"x"}]}`))
	req.Header.Set(ModelIDHeader, "m")
	req.Header.Set(StreamingHeader, "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "bad key") {
		t.Fatalf("%s", rr.Body.String())
	}
}

func TestLanguageModelStreamTools(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c1\",\"function\":{\"name\":\"bash\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1", APIKey: "k"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{
		"prompt":[{"role":"user","content":"ls"}],
		"tools":[{"name":"bash","inputSchema":{"type":"object"}}]
	}`))
	req.Header.Set(ModelIDHeader, "m")
	req.Header.Set(StreamingHeader, "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	body := rr.Body.String()
	for _, want := range []string{
		`"type":"tool-input-start"`,
		`"type":"tool-call"`,
		`"toolCallId":"c1"`,
		`"unified":"tool-calls"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in %s", want, body)
		}
	}
}

func TestRequireLoopbackStillHolds(t *testing.T) {
	if err := RequireLoopback("0.0.0.0:1"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestLanguageModelStreamEndsWithDoneSentinel(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{
		"prompt":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))
	req.Header.Set(ModelIDHeader, "glm-5.2")
	req.Header.Set(StreamingHeader, "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("stream must end with the [DONE] sentinel, got %q", body)
	}
	if rr.Header().Get("X-Accel-Buffering") != "no" {
		t.Fatalf("X-Accel-Buffering = %q", rr.Header().Get("X-Accel-Buffering"))
	}
}

func TestLanguageModelStreamHandlesVeryLongLine(t *testing.T) {
	huge := strings.Repeat("a", 2*1024*1024)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\""+huge+"\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()

	h := New(openai.Config{BaseURL: up.URL + "/v1"})
	req := httptest.NewRequest(http.MethodPost, "/v3/ai/language-model", strings.NewReader(`{
		"prompt":[{"role":"user","content":[{"type":"text","text":"hi"}]}]
	}`))
	req.Header.Set(ModelIDHeader, "glm-5.2")
	req.Header.Set(StreamingHeader, "true")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	if !strings.Contains(body, huge) {
		t.Fatalf("long delta was truncated (body %d bytes)", len(body))
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatal("long-line stream must still terminate with [DONE]")
	}
}
