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
  - **LIBRARY**: Direct access to **Podcasts** (with total count badge), **Personal feeds**, **Audiobooks**, and **Activity** (with live error badge).
  - **SETTINGS**: Centralized access to **API Keys**, **Downloader**, **System & Storage**, and **MCP Server**.
- **Dual View Modes (Grid & List)**:
  - Toggle between rich **Card Grid** view and high-density **List View** with view preference persisted in `localStorage`.
- **Live Search, Type Filters & Sorting**:
  - Instant search across podcast titles, authors, and show metadata with preserved focus.
  - Filter by feed type (**All types**, **Channels**, **Personal**, **Audiobooks**).
  - Sort by **Title A–Z**, **Recently updated**, or **Largest first** (storage footprint).
- **Status Quick-Filters**:
  - Interactive chips for **All**, **Updating**, and **Needs attention** with real-time numeric badges.
- **Mobile Responsive Layout**:
  - Collapsible offcanvas navigation via the top-bar burger menu with touch-friendly 44px interactive targets and `prefers-reduced-motion` compliance.
  - The top-bar OPML download button is hidden below 576px to keep the navigation within the viewport.

### Keyboard and form accessibility

Selection controls share a combobox with a padded chevron. Press Enter or Space
to open it, use arrow keys or Home/End to highlight an option, and Enter to apply
it. Typing highlights matching options. Escape cancels the highlighted change;
Tab closes the list and continues through the form. Inside a dialog, Escape
closes an open list before closing the dialog.

Form fields have associated labels and feed ID validation is exposed through
`aria-invalid` and linked help text. The library filters wrap at narrow widths;
mobile buttons, schedule presets and selection options have 44px touch targets.
Latte uses semantic text colors for readable status badges and secondary text.

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

### 5. Personal Feeds Management
- **Universal Feeds (Audio + Video)**: Curate individual videos and audio files into custom podcast feeds. Both audio tracks and video clips can reside in the same feed, with dynamic `<enclosure>` MIME types.
- **Unified Library & Dedicated Tab**:
  - Directly manage feeds, upload files, and add URLs in the dedicated **Personal feeds** tab.
  - Personal feeds are also surfaced on the main **Podcasts** tab with a **Personal** provider badge and quick action buttons.
- **Multiple Ingestion Methods**:
  - **URL Links**: Add episodes by URL from YouTube, VK Video, Rutube, or direct HTTP/HTTPS links to `.mp3`, `.ogg`, `.mp4` files, with format and quality/resolution selection (Audio: High/Low, Video: 480p, 720p HD, 1080p Full HD, 1440p 2K, 2160p 4K, Original) matching feed mirroring.
  - **Server Local Files**: Import media directly by local filesystem path (`file_path`).
  - **File Uploads**: Drag-and-drop or upload local audio/video files (up to 4 GiB) with real-time progress and automatic `ffmpeg` transcoding.
- **Episode Operations**:
  - In-browser playback with integrated audio/video player.
  - **Rename**: Change episode title in a modal dialog with instant feed RSS XML rebuild.
  - **Copy**: Clone an episode into another personal feed without duplicating physical storage.
  - **Move**: Transfer an episode between feeds while preserving its GUID.
  - **Retry**: Re-attempt failed downloads or transcoding.
- **Unified Subscription**: One-click **Subscribe** modal with QR code, direct RSS copy, and deep links for Apple Podcasts, Pocket Casts, and Overcast.

### 6. API Keys Management
- The **API Keys** tab displays current tokens for all providers:
  - **VK Video** (`PODSYNC_VKVIDEO_API_KEY` / `PODSYNC_VK_API_KEY`)
  - **YouTube** (`PODSYNC_YOUTUBE_API_KEY`)
  - **Rutube** (`PODSYNC_RUTUBE_API_KEY`, optional — Rutube is public)
  - **Vimeo** (`PODSYNC_VIMEO_API_KEY`)
  - **SoundCloud** (`PODSYNC_SOUNDCLOUD_API_KEY`)
- Existing keys are masked (`••••••••`) for security.
- Add or update keys directly from the web interface — changes are automatically saved to `config.toml`.

