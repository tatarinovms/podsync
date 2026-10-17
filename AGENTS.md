# AGENTS.md

This file provides comprehensive architecture guidance, development workflows, and behavioral rules for autonomous AI coding agents (Antigravity, Claude Code, OpenCode, Hermes Agent, Cursor, Cline) working on this repository.

---

## 1. Project Overview & Tech Stack

**Podsync** is a Go-based service that converts video and audio channels, playlists, communities, and user profiles (YouTube, VK Video, Vimeo, SoundCloud, Twitch) into standards-compliant podcast RSS feeds with automatic media downloading, transcoding, disk management, and metadata storage.

### Core Technologies
- **Language**: Go 1.25+ (`go.mod`)
- **Storage/DB**: BadgerDB v3 (embedded key-value database for feed/episode metadata and state)
- **Filesystem**: Local filesystem storage or AWS S3 (`pkg/fs/`)
- **Downloader**: `yt-dlp` (native preference with fallback to `youtube-dl`), using `ffmpeg` for audio conversion
- **Scheduling**: `robfig/cron/v3` supporting cron expressions and duration intervals
- **Configuration**: TOML via `pelletier/go-toml`
- **Web UI**: Embedded single-page application (`services/web/ui/index.html`) styled with Catppuccin Mocha & Latte
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
│   └── disk_unix.go / windows.go # Platform-specific disk space calculations
├── pkg/
│   ├── builder/              # Platform builders fetching metadata from source APIs
│   │   ├── vkvideo.go        # VK Video builder (official VK API client, handle resolution)
│   │   ├── youtube.go        # YouTube Data API v3 builder
│   │   ├── vimeo.go          # Vimeo API builder
│   │   ├── soundcloud.go     # SoundCloud HTML scraper / client
│   │   ├── twitch.go         # Twitch Helix API builder
│   │   └── url.go            # URL parsing and platform detection
│   ├── db/                   # BadgerDB wrappers for Feeds, Episodes, and State
│   ├── feed/                 # Podcast RSS XML generation, OPML export, API key rotation
│   ├── fs/                   # Storage abstraction (Local file system and S3)
│   ├── mcp/                  # Model Context Protocol server (stdio & HTTP/SSE)
│   ├── model/                # Domain models (Feed, Episode, Provider constants)
│   └── ytdl/                 # Wrapper around yt-dlp / youtube-dl CLI
├── services/
│   ├── web/                  # HTTP Server, Web Admin Console (/admin), REST API (/api/v1)
│   ├── update/               # Feed update orchestrator and regex episode matcher
│   └── migrate/              # Tooling for media filename migration
├── docs/                     # Guides, token setup instructions, and MCP specs
│   └── deployment_guide_agents.md # Linux systemd service & Docker deployment for OpenCode and Hermes
├── .agents/skills/podsync/   # Standard agent skill definition for MCP
├── Dockerfile                # Multi-stage Docker build (Go builder + Alpine runtime with yt-dlp)
├── docker-compose.yml        # Docker compose configuration for running Podsync
└── config.toml.example       # Canonical configuration reference
```

---

## 3. Platform Implementations & Specifics

### VK Video (`pkg/builder/vkvideo.go`)
- **Supported URLs**: `vkvideo.ru`, `vk.com`, `vk.ru`
  - Handles: `https://vkvideo.ru/@channel_name`
  - Group videos: `https://vk.com/video/@group_name` or `https://vk.com/video-196446247`
  - Playlists/Albums: `https://vkvideo.ru/playlist/-196446247_1`
  - User videos: `https://vk.com/video/id12345`
- **VK API Details**:
  - Base URL: `https://api.vk.com/method/` (API version `5.199`).
  - Owner IDs for communities/groups **must be negative** when calling `video.get` (e.g. `owner_id: -196446247`).
  - Screen names (handles `@foo`) are resolved via `utils.resolveScreenName`.
  - Downloads use canonical URLs: `https://vk.com/video{owner_id}_{video_id}` passed to `yt-dlp`.
  - Live streams and upcoming premieres (`live_status: "started" | "upcoming"`) are automatically excluded from feed generation to avoid broken podcast downloads.
  - Requires user access token with `video` and `offline` scopes.

### YouTube (`pkg/builder/youtube.go`)
- Requires YouTube Data API v3 key (`PODSYNC_YOUTUBE_API_KEY`).
- Supports multi-key Round-Robin rotation to prevent quota exhaustion.
- Downloads run through `yt-dlp`. Supports passing Netscape format `cookies.txt` or `--cookies-from-browser` via downloader config to bypass bot verification.

---

## 4. Web Admin Console & REST API (`services/web/`)

- Accessible at `/admin` (SPA embedded into the binary via `//go:embed ui/*` from `services/web/ui/index.html`).
- **Embedded UI & Brand Color System**:
  - The UI uses **Catppuccin Mocha** (dark theme) and **Latte** (light theme) palettes.
  - **Strict adherence to brand colors (Catppuccin Mocha & Latte)**: All UI components, buttons, badges, switches, and backgrounds must strictly adhere to the Catppuccin palette via CSS variables (`--ctp-*`, `--btn-primary-bg`, `--btn-danger-bg`, etc.). Never allow raw Bootstrap defaults to leak through (such as default blue `#0d6efd`, slate-green `#2b3035` on `.bg-body-tertiary`, or unstyled `btn-outline-primary`). Primary save/action buttons must always use `.btn-primary` or `.btn-brand` (Mauve).
