# Руководство по развертыванию и настройке для AI-агентов (OpenCode, Hermes)

[English](../deployment_guide_agents.md) | **Русский**

В этом руководстве собраны пошаговые автоматизированные инструкции для автономных агентов (**OpenCode**, **Hermes Agent / Nous Hermes**, **Antigravity**, **Claude Code**) по установке, настройке и эксплуатации Podsync.

---

## 1. Основной метод: Нативный сервис Linux Systemd (Рекомендуется)

Запуск Podsync напрямую в качестве сервиса `systemd` обеспечивает максимальную производительность, минимальный оверхед по памяти, прямой доступ к файловой системе и надёжный супервизинг процесса.

### Шаг 1.1: Установка системных зависимостей
Podsync требует наличия `ffmpeg` и `yt-dlp` (или `youtube-dl`).

**Debian / Ubuntu**:
```bash
sudo apt-get update && sudo apt-get install -y ffmpeg curl ca-certificates

# Установка актуального бинарника yt-dlp (рекомендуется вместо устаревшего пакета apt)
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

**RHEL / CentOS / Rocky Linux / Fedora**:
```bash
sudo dnf install -y ffmpeg curl
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

**Проверка зависимостей**:
```bash
yt-dlp --version
ffmpeg -version | head -n 1
```

---

### Шаг 1.2: Создание системного пользователя и директорий
Не запускайте Podsync из-под `root`. Создайте выделенного пользователя и папки:

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

### Шаг 1.3: Установка бинарника Podsync

#### Вариант A: Загрузка готового релиза (однострочник для агентов)
Агенты могут автоматически определить архитектуру процессора (`x86_64` или `arm64`) и скачать релиз с GitHub:

```bash
# Автоопределение архитектуры и получение последнего тега релиза
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) GORELEASER_ARCH="x86_64" ;;
  aarch64|arm64) GORELEASER_ARCH="arm64" ;;
  armv7l|armhf) GORELEASER_ARCH="armv7" ;;
  *) echo "Неподдерживаемая архитектура: $ARCH" && exit 1 ;;
esac

# Получение последней версии через GitHub API (или фиксированная fallback-версия "v2.19.0")
LATEST_TAG=$(curl -s "https://api.github.com/repos/tatarinovms/podsync/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST_TAG" ]; then
  LATEST_TAG="v2.19.0"
fi
VERSION_NUM="${LATEST_TAG#v}"

echo "Скачивание Podsync $LATEST_TAG для Linux $GORELEASER_ARCH..."
TAR_URL="https://github.com/tatarinovms/podsync/releases/download/${LATEST_TAG}/Podsync_${VERSION_NUM}_Linux_${GORELEASER_ARCH}.tar.gz"

# Скачивание и распаковка бинарника в /opt/podsync/bin
curl -sSL "$TAR_URL" | sudo tar -xz -C /opt/podsync/bin podsync
sudo chmod +x /opt/podsync/bin/podsync

# Проверка бинарного файла
/opt/podsync/bin/podsync --version || /opt/podsync/bin/podsync --help
```

#### Вариант B: Сборка из исходников
Если сборка выполняется из клонированного репозитория:
```bash
make build
sudo cp bin/podsync /opt/podsync/bin/podsync
sudo chmod +x /opt/podsync/bin/podsync
```

---

### Шаг 1.4: Файл конфигурации (`/etc/podsync/config.toml`)
Создайте конфигурационный файл:

```toml
# /etc/podsync/config.toml

[server]
port = 8080
# Опционально: публичный URL за обратным прокси (например, "https://podcasts.example.com")
# hostname = "https://podcasts.example.com"

[server.admin]
enabled = true
username = "admin"
password = "ChangeMeSecretPassword!" # Обязательно замените пароль!

[storage]
  [storage.local]
  data_dir = "/var/lib/podsync/data"

[database]
dir = "/var/lib/podsync/db"

[downloader]
timeout = 15
# Опционально: файл cookies формата Netscape для обхода защиты YouTube
# cookies_file = "/var/lib/podsync/data/cookies.txt"

[tokens]
# vkvideo = "YOUR_VK_TOKEN"
# youtube = "YOUR_YOUTUBE_API_KEY"

[feeds]
  # Пример: Лента VK Video
  [feeds.MINAEV]
  url = "https://vkvideo.ru/@minaev_live"
  page_size = 10
  quality = "high"
  format = "audio"
  update_period = "6h"

  # Пример: Канал YouTube
  # [feeds.YOUTUBE_SAMPLE]
  # url = "https://www.youtube.com/channel/..."
  # page_size = 10
  # quality = "high"
  # format = "audio"
  # update_period = "6h"
```

Настройте безопасные права доступа:
```bash
sudo chown podsync:podsync /etc/podsync/config.toml
sudo chmod 600 /etc/podsync/config.toml
```

---

### Шаг 1.5: Настройка службы Systemd
Создайте unit-файл systemd:

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

# Ограничения безопасности
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/podsync /etc/podsync

# Вывод логов в journald
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF
```

Примените конфигурацию и запустите сервис:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now podsync
```

