# Podsync (Fork)

<p align="center">
  <img src="docs/img/logo.png" alt="Podsync" width="400">
</p>

<p align="center">
  <a href="README.md">English</a> | <b>Русский</b>
</p>

Podsync — это лёгкий и универсальный сервис-генератор подкастов, который превращает каналы, плейлисты и видео из **YouTube**, **Rutube**, **VK Видео**, **Vimeo**, **SoundCloud**, **Twitch**, а также **локальные аудиокниги** в стандартные RSS-ленты подкастов.

Приложения для прослушивания подкастов предлагают отличный пользовательский опыт: фоновое скачивание новых выпусков, запоминание и синхронизацию позиции воспроизведения между устройствами и офлайн-прослушивание. Podsync переносит все эти преимущества на видеохостинги и вашу личную аудиотеку.

---

## Особенности данного форка

Этот форк расширяет оригинальный проект `mxpv/podsync` множеством современных возможностей:

- **Зеркалирование сторонних RSS-подкастов и предпросмотр**: Зеркалирование любых сторонних RSS-лент подкастов (`http://`, `https://`, `*.xml`) со стриминговым скачиванием медиафайлов напрямую на диск без использования промежуточного буфера в `/tmp` или оперативной памяти. Поддерживается карточка предпросмотра метаданных (обложка, число выпусков, размер) и выбор объёма скачивания **Download Scope** (`Все выпуски`, `Последние 10`, `25`, `50`) с автоматической синхронизацией `page_size` и `keep_last`, предотвращая переполнение диска.
- **Умная транслитерация и валидация Feed ID (URL slug)**: Автоматическая транслитерация кириллицы (русский, украинский, белорусский алфавиты и диакритические знаки) в латинские слаги на лету при вводе и вставке из буфера (`Клан Бобров` → `KLAN_BOBROV`, `ДТКД` → `DTKD`, `Война и мир` → `voyna-i-mir`). Интерактивная подсветка красным цветом (`is-invalid`) при вводе недопустимых символов с живым предпросмотром ссылки (`/CLANBEAVER.xml`) и серверная защита API (`^[a-zA-Z0-9_-]+$`).
- **Адаптивное мобильное меню (Drawer)**: Удобное всплывающее меню навигации для смартфонов (`<= 767px`) с закрытием по клику на пункт меню, клику вне шторки или нажатию клавиши Escape, исключающее наложение элементов на десктопе.
- **Персональные ленты**: Создание независимых пользовательских RSS-лент подкастов прямо в веб-интерфейсе в разделе **LIBRARY → Personal feeds** (с интеграцией в общую вкладку **Podcasts** и бейджем `Personal`). Позволяет вручную добавлять отдельные видео с YouTube, VK Видео, Rutube, прямые ссылки на медиафайлы или загружать локальные файлы до 4 ГиБ с автоматическим перекодированием через `ffmpeg` (Audio в MP3, Video в MP4). Поддерживается выбор формата и качества/разрешения (Audio: High/Low, Video: 480p, 720p HD, 1080p Full HD...), переименование эпизодов с мгновенным обновлением RSS-ленты, копирование между лентами (с подсчётом ссылок на общий медиафайл и новым стабильным GUID), перенос (с сохранением GUID) и безопасное подтверждённое удаление. Подробнее см. в [Руководстве по персональным лентам](./docs/ru/personal-feeds.md).
- **Локальные аудиокниги и автосканирование директорий**: Превращение локальных папок с аудиокнигами (`.mp3`, `.m4b`, `.m4a`, `.aac`, `.ogg`) в сериальные RSS-ленты стандарта Podcast 2.0 (`<itunes:type>serial</itunes:type>`, `<itunes:episode>N</itunes:episode>`). Поддерживается естественная цифровая сортировка глав (`01`, `02`, ..., `10`), автоматическое извлечение обложек и метаданных из ID3/APIC, импорт обнаруженных папок в один клик и прямая загрузка файлов через веб-интерфейс. Карточки аудиокниг в секции Active Audiobooks оформлены в едином чистом стиле `.feed-grid` / `.feed-card`.
- **Поддержка Rutube**: Нативная поддержка каналов, плейлистов, профилей пользователей и отдельных видео (`rutube.ru/channel/...`, `rutube.ru/plst/...`, `rutube.ru/u/...`, `rutube.ru/video/...`) с загрузкой через `yt-dlp` и без необходимости получения API-ключей.
- **Поддержка VK Видео**: Преобразование каналов, сообществ, пользователей и плейлистов VK Видео в подкаст-ленты (`vkvideo.ru`, `vk.com`, `vk.ru`).
- **Панель веб-администрирования (`/admin`)**: Встроенный адаптивный веб-дашборд с боковым меню навигации (Библиотека: Подкасты, Аудиокниги, Активность; Настройки: Ключи API, Загрузчик, Система и хранилище, MCP-сервер), переключением сетки/списка, детальным экраном подкаста с главами/выпусками, поиском, сортировкой, статусами и кнопкой удаления прямо в шапке. Включает загрузку пользовательских обложек с предпросмотром, готовые RSS-ссылки, копирование в 1 клик, QR-коды для смартфона и диплинки Apple Podcasts / Pocket Casts / Overcast. Подробнее см. в [Руководстве по Web Admin](./docs/ru/web_admin.md).
- **Защита от подбора пароля (Brute-Force Protection)**: Защита страницы входа и HTTP Basic Auth скользящим окном лимитов, настраиваемая через веб-интерфейс, переменные окружения или `config.toml` (лимит попыток, окно наблюдения, время блокировки).
- **Системные логи в реальном времени**: Встроенный кольцевой буфер логов в оперативной памяти с автообновлением каждые 2 секунды, поиском по ключевым словам, фильтрацией по уровням (DEBUG, INFO, WARN, ERROR), динамическим переключением уровня логирования на лету и выгрузкой в текстовый файл в 1 клик.
- **Диагностика загрузчика и зависимостей**: Отображение версий и статуса `yt-dlp` и `ffmpeg`, управление файлом `cookies.txt` (просмотр/загрузка/очистка, извлечение из установленных браузеров) для обхода проверок YouTube, настройка HTTP/SOCKS5 прокси и онлайн-тест извлечения видеопотока.
- **Умное извлечение токенов VK**: При добавлении токена VK можно просто вставить целиком URL редиректа из адресной строки браузера — Podsync сам извлечёт токен доступа.
- **Точные метрики диска и активных эпизодов**: На карточках отображаются только фактически скачанные эпизоды и их реальный объём, а очищенные (удалённые по лимиту) выпуски аккуратно отображаются в детальном просмотре ленты.
- **Темы оформления Catppuccin Mocha & Latte**: Палитра Catppuccin в веб-консоли с плавным переключением тёмного и светлого режимов, пастельными бейджами статусов и анимацией.
- **Сервер Model Context Protocol (MCP)**: Встроенный MCP-сервер с 22 инструментами для интеграции с AI-ассистентами (**OpenCode**, **Claude Desktop**, **Cursor**, **Cline**, **Antigravity**) по stdio и SSE, преднастроенный навык ([`.agents/skills/podsync/SKILL.md`](./.agents/skills/podsync/SKILL.md)) и генератор конфигурации. Подробнее см. в [Руководстве по MCP](./docs/ru/mcp.md).
- **Настраиваемый шаблон имён файлов `filename_template`**: Пользовательские имена сохраняемых медиафайлов и пути enclosure в RSS (например, `{{id}}`, `{{title}}`, `{{pub_date}}`).
- **Миграция имён файлов**: CLI-инструмент (`--migrate-filenames`, `--migrate-filenames-dry-run`) для переименования ранее скачанных файлов под новый шаблон.
- **Расширенные переменные окружения**: Удобный запуск в Docker с поддержкой переменных для учётных данных, безопасности, ключей, аудиокниг, прокси и кук.

