package translate

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"fx-openai/internal/openai"
)

func TestChatRequestBasicPrompt(t *testing.T) {
	req, err := ChatRequest("glm-5.2", true, []byte(`{
		"prompt":[
			{"role":"system","content":"you are a tool"},
			{"role":"user","content":[{"type":"text","text":"hello"}]}
		],
		"toolChoice":{"type":"auto"},
		"maxOutputTokens": 128
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Model != "glm-5.2" {
		t.Fatalf("model %q", req.Model)
	}
	if !req.Stream {
		t.Fatal("expected stream")
	}
	if req.MaxTokens == nil || *req.MaxTokens != 128 {
		t.Fatalf("max tokens %#v", req.MaxTokens)
	}
	if req.ToolChoice != "auto" {
		t.Fatalf("tool choice %#v", req.ToolChoice)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("messages %d", len(req.Messages))
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "you are a tool" {
		t.Fatalf("system %#v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "hello" {
		t.Fatalf("user %#v", req.Messages[1])
	}
}

func TestChatRequestPassesModelThroughUnchanged(t *testing.T) {
	for _, id := range []string{"glm-5.2", "zai/glm-5.2", "klia/custom", "gpt-oss:120b"} {
		req, err := ChatRequest(id, false, []byte(`{"prompt":[{"role":"user","content":"hi"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		if req.Model != id {
			t.Fatalf("want %q got %q", id, req.Model)
		}
		if req.Stream {
			t.Fatal("non-stream")
		}
	}
}

func TestChatRequestToolsAndHistory(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{
		"prompt":[
			{"role":"user","content":[{"type":"text","text":"list"}]} ,
			{"role":"assistant","content":[
				{"type":"text","text":"ok"},
				{"type":"tool-call","toolCallId":"c1","toolName":"bash","input":{"command":"ls"}}
			]},
			{"role":"tool","content":[
				{"type":"tool-result","toolCallId":"c1","toolName":"bash","output":{"type":"text","value":"a.txt"}}
			]}
		],
		"tools":[{"type":"function","name":"bash","description":"run","inputSchema":{"type":"object","properties":{"command":{"type":"string"}}}}],
		"toolChoice":{"type":"required"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "bash" {
		t.Fatalf("tools %#v", req.Tools)
	}
	var params map[string]any
	if err := json.Unmarshal(req.Tools[0].Function.Parameters, &params); err != nil {
		t.Fatal(err)
	}
	if params["type"] != "object" {
		t.Fatalf("parameters %v", params)
	}
	if req.ToolChoice != "required" {
		t.Fatalf("choice %#v", req.ToolChoice)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages %d: %#v", len(req.Messages), req.Messages)
	}
	asst := req.Messages[1]
	if asst.Content != "ok" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant %#v", asst)
	}
	if asst.ToolCalls[0].ID != "c1" || asst.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("call %#v", asst.ToolCalls[0])
	}
	if asst.ToolCalls[0].Function.Arguments != `{"command":"ls"}` {
		t.Fatalf("arguments %q", asst.ToolCalls[0].Function.Arguments)
	}
	tool := req.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "c1" || tool.Content != "a.txt" {
		t.Fatalf("tool %#v", tool)
	}
}

func TestChatRequestDropsImagesButKeepsText(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{
		"prompt":[{"role":"user","content":[
			{"type":"text","text":"what is this"},
			{"type":"file","data":"..."}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Messages[0].Content != "what is this" {
		t.Fatalf("content %q", req.Messages[0].Content)
	}
}

func TestChatRequestImageOnlyUserThenAssistantStillFails(t *testing.T) {
	_, err := ChatRequest("m", false, []byte(`{
		"prompt":[
			{"role":"user","content":[{"type":"file","mediaType":"image/png"}]},
			{"role":"assistant","content":[{"type":"text","text":"looking"}]}
		]
	}`))
	if !errors.Is(err, ErrImageOnly) {
		t.Fatalf("err %v", err)
	}
}

func TestChatRequestImageOnlyUserFails(t *testing.T) {
	_, err := ChatRequest("m", false, []byte(`{
		"prompt":[{"role":"user","content":[{"type":"file","mediaType":"image/png"}]}]
	}`))
	if !errors.Is(err, ErrImageOnly) {
		t.Fatalf("err %v", err)
	}
}

func TestChatRequestMissingModel(t *testing.T) {
	_, err := ChatRequest("  ", false, []byte(`{"prompt":[]}`))
	if !errors.Is(err, ErrMissingModel) {
		t.Fatalf("err %v", err)
	}
}

func TestChatRequestInvalidJSON(t *testing.T) {
	_, err := ChatRequest("m", false, []byte(`{`))
	if !errors.Is(err, ErrInvalidBody) {
		t.Fatalf("err %v", err)
	}
}

func TestChatRequestToolChoiceNoneAndString(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{"prompt":[],"toolChoice":{"type":"none"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoice != "none" {
		t.Fatalf("%#v", req.ToolChoice)
	}
	req, err = ChatRequest("m", false, []byte(`{"prompt":[],"toolChoice":"auto"}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoice != "auto" {
		t.Fatalf("%#v", req.ToolChoice)
	}
}

func TestChatRequestNamedToolChoiceIgnored(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{"prompt":[],"toolChoice":{"type":"tool","toolName":"bash"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.ToolChoice != nil {
		t.Fatalf("named choice should wait, got %#v", req.ToolChoice)
	}
}

func TestChatRequestIgnoresProviderOptions(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{
		"prompt":[{"role":"user","content":"hi"}],
		"providerOptions":{"gateway":{"speed":"fast"}},
		"reasoning":"high",
		"headers":{"user-agent":"fx"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if req.Messages[0].Content != "hi" {
		t.Fatal(req.Messages[0].Content)
	}
}

func TestChatRequestMultipleToolResults(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{
		"prompt":[{"role":"tool","content":[
			{"type":"tool-result","toolCallId":"a","output":{"type":"text","value":"1"}},
			{"type":"tool-result","toolCallId":"b","output":{"type":"text","value":"2"}}
		]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("got %d", len(req.Messages))
	}
	if req.Messages[0].ToolCallID != "a" || req.Messages[1].ToolCallID != "b" {
		t.Fatalf("%#v", req.Messages)
	}
}

func TestChatRequestEmptyToolSchema(t *testing.T) {
	req, err := ChatRequest("m", false, []byte(`{"prompt":[],"tools":[{"name":"ping"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(req.Tools[0].Function.Parameters) != `{"type":"object","properties":{}}` {
		t.Fatalf("%s", req.Tools[0].Function.Parameters)
	}
}

func TestChatRequestRoundTripJSON(t *testing.T) {
	req, err := ChatRequest("id/with/slash", true, []byte(`{
		"prompt":[{"role":"user","content":[{"type":"text","text":"x"}]}],
		"tools":[{"name":"t","inputSchema":{"type":"object"}}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var again openai.ChatRequest
	if err := json.Unmarshal(raw, &again); err != nil {
		t.Fatal(err)
	}
	if again.Model != "id/with/slash" {
		t.Fatal(again.Model)
	}
	if !strings.Contains(string(raw), `"model":"id/with/slash"`) {
		t.Fatalf("wire %s", raw)
	}
}
