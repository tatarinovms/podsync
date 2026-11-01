# Мониторинг Podsync в Uptime Kuma

[English](../monitoring_uptime_kuma.md) | **Русский**

В этом руководстве описана настройка автоматического мониторинга и оповещений для **Podsync** с помощью [Uptime Kuma](https://github.com/louislam/uptime-kuma). Эти проверки позволят моментально получать уведомления в **Telegram**, **Discord**, **Gotify** или Slack, если:
- Сервер Podsync или Docker-контейнер стал недоступен.
- Встроенная база данных Badger заблокирована или повреждена.
- Загрузка видео сбоит (например, сработала защита YouTube от ботов, истекли `cookies.txt` или включились гео-ограничения VK/Rutube).
- RSS-лента подкаста перестала отдаваться мобильным плеерам.

---

## 1. Обзор эндпоинтов здоровья и диагностики

Podsync предоставляет как публичные легковесные проверки, так и защищённые диагностические API:

| Эндпоинт | Авторизация | Назначение | Коды ответа и тело | Рекомендуемый монитор |
|---|---|---|---|---|
| `GET /health/ready` | **Нет** (Публичный) | **Готовность базы данных и сервиса**. Проверяет, что BadgerDB разблокирована и отвечает на запросы. Не зависит от временных ошибок загрузки отдельных видео. | `200 OK`: `{"status":"ready","timestamp":"..."}`<br>`503 Service Unavailable`: `{"status":"unhealthy","error":"database unavailable"}` | **Рецепт 1** (`HTTP(s)` или `Keyword`) |
| `GET /health/live` | **Нет** (Публичный) | **Liveness процесса**. Проверяет, что процесс веб-сервера жив и слушает TCP-порт. | `200 OK`: `{"status":"ok","timestamp":"..."}` | Liveness-проба для Kubernetes / Docker |
| `GET /health` | **Нет** (Публичный) | **Агрегированный статус**. Возвращает `503`, если база данных недоступна ИЛИ если за последние 24 часа был хотя бы один сбой загрузки. | `200 OK`: `{"status":"healthy"}`<br>`503 Service Unavailable`: `{"status":"unhealthy","failed_episodes":N}` | Простой бинарный триггер |
| `GET /api/v1/system/status` | **HTTP Basic Auth** (`admin`) | **Синхронизация и количество ошибок**. Возвращает статистику активных лент, загруженных выпусков, число ошибок загрузок, свободное место на диске и статус yt-dlp/ffmpeg. | `200 OK`: JSON `SyncStatusInfo` (см. ниже) | **Рецепт 2** (`HTTP(s) - JSON Query`) |
| `GET /api/v1/system` | **HTTP Basic Auth** (`admin`) | **Метрики среды выполнения**. Горутины, потребление памяти в МБ, дисковое пространство, аптайм, версия. | `200 OK`: JSON `SystemStats` | Пользовательская телеметрия |
| `GET /<FEED_ID>.xml` | **Нет** (Публичный) | **Раздача RSS подкаста**. Проверяет, что плееры подкастов успешно получают XML ленты. | `200 OK`: Валидный XML-документ с закрывающим тегом `</rss>` | **Рецепт 3** (`Keyword`) |

### Пример ответа `GET /api/v1/system/status`

```json
{
  "active_feeds": 4,
  "total_episodes": 142,
  "downloaded_episodes": 142,
  "failed_episodes": 0,
  "downloaded_past_24h": 6,
  "disk_free_bytes": 107374182400,
  "disk_total_bytes": 500107862016,
  "has_downloader": true,
  "has_ffmpeg": true,
  "feeds": [
    {
      "feed_id": "tech_podcast",
      "feed_title": "Tech Podcast",
      "url": "https://www.youtube.com/channel/...",
      "provider": "youtube",
      "cron_schedule": "0 */2 * * *",
      "episode_count": 45,
      "downloaded_count": 45,
      "error_count": 0
    }
  ]
}
```

---

## 2. Рецепт 1: Основной сервис и готовность БД (Рекомендуется)

Основной монитор доступности инстанса. Опрашивает эндпоинт `/health/ready` и проверяет, что сервер работает, а база Badger доступна, не генерируя ложных тревог при единичных ошибках загрузки видео.

### Настройки в Uptime Kuma

| Поле | Значение | Пояснение |
|---|---|---|
| **Тип монитора** | `HTTP(s)` или `HTTP(s) - Keyword` | Режим Keyword дополнительно проверяет содержимое ответа |
| **Имя (Friendly Name)** | `Podsync - Core Service` | |
| **URL** | `https://podsync.example.com/health/ready` | Или `http://<LAN_IP>:8080/health/ready` |
| **Интервал проверки** | `60` секунд | |
| **Повторные попытки (Retries)** | `2` | Защита от кратковременных сетевых задержек |
| **Ключевое слово (Keyword)** | `ready` | Проверяет `"status":"ready"` в теле ответа |
| **Ожидаемые статус-коды** | `200-299` | По умолчанию в Uptime Kuma |

> [!TIP]
> Для общего индикатора аптайма используйте именно `/health/ready`, а не `/health`. Если на YouTube выйдет ролик с региональной блокировкой, `/health` вернет `503`, тогда как `/health/ready` сохранит `200 OK` — график аптайма не будет ложно проседать.

---

## 3. Рецепт 2: Ошибки загрузок и устаревшие куки (`HTTP(s) - JSON Query`)

Когда YouTube ужесточает алгоритмы проверки ботов или истекает загруженный `cookies.txt`, `yt-dlp` перестает скачивать новые выпуски. Podsync увеличивает счетчик `failed_episodes` в ответе `/api/v1/system/status`.

С помощью монитора **HTTP(s) - JSON Query** в Uptime Kuma можно получать алерт, как только `failed_episodes > 0`.

### Настройки в Uptime Kuma

| Поле | Значение | Пояснение |
|---|---|---|
| **Тип монитора** | `HTTP(s) - JSON Query` | Доступен в Uptime Kuma v1.22+ |
| **Имя (Friendly Name)** | `Podsync - Ошибки загрузок и Cookies` | |
| **URL** | `https://podsync.example.com/api/v1/system/status` | Защищённый эндпоинт |
| **Интервал проверки** | `120` или `300` секунд | 2–5 минут оптимально |
| **Повторные попытки (Retries)** | `1` | Быстрое оповещение о проблеме |
| **Метод аутентификации** | `HTTP Basic Auth` | |
| **Имя пользователя** | Имя администратора Podsync | Из `config.toml` или переменной `PODSYNC_ADMIN_USERNAME` |
| **Пароль** | Пароль администратора Podsync | Из `config.toml` или переменной `PODSYNC_ADMIN_PASSWORD` |
| **JSON Query** | `$.failed_episodes` | JSONPath-выражение для извлечения числа ошибок |
| **Ожидаемое значение** | `0` | Монитор в статусе UP, пока `failed_episodes == 0` |

### Защита от перебора паролей (Brute-Force Rate Limiting)

> [!WARNING]
> В Podsync встроен ограничитель попыток входа: **10 неудачных попыток аутентификации в течение 15 минут приводят к блокировке IP на 30 минут** (HTTP `429 Too Many Requests`).
> 
> При настройке в Uptime Kuma:
> 1. Внимательно проверьте логин и пароль перед сохранением монитора.
> 2. Убедитесь, что запрос возвращает `200 OK`. При успешном входе счетчик ошибок сразу сбрасывается в 0.
> 3. Если Uptime Kuma обращается к Podsync через reverse proxy (Nginx, Caddy, Traefik), настройте корректную передачу заголовков `X-Forwarded-For` или `X-Real-IP`, чтобы не заблокировать сам прокси.

---

## 4. Рецепт 3: Проверка отдачи RSS-ленты

Проверяет генерацию XML-ленты и её доступность для мобильных приложений подкастов (Apple Podcasts, Pocket Casts, AntennaPod, Overcast).

### Настройки в Uptime Kuma

| Поле | Значение | Пояснение |
|---|---|---|
| **Тип монитора** | `HTTP(s) - Keyword` | |
| **Имя (Friendly Name)** | `Podsync - RSS (<FEED_ID>)` | Например, `Podsync - RSS (my_channel)` |
| **URL** | `https://podsync.example.com/<FEED_ID>.xml` | Например, `https://podsync.example.com/tech_podcast.xml` |
| **Интервал проверки** | `300` секунд (5 минут) | |
| **Ключевое слово (Keyword)** | `</rss>` | Проверяет наличие корректного закрывающего тега RSS 2.0 |
| **Инвертировать (Invert)** | `Выключено` | |

---

## 5. Рецепт 4: Монитор контейнера Docker и Healthcheck в `docker-compose.yml`

Если Uptime Kuma запущена на том же хосте (или имеет доступ к Docker Socket), контейнер Podsync можно отслеживать напрямую.

### 5.1 Добавление healthcheck в `docker-compose.yml`

В официальном контейнере Podsync (на базе Alpine Linux) утилита `wget` доступна по умолчанию. Добавьте блок `healthcheck` в сервис:

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
    healthcheck:
      test: ["CMD-SHELL", "wget --spider -q http://127.0.0.1:8080/health/ready || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

Примените изменения:
```bash
docker compose up -d
```

Проверьте статус здоровья:
```bash
docker ps --filter "name=podsync" --format "{{.Names}}: {{.Status}}"
# Вывод: podsync: Up 5 minutes (healthy)
```

### 5.2 Настройка монитора Docker в Uptime Kuma

| Поле | Значение | Пояснение |
|---|---|---|
| **Тип монитора** | `Docker Container` | |
| **Имя (Friendly Name)** | `Podsync - Контейнер Docker` | |
| **Docker Daemon** | `Local Docker Socket` (`/var/run/docker.sock`) | Или удаленный сокет через TCP |
| **Container Name / ID** | `podsync` | Имя контейнера |
| **Интервал проверки** | `30` секунд | |

---

## 6. Готовый пресет для импорта в Uptime Kuma (JSON)

Вы можете скопировать этот JSON и импортировать его в Uptime Kuma через **Настройки → Резервное копирование → Импорт**:

```json
{
  "version": "1.23.0",
  "notificationList": [],
  "monitorList": [
    {
      "name": "Podsync - Core Service & DB Readiness",
      "type": "http",
      "url": "https://podsync.example.com/health/ready",
      "interval": 60,
      "retryInterval": 30,
      "maxretries": 2,
      "accepted_statuscodes_json": "[\"200-299\"]"
    },
    {
      "name": "Podsync - Download Errors & Cookies Alert",
      "type": "json-query",
      "url": "https://podsync.example.com/api/v1/system/status",
      "interval": 180,
      "retryInterval": 60,
      "maxretries": 1,
      "authMethod": "basic",
      "basic_auth_user": "admin",
      "basic_auth_pass": "CHANGE_ME_TO_ADMIN_PASSWORD",
      "jsonPath": "$.failed_episodes",
      "expectedValue": "0"
    },
    {
      "name": "Podsync - RSS Feed Delivery",
      "type": "keyword",
      "url": "https://podsync.example.com/YOUR_FEED_ID.xml",
      "interval": 300,
      "retryInterval": 60,
      "maxretries": 2,
      "keyword": "</rss>"
    }
  ]
}
```

> [!NOTE]
> После импорта замените `podsync.example.com`, `CHANGE_ME_TO_ADMIN_PASSWORD` и `YOUR_FEED_ID` на реальные параметры вашего инстанса.

---

## 7. Настройка каналов оповещений

В Uptime Kuma (**Настройки → Уведомления**) рекомендуется подключить:

### Telegram
- **Bot Token**: создаётся через [@BotFather](https://t.me/BotFather).
- **Chat ID**: ID вашего профиля или приватного канала (узнать через [@userinfobot](https://t.me/userinfobot)).
- **Шаблон сообщения**:
  ```text
  ⚠️ [{{MONITOR_NAME}}] перешёл в статус {{STATUS}}!
  Причина: {{MSG}}
  Цель: {{TARGET}}
  ```

### Gotify / Ntfy
- Отличный выбор для локальных пуш-уведомлений на смартфон без сторонних облачных сервисов.

### Discord / Slack
- Создайте входящий вебхук (Incoming Webhook) в целевом канале сервера и укажите его URL в настройках Uptime Kuma.
