# Project Structure & Package Responsibilities

This document outlines the directory structure of **Podsync**, mapping each package and critical file to its responsibility in the overall architecture.

---

## 1. Directory Tree Overview

```
.
├── cmd/podsync/              # CLI entry point, configuration, and Feed Manager
├── html/                     # Mirrored Web UI static assets (admin.css, admin.js, index.html, theme.js)
├── pkg/
│   ├── builder/              # Platform builders (YouTube, VK, Rutube, Audiobooks, etc.)
│   ├── db/                   # BadgerDB key-value metadata store
│   ├── feed/                 # RSS XML, OPML generation, API key rotation
│   ├── fs/                   # Filesystem abstraction (Local, S3, Virtual Mounter)
│   ├── jobqueue/             # Persistent disk-backed job queue with crash recovery
│   ├── logbuffer/            # In-memory circular log buffer
│   ├── mcp/                  # Model Context Protocol server (Stdio, SSE, Proxy)
│   ├── model/                # Core domain types (Feed, Episode, Provider constants)
│   └── ytdl/                 # Wrapper around yt-dlp / youtube-dl CLI
├── services/
│   ├── migrate/              # Media filename migration tool
│   ├── update/               # Feed update orchestrator and episode matcher
│   └── web/                  # REST API server and Catppuccin Web Admin Console
│       └── ui/               # Embedded SPA assets (admin.css, admin.js, index.html, theme.js)
├── docs/                     # Guides, deployment documentation, and manuals
├── .agents/skills/podsync/   # Agent skill definition for MCP assistants
├── Dockerfile                # Multi-stage Docker container build
├── docker-compose.yml        # Docker compose service definition
└── config.toml.example       # Full configuration example
```

---

## 2. Package Breakdown

### 2.1 `cmd/podsync/` (Application Core)
- **`main.go`**: Parses command-line flags (`--config`, `--migrate-filenames`, `--mcp`), sets up signal trapping (`SIGINT`, `SIGTERM`), initializes BadgerDB, cron scheduler, web server, and starts the update queue listener.
- **`config.go`**: Struct definition for `Config` loaded from TOML (`pelletier/go-toml`). Resolves environment variable overrides (`PODSYNC_CONFIG_PATH`, `PODSYNC_YOUTUBE_API_KEY`, `PODSYNC_VKVIDEO_API_KEY`, `PODSYNC_ADMIN_ENABLED`, etc.).
- **`feed_manager.go`**: Implements `AppFeedManager`, which acts as the central coordinator implementing `web.AdminManager`. Handles in-memory feed maps, cron rescheduling, XML generation triggers, and `config.toml` updates.
- **`disk_unix.go` / `disk_windows.go`**: Platform-specific disk storage inspection (free, used, and total bytes via `sys/unix.Statfs`).

### 2.2 `pkg/builder/` (Content Ingestion)
- **`builder.go`**: Defines the `Builder` interface (`Build(ctx, cfg) (*model.Feed, error)`) and factory constructor `New(...)`.
- **`audiobook.go`**: Scans local directory for audio files, parses ID3 tags (`APIC` cover, album, artist, track number), applies natural alphanumeric sorting, and builds a serial podcast feed.
- **`vkvideo.go`**: Interfaces with the official VK API v5.199. Resolves screen names, fetches community/user videos, handles pagination, and filters out live streams.
- **`rutube.go`**: Scrapes and indexes Rutube channels, playlists, user videos, and single videos via `yt-dlp`.
- **`youtube.go`**: Queries YouTube Data API v3 for channel and playlist metadata.
- **`vimeo.go`**, **`soundcloud.go`**, **`twitch.go`**: Handlers for Vimeo, SoundCloud, and Twitch Helix APIs.
- **`url.go`**: URL parser determining provider (`youtube`, `vkvideo`, `rutube`, `audiobook`, `vimeo`, `soundcloud`, `twitch`).

### 2.3 `pkg/db/` (BadgerDB Data Layer)
- **`badger.go`**: Manages the lifecycle of the embedded BadgerDB instance.
- **`feed.go`**: Feed-level CRUD operations (`GetFeed`, `AddFeed`, `DeleteFeed`, `WalkFeeds`).
- **`episode.go`**: Episode-level operations (`GetEpisode`, `UpdateEpisode`, `DeleteEpisode`, `WalkEpisodes`).

### 2.4 `pkg/feed/` (RSS / iTunes Generation)
- **`xml.go`**: Generates RSS 2.0 XML with iTunes and Podcast 2.0 extensions. Enforces `itunes:block` for private feeds and `itunes:type=serial` for audiobooks.
- **`config.go`**: Feed configuration structs (`feed.Config`, `Filters`, `Custom`, `Cleanup`).
- **`keys.go`**: Thread-safe multi-key Round-Robin rotator for API keys.
- **`opml.go`**: Generates OPML subscriptions export file.

### 2.5 `pkg/fs/` (Storage & Mounter)
- **`storage.go`**: `Storage` interface for saving and retrieving media and XML files.
- **`local.go`**: Local filesystem storage implementation.
- **`s3.go`**: AWS S3 storage backend.
- **`mounter.go`**: `fs.Mounter` interface enabling zero-copy virtual folder mounting for local audiobook feeds.

### 2.6 `pkg/jobqueue/` (Persistent Job Queue)
- **`queue.go`**: Disk-backed persistent job queue with crash recovery and worker concurrency controls for managing asynchronous feed updates.

### 2.7 `pkg/logbuffer/` (In-Memory Log Streamer)
- **`buffer.go`**: Thread-safe circular ring buffer capturing system log entries for live querying and real-time frontend streaming.

### 2.8 `pkg/mcp/` (Model Context Protocol)
- **`server.go`**: Implements the MCP protocol server exposing 21 tools, resources (`podsync://feeds`, `podsync://system`, `podsync://tokens`), and prompt handlers.
- **`proxy.go`**: `HTTPClientManager` that forwards MCP commands to a running Podsync instance via HTTP REST.
- **`sse.go`**: SSE transport implementation for MCP clients.

### 2.9 `services/web/` (HTTP Server & Web UI)
- **`server.go`**: HTTP server initialization, route registration, rate limiting middleware, Basic Auth & session auth, and static asset embedding.
- **`api.go`**: REST API handlers (`/api/v1/*`) including feed operations, cover art upload, log querying, and system stats.
- **`auth.go`**: Session cookie authentication, login rate-limiting checks, and admin credential verification.
- **`feed_patch.go`**: Atomic partial feed configuration update handler.
- **`rate_limiter.go`**: In-memory sliding-window IP rate limiter preventing brute-force login attempts.
- **`admin_types.go`**: Data transfer objects (`FeedSummary`, `FeedConfigUpdate`, `AudiobookDirInfo`, `SystemStats`).
- **`ui/`**: Modular Single Page Application (`index.html`, `admin.css`, `admin.js`, `theme.js`) embedded via `//go:embed ui/*`.

### 2.10 `services/update/` (Updater Pipeline)
- **`updater.go`**: Manages the feed synchronization lifecycle: queries builders, checks episode statuses, downloads new media, runs hooks, cleans old files, and generates RSS XML.
- **`matcher.go`**: Regex and filter matching logic for episodes (title, description, duration, age).
