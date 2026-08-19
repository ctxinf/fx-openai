package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fx-openai/internal/gateway"
	"fx-openai/internal/openai"
	"fx-openai/internal/version"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("fx-openai: ")

	opts, err := parseArgs(os.Args[1:], os.Stderr)
	if err == flag.ErrHelp {
		return
	}
	if err != nil {
		os.Exit(2)
	}
	if opts.version {
		fmt.Println(version.String)
		return
	}
	if opts.howto {
		fmt.Print(howtoText)
		return
	}
	if err := gateway.RequireLoopback(opts.listen); err != nil {
		log.Fatal(err)
	}
	if opts.printEnv {
		fmt.Print(fxEnvBlock(opts.listen))
		return
	}

	key := openai.APIKeyFromEnv(os.Getenv)
	if key == "" {
		key = "ollama"
		log.Print("no OPENAI_API_KEY / OLLAMA_API_KEY; using dummy key \"ollama\" (fine for local Ollama)")
	}

	cfg := openai.Config{
		BaseURL: opts.upstream,
		APIKey:  key,
	}

	srv := &http.Server{
		Addr:              opts.listen,
		Handler:           gateway.New(cfg),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Printf("%s  http://%s  →  %s", version.String, opts.listen, strings.TrimRight(opts.upstream, "/"))
	log.Print("point fx at this process:")
	for _, line := range strings.Split(strings.TrimSuffix(fxEnvBlock(opts.listen), "\n"), "\n") {
		log.Print("  " + line)
	}

	err = srv.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

type options struct {
	listen   string
	upstream string
	printEnv bool
	howto    bool
	version  bool
}

func parseArgs(args []string, errOut io.Writer) (options, error) {
	fs := flag.NewFlagSet("fx-openai", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		fmt.Fprintf(errOut, `fx-openai %s

Loopback translator so fx can talk to an OpenAI-compatible server.

  fx  --Gateway protocol-->  127.0.0.1:8787  --OpenAI /v1-->  upstream

Usage:
  fx-openai [flags]

Flags:
`, version.String)
		fs.PrintDefaults()
		fmt.Fprintf(errOut, `
Gateway URLs: eval "$(fx-openai -print-env)" — never point them at the remote host.
Model: fx’s problem (FX_MODEL, /model, fx models). This process does not pick one.

  fx-openai -howto          full setup (human + agent)
  fx-openai -print-env      fx Gateway exports for this -listen

Examples:
  fx-openai
  fx-openai -upstream http://127.0.0.1:11434/v1
  fx-openai -upstream https://api.klia.tech/v1
`)
	}

	var opts options
	fs.StringVar(&opts.listen, "listen", envOr("LISTEN", "127.0.0.1:8787"), "loopback listen address")
	fs.StringVar(&opts.upstream, "upstream", envOr("OPENAI_BASE_URL", "https://ollama.com/v1"), "OpenAI-compatible base URL (include /v1)")
	fs.BoolVar(&opts.printEnv, "print-env", false, "print fx env vars for this -listen and exit")
	fs.BoolVar(&opts.howto, "howto", false, "print setup instructions and exit")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func fxEnvBlock(listen string) string {
	base := "http://" + listen
	lines := []string{
		"export FX_GATEWAY_BASE_URL=" + base,
		"export FX_GATEWAY_CHAT_URL=" + base + "/v3/ai/language-model",
		"export AI_GATEWAY_API_KEY=local",
	}
	// Never invent a model id. fx already has FX_MODEL, /model, and `fx models`.
	if model := os.Getenv("FX_MODEL"); model != "" {
		lines = append(lines, "export FX_MODEL="+shellSingleQuote(model))
	} else {
		lines = append(lines, "# model: fx default is zai/glm-5.2. Override with FX_MODEL, /model, or `fx models`.")
	}
	return strings.Join(lines, "\n") + "\n"
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

const howtoText = `fx-openai: fx (Vercel Gateway protocol) → this process on loopback → OpenAI /v1.

Do not point FX_GATEWAY_BASE_URL at the remote host. fx only accepts loopback HTTP.

Human:
  1. OPENAI_API_KEY=... ./fx-openai -upstream https://ollama.com/v1
     (local Ollama: -upstream http://127.0.0.1:11434/v1, key optional)
  2. In another terminal: eval "$(fx-openai -print-env)"
     Then pick a model the way fx already does (FX_MODEL, /model, fx models).

Agent:
  1. go build -o fx-openai ./cmd/fx-openai
  2. Start ./fx-openai -listen 127.0.0.1:8787 -upstream <base including /v1>
  3. GET http://127.0.0.1:8787/healthz → ok
  4. eval "$(fx-openai -print-env)"
  5. Optional: export FX_MODEL=<id from fx models> if you do not want fx's default
  6. fx ask --no-save --json -- "Reply with PONG. Do not use tools."

Fails:
  fx still calls ai-gateway.vercel.sh → missing FX_GATEWAY_CHAT_URL, or not loopback
  401/403 → bad OPENAI_API_KEY, or that model is paid
  model not found → fx's default / FX_MODEL is not an id on THIS upstream. Run: fx models

Full write-up: HOWTO.md
`

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
