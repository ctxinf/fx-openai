# Changelog

## 0.3.0

- Consolidate local settings into one `config.toml`
- Encrypt API keys as `secret:<base64>` and migrate `plain:` values on service init/start

## 0.2.0

- TOML configuration at `~/.local/share/fx-openai/config.toml` with explicit precedence
- `service init|start|stop|status|restart|remove` user systemd commands
- `fx` subcommand with Gateway environment and configured model injection
- User-local installer and compatibility `install-service` wrapper

## 0.1.0

First shareable release.

- HOWTO.md and `fx-openai -howto`

- Loopback Gateway surface: catalog, language-model stream/non-stream, credits 404
- OpenAI Chat Completions + `/v1/models` upstream (Ollama, KLIA, vLLM, …)
- Model ids passed through unchanged
- `fx-openai -print-env` / `-version`
- Bind loopback only
