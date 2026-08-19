package gateway

import "testing"

func TestRequireLoopback(t *testing.T) {
	ok := []string{"127.0.0.1:8787", "localhost:8787", "[::1]:8787"}
	for _, addr := range ok {
		if err := RequireLoopback(addr); err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
	}
	bad := []string{"0.0.0.0:8787", "192.168.1.2:8787", "example.com:8787", "127.0.0.1"}
	for _, addr := range bad {
		if err := RequireLoopback(addr); err == nil {
			t.Fatalf("%s: expected error", addr)
		}
	}
}
