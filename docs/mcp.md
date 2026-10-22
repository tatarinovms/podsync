# Podsync MCP Server (Model Context Protocol)

**English** | [Русский](./ru/mcp.md)


Podsync includes a built-in **MCP (Model Context Protocol)** server that enables autonomous AI assistants and agents (**OpenCode**, **Hermes Agent / Nous Hermes**, **Claude Desktop**, **Cursor**, **Cline**, **Antigravity**) to directly monitor, manage, and interact with podcast feeds, downloads, and errors.

---

## 1. Overview & Architecture

The MCP server operates in two interchangeable modes:

```
┌────────────────────────────────────────────────────────┐
│               AI Agent / Assistant                     │
│  (OpenCode / Hermes / Claude Desktop / Cursor / Cline) │
└───────────────────────┬────────────────────────────────┘
                        │
         ┌──────────────┴──────────────┐
         │                             │
   [1. Stdio Mode]              [2. Remote SSE Mode]
   `podsync --mcp`             `http://<host>:8080/mcp/sse`
         │                             │
         ▼                             ▼
┌──────────────────┐          ┌──────────────────┐
│ Auto-Proxy /     │          │ Podsync Web      │
│ BadgerDB Storage │          │ HTTP Service     │
└──────────────────┘          └──────────────────┘
```

1. **Stdio Mode (`podsync --mcp`)**: Runs directly on the local machine over standard input/output.
   - **Auto-Proxy**: When launched, the CLI checks if a Podsync daemon is already running locally on `http://localhost:<server.port>` using `[server.admin]` credentials. If reachable, it automatically acts as an HTTP client proxy, routing all tool calls through the running daemon to prevent BadgerDB lock conflicts (`resource temporarily unavailable`).
   - **Scope & Limitations**: The auto-proxy only probes plain HTTP on `localhost` at the configured `server.port`. It does not support TLS (HTTPS), custom external domain names, or reverse-proxy URL prefixes (e.g., `/podsync/`). If your service is behind a reverse proxy or deployed remotely, connect via **Remote SSE Mode** instead.
   - **Standalone Mode**: If no daemon is listening on `localhost:<server.port>`, the CLI opens the database directly in standalone mode.
2. **Remote SSE Mode (`/mcp/sse`)**: Built into the running web service, allowing remote agents running on other machines, in separate containers, or behind reverse proxies (Nginx/Traefik with HTTPS) to connect over HTTP/HTTPS with Basic Auth.

---

## 2. Tools Reference

The MCP server provides 27 specialized tools:

### Monitoring & Status
| Tool Name | Arguments | Description |
|---|---|---|
| **`get_sync_status`** | `{}` | High-level status snapshot: active feeds, sync schedules, next cron runs, downloads past 24h, active error counts, and storage space. |
| **`get_download_errors`** | `feed_id` *(opt)*, `limit` *(opt, def: 20)* | List failed episode downloads with cleaned diagnostic causes (e.g., bot detection, 429 rate limits, private videos) and recommendations. |
| **`get_recent_downloads`** | `feed_id` *(opt)*, `limit` *(opt, def: 10)* | List recent successfully downloaded episodes with file sizes, publication dates, and direct media URLs. |
| **`get_system_stats`** | `{}` | Retrieve live system health, RAM usage, disk usage (`data_dir`), and availability of `yt-dlp` and `ffmpeg`. |
| **`get_system_logs`** | `level` *(opt)*, `search` *(opt)*, `feed_id` *(opt)*, `limit` *(opt, def: 50)* | Fetch recent system log entries from in-memory ring buffer, filterable by severity or query. |

