# Monitoring Podsync with Uptime Kuma

**English** | [Русский](./ru/monitoring_uptime_kuma.md)

This guide covers how to set up professional, automated observability for **Podsync** using [Uptime Kuma](https://github.com/louislam/uptime-kuma). With these monitors, you will receive real-time notifications in **Telegram**, **Discord**, **Gotify**, or Slack whenever:
- The Podsync server or Docker container goes offline.
- The embedded Badger database becomes locked or unavailable.
- Video downloads fail (e.g. YouTube bot protection triggered, expired `cookies.txt`, or VK/Rutube geo-restrictions).
- RSS podcast feeds become unreachable.

---

## 1. Health & Diagnostic Endpoints Overview

Podsync exposes both lightweight public probes and authenticated deep-diagnostic endpoints:

| Endpoint | Auth Required | Purpose | HTTP Status & Response | Recommended Monitor |
|---|---|---|---|---|
| `GET /health/ready` | **None** (Public) | **Database & Core Service Readiness**. Tests whether BadgerDB is unlocked and responsive to reads. Independent of episode download status. | `200 OK`: `{"status":"ready","timestamp":"..."}`<br>`503 Service Unavailable`: `{"status":"unhealthy","error":"database unavailable"}` | **Recipe 1** (`HTTP(s)` or `Keyword`) |
| `GET /health/live` | **None** (Public) | **Process Liveness**. Verifies the HTTP web server process is alive and taking TCP connections. | `200 OK`: `{"status":"ok","timestamp":"..."}` | Liveness check / K8s probe |
| `GET /health` | **None** (Public) | **Aggregate Legacy Probe**. Returns `503` if the database is down OR if any episode download failed in the last 24 hours. | `200 OK`: `{"status":"healthy"}`<br>`503 Service Unavailable`: `{"status":"unhealthy","failed_episodes":N}` | Simple binary alarm |
| `GET /api/v1/system/status` | **HTTP Basic Auth** (`admin`) | **Full Sync Status & Failure Counts**. Returns total active feeds, downloaded episodes, recent download failures, disk space, and yt-dlp/ffmpeg health. | `200 OK`: `SyncStatusInfo` JSON (see below) | **Recipe 2** (`HTTP(s) - JSON Query`) |
| `GET /api/v1/system` | **HTTP Basic Auth** (`admin`) | **System Metrics**. Returns Go runtime metrics (goroutines, memory in MB, disk usage, uptime, version). | `200 OK`: `SystemStats` JSON | Custom telemetry |
| `GET /<FEED_ID>.xml` | **None** (Public) | **Podcast RSS Delivery**. Verifies that podcast clients can fetch the syndication feed XML. | `200 OK`: Valid XML document ending with `</rss>` | **Recipe 3** (`Keyword`) |

### JSON Schema: `GET /api/v1/system/status`

```json
{
  "active_feeds": 4,
  "total_episodes": 142,
  "downloaded_episodes": 142,
  "failed_episodes": 0,
  "downloaded_past_24h": 6,
  "disk_free_bytes": 107374182400,
  "disk_total_bytes": 500107862016,
  "has_downloader": true,
  "has_ffmpeg": true,
  "feeds": [
    {
      "feed_id": "tech_podcast",
      "feed_title": "Tech Podcast",
      "url": "https://www.youtube.com/channel/...",
      "provider": "youtube",
      "cron_schedule": "0 */2 * * *",
      "episode_count": 45,
      "downloaded_count": 45,
      "error_count": 0
    }
  ]
}
```

---

## 2. Monitor Recipe 1: Core Service & Database Readiness (Recommended)

This is the primary uptime monitor for your instance. It probes `/health/ready` to verify that Podsync is running and BadgerDB is accessible, without producing false alarms when an individual video download fails.

### Settings in Uptime Kuma

| Field | Value | Notes |
|---|---|---|
| **Monitor Type** | `HTTP(s)` or `HTTP(s) - Keyword` | Keyword mode provides extra verification |
| **Friendly Name** | `Podsync - Core Service` | |
| **URL** | `https://podsync.example.com/health/ready` | Or `http://<LAN_IP>:8080/health/ready` |
| **Heartbeat Interval** | `60` seconds | |
| **Retries** | `2` | Avoids alerting on temporary network blips |
| **Keyword** (if Keyword mode) | `ready` | Matches `"status":"ready"` in JSON body |
| **Accepted Status Codes** | `200-299` | Uptime Kuma default |

> [!TIP]
> Use `/health/ready` rather than `/health` for your general uptime gauge. If a YouTube channel uploads a geo-restricted clip, `/health` will return `503`, but `/health/ready` will stay `200 OK`, keeping your uptime SLA accurate.

---

## 3. Monitor Recipe 2: Download Errors & Expired Cookies Alert

When YouTube updates its bot protection algorithms or your uploaded `cookies.txt` expires, `yt-dlp` will fail to download new episodes. Podsync increments `failed_episodes` in `/api/v1/system/status`.

Using Uptime Kuma's **HTTP(s) - JSON Query** monitor, you can trigger an alert as soon as `failed_episodes > 0`.

### Settings in Uptime Kuma

| Field | Value | Notes |
|---|---|---|
| **Monitor Type** | `HTTP(s) - JSON Query` | Available in Uptime Kuma v1.22+ |
| **Friendly Name** | `Podsync - Download Errors & Cookies` | |
| **URL** | `https://podsync.example.com/api/v1/system/status` | Authenticated endpoint |
| **Heartbeat Interval** | `120` or `300` seconds | 2 to 5 minutes is optimal |
| **Retries** | `1` | Alert promptly when errors occur |
| **Authentication Method** | `HTTP Basic Auth` | |
| **Username** | Your Podsync admin username | Default: configured in `config.toml` or `PODSYNC_ADMIN_USERNAME` |
| **Password** | Your Podsync admin password | Configured in `config.toml` or `PODSYNC_ADMIN_PASSWORD` |
| **JSON Query** | `$.failed_episodes` | JSONPath expression extracting failed episode count |
| **Expected Value** | `0` | Monitor is UP when `failed_episodes == 0` |

### Security & Brute-Force Rate Limiting Notice

> [!WARNING]
> Podsync features a built-in login rate limiter: **10 consecutive failed attempts within 15 minutes causes a 30-minute IP lockout** (HTTP `429 Too Many Requests`).
> 
> When configuring this monitor in Uptime Kuma:
> 1. Double check your username and password for typos before saving.
> 2. Verify that successful requests return `200 OK`. Successful requests immediately reset the failure counter.
> 3. If you run Uptime Kuma behind a reverse proxy (e.g. Nginx, Caddy, Traefik), ensure `X-Forwarded-For` or `X-Real-IP` headers are correctly passed so that your proxy IP is not locked out.

---

## 4. Monitor Recipe 3: Podcast RSS Delivery

Verifies that your podcast feeds are generated correctly and accessible to mobile podcast players (Apple Podcasts, Pocket Casts, AntennaPod, Overcast).

### Settings in Uptime Kuma

| Field | Value | Notes |
|---|---|---|
| **Monitor Type** | `HTTP(s) - Keyword` | |
| **Friendly Name** | `Podsync - RSS Feed Delivery (<FEED_ID>)` | e.g. `Podsync - RSS (my_channel)` |
| **URL** | `https://podsync.example.com/<FEED_ID>.xml` | e.g. `https://podsync.example.com/tech_podcast.xml` |
| **Heartbeat Interval** | `300` seconds (5 minutes) | |
| **Keyword** | `</rss>` | Verifies standard RSS 2.0 footer tag |
| **Invert Keyword** | `Disabled` | |

---

## 5. Monitor Recipe 4: Docker Container & `docker-compose.yml` Healthcheck

If Uptime Kuma is hosted on the same Docker host (or connects via Docker Socket Proxy), you can monitor the Podsync container status directly.

### 5.1 Add Healthcheck to `docker-compose.yml`

Inside the official Podsync container (based on Alpine Linux), `wget` is available by default. Add the `healthcheck` stanza to your `docker-compose.yml`:

```yaml
services:
  podsync:
    container_name: podsync
    image: ghcr.io/tatarinovms/podsync:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data/
      - ./db:/app/db/
      - ./config.toml:/app/config.toml
    healthcheck:
      test: ["CMD-SHELL", "wget --spider -q http://127.0.0.1:8080/health/ready || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

After updating, apply with:
```bash
docker compose up -d
```

Verify health state with `docker ps`:
```bash
docker ps --filter "name=podsync" --format "{{.Names}}: {{.Status}}"
# Output: podsync: Up 5 minutes (healthy)
```

### 5.2 Configure Docker Monitor in Uptime Kuma

| Field | Value | Notes |
|---|---|---|
| **Monitor Type** | `Docker Container` | |
| **Friendly Name** | `Podsync - Docker Container` | |
| **Docker Daemon** | `Local Docker Socket` (`/var/run/docker.sock`) | Or remote TCP daemon |
| **Container Name / ID** | `podsync` | |
| **Heartbeat Interval** | `30` seconds | |

---

## 6. One-Click Import Preset (Uptime Kuma JSON)

You can copy this JSON preset and import it directly into Uptime Kuma via **Settings → Backup → Import**:

```json
{
  "version": "1.23.0",
  "notificationList": [],
  "monitorList": [
    {
      "name": "Podsync - Core Service & DB Readiness",
      "type": "http",
      "url": "https://podsync.example.com/health/ready",
      "interval": 60,
      "retryInterval": 30,
      "maxretries": 2,
      "accepted_statuscodes_json": "[\"200-299\"]"
    },
    {
      "name": "Podsync - Download Errors & Cookies Alert",
      "type": "json-query",
      "url": "https://podsync.example.com/api/v1/system/status",
      "interval": 180,
      "retryInterval": 60,
      "maxretries": 1,
      "authMethod": "basic",
      "basic_auth_user": "admin",
      "basic_auth_pass": "CHANGE_ME_TO_ADMIN_PASSWORD",
      "jsonPath": "$.failed_episodes",
      "expectedValue": "0"
    },
    {
      "name": "Podsync - RSS Feed Delivery",
      "type": "keyword",
      "url": "https://podsync.example.com/YOUR_FEED_ID.xml",
      "interval": 300,
      "retryInterval": 60,
      "maxretries": 2,
      "keyword": "</rss>"
    }
  ]
}
```

> [!NOTE]
> Replace `podsync.example.com`, `CHANGE_ME_TO_ADMIN_PASSWORD`, and `YOUR_FEED_ID` with your actual instance values after importing.

---

## 7. Recommended Alert Notifications

In Uptime Kuma (**Settings → Notifications**), configure one of the following channels:

### Telegram
- **Bot Token**: Created via [@BotFather](https://t.me/BotFather).
- **Chat ID**: Your personal ID or channel ID (via [@userinfobot](https://t.me/userinfobot)).
- **Custom Message**:
  ```text
  ⚠️ [{{MONITOR_NAME}}] is {{STATUS}}!
  Reason: {{MSG}}
  Target: {{TARGET}}
  ```

### Gotify / Ntfy
- Ideal for self-hosters wanting instant push notifications on Android/iOS without third-party cloud dependencies.

### Discord / Slack
- Create an incoming webhook in your alerts channel and paste the Webhook URL into Uptime Kuma.
