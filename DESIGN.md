# 🎨 Design System & UI Guidelines: Catppuccin (Latte & Mocha)

> **Instructions for AI agents:** This document is the single source of truth for the project's visual design. All UI components, pages, and styles MUST follow the Catppuccin palette and rules below. Never use default framework colors or pure black/white; use the designated Catppuccin tokens instead.

## 1. Design Philosophy (Aesthetic Direction)

- **Style:** Soothing pastel aesthetic, cozy appearance, and gentle contrast. Avoid harsh neon tones and pure black or white.
- **Themes:** Support 🌻 **Catppuccin Latte** (light mode) and 🌿 **Catppuccin Mocha** (dark mode).
- **Shapes:** Soft corners (`rounded-xl`, `rounded-2xl`), subtle 1px borders, delicate shadows in Latte, and surface-layer contrast in Mocha.

## 2. Color Palette (Color Tokens)

Use only the HEX values specified here for the design palette.

### A. Backgrounds, Surfaces & Typography

| Token | UI role | 🌻 Latte (Light) | 🌿 Mocha (Dark) |
|---|---|---|---|
| Base | Main page background (`body`) | `#eff1f5` | `#1e1e2e` |
| Mantle | Secondary background (sidebars, footers, code blocks) | `#e6e9ef` | `#181825` |
| Crust | Deepest background (backdrops, dividers, overlays) | `#dce0e8` | `#11111b` |
| Surface 0 | Cards, inputs, inactive buttons | `#ccd0da` | `#313244` |
| Surface 1 | Card hover states, dropdowns | `#bcc0cc` | `#45475a` |
| Surface 2 | Active elements, borders, badges | `#acb0be` | `#585b70` |
| Overlay 0 | Muted icons, placeholders | `#9ca0b0` | `#6c7086` |
| Overlay 1 | Disabled text, scrollbars | `#8c8fa1` | `#7f849c` |
| Overlay 2 | Secondary icons, decorative lines | `#7c7f93` | `#9399b2` |
| Subtext 0 | Small text, captions, metadata, copyright | `#6c6f85` | `#a6adc8` |
| Subtext 1 | Subheadings, descriptions, secondary text | `#5c5f77` | `#bac2de` |
| Text | Primary text, H1–H6 headings | `#4c4f69` | `#cdd6f4` |

### B. Accents & Semantics

| Token | Recommended use | 🌻 Latte (Light) | 🌿 Mocha (Dark) |
|---|---|---|---|
| Mauve | Primary accent: main CTA buttons, active tabs | `#8839ef` | `#cba6f7` |
| Blue | Secondary accent: links, informational badges, focus | `#1e66f5` | `#89b4fa` |
| Lavender | Heading accents, gradients with Mauve/Blue | `#7287fd` | `#b4befe` |
| Sapphire | Icons, interactive elements, link hover states | `#209fb5` | `#74c7ec` |
| Sky | Tags, decorative elements | `#04a5e5` | `#89dceb` |
| Teal | Supporting accents, progress indicators | `#179299` | `#94e2d5` |
| Green | Success, online/ready status | `#40a02b` | `#a6e3a1` |
| Yellow | Warnings, ratings, important notes | `#df8e1d` | `#f9e2af` |
| Peach | Notifications, new/popular badges | `#fe640b` | `#fab387` |
| Maroon | Secondary errors, noncritical deletion | `#e64553` | `#eba0ac` |
| Red | Danger/errors, delete buttons | `#d20f39` | `#f38ba8` |
| Pink | Creative accents, likes, favorites | `#ea76cb` | `#f5c2e7` |
| Flamingo | Decorative illustrations, avatars | `#dd7878` | `#f2cdcd` |
| Rosewater | Soft glow, cursors, subtle accents | `#dc8a78` | `#f5e0dc` |

## 3. Technical Implementation (CSS Variables & Tailwind)

Define these CSS variables in the global stylesheet (`globals.css` / `index.css`). Switch themes using `.dark` or `data-theme="mocha"` on `<html>`.

