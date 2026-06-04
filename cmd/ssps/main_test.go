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

func TestParseMaintenanceDurationEnv(t *testing.T) {
	t.Setenv("SSPS_DB_CHECKPOINT_INTERVAL", "2m")
	t.Setenv("SSPS_DB_COMPACT_INTERVAL", "6h")
	t.Setenv("SSPS_WS_UPDATE_INTERVAL", "45s")

	checkpointInterval, err := parseDurationEnv("SSPS_DB_CHECKPOINT_INTERVAL", 5*time.Minute)
	if err != nil {
		t.Fatalf("parse checkpoint interval: %v", err)
	}
	if checkpointInterval != 2*time.Minute {
		t.Fatalf("checkpoint interval = %s, want 2m", checkpointInterval)
	}

	compactInterval, err := parseDurationEnv("SSPS_DB_COMPACT_INTERVAL", 24*time.Hour)
	if err != nil {
		t.Fatalf("parse compact interval: %v", err)
	}
	if compactInterval != 6*time.Hour {
		t.Fatalf("compact interval = %s, want 6h", compactInterval)
	}

	webSocketUpdateInterval, err := parseDurationEnv("SSPS_WS_UPDATE_INTERVAL", 30*time.Second)
	if err != nil {
		t.Fatalf("parse websocket update interval: %v", err)
	}
	if webSocketUpdateInterval != 45*time.Second {
		t.Fatalf("websocket update interval = %s, want 45s", webSocketUpdateInterval)
	}
}
