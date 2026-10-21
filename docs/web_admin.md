# Web Admin Console

**English** | [Русский](./ru/web_admin.md)


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
    image: ghcr.io/tatarinovms/podsync:latest
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

### 3. Override via CLI Flags

You can override the admin console setting at runtime using command-line flags:

```bash
# Force-enable admin console (/admin) regardless of config.toml / ENV:
./bin/podsync --config config.toml --admin

# Force-disable admin console (/admin) completely:
./bin/podsync --config config.toml --no-admin
```

> [!NOTE]
> CLI flags (`--admin` / `--no-admin`) have the highest priority and override both environment variables (`PODSYNC_ADMIN_ENABLED`) and `config.toml` (`[server.admin] enabled = ...`).

---

## Accessing the Console

Open your web browser and navigate to:
```text
http://<your-podsync-host>:<port>/admin/
```

Log in using the configured username and password.

---

## Features & Functionality

### 1. Sidebar Navigation & Library Organization
- **Structured Sidebar (`.app-sidebar`)**:
  - **LIBRARY**: Direct access to **Podcasts** (with total count badge), **Audiobooks**, and **Activity** (with live error badge).
  - **SETTINGS**: Centralized access to **API Keys**, **Downloader**, **System & Storage**, and **MCP Server**.
- **Dual View Modes (Grid & List)**:
  - Toggle between rich **Card Grid** view and high-density **List View** with view preference persisted in `localStorage`.
- **Live Search, Type Filters & Sorting**:
  - Instant search across podcast titles, authors, and show metadata with preserved focus.
  - Filter by feed type (**All types**, **Channels**, **Audiobooks**).
  - Sort by **Title A–Z**, **Recently updated**, or **Largest first** (storage footprint).
- **Status Quick-Filters**:
  - Interactive chips for **All**, **Updating**, and **Needs attention** with real-time numeric badges.
- **Mobile Responsive Layout**:
  - Collapsible offcanvas navigation via the top-bar burger menu with touch-friendly 44px interactive targets and `prefers-reduced-motion` compliance.

### 2. Unified Subscription Window (RSS, Apps & QR)
- Clicking **Subscribe** on any card or detail page opens a unified dialog:
  - **Direct RSS Feed URL**: Read-only URL box with a one-click **Copy** button.
  - **QR Code Generator**: Instant on-screen QR code for camera scanning with phones and tablets.
  - **Podcast Player Deep-Links**: Direct one-tap subscription for:
    - **Apple Podcasts** (`podcast://...`)
    - **Pocket Casts** (`pktc://subscribe/...`)
    - **Overcast** (`overcast://...`)

### 3. Dedicated Podcast Page (Episodes, Settings & History)
Clicking any podcast card opens a full-width dedicated show view (`.feed-detail`) instead of a modal popup:
- **Header**: High-resolution artwork, provider badge, show title, subtitle, and prominent **Subscribe** button.
- **Sub-navigation Tabs**:
  - **Episodes**: Full media library with real-time episode search, status filtering (**Downloaded**, **Pending**, **Errors**, **Cleaned**), duration filtering (&lt; 15 min, 15–60 min, &gt; 60 min), flexible sorting by date (newest/oldest), duration (longest/shortest), and title (A–Z), in-browser audio playback, file download links, and individual retry buttons.
  - **Settings**: Adjust feed parameters (update schedule, video/audio format, quality presets 480p/720p/1080p, retention limits, language, custom cover art URL or image file upload with live preview) directly on the show page.
  - **History**: Dedicated timeline of synchronization events, download activity, and errors for this specific feed.
- **Back to Library**: Seamless one-click return to the main library preserving active filters and scroll position.

### 4. Feed Management & Bulk Actions
- **Add Podcast**: Modal to subscribe to channels, playlists, or users (YouTube, Rutube, VK Video, Vimeo, SoundCloud).
  - Auto-detection of Feed ID from source URL.
  - Preset video qualities (**480p**, **720p**, **1080p**) and automatic synchronization of **Page Size &rarr; Keep Last**.
  - Immediate persistence to `config.toml`, cron scheduler, and download queue.
- **Library Actions Menu**:
  - **Update All**: Trigger concurrent updates across all configured feeds.
  - **Retry Failed Episodes**: One-click batch retry across all failing episodes in the entire library.
  - **Refresh List**: Reload library metadata on demand.

### 5. API Keys Management
- The **API Keys** tab displays current tokens for all providers:
  - **VK Video** (`PODSYNC_VKVIDEO_API_KEY` / `PODSYNC_VK_API_KEY`)
  - **YouTube** (`PODSYNC_YOUTUBE_API_KEY`)
  - **Rutube** (`PODSYNC_RUTUBE_API_KEY`, optional — Rutube is public)
  - **Vimeo** (`PODSYNC_VIMEO_API_KEY`)
  - **SoundCloud** (`PODSYNC_SOUNDCLOUD_API_KEY`)
  - **Twitch** (`PODSYNC_TWITCH_API_KEY`)
- Existing keys are masked (`••••••••`) for security.
- Add or update keys directly from the web interface — changes are automatically saved to `config.toml`.

### 6. System Health & Observability
- **Security & Rate Limiting**:
  - Brute-force protection tracking failed login attempts per client IP and username.
  - Dynamically configurable via Web UI modal or API (`max_attempts`, `window`, `lockout`).
  - Returns `429 Too Many Requests` with standard `Retry-After` header when thresholds are exceeded.