- Secured by HTTP Basic Auth and session cookies (`cfg.Server.Admin`).
- REST API mounted under `/api/v1`:
  - `/api/v1/feeds` (GET, POST)
  - `/api/v1/feeds/{id}` (GET, PUT/PATCH, DELETE)
  - `/api/v1/feeds/{id}/update` (POST)
  - `/api/v1/feeds/{id}/episodes/{ep_id}/retry` (POST)
  - `/api/v1/tokens` (GET, POST with optional append, DELETE with optional `?index={idx}`)
  - `/api/v1/auth/password` (POST)
  - `/api/v1/system` (GET)
  - `/api/v1/downloader` (GET, POST)
  - `/api/v1/downloader/cookies` (GET, POST, DELETE)

---

## 5. Model Context Protocol (MCP) (`pkg/mcp/`)

Podsync exposes a full suite of MCP tools and resources for autonomous AI agents:
- **Tools**:
  - `list_feeds`, `get_feed`, `add_feed`, `delete_feed`, `update_feed`, `update_all_feeds`, `update_feed_config`, `retry_episode`
  - `get_download_errors`, `get_recent_downloads`, `get_sync_status`
  - `get_system_stats`, `get_tokens`, `update_tokens`, `delete_tokens`
  - `get_downloader_config`, `update_downloader_config`, `test_downloader`
- **Embedded Resources**:
  - `podsync://feeds`: Live JSON snapshot of all registered podcast feeds, episode counts, and status.
- **Prompts**:
  - `diagnose_feed`: Troubleshoots download errors or feed parsing failures.

Available over:
- **Stdio**: `podsync --config config.toml --mcp` (proxies to live instance if running, or operates directly on local BadgerDB).
- **HTTP / SSE**: `/mcp` and `/mcp/sse`.
- Standard agent skill: [`.agents/skills/podsync/SKILL.md`](./.agents/skills/podsync/SKILL.md).

---

## 6. Development & Testing Commands

### Build
```bash
# Build binary into bin/podsync
make build
# or: go build -o bin/podsync ./cmd/podsync
```

### Run Tests
```bash
# Run all unit tests with race detection
go test -v -race ./...

# Run tests for a specific package
go test -v -race ./pkg/builder
go test -v -race ./cmd/podsync
```

### Code Formatting & Linting
```bash
# Run golangci-lint
golangci-lint run

# Format code
go fmt ./...
```

### Docker
```bash
# Build Docker image locally
docker build -t podsync:local .

# Run with docker compose
docker compose up -d
```

---

## 7. Golden Rules for AI Agents

1. **Never break existing platform builders**: Any changes to `pkg/builder/url.go` or `pkg/builder/builder.go` must maintain full backwards compatibility with YouTube, Vimeo, SoundCloud, Twitch, and VK Video.
2. **Preserve BadgerDB integrity**: Always close DB transactions and database instances properly. Do not write corrupted metadata keys.
3. **Always run `-race` tests**: After modifying any Go code, always run `go test -race ./...` to verify there are no data races or regressions.
4. **Clean up resources**: Background processes, test databases, and temporary download folders must always be safely terminated and cleaned up.
5. **No obsolete files**: Do not add unused IDE settings, duplicate skill files, or dead configuration templates to the root of the repository.
6. **Strict Brand & Theme Compliance (Catppuccin Mocha & Latte)**:
   - When modifying or creating UI components in the embedded Web UI (`services/web/ui/index.html`), strictly adhere to the **Catppuccin Mocha** (dark theme) and **Catppuccin Latte** (light theme) design system.
   - Always use project CSS variables (`--ctp-*`, `--ctp-base`, `--ctp-mantle`, `--ctp-crust`, `--ctp-surface0`, `--ctp-surface1`, `--ctp-surface2`, `--ctp-mauve`, `--ctp-text`, `--ctp-subtext0`, etc.) and standard styled classes (`.btn-primary`, `.btn-brand`, `.btn-secondary`, `.btn-outline-danger`).
   - **Never allow Bootstrap default colors to leak**: Raw Bootstrap 5 utility classes (e.g. `.bg-body-tertiary`, `.bg-body-secondary`, `.form-check-input`, `.form-switch`, unstyled focus rings, or default badges) carry hardcoded Bootstrap colors like slate-green `#2b3035` or blue `#0d6efd`. In Catppuccin Mocha/Latte, these appear as jarring, mismatched tints (e.g. greenish or bright blue patches). Ensure all container backgrounds, modal navigation tabs, switches (`.form-check-input:checked`), and utility backgrounds are explicitly mapped to the corresponding Catppuccin tokens (`--ctp-crust`, `--ctp-surface0`, `--ctp-mauve`).
   - Primary action/save buttons must always be styled with `--btn-primary-bg` / `--btn-brand` (Mauve), never unstyled blue Bootstrap buttons (`.btn-primary` without custom variable mapping or raw `btn-outline-primary`).
7. **Proper Agent Skill Formatting (`SKILL.md`)**:
   - In YAML frontmatter, always enclose scalar string values in double quotes (`"..."`), especially `description`, to prevent YAML parse errors when colons (`: `) or special characters are present (`mapping values are not allowed here`).
   - Never fabricate or hallucinate skill content: always read the real implementation and actual files in the repository (`.agents/skills/podsync/SKILL.md`) as the canonical source.
   - When updating tools, endpoints, or arguments, keep `.agents/skills/podsync/SKILL.md` and user skill directories in sync.