```css
:root {
  /* Catppuccin Latte (default light mode) */
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
  /* Catppuccin Mocha (dark mode) */
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

### Tailwind CSS Integration

If Tailwind is used, install `@catppuccin/tailwindcss` or map colors in `tailwind.config.js`:

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

## 4. UI Component Rules

### ⚠️ Critical Contrast Rule (Buttons & Badges)

Accent colors have different lightness levels in Latte and Mocha:

- In **Latte**, accents such as Mauve `#8839ef` and Blue `#1e66f5` are saturated; use light foreground colors such as Base `#eff1f5` or Crust `#dce0e8` where contrast permits.
- In **Mocha**, accents such as Mauve `#cba6f7` are pastel and light; white text is difficult to read. Prefer a dark foreground such as Crust `#11111b` or Base `#1e1e2e`.
- **Default approach:** Use `color: var(--ctp-base)` or `color: var(--ctp-crust)` for filled accent buttons, **but verify WCAG contrast for each accent/background pairing** rather than assuming all combinations pass.

### 1. Cards & Panels

- **Background:** `Surface 0`, or `Mantle` for large sections and sidebars.
- **Border:** `1px solid var(--ctp-surface1)`; on hover use `var(--ctp-surface2)` or `var(--ctp-lavender)`.
- **Radius:** `12px` (`rounded-xl`) or `16px` (`rounded-2xl`).
- **Shadow:** Latte: `0 4px 20px -2px rgba(76, 79, 105, 0.08)`. Mocha: rely mainly on contrast between Base, Surface 0 and Surface 1 rather than shadows.

### 2. Buttons

- **Primary:** `var(--ctp-mauve)` or `var(--ctp-blue)` background; contrasting `var(--ctp-base)` foreground (subject to contrast checks); medium/semibold text. Hover: subtle opacity change (`opacity-90`) or Lavender transition and small lift (`-translate-y-0.5`).
- **Secondary / Ghost:** `var(--ctp-surface0)` background, `var(--ctp-text)` foreground, `1px solid var(--ctp-surface1)` border; hover uses `var(--ctp-surface1)`.

### 3. Inputs, Textareas & Selects

- Background: `var(--ctp-mantle)` or `var(--ctp-surface0)`.
- Border: `1px solid var(--ctp-surface1)`.
- Text: `var(--ctp-text)`.
- Placeholder: `var(--ctp-overlay0)`.
- Focus: replace default outline only when providing an equally visible accessible focus indicator, such as a Mauve/Blue border and `ring-2 ring-ctp-mauve/30`.

### 4. Navbar & Header

- Use a frosted-glass effect: `var(--ctp-mantle)` at 80% opacity with `backdrop-blur-md`.
- Bottom border: `1px solid var(--ctp-surface0)`.
- Highlight active links with `var(--ctp-mauve)` and a `var(--ctp-surface0)` background.

### 5. Badges & Pills

- Avoid solid fills for secondary tags. Use an accent-tinted background at 15–20% opacity, accent-colored text, and a subtle accent border.
- Example (success badge):

```css
.badge-success {
  background: color-mix(in srgb, var(--ctp-green) 15%, transparent);
  color: var(--ctp-green);
  border: 1px solid color-mix(in srgb, var(--ctp-green) 30%, transparent);
}
```

## 5. Typography

- **Primary sans-serif:** `Inter`, `Plus Jakarta Sans`, or `Outfit`.
- **Monospace:** `JetBrains Mono` or `Fira Code`.
- **H1/H2:** `var(--ctp-text)`; optional subtle Mauve-to-Blue/Sapphire text gradient.
- **Body paragraphs:** `var(--ctp-subtext1)` for a gentler long-form reading experience, provided contrast is sufficient.
- **Captions and dates:** `var(--ctp-subtext0)`.

## 6. AI Agent Self-Review Checklist

- [ ] No arbitrary hard-coded colors such as `#fff`, `#000`, `gray-800`, or `slate-900` appear in UI styles.
- [ ] A convenient theme switcher is available (🌻 Latte / 🌿 Mocha).
- [ ] Text on Mauve, Blue, Green, and other accent backgrounds uses a readable, contrast-checked foreground such as Base or Crust.
- [ ] The surface hierarchy is respected: Crust → Mantle → Base → Surface 0 → Surface 1.
- [ ] Rounded corners, spacing, and transitions remain soft and consistent.
- [ ] All product UI text is in English; there are no Russian labels, buttons, tooltips, or default names.

