// Shared helpers for the Podsync Share extension.
// Loaded before popup.js in Firefox and required directly by the extension tests.
(function (global) {
  'use strict';

  const SUPPORTED_DOMAINS = ['youtube.com', 'youtu.be', 'vkvideo.ru', 'vk.com', 'vk.ru', 'rutube.ru'];
  const VIDEO_HEIGHTS = [0, 480, 720, 1080, 1440, 2160];
  const LOCAL_HOSTS = ['localhost', '127.0.0.1'];

  function parseURL(value) {
    return new URL(String(value).trim());
  }

  function isHTTP(url) {
    return url.protocol === 'https:' || url.protocol === 'http:';
  }

  // Returns the tab URL without its fragment so Podsync receives a clean source link.
  function normaliseSourceURL(value) {
    const url = parseURL(value);
    if (!isHTTP(url)) throw new Error('Only HTTP and HTTPS links can be sent to Podsync.');
    url.hash = '';
    return url.toString();
  }

  function isSupportedURL(value) {
    let url;
    try {
      url = parseURL(value);
    } catch (error) {
      return false;
    }
    if (!isHTTP(url)) return false;
    const host = url.hostname.toLowerCase().replace(/^www\./, '');
    return SUPPORTED_DOMAINS.some(domain => host === domain || host.endsWith(`.${domain}`));
  }

  function isLocalHost(hostname) {
    return LOCAL_HOSTS.includes(String(hostname).toLowerCase());
  }

  // Normalises the configured API URL through URL fields, so trailing slashes,
  // query strings, fragments and an optional /api/v1 suffix are handled correctly.
  function apiBase(value) {
    const url = parseURL(value);
    const secure = url.protocol === 'https:';
    if (!secure && !(url.protocol === 'http:' && isLocalHost(url.hostname))) {
      throw new Error('Use HTTPS for a remote Podsync server.');
    }
    url.search = '';
    url.hash = '';
    const path = url.pathname.replace(/\/+$/, '').replace(/\/api\/v1$/, '');
    return `${url.origin}${path}/api/v1`;
  }

  function serverOrigin(value) {
    return new URL(apiBase(value)).origin;
  }

  // Firefox's btoa only accepts Latin-1, so encode the credentials as UTF-8 first.
  function base64Encode(text) {
    const bytes = new TextEncoder().encode(String(text));
    let binary = '';
    for (const byte of bytes) binary += String.fromCharCode(byte);
    return btoa(binary);
  }

  function authorization(config) {
    return `Basic ${base64Encode(`${config.username}:${config.password}`)}`;
  }

  function episodePayload(tab, options = {}) {
    const format = options.format === 'video' ? 'video' : 'audio';
    const quality = options.quality === 'low' ? 'low' : 'high';
    const requestedHeight = Number(options.maxHeight);
    const maxHeight = VIDEO_HEIGHTS.includes(requestedHeight) ? requestedHeight : 0;
    return {
      url: normaliseSourceURL(tab.url),
      title: String(tab.title || '').trim(),
      format,
      quality,
      max_height: format === 'video' ? maxHeight : 0
    };
  }

  function queuedDetails(feedTitle, data = {}) {
    return {
      feed: String(feedTitle || '').trim() || 'Personal feed',
      episode: String(data.id || data.episode_id || '').trim(),
      title: String(data.title || '').trim()
    };
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

  const api = {
    SUPPORTED_DOMAINS,
    VIDEO_HEIGHTS,
    apiBase,
    authorization,
    base64Encode,
    episodePayload,
    isSupportedURL,
    normaliseSourceURL,
    queuedDetails,
    request,
    serverOrigin
  };

  global.PodsyncShare = api;
  if (typeof module !== 'undefined' && module.exports) module.exports = api;
})(typeof globalThis !== 'undefined' ? globalThis : this);
