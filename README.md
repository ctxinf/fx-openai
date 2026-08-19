# fx-openai

Loopback translator so [fx](https://github.com/vercel-labs/fx) can use any OpenAI-compatible API.

```
fx  --Gateway protocol-->  127.0.0.1:8787  --OpenAI /v1-->  your provider
```

fx is not an OpenAI client. It talks Vercel’s language-model HTTP API (`/v3/ai/language-model`). Pointing `FX_GATEWAY_BASE_URL` at OpenAI, OpenRouter, xAI, Ollama, or your own host does nothing: fx ignores anything that is not loopback HTTP, and the request shape is different. This process is the missing server.

If the upstream speaks `/v1/chat/completions` and `/v1/models`, it should work. Local models, Ollama Cloud, OpenAI, OpenRouter, xAI, vLLM, llama.cpp, LM Studio, KLIA, and anything else on that API. Model ids are passed through unchanged.

## Install

Go 1.22+.

```bash
git clone https://github.com/BorjaGM1/fx-openai
cd fx-openai
go build -o fx-openai ./cmd/fx-openai
```

## Run

Terminal 1 — point `-upstream` at **your** OpenAI-compatible base (must include `/v1`):

```bash
export OPENAI_API_KEY=...          # whatever that provider expects; unused locally
./fx-openai -upstream https://openrouter.ai/api/v1
```

Terminal 2:

```bash
eval "$(fx-openai -print-env)"
export FX_MODEL=...                # an id that provider lists; or `fx models` / `/model`
fx
```

`-print-env` sets the Gateway URLs (both required, loopback only). It does not pick a model. That stays in fx (`FX_MODEL`, `/model`, `fx models`).

More detail: [HOWTO.md](HOWTO.md) or `./fx-openai -howto`.

## Example upstreams

Same process for all of these. Only the URL, key, and model id change.

| Provider | `-upstream` |
| --- | --- |
| Any OpenAI-compatible `/v1` | that base URL |
| Local Ollama | `http://127.0.0.1:11434/v1` |
| LM Studio / llama.cpp | `http://127.0.0.1:<port>/v1` |
| Ollama Cloud | `https://ollama.com/v1` (binary default) |
| OpenAI | `https://api.openai.com/v1` |
| OpenRouter | `https://openrouter.ai/api/v1` |
| xAI | `https://api.x.ai/v1` |
| vLLM | `http://127.0.0.1:<port>/v1` |

The listen address must stay loopback (`127.0.0.1`, `localhost`, `::1`). That is fx’s allowlist. The upstream may be local or remote.

## What it implements

| Method | Path | Upstream |
| --- | --- | --- |
| `GET` | `/healthz` | (none) |
| `GET` | `/coding-agent/v1/models` | `{base}/models` |
| `POST` | `/v3/ai/language-model` | `{base}/chat/completions` |
| `GET` | `/coding-agent/v1/credits` | 404 |

Streaming maps `text-delta`, `reasoning-delta`, tool-input start/delta/end, `tool-call`, `finish`. fx’s dummy `Authorization` is not forwarded.

Wire format: [PROTOCOL.md](PROTOCOL.md).

## Tests

```bash
go test ./...
```

Live tests (`-run Live`) hit a real upstream if a key is set. They skip otherwise. Do not add extra live generations.

## License

Apache-2.0.