### 7. System Health & Observability
- **Security & Rate Limiting**:
  - Brute-force protection tracking failed login attempts per client IP and username.
  - Dynamically configurable via Web UI modal or API (`max_attempts`, `window`, `lockout`).
  - Returns `429 Too Many Requests` with standard `Retry-After` header when thresholds are exceeded.

- The **System** tab provides real-time instance metrics:
  - **Disk Storage**: Total, Used, and Free disk space on the storage volume (`data_dir`) with a visual capacity bar.
  - **Memory Usage**: Real-time RAM allocated by the Podsync process.
  - **Server Uptime**: How long the Podsync daemon has been running.
  - **External Dependencies Check**: Verifies that `yt-dlp` and `ffmpeg` are detected and accessible in the system `$PATH`.
  - **Podsync Updates**: Checks the latest GitHub Release. On a supported native Linux or macOS installation, **Update Podsync** downloads the matching release archive, verifies its SHA-256 from `checksums.txt`, replaces the executable, and restarts the process. The button is disabled for Docker deployments; follow the Compose update commands in the deployment guide instead.

### 7. Downloader & Cookies Configuration (yt-dlp)
- The **Downloader & Cookies** tab addresses YouTube bot detection restrictions:
  - **Bypassing Bot Verification**: Overcomes the `Sign in to confirm you're not a bot. Use --cookies-from-browser or --cookies for the authentication` error.
  - **Netscape Cookies (`cookies.txt`)**: Paste or upload exported browser cookies directly into the web UI. Stored automatically in `data/cookies.txt`.
  - **Cookies from Browser**: On local desktop installations, select your installed browser (Chrome, Firefox, Safari, Brave, Edge) to automatically extract session cookies.
  - **HTTP/SOCKS5 Proxy**: Configure custom proxy routing (`--proxy`) for yt-dlp traffic.
  - **Engine Options**: Configure download timeouts and 24h automatic self-update.
  - **Live URL Verification Tool**: Test yt-dlp and cookies against any video URL on-demand to verify that metadata extraction succeeds.

### 8. Network DNS
- The **Network DNS** card accepts comma-separated IPv4 or IPv6 resolver addresses.
- Saved addresses apply immediately to Podsync provider API and RSS metadata requests, including VK Video. Leave the field empty to return to the operating system DNS resolver.
- `yt-dlp` is a separate process and continues to use the operating system DNS.

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
| `/api/v1/system/update` | `GET`, `POST` | Check the latest GitHub Release or install it and restart a native Podsync process |
| `/api/v1/feeds` | `GET` | List all configured feeds with stats |
| `/api/v1/feeds` | `POST` | Add a new feed |
| `/api/v1/feeds/status` | `GET` | Lightweight in-memory feed status (state: idle, queued, running; last_error, update times) |
| `/api/v1/feeds/update` | `POST` | Trigger bulk update for all configured feeds at once |
| `/api/v1/feeds/{id}` | `GET` | Get feed details and list of episodes |
| `/api/v1/feeds/{id}/episodes` | `GET` | Paginated episodes (`limit`, `offset`, `status`, `q`, `sort`, `duration`) |
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
| `/api/v1/feeds/preview` | `POST` | Inspect remote feed / RSS URL, parse metadata, episode count, and size |
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
| `/api/v1/network` | `GET` | Get custom DNS servers for provider API and RSS metadata requests |
| `/api/v1/network` | `POST` | Update custom DNS servers (`dns_servers`) |

Episode queries return `feed`, `episodes`, `total`, `matching`, `status_counts`,
`offset`, `limit`, `has_more`, and `next_offset` when another page exists.
`status_counts` describes the whole feed before filters. Limits default to 40 and
are capped at 200. An offset beyond the matching results is moved to the last
available page; clients should use the returned `offset`. The feed detail endpoint
accepts the same query parameters when `limit` is supplied, while requests without
`limit` retain the complete episode list.

For monitoring, use public `/health/live` for process liveness and `/health/ready`
for database readiness. Legacy `/health` also reports recent episode download
failures and can return 503 while the process and database remain available.

The **MCP Server** settings provide connection parameters for stdio and HTTP/SSE.
Updated stdio clients relay JSON-RPC directly to the daemon's `POST /mcp`, including
all personal-feed tools. See the [MCP guide](./mcp.md) for setup, version reporting,
and troubleshooting older clients.
