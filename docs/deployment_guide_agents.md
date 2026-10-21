# Deployment & Setup Guide for AI Agents (OpenCode, Hermes)

**English** | [Русский](./ru/deployment_guide_agents.md)


This guide provides automated step-by-step instructions for autonomous agents (**OpenCode**, **Hermes Agent / Nous Hermes**, **Antigravity**, **Claude Code**) to deploy, configure, and operate Podsync.

---

## 1. Primary Method: Native Linux Systemd Service (Recommended)

Running Podsync natively as a `systemd` service provides maximum performance, minimal resource overhead, direct filesystem access, and seamless process supervision.

### Step 1.1: Install System Dependencies
Podsync requires `ffmpeg` and `yt-dlp` (or `youtube-dl`).

**Debian / Ubuntu**:
```bash
sudo apt-get update && sudo apt-get install -y ffmpeg curl ca-certificates

# Install latest yt-dlp binary (recommended over apt repository package)
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

**RHEL / CentOS / Rocky Linux / Fedora**:
```bash
sudo dnf install -y ffmpeg curl
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

**Verify dependencies**:
```bash
yt-dlp --version
ffmpeg -version | head -n 1
```

---

### Step 1.2: Prepare Dedicated User & Directory Layout
Never run Podsync as `root`. Create a dedicated system user and service directories:

```bash
sudo useradd -r -s /bin/false -d /var/lib/podsync podsync
sudo mkdir -p /opt/podsync/bin
sudo mkdir -p /etc/podsync
sudo mkdir -p /var/lib/podsync/data
sudo mkdir -p /var/lib/podsync/db

sudo chown -R podsync:podsync /var/lib/podsync
sudo chown -R podsync:podsync /etc/podsync
```

---

### Step 1.3: Install Podsync Binary

#### Option A: Download Precompiled Release Binary (One-Liner for Agents)
Agents can auto-detect CPU architecture (`x86_64` vs `arm64`) and fetch the latest binary from GitHub Releases without needing Go installed:

```bash
# Auto-detect architecture and fetch latest release tag
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) GORELEASER_ARCH="x86_64" ;;
  aarch64|arm64) GORELEASER_ARCH="arm64" ;;
  armv7l|armhf) GORELEASER_ARCH="armv7" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

# Get latest release version from GitHub API (or set fixed VERSION="v2.18.1")
LATEST_TAG=$(curl -s "https://api.github.com/repos/tatarinovms/podsync/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST_TAG" ]; then
  LATEST_TAG="v2.18.1"
fi
VERSION_NUM="${LATEST_TAG#v}"

echo "Downloading Podsync $LATEST_TAG for Linux $GORELEASER_ARCH..."
TAR_URL="https://github.com/tatarinovms/podsync/releases/download/${LATEST_TAG}/Podsync_${VERSION_NUM}_Linux_${GORELEASER_ARCH}.tar.gz"

# Download and extract binary directly into /opt/podsync/bin
curl -sSL "$TAR_URL" | sudo tar -xz -C /opt/podsync/bin podsync
sudo chmod +x /opt/podsync/bin/podsync

# Verify binary
/opt/podsync/bin/podsync --version || /opt/podsync/bin/podsync --help
```

#### Option B: Build from Source
If building directly from cloned repository source:
```bash
make build
sudo cp bin/podsync /opt/podsync/bin/podsync
sudo chmod +x /opt/podsync/bin/podsync
```

---

### Step 1.4: Configuration File (`/etc/podsync/config.toml`)
Create the configuration file:

```toml
# /etc/podsync/config.toml

[server]
port = 8080
# Optional: public reverse proxy URL (e.g. "https://podcasts.example.com")
# hostname = "https://podcasts.example.com"

[server.admin]
enabled = true
username = "admin"
password = "ChangeMeSecretPassword!" # Change this!

[storage]
  [storage.local]
  data_dir = "/var/lib/podsync/data"

[database]
dir = "/var/lib/podsync/db"

[downloader]
timeout = 15
# Optional: Netscape cookies file to bypass YouTube bot checks
# cookies_file = "/var/lib/podsync/data/cookies.txt"

[tokens]
# vkvideo = "YOUR_VK_TOKEN"
# youtube = "YOUR_YOUTUBE_API_KEY"

[feeds]
  # Example: VK Video Feed
  [feeds.MINAEV]
  url = "https://vkvideo.ru/@minaev_live"
  page_size = 10
  quality = "high"
  format = "audio"
  update_period = "6h"

  # Example: YouTube Channel Feed
  # [feeds.YOUTUBE_SAMPLE]
  # url = "https://www.youtube.com/channel/..."
  # page_size = 10
  # quality = "high"
  # format = "audio"
  # update_period = "6h"
```

