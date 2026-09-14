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

For a user-local binary and a user-level systemd service, run:

```bash
./install.sh -upstream https://api.openai.com/v1
fx-openai service start
```

This installs `~/.local/bin/fx-openai` and generates
`~/.local/share/fx-openai/config.toml` plus the service unit. Set
`OPENAI_API_KEY` or `OLLAMA_API_KEY` before running the installer; the key is
stored locally with mode `600`.

The TOML configuration is read with this priority: built-in defaults, then
`config.toml`, then environment variables, then command-line flags. All local
settings, including the model, base URL, and API key, are in one file:

```toml
listen = "127.0.0.1:8787"
base_url = "https://api.openai.com/v1"
model = "your-provider-model"
api_key = "secret:..."
```

For manual setup, `api_key = "plain:your-key"` is accepted. On `service init`,
`service start`, or `service restart`, plaintext values are encrypted in place
as `secret:<base64>`.

Manage the user service with the binary itself:

```bash
fx-openai service init
fx-openai service start
fx-openai service status
fx-openai service stop
fx-openai service restart
```

To run fx with the Gateway variables and configured model injected in one
command, use `fx-openai fx -- ...`:

```bash
fx-openai fx -- ask --no-save -- "Reply with PONG."
```

The old `eval "$(fx-openai -print-env)"` form remains supported and now also
prints `FX_MODEL` when it is configured. It also sets the non-empty placeholder
`AI_GATEWAY_API_KEY=local`, which newer fx versions require before they will
send a request to a loopback Gateway URL. This is not the upstream credential:
fx-openai ignores incoming Authorization and uses the `api_key` from its own
config for the OpenAI-compatible provider. `install-service` remains as a
compatibility wrapper for the same service subcommands.

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

`-print-env` sets the Gateway URLs (both required, loopback only) and the
placeholder `AI_GATEWAY_API_KEY=local` required by newer fx versions. The
placeholder is consumed only by the loopback shim; the shim ignores fx's
incoming Authorization header and authenticates upstream with `api_key` from
`config.toml`. It does not pick a model. That stays in fx (`FX_MODEL`, `/model`,
`fx models`).

More detail: [HOWTO.md](HOWTO.md) or `./fx-openai -howto`.

## Releases

Push a version tag to build and publish downloadable archives automatically:

```bash
git tag v0.3.1
git push origin v0.3.1
```

The release workflow publishes Linux, macOS, and Windows archives for amd64
and arm64, plus `SHA256SUMS`. The binary version is taken from the tag.

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
