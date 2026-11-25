const fs = require('fs');
const vm = require('vm');
const assert = require('assert');

let html = '';
function makeDummy(id) {
    return {
        id,
        tagName: 'DIV',
        parentElement: null,
        children: [],
        firstElementChild: null,
        attributes: {},
        dataset: {},
        classList: {
            add(c){ this[c]=true; },
            remove(c){ delete this[c]; },
            contains(c){ return !!this[c]; },
            toggle(c, force){ const on = force === undefined ? !this[c] : !!force; if (on) this[c]=true; else delete this[c]; return on; }
        },
        style: {},
        getAttribute(k){ return this.attributes[k] || null; },
        setAttribute(k, v){ this.attributes[k] = String(v); },
        removeAttribute(k){ delete this.attributes[k]; },
        querySelector(sel){ return null; },
        querySelectorAll(sel){ return []; },
        remove(){},
        appendChild(child){ this.children.push(child); child.parentElement = this; },
        addEventListener(){},
        set innerHTML(v){ html = v; },
        get innerHTML(){ return html; }
    };
}

const feedsContainer = makeDummy('feeds-container');
const feedSearch = Object.assign(makeDummy('feed-search'), { value: '', focus(){}, setSelectionRange(){} });
const feedTypeFilter = Object.assign(makeDummy('feed-type-filter'), { value: 'all' });
const feedsCount = Object.assign(makeDummy('feeds-count'), { innerText: '' });
const filterInfo = makeDummy('feeds-filter-info');
const alertBanner = makeDummy('feeds-connection-alert');
const qrCanvas = makeDummy('qr-canvas');
const qrFeedTitle = makeDummy('qr-feed-title');
const qrFeedURL = makeDummy('qr-feed-url');
const qrCalls = [];
const dummyCache = new Map();
function getOrCreateDummy(id) {
    if (!dummyCache.has(id)) dummyCache.set(id, makeDummy(id));
    return dummyCache.get(id);
}

class QRCodeMock {
    constructor(element, options) {
        this.element = element;
        this.options = options;
        qrCalls.push({ type: 'create', element, options });
    }
    makeCode(value) {
        qrCalls.push({ type: 'update', value });
    }
}
QRCodeMock.CorrectLevel = { M: 0 };

const document = {
    documentElement: { setAttribute(){} },
    getElementById(id) {
        if (id === 'feeds-container') return feedsContainer;
        if (id === 'feed-search') return feedSearch;
        if (id === 'feed-type-filter') return feedTypeFilter;
        if (id === 'feeds-count') return feedsCount;
        if (id === 'feeds-filter-info') return filterInfo;
        if (id === 'feeds-connection-alert') return alertBanner;
        if (id === 'qr-canvas') return qrCanvas;
        if (id === 'qr-feed-title') return qrFeedTitle;
        if (id === 'qr-feed-url') return qrFeedURL;
        return getOrCreateDummy(id);
    },
    createElement(tag) { return makeDummy(tag); },
    addEventListener(){},
    querySelectorAll(){ return []; }
};

const crypto = require('crypto');
const context = {
    document,
    window: { location: { href: 'http://localhost/admin/' }, crypto: crypto.webcrypto },
    bootstrap: { Toast: class { show(){} }, Modal: class { show(){} } },
    QRCode: QRCodeMock,
    URL,
    Uint32Array,
    console,
    setInterval(){ return 1; },
    clearInterval(){},
    setTimeout(){},
    fetch(){},
    confirm(){ return true; },
    navigator: { clipboard: { writeText: async () => {} } },
    localStorage: { getItem(){ return 'mocha'; }, setItem(){} }
};

vm.createContext(context);
const code = fs.readFileSync(require('path').join(__dirname, 'ui/admin.js'), 'utf8').replaceAll('checkAuth();', 'void 0;');
vm.runInContext(code, context);

