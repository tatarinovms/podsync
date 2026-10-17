---
name: podsync
description: "Manage Podsync podcast generator: subscribe to YouTube/VK Video/Vimeo channels and playlists as podcasts, trigger episode updates, retry downloads, check disk space/health, and rotate API keys."
version: 1.0.0
triggers:
  - podsync
  - podcast
  - youtube podcast
  - vk video podcast
  - retry episode
  - feed update
---

# Podsync Skill

This skill provides comprehensive instructions for AI agents on how to monitor, diagnose, and manage [Podsync](https://github.com/mxpv/podsync) instances using **MCP (Model Context Protocol)** or the **REST API**.

---

## MCP Tools Reference

When connected to Podsync's MCP server (`podsync --mcp` or `/mcp/sse`), use the following tools:

### 1. `list_feeds`
- **Purpose**: Lists all active podcast feeds, episode count, downloaded count, error count, disk space used, and direct RSS URLs.
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
  - `url` *(string, required)*: Source channel or playlist URL (e.g. YouTube, VK Video, Vimeo, SoundCloud).
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
- **Purpose**: Updates configuration settings for an existing podcast feed (cron schedule, format, quality, page_size, retention keep_last, max_height, playlist_sort, private_feed, language) and saves changes to `config.toml`.
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

### 8. `get_system_stats`
- **Purpose**: Returns memory usage, disk space (total, used, free on `data_dir`), uptime, and whether `ffmpeg` and `yt-dlp` are installed and functioning.
- **Arguments**: None `{}`.
- **When to use**: Health checks, checking remaining disk capacity, or troubleshooting download failures.

### 9. `get_tokens`
- **Purpose**: Returns masked API keys for all providers (`youtube`, `vkvideo`, `vimeo`, `soundcloud`, `twitch`) and indicates if they are loaded from ENV or `config.toml`.
- **Arguments**: None `{}`.

### 10. `update_tokens`
- **Purpose**: Updates API keys for a provider in `config.toml`. Accepts multiple keys to enable automatic Round-Robin quota rotation (especially useful for YouTube API).
- **Arguments**:
  - `provider` *(string, required)*: `"youtube"`, `"vkvideo"`, `"vimeo"`, `"soundcloud"`, or `"twitch"`.
  - `tokens` *(array of strings, required)*: List of API keys.

### 11. `delete_tokens`
- **Purpose**: Deletes a specific API token by index, or clears all configured API tokens for a provider from `config.toml`.
- **Arguments**:
  - `provider` *(string, required)*: `"youtube"`, `"vkvideo"`, `"vimeo"`, `"soundcloud"`, or `"twitch"`.
  - `index` *(integer, optional)*: 0-based index of a specific token to delete. If omitted or negative, all tokens for the provider are removed.
- **When to use**: Removing invalid, revoked, or unnecessary API keys (either a single key from rotation or the entire provider).

### 12. `get_downloader_config`
- **Purpose**: Retrieves yt-dlp binary path, version, current timeout, proxy, and status of installed cookies file (`cookies.txt` line count, file size, last modified).
- **Arguments**: None `{}`.
- **When to use**: Diagnosing YouTube download failures such as bot verification blocks or rate limits.

### 13. `update_downloader_config`
- **Purpose**: Updates yt-dlp downloader configuration (download timeout, proxy URL, preferred audio language, or browser cookie extraction).
- **Arguments**:
  - `timeout` *(integer, optional)*: Timeout in minutes.
  - `proxy` *(string, optional)*: Proxy URL (e.g. `socks5://127.0.0.1:1080` or `http://proxy:8080`).
  - `cookies_from_browser` *(string, optional)*: Browser name to extract session cookies from (e.g. `chrome`, `firefox`).
  - `language` *(string, optional)*: Global preferred audio track language code (e.g. `"ru"`, `"en"`) for multi-audio videos and YouTube dubbing.

### 14. `test_downloader`
- **Purpose**: Tests yt-dlp connectivity and cookie verification against a video URL without downloading the file.
- **Arguments**:
  - `url` *(string, optional)*: Video URL to test (defaults to YouTube sample video).

---


### 15. `get_download_errors`
- **Purpose**: Returns a diagnostic list of all failed podcast episodes across all feeds or for a specific feed, with exact error causes (e.g. YouTube bot verification, private video, 429 rate limits, ffmpeg errors) and actionable recommendations.
- **Arguments**:
  - `feed_id` *(string, optional)*: Filter errors by feed ID.
  - `limit` *(integer, optional, default: 20, max: 100)*: Maximum error records to return.
- **When to use**: Answering *"Are there any download errors?"*, *"Why did download fail on channel X?"*, or before proposing retries.

### 16. `get_recent_downloads`
- **Purpose**: Returns the most recently downloaded podcast episodes across all channels or for a single feed, complete with file sizes, durations, publication dates, and playable audio/video URLs.
- **Arguments**:
  - `feed_id` *(string, optional)*: Filter recent downloads by feed ID.
  - `limit` *(integer, optional, default: 10, max: 100)*: Number of episodes to return.
- **When to use**: Answering *"What was downloaded recently?"*, *"Show me latest episodes from channel X"*, or checking if a new episode is already available.

### 17. `get_sync_status`
- **Purpose**: Returns a high-level operational status overview: active feeds, last sync times, next cron execution for each feed, number of episodes downloaded in the past 24 hours, failed episode counts, and disk space free.
- **Arguments**: None `{}`.
- **When to use**: General status checks (*"Is Podsync working?"*, *"When is the next sync?"*).

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

# List feeds
curl -u "$USER:$PASS" "$HOST/api/v1/feeds"

# Add feed
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/feeds" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://youtube.com/channel/...","format":"audio","quality":"high"}'

# Trigger update
curl -u "$USER:$PASS" -X POST "$HOST/api/v1/feeds/FEED_ID/update"

# System health
curl -u "$USER:$PASS" "$HOST/api/v1/system"
```

---

## Best Practices for Agents

1. **Space Management**: Before adding large feeds with video format, call `get_system_stats` to verify sufficient disk space.
2. **Episode Retries**: If an episode has an error status, check the error message in `get_feed`. If it was an intermittent network error or YouTube rate limit, call `retry_episode`.
3. **Key Rotation**: When managing YouTube feeds that frequently hit quota limits, recommend configuring 2-3 YouTube API keys using `update_tokens` for automatic Round-Robin rotation.
