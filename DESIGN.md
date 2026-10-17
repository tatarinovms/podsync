# 🎨 Design System & UI Guidelines: Catppuccin (Latte & Mocha)

> **Инструкция для AI-агента:** Этот документ является единственным источником правды (Single Source of Truth) для визуального оформления проекта. При генерации любых UI-компонентов, страниц и стилей ты **обязан** строго следовать палитре и правилам Catppuccin, описанным ниже. Никогда не используй стандартные цвета (например, чистый `#000000`, `#ffffff` или дефолтные цвета фреймворков вроде `bg-blue-500`, `text-gray-500`), всегда заменяй их на токены Catppuccin.

---

## 1. Философия дизайна (Aesthetic Direction)

* **Стиль:** Пастельный, уютный, с мягким контрастом (Soothing pastel theme). Отсутствие резких неоновых или чисто черных/белых оттенков.
* **Темы:** Приложение должно поддерживать два режима:
  * 🌻 **Catppuccin Latte** — светлая тема (по умолчанию для светлого режима).
  * 🌿 **Catppuccin Mocha** — глубокая темная тема (по умолчанию для темного режима).
* **Формы и линии:** Мягкие скругления (`rounded-xl`, `rounded-2xl`), тонкие ненавязчивые рамки (`1px solid`), деликатные тени в светлой теме и акцент на разнице оттенков поверхностей (Surfaces) в темной теме.

---

## 2. Цветовая палитра (Color Tokens)

Используй исключительно эти HEX-значения.

### А. Базовые цвета, фоны и текст (Backgrounds, Surfaces & Typography)

| Токен | Роль в UI | 🌻 Latte (Light) | 🌿 Mocha (Dark) |
| :--- | :--- | :--- | :--- |
| **Base** | Основной фон страницы (`body`) | `#eff1f5` | `#1e1e2e` |
| **Mantle** | Вторичный фон (сайдбары, футеры, код-блоки) | `#e6e9ef` | `#181825` |
| **Crust** | Самый глубокий фон (подложки, разделители, оверлеи) | `#dce0e8` | `#11111b` |
| **Surface 0** | Карточки (Cards), инпуты, неактивные кнопки | `#ccd0da` | `#313244` |
| **Surface 1** | Состояние `:hover` для карточек, выпадающие меню | `#bcc0cc` | `#45475a` |
| **Surface 2** | Активные элементы, бордеры (рамки), бейджи | `#acb0be` | `#585b70` |
| **Overlay 0** | Приглушенные иконки, плейсхолдеры (`placeholder`) | `#9ca0b0` | `#6c7086` |
| **Overlay 1** | Неактивный текст (`disabled`), скроллбары | `#8c8fa1` | `#7f849c` |
| **Overlay 2** | Второстепенные иконки, декоративные линии | `#7c7f93` | `#9399b2` |
| **Subtext 0** | Мелкий текст, подписи, мета-информация, копирайт | `#6c6f85` | `#a6adc8` |
| **Subtext 1** | Подзаголовки, описания, вторичный текст | `#5c5f77` | `#bac2de` |
| **Text** | Основной текст, заголовки H1-H6 | `#4c4f69` | `#cdd6f4` |

### Б. Акцентные и семантические цвета (Accents & Semantics)

| Токен | Рекомендуемое применение в UI | 🌻 Latte (Light) | 🌿 Mocha (Dark) |
| :--- | :--- | :--- | :--- |
| **Mauve** | **Primary Accent** (Главные CTA-кнопки, активные табы) | `#8839ef` | `#cba6f7` |
| **Blue** | **Secondary Accent** (Ссылки, информационные бейджи, фокус) | `#1e66f5` | `#89b4fa` |
| **Lavender**| Акценты заголовков, градиенты (в паре с Mauve/Blue) | `#7287fd` | `#b4befe` |
| **Sapphire**| Иконки, интерактивные элементы, ховеры ссылок | `#209fb5` | `#74c7ec` |
| **Sky** | Теги, декоративные элементы | `#04a5e5` | `#89dceb` |
| **Teal** | Вспомогательные акценты, индикаторы прогресса | `#179299` | `#94e2d5` |
| **Green** | **Success** (Успешные действия, статусы «Онлайн» / «Готово») | `#40a02b` | `#a6e3a1` |
| **Yellow** | **Warning** (Предупреждения, рейтинги, важные заметки) | `#df8e1d` | `#f9e2af` |
| **Peach** | Уведомления, бейджи «Новое» / «Популярное» | `#fe640b` | `#fab387` |
| **Maroon** | Вторичные ошибки, удаление некритичных данных | `#e64553` | `#eba0ac` |
| **Red** | **Danger / Error** (Ошибки валидации, кнопки удаления) | `#d20f39` | `#f38ba8` |
| **Pink** | Креативные акценты, лайки, избранное | `#ea76cb` | `#f5c2e7` |
| **Flamingo**| Декоративные иллюстрации, аватары | `#dd7878` | `#f2cdcd` |
| **Rosewater**| Мягкое свечение, курсоры, тонкие акценты | `#dc8a78` | `#f5e0dc` |

