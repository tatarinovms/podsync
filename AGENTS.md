# AGENTS.md

This file provides comprehensive architecture guidance, development workflows, and behavioral rules for autonomous AI coding agents (Antigravity, Claude Code, OpenCode, Hermes Agent, Cursor, Cline) working on this repository.

---

## 1. Project Overview & Tech Stack

**Podsync** is a high-performance Go service that converts video and audio channels, playlists, communities, user profiles (YouTube, Rutube, VK Video, Vimeo, SoundCloud, Twitch), and **local audiobook folder collections** into standards-compliant podcast RSS feeds with automatic media downloading, transcoding, disk management, and metadata storage.

### Core Technologies
- **Language**: Go 1.25+ (`go.mod`)
- **Storage/DB**: BadgerDB (embedded key-value database for feed/episode metadata and state)
- **Job Queue & Concurrency**: Persistent disk-backed job queue (`pkg/jobqueue/`) with crash recovery, state persistence, and worker concurrency limits
- **Filesystem**: Local filesystem storage or AWS S3 (`pkg/fs/`), with dynamic Virtual Mounter (`fs.Mounter`) for zero-copy audiobook serving
- **Downloader**: `yt-dlp` (native preference with fallback to `youtube-dl`), using `ffmpeg` for audio conversion, duration probing, and metadata extraction
- **Scheduling**: `robfig/cron/v3` supporting standard 5-field cron expressions and `@every` duration intervals
- **Configuration**: TOML via `pelletier/go-toml`
- **Security & Rate Limiting**: In-memory sliding-window IP rate limiter (`services/web/rate_limiter.go`) with brute-force lockout, session cookie authentication, and HTTP Basic Auth
- **Web UI**: Embedded modular single-page application (`services/web/ui/` with `index.html`, `admin.css`, `admin.js`, `theme.js`, mirrored to root `html/`) styled with strict Catppuccin Mocha (dark) & Latte (light) themes
- **Agent Protocols**: Built-in Model Context Protocol (MCP) server (`pkg/mcp/`) supporting Stdio and SSE/HTTP

---

## 2. Repository Layout

