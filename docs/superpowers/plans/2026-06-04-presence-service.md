# SSPS Presence Service Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a working Go scaffold for the SSPS presence and visit counter service.

**Architecture:** One Go binary serves the public website, generated embed script, JSON APIs, and WebSocket presence endpoint. SQLite stores generated site IDs and compacted counters, while live presence and unflushed visit events stay in memory.

**Tech Stack:** Go, `net/http`, SQLite, WebSockets, HTML templates, JavaScript embed script, Go tests.

---

## File Structure

- `go.mod`: module metadata and third-party dependencies.
- `cmd/ssps/main.go`: process entrypoint, configuration, dependency wiring, graceful shutdown.
- `internal/storage/storage.go`: SQLite connection, schema migration, site ID creation, counter writes, and stats reads.
- `internal/presence/presence.go`: in-memory connection registry and per-site subscriber broadcasts.
- `internal/counter/counter.go`: visit event aggregation and flush loop.
- `internal/web/server.go`: HTTP server construction and route handlers.
- `internal/web/assets.go`: homepage template and embeddable script response.
- `internal/*/*_test.go`: focused tests for behavior.
- `README.md`: local development, API, embed, and deployment notes.

## Task 1: Storage

**Files:**
- Create: `go.mod`
- Create: `internal/storage/storage.go`
- Create: `internal/storage/storage_test.go`

- [ ] Write failing tests that open a temporary SQLite database, migrate it, create two site IDs, record compacted hits and visitor IDs, and assert generated IDs and stats.
- [ ] Run `go test ./internal/storage -run Test -count=1` and confirm it fails because storage code is missing.
- [ ] Implement SQLite open, migration, `CreateSite`, `Stats`, `SiteStats`, and `ApplyVisitBatch`.
- [ ] Run `go test ./internal/storage -run Test -count=1` and confirm it passes.

## Task 2: Presence Hub

**Files:**
- Create: `internal/presence/presence.go`
- Create: `internal/presence/presence_test.go`

- [ ] Write failing tests for register, unregister, live user totals, active site totals, and per-site broadcasts.
- [ ] Run `go test ./internal/presence -run Test -count=1` and confirm it fails because presence code is missing.
- [ ] Implement a mutex-protected hub with connection IDs, per-site counts, and buffered subscriber channels.
- [ ] Run `go test ./internal/presence -run Test -count=1` and confirm it passes.

## Task 3: Counter Aggregator

**Files:**
- Create: `internal/counter/counter.go`
- Create: `internal/counter/counter_test.go`

- [ ] Write failing tests for recording hits, compacting visitor IDs, flushing to storage, and keeping new events separate from flushed events.
- [ ] Run `go test ./internal/counter -run Test -count=1` and confirm it fails because counter code is missing.
- [ ] Implement the aggregator with `Record`, `Flush`, `Run`, and `Stop`.
- [ ] Run `go test ./internal/counter -run Test -count=1` and confirm it passes.

## Task 4: HTTP and Script

**Files:**
- Create: `internal/web/server.go`
- Create: `internal/web/assets.go`
- Create: `internal/web/server_test.go`

- [ ] Write failing tests for `/healthz`, `/`, `/generate`, `/ssps.js`, `/api/stats`, and `/api/sites/{id}/stats`.
- [ ] Run `go test ./internal/web -run Test -count=1` and confirm it fails because web code is missing.
- [ ] Implement the router, HTML responses, JSON responses, generated script, and WebSocket handler.
- [ ] Run `go test ./internal/web -run Test -count=1` and confirm it passes.

## Task 5: Entrypoint and Docs

**Files:**
- Create: `cmd/ssps/main.go`
- Create: `README.md`

- [ ] Wire config, storage, presence, counter, web server, shutdown, and logging in `cmd/ssps/main.go`.
- [ ] Document the embed snippet, API routes, local commands, and VPS notes in `README.md`.
- [ ] Run `go test ./...` and confirm all tests pass.
- [ ] Run `go build ./cmd/ssps` and confirm the binary builds.

## Self-Review

- Spec coverage: The tasks cover site ID generation, the website, embed script, WebSocket presence, programmatic stats, DOM updates, compacted visit counters, network stats, and deployment notes.
- Placeholder scan: This plan contains no placeholder markers and no deferred feature promises.
- Type consistency: Planned package boundaries use the same storage, presence, counter, and web names throughout.
