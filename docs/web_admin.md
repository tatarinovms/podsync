# Web Admin Console

Podsync includes a built-in, responsive web management console designed for easy feed administration, episode monitoring, one-click podcast subscriptions, and system observability.

The admin console is completely self-contained (embedded in the Go binary) and requires no external node/npm runtimes or separate static file deployments.

---

## Quick Setup

### 1. Enable via `config.toml`

Add the `[server.admin]` section to your `config.toml`:

```toml
[server]
port = 8080
# hostname is used to construct podcast RSS links and QR codes.
# Set it to the address where your Podsync instance is reachable from your devices:
hostname = "http://192.168.1.100:8080"

[server.admin]
enabled = true
username = "admin"
password = "your-secure-password"
```

### 2. Enable via Environment Variables (Docker / Docker Compose)

Alternatively, you can configure the admin console using environment variables without modifying `config.toml`:

```bash
PODSYNC_ADMIN_ENABLED=true
PODSYNC_ADMIN_USERNAME=admin
PODSYNC_ADMIN_PASSWORD=your-secure-password
```

Example in `docker-compose.yml`:

```yaml
services:
  podsync:
    image: tatarinovms/podsync:latest
    container_name: podsync
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - PODSYNC_ADMIN_ENABLED=true
      - PODSYNC_ADMIN_USERNAME=admin
      - PODSYNC_ADMIN_PASSWORD=your-secure-password
    volumes:
      - ./config.toml:/app/config.toml
      - ./data:/app/data
      - ./db:/app/db
```

---

## Accessing the Console

Open your web browser and navigate to:
```text
http://<your-podsync-host>:<port>/admin/
```

Log in using the configured username and password.

---

## Features & Functionality

### 1. Podcast Feeds & One-Click Subscribing
- **Channel Cards**: View all active channels with high-resolution artwork, provider badges (VK Video, YouTube, Vimeo, SoundCloud, Twitch), episode counts, and total disk space used.
- **Copy RSS Link**: Copy the direct feed URL (`http://<host>:<port>/<feed_id>.xml`) into your clipboard with a single click.
- **QR Code Generator**: Click the QR icon on any feed card to display an on-screen QR code. Scan it with your phone's camera to instantly open and subscribe in your podcast app.
- **Player Deep-Links**: Direct links to subscribe in popular podcast players:
  - **Apple Podcasts** (`podcast://...`)
  - **Pocket Casts** (`pktc://subscribe/...`)
  - **Overcast** (`overcast://...`)

### 2. Feed Management
- **Add Feed**: Click **+ Add Feed** to register a new channel, user, or playlist (VK Video, YouTube, Vimeo, etc.).
  - Auto-detection of Feed ID from URL.
  - Configuration of format (`video`, `audio`), quality (`high`, `low`), update frequency, and title filters.
  - **Instant synchronization**: Newly created feeds are immediately written to `config.toml`, scheduled in cron, and added to the download queue.
- **Delete Feed**: Remove a feed from memory and `config.toml`, with an optional checkbox to remove all downloaded media files from disk.
- **Manual Update ("Update Now")**: Trigger an immediate update check without waiting for the next cron cycle.

### 3. Episode Inspection & Retry
- Click **Episodes** on any feed to view all discovered media items.
- See status badges: `Downloaded` (with file size and date), `New`, or `Error`.
- **Retry Failed Downloads**: If an episode failed to download (e.g., temporary network error or rate limit), click **Retry** to reset its status and re-enqueue it.

### 4. API Keys Management
- The **API Keys** tab displays current tokens for all providers:
  - **VK Video** (`PODSYNC_VKVIDEO_API_KEY` / `PODSYNC_VK_API_KEY`)
  - **YouTube** (`PODSYNC_YOUTUBE_API_KEY`)
  - **Vimeo** (`PODSYNC_VIMEO_API_KEY`)
  - **SoundCloud** (`PODSYNC_SOUNDCLOUD_API_KEY`)
  - **Twitch** (`PODSYNC_TWITCH_API_KEY`)
- Existing keys are masked (`••••••••`) for security.
- Add or update keys directly from the web interface — changes are automatically saved to `config.toml`.

### 5. System Health & Observability
- The **System** tab provides real-time instance metrics:
  - **Disk Storage**: Total, Used, and Free disk space on the storage volume (`data_dir`) with a visual capacity bar.
  - **Memory Usage**: Real-time RAM allocated by the Podsync process.
  - **Server Uptime**: How long the Podsync daemon has been running.
  - **External Dependencies Check**: Verifies that `yt-dlp` and `ffmpeg` are detected and accessible in the system `$PATH`.

---

## REST API Endpoints

The admin console is powered by a JSON REST API that can also be used for automation:

All endpoints require authentication (Session Cookie or HTTP Basic Auth).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/auth/status` | `GET` | Get current authentication status |
| `/api/v1/auth/login` | `POST` | Authenticate and obtain session cookie |
| `/api/v1/auth/logout` | `POST` | Invalidate session |
| `/api/v1/feeds` | `GET` | List all configured feeds with stats |
| `/api/v1/feeds` | `POST` | Add a new feed |
| `/api/v1/feeds/{id}` | `GET` | Get feed details and list of episodes |
| `/api/v1/feeds/{id}` | `DELETE` | Delete feed (optional: `?delete_files=true`) |
| `/api/v1/feeds/{id}/update` | `POST` | Trigger an immediate feed update |
| `/api/v1/feeds/{id}/episodes/{ep_id}/retry` | `POST` | Reset episode status to retry download |
| `/api/v1/tokens` | `GET` | Get masked tokens for all providers |
| `/api/v1/tokens` | `POST` | Update tokens for a provider |
| `/api/v1/system` | `GET` | Get system metrics, disk usage, and dependencies |
