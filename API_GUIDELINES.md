# REST API Guidelines & Specification

This document defines the REST API standards, endpoint contracts, authentication mechanisms, and request/response payloads for **Podsync** (`/api/v1`).

---

## 1. Protocol & Authentication

### 1.1 Base URL
All API endpoints are mounted under:
```
http://<host>:<port>/api/v1
```

### 1.2 Authentication
Endpoints are protected when `[server.admin]` is enabled in `config.toml`. Podsync supports two authentication mechanisms:
1. **HTTP Basic Authentication**:
   Include standard `Authorization: Basic <base64>` header with credentials defined in `[server.admin]`.
2. **Session Cookie**:
   Browser clients authenticate via `POST /api/v1/auth/login` and receive a session cookie (`podsync_session`).

Unauthenticated requests to protected endpoints return `401 Unauthorized`.

---

## 2. Response & Error Conventions

### 2.1 Success Responses
All successful endpoints return `Content-Type: application/json` with appropriate HTTP status codes:
- `200 OK`: Request succeeded, returning requested resource or state.
- `201 Created`: New resource successfully created (e.g. feed added, file uploaded).

### 2.2 Error Responses
Errors return standard JSON objects with an `error` message and appropriate HTTP status codes (`400`, `401`, `404`, `405`, `500`):
```json
{
  "error": "directory name is required"
}
```

---

## 3. Endpoints Reference

### 3.1 Feeds Management

#### `GET /api/v1/feeds`
Returns a list of all active feeds.
- **Response**: Array of `FeedSummary` objects:
```json
[
  {
    "id": "mybook",
    "url": "audiobook://Dune",
    "title": "Dune",
    "author": "Frank Herbert",
    "provider": "audiobook",
    "format": "audio",
    "quality": "high",
    "page_size": 1000,
    "cron_schedule": "@every 12h",
    "episode_count": 22,
    "downloaded_count": 22,
    "total_size": 429496729,
    "feed_url": "http://localhost:8080/mybook.xml",
    "private_feed": true
  }
]
```

#### `POST /api/v1/feeds`
Creates a new podcast feed.
- **Request Body**:
```json
{
  "url": "https://vkvideo.ru/@channel",
  "format": "audio",
  "quality": "high",
  "page_size": 50,
  "cron_schedule": "@every 12h",
  "language": "ru",
  "private_feed": false
}
```

#### `GET /api/v1/feeds/{id}`
Returns details for a specific feed and its episodes list.

#### `PUT /api/v1/feeds/{id}`
Updates feed settings and triggers an immediate XML rebuild.
- **Request Body**: `FeedConfigUpdate` object:
```json
{
  "format": "audio",
  "quality": "high",
  "page_size": 50,
  "cron_schedule": "@every 6h",
  "opml": true,
  "private_feed": true,
  "keep_last": 10,
  "language": "ru",
  "custom": {
    "title": "Custom Show Title",
    "author": "Host Name",
    "description": "Show notes",
    "cover_art": "https://example.com/cover.jpg"
  }
}
```

#### `DELETE /api/v1/feeds/{id}?delete_files={bool}`
Deletes the feed from memory and `config.toml`. Set `delete_files=true` to delete media files from disk.

#### `POST /api/v1/feeds/{id}/update`
Triggers an immediate update for a single feed.

#### `POST /api/v1/feeds/update-all`
Queues updates for all configured feeds.

---

### 3.2 Audiobooks Management

#### `GET /api/v1/audiobooks/dirs`
Discovers and lists folders in the audiobooks directory (`[audiobooks].dir`).
- **Query Parameters**: `?unlinked=true` (filters only folders not linked to feeds).
- **Response**:
```json
[
  {
    "name": "Frank Herbert - Dune",
    "file_count": 22,
    "total_size": 429496729,
    "has_cover": true,
    "linked": true,
    "feed_id": "dune",
    "suggested_title": "Dune",
    "suggested_author": "Frank Herbert"
  }
]
```

