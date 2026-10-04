# Podsync MCP Server (Model Context Protocol)

Podsync includes a built-in **MCP (Model Context Protocol)** server that enables AI assistants (such as **Claude Desktop**, **Cursor**, **Cline**, **Antigravity**, and other LLM agents) to directly monitor, manage, and interact with your podcast feeds and episodes.

---

## 🌟 Capabilities

The MCP server exposes:

### 🛠️ Tools
- **`list_feeds`**: List all configured podcast feeds, episode counts, total sizes, and feed URLs.
- **`get_feed`**: Get detailed metadata for a feed along with the list of episodes and their download statuses (`downloaded`, `new`, `error`).
- **`add_feed`**: Add a new podcast channel/playlist (YouTube, VK Video, Vimeo, etc.) with format (`audio`/`video`), quality (`high`/`low`), and page size. Automatically updates `config.toml`.
- **`delete_feed`**: Remove a feed and optionally delete its downloaded media files from disk.
- **`update_feed`**: Trigger an immediate update check and download cycle for a feed.
- **`retry_episode`**: Reset an episode that failed to download so Podsync attempts downloading it again.
- **`get_system_stats`**: Retrieve live system health, RAM usage, disk usage (`data_dir`), and availability of `yt-dlp` and `ffmpeg`.
- **`get_tokens`**: View current API key status (masked for security) for YouTube, VK Video, Vimeo, SoundCloud, and Twitch.
- **`update_tokens`**: Update API tokens (supports multiple tokens for automatic round-robin rotation).

### 📦 Resources
- **`podsync://feeds`**: Live JSON snapshot of all podcast feeds.
- **`podsync://system`**: System diagnostics, hardware usage, and binary paths.
- **`podsync://tokens`**: Current API token configurations and sources (ENV vs config.toml).

### 💡 Prompts
- **`diagnose_podsync`**: Comprehensive system diagnosis (missing tokens, download errors, system tools health).
- **`summarize_library`**: Executive summary of podcast feeds, disk usage, and suggested optimizations.

---

## 🚀 Running Modes

The Podsync MCP server can be used in two ways:

### 1. Stdio Mode (for Claude Desktop, Cursor, local AI tools)

In this mode, your AI client launches Podsync as a subprocess over standard input/output (`stdio`):

```bash
./bin/podsync --config config.toml --mcp
```

> [!TIP]
> If Podsync is already running as a daemon or container on your machine, `podsync --mcp` will automatically connect to the running instance over HTTP via its REST API (avoiding database lock conflicts). If Podsync is not running, it will open the local database directly.

#### Configuration for Claude Desktop

Add this to your Claude Desktop configuration file:
- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "podsync": {
      "command": "/absolute/path/to/bin/podsync",
      "args": [
        "--config",
        "/absolute/path/to/config.toml",
        "--mcp"
      ]
    }
  }
}
```

#### Configuration for OpenCode v2

OpenCode v2 configures MCP servers in `opencode.json` (or `opencode.jsonc`) placed in your project root or globally in `~/.config/opencode/opencode.json`:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "local",
        "command": [
          "/absolute/path/to/bin/podsync",
          "--config",
          "/absolute/path/to/config.toml",
          "--mcp"
        ],
        "enabled": true
      }
    }
  }
}
```

Or connect to an already running Podsync instance via **remote SSE**:

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "remote",
        "url": "http://localhost:8080/mcp/sse",
        "headers": {
          "Authorization": "Basic YWRtaW46cGFzc3dvcmQ=" // base64(admin:password)
        },
        "enabled": true
      }
    }
  }
}
```

Verify your setup in the terminal:
```bash
opencode mcp list
```

#### Configuration for Cursor / Cline

In your Cursor or Cline MCP Settings:
- **Name**: `podsync`
- **Type**: `command`
- **Command**: `/path/to/bin/podsync --config /path/to/config.toml --mcp`

---

## 🧠 Agent Skill

A ready-to-use skill definition for AI agents is provided in:
- [`.agents/skills/podsync/SKILL.md`](../.agents/skills/podsync/SKILL.md)
- [`.gemini/skills/podsync/SKILL.md`](../.gemini/skills/podsync/SKILL.md)

You can feed this skill directly to autonomous coding and operational agents (Antigravity, Gemini CLI, Claude Code, OpenCode) so the agent automatically understands when and how to call Podsync MCP tools, manage YouTube/VK subscriptions, retry failed downloads, and inspect system health.

---

### 2. HTTP / Server-Sent Events (SSE) Mode (for remote or web-connected AI tools)

When the Podsync server is running with the admin console enabled, the MCP server is mounted automatically:

- **SSE Streaming Endpoint**: `GET http://<host>:<port>/mcp/sse`
- **Messages Receiver**: `POST http://<host>:<port>/mcp/messages?sessionId=<id>`
- **Direct JSON-RPC Endpoint**: `POST http://<host>:<port>/mcp`

#### Authentication

The HTTP/SSE endpoints are protected with the same authentication as the web admin console. Provide your credentials using **HTTP Basic Authentication**:

```bash
# Direct JSON-RPC call example:
curl -u admin:password -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_system_stats","arguments":{}}}'
```

---

## Example Agent Interactions

Once configured in Claude Desktop or Cursor, you can ask your AI:

- *"What podcasts do I have in Podsync and how much disk space are they using?"*
- *"Add this YouTube playlist as an audio podcast: https://www.youtube.com/playlist?list=..."*
- *"Are there any episodes that failed to download? If so, please retry them."*
- *"Check system diagnostics and verify if ffmpeg and yt-dlp are found."*
- *"Add a new YouTube API key for rotation."*
