package openai

import "testing"

func TestAPIKeyFromEnv(t *testing.T) {
	env := map[string]string{"OLLAMA_API_KEY": "ollama-key"}
	getenv := func(k string) string { return env[k] }
	if got := APIKeyFromEnv(getenv); got != "ollama-key" {
		t.Fatalf("got %q", got)
	}
	env["OPENAI_API_KEY"] = "openai-key"
	if got := APIKeyFromEnv(getenv); got != "openai-key" {
		t.Fatalf("openai should win, got %q", got)
	}
}