Set secure permissions on the config file:
```bash
sudo chown podsync:podsync /etc/podsync/config.toml
sudo chmod 600 /etc/podsync/config.toml
```

---

### Step 1.5: Systemd Unit Setup
Create the systemd service file:

```bash
sudo tee /etc/systemd/system/podsync.service > /dev/null << 'EOF'
[Unit]
Description=Podsync Podcast Generator Service
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=podsync
Group=podsync
WorkingDirectory=/var/lib/podsync
ExecStart=/opt/podsync/bin/podsync --config /etc/podsync/config.toml
Restart=on-failure
RestartSec=5s

# Security sandboxing
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/podsync /etc/podsync

# Standard output to journald
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
```

Reload systemd and start service:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now podsync
```

**Inspect status and logs**:
```bash
sudo systemctl status podsync
sudo journalctl -u podsync -f -n 50
```

Verify service is responding:
```bash
curl -I http://localhost:8080/health
```

---

## 2. Alternative Method: Docker / Docker Compose

Use Docker if the target OS is not Linux (e.g. macOS, Windows), or if managing system dependencies (`ffmpeg`, `yt-dlp`) on the host is inconvenient.

### Step 2.1: Directory Setup
Create a working directory for Podsync:
```bash
mkdir -p podsync-compose/{data,db}
cd podsync-compose
```

### Step 2.2: Prepare `config.toml`
Create `config.toml` in the working directory:
```toml
[server]
port = 8080

[server.admin]
enabled = true
username = "admin"
password = "ChangeMeSecretPassword!"

[storage]
  [storage.local]
  data_dir = "/app/data/"

[database]
dir = "/app/db/"

[tokens]
# vkvideo = "..."
# youtube = "..."

[feeds]
  [feeds.SAMPLE]
  url = "https://vkvideo.ru/@clanbeaver"
  page_size = 5
  quality = "high"
  format = "audio"
  update_period = "12h"
```

### Step 2.3: `docker-compose.yml`
Create `docker-compose.yml`:
```yaml
services:
  podsync:
    container_name: podsync
    image: ghcr.io/tatarinovms/podsync:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data/
      - ./db:/app/db/
      - ./config.toml:/app/config.toml
    # environment:
    #   - PODSYNC_VKVIDEO_API_KEY=YOUR_VK_TOKEN
    #   - PODSYNC_YOUTUBE_API_KEY=YOUR_YOUTUBE_API_KEY
```

### Step 2.4: Launch Container
```bash
docker compose up -d

# Check logs
docker compose logs -f

# Check health
curl -I http://localhost:8080/health
```

---

## 3. Configuring OpenCode & Hermes for MCP Integration

Once Podsync is running (either as systemd service or via Docker), AI agents can manage feeds, inspect downloads, and retry errors through MCP.

### 3.1: OpenCode (`opencode.json` / `opencode.jsonc`)
Add to `opencode.json` in your project root or `~/.config/opencode/opencode.json`:

#### Option A: Remote SSE Mode (when Podsync is running on a server or container)
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "podsync": {
      "type": "remote",
      "url": "http://localhost:8080/mcp/sse",
      "headers": {
        "Authorization": "Basic YWRtaW46Q2hhbmdlTWVTZWNyZXRQYXNzd29yZCE="
      }
    }
  }
}
```
*(Note: `YWRtaW46...` is base64 of `username:password`)*.

#### Option B: Local Stdio Mode
```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "podsync": {
      "type": "local",
      "command": [
        "/opt/podsync/bin/podsync",
        "--config",
        "/etc/podsync/config.toml",
        "--mcp"
      ]
    }
  }
}
```

---

### 3.2: Hermes Agent Configuration
For Hermes Agent / Nous Hermes environments supporting MCP client configurations (`hermes.json` or MCP settings):

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