### Feed Management
| Tool Name | Arguments | Description |
|---|---|---|
| **`list_feeds`** | `{}` | List all configured podcast feeds, episode counts, total sizes, and feed URLs. |
| **`get_feed`** | `feed_id` *(req)* | Get detailed metadata for a feed along with the list of episodes and statuses (`downloaded`, `new`, `error`). |
| **`add_feed`** | `url` *(req)*, `format`, `quality`, `page_size`, `update_period`, `language` | Add a new podcast channel/playlist (YouTube, VK Video, Vimeo, etc.). Automatically updates `config.toml`. |
| **`delete_feed`** | `feed_id` *(req)*, `delete_files` *(opt, def: false)* | Remove a feed and optionally delete its downloaded media files from disk. |
| **`update_feed`** | `feed_id` *(req)* | Trigger an immediate update check and download cycle for a feed. |
| **`update_all_feeds`** | `{}` | Trigger an immediate update check and download cycle for all configured feeds. |
| **`update_feed_config`** | `feed_id` *(req)*, `cron_schedule`, `format`, `quality`, `page_size`, `keep_last`, `max_height`, `playlist_sort`, `private_feed`, `language` | Update supplied settings and save to `config.toml`; omitted fields are preserved. Invalid schedules are rejected. Audiobooks ignore cron schedules. |
| **`retry_episode`** | `feed_id` *(req)*, `episode_id` *(req)* | Reset an episode that failed to download so Podsync attempts downloading it again. |

### Downloader & Authentication
| Tool Name | Arguments | Description |
|---|---|---|
| **`get_downloader_config`** | `{}` | Get yt-dlp binary path, custom binary override, version, self-update status, timeout, cookies status, and proxy settings. |
| **`update_downloader_config`** | `timeout`, `proxy`, `cookies_from_browser`, `language`, `custom_binary`, `self_update` | Update yt-dlp downloader options, proxy, preferred audio language, custom binary override, or self-update behavior. |
| **`test_downloader`** | `url` *(opt)* | Test yt-dlp downloader against a video URL to verify cookies and connectivity. |
| **`get_tokens`** | `{}` | View current API key status (masked for security) for YouTube, VK Video, Rutube, Vimeo, SoundCloud, and Twitch. |
| **`update_tokens`** | `provider` *(req)*, `tokens` *(req)* | Update API tokens (supports multiple tokens for automatic round-robin rotation). |
| **`delete_tokens`** | `provider` *(req)*, `index` *(opt)* | Delete a specific API token by index, or clear all configured tokens for a provider from `config.toml`. |

### Personal Feeds Management
| Tool Name | Arguments | Description |
|---|---|---|
| **`list_personal_feeds`** | `{}` | List all curated personal podcast feeds, episode counts, total sizes, and feed URLs. |
| **`get_personal_feed`** | `feed_id` *(req)* | Get metadata and all episodes (with status, source URL/file, and media URL) for a personal feed. |
| **`create_personal_feed`** | `title` *(req)*, `description`, `cover_art`, `playlist_sort`, `private_feed` | Create a new curated personal podcast feed. |
| **`add_personal_episode`** | `feed_id` *(req)*, `url` *(opt)*, `file_path` *(opt)*, `title` *(opt)*, `format` *(opt: audio/video)* | Add an episode to a personal feed from a URL (YouTube, VK, Rutube, direct audio/video link) or a local file path. |
| **`delete_personal_episode`** | `feed_id` *(req)*, `episode_id` *(req)* | Delete an episode from a personal feed and clean up storage. |

### Audiobooks Management
| Tool Name | Arguments | Description |
|---|---|---|
| **`list_audiobook_dirs`** | `{}` | List available directories in audiobooks storage, showing track count, total size, cover status, and linked feed status. |
| **`import_audiobook`** | `dir_name` *(req)*, `feed_id`, `title`, `author`, `description` | Import a local audiobook folder into a serial podcast feed (Podcast 2.0 / Apple Podcasts serial format). Audiobooks do not use cron schedules. |
| **`delete_audiobook_dir`** | `dir_name` *(req)* | Delete an audiobook folder and all its audio files from disk (and remove any linked podcast feed). |

