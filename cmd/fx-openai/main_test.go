package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseArgsDefaults(t *testing.T) {
	useMissingConfig(t)
	opts, err := parseArgs(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.listen != "127.0.0.1:8787" {
		t.Fatalf("listen %q", opts.listen)
	}
	if opts.upstream != "https://ollama.com/v1" {
		t.Fatalf("upstream %q", opts.upstream)
	}
}

func TestParseArgsHelp(t *testing.T) {
	useMissingConfig(t)
	_, err := parseArgs([]string{"-h"}, io.Discard)
	if err != flag.ErrHelp {
		t.Fatalf("err %v", err)
	}
}

func TestParseArgsVersionAndPrintEnv(t *testing.T) {
	useMissingConfig(t)
	opts, err := parseArgs([]string{"-version"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.version {
		t.Fatal("expected version")
	}
	opts, err = parseArgs([]string{"-print-env", "-listen", "127.0.0.1:9"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.printEnv || opts.listen != "127.0.0.1:9" {
		t.Fatalf("%#v", opts)
	}
	opts, err = parseArgs([]string{"-howto"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.howto {
		t.Fatal("expected howto")
	}
}

func TestFxEnvBlockDoesNotInventAModel(t *testing.T) {
	t.Setenv("FX_MODEL", "")
	got := fxEnvBlock(options{listen: "127.0.0.1:8787"})
	for _, want := range []string{
		"export FX_GATEWAY_BASE_URL=http://127.0.0.1:8787",
		"export FX_GATEWAY_CHAT_URL=http://127.0.0.1:8787/v3/ai/language-model",
		"export AI_GATEWAY_API_KEY=local",
		"unset VERCEL_OIDC_TOKEN",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "export FX_MODEL=") {
		t.Fatalf("must not invent FX_MODEL: %q", got)
	}
}

func TestFxEnvBlockKeepsExistingModel(t *testing.T) {
	t.Setenv("FX_MODEL", "klia/custom")
	got := fxEnvBlock(options{listen: "127.0.0.1:8787", model: "klia/custom"})
	if !strings.Contains(got, "export FX_MODEL='klia/custom'") {
		t.Fatalf("%q", got)
	}
}

func TestConfigPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("listen = \"127.0.0.1:9000\"\nbase_url = \"https://config.example/v1\"\nmodel = \"config-model\"\napi_key = \"config-key\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FX_OPENAI_CONFIG", path)
	t.Setenv("LISTEN", "127.0.0.1:9001")
	t.Setenv("FX_MODEL", "env-model")
	opts, err := parseArgs([]string{"-upstream", "https://arg.example/v1", "-model", "arg-model"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if opts.listen != "127.0.0.1:9001" || opts.upstream != "https://arg.example/v1" || opts.model != "arg-model" || opts.apiKey != "config-key" {
		t.Fatalf("%#v", opts)
	}
}

func TestRunFXInjectsGatewayAndConfiguredModel(t *testing.T) {
	dir := t.TempDir()
	fxPath := filepath.Join(dir, "fx")
	output := filepath.Join(dir, "env")
	if err := os.WriteFile(fxPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$FX_GATEWAY_BASE_URL\" \"$FX_GATEWAY_CHAT_URL\" \"$AI_GATEWAY_API_KEY\" \"$VERCEL_OIDC_TOKEN\" \"$FX_MODEL\" >\"$FX_TEST_OUTPUT\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("listen = \"127.0.0.1:8788\"\nmodel = \"config-model\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FX_OPENAI_CONFIG", configPath)
	t.Setenv("FX_TEST_OUTPUT", output)
	t.Setenv("AI_GATEWAY_API_KEY", "vercel-key-must-not-leak")
	t.Setenv("VERCEL_OIDC_TOKEN", "oidc-token-must-not-leak")
	if err := runFX([]string{"--", "ask"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	want := "http://127.0.0.1:8788\nhttp://127.0.0.1:8788/v3/ai/language-model\nlocal\n\nconfig-model\n"
	if string(got) != want {
		t.Fatalf("env %q, want %q", got, want)
	}
}

func TestEncryptConfigAPIKeyMigratesLegacyFiles(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("base_url_file = \"baseURL\"\napi_key_file = \"apikey\"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "baseURL"), []byte("https://legacy.example/v1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apikey"), []byte("legacy-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FX_OPENAI_CONFIG", configPath)
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OLLAMA_API_KEY", "")
	t.Setenv("OPENAI_BASE_URL", "")
	opts, err := parseArgs(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := encryptConfigAPIKey(opts); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "base_url = \"https://legacy.example/v1\"") || !strings.Contains(string(got), "api_key = \"secret:") || strings.Contains(string(got), "base_url_file") || strings.Contains(string(got), "api_key_file") {
		t.Fatalf("migrated config %q", got)
	}
}

func useMissingConfig(t *testing.T) {
	t.Helper()
	t.Setenv("FX_OPENAI_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("LISTEN", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OLLAMA_API_KEY", "")
	t.Setenv("FX_MODEL", "")
}