#### `DELETE /api/v1/audiobooks/dirs?name={dir_name}`
Deletes an audiobook directory and all its audio files from disk. If the directory is linked to an active podcast feed, the feed is also cleaned up.
- **Query Parameter**: `name` (required, relative directory name).
- **Response**: `{"success": true, "deleted": "Frank Herbert - Dune"}`.

#### `POST /api/v1/audiobooks/import`
Imports a local directory as a serial podcast feed.
- **Request Body**:
```json
{
  "dir_name": "Frank Herbert - Dune",
  "id": "dune",
  "title": "Dune",
  "author": "Frank Herbert",
  "description": "Sci-Fi Classic",
  "cron_schedule": "@every 12h",
  "private_feed": true
}
```

#### `POST /api/v1/audiobooks/upload`
Multipart form upload for tracks and covers.
- **Form Fields**:
  - `book_dir` *(string)*: Target book folder name.
  - `files` *(files)*: Audio files (`.mp3`, `.m4a`, etc.).
  - `cover` *(file, optional)*: Cover image.
  - `auto_import` *(bool, optional)*: Automatically create feed upon upload completion.

---

### 3.3 Downloader & Cookies

#### `GET /api/v1/downloader`
Inspects yt-dlp binary version, path, ffmpeg version, cookies file status, and proxy setting.

#### `POST /api/v1/downloader`
Updates downloader settings (`timeout`, `proxy`, `language`, `cookies_from_browser`).

#### `GET /api/v1/downloader/cookies`
Reads current Netscape `cookies.txt` content.

#### `POST /api/v1/downloader/cookies`
Updates `cookies.txt` content.

#### `DELETE /api/v1/downloader/cookies`
Clears/deletes `cookies.txt`.

#### `POST /api/v1/downloader/test`
Runs non-downloading test of yt-dlp against a URL:
```json
{
  "url": "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
}
```

---

### 3.4 API Tokens & Rotation

#### `GET /api/v1/tokens`
Lists masked API tokens and environment variable sources.

#### `POST /api/v1/tokens`
Updates or appends tokens for a provider:
```json
{
  "provider": "youtube",
  "tokens": ["key1", "key2"],
  "append": false
}
```

#### `DELETE /api/v1/tokens?provider={provider}&index={index}`
Deletes a specific token by index, or clears all tokens for the provider.

---

### 3.5 System & Diagnostic Logs

#### `GET /api/v1/system`
Returns runtime stats: memory, uptime, disk total/used/free, goroutines, yt-dlp and ffmpeg status.

#### `GET /api/v1/system/logs?limit={n}&level={level}&search={q}&feed_id={id}`
Queries in-memory circular log buffer.

#### `GET /api/v1/system/logs/download`
Downloads log content as attachment (`podsync.log`).

#### `DELETE /api/v1/system/logs`
Clears in-memory log buffer.

### 3.6 Authentication & Security

#### `GET /api/v1/auth/status`
Returns current session authentication status, administrator username, and active brute-force rate limit configuration.
- **Access**: Public / Unauthenticated.
- **Response**:
```json
{
  "authenticated": true,
  "enabled": true,
  "username": "admin",
  "rate_limit": {
    "max_attempts": 5,
    "window": "5m",
    "lockout": "5m"
  }
}
```

#### `POST /api/v1/auth/login`
Authenticates user with username and password. On success, sets HTTP-only `podsync_session` cookie. Subject to brute-force rate limiting; returns `429 Too Many Requests` with a `Retry-After: <seconds>` header if limits are exceeded.
- **Access**: Public.
- **Request Body**:
```json
{
  "username": "admin",
  "password": "secretpassword"
}
```

#### `POST /api/v1/auth/logout`
Invalidates current session and clears `podsync_session` cookie.
- **Access**: Public.

#### `POST /api/v1/auth/password`
Changes administrator password. Requires authentication and verification of current password.
- **Access**: Authenticated.
- **Request Body**:
```json
{
  "old_password": "currentpassword",
  "new_password": "newsecretpassword"
}
```