// Per-provider direct routing disables its proxy URL and preserves the URL when
// switching back to a dedicated proxy.
const originalGetElementById = document.getElementById;
const downloaderElements = new Map();
document.getElementById = id => {
    if (!downloaderElements.has(id)) {
        downloaderElements.set(id, Object.assign(makeDummy(id), {value: '', checked: false, disabled: false}));
    }
    return downloaderElements.get(id);
};
vm.runInContext(`downloaderConfig = {
    timeout: 15,
    proxies: {youtube: 'socks5://127.0.0.1:1080', vkvideo: 'direct'}
}; renderDownloaderConfig();`, context);
assert.equal(downloaderElements.get('dl-proxy-youtube-direct').checked, false);
assert.equal(downloaderElements.get('dl-proxy-youtube').disabled, false);
assert.equal(downloaderElements.get('dl-proxy-vkvideo-direct').checked, true);
assert.equal(downloaderElements.get('dl-proxy-vkvideo').disabled, true);
assert.equal(downloaderElements.get('dl-proxy-rutube-direct').checked, true);
vm.runInContext(`toggleProviderProxyInput('youtube', true)`, context);
assert.equal(downloaderElements.get('dl-proxy-youtube').disabled, true);
vm.runInContext(`toggleProviderProxyInput('youtube', false)`, context);
assert.equal(downloaderElements.get('dl-proxy-youtube').value, 'socks5://127.0.0.1:1080');
assert.equal(vm.runInContext(`providerProxyValue('youtube')`, context), 'socks5://127.0.0.1:1080');
vm.runInContext(`toggleProviderProxyInput('youtube', true)`, context);
assert.equal(vm.runInContext(`providerProxyValue('youtube')`, context), 'direct');
document.getElementById = originalGetElementById;

// QR generation uses the locally bundled MIT QRCode.js, with no CDN request.
vm.runInContext(`showQRCode('Test feed', 'https://podsync.local/test.xml')`, context);
assert.equal(qrCalls.length, 1);
assert.equal(qrCalls[0].type, 'create');
assert.equal(qrCalls[0].element, qrCanvas);
assert.equal(qrCalls[0].options.text, 'https://podsync.local/test.xml');
assert.equal(qrCalls[0].options.correctLevel, QRCodeMock.CorrectLevel.M);
vm.runInContext(`showQRCode('Updated feed', 'https://podsync.local/updated.xml')`, context);
assert.deepEqual(qrCalls[1], { type: 'update', value: 'https://podsync.local/updated.xml' });
const adminHTML = fs.readFileSync(require('path').join(__dirname, 'ui/index.html'), 'utf8');
assert(adminHTML.includes('./qrcode.min.js?v=1'), 'Missing local QRCode.js asset');
assert(!adminHTML.includes('qrious'), 'QRious CDN reference remains in the admin UI');
assert(fs.existsSync(require('path').join(__dirname, 'ui/qrcodejs.LICENSE')), 'Missing bundled QRCode.js license');
assert(adminHTML.includes('./favicon.ico?v=1'), 'Missing local favicon reference');
assert(fs.existsSync(require('path').join(__dirname, 'ui/favicon.ico')), 'Missing bundled favicon');

// Test 1: Empty library
vm.runInContext('allFeeds = []; renderFeeds([]);', context);
assert(html.includes('No configured podcasts'), 'Empty library state failed');

// Test 2: Search filter with no matches shows Reset filters
vm.runInContext('allFeeds = [{id: "feed1", title: "Pod 1"}]; document.getElementById("feed-search").value = "nonexistent"; renderFeeds(getFilteredFeeds());', context);
assert(html.includes('No matching podcasts'), 'Empty filter state failed');
assert(html.includes('data-ui-action="reset-filters"'), 'Missing reset-filters action');

// Test 3: Reset filters
vm.runInContext('resetFeedFilters()', context);
assert.equal(feedSearch.value, '');
assert.equal(feedTypeFilter.value, 'all');

// Test 4: Filter matching
vm.runInContext('allFeeds = [{id: "tech1", title: "Tech Podcast", provider: "youtube"}, {id: "book1", title: "Audiobook 1", provider: "audiobook"}];', context);
feedSearch.value = 'tech';
let filtered = vm.runInContext('getFilteredFeeds()', context);
assert.equal(filtered.length, 1);
assert.equal(filtered[0].id, 'tech1');

feedSearch.value = '';
feedTypeFilter.value = 'audiobooks';
filtered = vm.runInContext('getFilteredFeeds()', context);
assert.equal(filtered.length, 1);
assert.equal(filtered[0].id, 'book1');

assert.equal(filtered[0].id, 'book1');

// Test 4b: Personal feeds filter matching and rendering
vm.runInContext('allFeeds = [{id: "tech1", title: "Tech Podcast", provider: "youtube"}, {id: "mypod", title: "My Personal Feed", provider: "personal"}];', context);
feedSearch.value = '';
feedTypeFilter.value = 'personal';
filtered = vm.runInContext('getFilteredFeeds()', context);
assert.equal(filtered.length, 1);
assert.equal(filtered[0].id, 'mypod');