```
.
├── cmd/podsync/              # Main application entry point & CLI commands
│   ├── main.go               # Flags, signal handling, HTTP server & cron initialization
│   ├── config.go             # Config struct, defaults, environment variable overrides
│   ├── config_test.go        # Unit tests for config loading and env vars
│   ├── feed_manager.go       # Implementation of AppFeedManager for Web UI and MCP
│   ├── feed_manager_test.go  # Tests for feed management, private feeds, audiobook deletion
│   └── disk_unix.go / windows.go # Platform-specific disk space calculations
├── html/                     # Mirrored Web UI static assets (admin.css, admin.js, index.html, theme.js)
├── pkg/
│   ├── builder/              # Platform builders fetching metadata from source APIs
│   │   ├── audiobook.go      # Local audiobook builder (ID3/APIC tags, natural sorting, serial format)
│   │   ├── audiobook_test.go # Unit tests for audiobook discovery, chapter sorting, and covers
│   │   ├── rutube.go         # Rutube builder (channel, playlists, users via yt-dlp)
│   │   ├── vkvideo.go        # VK Video builder (official VK API client, handle resolution)
│   │   ├── youtube.go        # YouTube Data API v3 builder
│   │   ├── vimeo.go          # Vimeo API builder
│   │   ├── soundcloud.go     # SoundCloud HTML scraper / client
│   │   ├── twitch.go         # Twitch Helix API builder
│   │   └── url.go            # URL parsing and platform detection
│   ├── db/                   # BadgerDB wrappers for Feeds, Episodes, and State
│   ├── feed/                 # Podcast RSS XML generation, OPML export, API key rotation
│   │   ├── xml.go            # RSS/iTunes XML builder (serial/episodic, itunes:block, covers)
│   │   └── xml_test.go       # RSS XML generation tests
│   ├── fs/                   # Storage abstraction (Local file system, S3, and Virtual Mounter)
│   ├── jobqueue/             # Persistent disk-backed job queue with crash recovery and concurrency control
│   ├── logbuffer/            # Thread-safe in-memory circular ring buffer for real-time logs
│   ├── mcp/                  # Model Context Protocol server (stdio & HTTP/SSE)
│   │   ├── server.go         # 28 MCP tools, resources (podsync://*), and diagnostic prompts
│   │   └── proxy.go          # JSON-RPC relay to /mcp and web.AdminManager REST client
│   ├── model/                # Domain models (Feed, Episode, Provider constants)
│   └── ytdl/                 # Wrapper around yt-dlp / youtube-dl CLI
├── services/
│   ├── web/                  # HTTP Server, Web Admin Console (/admin), REST API (/api/v1)
│   │   ├── admin_types.go    # FeedSummary, FeedConfigUpdate, AudiobookDirInfo structs
│   │   ├── api.go            # REST API endpoints (/api/v1/*)
│   │   ├── auth.go           # Session cookie auth, password verification, login handlers
│   │   ├── feed_patch.go     # Atomic partial feed configuration updates
│   │   ├── rate_limiter.go   # In-memory sliding-window IP brute-force protection
│   │   ├── server.go         # HTTP router, middleware, static asset embedding
│   │   └── ui/               # Embedded SPA assets (admin.css, admin.js, index.html, theme.js)
│   ├── update/               # Feed update orchestrator and regex episode matcher
│   └── migrate/              # Tooling for media filename migration
├── docs/                     # Guides, token setup instructions, and MCP specs
│   ├── dev/                  # Architecture, API/design guides, workflow, and review checklist
│   └── ru/                   # Russian user guides and developer documentation index
├── tests/e2e/               # Playwright browser regression tests with isolated npm dependencies
├── .agents/skills/podsync/   # Standard agent skill definition for MCP
├── Dockerfile                # Multi-stage Docker build (Go builder + Alpine runtime with yt-dlp)
├── docker-compose.yml        # Docker compose configuration for running Podsync
└── config.toml.example       # Canonical configuration reference
```

---

## 3. Platform Implementations & Specifics

### Personal feeds (`pkg/personal/`)
- Universal curated feeds capable of hosting both audio and video episodes simultaneously in a single feed. Each episode generates dynamic `<enclosure>` MIME types matching its real format (`audio/mpeg`, `video/mp4`, etc.).
- Dedicated **Personal feeds** tab in **LIBRARY**, plus unified card display on the main **Podcasts** tab with the `Personal` filter option.
- Unlimited manually curated feeds with immutable generated IDs (`personal-<32hex>`) and an initial default `personal` (`My Feed`).
- Media ingestion supports:
  - Direct media URLs (HTTP/HTTPS links to `.mp3`, `.ogg`, `.wav`, `.mp4`, `.mkv`, etc.).
  - Video and audio platform URLs (YouTube, VK Video, Rutube).
  - Local server file paths (`file_path`) via API and MCP tools.
  - Multipart file uploads up to 4 GiB via Web UI or REST API.
  - Automatic `ffmpeg` conversion to MP3 for audio and MP4 for video, preserving duration, metadata, and artwork.
  - Quality and resolution control when adding from URL: audio quality (`high`/`low`) and video resolution (`480p`, `720p HD`, `1080p Full HD`, `1440p 2K`, `2160p 4K`, `Default Original`), matching feed mirroring options.
  - Inline episode renaming with instant feed RSS XML rebuild (`PATCH /api/v1/personal-feeds/{id}/episodes/{ep_id}`).
- Dedicated transactional BadgerDB namespace (`podsync/personal/v1/`) for feed configs, episode memberships, and shared media references.
- Copying an episode creates a new stable GUID referencing shared media; moving an episode retains the original GUID. Media files are only deleted when their reference count drops to zero and file deletion was explicitly requested.
- Managed by 6 specialized MCP tools: `list_personal_feeds`, `get_personal_feed`, `create_personal_feed`, `add_personal_episode`, `delete_personal_episode`, `rename_personal_episode`.

