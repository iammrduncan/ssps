# Deploy SSPS Behind Cloudflare On A VPS

This guide deploys SSPS as one Linux service on a VPS, then routes `https://usessps.com` through Cloudflare to that service. Cloudflare is the DNS/proxy/tunnel layer. The VPS can be any provider that gives you a Linux VM.

Recommended path:

1. Run SSPS locally on the VPS at `127.0.0.1:8080`.
2. Run SSPS under `systemd` so it starts after reboot and restarts after crashes.
3. Route Cloudflare to the VPS with Cloudflare Tunnel.
4. Keep SQLite data on a persistent disk path such as `/var/lib/ssps/ssps.db`.

Cloudflare Tunnel is the easiest Cloudflare-to-VPS route because `cloudflared` opens outbound connections to Cloudflare and maps a public hostname to a local service. That means you do not need to expose inbound ports on the VPS for SSPS.

## Build The Server

On the VPS, install Go, SQLite, and the SQLite C library headers. On Ubuntu or Debian:

```bash
sudo apt-get update
sudo apt-get install -y build-essential golang sqlite3 libsqlite3-0 libsqlite3-dev
```

Clone the repo and build the binary:

```bash
git clone https://github.com/iammrduncan/ssps.git
cd ssps
go test ./...
go build -o ssps ./cmd/ssps
```

Install it into `/opt/ssps`:

```bash
sudo mkdir -p /opt/ssps /var/lib/ssps
sudo cp ./ssps /opt/ssps/ssps
sudo useradd --system --home /var/lib/ssps --shell /usr/sbin/nologin ssps || true
sudo chown -R ssps:ssps /var/lib/ssps
sudo chmod 755 /opt/ssps/ssps
```

## Run SSPS With systemd

Create `/etc/systemd/system/ssps.service`:

```ini
[Unit]
Description=SSPS presence service
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=ssps
Group=ssps
WorkingDirectory=/var/lib/ssps
ExecStart=/opt/ssps/ssps
Environment=SSPS_ADDR=127.0.0.1:8080
Environment=SSPS_DB_PATH=/var/lib/ssps/ssps.db
Environment=SSPS_FLUSH_INTERVAL=30m
Restart=on-failure
RestartSec=5s
StartLimitIntervalSec=60
StartLimitBurst=10
LimitNOFILE=200000
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=true
ProtectSystem=strict
ReadWritePaths=/var/lib/ssps

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ssps
sudo systemctl status ssps
```

Check logs:

```bash
journalctl -u ssps -f
```

Check the local health endpoint:

```bash
curl -fsS http://127.0.0.1:8080/healthz
```

Why this survives routine failures:

- `systemctl enable` starts SSPS on boot.
- `Restart=on-failure` restarts SSPS after crashes and non-zero exits.
- `RestartSec=5s` avoids a tight restart loop.
- `StartLimitIntervalSec` and `StartLimitBurst` cap runaway restarts.
- `LimitNOFILE=200000` gives WebSocket-heavy workloads room for many open connections.
- SQLite data lives in `/var/lib/ssps`, not inside the repo checkout.

If you change the binary later:

```bash
go build -o ssps ./cmd/ssps
sudo install -m 755 ./ssps /opt/ssps/ssps
sudo systemctl restart ssps
journalctl -u ssps -n 50 --no-pager
```

## Route Cloudflare With Cloudflare Tunnel

This is the recommended route for early SSPS hosting.

In Cloudflare Zero Trust:

1. Go to `Networks` -> `Tunnels`.
2. Create a tunnel named `ssps`.
3. Install `cloudflared` on the VPS using the command Cloudflare provides.
4. Add a public hostname:
   - Hostname: `usessps.com`
   - Service type: `HTTP`
   - Service URL: `http://localhost:8080`
5. Save the route.

Cloudflare will serve `https://usessps.com` publicly and forward requests to SSPS on the VPS. SSPS serves the homepage, `/generate`, `/ssps.js`, `/api/*`, and `/ws` from the same process, so one hostname route is enough.

