# Personal Feeds

**English** | [Русский](./ru/personal-feeds.md)

Podsync includes a built-in **Personal Feeds** engine that allows users to create custom, universal podcast feeds and manually curate individual episodes from web platforms, direct media URLs, local server files, and file uploads.

---

## 1. Overview & Key Capabilities

- **Universal Feeds (Audio + Video)**: Personal feeds are not restricted to audio-only or video-only. A single feed can seamlessly host both audio tracks (MP3, OGG, OPUS, WAV, FLAC, AAC) and video tracks (MP4, MKV, WebM, MOV). Each episode dynamically generates the correct `<enclosure>` MIME type (`audio/mpeg`, `video/mp4`, etc.) in the RSS XML feed.
- **Unified Library & Dedicated Tab**:
  - Direct access via the **Personal feeds** tab in the sidebar.
  - Personal feeds are also displayed on the main **Podcasts** tab with a **Personal** provider badge and a dedicated `Personal` option in the **All types** filter dropdown.
- **Multiple Ingestion Methods**:
  1. **Video & Audio Platforms**: Add episodes from YouTube, VK Video, Rutube, Vimeo, SoundCloud URLs.
  2. **Direct Media Links**: Download and transcode direct HTTP/HTTPS links (e.g. `https://example.com/audio.mp3`, `podcast.ogg`, `video.mp4`).
  3. **Local Server Files (`file_path`)**: Import media files directly from the server's local filesystem via REST API or MCP.
  4. **Direct File Upload**: Upload local audio/video files up to 4 GiB via the web console or multipart API.
- **Automatic Transcoding & Tag Probing**:
  - Audio files are converted to compatible MP3 with duration and metadata extraction via `ffmpeg`.
  - Video files are packaged into MP4 (H.264 / AAC) with thumbnail generation.
- **Zero-Copy Episode Reuse**:
  - Episodes can be **copied** between personal feeds without duplicating multi-gigabyte media files on disk. The copy receives a unique episode GUID while referencing the shared media record.
  - Episodes can be **moved** to another feed, preserving their original GUID and publication timestamp.
- **Automatic Seeding & Safe Initialization**:
  - The first feed is seeded as `personal` (**My Feed**) with RSS at `/personal.xml`.
  - Subsequent feeds receive immutable random slugs (`personal-<32 hex characters>`).
  - Deleting the default feed preserves the initialization flag so it will not regenerate unexpectedly on restart.

---

## 2. Managing Personal Feeds in Web UI

### Creating a Feed
1. Navigate to **LIBRARY → Personal feeds** (or click **Add podcast**).
2. Click **Create feed**.
3. Fill in the **Title**, optional **Description**, **Cover image URL** (or upload an image file), and **Episode order** (`Newest first` or `Oldest first`).
4. Click **Create feed**. The RSS feed is immediately generated and ready for subscription.

### Adding Episodes
Open any personal feed to access the episode management toolbar:
1. **From Link or Server Path**: Paste a video/audio URL (YouTube, VK, Rutube, direct MP3/MP4 link) or a local server file path, set an optional custom title and format override, and click **Add episode**.
2. **From Local File Upload**: Select an audio or video file from your computer (up to 4 GiB), specify an optional title, and click **Upload & add**. Upload progress is tracked in real-time.

### Episode Actions
Each episode card provides:
- **Audio / Video Player**: Integrated HTML5 player for immediate in-browser playback.
- **Move**: Transfers the episode to another personal feed while retaining its GUID.
- **Copy**: Clones the episode to another personal feed with a new GUID, sharing the same physical media file.
- **Retry**: Resubmits a failed download/transcoding job to the queue.
- **Delete**: Deletes the episode from the feed with optional disk file cleanup.

### Subscribing to Personal Feeds
Clicking **Subscribe** opens the unified subscription modal:
- Direct RSS link with one-click **Copy**.
- On-screen **QR Code** for instant smartphone camera scanning.
- One-tap deep-links for **Apple Podcasts**, **Pocket Casts**, and **Overcast**.

---

## 3. REST API Reference (`/api/v1/personal-feeds`)

All endpoints require administrator authentication (Basic Auth or session cookie).

| Method | Endpoint | Request Body / Parameters | Description |
|---|---|---|---|
| `GET` | `/api/v1/personal-feeds` | None | Returns array of all personal feeds. |
| `POST` | `/api/v1/personal-feeds` | `{"title": "...", "description": "...", "cover_art": "...", "playlist_sort": "desc", "private_feed": false}` | Creates a new personal feed (returns 201 Created). |
| `GET` | `/api/v1/personal-feeds/{id}` | None | Returns feed metadata and episodes list. |
| `PATCH` | `/api/v1/personal-feeds/{id}` | `{"title": "...", "description": "...", "cover_art": "...", "playlist_sort": "asc", "private_feed": true}` | Partially updates feed settings. |
| `DELETE` | `/api/v1/personal-feeds/{id}?confirm=true&delete_files=true` | Query params | Deletes feed and unmounts RSS. |
| `GET` | `/api/v1/personal-feeds/{id}/episodes` | None | Returns list of episode objects. |
| `POST` | `/api/v1/personal-feeds/{id}/episodes` | `{"url": "...", "file_path": "...", "title": "...", "format": "audio"}` | Adds episode from URL or local file path. |
| `POST` | `/api/v1/personal-feeds/{id}/upload` | Multipart form (`file`, `title`, `format`) | Uploads media file (up to 4 GiB). |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/copy` | `{"target_feed_id": "personal-..."}` | Copies episode to another feed (shared file). |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/move` | `{"target_feed_id": "personal-..."}` | Moves episode to another feed (retains GUID). |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/retry` | None | Resets failed episode to retry download. |
| `DELETE` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}?confirm=true&delete_files=true` | Query params | Deletes episode from personal feed. |

---

## 4. Model Context Protocol (MCP) Tools

AI assistants (**Claude Desktop**, **OpenCode**, **Cursor**, **Cline**, **Antigravity**) can manage personal feeds via the following MCP tools:

| Tool Name | Arguments | Description |
|---|---|---|
| **`list_personal_feeds`** | `{}` | List all curated personal feeds, episode counts, total sizes, and feed URLs. |
| **`get_personal_feed`** | `feed_id` *(req)* | Get metadata and all episodes (with status, source URL/file, and media URL). |
| **`create_personal_feed`** | `title` *(req)*, `description`, `cover_art`, `playlist_sort`, `private_feed` | Create a new curated personal podcast feed. |
| **`add_personal_episode`** | `feed_id` *(req)*, `url` *(opt)*, `file_path` *(opt)*, `title` *(opt)*, `format` *(opt)* | Add an episode from a URL or local server file path. |
| **`delete_personal_episode`** | `feed_id` *(req)*, `episode_id` *(req)* | Delete an episode from a personal feed and clean up storage. |
| **`rename_personal_episode`** | `feed_id` *(req)*, `episode_id` *(req)*, `title` *(req)* | Rename an episode in a personal podcast feed. |

---

## 5. Storage Architecture

Personal feeds store metadata and media in an isolated namespace:
- **BadgerDB Namespace**: `podsync/personal/v1/`
  - `feed/{id}`: Feed metadata and episode count.
  - `episode/{feed_id}/{episode_id}`: Membership record linking a feed episode to a media object.
  - `media/{media_id}`: Source metadata, transcode status, file path, and reference counter.
  - `initialized`: Persistent initialization flag.
- **Filesystem Storage**:
  - Media files: `<data_dir>/_personal/{media_id}/file.<ext>`
  - Generated feeds: `<data_dir>/{feed_id}.xml`
