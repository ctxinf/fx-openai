# fx-openai

Loopback translator so [fx](https://github.com/vercel-labs/fx) can use any OpenAI-compatible server.

```
fx  --Gateway protocol-->  127.0.0.1:8787  --OpenAI /v1-->  Ollama / KLIA / vLLM / …
```

fx is not an OpenAI client. It talks Vercel’s language-model HTTP API (`/v3/ai/language-model`). Setting `FX_GATEWAY_BASE_URL` to `https://api.openai.com` or your KLIA URL does nothing: fx ignores anything that is not loopback HTTP, and the request shape is different anyway. This process is the missing server.

Model ids are passed through unchanged. If the upstream lists `glm-5.2`, set `FX_MODEL=glm-5.2`. If it lists `zai/glm-5.2`, use that.

## Install

Go 1.22+.

```bash
git clone <this-repo>
cd fx-openai
go build -o fx-openai ./cmd/fx-openai
```

## Run

Terminal 1:

```bash
export OPENAI_API_KEY=...          # or OLLAMA_API_KEY; dummy "ollama" is fine locally
./fx-openai -upstream https://ollama.com/v1
```

Print the fx env, or the full setup:

```bash
./fx-openai -print-env
./fx-openai -howto
```

Step-by-step for humans and agents: [HOWTO.md](HOWTO.md).

Terminal 2:

```bash
eval "$(fx-openai -print-env)"
export FX_MODEL=gpt-oss:20b        # optional; fx already has /model and `fx models`
fx
```

`-print-env` is how you set the Gateway URLs. Do not type a remote host into `FX_GATEWAY_*`. Model selection stays in fx (`FX_MODEL`, `/model`, `fx models`). This process does not pick one.

## Upstreams

| Server | `-upstream` |
| --- | --- |
| Ollama Cloud | `https://ollama.com/v1` (default) |
| Local Ollama | `http://127.0.0.1:11434/v1` |
| KLIA | `https://api.klia.tech/v1` |
| vLLM / llama.cpp | `http://127.0.0.1:<port>/v1` |

The listen address must stay loopback (`127.0.0.1`, `localhost`, `::1`). That is fx’s allowlist, not optional. The upstream may be remote.

On Ollama Cloud, `glm-5.2` is subscription-only. Free-tier ids that have worked include `gpt-oss:20b`. Against KLIA, use whatever KLIA registered.

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

Live tests (`-run Live`) hit a real upstream, skip without a key, and use one 8-token call on `gpt-oss:20b`. Do not add more live generations.

## License

Apache-2.0.
