# Как получить токен Vimeo API

[English](../how_to_get_vimeo_token.md) | **Русский**

1. Зарегистрируйте аккаунт на https://vimeo.com
2. Перейдите в портал разработчиков: https://developer.vimeo.com
3. Нажмите кнопку `New app`.
![Создание приложения](../img/vimeo_create_app.png)
4. Заполните форму и нажмите `Create App`.
5. Перейдите в раздел [Generate an access token](https://developer.vimeo.com/apps/160740#generate_access_token).
![Генерация токена доступа](../img/vimeo_access_token.png)
6. Нажмите `Generate`.
![Токены](../img/vimeo_token.png)
7. Скопируйте полученный токен в файл конфигурации `config.toml` или задайте через переменную окружения:
```toml
[tokens]
vimeo = "key1"
```
Или через переменную окружения:
```sh
export PODSYNC_VIMEO_API_KEY="key1"
```

Для ротации токенов можно указать несколько ключей через пробел:
```sh
export PODSYNC_VIMEO_API_KEY="key1 key2"
```
