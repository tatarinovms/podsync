// Dependency-free tests for the Podsync Share Firefox extension.
// Run from the repository root: node extensions/podsync-firefox/tests/popup_test.js
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');

const root = path.join(__dirname, '..');
const lib = require(path.join(root, 'lib.js'));

// --- Provider URL validation -------------------------------------------------
const supported = [
  'https://www.youtube.com/watch?v=abc123',
  'https://youtube.com/watch?v=abc123',
  'https://m.youtube.com/watch?v=abc123',
  'https://youtu.be/abc123',
  'https://vk.com/video-196446247_456239017',
  'https://vk.ru/video-1_2',
  'https://m.vk.com/video-1_2',
  'https://vkvideo.ru/video-1_2',
  'https://www.vkvideo.ru/video-1_2',
  'https://rutube.ru/video/abc123/',
  'http://vk.com/video-1_2'
];
for (const url of supported) {
  assert.equal(lib.isSupportedURL(url), true, `${url} should be accepted`);
}

const rejected = [
  'https://vimeo.com/12345',
  'https://soundcloud.com/artist/track',
  'https://notyoutube.com/watch?v=abc',
  'https://youtube.com.evil.example/watch?v=abc',
  'https://myvk.com/video-1_2',
  'https://vk.com.evil.example/video-1_2',
  'javascript:alert(1)',
  'about:blank',
  'not a url',
  ''
];
for (const url of rejected) {
  assert.equal(lib.isSupportedURL(url), false, `${url} should be rejected`);
}

// The source URL keeps its path and query but drops the fragment.
assert.equal(
  lib.normaliseSourceURL('https://vk.com/video-1_2?t=30#comments'),
  'https://vk.com/video-1_2?t=30'
);
assert.equal(
  lib.normaliseSourceURL('  https://youtu.be/abc123?t=45  '),
  'https://youtu.be/abc123?t=45'
);
assert.throws(() => lib.normaliseSourceURL('javascript:alert(1)'), /HTTP and HTTPS/);

// --- API base URL normalisation ----------------------------------------------
assert.equal(lib.apiBase('https://podcast.example.com/api/v1'), 'https://podcast.example.com/api/v1');
assert.equal(lib.apiBase('https://podcast.example.com/api/v1/'), 'https://podcast.example.com/api/v1');
assert.equal(lib.apiBase('https://podcast.example.com/'), 'https://podcast.example.com/api/v1');
assert.equal(lib.apiBase('https://podcast.example.com/podsync/api/v1/?x=1#y'), 'https://podcast.example.com/podsync/api/v1');
assert.equal(lib.apiBase('http://localhost:8080/api/v1'), 'http://localhost:8080/api/v1');
assert.equal(lib.apiBase('http://127.0.0.1:8080'), 'http://127.0.0.1:8080/api/v1');
assert.throws(() => lib.apiBase('http://[::1]:8080/api/v1'), /HTTPS/);
assert.throws(() => lib.apiBase('http://podcast.example.com/api/v1'), /HTTPS/);
assert.throws(() => lib.apiBase('ftp://podcast.example.com/api/v1'), /HTTPS/);
assert.equal(lib.serverOrigin('https://podcast.example.com/api/v1'), 'https://podcast.example.com');
assert.equal(lib.serverOrigin('http://localhost:8080/api/v1'), 'http://localhost:8080');

// --- Credentials with non-ASCII characters ------------------------------------
const utf8Base64 = value => Buffer.from(value, 'utf8').toString('base64');
assert.equal(lib.authorization({ username: 'admin', password: 'secret' }), `Basic ${utf8Base64('admin:secret')}`);
assert.equal(lib.authorization({ username: 'admin', password: 'pässwörd' }), `Basic ${utf8Base64('admin:pässwörd')}`);
assert.equal(lib.authorization({ username: 'админ', password: 'секрет' }), `Basic ${utf8Base64('админ:секрет')}`);
assert.equal(lib.authorization({ username: 'admin', password: 'p@ss word:1' }), `Basic ${utf8Base64('admin:p@ss word:1')}`);

