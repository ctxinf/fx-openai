package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"fx-openai/internal/gateway"
)

// runServiceTestCmd exercises a running serve process over the loopback
// Gateway surface: the same path fx uses, so a failure here is a real failure
// for fx. It deliberately does not talk to the upstream directly.
func runServiceTestCmd(args []string) error {
	fs := flag.NewFlagSet("fx-openai service-test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var (
		addr    string
		model   string
		prompt  string
		timeout time.Duration
	)
	fs.StringVar(&addr, "addr", "", "gateway address to test (default: configured listen)")
	fs.StringVar(&model, "model", "", "model id (default: configured model, else first from catalog)")
	fs.StringVar(&prompt, "prompt", "Reply with PONG and nothing else.", "prompt to send")
	fs.DurationVar(&timeout, "timeout", 60*time.Second, "per-request timeout")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Call the running fx-openai translator end to end.")
		fmt.Fprintln(os.Stderr, "\nChecks: /healthz, model catalog, non-streaming completion, streaming completion.")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	opts, err := parseArgs(nil, os.Stderr)
	if err != nil {
		return err
	}
	if addr == "" {
		addr = opts.listen
	}
	if model == "" {
		model = opts.model
	}

	base := "http://" + addr
	client := &http.Client{Timeout: timeout}
	ctx := context.Background()
	fmt.Printf("gateway     %s\n", base)

	failures := 0
	step := func(name string, fn func() (string, error)) {
		detail, err := fn()
		if err != nil {
			failures++
			fmt.Printf("%-11s FAIL  %v\n", name, err)
			return
		}
		fmt.Printf("%-11s ok    %s\n", name, detail)
	}

	step("healthz", func() (string, error) { return checkHealthz(ctx, client, base) })

	step("models", func() (string, error) {
		ids, err := fetchModels(ctx, client, base)
		if err != nil {
			return "", err
		}
		if model == "" && len(ids) > 0 {
			model = ids[0]
		}
		return fmt.Sprintf("%d model(s)", len(ids)), nil
	})

	if model == "" {
		fmt.Println("completion  SKIP  no model configured and catalog was empty; pass -model")
		failures++
	} else {
		fmt.Printf("model       %s\n", model)
		step("completion", func() (string, error) {
			return checkCompletion(ctx, client, base, model, prompt, false)
		})
		step("streaming", func() (string, error) {
			return checkCompletion(ctx, client, base, model, prompt, true)
		})
	}

	if failures > 0 {
		return fmt.Errorf("%d check(s) failed", failures)
	}
	fmt.Println("\nall checks passed")
	return nil
}

func checkHealthz(ctx context.Context, client *http.Client, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w (is `fx-openai serve` running?)", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return strings.TrimSpace(string(body)), nil
}

func fetchModels(ctx context.Context, client *http.Client, base string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/coding-agent/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode catalog: %w", err)
	}
	ids := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// checkCompletion posts a one-turn prompt and reports the text it got back.
// In streaming mode it also asserts the [DONE] sentinel is present, since a
// stream that ends without one is what breaks intermediaries like mitmproxy.
func checkCompletion(ctx context.Context, client *http.Client, base, model, prompt string, stream bool) (string, error) {
	payload := map[string]any{
		"prompt": []any{
			map[string]any{
				"role":    "user",
				"content": []any{map[string]any{"type": "text", "text": prompt}},
			},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v3/ai/language-model", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(gateway.ModelIDHeader, model)
	req.Header.Set(gateway.StreamingHeader, boolHeader(stream))

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, truncate(strings.TrimSpace(string(raw)), 300))
	}
	if !stream {
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("non-stream, %d bytes", len(raw)), nil
	}
	return readStream(resp.Body)
}

func readStream(r io.Reader) (string, error) {
	var (
		text     strings.Builder
		events   int
		sawDone  bool
		sawError string
	)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			sawDone = true
			continue
		}
		events++
		var event struct {
			Type  string          `json:"type"`
			Delta string          `json:"delta"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		switch event.Type {
		case "text-delta":
			text.WriteString(event.Delta)
		case "error":
			sawError = truncate(string(event.Error), 200)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read stream: %w", err)
	}
	if sawError != "" {
		return "", fmt.Errorf("stream error event: %s", sawError)
	}
	if !sawDone {
		return "", fmt.Errorf("stream ended without [DONE] after %d event(s); proxies will report this as a dropped request", events)
	}
	return fmt.Sprintf("%d event(s), [DONE] seen, text %q", events, truncate(text.String(), 60)), nil
}

func boolHeader(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
