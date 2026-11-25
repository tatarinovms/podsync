const {
  episodePayload,
  isSupportedURL,
  queuedDetails,
  request,
  serverOrigin
} = globalThis.PodsyncShare;

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
const sendResult = document.getElementById('send-result');
const resultFeed = document.getElementById('result-feed');
const resultEpisode = document.getElementById('result-episode');
const resultTitleRow = document.getElementById('result-title-row');
const resultTitle = document.getElementById('result-title');
const closePopupButton = document.getElementById('close-popup');

function setStatus(message, type = '') {
  status.textContent = message;
  status.className = type;
}

async function getConfig() {
  return browser.storage.local.get({ apiURL: '', username: '', password: '', feedID: '', format: 'audio', quality: 'high', maxHeight: 0 });
}

function originPattern(value) {
  return `${serverOrigin(value)}/*`;
}

async function hasServerAccess(apiURL) {
  return browser.permissions.contains({ origins: [originPattern(apiURL)] });
}

async function requestServerAccess(apiURL) {
  // Called directly from the settings submit handler: Firefox requires
  // permissions.request() to run inside the user gesture, so no contains()
  // check is awaited before it.
  return browser.permissions.request({ origins: [originPattern(apiURL)] });
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

async function currentTab() {
  const [tab] = await browser.tabs.query({ active: true, currentWindow: true });
  return tab;
}

function validateTabURL(tab) {
  if (!tab?.url || !/^https?:/i.test(tab.url)) {
    return 'Open a regular HTTP or HTTPS page first.';
  }
  if (!isSupportedURL(tab.url)) {
    return 'Only YouTube, VK Video, and Rutube links are supported.';
  }
  return '';
}

function showQueued(details) {
  resultFeed.textContent = details.feed;
  resultEpisode.textContent = details.episode || 'Unknown';
  resultTitle.textContent = details.title;
  resultTitleRow.classList.toggle('hidden', !details.title);
  shareForm.classList.add('hidden');
  sendResult.classList.remove('hidden');
  closePopupButton.focus();
}

function selectedFeedTitle() {
  const option = feedSelect.options?.[feedSelect.selectedIndex];
  return option?.textContent || option?.text || feedSelect.value;
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
    if (!(await hasServerAccess(config.apiURL))) {
      setStatus('Podsync server access was not granted. Save the connection settings again.', 'error');
      return;
    }
    await loadFeeds(config, config.feedID);
  } catch (error) {
    setStatus(error.message, 'error');
  }
}

settingsForm.addEventListener('submit', async event => {
  event.preventDefault();
  const config = { apiURL: apiURLInput.value.trim(), username: usernameInput.value.trim(), password: passwordInput.value };
  try {
    const origin = serverOrigin(config.apiURL);
    if (!(await requestServerAccess(config.apiURL))) {
      setStatus(`Server access for ${origin} was not granted.`, 'error');
      return;
    }
    await loadFeeds(config);
    config.feedID = feedSelect.value;
    await browser.storage.local.set(config);
    sendResult.classList.add('hidden');
    sendSettings.classList.add('hidden');
    shareForm.classList.remove('hidden');
    openSendSettingsButton.classList.remove('hidden');
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
  const tab = await currentTab();
  const problem = validateTabURL(tab);
  if (problem) {
    setStatus(problem, 'error');
    return;
  }
  sendResult.classList.add('hidden');
  sendSettings.classList.remove('hidden');
  openSendSettingsButton.classList.add('hidden');
  formatSelect.focus();
});

cancelSendButton.addEventListener('click', () => {
  sendSettings.classList.add('hidden');
  openSendSettingsButton.classList.remove('hidden');
});

function updateFormatControls() {
  videoResolution.classList.toggle('hidden', formatSelect.value !== 'video');
}

formatSelect.addEventListener('change', updateFormatControls);

closePopupButton.addEventListener('click', () => window.close());

shareForm.addEventListener('submit', async event => {
  event.preventDefault();
  const config = await getConfig();
  const tab = await currentTab();
  const problem = validateTabURL(tab);
  if (problem) {
    setStatus(problem, 'error');
    return;
  }
  if (!(await hasServerAccess(config.apiURL))) {
    setStatus('Podsync server access was not granted. Save the connection settings again.', 'error');
    return;
  }
  let payload;
  try {
    payload = episodePayload(tab, {
      format: formatSelect.value,
      quality: qualitySelect.value,
      maxHeight: maxHeightSelect.value
    });
  } catch (error) {
    setStatus(error.message, 'error');
    return;
  }
  shareButton.disabled = true;
  setStatus('Adding to Podsync…');
  try {
    const data = await request(config, `/personal-feeds/${encodeURIComponent(feedSelect.value)}/episodes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    });
    setStatus('');
    showQueued(queuedDetails(selectedFeedTitle(), data));
  } catch (error) {
    setStatus(error.message, 'error');
  } finally {
    shareButton.disabled = false;
  }
});

initialise();