#### `GET /api/v1/auth/security`
Returns current brute-force protection and rate limiting parameters.
- **Access**: Authenticated.
- **Response**:
```json
{
  "max_attempts": 5,
  "window": "5m",
  "lockout": "5m"
}
```

#### `POST /api/v1/auth/security` (or `PUT`)
Updates brute-force rate limiter parameters on the fly in memory and persists them to `config.toml` under `[server.admin]`.
- **Access**: Authenticated.
- **Request Body**:
```json
{
  "max_attempts": 5,
  "window": "5m",
  "lockout": "15m"
}
```
- **Validation**:
  - `max_attempts`: integer from 1 to 100.
  - `window`: duration string (e.g. `1m`, `5m`, `15m`, `30m`, `1h`), minimum 10s.
  - `lockout`: duration string (e.g. `1m`, `5m`, `15m`, `30m`, `1h`, `24h`), minimum 10s.
- **Response**:
```json
{
  "success": true,
  "rate_limit": {
    "max_attempts": 5,
    "window": "5m",
    "lockout": "15m"
  }
}
```

### 3.7 Personal Feeds Management

Personal feeds allow users to curate custom hybrid podcast feeds containing both audio and video episodes. Media files are stored in a dedicated content-addressed directory with reference counting, allowing episodes to be shared across multiple feeds without duplicating large files.

#### `GET /api/v1/personal-feeds`
Returns a list of all personal feeds.
- **Response**: Array of `personal.Feed` objects:
```json
[
  {
    "id": "personal",
    "type": "personal",
    "title": "My Feed",
    "description": "My curated audio and video podcast",
    "cover_art": "https://example.com/cover.jpg",
    "playlist_sort": "desc",
    "private_feed": false,
    "feed_url": "http://localhost:8080/personal.xml",
    "episode_count": 8,
    "created_at": "2026-10-22T10:00:00Z",
    "updated_at": "2026-10-22T12:00:00Z"
  }
]
```

#### `POST /api/v1/personal-feeds`
Creates a new personal podcast feed.
- **Request Body**:
```json
{
  "title": "Tech & Talks",
  "description": "Curated collection of conference talks and podcasts",
  "cover_art": "https://example.com/cover.jpg",
  "playlist_sort": "desc",
  "private_feed": false
}
```
- **Validation**:
  - `title` *(required, non-empty)*
  - `playlist_sort` *(optional)*: `"desc"` (newest first, default) or `"asc"` (oldest first).
  - `private_feed` *(optional, bool)*: emits `<itunes:block>yes</itunes:block>` when true.
- **Response**: `201 Created` returning the new `personal.Feed` object with an auto-generated unique ID (`personal-<32 hex characters>`).

#### `GET /api/v1/personal-feeds/{id}`
Returns details for a personal feed and its list of episodes.
- **Response**:
```json
{
  "feed": {
    "id": "personal",
    "title": "My Feed",
    "feed_url": "http://localhost:8080/personal.xml",
    "episode_count": 1
  },
  "episodes": [
    {
      "id": "ep-uuid-1",
      "title": "Podcast Episode 1",
      "status": "downloaded",
      "media_type": "audio/mpeg",
      "media_url": "http://localhost:8080/_personal/media-hash/file.mp3",
      "duration": 3600,
      "size": 52428800,
      "pub_date": "2026-10-22T12:00:00Z"
    }
  ]
}
```

#### `PATCH /api/v1/personal-feeds/{id}`
Updates metadata for an existing personal feed. Omitted fields are preserved.
- **Request Body**:
```json
{
  "title": "Updated Title",
  "description": "New description",
  "cover_art": "https://example.com/new-cover.jpg",
  "playlist_sort": "asc",
  "private_feed": true
}
```

