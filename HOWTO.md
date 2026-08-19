# How to use fx-openai

fx talks Vercel AI Gateway. Your server talks OpenAI `/v1`. This process sits on loopback and translates.

```
fx  →  http://127.0.0.1:8787  →  OpenAI-compatible /v1
```

Works with Ollama, KLIA, vLLM, llama.cpp, or anything with `/v1/chat/completions` and `/v1/models`.

Verified with fx 0.0.3: `fx ask --no-save` against this process → Ollama Cloud `gpt-oss:20b` returned `PONG`, then a `glob_files` tool call found a workspace file.

## Configure fx (do not invent this)

There are two separate things. Only the first is this process’s job.

**Gateway URL (this process).** fx will not send traffic to a remote host. `eval` the loopback pair — never type `https://api.klia.tech` into `FX_GATEWAY_*`.

```bash
eval "$(fx-openai -print-env)"
```

That sets `FX_GATEWAY_BASE_URL`, `FX_GATEWAY_CHAT_URL` (both required), and a dummy `AI_GATEWAY_API_KEY`. It does **not** pick a model.

**Model (fx).** This process does not know your catalog. KLIA, Ollama, and a laptop llama.cpp do not share ids. Use fx:

```bash
fx models                  # list what the upstream actually has
export FX_MODEL=your-id    # process env, highest precedence
# or inside the TUI: /model
# or persist in ~/.fx/settings.json
```

fx’s built-in default is `zai/glm-5.2`. That is a Vercel catalog id. If your upstream does not have that exact string, set `FX_MODEL` (or `/model`) to one it does. Do not add `ollama/` or `openai/` prefixes unless the upstream listed them.

## Human (2 terminals)

**1. Start the translator**

```bash
cd fx-openai
go build -o fx-openai ./cmd/fx-openai
export OPENAI_API_KEY=...          # or OLLAMA_API_KEY
# local Ollama: skip the key, use -upstream http://127.0.0.1:11434/v1
./fx-openai
```

**2. Point fx at it**

```bash
eval "$(fx-openai -print-env)"
export FX_MODEL=gpt-oss:20b        # whatever `fx models` shows for YOU
fx ask --no-save -- "Reply with PONG. Do not use tools."
```

## Agent (do this, in order)

1. Go 1.22+ and an OpenAI-compatible base URL (`…/v1`).
2. `go build -o fx-openai ./cmd/fx-openai`
3. `./fx-openai -listen 127.0.0.1:8787 -upstream <OPENAI_BASE_URL>` with `OPENAI_API_KEY` or `OLLAMA_API_KEY` set (dummy `ollama` is fine locally).
4. `GET http://127.0.0.1:8787/healthz` → `ok`
5. `eval "$(fx-openai -print-env)"` — do not hand-write remote Gateway URLs.
6. `fx models` then `export FX_MODEL=<one of those ids>` if the default is wrong for this upstream.
7. `fx ask --no-save --json -- "Reply with PONG. Do not use tools."`

## Common failures

| Symptom | Cause |
| --- | --- |
| fx still hits `ai-gateway.vercel.sh` | Did not `eval "$(fx-openai -print-env)"` (missing `FX_GATEWAY_CHAT_URL`) |
| `FX_GATEWAY_BASE_URL=https://api.…` ignored | By design. Only loopback HTTP. The *upstream* flag is how you reach KLIA/Ollama |
| Model not found / 403 | fx default or `FX_MODEL` is not an id *this* upstream lists. `fx models` |
| Tools never appear | Catalog must tag `tool-use` (this process always does) |

## Flags

```
fx-openai [-listen 127.0.0.1:8787] [-upstream https://ollama.com/v1]
fx-openai -print-env
fx-openai -howto
fx-openai -version
```