### Local Audiobooks (`audiobook://<folder_name>`)
- **Protocol**: `audiobook://<folder>` or `audiobook://relative/path`
- **Podcast 2.0 Serial Standard**: Generated RSS feed contains `<itunes:type>serial</itunes:type>` and `<itunes:episode>N</itunes:episode>` to ensure podcast apps play chapters in strict chronological order (Chapter 1 -> Chapter 2).
- **Natural Chapter Sorting**: Files are naturally sorted (`01_intro.mp3`, `2_chapter.mp3`, `10_chapter.mp3`) using human-friendly alphanumeric ordering.
- **Automatic ID3 / APIC Cover Extraction**:
  - Automatically searches for external cover files (`cover.jpg`, `folder.png`, `album.jpeg`).
  - If no external cover exists, extracts embedded ID3v2 APIC cover art directly from the first MP3/M4A file using `ffmpeg`.
- **Zero-Copy Virtual Mounter (`fs.Mounter`)**: Mounts the audiobook directory directly into HTTP server storage (`/{feed_id}/{filename}`) without duplicating audio files.
- **Audiobook Feeds Protection**: Audiobook feeds are audio-only (`format = "audio"`, `quality = "high"`), retention cleanup (`keep_last`) is disabled, and video/ytdl-args options are hidden in the UI.

### VK Video (`pkg/builder/vkvideo.go`)
- **Supported URLs**: `vkvideo.ru`, `vk.com`, `vk.ru` (channels, groups, playlists, user videos).
- **VK API**: Base URL `https://api.vk.com/method/` (API v`5.199`). Community owner IDs must be negative (e.g. `-196446247`).
- Live streams and upcoming premieres (`live_status: "started" | "upcoming"`) are excluded.

### Rutube (`pkg/builder/rutube.go`)
- Supported URLs: `rutube.ru/channel/...`, `rutube.ru/plst/...`, `rutube.ru/u/...`, `rutube.ru/video/...`.
- Native zero-API-key metadata querying and downloading via `yt-dlp`.

### YouTube (`pkg/builder/youtube.go`)
- Requires YouTube Data API v3 key (`PODSYNC_YOUTUBE_API_KEY`).
- Multi-key Round-Robin rotation. Downloads run through `yt-dlp` with Netscape cookies support.

---

## 4. Web Admin Console & REST API (`services/web/`)

- Accessible at `/admin` (SPA modularized across `index.html`, `admin.css`, `admin.js`, and `theme.js`, embedded via `//go:embed ui/*`).
- **UI Architecture**:
  - **Sidebar Navigation (`.sidebar`)**: Live podcast count badges, quick filters (All, Active, Errors, Audiobooks), and real-time disk storage gauge.
  - **Dual Library Views**: Compact card grid (`#view-mode-cards`) and structured tabular list (`#view-mode-list`) with persistent view selection.
  - **Dedicated Podcast View (`.feed-detail`)**:
    - **Header**: High-resolution artwork, provider badge, show title, subtitle, and prominent **Subscribe** button.
    - **Episodes Tab**: Real-time episode search, status filtering (**Downloaded**, **Pending**, **Errors**, **Cleaned**), duration filtering (&lt; 15 min, 15–60 min, &gt; 60 min), flexible multi-criteria sorting (date newest/oldest, duration longest/shortest, title A–Z), in-browser audio player, file download links, and individual retry buttons.
    - **Settings Tab**: Inline feed parameters adjustment (cron schedule, format, quality presets 480p/720p/1080p, retention limits, language, custom cover art URL & file upload with live preview) directly on the show page.
    - **History Tab**: Dedicated chronological timeline of synchronization events, download activity, and errors for the feed.
