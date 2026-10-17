# Podsync MCP Server (Model Context Protocol)

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

1. **Stdio Mode (`podsync --mcp`)**: Runs directly on the local machine over standard input/output. If Podsync is already running as a daemon or container, the CLI tool automatically proxies calls to the running instance over HTTP, avoiding database lock conflicts.
2. **Remote SSE Mode (`/mcp/sse`)**: Built into the running web service, allowing remote agents running on other machines or in containers to connect over HTTP/HTTPS with Basic Auth.

---

## 2. Tools Reference

The MCP server provides 15 specialized tools:

### Monitoring & Status
| Tool Name | Arguments | Description |
|---|---|---|
| **`get_sync_status`** | `{}` | High-level status snapshot: active feeds, sync schedules, next cron runs, downloads past 24h, active error counts, and storage space. |
| **`get_download_errors`** | `feed_id` *(opt)*, `limit` *(opt, def: 20)* | List failed episode downloads with cleaned diagnostic causes (e.g., bot detection, 429 rate limits, private videos) and recommendations. |
| **`get_recent_downloads`** | `feed_id` *(opt)*, `limit` *(opt, def: 10)* | List recent successfully downloaded episodes with file sizes, publication dates, and direct media URLs. |
| **`get_system_stats`** | `{}` | Retrieve live system health, RAM usage, disk usage (`data_dir`), and availability of `yt-dlp` and `ffmpeg`. |

### Feed Management
| Tool Name | Arguments | Description |
|---|---|---|
| **`list_feeds`** | `{}` | List all configured podcast feeds, episode counts, total sizes, and feed URLs. |
| **`get_feed`** | `feed_id` *(req)* | Get detailed metadata for a feed along with the list of episodes and statuses (`downloaded`, `new`, `error`). |
| **`add_feed`** | `url` *(req)*, `format`, `quality`, `page_size`, `update_period` | Add a new podcast channel/playlist (YouTube, VK Video, Vimeo, etc.). Automatically updates `config.toml`. |
| **`delete_feed`** | `feed_id` *(req)*, `delete_files` *(opt, def: false)* | Remove a feed and optionally delete its downloaded media files from disk. |
| **`update_feed`** | `feed_id` *(req)* | Trigger an immediate update check and download cycle for a feed. |
| **`retry_episode`** | `feed_id` *(req)*, `episode_id` *(req)* | Reset an episode that failed to download so Podsync attempts downloading it again. |

### Downloader & Authentication
| Tool Name | Arguments | Description |
|---|---|---|
| **`get_downloader_config`** | `{}` | Get yt-dlp downloader configuration, cookies status, and proxy settings. |
| **`update_downloader_config`** | `timeout`, `proxy`, `cookies_from_browser` | Update yt-dlp downloader options. |
| **`test_downloader`** | `url` *(opt)* | Test yt-dlp downloader against a video URL to verify cookies and connectivity. |
| **`get_tokens`** | `{}` | View current API key status (masked for security) for YouTube, VK Video, Vimeo, SoundCloud, and Twitch. |
| **`update_tokens`** | `provider` *(req)*, `tokens` *(req)* | Update API tokens (supports multiple tokens for automatic round-robin rotation). |
| **`delete_tokens`** | `provider` *(req)* | Delete and clear configured API tokens for a provider from `config.toml`. |

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
