# Руководство по установке Podsync на Synology NAS

[English](../how_to_setup_podsync_on_synology_nas.md) | **Русский**

*Автор оригинала: [@lucasjanin](https://github.com/lucasjanin)*

Инструкция описывает установку Podsync на Synology NAS с поддержкой SSL/HTTPS.
Для работы потребуется настроенный домен с DDNS и действующий SSL-сертификат (например, бесплатный сертификат Let's Encrypt через Synology DSM).

1. Откройте «Центр пакетов» (Package Center) и установите Docker (или Container Manager на DSM 7.2+).
2. Создайте папку для медиафайлов и базы данных (например, `/volume1/docker/podsync/data`).
3. Создайте папку для файла конфигурации (например, `/volume1/docker/podsync/config`).
4. Создайте файл `config.toml` и сохраните его в созданную папку конфигурации. Пример:

```toml
[server]
port = 9090
hostname = "https://my-nas.synology.me"

[tokens]
youtube = "YOUR_YOUTUBE_API_KEY"

[feeds]
  [feeds.MY_FEED]
  url = "https://www.youtube.com/channel/UCJldRgT_D7Am-ErRHQZ90uw"
  update_period = "1h"
  quality = "high"
  format = "audio"
  clean = { keep_last = 20 }
```

5. Подключитесь к Synology по SSH и загрузите Docker-образ:

```bash
docker pull ghcr.io/tatarinovms/podsync:latest
```

6. Запустите контейнер:

```bash
docker run -d \
    --name podsync \
    --restart unless-stopped \
    -p 9090:9090 \
    -v /volume1/docker/podsync/data:/app/data/ \
    -v /volume1/docker/podsync/config/config.toml:/app/config.toml \
    ghcr.io/tatarinovms/podsync:latest
```

7. **Настройка Reverse Proxy в Synology DSM (для работы по HTTPS/443):**
   - Перейдите в *Панель управления* -> *Портал входа* (Login Portal) -> *Дополнительно* -> *Обратный прокси-сервер* (Reverse Proxy).
   - Создайте правило:
     - **Источник:** Протокол: `HTTPS`, Имя хоста: `my-nas.synology.me`, Порт: `443` (или поддомен/путь).
     - **Назначение:** Протокол: `HTTP`, Имя хоста: `localhost`, Порт: `9090`.
   - В разделе *Пользовательский заголовок* добавьте заголовки WebSocket (Upgrade, Connection) для корректной работы живых логов в Web Admin.

8. Откройте `https://my-nas.synology.me/admin/` в браузере или скопируйте ссылку на ленту (например, `https://my-nas.synology.me/MY_FEED.xml`) в подкаст-плеер на смартфоне.
