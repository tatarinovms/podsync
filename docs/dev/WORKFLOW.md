# Development & Operational Workflow

See the [developer documentation index](./README.md) for related guides.

This guide details the standard development lifecycle, test procedures, tagging, release processes, and deployment strategies for **Podsync**.

---

## 1. Local Development Setup

### 1.1 Prerequisites
Install required binaries:
- **Go**: 1.25.8 (`go version`)
- **yt-dlp**: Latest release (`yt-dlp --version`)
- **ffmpeg**: Required for audio transcoding and ID3 cover extraction (`ffmpeg -version`)

On macOS:
```bash
brew install go yt-dlp ffmpeg
```

On Ubuntu / Debian:
```bash
sudo apt update
sudo apt install -y golang ffmpeg
sudo wget https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -O /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

### 1.2 Running Locally
Copy sample configuration:
```bash
cp config.toml.example config.toml
```

Build and run:
```bash
make build
./bin/podsync --config config.toml
```

Or run directly without building:
```bash
go run ./cmd/podsync --config config.toml
```

The Web Admin console will be available at `http://localhost:8080/admin`.

---

## 2. Testing Procedures

Always verify test suites before committing changes.

CI builds on Linux, Windows, and macOS; run the tests below locally before committing.

### 2.1 Running All Unit Tests
```bash
# Run all tests with race detector
go test -v -race ./...

# Run without cache
go test -count=1 ./...
```

### 2.2 Package-Specific Testing
```bash
# Test Web API & Admin Handlers
go test -v ./services/web

# Test Feed Manager & Config Persistence
go test -v ./cmd/podsync

# Test RSS XML & iTunes Generator
go test -v ./pkg/feed

# Test Builders (VK, YouTube, Audiobooks, Rutube)
go test -v ./pkg/builder

# Test Personal Feeds Service
go test -v ./pkg/personal

# Test MCP Server, Documentation Synchronization, and Admin Capability Coverage
go test -v -run 'Test(MCPToolsDocumentationSync|AdminManagerMCPCoverage)' ./pkg/mcp/...
```

### 2.3 Web UI Tests

Run the dependency-free Node.js suites from the repository root:

```bash
node services/web/admin_ui_test.js
node services/web/admin_security_test.js
node services/web/episodes_ui_test.js
```

Run the browser regression suite separately:

```bash
cd tests/e2e
npm ci
npx playwright install chromium
npm test
```

See the [browser test guide](../../tests/e2e/README.md) for Linux dependencies,
screenshots, and executable overrides. Run this suite locally for browser changes.

---

## 3. Web UI Workflow (`services/web/ui/`)

The Web UI is an embedded Single Page Application located in `services/web/ui/index.html`.

### 3.1 Synchronizing Files
Whenever Web UI assets in `services/web/ui/` are modified, keep `html/` byte-for-byte synchronized:
```bash
cp services/web/ui/index.html html/index.html
cp services/web/ui/admin.js html/admin.js
cp services/web/ui/admin.css html/admin.css
cp services/web/ui/theme.js html/theme.js
```

The embedded admin UI reads `services/web/ui/`; local storage's `Local.Open`
serves the root `html/` assets when web UI serving is enabled. Both paths are
currently used and `TestAdminUIAssetMirror` fails if they diverge.

Serving notes: `services/web/ui.go` precompresses embedded text assets at
startup, derives an ETag from each file, and revalidates HTML/JS/CSS with `304`
responses. New UI files are picked up automatically from `//go:embed ui/*`.

### 3.2 Modal Lifecycle Rules
- **Do not use** `bootstrap.Modal.getInstance(el).hide()`. It can evaluate to `null` if the instance was created with `getOrCreateInstance` or detached.
- **Always use** `closeModal('modalId')`. It guarantees reliable closing, clears `.modal-backdrop`, and restores `document.body` scrolling.

### 3.3 Theme Verification
- Check both **Catppuccin Mocha** (dark) and **Catppuccin Latte** (light) using the top navigation toggle.
- Verify that buttons use `.btn-primary` (Mauve) or `.btn-brand`, and that no raw Bootstrap blue (`#0d6efd`) or slate-gray (`#2b3035`) is visible.

---

## 4. Git Versioning & Release Workflow

Podsync uses Semantic Versioning (`vMAJOR.MINOR.PATCH`).

### 4.1 Commit Standards
Follow conventional commit prefixes:
- `feat(...)`: New features (e.g. audiobook folder deletion, serial format).
- `fix(...)`: Bug fixes (e.g. modal closing, private feed persistence).
- `docs(...)`: Documentation updates (`AGENTS.md`, guides).
- `refactor(...)`: Code cleanup without behavior change.

### 4.2 Releasing a New Version
1. Verify all tests pass:
   ```bash
   go test -v -race ./...
   make build
   ```
2. Check git status to ensure working tree is clean:
   ```bash
   git status
   ```
3. Commit all changes:
   ```bash
   git add -A
   git commit -m "feat(audiobooks): add deletion of server audiobook folders from disk"
   ```
4. Create an annotated git tag and push:
   ```bash
   # Replace vX.Y.Z with the intended release version.
   git tag -a vX.Y.Z -m "Release vX.Y.Z"
   git push origin main
   git push origin refs/tags/vX.Y.Z
   ```

GoReleaser publishes native archives as
`Podsync_<version>_<OS>_<arch>` (for example,
`Podsync_2.23.8_Linux_x86_64.tar.gz`). Keep this exact format: the built-in
native updater selects an archive by that name before verifying its checksum.

Before marking a release as **Latest**, verify that it contains
`checksums.txt` and the expected native archives. A Firefox-extension-only
release must be a pre-release or non-latest release; otherwise
`/api/v1/system/update` will resolve an asset set that cannot update Podsync.

### 4.3 Firefox Extension Packaging

The extension is zero-build source under `extensions/podsync-firefox/`. Before
submission:

1. Increment `manifest.json` version and keep desktop/Android Firefox minimums
   compatible with `browser_specific_settings.gecko.data_collection_permissions`.
2. Ensure the disclosure declares `authenticationInfo` and
   `browsingActivity`, and verify that only YouTube, VK Video, and Rutube links
   can be submitted.
3. Create the ZIP with files at the archive root, submit it to AMO without a
   separate generated-source bundle, and attach the signed `.xpi` directly to a
   GitHub Release.
4. Use a direct release-asset URL in README files; do not link to a moving
   `releases/latest` page or a GitHub source/archive URL.

### 4.4 Backup Operations

For offline export and restore:

```bash
podsync --config config.toml --export-backup podsync-backup.tar.gz
podsync --config config.toml --restore-backup podsync-backup.tar.gz
```

Restore preserves the target server's storage paths and port by default. Use
`--no-preserve-paths` only when the archive's original paths are intentionally
required. Inspect an uploaded archive before restoring it through the Web UI or
REST API.

---

## 5. Docker Build & Deployment

### 5.1 Local Docker Build
```bash
docker build -t podsync:local .
```

The context excludes `.git/`, documentation, agent configuration, local
credentials, databases, downloaded media, and build artifacts. To include version
metadata, use `make docker` or pass `--build-arg TAG=...` and
`--build-arg COMMIT=...` to `docker build`. The Docker defaults are `dev` and
`unknown`; release workflows pass their tag and commit explicitly.

### 5.2 Running with Docker Compose
```bash
docker compose up -d
docker compose logs -f podsync
```

### 5.3 Volumes to Mount
- `/app/data/`: Podsync data directory (BadgerDB and downloaded media).
- `/app/config.toml`: Path to active config file.
- `/data/audiobooks`: Local directory containing audiobook subfolders.