const personalCardHtml = vm.runInContext('renderFeedCardHTML({id: "mypod", title: "My Personal Feed", provider: "personal", episode_count: 5})', context);
assert(personalCardHtml.includes('data-ui-action="personal-open"'), "Missing personal-open action on card");
assert(personalCardHtml.includes('Universal'), "Missing Universal badge on personal card");
assert(personalCardHtml.includes('Personal'), "Missing Personal provider on personal card");


// Test 5: Error badge rendering and clickable actions
const cardHtml = vm.runInContext('renderFeedCardHTML({id: "errpod", title: "Error Podcast", error_count: 3, total_size: 1024, downloaded_count: 5, episode_count: 8})', context);
assert(cardHtml.includes('data-ui-action="errors"'), "Missing data-ui-action=errors on error badge");
assert(cardHtml.includes('data-ui-arg1="errpod"'), "Missing data-ui-arg1=errpod on error badge");
assert(cardHtml.includes('role="button"'), "Missing role=button on error badge");
assert(cardHtml.includes('3 errors'), "Missing error count text in badge");
assert(cardHtml.includes('Click to view error log'), "Missing error badge tooltip");
assert.equal(typeof context.showFeedErrors, "function", "showFeedErrors must be a function");
assert.equal(typeof context.retryFeedError, "function", "retryFeedError must be a function");
assert.equal(typeof context.retryAllErrorsForFeed, "function", "retryAllErrorsForFeed must be a function");
assert.equal(typeof context.saveNetworkSettings, "function", "Network DNS settings save handler must be available");
assert.equal(typeof context.loadPodsyncUpdate, "function", "Podsync update status loader must be available");
assert.equal(typeof context.updatePodsync, "function", "Podsync update action must be available");

// Test 6: Change password generator and visibility reset
const newPwEl = document.getElementById('input-new-pw');
const confirmPwEl = document.getElementById('input-confirm-pw');
newPwEl.value = '';
newPwEl.type = 'password';
confirmPwEl.value = '';
confirmPwEl.type = 'password';

assert.equal(typeof context.generateAdminPassword, 'function', 'generateAdminPassword must be defined');
assert.equal(typeof context.resetChangePasswordVisibility, 'function', 'resetChangePasswordVisibility must be defined');
context.generateAdminPassword();

