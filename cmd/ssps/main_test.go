package main

import (
	"net/http"
	"testing"
	"time"
)

func TestHTTPServerHasDefensiveLimits(t *testing.T) {
	t.Parallel()

	server := newHTTPServer(":0", http.NotFoundHandler())

	if server.ReadHeaderTimeout < 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s, want at least 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout < 10*time.Second {
		t.Fatalf("ReadTimeout = %s, want at least 10s", server.ReadTimeout)
	}
	if server.WriteTimeout < 10*time.Second {
		t.Fatalf("WriteTimeout = %s, want at least 10s", server.WriteTimeout)
	}
	if server.IdleTimeout < time.Minute {
		t.Fatalf("IdleTimeout = %s, want at least 1m", server.IdleTimeout)
	}
	if server.MaxHeaderBytes == 0 || server.MaxHeaderBytes > 32<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want a non-zero cap no larger than 32KiB", server.MaxHeaderBytes)
	}
}
