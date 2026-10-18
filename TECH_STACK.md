# Technology Stack & Dependencies

This document provides a comprehensive inventory of the programming languages, runtime dependencies, external binaries, third-party libraries, and UI frameworks used in **Podsync**.

---

## 1. Core Languages & Runtime

| Component | Technology | Version | Purpose |
|---|---|---|---|
| **Backend Language** | Go | 1.25+ | High-throughput, concurrent service runtime |
| **Frontend Language** | JavaScript (ES6+) | Modern Vanilla | Zero-build embedded SPA admin panel |
| **Markup & Styling** | HTML5 / CSS3 | Modern | Responsive UI using CSS custom properties |
| **Container Engine** | Docker / Compose | Engine 20.10+ | Multi-stage image packaging on Alpine Linux |

---

## 2. External System Binaries

These utilities must be present in the execution environment ($PATH) or packaged into the container:

| Binary | Minimum Version | Required By | Purpose |
|---|---|---|---|
| **`yt-dlp`** | Latest Stable | `pkg/ytdl/` | Media querying and downloading for YouTube, Rutube, and VK Video |
| **`ffmpeg`** | 4.x / 5.x / 6.x | `pkg/ytdl/`, `pkg/builder/audiobook.go` | Audio transcoding to MP3/M4A, duration probing, and embedded ID3 APIC cover art extraction |
| **`deno`** *(optional)* | 1.x / 2.x | `yt-dlp` arguments | Optional JavaScript runtime passed via `--js-runtimes` for advanced YouTube challenge solving |

---

## 3. Go Modules & Key Libraries

From [`go.mod`](file:///Volumes/HDD/project/podsync/go.mod):

### Storage & Persistence
- **`github.com/dgraph-io/badger/v3`**: Embedded key-value database providing ACID transactions, zero external server dependencies, and high-performance disk-backed caching.
- **`github.com/aws/aws-sdk-go`**: AWS S3 client SDK for offloading podcast audio/video hosting to cloud object storage.

### Scheduling & Synchronization
- **`github.com/robfig/cron/v3`**: Cron scheduler supporting standard 5-part cron expressions (`0 */4 * * *`) and duration intervals (`@every 12h`).
- **`golang.org/x/sync`**: ErroGroup synchronization for managing background daemon workers.

### Configuration & Formatting
- **`github.com/pelletier/go-toml`**: TOML decoder and tree manipulator used to load and dynamically persist changes to `config.toml`.
- **`github.com/jessevdk/go-flags`**: Command-line flag parsing.
- **`github.com/eduncan911/podcast`**: Podcast RSS 2.0 XML generator compliant with iTunes standards.
- **`github.com/gilliek/go-opml`**: OPML export generator.

### Platform APIs & Logging
- **`github.com/sirupsen/logrus`**: Structured logging with severity levels (`error`, `warn`, `info`, `debug`).
- **`google.golang.org/api`**: YouTube Data API v3 client.
- **`github.com/silentsokolov/go-vimeo`**: Vimeo API client.
- **`github.com/nicklaw5/helix`**: Twitch Helix API client.
- **`github.com/bogem/id3v2/v2`**: MP3 ID3 metadata tag reader.

---

## 4. Frontend & Styling Stack

Embedded directly within the Go binary (`//go:embed ui/*`):

| Component | Library / Framework | Version | Purpose |
|---|---|---|---|
| **CSS Framework** | Bootstrap | 5.3.3 | Responsive grid layout, modals, and navigation tabs |
| **Icon Set** | Bootstrap Icons | 1.11.3 | SVG iconography across admin console |
| **Color Theme** | Catppuccin | Mocha & Latte | Pastel dark and light themes implemented via CSS variables (`--ctp-*`) |
| **QR Generator** | QRious | 4.0.2 | Zero-dependency client-side QR code generator for smartphone subscriptions |

---

## 5. Protocols & Standards

- **RSS 2.0 & iTunes Podcast Specifications**: Compliant XML schema with audio/video enclosures, artwork, categories, explicit tags, and `<itunes:block>yes</itunes:block>` for private feeds.
- **Podcast 2.0**: Serial feed specification (`<itunes:type>serial</itunes:type>` and `<itunes:episode>N</itunes:episode>`) for episodic audiobook chapters.
- **Model Context Protocol (MCP)**: Version `2024-11-05`, exposing JSON-RPC 2.0 tools and resources over Stdio and HTTP Server-Sent Events (SSE).
- **Netscape Cookie Format**: Standard cookies header format used by `yt-dlp` to authenticate and bypass bot challenges.
