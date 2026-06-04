# SSPS Security Review

This review focuses on what can go wrong for the current single-process SSPS server. It intentionally does not treat spoofing another site's numeric site ID as a vulnerability. SSPS has no ownership model yet, and the counters are best-effort rather than authoritative analytics.

## Threat Model

Public surfaces:

- `GET /` serves the homepage and docs.
- `GET /generate` creates a new numeric site ID.
- `GET /ssps.js` serves the embeddable browser script.
- `GET /api/stats` and `GET /api/sites/{siteID}/stats` expose JSON counters.
- `GET /ws?site-id={siteID}&visitor-id={visitorID}` accepts long-lived WebSocket presence connections and records visits.

Main risks:

- Resource exhaustion from many WebSockets, large headers, huge visitor IDs, or many unique visitor IDs before the next SQLite flush.
- Loose WebSocket protocol handling because the server implements the minimal protocol directly.
- Browser-facing hardening gaps such as missing security headers.
- Reconnect storms when many embedded clients reconnect at the same fixed interval.
- Accidental or automated `/generate` traffic creating unwanted site IDs.
- Disk growth from intentionally inflated visits or unique visitor IDs. This is related to the no-auth/no-ownership model and cannot be fully solved in-process.

## Fixed In This Pass

- Added defensive `http.Server` limits:
  - `ReadHeaderTimeout`
  - `ReadTimeout`
  - `WriteTimeout`
  - `IdleTimeout`
  - `MaxHeaderBytes`
- Cleared inherited net/http deadlines after WebSocket hijacking so long-lived sockets are not broken by regular HTTP write deadlines.
- Added browser-facing security headers:
  - `Content-Security-Policy`
  - `X-Content-Type-Options`
  - `Referrer-Policy`
  - `X-Frame-Options`
  - `Permissions-Policy`
- Marked `/generate` as `Cache-Control: no-store` and `X-Robots-Tag: noindex, nofollow` to reduce accidental indexing/caching of ID creation responses.
- Added a small in-process rate limit for `/generate` so one client cannot create unlimited site IDs in a tight loop.
- Validated WebSocket handshakes:
  - Requires `Sec-WebSocket-Version: 13`.
  - Requires a valid 16-byte base64 `Sec-WebSocket-Key`.
- Hardened WebSocket frame parsing:
  - Rejects unmasked client frames.
  - Rejects reserved bits and unsupported fragmentation.
  - Caps inbound frame payloads.
  - Enforces control frame size limits.
- Bounded visitor IDs before they reach memory or SQLite:
  - Missing visitor IDs get a generated anonymous ID.
  - Supplied visitor IDs must be 128 or fewer visible ASCII characters.
- Bounded pending counter memory between SQLite flushes:
  - Caps the number of site IDs admitted into a pending flush window.
  - Caps unique visitor IDs tracked per site in a pending flush window.
  - Hit counts continue for admitted sites even after the unique visitor cap is reached.
- Removed synchronous per-site WebSocket fanout from the hot connect/disconnect path. Presence updates are coalesced by interval so one site change does not immediately iterate every connected browser for that site.
- Added jittered exponential reconnect backoff in the embed script to reduce reconnect spikes after deploys, crashes, or proxy interruptions.

## Remaining Risks And Next Hardening

These are not fully solvable inside the current unauthenticated single-process model:

- `/generate` can still be intentionally spammed from many clients or bypassed by direct origin traffic. Keep the in-process limit, and add a Cloudflare rate limit or WAF rule on `/generate`.
- `/ws` can still be connection-flooded. Use Cloudflare protections, OS file descriptor limits, and VPS-level connection monitoring.
- Unique visitor totals can still be inflated by sending many different visitor IDs. That is accepted for now because SSPS is not meant to be authoritative analytics.
- SQLite can grow forever as total site IDs and all-time unique visitors grow. Add retention, compaction, export, or billing/tenant controls before treating this as a public multi-tenant product.
- A single process keeps live presence in memory. Multi-node deployments need a shared presence and fanout layer such as Redis or NATS.
- Keep Go, the OS, and Cloudflare Tunnel packages patched. The app has no third-party Go modules today, which keeps dependency surface small.

## Cloudflare Rules To Add

Recommended edge protections:

- Rate limit `/generate` by IP or Cloudflare bot score.
- Rate limit abnormal `/ws` connection attempts while allowing normal long-lived WebSocket traffic.
- Keep the origin private behind Cloudflare Tunnel when possible.
- If using direct DNS instead of Tunnel, firewall the VPS so only Cloudflare or the reverse proxy can reach the app.
- Alert on high 4xx/5xx rates, rapid growth in `/var/lib/ssps/ssps.db`, and live connection counts near the systemd `LimitNOFILE` ceiling.
