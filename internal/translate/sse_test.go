package translate

import (
	"bytes"
	"encoding/json"
	"testing"
)

func eventsOf(t *testing.T, events [][]byte) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(events))
	for _, raw := range events {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatalf("event %s: %v", raw, err)
		}
		out = append(out, m)
	}
	return out
}

func typesOf(events []map[string]any) []string {
	var types []string
	for _, e := range events {
		types = append(types, e["type"].(string))
	}
	return types
}

func TestStreamTextAndStop(t *testing.T) {
	s := NewStream()
	var got [][]byte
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"content":"Hel"}}]}`))...)
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`))...)
	if extra := s.Close(); extra != nil {
		t.Fatalf("double finish %s", extra)
	}
	ev := eventsOf(t, got)
	if want := []string{"text-delta", "text-delta", "finish"}; !equal(typesOf(ev), want) {
		t.Fatalf("types %v", typesOf(ev))
	}
	if ev[0]["delta"] != "Hel" || ev[1]["delta"] != "lo" {
		t.Fatalf("deltas %#v %#v", ev[0], ev[1])
	}
	reason := ev[2]["finishReason"].(map[string]any)
	if reason["unified"] != "stop" {
		t.Fatalf("reason %#v", reason)
	}
	usage := ev[2]["usage"].(map[string]any)
	if usage["inputTokens"].(map[string]any)["total"] != float64(3) {
		t.Fatalf("usage %#v", usage)
	}
}

func TestStreamToolCallFragments(t *testing.T) {
	s := NewStream()
	var got [][]byte
	chunks := []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"bash","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"co"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"mmand\":\"ls\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	}
	for _, c := range chunks {
		got = append(got, s.Consume([]byte(c))...)
	}
	ev := eventsOf(t, got)
	if want := []string{"tool-input-start", "tool-input-delta", "tool-input-delta", "tool-input-end", "tool-call", "finish"}; !equal(typesOf(ev), want) {
		t.Fatalf("types %v events %s", typesOf(ev), got)
	}
	if ev[0]["id"] != "c1" || ev[0]["toolName"] != "bash" {
		t.Fatalf("start %#v", ev[0])
	}
	if ev[4]["toolCallId"] != "c1" || ev[4]["toolName"] != "bash" {
		t.Fatalf("call %#v", ev[4])
	}
	input, _ := json.Marshal(ev[4]["input"])
	if string(input) != `{"command":"ls"}` {
		t.Fatalf("input %s", input)
	}
	reason := ev[5]["finishReason"].(map[string]any)
	if reason["unified"] != "tool-calls" {
		t.Fatalf("reason %#v", reason)
	}
}

func TestStreamInvalidToolJSONEmitsError(t *testing.T) {
	s := NewStream()
	var got [][]byte
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"bash","arguments":"{]"}}]}}]}`))...)
	got = append(got, s.Consume([]byte(`{"choices":[{"finish_reason":"tool_calls"}]}`))...)
	ev := eventsOf(t, got)
	var sawError, sawCall bool
	for _, e := range ev {
		if e["type"] == "error" {
			sawError = true
		}
		if e["type"] == "tool-call" {
			sawCall = true
		}
	}
	if !sawError || sawCall {
		t.Fatalf("want error without tool-call, got %v", typesOf(ev))
	}
}

func TestStreamParallelTools(t *testing.T) {
	s := NewStream()
	var got [][]byte
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"tool_calls":[
		{"index":0,"id":"a","function":{"name":"one","arguments":"{}"}},
		{"index":1,"id":"b","function":{"name":"two","arguments":"{}"}}
	]}}]}`))...)
	got = append(got, s.Consume([]byte(`{"choices":[{"finish_reason":"tool_calls"}]}`))...)
	ev := eventsOf(t, got)
	var ids []string
	for _, e := range ev {
		if e["type"] == "tool-call" {
			ids = append(ids, e["toolCallId"].(string))
		}
	}
	if !equal(ids, []string{"a", "b"}) {
		t.Fatalf("ids %v types %v", ids, typesOf(ev))
	}
}

