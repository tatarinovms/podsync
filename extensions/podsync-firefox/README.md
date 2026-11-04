# Podsync Share for Firefox

This Firefox extension sends the current browser tab to a selected Podsync personal feed.

## Install

Download the [signed Podsync Share `.xpi`](https://github.com/tatarinovms/podsync/releases/download/v2.26.6/924c9395d5e947a39fac-1.0.4.xpi), open it in Firefox, and approve the installation.

Open **Connection settings** and enter the public Podsync API URL ending in `/api/v1`, plus the same administrator username and password used for Web Admin. Select **Save and load feeds**, choose a Personal Feed, and use **Add current tab** on a supported page.

## Install for testing

1. Open `about:debugging#/runtime/this-firefox` in Firefox.
2. Select **Load Temporary Add-on**.
3. Select `manifest.json` from the unpacked `podsync-firefox` directory.
4. Open the extension from Firefox's toolbar, enter the Podsync API URL (for example `https://rssreader.example.com/api/v1`), and sign in with the local Podsync admin username and password.
5. Select a personal feed and use **Add current tab**.

Only YouTube, VK Video, and Rutube links can be sent. Before sending, choose MP3 audio or MP4 video, audio quality, and a maximum video resolution. The default is high-quality MP3 audio; video settings apply only when Video is selected.

Temporary add-ons are removed when Firefox restarts.

## Security

The server URL, username, and password are stored only in Firefox's extension-local storage. They are sent only to the configured Podsync API over HTTPS. Do not use an HTTP URL except for `localhost` during development.

The extension transmits the administrator credentials and the URL/title of the tab you explicitly add, only to the configured Podsync server. It performs no analytics, tracking, or third-party data sharing.