## Возможности

- Поддержка YouTube, Rutube, VK Видео, Vimeo, SoundCloud, Twitch, локальных аудиокниг и зеркал сторонних RSS-подкастов.
- Настройка лент: видео или аудио, профиль качества, лимит разрешения видео (максимальная высота) и ограничение объёма скачивания.
- Кодирование в mp3 с помощью ffmpeg.
- Прямое потоковое скачивание на диск без переполнения временного каталога `/tmp`.
- Спецификация Podcast 2.0 Serial для аудиокниг с нумерацией глав и отображением встроенных или внешних обложек.
- Расписание обновлений с поддержкой cron-выражений.
- Фильтрация эпизодов (по регулярным выражениям в названии, длительности, возрасту публикации).
- Кастомизация лент (загрузка своих обложек с живым предпросмотром, категория, язык и т.д.).
- Экспорт в OPML.
- Автоматическая очистка старых эпизодов (хранение последних N выпусков).
- Настраиваемые хуки (scripts/webhooks) для интеграций после завершения загрузок.
- Просмотр логов в реальном времени с изменением уровня логирования на лету.
- Встроенная защита от перебора паролей (brute-force rate limiting).
- Работает на Windows, macOS, Linux и Docker.
- Поддержка ARM (arm64, armv7) и x86_64.
- Ротация нескольких API-ключей для обхода квот.