---

## 3. Техническая реализация (CSS Variables & Tailwind)

При создании проекта сразу внедряй эти CSS-переменные в глобальный файл стилей (`globals.css` / `index.css`). Переключение темы должно происходить через класс `.dark` (или `data-theme="mocha"`) на теге `<html>`.

```css
:root {
  /* Catppuccin Latte (Light Mode Default) */
  --ctp-rosewater: #dc8a78;
  --ctp-flamingo: #dd7878;
  --ctp-pink: #ea76cb;
  --ctp-mauve: #8839ef;
  --ctp-red: #d20f39;
  --ctp-maroon: #e64553;
  --ctp-peach: #fe640b;
  --ctp-yellow: #df8e1d;
  --ctp-green: #40a02b;
  --ctp-teal: #179299;
  --ctp-sky: #04a5e5;
  --ctp-sapphire: #209fb5;
  --ctp-blue: #1e66f5;
  --ctp-lavender: #7287fd;
  --ctp-text: #4c4f69;
  --ctp-subtext1: #5c5f77;
  --ctp-subtext0: #6c6f85;
  --ctp-overlay2: #7c7f93;
  --ctp-overlay1: #8c8fa1;
  --ctp-overlay0: #9ca0b0;
  --ctp-surface2: #acb0be;
  --ctp-surface1: #bcc0cc;
  --ctp-surface0: #ccd0da;
  --ctp-base: #eff1f5;
  --ctp-mantle: #e6e9ef;
  --ctp-crust: #dce0e8;
}

.dark, [data-theme="mocha"] {
  /* Catppuccin Mocha (Dark Mode) */
  --ctp-rosewater: #f5e0dc;
  --ctp-flamingo: #f2cdcd;
  --ctp-pink: #f5c2e7;
  --ctp-mauve: #cba6f7;
  --ctp-red: #f38ba8;
  --ctp-maroon: #eba0ac;
  --ctp-peach: #fab387;
  --ctp-yellow: #f9e2af;
  --ctp-green: #a6e3a1;
  --ctp-teal: #94e2d5;
  --ctp-sky: #89dceb;
  --ctp-sapphire: #74c7ec;
  --ctp-blue: #89b4fa;
  --ctp-lavender: #b4befe;
  --ctp-text: #cdd6f4;
  --ctp-subtext1: #bac2de;
  --ctp-subtext0: #a6adc8;
  --ctp-overlay2: #9399b2;
  --ctp-overlay1: #7f849c;
  --ctp-overlay0: #6c7086;
  --ctp-surface2: #585b70;
  --ctp-surface1: #45475a;
  --ctp-surface0: #313244;
  --ctp-base: #1e1e2e;
  --ctp-mantle: #181825;
  --ctp-crust: #11111b;
}

body {
  background-color: var(--ctp-base);
  color: var(--ctp-text);
  transition: background-color 0.25s ease, color 0.25s ease;
}
```

### Интеграция с Tailwind CSS
Если в проекте используется Tailwind, используй пакет `@catppuccin/tailwindcss` или маппинг цветов в `tailwind.config.js`:

```javascript
colors: {
  ctp: {
    base: 'var(--ctp-base)',
    mantle: 'var(--ctp-mantle)',
    crust: 'var(--ctp-crust)',
    surface0: 'var(--ctp-surface0)',
    surface1: 'var(--ctp-surface1)',
    surface2: 'var(--ctp-surface2)',
    overlay0: 'var(--ctp-overlay0)',
    overlay1: 'var(--ctp-overlay1)',
    overlay2: 'var(--ctp-overlay2)',
    subtext0: 'var(--ctp-subtext0)',
    subtext1: 'var(--ctp-subtext1)',
    text: 'var(--ctp-text)',
    mauve: 'var(--ctp-mauve)',
    blue: 'var(--ctp-blue)',
    lavender: 'var(--ctp-lavender)',
    sapphire: 'var(--ctp-sapphire)',
    sky: 'var(--ctp-sky)',
    teal: 'var(--ctp-teal)',
    green: 'var(--ctp-green)',
    yellow: 'var(--ctp-yellow)',
    peach: 'var(--ctp-peach)',
    maroon: 'var(--ctp-maroon)',
    red: 'var(--ctp-red)',
    pink: 'var(--ctp-pink)',
    flamingo: 'var(--ctp-flamingo)',
    rosewater: 'var(--ctp-rosewater)',
  }
}
```

---

## 4. Правила построения UI-компонентов (Component Rules)

