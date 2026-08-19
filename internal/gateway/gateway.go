// Package gateway is the inbound Vercel AI Gateway HTTP surface.
// Routes, loopback listen policy, and Gateway request headers live here.
// It must not know OpenAI wire types.
package gateway

import (
	"fmt"
	"net"
)

// RequireLoopback rejects any listen address that is not loopback HTTP.
// fx will not send traffic anywhere else, and binding wider would expose
// the upstream key to the LAN.
func RequireLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen %q: %w", addr, err)
	}
	if port == "" {
		return fmt.Errorf("listen %q: port required", addr)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("listen must be loopback (127.0.0.1, localhost, ::1), got %q", addr)
}

// ModelIDHeader is where fx puts the model. It is not in the JSON body.
const ModelIDHeader = "ai-language-model-id"

// StreamingHeader is "true" or "false".
const StreamingHeader = "ai-language-model-streaming"