// --- Format and resolution selection -----------------------------------------
const tab = { url: 'https://youtube.com/watch?v=abc#t=10', title: '  Episode title  ' };
assert.deepEqual(lib.episodePayload(tab, { format: 'audio', quality: 'low', maxHeight: '1080' }), {
  url: 'https://youtube.com/watch?v=abc',
  title: 'Episode title',
  format: 'audio',
  quality: 'low',
  max_height: 0
});
assert.deepEqual(lib.episodePayload(tab, { format: 'video', quality: 'high', maxHeight: '720' }), {
  url: 'https://youtube.com/watch?v=abc',
  title: 'Episode title',
  format: 'video',
  quality: 'high',
  max_height: 720
});
assert.equal(lib.episodePayload(tab, { format: 'video', maxHeight: '999' }).max_height, 0);
assert.equal(lib.episodePayload(tab, { format: 'video', maxHeight: '0' }).max_height, 0);
assert.equal(lib.episodePayload(tab, { format: 'unsupported', quality: 'unsupported' }).format, 'audio');
assert.equal(lib.episodePayload(tab, { format: 'unsupported' }).quality, 'high');
assert.throws(() => lib.episodePayload({ url: 'javascript:alert(1)', title: '' }, {}), /HTTP and HTTPS/);

// --- Queued confirmation details ---------------------------------------------
assert.deepEqual(lib.queuedDetails('My Feed', { id: 'ep-1', title: 'A video' }), {
  feed: 'My Feed',
  episode: 'ep-1',
  title: 'A video'
});
assert.deepEqual(lib.queuedDetails('', { episode_id: 'ep-2' }), {
  feed: 'Personal feed',
  episode: 'ep-2',
  title: ''
});

// --- API request handling -----------------------------------------------------
const config = { apiURL: 'https://podsync.example.com/api/v1', username: 'admin', password: 'pässwörd' };

async function testAPIRequests() {
  const calls = [];
  let response = { ok: true, status: 200, json: async () => ({ id: 'ep-1' }) };
  global.fetch = async (url, options) => {
    calls.push({ url, options });
    return response;
  };

  const data = await lib.request(config, '/personal-feeds');
  assert.equal(calls[0].url, 'https://podsync.example.com/api/v1/personal-feeds');
  assert.equal(calls[0].options.headers.Authorization, lib.authorization(config));
  assert.equal(data.id, 'ep-1');

  response = { ok: false, status: 401, json: async () => ({ error: 'invalid credentials' }) };
  await assert.rejects(lib.request(config, '/personal-feeds'), /invalid credentials/);

  response = { ok: false, status: 502, json: async () => { throw new Error('not JSON'); } };
  await assert.rejects(lib.request(config, '/personal-feeds'), /HTTP 502/);

  global.fetch = async () => { throw new Error('network unreachable'); };
  await assert.rejects(lib.request(config, '/personal-feeds'), /network unreachable/);
}

// --- Popup behaviour with a mocked browser -----------------------------------
class ClassList {
  constructor() { this.values = new Set(); }
  add(...names) { names.forEach(name => this.values.add(name)); }
  remove(...names) { names.forEach(name => this.values.delete(name)); }
  contains(name) { return this.values.has(name); }
  has(name) { return this.values.has(name); }
  toggle(name, force) {
    const next = force === undefined ? !this.values.has(name) : Boolean(force);
    if (next) this.values.add(name); else this.values.delete(name);
    return next;
  }
}

class Option {
  constructor(text, value, defaultSelected, selected) {
    this.text = text;
    this.textContent = text;
    this.value = value;
    this.defaultSelected = Boolean(defaultSelected);
    this.selected = Boolean(selected);
  }
}

class Element {
  constructor(id) {
    this.id = id;
    this._value = '';
    this.textContent = '';
    this.disabled = false;
    this.options = [];
    this.selectedIndex = 0;
    this.listeners = {};
    this.classes = new ClassList();
    this.focused = false;
  }
  get classList() { return this.classes; }
  get value() {
    const option = this.options[this.selectedIndex];
    return option ? option.value : this._value;
  }
  set value(value) {
    const index = this.options.findIndex(option => option.value === value);
    if (index >= 0) this.selectedIndex = index; else this._value = value;
  }
  addEventListener(type, handler) { (this.listeners[type] ||= []).push(handler); }
  replaceChildren() { this.options = []; this.selectedIndex = 0; }
  add(option) {
    this.options.push(option);
    if (option.selected) this.selectedIndex = this.options.length - 1;
  }
  focus() { this.focused = true; }
}

