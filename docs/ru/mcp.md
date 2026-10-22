# Сервер Model Context Protocol (MCP) в Podsync

[English](../mcp.md) | **Русский**

Podsync включает встроенный сервер **MCP (Model Context Protocol)**, позволяющий автономным AI-ассистентам и агентам (**OpenCode**, **Hermes Agent / Nous Hermes**, **Claude Desktop**, **Cursor**, **Cline**, **Antigravity**) напрямую отслеживать состояние подкастов, управлять лентами, перезапускать загрузки и анализировать ошибки.

---

## 1. Обзор и архитектура

Сервер MCP работает в двух взаимозаменяемых режимах:

```
┌────────────────────────────────────────────────────────┐
│                  AI-агент / Ассистент                  │
│  (OpenCode / Hermes / Claude Desktop / Cursor / Cline) │
└───────────────────────┬────────────────────────────────┘
                        │
         ┌──────────────┴──────────────┐
         │                             │
   [1. Режим Stdio]             [2. Удалённый режим SSE]
   `podsync --mcp`             `http://<host>:8080/mcp/sse`
         │                             │
         ▼                             ▼
┌──────────────────┐          ┌──────────────────┐
│ Auto-Proxy /     │          │ Веб-сервер       │
│ Хранилище Badger │          │ HTTP Podsync     │
└──────────────────┘          └──────────────────┘
```

1. **Режим Stdio (`podsync --mcp`)**: Запускается локально на машине через стандартные потоки ввода/вывода (stdin/stdout).
   - **Auto-Proxy**: При запуске утилита проверяет, запущен ли уже основной демон Podsync на `http://localhost:<server.port>`, используя учётные данные из `[server.admin]`. Если сервис отвечает, CLI прозрачно проксирует все вызовы инструментов через запущенный экземпляр по HTTP, предотвращая конфликты блокировки базы BadgerDB (`resource temporarily unavailable`).
   - **Ограничения**: Автопрокси опрашивает только локальный порт `http://localhost:<server.port>`. Он не поддерживает TLS (HTTPS), внешние доменные имена и префиксы reverse-proxy (например, `/podsync/`). Если сервис работает за обратным прокси или на удалённом сервере, используйте **Remote SSE Mode**.
   - **Автономный режим (Standalone)**: Если на `localhost:<server.port>` сервис не обнаружен, утилита открывает базу данных напрямую на чтение/запись.
2. **Удалённый режим SSE (`/mcp/sse`)**: Встроен непосредственно в веб-сервер Podsync. Позволяет агентам на других серверах, в Docker-контейнерах или за reverse-proxy (Nginx/Traefik с HTTPS) безопасно подключаться по HTTP/HTTPS с базовой авторизацией.

---

## 2. Справочник инструментов (Tools Reference)

MCP-сервер предоставляет 28 специализированных инструментов:

### Мониторинг и статус
| Имя инструмента | Аргументы | Описание |
|---|---|---|
| **`get_sync_status`** | `{}` | Общий срез состояния системы: активные ленты, расписание cron, следующие запуски, загрузки за последние 24ч, активные ошибки и свободное место на диске. |
| **`get_download_errors`** | `feed_id` *(опц)*, `limit` *(опц, по умолч: 20)* | Список неудачных загрузок с очищенными диагностическими причинами (детект ботов, 429 rate limit, приватные видео) и рекомендациями. |
| **`get_recent_downloads`** | `feed_id` *(опц)*, `limit` *(опц, по умолч: 10)* | Список последних успешно загруженных эпизодов с размерами файлов, датами публикации и прямыми ссылками. |
| **`get_system_stats`** | `{}` | Метрики экземпляра в реальном времени: потребление RAM, место на диске (`data_dir`), доступность `yt-dlp` и `ffmpeg`. |
| **`get_system_logs`** | `level` *(опц)*, `search` *(опц)*, `feed_id` *(опц)*, `limit` *(опц, по умолч: 50)* | Выборка системных логов из кольцевого буфера памяти с фильтрацией по уровню, тексту или ленте. |

