# Как получить ключ YouTube API

[English](../how_to_get_youtube_api_key.md) | **Русский**

1. Откройте Google Cloud Console: https://console.developers.google.com
2. Нажмите `Select a project` (Выбрать проект).
![Выбор проекта](../img/youtube_select_project.png)
3. Нажмите `New project` (Новый проект).
![Новый проект](../img/youtube_new_project.png)
4. Укажите название и нажмите `Create`.
![Панель управления](../img/youtube_dashboard.png)
5. Откройте `Library` (Библиотека), найдите и выберите `YouTube Data API v3`.
![YouTube Data API](../img/youtube_data_api_v3.png)
6. Нажмите `Enable` (Включить).
![Включение API](../img/youtube_data_api_enable.png)
7. Перейдите в раздел `Credentials` (Учётные данные).
8. Нажмите `Create credentials` -> `API key`.
![Создание API key](../img/youtube_create_api_key.png)
9. Скопируйте ключ в файл `config.toml` или укажите в переменной окружения:
![Копирование токена](../img/youtube_copy_token.png)
```toml
[tokens]
youtube = "key1"
```
Или через переменную окружения:
```sh
export PODSYNC_YOUTUBE_API_KEY="key1"
```

Для автоматической ротации ключей можно указать несколько ключей через пробел:
```sh
export PODSYNC_YOUTUBE_API_KEY="key1 key2"
```