function createContext() {
  const elements = new Map();
  const getElement = id => {
    if (!elements.has(id)) elements.set(id, new Element(id));
    return elements.get(id);
  };
  const storage = {
    apiURL: 'https://podsync.example.com/api/v1',
    username: 'admin',
    password: 'pässwörd',
    feedID: 'feed-1',
    format: 'audio',
    quality: 'high',
    maxHeight: 0
  };
  const storageWrites = [];
  const tabState = { url: 'https://vk.com/video-196446247_456239017?t=45#comments', title: '  VK video  ' };
  const permissions = { granted: true, requests: [] };
  const browser = {
    storage: {
      local: {
        get: async defaults => ({ ...defaults, ...storage }),
        set: async patch => { Object.assign(storage, patch); storageWrites.push(patch); }
      }
    },
    permissions: {
      contains: async request => {
        assert.deepEqual(Array.from(request.origins), ['https://podsync.example.com/*']);
        return permissions.granted;
      },
      request: async request => {
        permissions.requests.push(Array.from(request.origins));
        return permissions.granted;
      }
    },
    tabs: { query: async () => [tabState] }
  };
  const windowMock = { closed: false, close() { this.closed = true; } };
  const requests = [];
  let responseFor = ({ url, options }) => {
    if (url.endsWith('/personal-feeds') && !options.method) {
      return { ok: true, status: 200, json: async () => [{ id: 'feed-1', title: 'My Feed' }, { id: 'feed-2', title: 'Second Feed' }] };
    }
    if (url.endsWith('/personal-feeds/feed-1/episodes') && options.method === 'POST') {
      return { ok: true, status: 202, json: async () => ({ id: 'episode-1', title: 'VK video' }) };
    }
    return { ok: false, status: 404, json: async () => ({ error: 'unexpected request' }) };
  };
  const fetch = async (url, options = {}) => {
    const call = { url, options };
    requests.push(call);
    return responseFor(call);
  };
  const context = vm.createContext({
    console,
    URL,
    TextEncoder,
    btoa,
    setTimeout,
    clearTimeout,
    Option,
    browser,
    document: { getElementById: getElement },
    window: windowMock,
    fetch
  });
  vm.runInContext(fs.readFileSync(path.join(root, 'lib.js'), 'utf8'), context, { filename: 'lib.js' });
  vm.runInContext(fs.readFileSync(path.join(root, 'popup.js'), 'utf8'), context, { filename: 'popup.js' });
  return {
    getElement,
    elements,
    storage,
    storageWrites,
    tabState,
    permissions,
    requests,
    windowMock,
    setResponse(handler) { responseFor = handler; }
  };
}

const settle = () => new Promise(resolve => setTimeout(resolve, 0));

