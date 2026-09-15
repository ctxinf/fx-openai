# fx-openai

A local loopback translator that lets [fx](https://github.com/vercel-labs/fx) use
any OpenAI-compatible API.

## Difference from [upstream `fx-openai`](https://github.com/BorjaGM1/fx-openai)

This fork is designed for long-running use and adds:

- Persistent configuration and a user-level systemd service
- `fx-openai fx -- ...` to launch fx with one command
- End-to-end checks via `service-test`
- A fix for truncated long SSE and tool-call streams
- Release archives for Linux, macOS, and Windows on amd64/arm64

The core purpose is unchanged: translate fx requests into OpenAI Chat
Completions requests.

## Basic usage

```bash
# Start the translator in the foreground.
OPENAI_API_KEY=... fx-openai serve -upstream https://api.openai.com/v1
```

```bash
# Create and manage the user-level service.
fx-openai service init
fx-openai service start
fx-openai service status
fx-openai service stop
fx-openai service remove
```

```bash
# Run fx with the local translator configured automatically.
fx-openai fx -- ask --no-save -- "Reply with PONG."
```

```bash
# Check the running translator end to end.
fx-openai service-test
```

## License

Apache-2.0.