- **Button Standards & Layout Metrics**:
  - **Modal & Card Action Pairs**: Every action pair (Cancel / Close on the left, Save / Update / Confirm on the right) uses standard `btn-sm` with `min-width: 105px` and 35px height on desktop (`padding: 6px 16px`, `font-size: 0.875rem`, `line-height: 1.5`).
  - **Mobile Touch Targets**: Under mobile viewports (`max-width: 575px`), action buttons automatically expand to `min-height: 44px` with `padding: 8px 16px` for touch accessibility.
  - **Secondary Actions**: Use `.btn-secondary.btn-sm` (Neutral surface).
  - **Primary Actions**: Use `.btn-brand.btn-sm` (Mauve accent `#cba6f7` / `#8839ef`).
  - **Presets**: Compact badges and cron interval buttons use `.btn-xs` (`font-size: 0.78rem`, `padding: 2px 8px`, `border-radius: 6px`).
- **Brand Colors (Catppuccin Mocha & Latte)**:
  - All UI elements must strictly use project CSS variables (`--ctp-*`).
  - Never allow raw Bootstrap defaults (`#0d6efd`, slate `#2b3035`) to leak into backgrounds or buttons.
  - Primary buttons must use `.btn-brand` (Mauve).
- **Modal Lifecycle Reliability**:
  - Never use `bootstrap.Modal.getInstance(el).hide()` directly as it can return `null`.
  - Always use `closeModal(modalId)` which leverages `bootstrap.Modal.getOrCreateInstance(el)` and ensures DOM backdrop/class cleanup.
- **Instant Feed Rebuild**: Saving feed settings (`UpdateFeed`) immediately triggers `m.updates <- f` so changes (`private_feed`, `title`, `author`, `custom cover`) appear in `{feed_id}.xml` right away.
- **Strict English-Only Interface**:
  - The Web Admin Console is strictly English-only.
  - All navigation labels, headings, subtitles, button labels, forms, placeholders, badges, status indicators, empty states, modals, and notifications must be written in English.
  - Never hardcode Russian or other non-English text in HTML templates, JavaScript code, or backend default values (e.g. default personal feed title is `My Feed`, not `Моя лента`).
  - User-provided Cyrillic titles used as slugs/IDs must be safely transliterated to ASCII/Latin slugs automatically (`transliterateCyrillic`) with visual validation.

