# How to get VK API token

ВКонтакте убрал создание standalone-приложений из новой панели разработчиков. Поэтому получить токен доступа пользователя (`video`, `offline`) можно напрямую через официальный OAuth ВКонтакте:

> [!NOTE]
> Авторизация происходит с идентификатором `client_id=2685278`, который принадлежит известному клиенту **Kate Mobile**. На экране подтверждения ВКонтакте будет отображаться запрос прав для приложения «Kate Mobile».

1. Откройте в браузере следующую ссылку авторизации:
   ```text
   https://oauth.vk.com/authorize?client_id=2685278&scope=video,offline&redirect_uri=https://oauth.vk.com/blank.html&response_type=token
   ```

2. Нажмите кнопку **«Разрешить»** (выдаются права на доступ к видеозаписям и бессрочный доступ `offline`).

3. Браузер перенаправит вас на пустую страницу. В адресной строке появится URL вида:
   ```text
   https://oauth.vk.com/blank.html#access_token=vk1.a.XXXXXXXXXXXX...&expires_in=0&user_id=12345678
   ```

4. Скопируйте значение `access_token` (начиная с `vk1.a...` и до `&expires_in=0`).

5. Вставьте полученный токен в `config.toml`:
   ```toml
   [tokens]
   vkvideo = "vk1.a.XXXXXXXXXXXX..."
   ```
   Или задайте через переменную окружения:
   ```sh
   export PODSYNC_VKVIDEO_API_KEY="vk1.a.XXXXXXXXXXXX..."
   ```