### ⚠️ КРИТИЧЕСКОЕ ПРАВИЛО КОНТРАСТНОСТИ (Кнопки и Бейджи)
Акцентные цвета в Latte и Mocha имеют разную светлоту:
* В **Latte** акцентные цвета (например, `Mauve #8839ef` или `Blue #1e66f5`) достаточно насыщенные, поэтому текст внутри такой кнопки должен быть светлым: используй `Base (#eff1f5)` или `Crust (#dce0e8)`.
* В **Mocha** акцентные цвета пастельные и светлые (например, `Mauve #cba6f7`), поэтому белый текст на них **не читается**! Текст внутри акцентных кнопок в Mocha **обязан** быть темным: используй `Crust (#11111b)` или `Base (#1e1e2e)`.
* **Универсальное решение:** Для текста на фоне любого акцентного цвета всегда задавай `color: var(--ctp-base)` или `color: var(--ctp-crust)`. Это автоматически даст светлый текст в Latte и темный текст в Mocha!

### 1. Карточки и контейнеры (Cards & Panels)
* **Фон:** `Surface 0` (или `Mantle` для больших секций/сайдбаров).
* **Рамка (Border):** `1px solid var(--ctp-surface1)` (при наведении меняй на `var(--ctp-surface2)` или `var(--ctp-lavender)`).
* **Скругление:** `12px` (`rounded-xl`) или `16px` (`rounded-2xl`).
* **Тень:**
  * В *Latte*: Мягкая тень `0 4px 20px -2px rgba(76, 79, 105, 0.08)`.
  * В *Mocha*: Тень почти не видна, отделение от фона идет за счет разницы `Base` -> `Surface 0` и рамки `Surface 1`.

### 2. Кнопки (Buttons)
* **Primary Button:**
  * Фон: `var(--ctp-mauve)` (или `var(--ctp-blue)`).
  * Текст: `var(--ctp-base)` (жирный, `font-medium` или `font-semibold`).
  * Hover: Легкое изменение прозрачности (`opacity-90`) или плавный переход в `var(--ctp-lavender)` + легкий подъем (`-translate-y-0.5`).
* **Secondary / Ghost Button:**
  * Фон: `var(--ctp-surface0)`.
  * Текст: `var(--ctp-text)`.
  * Рамка: `1px solid var(--ctp-surface1)`.
  * Hover: Фон меняется на `var(--ctp-surface1)`.

### 3. Поля ввода (Inputs, Textareas, Selects)
* **Фон:** `var(--ctp-mantle)` или `var(--ctp-surface0)`.
* **Рамка:** `1px solid var(--ctp-surface1)`.
* **Текст:** `var(--ctp-text)`.
* **Placeholder:** `var(--ctp-overlay0)`.
* **Focus State:** `outline: none`, рамка меняется на `var(--ctp-mauve)` или `var(--ctp-blue)`, добавляется кольцо фокуса `ring-2 ring-ctp-mauve/30`.

### 4. Навигация и Хедер (Navbar)
* Используй эффект матового стекла (Glassmorphism):
  * Фон: `var(--ctp-mantle)` с прозрачностью `80%` (`backdrop-blur-md`).
  * Нижняя граница: `1px solid var(--ctp-surface0)`.
* Активная ссылка выделяется цветом `var(--ctp-mauve)` и плашкой на фоне `var(--ctp-surface0)`.

### 5. Бейджи и Теги (Badges / Pills)
* Не используй сплошную заливку для второстепенных тегов. Используй полупрозрачный фон акцентного цвета (15-20% opacity) и сплошной текст того же акцентного цвета:
  * *Пример (Success Badge):* Фон `rgba(var(--ctp-green), 0.15)`, Текст `var(--ctp-green)`, Рамка `1px solid rgba(var(--ctp-green), 0.3)`.

---

## 5. Типографика (Typography)

* **Основной шрифт (Sans-serif):** `Inter`, `Plus Jakarta Sans` или `Outfit`.
* **Моноширинный шрифт (Monospace — для кода, цифр, тегов):** `JetBrains Mono` или `Fira Code` (визитная карточка эстетики Catppuccin).
* **Иерархия текста:**
  * Главные заголовки (`H1`, `H2`): `var(--ctp-text)`, можно добавлять легкий градиент текста от `var(--ctp-mauve)` к `var(--ctp-blue)` или `var(--ctp-sapphire)`.
  * Основной параграф (`p`): `var(--ctp-subtext1)` (делает чтение длинных текстов более мягким для глаз, чем максимальный `text`).
  * Вспомогательные подписи, даты: `var(--ctp-subtext0)`.

---

## 6. Чек-лист самопроверки для AI-агента перед выдачей кода

1. [ ] В коде **нет** хардкодных цветов вроде `#fff`, `#000`, `gray-800`, `slate-900`?
2. [ ] Присутствует ли в интерфейсе удобный переключатель тем (Theme Toggle: 🌻 Latte / 🌿 Mocha)?
3. [ ] На всех ли кнопках с акцентным фоном (`Mauve`, `Blue`, `Green`) цвет текста установлен в `var(--ctp-base)` или `var(--ctp-crust)` для читаемости в темной теме?
4. [ ] Соблюдена ли иерархия фонов (`Crust` -> `Mantle` -> `Base` -> `Surface 0` -> `Surface 1`)?
5. [ ] Выглядят ли скругления, отступы и переходы мягкими и плавными?