# Agent & Developer Self-Review Checklist

See the [developer documentation index](./README.md) for related guides.

Before committing changes, opening pull requests, or creating release tags, run through this comprehensive checklist to guarantee system stability, security, and UI consistency.

---

## 1. Code Quality & Concurrency

- [ ] **Race Detector**: Ran `go test -v -race ./...` without any data race warnings or deadlocks.
- [ ] **Mutex Discipline**:
  - All shared map accesses (`m.feeds`, `m.cronEntries`, `m.tokens`, `m.feedErrors`) in `AppFeedManager` are properly protected by `m.mu.RLock()` or `m.mu.Lock()`.
  - `defer m.mu.Unlock()` or `defer m.mu.RUnlock()` is called immediately after acquiring the lock.
  - No lock is held across long-running network I/O or disk operations.
- [ ] **Channel Buffering**: No goroutine pushes indefinitely to unbuffered or full channels; non-blocking selects (`select { case ch <- val: ... default: ... }`) are used where appropriate.
- [ ] **Persistent Job Lifecycle**: Every claimed queue job ends in `MarkDone`, `MarkFailed`, or `YieldJob` with the current claim token. `personal://<feed_id>` jobs are not rejected merely because they are absent from the regular feed map.
- [ ] **Error Propagation**: Errors are wrapped with context (`errors.Wrapf`, `fmt.Errorf("%w", ...)`) and not silently ignored.

---

## 2. Persistence & Config Integrity

- [ ] **Dual State Sync**:
  - Any parameter updated via Web UI or MCP (`private_feed`, `quality`, `format`, `page_size`, `cron_schedule`, `custom`, `filters`) is updated **both** in memory (`m.feeds[id]`), in BadgerDB (`model.Feed`), and saved to disk via `m.saveFeedToConfig(f)` in `config.toml`.
- [ ] **Instant XML Rebuild**:
  - When feed settings change, `m.updates <- f` is queued so the RSS XML file `{feed_id}.xml` is regenerated immediately.
- [ ] **BadgerDB Safety**:
  - No unclosed BadgerDB iterators or transactions (`defer it.Close()`, `defer txn.Discard()`).
  - Keys use the correct namespaces (`feed/`, `episode/`, `stats/`, `job/`, and `podsync/personal/v1/`); preserve existing state keys.

---

## 3. Security & Filesystem Operations

- [ ] **Path Traversal Protection**:
  - Any directory or file path parameter received from HTTP requests or MCP arguments (`dir_name`, `book_dir`, `filename`) is cleaned with `filepath.Clean()`.
  - Path traversal sequences (`..`, leading `/`, relative components) are strictly checked and rejected.
  - `filepath.Rel(baseDir, targetPath)` is verified to ensure the path resides strictly within the allowed directory.
- [ ] **Safe Deletion**:
  - Deletion routines (`DeleteFeed`, `DeleteAudiobookDir`) never delete parent directories or root filesystem paths.
  - Linked feeds are safely unmounted and deregistered before deleting underlying directories.
- [ ] **Backup Safety**:
  - Export uses a transactionally consistent Badger stream and excludes downloaded media.
  - Inspect/restore verifies manifest schema, checksums, archive paths, upload/extraction limits, and restores through temporary files.
  - Cross-server restore preserves target storage paths and port by default; secret restore is explicit and covered by tests.

---

## 4. Podcast & RSS Standards

- [ ] **Private Feeds**:
  - Checked `feed.PrivateFeed || cfg.PrivateFeed`. When true, `<itunes:block>yes</itunes:block>` is emitted in `{feed_id}.xml`.
- [ ] **Audiobook Serial Format**:
  - Contains `<itunes:type>serial</itunes:type>` and `<itunes:episode>N</itunes:episode>`.
  - Chapters are sorted naturally ascending (Chapter 1, Chapter 2 ... Chapter 10).
- [ ] **Valid Enclosures**:
  - Media URLs are properly escaped with `url.PathEscape()`.
  - Correct MIME types are assigned (`audio/mpeg`, `audio/mp4`, `video/mp4`).

---

## 5. Web UI & Brand Compliance

- [ ] **Catppuccin Palette**:
  - No raw Bootstrap colors (no default blue `#0d6efd`, no slate-green `#2b3035`).
  - Primary buttons use `.btn-primary` or `.btn-brand` (Mauve `#cba6f7`).
  - Tested in both dark (Mocha) and light (Latte) themes.
- [ ] **Modal Closing**:
  - No direct calls to `bootstrap.Modal.getInstance(el).hide()`.
  - Uses `closeModal(modalId)` to cleanly remove backdrops and restore scrollability.
- [ ] **File Sync**:
  - Kept Web UI assets in `services/web/ui/` and `html/` byte-for-byte identical, including local Bootstrap, icons/fonts, QRCode.js, and `select.js`; `Local.Open` serves the root copy when web UI serving is enabled.
  - `TestAdminUIAssetMirror` passes, confirming the mirrored copies are identical.
- [ ] **Asset Delivery**: Embedded assets still respond with the correct content type, gzip only when `Accept-Encoding` allows it, and `304 Not Modified` for a matching `If-None-Match` ETag.
- [ ] **Player & System Cards**: Sticky player does not cover content; Download Scope, System Backup, and Podsync Updates states are correct on desktop and mobile.

---

## 6. MCP & Skill Synchronization

- [ ] **Tool Count**: All 34 MCP tools in `pkg/mcp/server.go` have handlers and synchronized references. Stdio proxy mode relays JSON-RPC to the daemon, including personal-feed tools.
- [ ] **MCP Coverage Decision**: Every `web.AdminManager` method is listed in `adminManagerMCPExposure` with a registered MCP tool or a documented web-only reason. Run `TestAdminManagerMCPCoverage`.
- [ ] **Skill Parity**:
  - Updated `.agents/skills/podsync/SKILL.md`.
  - Copied to `/Users/tatarinovms/.gemini/config/skills/podsync/SKILL.md` and project skill directories.
  - All scalar strings in YAML frontmatter are enclosed in double quotes (`"..."`).

- [ ] **Documentation**: Reviewed both README files, affected English/Russian guides, and skills; updated changed behavior, examples, and links.
- [ ] **UI Tests**: Ran the dependency-free Node.js tests and, when changing browser behavior, the [Playwright suite](../../tests/e2e/README.md).
- [ ] **Docker Metadata**: Version and commit reach Docker builds through `TAG` and `COMMIT` build arguments; `.git/` and local data stay outside the context.
- [ ] **Release Integrity**: A release marked Latest contains `checksums.txt` and native archives expected by `pkg/selfupdate`; extension-only releases are not Latest.
- [ ] **Firefox Extension**: Manifest version, icon, supported-host validation, Firefox minimum versions, Mozilla data-collection permissions, signed `.xpi`, and direct README download links are current.
