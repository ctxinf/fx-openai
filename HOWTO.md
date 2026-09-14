# How to use fx-openai

fx talks Vercel AI Gateway. Your server talks OpenAI `/v1`. This process sits on loopback and translates.

```
fx  →  http://127.0.0.1:8787  →  OpenAI-compatible /v1
```

Anything with `/v1/chat/completions` and `/v1/models`: local models, Ollama, OpenAI, OpenRouter, xAI, vLLM, llama.cpp, LM Studio, KLIA, etc.

## Configure fx (do not invent this)

There are two separate things. Only the first is this process’s job.

**Gateway URL (this process).** fx will not send traffic to a remote host. `eval` the loopback pair — never type the provider URL into `FX_GATEWAY_*`.

```bash
eval "$(fx-openai -print-env)"
```

That sets `FX_GATEWAY_BASE_URL` and `FX_GATEWAY_CHAT_URL`, and removes
Vercel-only credentials from this shell. It does **not** pick a model unless
one is configured.

The default configuration file is `~/.local/share/fx-openai/config.toml`.
Values are applied in this order: built-in defaults, TOML, environment, then
flags. For example:

```toml
listen = "127.0.0.1:8787"
base_url = "https://api.openai.com/v1"
model = "your-provider-model"
api_key = "secret:..."
```

The `api_key` field may initially be entered as `plain:your-key`. The service
commands automatically replace it with `secret:<base64>` before starting.

The model is included by `-print-env` when set. You can also run fx directly;
the child process receives the same variables without trying to modify the
parent shell:

```bash
fx-openai fx -- ask --no-save -- "Reply with PONG."
```

**Model (fx).** This process does not know your catalog. Providers do not share ids. Use fx:

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
export OPENAI_API_KEY=...          # that provider's key; skip for most local servers
./fx-openai -upstream <https://…/v1 or http://127.0.0.1:<port>/v1>
```

**2. Point fx at it**

```bash
eval "$(fx-openai -print-env)"
export FX_MODEL=...                # whatever `fx models` shows for that upstream
fx ask --no-save -- "Reply with PONG. Do not use tools."
```

## User service

Install the binary and generate the local service configuration under the XDG
user directories:

```bash
OPENAI_API_KEY=... ./install.sh -upstream https://api.openai.com/v1
fx-openai service start
fx-openai service status
```

The generated directory contains `config.toml`, `fx-openai.service`, and
`install-service`. Use `fx-openai service stop` or
`fx-openai service remove` when the
service is no longer needed. This is a `systemctl --user` service and listens
on loopback by default.

## Agent (do this, in order)

1. Go 1.22+ and an OpenAI-compatible base URL (`…/v1`).
2. `go build -o fx-openai ./cmd/fx-openai`
3. `./fx-openai -listen 127.0.0.1:8787 -upstream <OPENAI_BASE_URL>` with `OPENAI_API_KEY` or `OLLAMA_API_KEY` set (dummy `ollama` is fine locally).
4. `GET http://127.0.0.1:8787/healthz` → `ok`
5. `eval "$(fx-openai -print-env)"` — do not hand-write remote Gateway URLs.
   This also injects the local `AI_GATEWAY_API_KEY=local` placeholder required
   by newer fx versions; it is not sent upstream as the provider key.
6. `fx models` then `export FX_MODEL=<one of those ids>` if the default is wrong for this upstream.
7. `fx ask --no-save --json -- "Reply with PONG. Do not use tools."`

## Common failures

| Symptom | Cause |
| --- | --- |
| fx still hits `ai-gateway.vercel.sh` | Did not `eval "$(fx-openai -print-env)"` (missing `FX_GATEWAY_CHAT_URL`) |
| `FX_GATEWAY_BASE_URL=https://api.…` ignored | By design. Only loopback HTTP. The *upstream* flag is how you reach the provider |
| Model not found / 403 | fx default or `FX_MODEL` is not an id *this* upstream lists. `fx models` |
| Tools never appear | Catalog must tag `tool-use` (this process always does) |

## Flags

```
fx-openai [-listen 127.0.0.1:8787] [-upstream https://ollama.com/v1]
fx-openai -print-env
fx-openai fx -- ask --no-save -- "Reply with PONG."
fx-openai service init|start|stop|status|restart|remove
fx-openai -howto
fx-openai -version
```