### Управление лентами
| Имя инструмента | Аргументы | Описание |
|---|---|---|
| **`list_feeds`** | `{}` | Список всех настроенных подкаст-лент, количество эпизодов, общий объём и публичные URL. |
| **`get_feed`** | `feed_id` *(обяз)* | Детальные метаданные ленты со списком всех эпизодов и их статусами (`downloaded`, `new`, `error`). |
| **`add_feed`** | `url` *(обяз)*, `format`, `quality`, `page_size`, `update_period`, `language` | Добавить новый канал/плейлист (YouTube, VK Video, Rutube, Vimeo и др.) с автоматической записью в `config.toml`. |
| **`delete_feed`** | `feed_id` *(обяз)*, `delete_files` *(опц, по умолч: false)* | Удалить ленту и опционально стереть скачанные файлы с диска. |
| **`update_feed`** | `feed_id` *(обяз)* | Запустить немедленный цикл проверки и скачивания для указанной ленты. |
| **`update_all_feeds`** | `{}` | Запустить немедленную проверку и скачивание для всех настроенных лент одновременно. |
| **`update_feed_config`** | `feed_id` *(обяз)*, `cron_schedule`, `format`, `quality`, `page_size`, `keep_last`, `max_height`, `playlist_sort`, `private_feed`, `language` | Обновить параметры ленты с сохранением в `config.toml` (неуказанные поля сохраняются). Аудиокниги cron не используют. |
| **`retry_episode`** | `feed_id` *(обяз)*, `episode_id` *(обяз)* | Сбросить ошибку эпизода, чтобы Podsync попытался скачать его заново. |

### Загрузчик и аутентификация
| Имя инструмента | Аргументы | Описание |
|---|---|---|
| **`get_downloader_config`** | `{}` | Путь к бинарнику yt-dlp, кастомный бинарник, версия, статус автообновления, таймаут, cookies и прокси. |
| **`update_downloader_config`** | `timeout`, `proxy`, `cookies_from_browser`, `language`, `custom_binary`, `self_update` | Обновить параметры yt-dlp, прокси, предпочитаемый язык аудио, переопределение бинарника или автообновление. |
| **`test_downloader`** | `url` *(опц)* | Протестировать извлечение метаданных yt-dlp по URL для проверки cookies и сетевого подключения. |
| **`get_tokens`** | `{}` | Статус API-токенов (маскированы в целях безопасности) для YouTube, VK Video, Rutube, Vimeo, SoundCloud и Twitch. |
| **`update_tokens`** | `provider` *(обяз)*, `tokens` *(обяз)* | Обновить токены провайдера (поддерживается массив токенов для round-robin ротации). |
| **`delete_tokens`** | `provider` *(обяз)*, `index` *(опц)* | Удалить отдельный токен по индексу или очистить все токены указанного провайдера. |

### Управление персональными лентами
| Имя инструмента | Аргументы | Описание |
|---|---|---|
| **`list_personal_feeds`** | `{}` | Список всех персональных лент, количество эпизодов, общий объём и публичные URL. |
| **`get_personal_feed`** | `feed_id` *(обяз)* | Метаданные ленты и список всех эпизодов (со статусом, исходным URL/файлом и медиа-ссылкой). |
| **`create_personal_feed`** | `title` *(обяз)*, `description`, `cover_art`, `playlist_sort`, `private_feed` | Создать новую персональную ленту. |
| **`add_personal_episode`** | `feed_id` *(обяз)*, `url` *(опц)*, `file_path` *(опц)*, `title` *(опц)*, `format` *(опц: audio/video)* | Добавить эпизод в персональную ленту по ссылке (YouTube, VK, Rutube, прямая аудио/видео ссылка) или пути к файлу на сервере. |
| **`delete_personal_episode`** | `feed_id` *(обяз)*, `episode_id` *(обяз)* | Удалить эпизод из персональной ленты и очистить занятое дисковое пространство. |
| **`rename_personal_episode`** | `feed_id` *(обяз)*, `episode_id` *(обяз)*, `title` *(обяз)* | Переименовать эпизод в персональной подкаст-ленте. |

