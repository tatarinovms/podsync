# 📚 Audiobooks in Podsync

**English** | [Русский](./ru/audiobooks.md)


Podsync includes first-class support for local audiobooks, transforming folders of audio tracks on disk into serial podcast feeds fully compliant with Apple Podcasts, Pocket Casts, Overcast, and Podcast 2.0 standards.

---

## Key Features

- **Podcast 2.0 Serial Standard**: Renders `<itunes:type>serial</itunes:type>` and numbered `<itunes:episode>N</itunes:episode>` tags so podcast players present chapters in the correct chronological reading order (from Chapter 1 to Chapter N).
- **Natural Chapter Sorting**: Automatically sorts track filenames naturally (`01`, `02` ... `10`, `11`) instead of naive alphabetical order (`1`, `10`, `2`).
- **Embedded Cover Art Extraction**: Automatically extracts album cover art from ID3/APIC tags inside audio files, or falls back to `cover.jpg`, `cover.png`, or `folder.jpg` inside the book directory.
- **Multiple Audio Formats**: Supports `.mp3`, `.m4a`, `.m4b`, `.aac`, `.flac`, and `.ogg`.
- **Dynamic Rescanning**: Adding new chapter files to an existing folder automatically updates episode count and metadata on the next scan without re-uploading the entire book.
- **Zero Cron Overhead**: Audiobook feeds are static collections on disk and do not waste CPU cycles on scheduled YouTube-style polling loops; they refresh on import, settings changes, or on-demand update.
- **Web UI & 1-Click Import**: Manage, upload, import, and delete books directly from the web console.

---

## Configuration

Set the root audiobooks directory in `config.toml` (or via `PODSYNC_AUDIOBOOKS_DIR` environment variable):

```toml
[audiobooks]
  dir = "/mnt/dietpi_userdata/podsync/audiobooks"
```

Example directory layout on the server:

```text
/mnt/dietpi_userdata/podsync/audiobooks/
├── Solaris/
│   ├── cover.jpg
│   ├── 01 Chapter 1.mp3
│   ├── 02 Chapter 2.mp3
│   └── 03 Chapter 3.mp3
└── Dune/
    ├── 01 Part One.m4b
    └── 02 Part Two.m4b
```

---

## Importing an Audiobook

### 1. Via Web Admin Console

1. Navigate to the **Audiobooks** tab in `/admin/`.
2. Any unlinked folder in your audiobooks directory will appear under **Unlinked Directories**.
3. Click **Import** next to the folder:
   - Provide a unique **Feed ID** (slug);
   - Customize Title, Author/Narrator, and Description;
   - Click **Import as Podcast Feed**.
4. The feed URL will be generated immediately (e.g., `https://rssreader.example.com/solaris.xml`).

### 2. Via Web UI File Upload

You can upload audio tracks directly from your browser:
- Drag and drop audio files (e.g. `.mp3`, `.m4b`, `.m4a`) and optional cover image (`cover.jpg`/`cover.png`) directly into the upload area on the **Audiobooks** tab (upload raw audio files; ZIP archives are not automatically extracted by the server);
- Set the target book folder name;
- Check **"Auto-import as Podcast feed"** to automatically create the feed upon upload completion;
- **Upload Limit**: Supports uploads up to **1 GiB** per batch with streaming multipart processing.

### 3. Via REST API

```bash
# 1. Discover available directories on disk
curl -u "admin:password" http://localhost:8080/api/v1/audiobooks/dirs

# 2. Import folder as feed
curl -u "admin:password" -X POST http://localhost:8080/api/v1/audiobooks/import \
  -H "Content-Type: application/json" \
  -d '{
    "dir_name": "Solaris",
    "id": "solaris",
    "title": "Solaris",
    "author": "Stanislaw Lem",
    "description": "Classic sci-fi audiobook"
  }'

# 3. Upload tracks to server
curl -u "admin:password" -X POST http://localhost:8080/api/v1/audiobooks/upload \
  -F "book_dir=Solaris" \
  -F "files=@chapter01.mp3" \
  -F "cover=@cover.jpg" \
  -F "auto_import=true"

# 4. Delete folder and remove feed from disk
curl -u "admin:password" -X DELETE "http://localhost:8080/api/v1/audiobooks/dirs?name=Solaris"
```

### 4. Via MCP (Model Context Protocol)

AI assistants can manage audiobooks using dedicated MCP tools:
- `list_audiobook_dirs`: Report directories, track count, total size, and linked feed status.
- `import_audiobook`: `{"dir_name": "Solaris", "id": "solaris", "title": "Solaris", "author": "Stanislaw Lem"}`.
- `delete_audiobook_dir`: `{"dir_name": "Solaris"}` (unmounts feed, removes from config & DB, and cleans files from disk).

---

## Deleting an Audiobook

Deleting an audiobook folder via the web console or API (`DELETE /api/v1/audiobooks/dirs?name=BookName`):
1. Confirms the deletion with the user;
2. Unmounts the virtual feed route from the HTTP file server;
3. Removes the feed definition from `config.toml` and BadgerDB;
4. Safely deletes the book folder and all audio files from disk.
