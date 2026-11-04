# Podsync Share for Firefox

This Firefox extension sends the current browser tab to a selected Podsync personal feed.

## Install for testing

1. Open `about:debugging#/runtime/this-firefox` in Firefox.
2. Select **Load Temporary Add-on**.
3. Select `manifest.json` from the unpacked `podsync-firefox` directory.
4. Open the extension from Firefox's toolbar, enter the Podsync API URL (for example `https://rssreader.example.com/api/v1`), and sign in with the local Podsync admin username and password.
5. Select a personal feed and use **Add current tab**.

Only YouTube, VK Video, and Rutube links can be sent. Before sending, choose MP3 audio or MP4 video, audio quality, and a maximum video resolution. The default is high-quality MP3 audio; video settings apply only when Video is selected.

Temporary add-ons are removed when Firefox restarts. For permanent installation, upload the ZIP to AMO as **On your own**, download Mozilla's signed `.xpi`, and install it from Firefox's Add-ons Manager.

## Security

The server URL, username, and password are stored only in Firefox's extension-local storage. They are sent only to the configured Podsync API over HTTPS. Do not use an HTTP URL except for `localhost` during development.

The extension transmits the administrator credentials and the URL/title of the tab you explicitly add, only to the configured Podsync server. It performs no analytics, tracking, or third-party data sharing.
