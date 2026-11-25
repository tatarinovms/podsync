# Podsync Share for Firefox

This Firefox extension sends the current browser tab to a selected Podsync personal feed.

## Install

Download the [signed Podsync Share `.xpi`](https://github.com/tatarinovms/podsync/releases/download/v2.27.0/924c9395d5e947a39fac-1.0.5.xpi), open it in Firefox, and approve the installation.

Open **Connection settings** and enter the public Podsync API URL ending in `/api/v1`, plus the same administrator username and password used for Web Admin. Select **Save and load feeds** and approve the Firefox prompt for your Podsync server. Select a Personal Feed, and use **Add current tab** on a supported page.

## Permissions

The extension requests only `activeTab` and `storage` up front. Firefox grants `activeTab` when you open the popup, which is enough to read the URL and title of the current tab.

Access to your Podsync server is an optional host permission requested for that server's origin when you save the connection settings. There is no permanent `<all_urls>` access; if you change the Podsync URL, Firefox asks for access to the new origin. Revoking the permission in Firefox's add-on manager stops all network access until you save the settings again.

## Install for testing

1. Open `about:debugging#/runtime/this-firefox` in Firefox.
2. Select **Load Temporary Add-on**.
3. Select `manifest.json` from the unpacked `podsync-firefox` directory.
4. Open the extension from Firefox's toolbar, enter the Podsync API URL (for example `https://rssreader.example.com/api/v1`), and sign in with the local Podsync admin username and password.
5. Select a personal feed and use **Add current tab**.

Only YouTube, VK Video (`vkvideo.ru`, `vk.com`, `vk.ru`), and Rutube links can be sent. Before sending, choose MP3 audio or MP4 video, audio quality, and a maximum video resolution. The default is high-quality MP3 audio; video settings apply only when Video is selected. After Podsync accepts the link, the popup shows the feed name and the queued episode ID and stays open until you close it.

Temporary add-ons are removed when Firefox restarts.

## Tests

Run the dependency-free extension suite from the repository root:

```bash
node extensions/podsync-firefox/tests/popup_test.js
```

The suite covers provider URL validation (`youtube.com`, `youtu.be`, `vkvideo.ru`, `vk.com`, `vk.ru`, `rutube.ru`), API URL normalisation, UTF-8 encoding of non-ASCII passwords, API error handling, the audio/video payload, and the queued confirmation flow with a mocked browser.

## Security

The server URL, username, and password are stored only in Firefox's extension-local storage. They are sent only to the configured Podsync API over HTTPS. Do not use an HTTP URL except for `localhost` during development.

The extension transmits the administrator credentials and the URL/title of the tab you explicitly add, only to the configured Podsync server. It performs no analytics, tracking, or third-party data sharing.