- The **System** tab provides real-time instance metrics:
  - **Disk Storage**: Total, Used, and Free disk space on the storage volume (`data_dir`) with a visual capacity bar.
  - **Memory Usage**: Real-time RAM allocated by the Podsync process.
  - **Server Uptime**: How long the Podsync daemon has been running.
  - **External Dependencies Check**: Verifies that `yt-dlp` and `ffmpeg` are detected and accessible in the system `$PATH`.

### 7. Downloader & Cookies Configuration (yt-dlp)
- The **Downloader & Cookies** tab addresses YouTube bot detection restrictions:
  - **Bypassing Bot Verification**: Overcomes the `Sign in to confirm you're not a bot. Use --cookies-from-browser or --cookies for the authentication` error.
  - **Netscape Cookies (`cookies.txt`)**: Paste or upload exported browser cookies directly into the web UI. Stored automatically in `data/cookies.txt`.
  - **Cookies from Browser**: On local desktop installations, select your installed browser (Chrome, Firefox, Safari, Brave, Edge) to automatically extract session cookies.
  - **HTTP/SOCKS5 Proxy**: Configure custom proxy routing (`--proxy`) for yt-dlp traffic.
  - **Engine Options**: Configure download timeouts and 24h automatic self-update.
  - **Live URL Verification Tool**: Test yt-dlp and cookies against any video URL on-demand to verify that metadata extraction succeeds.

---

## REST API Endpoints

The admin console is powered by a JSON REST API that can also be used for automation:

Endpoints under `/api/v1/` require authentication (Session Cookie or HTTP Basic Auth), except public authentication endpoints (`/api/v1/auth/status`, `/api/v1/auth/login`, `/api/v1/auth/logout`).

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/auth/status` | `GET` | Get current authentication status |
| `/api/v1/auth/login` | `POST` | Authenticate and obtain session cookie |
| `/api/v1/auth/logout` | `POST` | Invalidate session |
| `/api/v1/auth/password` | `POST` | Change admin password (requires current password) |
| `/api/v1/auth/security` | `GET`, `POST` | Get or update brute-force rate limiting settings (max attempts, window, lockout) |
| `/api/v1/system/errors` | `GET` | List download errors with diagnostic causes and recommendations |
| `/api/v1/system/recent` | `GET` | List recent successfully downloaded episodes |
| `/api/v1/system/status` | `GET` | High-level status snapshot (sync schedules, 24h count, active errors) |
| `/api/v1/feeds` | `GET` | List all configured feeds with stats |
| `/api/v1/feeds` | `POST` | Add a new feed |
| `/api/v1/feeds/status` | `GET` | Lightweight in-memory feed status (state: idle, queued, running; last_error, update times) |
| `/api/v1/feeds/update` | `POST` | Trigger bulk update for all configured feeds at once |
| `/api/v1/feeds/{id}` | `GET` | Get feed details and list of episodes |
| `/api/v1/feeds/{id}` | `PUT` | Update feed configuration settings (cron, format, quality, page_size, retention, language) |
| `/api/v1/feeds/{id}` | `DELETE` | Delete feed (optional: `?delete_files=true`) |
| `/api/v1/feeds/{id}/update` | `POST` | Trigger an immediate feed update |
| `/api/v1/feeds/{id}/episodes/{ep_id}/retry` | `POST` | Reset episode status to retry download |
| `/api/v1/tokens` | `GET` | Get masked tokens for all providers |
| `/api/v1/tokens` | `POST` | Update tokens for a provider |
| `/api/v1/tokens` | `DELETE` | Delete single token by provider & index or clear all tokens for provider |
| `/api/v1/audiobooks/dirs` | `GET` | List audiobook directories, chapter counts, sizes, and linked feeds |
| `/api/v1/audiobooks/import` | `POST` | Import local audiobook folder into Podcast 2.0 serial feed |
| `/api/v1/audiobooks/upload` | `POST` | Upload audiobook audio tracks and cover directly via multipart/form-data |
| `/api/v1/covers/upload` | `POST` | Upload custom cover art image (JPEG, PNG, WebP) with live preview |
| `/api/v1/audiobooks/dirs` | `DELETE` | Delete audiobook folder and linked feed from disk |
| `/api/v1/audiobooks/config` | `GET` | Get configured audiobooks directory path and status |
| `/api/v1/system/logs` | `GET` | Query recent system logs with level/search/feed_id filtering |
| `/api/v1/system/logs` | `DELETE` | Clear in-memory system log buffer |
| `/api/v1/system/logs/level` | `GET` | Get current server logging level (info, debug, warn, error) |
| `/api/v1/system/logs/level` | `POST` | Change server logging level on the fly (info, debug, warn, error) |
| `/api/v1/system/logs/download` | `GET` | Download complete server log file as text |
| `/api/v1/mcp/config` | `GET` | Get MCP stdio and SSE client connection configuration |
| `/api/v1/system` | `GET` | Get system metrics, disk usage, and dependencies |
| `/api/v1/downloader` | `GET` | Get yt-dlp downloader configuration, cookies status, and proxy |
| `/api/v1/downloader` | `POST` | Update downloader settings (timeout, proxy, browser, self-update) |
| `/api/v1/downloader/cookies` | `GET` | Get Netscape cookies.txt content |
| `/api/v1/downloader/cookies` | `POST` | Save or upload new Netscape cookies.txt content |
| `/api/v1/downloader/cookies` | `DELETE` | Clear and delete installed cookies.txt file |
| `/api/v1/downloader/test` | `POST` | Test URL extraction with yt-dlp and configured cookies/proxy |
