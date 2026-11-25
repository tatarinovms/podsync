# Telegram bot

The optional bot controls one Podsync instance from one private Telegram chat. It uses long polling, so Podsync does not need a public webhook URL. The bot interface is in English.

## Setup

1. Create a bot with BotFather and keep its token private.
2. Enable Web Admin and open **Access & integrations → Telegram**.
3. Paste the token, optionally set **Allowed Telegram user ID** and an **HTTP / SOCKS5 proxy**, switch **Enable Telegram bot** on, and select **Save**. Podsync validates the token with Telegram `getMe` before saving it to `config.toml` and starts polling immediately; no restart is required. The configuration file is written with `0600` permissions.
4. Select **Connect** and send the displayed `/start <code>` to your bot in a private chat within 10 minutes. Only that Telegram account and private chat can use the bot.
5. Select **Send test** to verify delivery. **Disconnect** removes the owner. The panel shows the connected owner's numeric user ID with a copy button.

Disabling the bot stops polling and notifications but keeps the owner pairing and queued delivery state for the next enable. Changing the token disconnects the current owner and requires a new `/start <code>` code. Removing the token is available only while the bot is disabled and the token comes from `config.toml`.

### Allowed Telegram user ID

`allowed_user_id` is an optional positive numeric Telegram **user ID** (`from.id`), not an `@username` and not a chat ID. `0` keeps the default mode where anyone who knows a valid pairing code can connect.

When the value is set, only that Telegram account can pair and use commands or buttons. A matching ID preserves the existing owner; a different ID disconnects the current owner, clears the active pairing code and drafts, and requires a new `/start` code. Setting the field back to `0` removes the restriction but does not restore an owner that was previously disconnected. Unknown users are ignored silently, including callbacks; group chats remain unsupported.

If the owner ID is not known yet, pair your own chat first, copy the displayed user ID, then set the restriction. A failed token check, configuration write, or bot restart rolls the running bot and pairing state back to the previous settings.

### Telegram proxy

If `api.telegram.org` is blocked in your region, set `proxy` in the Telegram panel. Supported schemes are `http://`, `https://`, `socks5://`, and `socks5h://`; credentials may be included in the URL. The proxy is used for Bot API polling, token validation with `getMe`, and test messages. It does not affect feed downloads, which use the Downloader proxy settings. Leave the field empty for a direct connection. Changes apply without restarting Podsync.

### Environment token

`PODSYNC_TELEGRAM_BOT_TOKEN` overrides `[telegram].token`. When it is set, Web Admin shows **token from environment** and disables token editing and removal. The enable switch can still be saved. To replace or remove an environment token, change the variable and restart Podsync, then save the settings again if needed.

If `config.toml` is not writable by the Podsync process, use the environment token or make the configuration file writable. A failed token check, configuration write, or bot restart leaves the previous running configuration in place and reports a safe error in the UI.

## Commands

- Send an HTTP or HTTPS media URL. Choose a Personal Feed, Audio or Video, then quality or video resolution using the buttons. The bot confirms that the episode was queued and gives its episode ID.
- `/status` shows feed, download, and error counts.
- `/errors` shows recent errors and IDs for retry.
- `/retry <feed_id> <episode_id>` queues a retry for a regular or Personal Feed episode.
- `/cancel` clears an unfinished URL selection; `/help` lists commands.

The bot sends notifications when a completed update reveals a new download error or a prior error clears. Telegram delivery failures do not interrupt feed updates. Unsent notifications and duplicate tracking are stored in `telegram-state.json` beside the Badger database. This pairing and delivery state is not part of portable backups; reconnect after migration. `include_secrets=false` removes the bot token from a backup's configuration.

The bot is available when Web Admin is enabled and Podsync runs as a normal local-storage server. It does not run in headless, backup, migration, or standalone MCP modes.
