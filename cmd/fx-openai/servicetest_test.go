package main

import (
	"strings"
	"testing"
)

func TestReadStreamRequiresDoneSentinel(t *testing.T) {
	body := "data: {\"type\":\"text-delta\",\"delta\":\"Hi\"}\n\n" +
		"data: {\"type\":\"finish\",\"finishReason\":{\"unified\":\"stop\"}}\n\n"
	if _, err := readStream(strings.NewReader(body)); err == nil {
		t.Fatal("a stream without [DONE] must fail; that is the mitmproxy symptom")
	} else if !strings.Contains(err.Error(), "[DONE]") {
		t.Fatalf("error should name the sentinel, got %v", err)
	}
}

func TestReadStreamCollectsTextAndDone(t *testing.T) {
	body := "data: {\"type\":\"text-delta\",\"delta\":\"PO\"}\n\n" +
		"data: {\"type\":\"text-delta\",\"delta\":\"NG\"}\n\n" +
		"data: [DONE]\n\n"
	got, err := readStream(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"PONG"`) {
		t.Fatalf("deltas should be joined into PONG, got %q", got)
	}
	if !strings.Contains(got, "2 event(s)") {
		t.Fatalf("event count wrong: %q", got)
	}
}

func TestReadStreamSurfacesErrorEvent(t *testing.T) {
	body := "data: {\"type\":\"error\",\"error\":{\"message\":\"upstream exploded\"}}\n\n" +
		"data: [DONE]\n\n"
	_, err := readStream(strings.NewReader(body))
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Fatalf("error event must surface, got %v", err)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("  hello  ", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := truncate("abcdefghij", 4); got != "abcd…" {
		t.Fatalf("got %q", got)
	}
}