**Проверка статуса и логов**:
```bash
sudo systemctl status podsync
sudo journalctl -u podsync -f -n 50
```

Проверка доступности healthcheck:
```bash
curl -I http://localhost:8080/health
```

---

## 2. Альтернативный метод: Docker / Docker Compose

Используйте Docker, если целевая ОС отлична от Linux (например, macOS, Windows) или при нежелании вручную управлять системными пакетами (`ffmpeg`, `yt-dlp`).

### Шаг 2.1: Создание структуры папок
```bash
mkdir -p podsync-compose/{data,db}
cd podsync-compose
```

### Шаг 2.2: Создание `config.toml`
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

### Шаг 2.3: `docker-compose.yml`
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
```

### Шаг 2.4: Запуск контейнера
```bash
docker compose up -d

# Просмотр логов
docker compose logs -f

# Проверка health
curl -I http://localhost:8080/health
```

---

## 3. Настройка интеграции MCP с OpenCode и Hermes

После запуска сервиса AI-агенты могут управлять подкастами через Model Context Protocol.

### 3.1: OpenCode (`opencode.json` / `opencode.jsonc`)
Добавьте в `opencode.json` в корне проекта или в `~/.config/opencode/opencode.json`:

#### Вариант A: Удалённый режим SSE (когда Podsync запущен на сервере или в контейнере)
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
*(Примечание: `YWRtaW46...` — это base64 от `username:password`)*.

#### Вариант B: Локальный режим Stdio
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

### 3.2: Настройка Hermes Agent
Для окружений Hermes Agent / Nous Hermes (`hermes.json`):

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

При подключении через SSE:
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

## 4. Обновление существующих установок

### 4.1: Обновление службы Linux Systemd (без потери данных)

Агенты могут выполнять следующий автоматизированный скрипт:

```bash
# 1. Остановка сервиса
sudo systemctl stop podsync

# 2. Создание резервной копии текущего бинарника
if [ -f /opt/podsync/bin/podsync ]; then
  sudo cp /opt/podsync/bin/podsync /opt/podsync/bin/podsync.bak
fi

# 3. Определение архитектуры и скачивание релиза
ARCH=$(uname -m)
case "$ARCH" in
  x86_64) GORELEASER_ARCH="x86_64" ;;
  aarch64|arm64) GORELEASER_ARCH="arm64" ;;
  armv7l|armhf) GORELEASER_ARCH="armv7" ;;
  *) echo "Неподдерживаемая архитектура: $ARCH" && exit 1 ;;
esac

LATEST_TAG=$(curl -s "https://api.github.com/repos/tatarinovms/podsync/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST_TAG" ]; then
  LATEST_TAG="v2.19.0"
fi
VERSION_NUM="${LATEST_TAG#v}"

echo "Обновление Podsync до $LATEST_TAG ($GORELEASER_ARCH)..."
TAR_URL="https://github.com/tatarinovms/podsync/releases/download/${LATEST_TAG}/Podsync_${VERSION_NUM}_Linux_${GORELEASER_ARCH}.tar.gz"

curl -sSL "$TAR_URL" | sudo tar -xz -C /opt/podsync/bin podsync
sudo chmod +x /opt/podsync/bin/podsync

# Опционально: обновление yt-dlp
if [ -f /usr/local/bin/yt-dlp ]; then
  sudo /usr/local/bin/yt-dlp -U || true
fi

# 4. Запуск службы и проверка здоровья
sudo systemctl start podsync
sleep 2

if curl -fs "http://localhost:8080/health" > /dev/null; then
  echo "Podsync успешно обновлён до $LATEST_TAG и работает стабильно!"
else
  echo "Сбой healthcheck после обновления! Откат к резервной копии..."
  sudo cp /opt/podsync/bin/podsync.bak /opt/podsync/bin/podsync
  sudo systemctl restart podsync
  exit 1
fi
```

### 4.2: Обновление Docker / Docker Compose

```bash
cd /path/to/podsync-compose

# 1. Загрузка свежего образа
docker compose pull

# 2. Пересоздание контейнера с сохранением volumes
docker compose up -d

# 3. Проверка статуса
docker compose ps
docker compose logs --tail=20 podsync
curl -I http://localhost:8080/health
```

---

## 5. Чек-лист операций для AI-агентов

| Действие | Рекомендуемый инструмент / команда |
|---|---|
| **Проверка здоровья сервиса** | `curl -s http://localhost:8080/health` или MCP-инструмент `get_sync_status` |
| **Анализ ошибок загрузки** | MCP-инструмент `get_download_errors` |
| **Просмотр свежих эпизодов** | MCP-инструмент `get_recent_downloads` |
| **Добавление нового канала** | MCP-инструмент `add_feed` (автоматически обновляет `config.toml`) |
| **Принудительное обновление** | MCP-инструмент `update_feed(feed_id)` |
| **Повтор ошибки эпизода** | MCP-инструмент `retry_episode(feed_id, episode_id)` |
| **Обход защиты YouTube** | Загрузка Netscape `cookies.txt` через веб-панель или `PODSYNC_DOWNLOADER_COOKIES_FILE` |