### REST API Endpoints (`/api/v1`)
| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/health/ready` | Readiness and database connectivity check |
| `GET` | `/health` | Legacy diagnostic check, including recent download failures |
| `GET` | `/health/live` | Lightweight process liveness probe |
| `GET` | `/api/v1/auth/status` | Get current authentication status |
| `POST` | `/api/v1/auth/login` | Authenticate admin session and issue cookie |
| `POST` | `/api/v1/auth/logout` | Invalidate current admin session |
| `POST` | `/api/v1/auth/password` | Change admin password (requires current password) |
| `GET`, `POST` | `/api/v1/auth/security` | Get or update login rate-limiter & lockout settings |
| `GET` | `/api/v1/system/errors` | List download errors with diagnostic causes and recommendations (`?feed_id=...&limit=...`) |
| `GET` | `/api/v1/system/recent` | List recent successfully downloaded episodes (`?feed_id=...&limit=...`) |
| `GET` | `/api/v1/system/status` | High-level status snapshot (sync schedules, 24h count, active errors) |
| `GET` | `/api/v1/system` | Get system metrics, disk usage, and dependencies |
| `GET` | `/api/v1/system/logs` | Query in-memory server logs (level/search/feed_id filter) |
| `GET` | `/api/v1/system/logs/download` | Download complete server log file as text |
| `DELETE` | `/api/v1/system/logs` | Clear in-memory system log buffer |
| `GET`, `POST` | `/api/v1/system/logs/level` | Get or change server logging level on the fly |
| `GET` | `/api/v1/feeds` | List all active feeds (channels and audiobooks) |
| `POST` | `/api/v1/feeds` | Create new podcast feed |
| `GET` | `/api/v1/feeds/status` | Lightweight in-memory feed states (idle, queued, running) |
| `POST` | `/api/v1/feeds/update` | Trigger bulk update for all configured feeds at once |
| `GET` | `/api/v1/feeds/{id}` | Get feed details and episode list (supports `?limit=40&offset=0` pagination) |
| `GET` | `/api/v1/feeds/{id}/episodes` | Paginated episode query (`limit`, `offset`, `status`, `q`, `sort`, `duration`) |
| `PUT` | `/api/v1/feeds/{id}` | Update feed configuration and trigger XML rebuild |
| `DELETE` | `/api/v1/feeds/{id}` | Delete feed (optional `?delete_files=true`) |
| `POST` | `/api/v1/feeds/{id}/update` | Trigger manual update for a feed |
| `POST` | `/api/v1/feeds/{id}/cover` | Attach uploaded cover directly to a specific feed |
| `POST` | `/api/v1/feeds/{id}/episodes/{ep_id}/retry` | Reset failed episode state |
| `POST` | `/api/v1/covers/upload` | Upload custom cover image file (JPEG, PNG, WebP) with live preview |
| `GET` | `/api/v1/tokens` | List masked API tokens and ENV sources |
| `POST` | `/api/v1/tokens` | Update / append API tokens for rotation |
| `DELETE` | `/api/v1/tokens` | Delete token by `?provider=...&index=...` |
| `GET` | `/api/v1/audiobooks/dirs` | List folders in `[audiobooks].dir` (track count, cover, linked status) |
| `DELETE` | `/api/v1/audiobooks/dirs` | Delete audiobook folder and audio files from disk (`?name=...`) |
| `POST` | `/api/v1/audiobooks/import` | Import local audiobook folder as serial podcast feed |
| `POST` | `/api/v1/audiobooks/upload` | Multipart file upload for audiobook chapters/cover |
| `GET` | `/api/v1/audiobooks/config` | Read configured audiobooks directory |
| `GET` | `/api/v1/downloader` | Inspect yt-dlp binary, ffmpeg version, timeout, cookies stats |
| `POST` | `/api/v1/downloader` | Update downloader timeout, proxy, language, browser cookies |
| `GET`/`POST`/`DELETE` | `/api/v1/downloader/cookies` | Manage Netscape `cookies.txt` content |
| `POST` | `/api/v1/downloader/test` | Test yt-dlp metadata extraction against URL |
| `GET` | `/api/v1/mcp/config` | Fetch MCP connection parameters for agents |
| `GET` | `/api/v1/personal-feeds` | List all personal podcast feeds |
| `POST` | `/api/v1/personal-feeds` | Create new personal podcast feed |
| `GET` | `/api/v1/personal-feeds/{id}` | Get personal feed details and episodes |
| `PATCH` | `/api/v1/personal-feeds/{id}` | Update personal feed title, description, cover, or sort order |
| `DELETE` | `/api/v1/personal-feeds/{id}` | Delete personal feed (optional `?delete_files=true`) |
| `GET` | `/api/v1/personal-feeds/{id}/episodes` | List all episodes in a personal feed |
| `POST` | `/api/v1/personal-feeds/{id}/episodes` | Add episode via `url`, `file_path`, `title`, `format`, `quality`, `max_height` |
| `POST` | `/api/v1/personal-feeds/{id}/upload` | Multipart file upload with `file`, `title`, `format`, `quality`, `max_height` |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/copy` | Copy episode to another personal feed (`target_feed_id`) |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/move` | Move episode to another personal feed (`target_feed_id`) |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/retry` | Retry failed personal feed episode download |
| `PATCH`, `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}` | Rename episode in personal feed (`{"title": "..."}`) and rebuild RSS XML |
| `DELETE` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}` | Delete episode from personal feed (optional `?delete_files=true`) |

---

## 5. Model Context Protocol (MCP) (`pkg/mcp/`)

Podsync exposes 28 MCP tools. `update_feed_config` applies only supplied fields, preserving omitted settings; audiobook feeds do not use cron schedules:
- `list_feeds`, `get_feed`, `add_feed`, `delete_feed`, `update_feed`, `update_all_feeds`, `update_feed_config`, `retry_episode`
- `list_personal_feeds`, `get_personal_feed`, `create_personal_feed`, `add_personal_episode`, `delete_personal_episode`, `rename_personal_episode`
- `list_audiobook_dirs`, `import_audiobook`, `delete_audiobook_dir`
- `get_download_errors`, `get_recent_downloads`, `get_sync_status`
- `get_system_stats`, `get_tokens`, `update_tokens`, `delete_tokens`
- `get_downloader_config`, `update_downloader_config`, `test_downloader`
- `get_system_logs`

Available over **Stdio** (`podsync --config config.toml --mcp`) and **HTTP/SSE** (`/mcp` and `/mcp/sse`).

When a local daemon is ready, stdio relays JSON-RPC directly to `POST /mcp` with
Basic Auth. All 28 tools, including personal feeds, resources and prompts are
served by that daemon. Set the MCP server version from the binary build version
before serving; proxied initialization preserves the daemon's version.

---

## 6. Golden Rules for AI Agents

1. **Never break existing platform builders**: Any modifications to `pkg/builder/` must preserve full backwards compatibility across all supported providers (YouTube, Rutube, VK Video, Vimeo, SoundCloud, Twitch, Audiobooks).
2. **Preserve BadgerDB integrity**: Always handle transactions cleanly and prevent corrupt keys.
3. **Always run `-race` tests**: After modifying Go code, run `go test -race ./...` to verify there are no data races.
4. **Instant XML Rebuild on Config Change**: Whenever feed metadata or `private_feed` is updated, ensure `m.updates <- f` is queued so the XML file on disk reflects changes immediately.
5. **Private Feeds Guarantee**: Generates `<itunes:block>yes</itunes:block>` in RSS XML when `feed.PrivateFeed || cfg.PrivateFeed`. Both in-memory `Config`, BadgerDB `Feed`, and `config.toml` must be kept in sync.
6. **Path Traversal Protection**: Any file or directory operations (`AudiobookDir`, `DeleteAudiobookDir`, uploads) must strictly validate with `filepath.Clean` and verify `filepath.Rel` stays inside the configured parent directory.
7. **Strict Brand & Theme Compliance (Catppuccin Mocha & Latte)**:
   - When modifying Web UI, strictly adhere to Catppuccin color tokens. Never let default Bootstrap blue or slate-gray leak through.
   - Synchronize all Web UI files (`index.html`, `admin.css`, `admin.js`, `theme.js`) byte-for-byte between `services/web/ui/` and root `html/`.
8. **Proper Agent Skill Formatting (`SKILL.md`)**:
   - In YAML frontmatter, always enclose scalar string values in double quotes (`"..."`).
   - Keep `.agents/skills/podsync/SKILL.md` and user config skill directories synchronized.
9. **Mandatory MCP Tools Synchronization & Automated Verification**:
   - Whenever adding, removing, or modifying MCP tools in `pkg/mcp/server.go`:
     1. Update `getToolDefinitions()` and `callTool()`.
     2. Update the `#tab-mcp` tools reference table in `services/web/ui/index.html` (tool name, description, arguments).
     3. Synchronize `html/index.html` byte-for-byte with `services/web/ui/index.html`.
     4. Update `docs/mcp.md` (tool count and reference table).
     5. Update `AGENTS.md` (tool count and list in Section 5).
     6. Update `SKILL.md` (and propagate to all 4 skill locations: `.agents/skills/podsync/`, `.gemini/skills/podsync/`, `~/.gemini/config/skills/podsync/`, `~/.gemini/antigravity-cli/skills/podsync/`).
     7. Run `go test -v -run TestMCPToolsDocumentationSync ./pkg/mcp/...` to automatically verify that all registered tools match the UI table and documentation.
