package main

import (
	"flag"
	"io"
	"strings"
	"testing"
)

func TestParseArgsDefaults(t *testing.T) {
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
	_, err := parseArgs([]string{"-h"}, io.Discard)
	if err != flag.ErrHelp {
		t.Fatalf("err %v", err)
	}
}

func TestParseArgsVersionAndPrintEnv(t *testing.T) {
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
	got := fxEnvBlock("127.0.0.1:8787")
	for _, want := range []string{
		"export FX_GATEWAY_BASE_URL=http://127.0.0.1:8787",
		"export FX_GATEWAY_CHAT_URL=http://127.0.0.1:8787/v3/ai/language-model",
		"export AI_GATEWAY_API_KEY=local",
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
	got := fxEnvBlock("127.0.0.1:8787")
	if !strings.Contains(got, "export FX_MODEL='klia/custom'") {
		t.Fatalf("%q", got)
	}
}
