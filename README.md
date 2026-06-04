# SSPS

SSPS is a stupid simple presence service. It gives any site a live active visitor count and basic visit counters with one script tag.

## Run Locally

```bash
go run ./cmd/ssps
```

The server listens on `:8080` by default and stores SQLite data at `./data/ssps.db`.

Configuration:

```bash
SSPS_ADDR=:8080
SSPS_DB_PATH=./data/ssps.db
SSPS_FLUSH_INTERVAL=30m
```

## Embed

Create a site ID:

```text
GET /generate
```

Embed the generated script on any site or page that should count toward the same presence pool:

```html
<script async src="https://usessps.com/ssps.js" data-site-id="123"></script>
```

Each browser tab with the script loaded counts as one active live visitor until its WebSocket disconnects.

Site ID `0` is reserved for SSPS itself. Generated customer IDs start at `1`.

## DOM Counters

The script updates these elements when present:

```html
<span id="ssps-live-count"></span>
<span id="visit-count"></span>
<span id="unique-visit-count"></span>
```

Equivalent data attributes also work:

```html
<span data-ssps-live-count></span>
<span data-ssps-visit-count></span>
<span data-ssps-unique-visit-count></span>
```

## Programmatic API

```js
window.addEventListener("ssps:update", (event) => {
  console.log(event.detail.live)
  console.log(event.detail.totalHits)
  console.log(event.detail.uniqueVisitors)
})

const stats = window.SSPS.getStats()
```

## HTTP API

```text
GET /api/stats
GET /api/sites/{siteID}/stats
GET /healthz
```

Example per-site response:

```json
{
  "siteId": 123,
  "live": 2,
  "totalHits": 50,
  "uniqueVisitors": 12
}
```

Example network response:

```json
{
  "idsCreated": 10,
  "liveUsers": 3,
  "activeSites": 2,
  "totalVisits": 500
}
```

## Counters

Live presence is in memory. Visits are recorded into an in-memory stream and compacted into SQLite on `SSPS_FLUSH_INTERVAL`, which defaults to 30 minutes. If the process crashes, unflushed visit events can be lost by design.

## VPS Notes

For high WebSocket counts on a small Linux VPS:

- Raise the process file descriptor limit, for example with systemd `LimitNOFILE=200000`.
- Put the service behind a reverse proxy that supports WebSocket upgrades.
- Keep proxy read timeouts high enough for long-lived sockets.
- Use one process for v1. If multiple app instances are needed later, move presence fanout and live counters behind Redis, NATS, or another shared coordination layer.

## Verification

```bash
go test ./...
go build ./cmd/ssps
```