func TestStreamFailDoesNotEmitStop(t *testing.T) {
	s := NewStream()
	_ = s.Consume([]byte(`{"choices":[{"delta":{"content":"Hi"}}]}`))
	ev := eventsOf(t, s.Fail("read failed"))
	if want := []string{"error", "finish"}; !equal(typesOf(ev), want) {
		t.Fatalf("types %v", typesOf(ev))
	}
	if ev[1]["finishReason"].(map[string]any)["unified"] != "error" {
		t.Fatalf("%v", ev[1])
	}
	if extra := s.Close(); extra != nil {
		t.Fatalf("close after fail %s", extra)
	}
}

func TestStreamDoneWithoutFinish(t *testing.T) {
	s := NewStream()
	_ = s.Consume([]byte(`{"choices":[{"delta":{"content":"x"}}]}`))
	ev := eventsOf(t, s.Close())
	if len(ev) != 1 || ev[0]["type"] != "finish" {
		t.Fatalf("%v", ev)
	}
	if ev[0]["finishReason"].(map[string]any)["unified"] != "stop" {
		t.Fatalf("%v", ev[0])
	}
}

func TestStreamFinishReasonTable(t *testing.T) {
	cases := map[string]string{
		"stop":           "stop",
		"length":         "length",
		"content_filter": "content-filter",
		"tool_calls":     "tool-calls",
		"error":          "error",
		"mystery":        "other",
	}
	for in, want := range cases {
		if got := unifiedFinish(in); got != want {
			t.Fatalf("%s → %s want %s", in, got, want)
		}
	}
}

func TestStreamDropsMalformedAndEmpty(t *testing.T) {
	s := NewStream()
	if ev := s.Consume([]byte(`not-json`)); ev != nil {
		t.Fatalf("%s", ev)
	}
	if ev := s.Consume([]byte(``)); ev != nil {
		t.Fatalf("%s", ev)
	}
	if ev := s.Consume([]byte(`[DONE]`)); ev != nil {
		t.Fatalf("%s", ev)
	}
}

func TestStreamUpstreamErrorField(t *testing.T) {
	s := NewStream()
	ev := eventsOf(t, s.Consume([]byte(`{"error":{"message":"nope"}}`)))
	if len(ev) != 1 || ev[0]["type"] != "error" {
		t.Fatalf("%v", ev)
	}
}

func TestStreamReasoningDelta(t *testing.T) {
	s := NewStream()
	var got [][]byte
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"reasoning":"think "}}]}`))...)
	got = append(got, s.Consume([]byte(`{"choices":[{"delta":{"reasoning_content":"more","content":"Hi"},"finish_reason":"stop"}]}`))...)
	ev := eventsOf(t, got)
	if want := []string{"reasoning-delta", "text-delta", "reasoning-delta", "finish"}; !equal(typesOf(ev), want) {
		t.Fatalf("types %v", typesOf(ev))
	}
	if ev[0]["delta"] != "think " || ev[2]["delta"] != "more" {
		t.Fatalf("%v", ev)
	}
}

func TestStreamSkipsEmptyContent(t *testing.T) {
	s := NewStream()
	if ev := s.Consume([]byte(`{"choices":[{"delta":{"content":""}}]}`)); ev != nil {
		t.Fatalf("empty content should not emit, got %s", ev)
	}
}

func TestWriteSSEFraming(t *testing.T) {
	var buf bytes.Buffer
	WriteSSE(&buf, []byte(`{"type":"text-delta","delta":"x"}`))
	got := buf.String()
	if got != "data: {\"type\":\"text-delta\",\"delta\":\"x\"}\n\n" {
		t.Fatalf("%q", got)
	}
}

func equal[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
