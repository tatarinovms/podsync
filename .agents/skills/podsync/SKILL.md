---
name: "podsync"
description: "Manage Podsync podcast generator: subscribe to YouTube/Rutube/VK Video/Vimeo channels and playlists as podcasts, trigger episode updates, retry downloads, check disk space/health, rotate API keys, and check or install verified native releases."
version: "1.0.2"
triggers:
  - "podsync"
  - "podcast"
  - "youtube podcast"
  - "vk video podcast"
  - "rutube podcast"
  - "retry episode"
  - "feed update"
---

# Podsync Skill

This skill provides comprehensive instructions for AI agents on how to monitor, diagnose, and manage [Podsync](https://github.com/mxpv/podsync) instances using **MCP (Model Context Protocol)** or the **REST API**.

---

## MCP Connection and Library Scope

- In updated builds, `podsync --mcp` probes `http://localhost:<server.port>/health/ready`. When the daemon is ready, it relays the complete JSON-RPC exchange to `POST /mcp` with Basic Auth from `[server.admin]`. All 32 tools, including the six personal-feed tools, resources, and prompts are served by that daemon. If no daemon is ready, stdio initializes a standalone instance with the local database.
- `initialize` reports the serving binary's build version in `serverInfo.version` (`dev` if unspecified). In proxy mode this is the daemon's version, which may differ from the stdio client's version.
- For a complete library inventory, call both `list_feeds` and `list_personal_feeds`. The first lists channel feeds and audiobooks; personal feeds have a separate tool. An empty result from either tool does not mean the entire library is empty.
- Older stdio clients can report `personal feeds service is not enabled` while direct HTTP MCP works. Compare `tools/list` and `list_personal_feeds` through authenticated `POST /mcp` before concluding that personal feeds are absent. Updating and restarting the stdio client enables the relay; update the daemon as well to correct an older hardcoded `serverInfo.version`.

## MCP Tools Reference

When connected to Podsync's MCP server (`podsync --mcp`, `POST /mcp`, or `/mcp/sse`), use the following tools:

### 1. `list_feeds`
- **Purpose**: Lists configured channel feeds and audiobooks, episode count, downloaded count, error count, disk space used, and direct RSS URLs. Use `list_personal_feeds` for curated personal feeds.
- **Arguments**: None `{}`.
- **When to use**: Whenever the user asks:
  - *"What podcasts do I have?"*
  - *"How much space do my podcasts take?"*
  - *"Give me the RSS link for my feed."*

### 2. `get_feed`
- **Purpose**: Retrieves full metadata for a specific podcast channel and lists its episodes with status (`downloaded`, `new`, `error`).
- **Arguments**:
  - `feed_id` *(string, required)*: The unique identifier of the feed (e.g., `LABELCOM`, `PODCAST1`).
- **When to use**: Inspecting episodes, checking if a new episode was downloaded, or viewing error details.

### 3. `add_feed`
- **Purpose**: Subscribes to a new video or audio channel/playlist/user and generates a podcast RSS feed for it. Automatically appends the feed to `config.toml`.
- **Arguments**:
  - `url` *(string, required)*: Source channel or playlist URL (e.g. YouTube, Rutube, VK Video, Vimeo, SoundCloud).
  - `format` *(string, optional)*: `"audio"` (default, MP3/M4A) or `"video"` (MP4).
  - `quality` *(string, optional)*: `"high"` (default) or `"low"`.
  - `page_size` *(integer, optional)*: How many recent episodes to fetch/keep (default: 50).
  - `update_period` *(string, optional)*: Update interval (e.g. `"1h"`, `"6h"`, `"12h"`, or cron expression).
  - `language` *(string, optional)*: Preferred audio track language code (e.g. `"ru"`, `"en"`) for multi-audio videos and YouTube dubbing.
- **When to use**: Adding a new show or channel. Always confirm the feed ID and URL with the user.

### 4. `delete_feed`
- **Purpose**: Removes a podcast feed from Podsync and removes it from `config.toml`.
- **Arguments**:
  - `feed_id` *(string, required)*: ID of the feed to delete.
  - `delete_files` *(boolean, optional, default: false)*: Set to `true` to delete downloaded media files from disk.
- **When to use**: Deleting a feed. *Agent note: Always confirm with the user before deleting files!*

### 5. `update_feed`
- **Purpose**: Triggers an immediate download and synchronization check for a feed without waiting for the cron timer.
- **Arguments**:
  - `feed_id` *(string, required)*: ID of the feed to update.
- **When to use**: When the user says *"Check for new episodes now"* or *"Update my podcast"*.

### 6. `update_all_feeds`
- **Purpose**: Triggers an immediate download and synchronization check for all configured podcast feeds at once.
- **Arguments**: None `{}`.
- **When to use**: When the user says *"Update all podcasts"*, *"Sync everything"*, or wants a bulk update.

### 7. `update_feed_config`
- **Purpose**: Updates configuration settings for an existing podcast feed (cron schedule, format, quality, page_size, retention keep_last, max_height, playlist_sort, private_feed, language) and saves changes to `config.toml`. Only supplied fields change; omitted settings are preserved. Invalid schedules are rejected. Audiobooks do not use cron schedules.
- **Arguments**:
  - `feed_id` *(string, required)*: Feed identifier.
  - `cron_schedule` *(string, optional)*: Cron expression or interval (e.g. `"@every 6h"`).
  - `page_size` *(integer, optional)*: Fetch count (e.g. `50`).
  - `format` *(string, optional)*: `"audio"` or `"video"`.
  - `quality` *(string, optional)*: `"high"` or `"low"`.
  - `keep_last` *(integer, optional)*: Retention policy (e.g. `10` to keep last 10 episodes on disk).
  - `max_height` *(integer, optional)*: Video resolution limit (e.g. `720`, `1080`).
  - `playlist_sort` *(string, optional)*: `"asc"` or `"desc"`.
  - `private_feed` *(boolean, optional)*: Mark feed private.
  - `language` *(string, optional)*: Preferred audio track language code (e.g. `"ru"`, `"en"`) for multi-audio videos and YouTube dubbing.
- **When to use**: Changing how often a podcast updates, switching from audio to video, setting retention limits, or adjusting download parameters.

### 8. `retry_episode`
- **Purpose**: Clears the error state of an episode that failed to download (e.g. due to temporary network error or rate limit) so Podsync re-attempts downloading it.
- **Arguments**:
  - `feed_id` *(string, required)*: Feed identifier.
  - `episode_id` *(string, required)*: Episode identifier.
- **When to use**: Fixing failed episodes reported in `get_feed` or `diagnose_podsync`.

### 9. `list_personal_feeds`
- **Purpose**: Lists all curated personal podcast feeds, their episode counts, total sizes, and public RSS URLs.
- **Arguments**: None `{}`.
- **When to use**: When the user asks to see their personal or custom podcast feeds.

### 10. `get_personal_feed`
- **Purpose**: Retrieves full details and all episodes for a specific personal feed, including episode processing status, error messages, and media URLs.
- **Arguments**:
  - `feed_id` *(string, required)*: The personal feed identifier.
- **When to use**: Checking the episodes, download status, or media links in a personal feed.

### 11. `create_personal_feed`
- **Purpose**: Creates a new curated personal podcast feed. Personal feeds are universal and can contain both audio and video episodes.
- **Arguments**:
  - `title` *(string, required)*: Feed title.
  - `description` *(string, optional)*: Feed description.
  - `cover_art` *(string, optional)*: Public URL to cover image.
  - `playlist_sort` *(string, optional)*: Episode sort order (`"desc"` or `"asc"`).
  - `private_feed` *(boolean, optional)*: Whether to block feed indexing with `<itunes:block>yes</itunes:block>`.
- **When to use**: When the user wants to create a new personal / custom podcast feed.

### 12. `add_personal_episode`
- **Purpose**: Adds an episode to a personal feed from a URL (YouTube, VK, Rutube, direct media link like MP3, OGG, MP4) or a local file path on the server (`file_path`). Automatically detects format or accepts an explicit format override (`"audio"`, `"video"`, or `"auto"`), with optional audio quality (`"high"`, `"low"`) and max video height (`480`, `720`, `1080`...).
- **Arguments**:
  - `feed_id` *(string, required)*: Target personal feed identifier.
  - `url` *(string, optional)*: Source media or video URL (YouTube, Rutube, VK Video, direct MP3/OGG/MP4 link).
  - `file_path` *(string, optional)*: Local server file path to import directly into the feed.
  - `title` *(string, optional)*: Episode title override.
  - `format` *(string, optional)*: Preferred episode media format (`"audio"`, `"video"`, or `"auto"`). Auto-detected if omitted.
  - `quality` *(string, optional)*: Media quality preference (`"high"` [default] or `"low"`).
  - `max_height` *(integer, optional)*: Maximum video height (e.g. `480`, `720`, `1080`). Defaults to 0 (unlimited/original).
- **When to use**: Adding media files or URLs to a personal feed via MCP.

### 13. `delete_personal_episode`
- **Purpose**: Deletes an episode from a personal feed and removes its media file from storage.
- **Arguments**:
  - `feed_id` *(string, required)*: Feed identifier.
  - `episode_id` *(string, required)*: Episode identifier.
  - `delete_files` *(boolean, optional, default: false)*: Whether to delete the media file from disk if unreferenced by other feeds.
- **When to use**: Removing an episode from a personal feed.

### 14. `rename_personal_episode`
- **Purpose**: Renames an episode in a personal podcast feed and immediately rebuilds the feed's public RSS XML file.
- **Arguments**:
  - `feed_id` *(string, required)*: Personal feed identifier (e.g. `personal-xyz` or `personal`).
  - `episode_id` *(string, required)*: Episode identifier to rename.
  - `title` *(string, required)*: New title for the episode.
- **When to use**: When the user requests renaming an episode in a personal feed.

### 16. `get_system_stats`
- **Purpose**: Returns memory usage, disk space (total, used, free on `data_dir`), uptime, and whether `ffmpeg` and `yt-dlp` are installed and functioning.
- **Arguments**: None `{}`.
- **When to use**: Health checks, checking remaining disk capacity, or troubleshooting download failures.

### 16. `get_tokens`
- **Purpose**: Returns masked API keys for all providers (`youtube`, `vkvideo`, `vimeo`, `soundcloud`, `rutube`) and indicates if they are loaded from ENV or `config.toml`.
- **Arguments**: None `{}`.

### 17. `update_tokens`
- **Purpose**: Updates API keys for a provider in `config.toml`. Accepts multiple keys to enable automatic Round-Robin quota rotation (especially useful for YouTube API).
- **Arguments**:
  - `provider` *(string, required)*: `"youtube"`, `"vkvideo"`, `"vimeo"`, `"soundcloud"`, or `"rutube"`.
  - `tokens` *(array of strings, required)*: List of API keys.

### 18. `delete_tokens`
- **Purpose**: Deletes a specific API token by index, or clears all configured API tokens for a provider from `config.toml`.
- **Arguments**:
  - `provider` *(string, required)*: `"youtube"`, `"vkvideo"`, `"vimeo"`, `"soundcloud"`, or `"rutube"`.
  - `index` *(integer, optional)*: 0-based index of a specific token to delete. If omitted or negative, all tokens for the provider are removed.
- **When to use**: Removing invalid, revoked, or unnecessary API keys (either a single key from rotation or the entire provider).

### 19. `get_downloader_config`
- **Purpose**: Retrieves yt-dlp binary path, version, ffmpeg version, current timeout, proxy, and status of installed cookies file (`cookies.txt` line count, file size, last modified).
- **Arguments**: None `{}`.
- **When to use**: Diagnosing YouTube download failures such as bot verification blocks or rate limits.

### 20. `update_downloader_config`
- **Purpose**: Updates yt-dlp downloader configuration (download timeout, proxy URL, preferred audio language, or browser cookie extraction).
- **Arguments**:
  - `timeout` *(integer, optional)*: Timeout in minutes.
  - `proxy` *(string, optional)*: Proxy URL (e.g. `socks5://127.0.0.1:1080` or `http://proxy:8080`).
  - `cookies_from_browser` *(string, optional)*: Browser name to extract session cookies from (e.g. `chrome`, `firefox`).
  - `language` *(string, optional)*: Global preferred audio track language code (e.g. `"ru"`, `"en"`) for multi-audio videos and YouTube dubbing.

### 21. `test_downloader`
- **Purpose**: Tests yt-dlp connectivity and cookie verification against a video URL without downloading the file.
- **Arguments**:
  - `url` *(string, optional)*: Video URL to test (defaults to YouTube sample video).

---


### 22. `get_download_errors`
- **Purpose**: Returns a diagnostic list of all failed podcast episodes across all feeds or for a specific feed, with exact error causes (e.g. YouTube bot verification, private video, 429 rate limits, ffmpeg errors) and actionable recommendations.
- **Arguments**:
  - `feed_id` *(string, optional)*: Filter errors by feed ID.
  - `limit` *(integer, optional, default: 20, max: 100)*: Maximum error records to return.
- **When to use**: Answering *"Are there any download errors?"*, *"Why did download fail on channel X?"*, or before proposing retries.

### 23. `get_recent_downloads`
- **Purpose**: Returns the most recently downloaded podcast episodes across all channels or for a single feed, complete with file sizes, durations, publication dates, and playable audio/video URLs.
- **Arguments**:
  - `feed_id` *(string, optional)*: Filter recent downloads by feed ID.
  - `limit` *(integer, optional, default: 10, max: 100)*: Number of episodes to return.
- **When to use**: Answering *"What was downloaded recently?"*, *"Show me latest episodes from channel X"*, or checking if a new episode is already available.

### 24. `get_sync_status`
- **Purpose**: Returns a high-level operational status overview: active feeds, last sync times, next cron execution for each feed, number of episodes downloaded in the past 24 hours, failed episode counts, and disk space free.
- **Arguments**: None `{}`.
- **When to use**: General status checks (*"Is Podsync working?"*, *"When is the next sync?"*).

### 25. `get_system_logs`
- **Purpose**: Retrieves recent system and application logs from Podsync's in-memory ring buffer, filterable by severity level (`error`, `warn`, `info`, `debug`) or search query.
- **Arguments**:
  - `limit` *(integer, optional, default: 50, max: 200)*: Number of log entries to retrieve.
  - `level` *(string, optional, default: "all")*: Filter by severity (`"all"`, `"error"`, `"warn"`, `"info"`, `"debug"`).
  - `search` *(string, optional)*: Case-insensitive search string (e.g. `"vk"`, `"token"`, `"youtube"`).
  - `feed_id` *(string, optional)*: Filter logs for a specific feed ID.
- **When to use**: Diagnosing issues when feed updates fail, missing API tokens, network problems, or yt-dlp execution failures.


### 26. `list_audiobook_dirs`
- **Purpose**: Discovers and lists all folders in the configured audiobooks directory (`[audiobooks].dir`), reporting track count, total size, cover image presence, and whether the folder is already linked to an active podcast feed.
- **Arguments**: None `{}`.
- **When to use**: When the user asks what audiobooks are available on the server, wants to see unlinked folders, or wants to check local audiobook storage.

### 27. `import_audiobook`
- **Purpose**: Imports a local audiobook folder into a serial podcast feed (Podcast 2.0 / Apple Podcasts compliant with `<itunes:type>serial</itunes:type>` and `<itunes:episode>N</itunes:episode>`). Chapters are naturally sorted (01, 02... 10) with automatic ID3/APIC cover extraction.
- **Arguments**:
  - `dir_name` *(string, required)*: Directory name in the audiobooks directory (e.g. `"George Orwell - 1984"`).
  - `feed_id` *(string, optional)*: Unique slug/ID (auto-generated from title or folder if omitted).
  - `title` *(string, optional)*: Book title override (defaults to folder name or ID3 album tag).
  - `author` *(string, optional)*: Author/narrator override (defaults to parsed author or ID3 artist tag).
  - `description` *(string, optional)*: Book synopsis override.
  - `cron_schedule` *(string, optional)*: Update schedule (defaults to `@every 12h`).
- **When to use**: Setting up an audiobook feed from a local directory on disk.

### 28. `delete_audiobook_dir`
- **Purpose**: Deletes an audiobook folder and all its audio files from the audiobooks storage on disk. If the folder is linked to an active podcast feed, it automatically removes the feed, unmounts it, and deletes it from `config.toml` and database.
- **Arguments**:
  - `dir_name` *(string, required)*: Directory name in the audiobooks directory to delete (e.g. `"George Orwell - 1984"`).
- **When to use**: When the user asks to delete, clean up, or remove an audiobook folder from the server. Always confirm with the user before deleting.

### 29. `get_network_config`
- **Purpose**: Returns custom DNS server IP addresses used for provider API and RSS metadata requests.
- **Arguments**: None `{}`.

### 30. `update_network_config`
- **Purpose**: Updates custom DNS servers immediately and saves them to `config.toml`.
- **Arguments**:
  - `dns_servers` *(array of strings, required)*: IPv4 or IPv6 DNS server addresses. Pass `[]` to restore the operating system resolver.

### 31. `get_podsync_update`
- **Purpose**: Checks the latest Podsync GitHub Release and reports the installed version, the latest version, the release URL, and whether this installation can update itself.
- **Arguments**: None `{}`.
- **When to use**: When the user wants to check whether Podsync can be updated.

### 32. `update_podsync`
- **Purpose**: Downloads the matching latest GitHub Release, verifies its SHA-256 checksum, replaces the native Podsync binary, and restarts the process.
- **Arguments**: None `{}`.
- **When to use**: Only after the user asks to install the current Podsync release. It is unavailable for Docker deployments.

## MCP Resources

Agents can read live context from Podsync via resources:
- `podsync://feeds`: JSON list of all configured podcast feeds.
- `podsync://system`: Hardware metrics, memory, disk, and tool availability.
- `podsync://tokens`: API tokens status and sources.

---

## MCP Prompts

- **`diagnose_podsync`**: Runs a full diagnostic scan across system metrics, tokens, and feeds to highlight errors or bottlenecks.
- **`summarize_library`**: Produces a structured executive summary of all podcast channels and storage consumption.

---

## Fallback: REST API Usage (via curl / http)

If MCP tools are not directly loaded into the agent context, use standard HTTP requests to `http://localhost:8080/api/v1`:

```bash
# Basic Auth credentials from config.toml ([server.admin])
USER="admin"
PASS="password"
HOST="http://localhost:8080"

# List feeds (full summaries with episode counts and sizes)
curl -u "$USER:$PASS" "$HOST/api/v1/feeds"

# Check lightweight feed status (in-memory state: idle, queued, running)
curl -u "$USER:$PASS" "$HOST/api/v1/feeds/status"

# Add feed
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/feeds" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://youtube.com/channel/...","format":"audio","quality":"high"}'

# Trigger update for a single feed
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/feeds/FEED_ID/update"

# Trigger bulk update for all configured feeds
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/feeds/update"

# System health
curl -u "$USER:$PASS" "$HOST/api/v1/system"

# Audiobooks API
# 1. List available audiobook directories
curl -u "$USER:$PASS" "$HOST/api/v1/audiobooks/dirs"

# 2. Import an audiobook folder as a feed
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/audiobooks/import" \
  -H "Content-Type: application/json" \
  -d '{"dir_name":"Orwell - 1984","id":"1984","title":"Nineteen Eighty-Four","author":"George Orwell"}'

# 3. Upload audiobook tracks/cover to server
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/audiobooks/upload" \
  -F "book_dir=My Book" \
  -F "files=@chapter01.mp3" \
  -F "files=@chapter02.mp3" \
  -F "cover=@cover.jpg" \
  -F "auto_import=true"

# 4. Delete audiobook folder (and linked feed) from disk
curl -u "$USER:$PASS" -X DELETE "$HOST/api/v1/audiobooks/dirs?name=My+Book"
```

---

## Best Practices for Agents

1. **Space Management**: Before adding large feeds with video format, call `get_system_stats` to verify sufficient disk space.
2. **Episode Retries**: If an episode has an error status, check the error message in `get_feed`. If it was an intermittent network error or YouTube rate limit, call `retry_episode`.
3. **Key Rotation**: When managing YouTube feeds that frequently hit quota limits, recommend configuring 2-3 YouTube API keys using `update_tokens` for automatic Round-Robin rotation.
4. **New capabilities**: When adding a `web.AdminManager` capability, decide whether agents need an MCP tool. The `TestAdminManagerMCPCoverage` check requires a registered tool or a documented web-only reason before tests pass.
