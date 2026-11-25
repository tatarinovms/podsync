# Telegram bot

The optional bot controls one Podsync instance from one private Telegram chat. It uses long polling, so Podsync does not need a public webhook URL. The bot interface is in English.

## Setup

1. Create a bot with BotFather and keep its token private.
2. Enable Web Admin and add `[telegram]` with `enabled = true` to `config.toml`. Set `PODSYNC_TELEGRAM_BOT_TOKEN` (or `token` in the `[telegram]` section), then restart Podsync.
3. Open **Access & integrations → Telegram** in Web Admin and click **Connect**. Send the displayed `/start <code>` to your bot in a private chat within 10 minutes. Only that Telegram account and private chat can use the bot. **Send test** checks delivery; **Disconnect** removes the owner.

## Commands

- Send an HTTP or HTTPS media URL. Choose a Personal Feed, Audio or Video, then quality or video resolution using the buttons. The bot confirms that the episode was queued and gives its episode ID.
- `/status` shows feed, download, and error counts.
- `/errors` shows recent errors and IDs for retry.
- `/retry <feed_id> <episode_id>` queues a retry for a regular or Personal Feed episode.
- `/cancel` clears an unfinished URL selection; `/help` lists commands.

The bot sends notifications when a completed update reveals a new download error or a prior error clears. Telegram delivery failures do not interrupt feed updates. Unsent notifications and duplicate tracking are stored in `telegram-state.json` beside the Badger database. This pairing and delivery state is not part of portable backups; reconnect after migration. `include_secrets=false` removes the bot token from a backup's configuration.

The bot is available when Web Admin is enabled and Podsync runs as a normal local-storage server. It does not run in headless, backup, migration, or standalone MCP modes.