10. **Button Metrics & Interaction Consistency**:
    - Preserve unified action pair button metrics: standard `btn-sm`, `min-width: 105px`, 35px desktop height, and >= 44px mobile touch target.
    - Cancel / Close buttons must always precede Save / Update / Confirm buttons, with secondary styling on the left and Mauve `.btn-brand` primary styling on the right.
11. **Strict English-Only Web UI (Internationalization Standard)**:
    - The Web Admin Console interface must be 100% in English.
    - All tab titles, navigation items, page headings, subtitles, button labels, form placeholders, badges, status indicators, empty states, modal text, and notifications must strictly be written in English.
    - Never introduce Russian, Cyrillic, or any non-English text into HTML templates, JavaScript code, or backend default strings (e.g. default personal feed name is `My Feed`, never `Моя лента`).
    - User input containing Cyrillic or non-ASCII characters (e.g., feed titles used as URL slugs/IDs) must be safely handled (e.g. automatic transliteration to ASCII/Latin slugs via `transliterateCyrillic` and red outline/validation for invalid characters).
12. **Mandatory Documentation & Skills Review After Changes**:
    - Before completing any change, review its impact on `README.md`, `README_RU.md`, `docs/`, and `.agents/skills/`. Update affected documentation and skills as part of the same change.
    - Keep English and Russian documentation consistent: update both README files and the corresponding guides in `docs/` and `docs/ru/` whenever documented behavior changes.
    - Cover changes to features, API/MCP behavior, transports, configuration, defaults, deployment, and troubleshooting, including fixes that do not add or remove tools. Keep examples, commands, endpoint descriptions, and tool counts accurate.
    - When agent workflows change, update the affected skills under `.agents/skills/` and synchronize their installed copies. For the Podsync skill, use all four locations listed in Rule 9.
    - Before reporting completion, check affected local links and examples and run applicable documentation synchronization checks. If no documentation or skill changes are needed, confirm that during the review.

