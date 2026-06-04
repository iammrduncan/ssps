# SSPS Presence Service Design

## Summary

SSPS is a small Go service that serves a public homepage, generates numeric site IDs, serves an embeddable browser script, accepts long-lived WebSocket connections, and reports live presence plus visit counters for each site ID.

The first version is optimized for a single cheap VPS: one Go process, in-memory live presence, SQLite for durable site IDs and compacted counters, and a buffered in-memory visit stream that flushes to SQLite every 30 minutes. The design keeps interfaces narrow so a later multi-node version can replace the in-memory hub with Redis, NATS, or another coordination layer.

## Goals

- Generate numeric site IDs at `/generate` using SQLite integer primary keys.
- Serve an embeddable script at `/ssps.js`.
- Count each loaded browser tab/session as one active live visitor while its WebSocket is connected.
- Let users read live and visit counts programmatically.
- Let users opt into automatic DOM updates for common live and visit counter elements.
- Track total hits and all-time unique visitors per site ID.
- Batch visit events in memory and periodically compact them into SQLite.
- Show network-wide stats on the homepage:
  - IDs created.
  - Current users across all sites.
  - Current site IDs actively being viewed.
  - Total visits across the network.
- Keep the v1 deployment simple enough for a $5 DigitalOcean VPS.

## Non-Goals

- Authentication, billing, dashboards, tenants, ownership checks, or site ID validation.
- Durable per-visit event storage.
- Multi-node live presence consistency in v1.
- Bot filtering, fraud prevention, or analytics segmentation.
- A styled marketing site beyond a useful homepage and quick docs.

## Architecture

The service is a single Go binary:

- `cmd/ssps`: process entrypoint and HTTP server startup.
- `internal/storage`: SQLite schema, generated site IDs, compacted counters, and global stored stats.
- `internal/presence`: in-memory site connection registry and broadcast fanout.
- `internal/counter`: in-memory visit aggregation stream and periodic SQLite flush.
- `internal/web`: HTTP handlers, homepage, `/generate`, `/ssps.js`, `/api/sites/{id}/stats`, `/api/stats`, and `/ws`.

Go is the default runtime because long-lived mostly-idle WebSocket connections fit Go's goroutine model well, the deployment artifact is simple, and the standard HTTP server is a good base for this service. SQLite runs in WAL mode so readers and the single batched writer can coexist cleanly.

## Data Model

SQLite tables:

```sql
CREATE TABLE IF NOT EXISTS sites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS site_counters (
  site_id INTEGER PRIMARY KEY,
  total_hits INTEGER NOT NULL DEFAULT 0,
  unique_visitors INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS site_visitors (
  site_id INTEGER NOT NULL,
  visitor_id TEXT NOT NULL,
  first_seen_at TEXT NOT NULL,
  PRIMARY KEY (site_id, visitor_id)
);
```

SQLite signed row IDs support values far beyond trillions, so numeric site IDs are enough for the stated scale target.

## Runtime Flow

1. A user visits `/generate`.
2. The server inserts a row into `sites` and returns a snippet using that numeric ID.
3. A customer embeds:

   ```html
   <script async src="https://usessps.com/ssps.js" data-site-id="123"></script>
   ```

4. The script creates or reuses a browser-local visitor ID.
5. The script opens `wss://usessps.com/ws?site-id=123&visitor-id=<id>`.
6. The server registers one live connection for the site ID.
7. The counter stream records one hit and the visitor ID for unique counting.
8. The server broadcasts updated stats to all active connections for that site ID.
9. The script updates default DOM targets when present and dispatches a browser event for programmatic consumers.
10. When the socket closes, the server removes that connection and broadcasts updated live stats.

## Browser Script API

The script supports the simplest embed and a small programmatic API.

Default DOM targets:

- `#ssps-live-count`
- `[data-ssps-live-count]`
- `#visit-count`
- `[data-ssps-visit-count]`
- `#unique-visit-count`
- `[data-ssps-unique-visit-count]`

Programmatic surface:

```js
window.SSPS.getStats()
window.addEventListener("ssps:update", (event) => {
  console.log(event.detail.live, event.detail.totalHits, event.detail.uniqueVisitors)
})
```

The script uses `data-site-id` on the current script tag. It also accepts `?site-id=123` in the script URL for sites that prefer query-string configuration.

## HTTP API

- `GET /`: homepage and docs.
- `GET /generate`: creates a site ID and shows the script snippet.
- `GET /ssps.js`: browser script.
- `GET /api/stats`: network-wide JSON stats.
- `GET /api/sites/{siteID}/stats`: per-site JSON stats.
- `GET /ws?site-id={siteID}&visitor-id={visitorID}`: WebSocket presence connection.
- `GET /healthz`: health check.

Per-site JSON shape:

```json
{
  "siteId": 123,
  "live": 2,
  "totalHits": 50,
  "uniqueVisitors": 12
}
```

Network JSON shape:

```json
{
  "idsCreated": 10,
  "liveUsers": 3,
  "activeSites": 2,
  "totalVisits": 500
}
```

## Counter Flush

The counter aggregator accepts visit events into memory and periodically compacts them into per-site totals:

- `hits`: number of events per site since last flush.
- `visitor_ids`: unique visitor IDs observed per site since last flush.

Every 30 minutes in production, the aggregator writes one compact transaction:

- Insert missing `(site_id, visitor_id)` rows with `INSERT OR IGNORE`.
- Increment `site_counters.total_hits` by the compacted hit count.
- Recompute or increment `site_counters.unique_visitors` by the number of newly inserted unique visitor rows.

The flush interval is configurable so tests and local development can use shorter intervals. A process crash can lose unflushed visit events; that is acceptable for v1.

## Error Handling

- Missing or non-numeric site IDs return `400` for API and WebSocket requests.
- Unknown site IDs are accepted because v1 intentionally does not validate ownership or existence.
- WebSocket clients receive best-effort updates; slow or broken clients are disconnected.
- Counter flush errors are logged and the process continues.
- SQLite open and migration errors fail startup.

## Deployment Notes

The service listens on `SSPS_ADDR`, defaulting to `:8080`, and writes SQLite to `SSPS_DB_PATH`, defaulting to `./data/ssps.db`.

For large socket counts on Linux, deployment docs should call out:

- Raising file descriptor limits with systemd `LimitNOFILE`.
- Ensuring the reverse proxy supports WebSocket upgrades.
- Keeping proxy read timeouts high enough for long-lived sockets.
- Adding Redis or NATS later if multiple app instances need shared live counts.

## Testing

The scaffold should include tests for:

- SQLite migrations, site ID generation, and compacted counter updates.
- In-memory presence connect/disconnect counts.
- Counter aggregation and flush behavior.
- HTTP route behavior for homepage, generation, stats, script, and health.
- WebSocket connect lifecycle enough to prove live counts change.

Fresh verification before completion must include:

```bash
go test ./...
go build ./cmd/ssps
```

