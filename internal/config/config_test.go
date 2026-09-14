package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	content := "listen = \"127.0.0.1:8787\"\n" +
		"base_url = \"https://config.example/v1\"\n" +
		"api_key = 'plain-key'\n" +
		"model = \"openai/gpt-5\" # keep model in config\n" +
		"unknown = \"ignored\"\n"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Listen != "127.0.0.1:8787" || got.BaseURL != "https://config.example/v1" || got.APIKey != "plain-key" || got.Model != "openai/gpt-5" {
		t.Fatalf("%#v", got)
	}
	if ResolvePath(path, "baseURL") != filepath.Join(dir, "baseURL") {
		t.Fatalf("resolved path %q", ResolvePath(path, "baseURL"))
	}
}

func TestLoadMissing(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != (File{}) {
		t.Fatalf("%#v", got)
	}
}

func TestAPIKeyEncoding(t *testing.T) {
	encoded, err := EncodeAPIKey("sk-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encoded == "sk-test-secret" || encoded[:7] != "secret:" {
		t.Fatalf("encoded key %q", encoded)
	}
	decoded, encrypted, err := DecodeAPIKey(encoded)
	if err != nil || !encrypted || decoded != "sk-test-secret" {
		t.Fatalf("decoded=%q encrypted=%v err=%v", decoded, encrypted, err)
	}
	plain, encrypted, err := DecodeAPIKey("plain:sk-plain")
	if err != nil || encrypted || plain != "sk-plain" {
		t.Fatalf("plain=%q encrypted=%v err=%v", plain, encrypted, err)
	}
}

func TestUpdateAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = \"m\"\napi_key = \"plain:key\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := UpdateAPIKey(path, "secret:encoded"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "model = \"m\"\napi_key = \"secret:encoded\"\n" {
		t.Fatalf("%q", got)
	}
}

func TestRemoveKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("base_url_file = \"baseURL\"\nbase_url = \"https://example/v1\"\napi_key_file = \"apikey\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveKeys(path, "base_url_file", "api_key_file"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "base_url = \"https://example/v1\"\n" {
		t.Fatalf("%q", got)
	}
}
