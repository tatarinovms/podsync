const settingsForm = document.getElementById('settings-form');
const shareForm = document.getElementById('share-form');
const apiURLInput = document.getElementById('api-url');
const usernameInput = document.getElementById('username');
const passwordInput = document.getElementById('password');
const feedSelect = document.getElementById('feed');
const openSendSettingsButton = document.getElementById('open-send-settings');
const sendSettings = document.getElementById('send-settings');
const cancelSendButton = document.getElementById('cancel-send');
const formatSelect = document.getElementById('format');
const qualitySelect = document.getElementById('quality');
const maxHeightSelect = document.getElementById('max-height');
const videoResolution = document.getElementById('video-resolution');
const shareButton = document.getElementById('share');
const status = document.getElementById('status');

function setStatus(message, type = '') {
  status.textContent = message;
  status.className = type;
}

function apiBase(value) {
  const url = new URL(value);
  if (url.protocol !== 'https:' && !['localhost', '127.0.0.1'].includes(url.hostname)) {
    throw new Error('Use HTTPS for a remote Podsync server.');
  }
  return url.toString().replace(/\/$/, '').replace(/\/api\/v1$/, '') + '/api/v1';
}

function authorization(config) {
  return `Basic ${btoa(`${config.username}:${config.password}`)}`;
}

async function getConfig() {
  return browser.storage.local.get({ apiURL: '', username: '', password: '', feedID: '', format: 'audio', quality: 'high', maxHeight: 0 });
}

async function request(config, path, options = {}) {
  const response = await fetch(`${apiBase(config.apiURL)}${path}`, {
    ...options,
    headers: { Authorization: authorization(config), ...(options.headers || {}) }
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(data.error || `Podsync returned HTTP ${response.status}`);
  return data;
}

async function loadFeeds(config, selectedFeedID = '') {
  const feeds = await request(config, '/personal-feeds');
  feedSelect.replaceChildren();
  for (const feed of feeds) {
    const option = new Option(feed.title || feed.id, feed.id, false, feed.id === selectedFeedID);
    feedSelect.add(option);
  }
  const hasFeeds = feeds.length > 0;
  feedSelect.disabled = !hasFeeds;
  openSendSettingsButton.disabled = !hasFeeds;
  shareButton.disabled = !hasFeeds;
  if (!hasFeeds) setStatus('No personal feeds found. Create one in Podsync first.', 'error');
}

async function initialise() {
  const config = await getConfig();
  apiURLInput.value = config.apiURL;
  usernameInput.value = config.username;
  passwordInput.value = config.password;
  formatSelect.value = config.format;
  qualitySelect.value = config.quality;
  maxHeightSelect.value = String(config.maxHeight);
  updateFormatControls();
  if (!config.apiURL || !config.username || !config.password) return;
  try {
    await loadFeeds(config, config.feedID);
  } catch (error) {
    setStatus(error.message, 'error');
  }
}

settingsForm.addEventListener('submit', async event => {
  event.preventDefault();
  const config = { apiURL: apiURLInput.value.trim(), username: usernameInput.value.trim(), password: passwordInput.value };
  try {
    apiBase(config.apiURL);
    await loadFeeds(config);
    config.feedID = feedSelect.value;
    await browser.storage.local.set(config);
    setStatus('Saved. Choose a feed and add the current tab.', 'success');
  } catch (error) {
    setStatus(error.message, 'error');
  }
});

feedSelect.addEventListener('change', () => browser.storage.local.set({ feedID: feedSelect.value }));
for (const control of [formatSelect, qualitySelect, maxHeightSelect]) {
  control.addEventListener('change', () => browser.storage.local.set({
    format: formatSelect.value,
    quality: qualitySelect.value,
    maxHeight: Number(maxHeightSelect.value)
  }));
}

openSendSettingsButton.addEventListener('click', async () => {
  const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url || !/^https?:/.test(tab.url) || !isSupportedURL(tab.url)) {
    setStatus('Only YouTube, VK Video, and Rutube links are supported.', 'error');
    return;
  }
  sendSettings.classList.remove('hidden');
  openSendSettingsButton.classList.add('hidden');
  formatSelect.focus();
});

cancelSendButton.addEventListener('click', () => {
  sendSettings.classList.add('hidden');
  openSendSettingsButton.classList.remove('hidden');
});

function isSupportedURL(value) {
  const host = new URL(value).hostname.toLowerCase().replace(/^www\./, '');
  return host === 'youtu.be' || host.endsWith('.youtube.com') || host === 'youtube.com' ||
    host === 'vkvideo.ru' || host.endsWith('.vkvideo.ru') || host === 'rutube.ru' || host.endsWith('.rutube.ru');
}

function updateFormatControls() {
  videoResolution.classList.toggle('hidden', formatSelect.value !== 'video');
}

formatSelect.addEventListener('change', updateFormatControls);

shareForm.addEventListener('submit', async event => {
  event.preventDefault();
  const config = await getConfig();
  const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
  if (!tab?.url || !/^https?:/.test(tab.url)) {
    setStatus('Open a regular HTTP or HTTPS page first.', 'error');
    return;
  }
  if (!isSupportedURL(tab.url)) {
    setStatus('Only YouTube, VK Video, and Rutube links are supported.', 'error');
    return;
  }
  shareButton.disabled = true;
  setStatus('Adding to Podsync…');
  try {
    await request(config, `/personal-feeds/${encodeURIComponent(feedSelect.value)}/episodes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        url: tab.url,
        title: tab.title || '',
        format: formatSelect.value,
        quality: qualitySelect.value,
        max_height: Number(maxHeightSelect.value)
      })
    });
    setStatus('Queued in Podsync.', 'success');
    setTimeout(() => window.close(), 500);
  } catch (error) {
    setStatus(error.message, 'error');
  } finally {
    shareButton.disabled = false;
  }
});

initialise();
