# Usage metrics (Prometheus + Grafana)

The signaling server can export **anonymous usage counters** for Prometheus: how many devices are online, how many distinct devices connect each day and week, connections, pairings, started transfers and refused connections. Every label takes one of a few fixed values (`browser`/`cli`, a device kind, an event name); device codes, names, IP addresses and anything about files are never exported. Distinct devices are counted by hashing each device code with a salt that only lives for the current day or week, then thrown away with it.

The server sees transfers **start** (the WebRTC offer it relays), never their content, size or whether they finished.

## 1. Turn it on

In `docker-compose.yml` (already set):

```yaml
    ports:
      - "${FILESWAP_METRICS_PORT:-9465}:9465"
    environment:
      METRICS_PORT: 9465
      TZ: UTC                      # your time zone, e.g. America/Bogota: when "today" starts
      # METRICS_TOKEN: change-me   # optional bearer token
```

The metrics have **their own port**, separate from the web app's. Keep it private: don't forward it in the router and don't proxy it through Caddy. Check it from the Docker host:

```bash
curl -s localhost:9465/metrics | grep '^fileswap_'
```

## 2. Scrape it with Prometheus

Add a job to `prometheus.yml` (the address is how your Prometheus reaches the FileSwap host, e.g. its Tailscale IP):

```yaml
  - job_name: fileswap
    scrape_interval: 30s
    static_configs:
      - targets: ['100.107.52.17:9465']
    # With METRICS_TOKEN:
    # authorization:
    #   credentials: change-me
```

Reload Prometheus (`curl -X POST localhost:9090/-/reload` with `--web.enable-lifecycle`, or restart it) and check that *Status → Targets* shows `fileswap` as **UP**.

## 3. Import the dashboard in Grafana

*Dashboards → New → Import*, upload [`grafana-dashboard.json`](grafana-dashboard.json) and pick your Prometheus in the **Prometheus** selector at the top. It creates **FileSwap — Usage** (uid `fileswap-usage`):

| Row | Panels |
|---|---|
| Headline | Online now · distinct devices today / this week · transfers, new pairings and cancelled share in the selected range |
| Devices | Online over time (browser vs CLI) · distinct devices, resetting at midnight (each peak is a day's total) |
| Activity | Transfers (file vs clipboard listing) · connections (browser vs CLI) |
| Who | Pairing events · online by kind · registrations browser vs CLI |
| Health | Refused connections and messages (bots, limits) · web app version · uptime · memory |

Bars adapt to the time range (about 60 per range, one hour at least); the tooltip shows each bar's period.

**Downloading data:** on any panel, *⋮ → Inspect → Data → Download CSV* exports what it shows for the chosen range. Exporting a panel as PNG (*Share → Link → Render image*) needs the `grafana-image-renderer` plugin or container.

## Metrics

| Metric | Type | Labels |
|---|---|---|
| `fileswap_devices_online` | gauge | `client` (browser, cli), `kind` (phone, tablet, laptop, desktop, server, unknown) |
| `fileswap_devices_unique` | gauge | `period` (day, week) — distinct devices since the period started |
| `fileswap_ws_connections_total` | counter | `client` |
| `fileswap_registrations_total` | counter | `client`, `kind` |
| `fileswap_pairings_total` | counter | `event` (requested, accepted, rejected, unpaired) |
| `fileswap_transfers_started_total` | counter | `type` (file — a file, ZIP or clipboard attachment; clip_list — reading another device's clipboard) |
| `fileswap_transfers_aborted_total` | counter | — |
| `fileswap_ws_rejected_total` | counter | `reason` (origin, ip_limit, rate_limit, too_big, capacity) |
| `fileswap_build_info` | gauge | `version` (hash of the served `index.html`) |
| `process_start_time_seconds`, `process_resident_memory_bytes` | gauge | — |

Counters reset when the server restarts; `increase()` and `rate()` handle that. Connections count reconnects too (phones in the background, network changes), so distinct devices is the better measure of people.

Useful queries:

```promql
# Transfers per day
sum(increase(fileswap_transfers_started_total{type="file"}[1d]))
# Share of pairing requests that get accepted, last 30 days
sum(increase(fileswap_pairings_total{event="accepted"}[30d])) / sum(increase(fileswap_pairings_total{event="requested"}[30d]))
# Busiest moment of the last week
max_over_time(sum(fileswap_devices_online)[7d:5m])
```