assert.equal(newPwEl.value.length, 20, 'Generated password length should be 20');
assert.equal(newPwEl.value, confirmPwEl.value, 'New and Confirm password values must match');
assert.equal(newPwEl.type, 'text', 'New password field should be revealed after generation');
assert.equal(confirmPwEl.type, 'text', 'Confirm password field should be revealed after generation');
assert(/[a-z]/.test(newPwEl.value), 'Password must contain lowercase');
assert(/[A-Z]/.test(newPwEl.value), 'Password must contain uppercase');
assert(/[0-9]/.test(newPwEl.value), 'Password must contain digits');
assert(/[!@#$%^&*()_+= -]/.test(newPwEl.value), 'Password must contain symbols');

context.resetChangePasswordVisibility();
assert.equal(newPwEl.type, 'password', 'Password type should reset to password');
assert.equal(confirmPwEl.type, 'password', 'Confirm password type should reset to password');

console.log('Password generator and visibility tests passed!');

console.log('UI search, filter, and rendering tests passed!');

// Status filters include feed-level failures even when no episode has failed yet.
feedTypeFilter.value = 'all';
feedSearch.value = '';
vm.runInContext(`allFeeds = [
    {id:'alpha',title:'Alpha',total_size:10,last_update:'2026-01-01'},
    {id:'beta',title:'Beta',total_size:30,last_update:'2026-02-01',last_error:'API unavailable'},
    {id:'gamma',title:'Gamma',total_size:20,error_count:2}
]; updatingFeeds.add('alpha'); feedStatusFilter = 'errors';`, context);
assert.deepEqual(Array.from(vm.runInContext('getFilteredFeeds().map(f => f.id)', context)), ['beta','gamma']);
vm.runInContext("feedStatusFilter = 'updating'", context);
assert.deepEqual(Array.from(vm.runInContext('getFilteredFeeds().map(f => f.id)', context)), ['alpha']);
vm.runInContext("feedStatusFilter = 'all'", context);

// A response whose JSON arrives late must never overwrite a newer library snapshot.
async function testFeedResponseOrdering() {
    let resolveOldJSON;
    context.fetch = async () => ({ok:true, json:() => new Promise(resolve => {resolveOldJSON=resolve;})});
    const oldRequest = context.loadFeeds();
    await new Promise(setImmediate);
    context.fetch = async () => ({ok:true, json:async () => [{id:'latest',title:'Latest'}]});
    await context.loadFeeds();
    resolveOldJSON([{id:'stale',title:'Stale'}]);
    await oldRequest;
    assert.equal(vm.runInContext('allFeeds[0].id',context),'latest');
    console.log('Library status filters and response ordering tests passed');
}
// Telegram configuration UI states, token handling, and save errors.
async function testTelegramUI() {
    const telegramElements = new Map();
    document.getElementById = id => {
        if (!telegramElements.has(id)) {
            telegramElements.set(id, Object.assign(makeDummy(id), {
                value: '', checked: false, disabled: false, textContent: '', className: ''
            }));
        }
        return telegramElements.get(id);
    };
    const el = id => telegramElements.get(id);
    const requests = [];
    let response = { ok: true, data: { enabled: false, connected: false, token_configured: false, token_source: 'none', running: false } };
    context.fetch = async (url, options = {}) => {
        requests.push({ url, method: options.method || 'GET', body: options.body ? JSON.parse(options.body) : null });
        return { ok: response.ok !== false, json: async () => response.data };
    };

    await vm.runInContext('loadTelegram()', context);
    assert.equal(el('telegram-enabled').checked, false, 'disabled bot must leave the switch off');
    assert.equal(el('telegram-token').disabled, false, 'token input is editable without an environment token');
    assert.equal(el('telegram-remove-token-btn').disabled, true, 'remove is unavailable without a stored token');
    assert.equal(el('telegram-pair-btn').disabled, true, 'pairing requires an enabled bot');
    assert.equal(el('telegram-status-badge').textContent, 'Disabled');
    assert.equal(el('telegram-allowed-user-id').value, '0');
    assert.match(el('telegram-allowed-hint').textContent, /No user restriction/i);

    vm.runInContext(`renderTelegramStatus({enabled:false,connected:false,token_configured:true,token_source:'env',running:false})`, context);
    assert.equal(el('telegram-token').disabled, true, 'environment token must be read-only');
    assert.equal(el('telegram-remove-token-btn').disabled, true, 'environment token cannot be removed');
    assert.match(el('telegram-token-hint').textContent, /environment/i);
    el('telegram-enabled').checked = true;
    response = { ok: true, data: { enabled: true, connected: false, token_configured: true, token_source: 'env', running: true } };
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    assert.deepEqual(requests[requests.length - 1].body, { enabled: true, allowed_user_id: 0, proxy: '' }, 'environment token must not be sent from the form');

    vm.runInContext(`renderTelegramStatus({enabled:true,connected:false,token_configured:true,token_source:'config',running:false,last_error:'telegram token check failed: Unauthorized'})`, context);
    assert.equal(el('telegram-status-badge').textContent, 'Invalid token');
    assert.match(el('telegram-error').textContent, /Unauthorized/);

    vm.runInContext(`renderTelegramStatus({enabled:true,connected:true,user_id:123,allowed_user_id:123,token_configured:true,token_source:'config',running:true})`, context);
    assert.equal(el('telegram-enabled').checked, true);
    assert.equal(el('telegram-pair-btn').disabled, true, 'a connected owner cannot be paired again');
    assert.equal(el('telegram-test-btn').disabled, false, 'test works for a connected owner');
    assert.equal(el('telegram-status-badge').textContent, 'Connected');
    assert.equal(el('telegram-allowed-user-id').value, '123');
    assert.equal(el('telegram-owner-user-id').textContent, '123');
    assert.equal(el('telegram-owner-row').classList.contains('d-none'), false);
    assert.match(el('telegram-allowed-hint').textContent, /Only Telegram user ID 123/);

    vm.runInContext(`renderTelegramStatus({enabled:true,connected:false,token_configured:true,token_source:'config',running:true})`, context);
    assert.equal(el('telegram-pair-btn').disabled, false, 'ready bot can be paired');
    assert.equal(el('telegram-test-btn').disabled, true, 'test requires a connected owner');

    el('telegram-enabled').checked = true;
    el('telegram-token').value = '123:secret-token';
    response = { ok: true, data: { enabled: true, connected: false, token_configured: true, token_source: 'config', running: true } };
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    const saveRequest = requests.filter(r => r.method === 'PUT').pop();
    assert.deepEqual(saveRequest.body, { enabled: true, token: '123:secret-token', allowed_user_id: 0, proxy: '' });
    assert.equal(el('telegram-token').value, '', 'token input is cleared after a successful save');
    assert.equal(el('telegram-status-badge').textContent, 'Ready to connect');

    el('telegram-enabled').checked = true;
    el('telegram-token').value = 'bad-token';
    response = { ok: false, data: { error: 'telegram token check failed: Unauthorized' } };
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    assert.equal(el('telegram-token').value, 'bad-token', 'failed save must keep the entered token');
    assert.equal(el('telegram-status-badge').textContent, 'Cannot save');
    assert.match(el('telegram-error').textContent, /Unauthorized/);
    assert.equal(el('telegram-error').classList.contains('d-none'), false, 'save error must be visible');

    vm.runInContext(`renderTelegramStatus({enabled:true,connected:true,user_id:123,allowed_user_id:0,token_configured:true,token_source:'config',running:true})`, context);
    el('telegram-allowed-user-id').value = '456';
    const requestsBeforeDecline = requests.length;
    context.confirm = () => false;
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    assert.equal(requests.length, requestsBeforeDecline, 'declining the owner-change warning must not save');
    assert.equal(el('telegram-token').value, 'bad-token');
    context.confirm = () => true;
    el('telegram-token').value = '';
    response = { ok: true, data: { enabled: true, connected: false, user_id: 0, allowed_user_id: 456, token_configured: true, token_source: 'config', running: true } };
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    const allowedRequest = requests.filter(r => r.method === 'PUT').pop();
    assert.deepEqual(allowedRequest.body, { enabled: true, allowed_user_id: 456, proxy: '' }, 'user ID change must be saved');
    assert.match(el('telegram-allowed-hint').textContent, /Only Telegram user ID 456/);

    el('telegram-proxy').value = 'socks5://127.0.0.1:1080';
    el('telegram-allowed-user-id').value = '456';
    response = { ok: true, data: { enabled: true, connected: false, user_id: 0, allowed_user_id: 456, proxy: 'socks5://127.0.0.1:1080', token_configured: true, token_source: 'config', running: true } };
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    const proxyRequest = requests.filter(r => r.method === 'PUT').pop();
    assert.equal(proxyRequest.body.proxy, 'socks5://127.0.0.1:1080', 'proxy must be saved');
    assert.equal(el('telegram-proxy').value, 'socks5://127.0.0.1:1080', 'saved proxy is rendered back');

    el('telegram-allowed-user-id').value = '-1';
    const beforeInvalid = requests.length;
    await vm.runInContext(`saveTelegram({preventDefault(){}})`, context);
    assert.equal(requests.length, beforeInvalid, 'invalid user IDs must not be saved');
    assert.match(el('telegram-error').textContent, /non-negative whole number/);

    assert(adminHTML.includes('id="telegram-config-form"'), 'Missing Telegram configuration form');
    assert(adminHTML.includes('id="telegram-status-badge"'), 'Missing Telegram status badge');
    document.getElementById = originalGetElementById;
    console.log('Telegram configuration UI tests passed!');
}

async function testYtdlpVersionUI() {
    const elements = new Map();
    document.getElementById = id => {
        if (!elements.has(id)) {
            elements.set(id, Object.assign(makeDummy(id), { disabled: false, textContent: '', className: '' }));
        }
        return elements.get(id);
    };
    const el = id => elements.get(id);
    context.fetch = async () => ({
        ok: true,
        json: async () => ({
            current_version: '2026.10.26',
            latest_version: '2026.11.23',
            update_available: true,
            release_url: 'https://github.com/yt-dlp/yt-dlp/releases/tag/2026.11.23',
            self_update: false,
            message: 'yt-dlp 2026.11.23 is available; installed 2026.10.26.'
        })
    });
    await vm.runInContext('checkYtdlpVersion()', context);
    assert.match(el('ytdlp-version-result').innerHTML, /Installed <code>2026\.10\.26<\/code>/);
    assert.match(el('ytdlp-version-result').innerHTML, /Latest <code>2026\.11\.23<\/code>/);
    assert.match(el('ytdlp-version-result').innerHTML, /Automatic updates: <strong>off/);
    assert.match(el('ytdlp-version-result').innerHTML, /Release notes/);
    assert.equal(el('ytdlp-version-result').className, 'small text-warning');
    assert.equal(el('btn-check-ytdlp').disabled, false, 'check button must be usable again');

    context.fetch = async () => ({ ok: false, json: async () => ({ error: 'GitHub is unreachable' }) });
    await vm.runInContext('checkYtdlpVersion()', context);
    assert.equal(el('ytdlp-version-result').textContent, 'GitHub is unreachable');
    assert.equal(el('ytdlp-version-result').className, 'small text-danger');
    document.getElementById = originalGetElementById;
    console.log('yt-dlp version check UI tests passed!');
}

testYtdlpVersionUI().then(testTelegramUI).then(testFeedResponseOrdering).catch(err=>{console.error(err);process.exitCode=1;});
