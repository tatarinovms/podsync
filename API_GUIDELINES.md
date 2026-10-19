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
