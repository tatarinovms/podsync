# How to get VK API token

VK has removed standalone app creation from their new developer portal. Therefore, obtaining a user access token (`video`, `offline`) can be done either via a direct official VK OAuth link or through a trusted token generator service:

### Method 1: Direct VK OAuth link (oauth.vk.ru)

1. Open the following authorization link in your browser:
   ```text
   https://oauth.vk.ru/authorize?client_id=2685278&scope=video,offline&redirect_uri=https://oauth.vk.ru/blank.html&display=page&response_type=token&revoke=1
   ```

2. Click the **"Allow"** button (grants access to video files and indefinite `offline` access).

3. The browser will redirect you to a technical placeholder page. The address bar will display a URL like:
   ```text
   https://oauth.vk.ru/blank.html#access_token=vk1.a.XXXXXXXXXXXX...&expires_in=0&user_id=12345678
   ```

> [!NOTE]
> The text on the blank page: *"Please do not copy data from the address bar for third-party sites..."* is **not an error**, but a standard VK security warning. The token has already been generated and is present in your browser's address bar.

4. Copy the `access_token` value (starting from `vk1.a...` up to `&expires_in=0`).

### Method 2: Using vkhost.github.io generator

If the direct link is blocked by your browser or network provider:

1. Navigate to [vkhost.github.io](https://vkhost.github.io).
2. Click on the **Kate Mobile** (or **VK Admin**) tile.
3. Click the **"Allow"** button on the VK authorization page.
4. A blank page will open. Copy the access token from the browser's address bar (value from `access_token=` up to `&expires_in`).

### Configuring token in Podsync

The obtained token can be configured in several ways:

1. **Via Podsync Web Admin UI**: Open the **"API Keys"** tab, enter the token in the **VKVIDEO** field, and click **"Save"**.
2. **Via `config.toml`**:
   ```toml
   [tokens]
   vkvideo = "vk1.a.XXXXXXXXXXXX..."
   ```
3. **Via environment variable**:
   ```sh
   export PODSYNC_VKVIDEO_API_KEY="vk1.a.XXXXXXXXXXXX..."
   ```
