# AGENTS.md

Loopback translator: Vercel AI Gateway language-model HTTP in, OpenAI Chat Completions out. Read [PROTOCOL.md](PROTOCOL.md) before touching mapping code.

## Declaring work ready

1. `gofmt -w .` and `go test ./...`
2. If the change touches HTTP or mapping, keep or add a test that would have failed before the change
3. Live path (optional, costs upstream tokens): `OPENAI_API_KEY=... go test ./internal/gateway -run Live -count=1`. Keep it one small model and one short completion. Do not add extra live generations.
4. Do not claim fx itself was verified unless you ran an fx binary against `127.0.0.1:8787`

## Language

Go, this module, **stdlib only** until a dependency is justified. Do not add Chi, Gin, gorilla, SSE libraries, or the Vercel AI SDK. Do not rewrite this in Zig.

## Layout

| Path | Owns |
| --- | --- |
| `cmd/fx-openai` | flags, env, process lifetime |
| `internal/gateway` | listen policy, routes, Gateway headers |
| `internal/openai` | upstream HTTP client |
| `internal/translate` | prompt/tools/SSE mapping |

Keep Gateway types out of the OpenAI client and OpenAI wire types out of the HTTP server. `translate` is the only package that knows both.

## Rules

- Bind loopback HTTP only. Refuse `0.0.0.0`, LAN IPs, and TLS listen. Upstream URL may be remote.
- Incoming `Authorization` from fx authenticates nothing. Upstream key is `OPENAI_API_KEY` or `OLLAMA_API_KEY`.
- Model id comes from header `ai-language-model-id` and is forwarded **verbatim**. Never prefix, strip, or alias ids.
- Catalog entries must include tag `tool-use` or fx will not advertise tools.
- Do not implement credits, generation usage, or web search. 404 is correct.
- Do not loosen fx’s own loopback check. This process exists *because* of that check.
- Do not write API keys into the repo. `.env` is gitignored.
- Small diffs.

## Commands

```bash
go run ./cmd/fx-openai -listen 127.0.0.1:8787 -upstream https://ollama.com/v1
go test ./...
gofmt -w .
./fx-openai -version
./fx-openai -print-env
./fx-openai -howto
```