## 7. Interface Language: English Only

- The entire Web Admin Console UI—headings, navigation tabs, sidebars, buttons, forms, placeholders, badges, modals, notifications, tooltips, and backend-defined default names such as `My Feed`—MUST use English.
- Do not mix languages or insert Russian text into UI markup, styles, or templates.
- When users enter Cyrillic names for identifiers (such as Feed IDs or URL slugs), automatically transliterate them to Latin ASCII slugs and validate the result. **Preserve the original user-entered display name**; transliteration applies to identifiers, not content.

## 8. Responsive Layout (Added)

- Build mobile-first layouts and verify at approximately 320px, 375px, 768px, 1024px, and 1440px viewport widths.
- On narrow screens, collapse the sidebar into a keyboard-accessible drawer; do not let navigation obscure primary content.
- Make grids responsive, prevent horizontal page overflow, and allow wide data tables to scroll within their own container.
- Maintain comfortable touch targets (prefer at least 44×44px) and spacing between adjacent actions.

## 9. Accessibility (Added)

- Target WCAG 2.2 AA: 4.5:1 contrast for normal text, 3:1 for large text and essential non-text UI boundaries where applicable.
- Provide visible `:focus-visible` indicators, logical tab order, keyboard operability, accessible labels, and semantic HTML.
- Do not communicate status or errors through color alone; include text or an icon with an accessible label.
- Support screen readers, browser zoom, text resizing, and reduced-motion preferences.
- Audit the actual rendered colors, including text over tinted surfaces and translucent badges.

## 10. Spacing, Sizing & Iconography (Added)

- Use a consistent spacing scale: `4, 8, 12, 16, 24, 32, 48, 64px`.
- Prefer existing spacing and radius tokens over arbitrary per-component values.
- Use one consistent icon family, preferably Lucide, with standardized 16/20/24px sizes and consistent stroke weight.
- Icons alone must have accessible names when they perform actions.

## 11. Loading, Empty, Error & Success States (Added)

- Every async data view must define loading, empty, error, and loaded states.
- Use subtle skeleton placeholders for content loading, with reduced-motion support.
- Empty states must explain what is missing and, when useful, provide a clear next action.
- Errors must state what failed and offer retry or recovery when possible.
- Prevent duplicate submissions, show progress for long-running operations, and provide feedback after success.

## 12. Forms, Validation & Destructive Actions (Added)

- Use persistent labels, helpful descriptions, and inline field-level validation.
- Associate error messages with fields programmatically; do not rely on placeholders as labels.
- Clearly distinguish optional and required fields.
- Confirm destructive actions such as deleting a feed; explain consequences and offer undo when feasible.
- Keep button labels action-specific (`Save Changes`, `Delete Feed`) rather than ambiguous (`OK`, `Submit`).

## 13. Motion & Theme Preferences (Added)

- Keep UI transitions subtle, typically 150–250ms; avoid unnecessary large movements.
- Respect `prefers-reduced-motion: reduce` by disabling nonessential animations.
- Offer **Light / Dark / System** theme preferences; resolve System using `prefers-color-scheme`.
- Persist an explicit user preference and avoid a flash of the wrong theme during initial render.
- Verify dialogs, popovers, charts, scrollbars, and browser-native controls in both themes.

## 14. Content Density, Reuse & Quality Gates (Added)

- Truncate long titles only where necessary, while keeping the full text accessible via tooltip or expanded view.
- Wrap or safely truncate long URLs; avoid breaking responsive layouts.
- Use pagination or virtualization for large feed/item tables and lists.
- Reuse existing shared UI components and tokens before introducing new variants.
- Avoid creating duplicate implementations of buttons, inputs, cards, or theme logic.
- Before merging UI changes, test both themes, keyboard navigation, responsive widths, loading/empty/error states, and contrast.
- Keep user-generated content separate from English-only interface chrome: do not translate or rewrite user content automatically.