---

## 7. Dependency & Infrastructure Architecture Notes

1. **Docker Build Layer Caching**:
   `Dockerfile` caches Go module downloads in a dedicated layer (`COPY go.mod go.sum ./` followed by `RUN go mod download`) prior to copying the full source tree (`COPY . .`). This prevents expensive dependency re-downloads on every source code commit.
   `.dockerignore` excludes Git history, documentation, agent configuration, local credentials, databases, and media. Pass `TAG` and `COMMIT` as Docker build arguments; do not rely on `.git` being available inside the builder. Release/nightly workflows and `make docker` supply these arguments.

2. **BadgerDB Migration Path (v1 to v4)**:
   - Podsync currently uses Badger v1 (`github.com/dgraph-io/badger`), which is stable and reliable for current single-node deployments.
   - Feed, episode, and job iterators check `ctx.Done()`. High-frequency queries read feed metadata without episodes from `feed/<feed_id>`, aggregate stats from `stats/<feed_id>`, and active jobs from `job_active/<id>`.
   - *Migration to Badger v4 (`github.com/dgraph-io/badger/v4`)*:
     1. Because Badger v1 and v4 on-disk table formats are incompatible, an automated migration utility must be added to `services/migrate/` (opening source DB in read-only mode with v1, iterating keys, and streaming to target directory using v4 writer).
     2. Transition `DeleteFeed` and job cleanups to native prefix dropping `db.DropPrefix()`.
     3. Enable native ZSTD compression in v4 badger options to reduce database footprint.

3. **AWS SDK Migration Path (v1 to v2)**:
   - `pkg/fs/s3.go` currently utilizes `github.com/aws/aws-sdk-go` v1.
   - When migrating to `github.com/aws/aws-sdk-go-v2`:
     1. Replace `s3.New(session)` with `s3.NewFromConfig(cfg)`.
     2. Replace `s3manager.Uploader` with `feature/s3/manager` v2.
     3. Update config parsing and credential provider resolution in `cmd/podsync/config.go`.

## 8. Developer Guides & Browser Tests

- Internal architecture, API, design, stack, workflow, and review guides live in [`docs/dev/`](docs/dev/README.md). Keep the root focused on `AGENTS.md` and the two README files.
- Browser regression tests live in `tests/e2e/`; install their pinned dependencies with `npm ci`, install Chromium with `npx playwright install chromium`, and run `npm test` from that directory. CI runs them in the **Browser tests** job.
- Dependency-free Node.js UI tests remain in `services/web/`.
