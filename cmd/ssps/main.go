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

	server := &http.Server{
		Addr:              addr,
		Handler:           web.NewServer(store, hub, aggregator),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("ssps listening", "addr", addr, "db", dbPath, "flushInterval", flushInterval)
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
