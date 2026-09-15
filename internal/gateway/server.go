package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"fx-openai/internal/openai"
	"fx-openai/internal/translate"
)

// New returns the loopback Gateway HTTP surface.
func New(cfg openai.Config) http.Handler {
	s := &server{client: openai.New(cfg)}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /coding-agent/v1/models", s.models)
	mux.HandleFunc("POST /v3/ai/language-model", s.languageModel)
	mux.HandleFunc("GET /coding-agent/v1/credits", s.credits)
	return mux
}

type server struct {
	client *openai.Client
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *server) credits(w http.ResponseWriter, _ *http.Request) {
	http.NotFound(w, nil)
}

func (s *server) models(w http.ResponseWriter, r *http.Request) {
	resp, err := s.client.Models(r.Context())
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return
	}
	out, err := translate.Catalog(body)
	if err != nil {
		httpError(w, http.StatusBadGateway, "upstream models: "+err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(out)
}

func (s *server) languageModel(w http.ResponseWriter, r *http.Request) {
	model := r.Header.Get(ModelIDHeader)
	stream := strings.EqualFold(r.Header.Get(StreamingHeader), "true")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	req, err := translate.ChatRequest(model, stream, body)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp, err := s.client.Chat(r.Context(), req)
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		w.Header().Set("Content-Type", contentType(resp, "application/json"))
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return
	}

	if !stream {
		w.Header().Set("Content-Type", contentType(resp, "application/json"))
		_, _ = io.Copy(w, resp.Body)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	// Proxies (mitmproxy, nginx) otherwise buffer the whole body and report the
	// stream as broken instead of rendering it incrementally.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	// Flush the headers immediately so intermediaries see a live stream even
	// when the first upstream token is slow to arrive.
	if flusher != nil {
		flusher.Flush()
	}

	conv := translate.NewStream()
	// bufio.Reader has no line-length ceiling, unlike bufio.Scanner, whose
	// ErrTooLong on a large tool-call payload would truncate the stream.
	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, err := readLine(reader)
		if err != nil {
			if err != io.EOF && r.Context().Err() == nil {
				log.Printf("language-model stream read: %v", err)
				writeEvents(w, flusher, conv.Fail(err.Error()))
				writeDone(w, flusher)
				return
			}
			break
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}
		writeEvents(w, flusher, conv.Consume([]byte(data)))
	}
	writeEvents(w, flusher, conv.Close())
	writeDone(w, flusher)
}

// readLine reads one \n-terminated line of unbounded length, returning it
// without the trailing CR/LF.
func readLine(r *bufio.Reader) (string, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return "", err
		}
		if buf == nil && !isPrefix {
			return string(chunk), nil
		}
		buf = append(buf, chunk...)
		if !isPrefix {
			return string(buf), nil
		}
	}
}

// writeDone emits the SSE sentinel. Without it a proxy sees a body that simply
// stops and reports the request as dropped rather than complete.
func writeDone(w http.ResponseWriter, flusher http.Flusher) {
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if flusher != nil {
		flusher.Flush()
	}
}

func writeEvents(w http.ResponseWriter, flusher http.Flusher, events [][]byte) {
	for _, event := range events {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(event)
		_, _ = w.Write([]byte("\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func httpError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func contentType(resp *http.Response, fallback string) string {
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		return ct
	}
	return fallback
}
