# Contributing

This is a small Go translator. Keep it that way.

- Go 1.22+, stdlib only
- Loopback listen only
- Model ids are opaque strings. Do not rewrite them
- Mapping contract is [PROTOCOL.md](PROTOCOL.md)
- Mapping and tool-call tests belong in httptest. Live tests stay one small model, one short completion

```bash
gofmt -w .
go test ./...
go build -o fx-openai ./cmd/fx-openai
./fx-openai -version
```
