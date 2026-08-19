# Protocol contract

Inbound: Vercel AI Gateway / AI SDK Language Model spec v4 over HTTP.
Outbound: OpenAI Chat Completions (+ `/v1/models`).

fx will not call this process unless the origin is loopback HTTP. That is an fx security allowlist, not something this repo can override.

## Endpoints this process serves

| Method | Path | Upstream | v1 |
| --- | --- | --- | --- |
| `GET` | `/healthz` | (none) | `200` text |
| `GET` | `/coding-agent/v1/models` | `GET {base}/models` | rewrite catalog |
| `POST` | `/v3/ai/language-model` | `POST {base}/chat/completions` | translate |
| `GET` | `/coding-agent/v1/credits` | (none) | `404` |

`{base}` is `OPENAI_BASE_URL` with no trailing slash, e.g. `https://ollama.com/v1` or `http://127.0.0.1:11434/v1`.

## Inbound headers (from fx)

Required to honor:

| Header | Meaning |
| --- | --- |
| `ai-language-model-id` | OpenAI `model`. **Not in the JSON body.** |
| `ai-language-model-streaming` | `"true"` → `stream: true` |
| `ai-gateway-protocol-version` | `"0.0.1"` (accept, do not require a fork of the protocol) |
| `ai-language-model-specification-version` | `"4"` |
| `Authorization` | ignore on loopback; do not forward fx’s dummy key |

## Catalog rewrite

OpenAI:

```json
{"data":[{"id":"llama3.2","object":"model"}]}
```

fx:

```json
{"data":[{"id":"llama3.2","type":"language","tags":["tool-use"],"context_window":0,"max_tokens":0}]}
```

Pass `id` through unchanged. Always tag `tool-use`. Unknown windows may be `0`; do not invent 128k. Do not prefix ids with `ollama/` or `openai/` unless the upstream already did.

## Request body: Gateway → OpenAI

Gateway (simplified):

```json
{
  "prompt": [
    {"role":"system","content":"..."},
    {"role":"user","content":[{"type":"text","text":"..."}]},
    {"role":"assistant","content":[
      {"type":"text","text":"..."},
      {"type":"tool-call","toolCallId":"id","toolName":"bash","input":{}}
    ]},
    {"role":"tool","content":[
      {"type":"tool-result","toolCallId":"id","toolName":"bash","output":{"type":"text","value":"..."}}
    ]}
  ],
  "tools":[{"type":"function","name":"bash","description":"...","inputSchema":{}}],
  "toolChoice":{"type":"auto"},
  "maxOutputTokens": 4096
}
```

OpenAI:

```json
{
  "model": "<ai-language-model-id>",
  "messages": [
    {"role":"system","content":"..."},
    {"role":"user","content":"..."},
    {"role":"assistant","content":"...","tool_calls":[{"id":"id","type":"function","function":{"name":"bash","arguments":"{}"}}]},
    {"role":"tool","tool_call_id":"id","content":"..."}
  ],
  "tools":[{"type":"function","function":{"name":"bash","description":"...","parameters":{}}}],
  "tool_choice":"auto",
  "max_tokens": 4096,
  "stream": true
}
```

Mapping notes:

- Gateway `prompt[]` → OpenAI `messages[]`.
- User/assistant `content` is an array of parts. Flatten text parts. Drop image/file parts in v1 (fail the request if the last user message is image-only).
- Tool calls: `input` is already JSON; OpenAI `function.arguments` is a **string**.
- Tool results: `output.value` → `content`.
- Tools: `inputSchema` → `function.parameters`.
- `toolChoice.type` of `auto` / `required` / `none` maps directly. Named-tool choice can wait.
- Ignore `providerOptions`, `reasoning`, `headers`, prompt cache metadata in v1.

## SSE: OpenAI → Gateway

OpenAI chunks:

```
data: {"choices":[{"delta":{"content":"hi"}}]}
data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"id","function":{"name":"bash","arguments":"{"}}]}}]}
data: {"choices":[{"delta":{},"finish_reason":"stop"}]}
data: [DONE]
```

Gateway events fx already parses (minimum set):

```
data: {"type":"text-delta","delta":"hi"}

data: {"type":"tool-input-start","id":"id","toolName":"bash"}
data: {"type":"tool-input-delta","id":"id","delta":"{"}
data: {"type":"tool-input-end","id":"id"}
data: {"type":"tool-call","toolCallId":"id","toolName":"bash","input":{}}

data: {"type":"finish","finishReason":{"unified":"stop"}}
```

Rules:

- One OpenAI `delta.content` → one `text-delta`.
- Tool calls are incremental. Emit `tool-input-start` on first fragment with id+name, then `tool-input-delta` for argument fragments, `tool-input-end` + `tool-call` when that tool is complete (finish_reason `tool_calls` or stream end).
- `tool-call.input` must be a JSON object, not a string. If arguments are not valid JSON yet, skip `tool-call` and let fx see an error event.
- `finishReason.unified` values: `stop`, `length`, `content-filter`, `tool-calls`, `error`, `other`. Hyphens, not underscores.
- Map OpenAI `finish_reason`: `stop`→`stop`, `length`→`length`, `content_filter`→`content-filter`, `tool_calls`→`tool-calls`. Anything else → `other`.

## Non-stream POST

When `ai-language-model-streaming` is `false`, fx’s `postGatewayCompletion` expects **OpenAI-shaped JSON**, not SSE and not a Gateway event list:

```json
{"choices":[{"finish_reason":"stop","message":{"content":"...","tool_calls":[]}}]}
```

`finish_reason` here is the legacy OpenAI spelling (`tool_calls`, `content_filter`). `tool_calls[].function.arguments` is a JSON string.

Interactive fx uses streaming. Non-stream is workers (permission review, etc.). Implement streaming first; non-stream can forward the upstream JSON with almost no rewrite.

## Out of scope (v1)

- sources, files, response-metadata

Reasoning: OpenAI `delta.reasoning` / `delta.reasoning_content` → Gateway `reasoning-delta`. Needed for gpt-oss and similar. Still ignore start/end wrappers.
- credits and generation-id billing
- Vercel team headers, OIDC, prompt caching
- Binding non-loopback
- Anthropic native, Responses API, Gemini native

If an inbound field is unknown, ignore it. If an upstream event is unknown, drop it. Do not crash the stream.