## Системные зависимости

Если вы запускаете Podsync как бинарный файл (не через Docker), убедитесь, что в системе установлены: `yt-dlp`, `ffmpeg` и `go` (для сборки из исходников).

На macOS:
```bash
brew install yt-dlp ffmpeg go
```

На Ubuntu/Debian:
```bash
sudo apt-get update && sudo apt-get install -y ffmpeg
# Установка актуальной версии yt-dlp:
sudo curl -L https://github.com/yt-dlp/yt-dlp/releases/latest/download/yt-dlp -o /usr/local/bin/yt-dlp
sudo chmod a+rx /usr/local/bin/yt-dlp
```

## Документация

- [Руководство по развертыванию для AI-агентов (OpenCode, Hermes)](./docs/ru/deployment_guide_agents.md)
- [Руководство по Web Admin консоли](./docs/ru/web_admin.md)
- [Руководство по локальным аудиокнигам](./docs/ru/audiobooks.md)
- [Руководство по Model Context Protocol (MCP)](./docs/ru/mcp.md)
- [Инструкция: как получить токен VK API](./docs/ru/how_to_get_vk_token.md)
- [Инструкция: как получить YouTube API Key](./docs/ru/how_to_get_youtube_api_key.md)
- [Инструкция: как получить токен Vimeo API](./docs/ru/how_to_get_vimeo_token.md)
- [Установка Podsync на Synology NAS](./docs/ru/how_to_setup_podsync_on_synology_nas.md)
- [Установка Podsync на QNAP NAS](./docs/ru/how_to_setup_podsync_on_qnap_nas.md)
- [Настройка расписания обновлений по cron](./docs/ru/cron.md)
- [Фильтрация выпусков с помощью регулярных выражений](./docs/ru/filters.md)

### Токены доступа

Для обращения к API YouTube, Vimeo или VK Видео необходимо получить токен (Rutube работает напрямую без API-ключей):