If connecting via SSE/HTTP transport:
```json
{
  "mcpServers": {
    "podsync": {
      "url": "http://127.0.0.1:8080/mcp/sse",
      "headers": {
        "Authorization": "Basic YWRtaW46Q2hhbmdlTWVTZWNyZXRQYXNzd29yZCE="
      }
    }
  }
}
```

---


---

## 4. Upgrade Guide for Existing Installations

When upgrading a running Podsync instance to a newer version:

### 4.1: Upgrading Linux Systemd Service (Zero Database Data Loss)

Agents can execute this automated 4-step upgrade workflow:

```bash
# 1. Stop the running service safely
sudo systemctl stop podsync

# 2. Backup current binary
if [ -f /opt/podsync/bin/podsync ]; then
  sudo cp /opt/podsync/bin/podsync /opt/podsync/bin/podsync.bak
fi

# 3. Detect architecture & download latest binary
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) GORELEASER_ARCH="x86_64" ;;
  aarch64|arm64) GORELEASER_ARCH="arm64" ;;
  armv7l|armhf) GORELEASER_ARCH="armv7" ;;
  *) echo "Unsupported architecture: $ARCH" && exit 1 ;;
esac

LATEST_TAG=$(curl -s "https://api.github.com/repos/tatarinovms/podsync/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST_TAG" ]; then
  LATEST_TAG="v2.18.1"
fi
VERSION_NUM="${LATEST_TAG#v}"

echo "Upgrading Podsync to $LATEST_TAG ($GORELEASER_ARCH)..."
TAR_URL="https://github.com/tatarinovms/podsync/releases/download/${LATEST_TAG}/Podsync_${VERSION_NUM}_Linux_${GORELEASER_ARCH}.tar.gz"

curl -sSL "$TAR_URL" | sudo tar -xz -C /opt/podsync/bin podsync
sudo chmod +x /opt/podsync/bin/podsync

# Optional: Update yt-dlp to latest version as well (prevents video extraction failures)
if [ -f /usr/local/bin/yt-dlp ]; then
  sudo /usr/local/bin/yt-dlp -U || true
fi

# 4. Start service and verify health
sudo systemctl start podsync
sleep 2

if curl -fs "http://localhost:8080/health" > /dev/null; then
  echo "Podsync successfully upgraded to $LATEST_TAG and running healthy!"
else
  echo "Health check failed after upgrade! Reverting to backup binary..."
  sudo cp /opt/podsync/bin/podsync.bak /opt/podsync/bin/podsync
  sudo systemctl restart podsync
  exit 1
fi
```

### 4.2: Upgrading Docker / Docker Compose

For containerized setups:

```bash
cd /path/to/podsync-compose

# 1. Pull the newest image from GitHub Container Registry
docker compose pull

# 2. Recreate container with existing volumes (data and db are untouched)
docker compose up -d

# 3. Verify status
docker compose ps
docker compose logs --tail=20 podsync
curl -I http://localhost:8080/health
```

### 4.3: Upgrading via Source Build (Git)

```bash
cd /path/to/cloned/podsync

# 1. Fetch latest commits or release tag
git fetch --tags
git checkout $(git describe --tags `git rev-list --tags --max-count=1`)

# 2. Rebuild binary
make build

# 3. Restart systemd service
sudo systemctl stop podsync
sudo cp bin/podsync /opt/podsync/bin/podsync
sudo systemctl start podsync
```

## 5. Operational Checklist for AI Agents

When an agent needs to perform tasks on Podsync:

| Action | Recommended Tool / Command |
|---|---|
| **Check service health** | `curl -s http://localhost:8080/health` or MCP tool `get_sync_status` |
| **Inspect download errors** | MCP tool `get_download_errors` |
| **View recent downloads** | MCP tool `get_recent_downloads` |
| **Add a new podcast channel** | MCP tool `add_feed` (auto-updates `config.toml`) |
| **Trigger immediate sync** | MCP tool `update_feed(feed_id)` |
| **Retry failed episode** | MCP tool `retry_episode(feed_id, episode_id)` |
| **Bypass YouTube bot verification**| Upload Netscape `cookies.txt` via Admin UI or `PODSYNC_DOWNLOADER_COOKIES_FILE` |