async function testPopup() {
  const harness = createContext();
  await settle();
  await settle();

  const feedSelect = harness.getElement('feed');
  assert.equal(feedSelect.options.length, 2, 'feeds should be loaded from Podsync');
  assert.equal(feedSelect.value, 'feed-1', 'the saved feed should be selected');
  assert.equal(feedSelect.disabled, false);
  assert.equal(harness.getElement('share').disabled, false);

  // Unsupported pages are blocked before any API call.
  harness.tabState.url = 'https://vimeo.com/12345';
  const requestsBefore = harness.requests.length;
  await harness.getElement('open-send-settings').listeners.click[0]();
  assert.match(harness.getElement('status').textContent, /Only YouTube, VK Video, and Rutube/);
  assert.equal(harness.getElement('status').className, 'error');
  assert.equal(harness.requests.length, requestsBefore, 'no API request is made for unsupported pages');

  // Supported VK pages open the send settings and can be queued.
  harness.tabState.url = 'https://vk.com/video-196446247_456239017?t=45#comments';
  await harness.getElement('open-send-settings').listeners.click[0]();
  assert.equal(harness.getElement('send-settings').classes.has('hidden'), false);

  harness.getElement('format').value = 'video';
  harness.getElement('quality').value = 'low';
  harness.getElement('max-height').value = '720';
  await harness.getElement('share-form').listeners.submit[0]({ preventDefault() {} });

  const post = harness.requests.find(call => call.options.method === 'POST');
  assert.deepEqual(JSON.parse(post.options.body), {
    url: 'https://vk.com/video-196446247_456239017?t=45',
    title: 'VK video',
    format: 'video',
    quality: 'low',
    max_height: 720
  });
  assert.equal(post.options.headers.Authorization, lib.authorization({
    username: 'admin',
    password: 'pässwörd'
  }));

  assert.equal(harness.getElement('result-feed').textContent, 'My Feed');
  assert.equal(harness.getElement('result-episode').textContent, 'episode-1');
  assert.equal(harness.getElement('result-title').textContent, 'VK video');
  assert.equal(harness.getElement('send-result').classes.has('hidden'), false);
  assert.equal(harness.getElement('share-form').classes.has('hidden'), true, 'the send form is replaced by the confirmation');
  assert.equal(harness.getElement('close-popup').focused, true);
  assert.equal(harness.getElement('share').disabled, false, 'the button is usable again');
  assert.equal(harness.getElement('status').textContent, '');
  assert.equal(harness.getElement('status').className, '');

  // The popup must not close itself after a successful queue operation.
  await new Promise(resolve => setTimeout(resolve, 600));
  assert.equal(harness.windowMock.closed, false, 'the popup stays open until the user closes it');

  // API failures are surfaced verbatim and keep the popup usable.
  harness.setResponse(() => ({ ok: false, status: 403, json: async () => ({ error: 'forbidden by Podsync' }) }));
  await harness.getElement('open-send-settings').listeners.click[0]();
  await harness.getElement('share-form').listeners.submit[0]({ preventDefault() {} });
  assert.equal(harness.getElement('status').textContent, 'forbidden by Podsync');
  assert.equal(harness.getElement('status').className, 'error');
  assert.equal(harness.getElement('send-result').classes.has('hidden'), true);
  assert.equal(harness.getElement('share').disabled, false);

  // Denied optional host permission is reported instead of saving silently.
  harness.permissions.granted = false;
  await harness.getElement('settings-form').listeners.submit[0]({ preventDefault() {} });
  assert.match(harness.getElement('status').textContent, /Server access for https:\/\/podsync\.example\.com was not granted/);
  assert.deepEqual(harness.permissions.requests, [['https://podsync.example.com/*']]);
}

// --- Manifest and markup ------------------------------------------------------
function testManifest() {
  const manifest = JSON.parse(fs.readFileSync(path.join(root, 'manifest.json'), 'utf8'));
  assert.equal(manifest.manifest_version, 3);
  assert.deepEqual([...manifest.permissions].sort(), ['activeTab', 'storage']);
  assert.equal(manifest.host_permissions, undefined, 'no permanent host permissions');
  assert.equal(JSON.stringify(manifest).includes('<all_urls>'), false);
  assert.deepEqual(manifest.optional_host_permissions, [
    'https://*/*',
    'http://localhost/*',
    'http://127.0.0.1/*'
  ]);
  assert.equal(manifest.browser_specific_settings.gecko.strict_min_version, '140.0');
  assert.deepEqual(
    manifest.browser_specific_settings.gecko.data_collection_permissions.required,
    ['authenticationInfo', 'browsingActivity']
  );

  const html = fs.readFileSync(path.join(root, 'popup.html'), 'utf8');
  const libIndex = html.indexOf('src="lib.js"');
  const popupIndex = html.indexOf('src="popup.js"');
  assert.ok(libIndex >= 0, 'popup.html loads lib.js');
  assert.ok(popupIndex > libIndex, 'lib.js is loaded before popup.js');
  for (const id of ['send-result', 'result-feed', 'result-episode', 'close-popup']) {
    assert.ok(html.includes(`id="${id}"`), `popup.html contains #${id}`);
  }
}

(async () => {
  testManifest();
  await testAPIRequests();
  await testPopup();
  console.log('Podsync Share extension tests passed.');
})().catch(error => {
  console.error(error);
  process.exitCode = 1;
});