### Resources & Prompts
- **`podsync://feeds`**: Live JSON snapshot of all podcast feeds.
- **`podsync://system`**: System diagnostics, hardware usage, and binary paths.
- **`podsync://tokens`**: Current API token configurations and sources (ENV vs config.toml).
- **`diagnose_podsync`**: Comprehensive system diagnosis (missing tokens, download errors, system tools health).
- **`summarize_library`**: Executive summary of podcast feeds, disk usage, and suggested optimizations.

---

## 3. How to Deploy & Connect MCP

### Method A: OpenCode v2

OpenCode v2 configures MCP servers in `opencode.json` (or `opencode.jsonc`) placed in your project root or globally in `~/.config/opencode/opencode.json`.

#### Option 1: Remote SSE Mode (Recommended for running services)
```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "remote",
        "url": "http://localhost:8080/mcp/sse",
        "headers": {
          "Authorization": "Basic YWRtaW46c2VjcmV0cGFzc3dvcmQ=" // base64(username:password)
        },
        "enabled": true
      }
    }
  }
}
```

#### Option 2: Local Stdio Mode
```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "local",
        "command": [
          "/opt/podsync/bin/podsync",
          "--config",
          "/etc/podsync/config.toml",
          "--mcp"
        ],
        "enabled": true
      }
    }
  }
}
```

Verify in OpenCode:
```bash
opencode mcp list
```

---


### Stdio Proxy vs Standalone Mode
When starting with `podsync --config config.toml --mcp`:
1. **HTTP Proxy Mode**: Podsync checks if a primary daemon is already running locally on `http://localhost:<server.port>`. If reachable, `--mcp` connects as a lightweight HTTP client proxy via Basic Auth credentials from `[server.admin]`. Note: this check targets `localhost` on the configured `server.port` (without reverse-proxy path or TLS wrappers).
2. **Standalone Daemon Mode**: If no local server is listening on `localhost:<server.port>`, the `--mcp` process initializes the database, storage, and feed manager directly to serve MCP tool requests.

### Method B: Hermes Agent (Nous Hermes)

For Hermes Agent or any agent supporting standard MCP configuration schemas (`hermes.json` / `settings.json`):

#### Option 1: Stdio Process
```json
{
  "mcpServers": {
    "podsync": {
      "command": "/opt/podsync/bin/podsync",
      "args": [
        "--config",
        "/etc/podsync/config.toml",
        "--mcp"
      ],
      "env": {
        "PODSYNC_CONFIG_PATH": "/etc/podsync/config.toml"
      }
    }
  }
}
```

#### Option 2: Remote SSE Transport
```json
{
  "mcpServers": {
    "podsync": {
      "url": "http://127.0.0.1:8080/mcp/sse",
      "headers": {
        "Authorization": "Basic YWRtaW46c2VjcmV0cGFzc3dvcmQ="
      }
    }
  }
}
```

---

### Method C: Claude Desktop

Add to your Claude Desktop configuration file:
- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "podsync": {
      "command": "/opt/podsync/bin/podsync",
      "args": [
        "--config",
        "/etc/podsync/config.toml",
        "--mcp"
      ]
    }
  }
}
```

---

### Method D: Cursor / Cline

In your Cursor or Cline MCP Settings:
- **Name**: `podsync`
- **Type**: `command`
- **Command**: `/opt/podsync/bin/podsync --config /etc/podsync/config.toml --mcp`

---

## 4. Testing & Verification

### Test via Terminal (Direct JSON-RPC)
You can directly test the MCP server over HTTP using `curl`:

```bash
# Call get_sync_status
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_sync_status","arguments":{}}}'

# Call get_download_errors
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_download_errors","arguments":{"limit":5}}}'

# Call get_recent_downloads
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_recent_downloads","arguments":{"limit":5}}}'
```

### Agent Skill Integration
To make AI agents fully autonomous when managing Podsync, provide them with the skill definition in:
- [`.agents/skills/podsync/SKILL.md`](../.agents/skills/podsync/SKILL.md)
