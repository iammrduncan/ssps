# Designing SSPS For 500,000 Concurrent Connections

This is the single-VPS scale target for SSPS. It is a design target, not a claim that every VPS can handle it. A tiny $5 VPS is useful for early testing, but 500,000 concurrent WebSockets needs a much larger machine, high file descriptor limits, tuned kernel queues, enough RAM, and a proxy path that has been load tested.

## Server Design

The server is shaped for mostly-idle browser tabs:

- Live presence is in memory.
- Visit writes are buffered in memory and flushed to SQLite in batches.
- SQLite uses WAL mode with app-managed checkpoints.
- The WebSocket hub tracks counts and version numbers, not one update channel per connection.
- WebSocket clients receive an initial payload, then count updates are coalesced on `SSPS_WS_UPDATE_INTERVAL`.
- A connect/disconnect no longer synchronously iterates every browser on the same site ID.

This means a single popular site ID can have a large audience without turning each connection change into a full fanout event. The tradeoff is that live counts update on an interval rather than instantly at very large scale.

## Runtime Settings

Recommended starting settings:

```bash
SSPS_FLUSH_INTERVAL=30m
SSPS_DB_CHECKPOINT_INTERVAL=5m
SSPS_DB_COMPACT_INTERVAL=24h
SSPS_WS_UPDATE_INTERVAL=30s
```

For very large rooms, keep `SSPS_WS_UPDATE_INTERVAL` at `30s` or higher. Lower values wake more connection goroutines per second and increase CPU pressure.

## SQLite Maintenance

SSPS uses:

- `PRAGMA journal_mode = WAL`
- `PRAGMA synchronous = NORMAL`
- `PRAGMA wal_autocheckpoint = 0`
- periodic `PRAGMA wal_checkpoint(PASSIVE)`
- periodic incremental vacuum plus `PRAGMA wal_checkpoint(TRUNCATE)`

The important idea is that visit events compact in memory first, then SQLite receives one batched transaction. WAL checkpointing is handled by the app on a timer so checkpoint work is less likely to land on an unlucky visit flush.

Databases created before incremental auto-vacuum was enabled are converted during compaction, which can make the first compaction heavier than later incremental runs.

Watch these files:

```bash
ls -lh /var/lib/ssps/ssps.db /var/lib/ssps/ssps.db-wal /var/lib/ssps/ssps.db-shm
```

If the WAL grows continuously, shorten `SSPS_DB_CHECKPOINT_INTERVAL` or investigate long-running readers.

## systemd Limits

Use at least:

```ini
LimitNOFILE=1048576
TasksMax=infinity
```

500,000 sockets need at least 500,000 file descriptors for SSPS, plus descriptors for the proxy, logs, SQLite, and the operating system. If Cloudflare Tunnel runs on the same VPS, `cloudflared` also needs enough descriptors.

## Kernel Starting Points

These are starting points, not magic values:

```conf
fs.file-max = 2000000
fs.nr_open = 2000000
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535
```

Apply with a file such as `/etc/sysctl.d/99-ssps.conf`, then run:

```bash
sudo sysctl --system
```

Watch current descriptor use:

```bash
sudo ls /proc/$(pgrep -x ssps)/fd | wc -l
```

## Proxy Notes

The proxy path must support long-lived WebSockets. If using Cloudflare Tunnel, load test `cloudflared` and SSPS together because both processes will hold many open connections. If using direct Cloudflare DNS to a reverse proxy, make sure the reverse proxy has its own descriptor limits and generous WebSocket read timeouts.

For Caddy or nginx, keep SSPS bound to `127.0.0.1:8080` and expose only the proxy publicly.

## What To Measure

Before trusting a 500,000-connection deployment, load test the exact VPS and proxy path and watch:

- SSPS RSS memory.
- Go goroutine count.
- File descriptor count for SSPS and the proxy.
- CPU during connect spikes.
- `/var/lib/ssps/ssps.db-wal` growth.
- SQLite flush latency.
- WebSocket disconnect and reconnect rates.
- Cloudflare 4xx/5xx rates.

## When One VPS Stops Being Enough

Move to a shared coordination layer when:

- one process cannot hold the socket count within RAM,
- live-count update lag gets too high,
- one SQLite writer cannot keep up with compacted visit flushes,
- or you need multiple regions.

The natural next pieces are Redis or NATS for live presence/fanout and a separate durable counter pipeline for visits.