#### `DELETE /api/v1/personal-feeds/{id}?confirm=true&delete_files={bool}`
Deletes the personal feed and unmounts its RSS XML.
- **Query Parameters**:
  - `confirm`: required (`true`).
  - `delete_files` *(optional, default: false)*: when `true`, media files associated exclusively with this feed are removed from disk. Shared files referenced by other feeds are preserved.

#### `GET /api/v1/personal-feeds/{id}/episodes`
Lists all episode objects for a personal feed.

#### `POST /api/v1/personal-feeds/{id}/episodes`
Queues a new episode for downloading and ingestion into the personal feed from a URL or local server file path.
- **Request Body**:
```json
{
  "url": "https://www.youtube.com/watch?v=... or https://example.com/file.mp3",
  "file_path": "/var/media/audio.mp3",
  "title": "Custom Title Override",
  "format": "audio",
  "quality": "high",
  "max_height": 720
}
```
- **Fields**:
  - `url` *(string, optional)*: Web URL (YouTube, VK Video, Rutube, or direct HTTP/HTTPS link to MP3, OGG, MP4, etc.).
  - `file_path` *(string, optional)*: Absolute path to a media file located directly on the server filesystem.
  - `title` *(string, optional)*: Custom episode title override.
  - `format` *(string, optional)*: Target media format (`"audio"` or `"video"`). Auto-detected if omitted.
  - `quality` *(string, optional)*: Media quality preference (`"high"` or `"low"`). Defaults to `"high"`.
  - `max_height` *(integer, optional)*: Maximum video height (e.g. `480`, `720`, `1080`, `1440`, `2160`, or `0` for original). Defaults to `0` (unlimited).
- **Response**: `202 Accepted` with episode status object.

#### `POST /api/v1/personal-feeds/{id}/upload`
Directly uploads a media file (up to 4 GiB) via multipart form data.
- **Form Fields**:
  - `file` *(file, required)*: Audio or video file (`.mp3`, `.ogg`, `.opus`, `.m4a`, `.wav`, `.flac`, `.mp4`, `.mkv`, `.webm`, `.mov`).
  - `title` *(string, optional)*: Custom episode title.
  - `format` *(string, optional)*: Preferred media format (`"audio"` or `"video"`).
- **Processing**:
  - Audio files are converted to compatible MP3 with duration and metadata extraction via `ffmpeg`.
  - Video files are remuxed to MP4 (H.264 / AAC) with duration and thumbnail extraction.
- **Response**: `202 Accepted`.

#### `POST /api/v1/personal-feeds/{id}/episodes/{episode_id}/copy`
Copies an episode into another personal feed without duplicating the underlying media file on disk.
- **Request Body**:
```json
{
  "target_feed_id": "personal-target-id"
}
```
- **Behavior**: Generates a new unique episode membership and GUID in the target feed, incrementing the underlying media object reference count.

#### `POST /api/v1/personal-feeds/{id}/episodes/{episode_id}/move`
Moves an episode from one personal feed to another.
- **Request Body**:
```json
{
  "target_feed_id": "personal-target-id"
}
```
- **Behavior**: Reassigns the episode membership while preserving the episode GUID and publication date.

#### `POST /api/v1/personal-feeds/{id}/episodes/{episode_id}/retry`
Resets a failed episode in the queue to retry downloading.
- **Response**: `202 Accepted`.

#### `PATCH /api/v1/personal-feeds/{id}/episodes/{episode_id}`
Renames an episode in the personal feed. Also accepts `POST /api/v1/personal-feeds/{id}/episodes/{episode_id}/rename` and `PUT`.
- **Request Body**:
```json
{
  "title": "New Episode Title"
}
```
- **Response**: `200 OK` with the updated `Episode` object. Rebuilds the feed's RSS XML immediately.

#### `DELETE /api/v1/personal-feeds/{id}/episodes/{episode_id}?confirm=true&delete_files={bool}`
Removes an episode from the personal feed.
- **Query Parameters**:
  - `confirm`: required (`true`).
  - `delete_files` *(optional, default: false)*: when `true`, decrements the media reference counter and deletes the file from disk if no other personal feeds reference it.