- [Как получить токен VK API](./docs/ru/how_to_get_vk_token.md) *(Podsync автоматически извлечёт токен, если вставить URL редиректа целиком)*
- [Как получить ключ YouTube API](https://elfsight.com/blog/2016/12/how-to-get-youtube-api-key-tutorial/)
- [Как создать токен доступа Vimeo](https://developer.vimeo.com/api/guides/start#generate-access-token)

## Конфигурация

Podsync использует конфигурационный файл TOML (например, `config.toml`). Полный перечень параметров смотрите в [config.toml.example](./config.toml.example).

Пример конфигурации:

```toml
[server]
port = 8080
# hostname = "https://podcast.example.com" # Опциональный публичный URL за reverse proxy

[server.admin]
enabled = true
username = "admin"
password = "secretpassword"

[storage]
  [storage.local]
  data_dir = "/app/data/"

[audiobooks]
dir = "/app/data/audiobooks" # Корневая папка для папок с аудиокнигами
supported_extensions = [".mp3", ".m4b", ".m4a", ".aac", ".ogg"]
cover_filenames = ["cover.jpg", "cover.jpeg", "cover.png", "folder.jpg"]

[tokens]
vkvideo = "YOUR_VK_TOKEN"    # Или через переменную PODSYNC_VKVIDEO_API_KEY
youtube = "YOUR_YOUTUBE_KEY" # Или через переменную PODSYNC_YOUTUBE_API_KEY

[feeds]
  # Канал VK Видео
  [feeds.CLANBEAVER]
  url = "https://vkvideo.ru/@clanbeaver"
  page_size = 10
  quality = "high"
  format = "video"

  # Канал Rutube (ключ API не требуется)
  [feeds.RUTUBE_SAMPLE]
  url = "https://rutube.ru/channel/25758171/"
  page_size = 10
  quality = "high"
  format = "audio"

  # Фид локальной аудиокниги
  [feeds.WAR_AND_PEACE]
  url = "audiobook://War_and_Peace" # Указывает на папку /app/data/audiobooks/War_and_Peace
  title = "Война и мир"
  author = "Лев Толстой"
  format = "audio"
```

### Переменные окружения

Основные параметры можно переопределять через переменные окружения:

| Имя переменной | Описание | Пример значения |
|---|---|---|
| `PODSYNC_CONFIG_PATH` | Путь к файлу конфигурации (переопределяет флаг `--config`) | `/app/config.toml` |
| `PODSYNC_ADMIN_ENABLED` | Включение веб-панели управления (`true` или `false`) | `true` |
| `PODSYNC_ADMIN_USERNAME` | Логин администратора веб-панели | `admin` |
| `PODSYNC_ADMIN_PASSWORD` | Пароль администратора веб-панели | `secretpassword` |
| `PODSYNC_ADMIN_MAX_ATTEMPTS` | Максимум неудачных попыток входа до блокировки | `5` |
| `PODSYNC_ADMIN_WINDOW` | Окно наблюдения за неудачными попытками | `5m` |
| `PODSYNC_ADMIN_LOCKOUT` | Длительность блокировки после исчерпания попыток | `15m` |
| `PODSYNC_JOBS_WORKERS` | Число параллельных воркеров очереди загрузок | `2` |
| `PODSYNC_AUDIOBOOKS_DIR` | Корневая директория для поиска аудиокниг | `/app/data/audiobooks` |
| `PODSYNC_VKVIDEO_API_KEY` | Токены VK Видео (через пробел для ротации, алиас: `PODSYNC_VK_API_KEY`) | `vk_token1 vk_token2` |
| `PODSYNC_YOUTUBE_API_KEY` | Ключи YouTube API (через пробел для ротации) | `key1 key2 key3` |
| `PODSYNC_VIMEO_API_KEY` | Ключи Vimeo API (через пробел для ротации) | `key1 key2` |
| `PODSYNC_SOUNDCLOUD_API_KEY` | Ключи SoundCloud API (через пробел) | `key1 key2` |
| `PODSYNC_TWITCH_API_KEY` | Ключи Twitch API (`CLIENT_ID:CLIENT_SECRET`) | `id1:secret1 id2:secret2` |
| `PODSYNC_DOWNLOADER_COOKIES_FILE` | Путь к файлу Netscape `cookies.txt` для yt-dlp | `/app/data/cookies.txt` |
| `PODSYNC_DOWNLOADER_COOKIES_FROM_BROWSER` | Имя браузера для импорта кук (chrome, firefox, safari, brave) | `chrome` или `firefox:Default` |
| `PODSYNC_DOWNLOADER_PROXY` | URL прокси HTTP/HTTPS/SOCKS5 для загрузчика | `socks5://127.0.0.1:1080` |
| `PODSYNC_DOWNLOADER_CUSTOM_BINARY` | Путь к кастомному исполняемому файлу yt-dlp | `/usr/local/bin/yt-dlp` |

## Веб-панель администратора и REST API

Podsync содержит встроенную панель управления, доступную по адресу `/admin`. См. [Руководство по Web Admin](./docs/ru/web_admin.md) для получения подробной информации.

### Эндпоинты REST API

Все эндпоинты администратора поддерживают **HTTP Basic Auth** (`-u username:password`) и cookie сессии браузера:

| Метод | Эндпоинт | Описание |
|---|---|---|
| `GET` | `/health` | Проверка жизнеспособности сервиса (публичный доступ) |
| `GET` | `/api/v1/auth/status` | Текущий статус аутентификации |
| `POST` | `/api/v1/auth/login` | Вход в сессию по логину и паролю |
| `POST` | `/api/v1/auth/logout` | Выход из сессии |
| `POST` | `/api/v1/auth/password` | Смена пароля администратора |
| `GET`, `POST` | `/api/v1/auth/security` | Просмотр и изменение настроек rate limiting защиты от перебора |
| `GET` | `/api/v1/feeds` | Список всех лент со статистикой активных эпизодов и объёмом на диске |
| `POST` | `/api/v1/feeds` | Добавление новой ленты (канал/плейлист/зеркало RSS) с записью в `config.toml` |
| `POST` | `/api/v1/feeds/preview` | Предпросмотр метаданных ленты/RSS-зеркала (название, автор, обложка, число глав, размер) |
| `GET` | `/api/v1/feeds/status` | Быстрый статус лент в памяти (idle, queued, running) |
| `POST` | `/api/v1/feeds/update` | Массовый запуск обновления всех настроенных лент |
| `GET` | `/api/v1/feeds/{id}` | Детальная информация о ленте и статус эпизодов |
| `PUT`, `PATCH` | `/api/v1/feeds/{id}` | Обновление конфигурации ленты и перегенерация XML |
| `DELETE` | `/api/v1/feeds/{id}` | Удаление ленты (`?delete_files=true` для удаления медиафайлов) |
| `POST` | `/api/v1/feeds/{id}/update` | Запуск немедленного обновления ленты и скачивания |
| `POST` | `/api/v1/feeds/{id}/cover` | Привязка загруженной обложки к конкретной ленте |
| `POST` | `/api/v1/covers/upload` | Загрузка файла обложки (JPEG, PNG, WebP) с предпросмотром |
| `POST` | `/api/v1/feeds/{id}/episodes/{ep_id}/retry` | Сброс статуса ошибки для повторного скачивания |
| `GET` | `/api/v1/personal-feeds` | Список всех персональных лент |
| `POST` | `/api/v1/personal-feeds` | Создать новую персональную ленту (`title`, `format`, `cover_art` и др.) |
| `GET` | `/api/v1/personal-feeds/{id}` | Метаданные персональной ленты и список её эпизодов |
| `PATCH` | `/api/v1/personal-feeds/{id}` | Обновить настройки ленты (название, описание, обложка, сортировка) |
| `DELETE` | `/api/v1/personal-feeds/{id}` | Удалить персональную ленту (`?confirm=true&delete_files=true`) |
| `GET` | `/api/v1/personal-feeds/{id}/episodes` | Список эпизодов персональной ленты |
| `POST` | `/api/v1/personal-feeds/{id}/episodes` | Добавить эпизод по ссылке на видео (YouTube, VK, Rutube, прямой URL) |
| `POST` | `/api/v1/personal-feeds/{id}/upload` | Загрузить локальный аудио/видео файл до 4 ГиБ с автоконвертацией |
| `DELETE` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}` | Удалить эпизод из ленты (`?confirm=true&delete_files=true`) |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/copy` | Скопировать эпизод в другую ленту (общий файл, новый GUID) |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/move` | Перенести эпизод в другую ленту (сохраняет GUID) |
| `POST` | `/api/v1/personal-feeds/{id}/episodes/{ep_id}/retry` | Повторить загрузку завершившегося ошибкой эпизода |
| `GET` | `/api/v1/audiobooks/dirs` | Автопоиск доступных папок аудиокниг, количества файлов и обложек |
| `DELETE` | `/api/v1/audiobooks/dirs` | Удаление папки аудиокниги и файлов с диска (`?name=...`) |
| `POST` | `/api/v1/audiobooks/import` | Создание новой подкаст-ленты из обнаруженной папки аудиокниги |
| `POST` | `/api/v1/audiobooks/upload` | Загрузка аудиофайлов и обложки напрямую в новую аудиокнигу |
| `GET` | `/api/v1/audiobooks/config` | Конфигурация модуля аудиокниг и статус корневого каталога |
| `GET` | `/api/v1/system/logs` | Запрос системных логов (параметры `?limit=100&level=INFO&search=...`) |
| `DELETE` | `/api/v1/system/logs` | Очистка буфера логов в памяти |
| `GET` | `/api/v1/system/logs/level` | Текущий активный уровень логирования (`debug`, `info`, `warn`, `error`) |
| `POST` | `/api/v1/system/logs/level` | Динамическое изменение уровня логирования на лету |
| `GET` | `/api/v1/system/logs/download` | Скачивание полной истории логов в виде текстового файла |
| `GET` | `/api/v1/system/errors` | Список ошибок загрузки с диагностикой причин и рекомендациями |
| `GET` | `/api/v1/system/recent` | Список последних успешно загруженных эпизодов |
| `GET` | `/api/v1/system/status` | Сводная панель статуса (расписания синхронизации, загрузки за 24 ч, ошибки) |
| `GET` | `/api/v1/system` | Системные метрики (RAM, аптайм, диск, версии yt-dlp и ffmpeg) |
| `GET` | `/api/v1/tokens` | Получение маскированных токенов API всех провайдеров |
| `POST` | `/api/v1/tokens` | Обновление токенов API (с поддержкой ротации и парсинга ссылок) |
| `DELETE` | `/api/v1/tokens` | Удаление токена по провайдеру и индексу (`?provider=...&index=...`) |
| `GET` | `/api/v1/downloader` | Настройки yt-dlp/ffmpeg, статус кук и прокси |
| `POST` | `/api/v1/downloader` | Обновление настроек загрузчика (таймаут, прокси, браузер, автоапдейт) |
| `GET` | `/api/v1/downloader/cookies` | Просмотр содержимого Netscape `cookies.txt` |
| `POST` | `/api/v1/downloader/cookies` | Сохранение или загрузка нового файла `cookies.txt` |
| `DELETE` | `/api/v1/downloader/cookies` | Удаление установленного файла кук |
| `POST` | `/api/v1/downloader/test` | Тестирование извлечения потока через yt-dlp с куками/прокси |
| `GET` | `/api/v1/mcp/config` | Параметры подключения к серверу MCP для AI-агентов |

## Сервер Model Context Protocol (MCP)

Podsync содержит встроенный **MCP Server** с 28 инструментами, позволяющий AI-ассистентам (**OpenCode**, **Claude Desktop**, **Cursor**, **Cline**, **Antigravity**) напрямую проверять и настраивать ленты, запускать загрузки, перезапускать сбойные эпизоды, импортировать аудиокниги, читать логи и отслеживать состояние системы.

Готовый навык для агентов расположен в [`.agents/skills/podsync/SKILL.md`](./.agents/skills/podsync/SKILL.md). Подробности см. в [Руководстве по MCP](./docs/ru/mcp.md).

### Инструменты MCP:
- **Управление лентами**: `list_feeds`, `get_feed`, `add_feed`, `delete_feed`, `update_feed`, `update_all_feeds`, `update_feed_config`, `retry_episode`
- **Персональные ленты**: `list_personal_feeds`, `get_personal_feed`, `create_personal_feed`, `add_personal_episode`, `delete_personal_episode`, `rename_personal_episode`
- **Аудиокниги**: `list_audiobook_dirs`, `import_audiobook`, `delete_audiobook_dir`
- **Система и логи**: `get_system_stats`, `get_sync_status`, `get_recent_downloads`, `get_download_errors`, `get_system_logs`
- **Загрузчик и диагностика**: `get_downloader_config`, `update_downloader_config`, `test_downloader`
- **Токены и ключи**: `get_tokens`, `update_tokens`, `delete_tokens`

### Варианты запуска MCP:
- **Режим Stdio (Claude Desktop, Cursor)**:
  ```bash
  ./bin/podsync --config config.toml --mcp
  ```
  *(Автоматически проксирует запросы к работающему серверу Podsync или обращается напрямую к локальной БД).*
- **Режим HTTP / SSE**: Доступен по путям `/mcp` и `/mcp/sse` при работающем веб-сервере.

## Запуск приложения

### Готовые бинарные сборки:

Скачайте релизный архив для вашей платформы со страницы [GitHub Releases](https://github.com/tatarinovms/podsync/releases):

```bash
# Автоматический скрипт установки для Linux (x86_64 / arm64)
curl -sSL https://raw.githubusercontent.com/tatarinovms/podsync/main/docs/deployment_guide_agents.md
```

### Сборка из исходников:

```bash
make
./bin/podsync --config config.toml

# Дополнительные флаги запуска:
# ./bin/podsync --config config.toml --admin     # Принудительно включить веб-панель (/admin)
# ./bin/podsync --config config.toml --no-admin  # Принудительно отключить веб-панель (/admin)
# ./bin/podsync --config config.toml --mcp       # Запуск в режиме MCP-сервера через stdio
```

### Миграция имён файлов

Если вы изменили шаблон `filename_template` и хотите переименовать уже скачанные файлы:

```bash
./bin/podsync --config config.toml --migrate-filenames
```

Режим предпросмотра (без физического переименования):

```bash
./bin/podsync --config config.toml --migrate-filenames --migrate-filenames-dry-run
```

### Запуск через Docker:

```bash
docker run \
    -p 8080:8080 \
    -v $(pwd)/data:/app/data/ \
    -v $(pwd)/db:/app/db/ \
    -v $(pwd)/config.toml:/app/config.toml \
    ghcr.io/tatarinovms/podsync:latest
```

Или локальная сборка образа:
```bash
make docker
```

### Запуск через Docker Compose:

```yaml
services:
  podsync:
    container_name: podsync
    build: .
    image: ghcr.io/tatarinovms/podsync:latest
    restart: unless-stopped
    ports:
      - 8080:8080
    volumes:
      - ./data:/app/data/
      - ./db:/app/db/
      - ./config.toml:/app/config.toml
```

```bash
docker compose up -d
```

## Лицензия

Проект распространяется под лицензией MIT — подробности в файле [LICENSE](LICENSE).
