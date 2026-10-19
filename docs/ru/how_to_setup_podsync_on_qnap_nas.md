# Руководство по установке Podsync на QNAP NAS

[English](../how_to_setup_podsync_on_qnap_nas.md) | **Русский**

*Автор оригинала: [@Rumik](https://github.com/Rumik)*

1. Установите **Container Station** из App Center на вашем QNAP.
2. Создайте общую папку на QNAP, где Podsync будет хранить файлы конфигурации и данные (например, `/share/CACHEDEV1_DATA/appdata/podsync`).
3. Создайте файл `config.toml` в любом текстовом редакторе и скопируйте его в созданную папку. Пример конфигурации:

```toml
[server]
port = 6969
data_dir = "/share/CACHEDEV1_DATA/appdata/podsync"
hostname = "http://my.customhostname.com:6969"

[tokens]
youtube = "INSERTYOUTUBEAPI" # Ваш YouTube API key

[feeds]
  [feeds.KFGD] # Kinda Funny Games Daily
  url = "youtube.com/playlist?list=PLy3mMHt2i7RIl9pkdvrA98kN-RD4yoRhv"
  page_size = 3
  update_period = "60m"
  quality = "high"
  format = "video"
  cover_art = "http://i1.sndcdn.com/avatars-000319281278-0merek-original.jpg"
```

*Примечание:* Порт `6969` взят для примера, так как стандартный `8080` может быть занят другими сервисами. Также указан собственный hostname для доступа снаружи домашней сети (для этого потребуется проброс порта на роутере).

4. Подключитесь к QNAP по SSH (например, через терминал или PuTTY).
5. Загрузите актуальный Docker-образ:

```bash
docker pull ghcr.io/tatarinovms/podsync:latest
```

6. Запустите контейнер Podsync:

```bash
docker run -d \
    --name podsync \
    --restart unless-stopped \
    -p 6969:6969 \
    -v /share/CACHEDEV1_DATA/appdata/podsync:/app/data/ \
    -v /share/CACHEDEV1_DATA/appdata/podsync/config.toml:/app/config.toml \
    ghcr.io/tatarinovms/podsync:latest
```

Podsync запустится в фоновом режиме, прочитает `config.toml` и начнет обработку фидов.

7. В Container Station убедитесь, что для контейнера включён автозапуск (Auto Start).
8. После загрузки выпусков перейдите в веб-браузере по адресу фида (например, `http://my.customhostname.com:6969/KFGD.xml`) или в веб-интерфейс `http://my.customhostname.com:6969/admin/`.
Скопируйте URL ленты и добавьте в любимое приложение для подкастов.

Если у вас динамический IP-адрес, воспользуйтесь DDNS-сервисом (встроенным myQNAPcloud или сторонними DuckDNS / No-IP).