Verify from your laptop:

```bash
curl -fsS https://usessps.com/healthz
curl -fsS https://usessps.com/api/stats
```

Verify the embed script:

```html
<script async src="https://usessps.com/ssps.js" data-site-id="123"></script>
```

Cloudflare supports proxied WebSocket connections, so `/ws?site-id=123&visitor-id=...` should work through the same hostname.

Check `cloudflared` on the VPS:

```bash
sudo systemctl status cloudflared
journalctl -u cloudflared -f
```

If Cloudflare generated a systemd service for `cloudflared`, make sure it is enabled:

```bash
sudo systemctl enable --now cloudflared
```

## Alternative: Direct Cloudflare DNS To The VPS

Use this if you want the VPS to accept inbound HTTP/HTTPS directly.

Run SSPS on localhost, then put a reverse proxy such as Caddy or nginx on ports `80` and `443`.
Keep SSPS bound to `127.0.0.1`; expose only the reverse proxy or Cloudflare Tunnel publicly.

Example Caddyfile:

```caddyfile
usessps.com {
	reverse_proxy 127.0.0.1:8080
}
```

Cloudflare DNS:

1. Create an `A` record:
   - Name: `usessps.com` or `@`
   - IPv4 address: your VPS public IPv4
   - Proxy status: `Proxied`
2. Optional: create an `AAAA` record for your VPS public IPv6.
3. In `SSL/TLS`, use `Full (strict)` once the origin has a valid certificate.
4. In `Network`, keep WebSockets enabled.

VPS firewall:

```bash
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw enable
```

With this route, Cloudflare connects to the reverse proxy, and the reverse proxy connects to SSPS at `127.0.0.1:8080`.

## Operational Checks

After a deploy:

```bash
systemctl is-active ssps
curl -fsS http://127.0.0.1:8080/healthz
curl -fsS https://usessps.com/healthz
curl -fsS https://usessps.com/api/stats
```

Simulate a crash restart:

```bash
sudo systemctl kill -s SIGKILL ssps
sleep 10
systemctl is-active ssps
journalctl -u ssps -n 50 --no-pager
```

Simulate a reboot:

```bash
sudo reboot
```

After the VPS comes back:

```bash
systemctl is-active ssps
systemctl is-active cloudflared
curl -fsS https://usessps.com/healthz
```

If `ssps` is inactive:

```bash
journalctl -u ssps -n 100 --no-pager
systemctl status ssps
```

## Backups

Back up SQLite from `/var/lib/ssps`. The safest simple backup is SQLite's online backup command:

```bash
sudo sqlite3 /var/lib/ssps/ssps.db ".backup '/var/lib/ssps/ssps-$(date +%Y%m%d-%H%M%S).db'"
```

Copy backup files off the VPS with `scp`, `rsync`, or your provider's snapshot system.

## Notes For Scale

SSPS keeps live presence in memory. A single process can handle many mostly-idle WebSockets, but the practical limit depends on VPS RAM, CPU, kernel limits, and Cloudflare connection behavior.

For more headroom:

- Keep `LimitNOFILE` high.
- Increase VPS RAM before adding architectural complexity.
- Watch memory and file descriptors:

  ```bash
  systemctl status ssps
  sudo ls /proc/$(pgrep -x ssps)/fd | wc -l
  ```

- Move live presence fanout to Redis or NATS only when one process is no longer enough.

## References

- Cloudflare Tunnel overview: https://developers.cloudflare.com/tunnel/
- Cloudflare Tunnel routing: https://developers.cloudflare.com/tunnel/routing/
- Cloudflare Tunnel setup: https://developers.cloudflare.com/tunnel/setup/
- Cloudflare proxied DNS records: https://developers.cloudflare.com/dns/manage-dns-records/reference/proxied-dns-records/
- Cloudflare WebSockets: https://developers.cloudflare.com/network/websockets/
- Cloudflare Full (strict) SSL/TLS: https://developers.cloudflare.com/ssl/origin-configuration/ssl-modes/full-strict/
- systemd service restart behavior: https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html
