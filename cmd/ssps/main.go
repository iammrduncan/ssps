package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/josephduncan/ssps/internal/counter"
	"github.com/josephduncan/ssps/internal/presence"
	"github.com/josephduncan/ssps/internal/storage"
	"github.com/josephduncan/ssps/internal/web"
)

func main() {
	if err := run(); err != nil {
		slog.Error("ssps stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	addr := env("SSPS_ADDR", ":8080")
	dbPath := env("SSPS_DB_PATH", "./data/ssps.db")
	flushInterval, err := parseDurationEnv("SSPS_FLUSH_INTERVAL", 30*time.Minute)
	if err != nil {
		return err
	}
	checkpointInterval, err := parseDurationEnv("SSPS_DB_CHECKPOINT_INTERVAL", 5*time.Minute)
	if err != nil {
		return err
	}
	compactInterval, err := parseDurationEnv("SSPS_DB_COMPACT_INTERVAL", 24*time.Hour)
	if err != nil {
		return err
	}
	webSocketUpdateInterval, err := parseDurationEnv("SSPS_WS_UPDATE_INTERVAL", 30*time.Second)
	if err != nil {
		return err
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := presence.NewHub()
	aggregator := counter.NewAggregator(store)
	go aggregator.Run(ctx, flushInterval)
	go store.RunMaintenance(ctx, checkpointInterval, compactInterval)

	server := newHTTPServer(addr, web.NewServerWithOptions(store, hub, aggregator, web.Options{
		WebSocketUpdateInterval: webSocketUpdateInterval,
	}))

	errCh := make(chan error, 1)
	go func() {
		slog.Info("ssps listening", "addr", addr, "db", dbPath, "flushInterval", flushInterval, "checkpointInterval", checkpointInterval, "compactInterval", compactInterval, "webSocketUpdateInterval", webSocketUpdateInterval)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := aggregator.Flush(context.Background()); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func parseDurationEnv(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	return time.ParseDuration(raw)
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
	}
}