### Управление аудиокнигами
| Имя инструмента | Аргументы | Описание |
|---|---|---|
| **`list_audiobook_dirs`** | `{}` | Список папок в хранилище аудиокниг: количество глав, суммарный размер, наличие обложки и статус привязанной ленты. |
| **`import_audiobook`** | `dir_name` *(обяз)*, `feed_id`, `title`, `author`, `description` | Импортировать локальную директорию аудиокниги в последовательную ленту (Podcast 2.0 / Apple Podcasts serial format). Аудиокниги cron не используют. |
| **`delete_audiobook_dir`** | `dir_name` *(обяз)* | Удалить папку аудиокниги и все её файлы с диска (а также отвязать подкаст-ленту). |

### Ресурсы и подсказки (Resources & Prompts)
- **`podsync://feeds`**: Снимок всех лент в формате JSON.
- **`podsync://system`**: Диагностика системы, потребление ресурсов и пути к бинарникам.
- **`podsync://tokens`**: Текущие конфигурации токенов и источники (ENV vs config.toml).
- **`diagnose_podsync`**: Комплексная диагностика экземпляра (недостающие токены, ошибки скачивания, системные утилиты).
- **`summarize_library`**: Сводный отчёт по библиотеке подкастов, занятому пространству и рекомендациям.

---

## 3. Подключение и интеграция с клиентами MCP

### Вариант A: OpenCode v2

Настройка осуществляется в файле `opencode.json` (или `opencode.jsonc`) в корне проекта или глобально в `~/.config/opencode/opencode.json`.

#### Режим Remote SSE (Рекомендуется для работающего сервиса)
```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "remote",
        "url": "http://localhost:8080/mcp/sse",
        "headers": {
          "Authorization": "Basic YWRtaW46c2VjcmV0cGFzc3dvcmQ=" // base64(username:password)
        },
        "enabled": true
      }
    }
  }
}
```

#### Режим Local Stdio
```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "servers": {
      "podsync": {
        "type": "local",
        "command": [
          "/opt/podsync/bin/podsync",
          "--config",
          "/etc/podsync/config.toml",
          "--mcp"
        ],
        "enabled": true
      }
    }
  }
}
```

Проверка списка инструментов в OpenCode:
```bash
opencode mcp list
```

---

### Вариант B: Hermes Agent (Nous Hermes)

В файле `hermes.json` / `settings.json`:

#### Через процесс Stdio
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

#### Через транспорт Remote SSE
```json
{
  "mcpServers": {
    "podsync": {
      "url": "http://127.0.0.1:8080/mcp/sse",
      "headers": {
        "Authorization": "Basic YWRtaW46c2VjcmV0cGFzc3dvcmQ="
      }
    }
  }
}
```

---

### Вариант C: Claude Desktop

Добавьте секцию в конфигурационный файл Claude Desktop:
- **macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "podsync": {
      "command": "/opt/podsync/bin/podsync",
      "args": [
        "--config",
        "/etc/podsync/config.toml",
        "--mcp"
      ]
    }
  }
}
```

---

### Вариант D: Cursor / Cline

В настройках MCP в Cursor или Cline:
- **Name**: `podsync`
- **Type**: `command`
- **Command**: `/opt/podsync/bin/podsync --config /etc/podsync/config.toml --mcp`

---

## 4. Проверка работоспособности

### Тестирование через curl (JSON-RPC)
Проверить работу сервера MCP по протоколу HTTP можно напрямую:

```bash
# Вызов get_sync_status
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get_sync_status","arguments":{}}}'

# Вызов get_download_errors
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_download_errors","arguments":{"limit":5}}}'

# Вызов get_recent_downloads
curl -u admin:secretpassword -X POST http://localhost:8080/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_recent_downloads","arguments":{"limit":5}}}'
```

### Навык для AI-агентов (Agent Skill)
Готовое описание навыка для агентов размещено в:
- [`.agents/skills/podsync/SKILL.md`](../../.agents/skills/podsync/SKILL.md)
