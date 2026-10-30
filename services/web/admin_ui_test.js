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
            contains(c){ return !!this[c]; }
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

const document = {
    documentElement: { setAttribute(){} },
    getElementById(id) {
        if (id === 'feeds-container') return feedsContainer;
        if (id === 'feed-search') return feedSearch;
        if (id === 'feed-type-filter') return feedTypeFilter;
        if (id === 'feeds-count') return feedsCount;
        if (id === 'feeds-filter-info') return filterInfo;
        if (id === 'feeds-connection-alert') return alertBanner;
        return makeDummy(id);
    },
    createElement(tag) { return makeDummy(tag); },
    addEventListener(){},
    querySelectorAll(){ return []; }
};

const context = {
    document,
    window: { location: { href: 'http://localhost/admin/' } },
    bootstrap: { Toast: class{}, Modal: class{} },
    URL,
    console,
    setInterval(){ return 1; },
    clearInterval(){},
    setTimeout(){},
    fetch(){},
    navigator: {},
    localStorage: { getItem(){ return 'mocha'; }, setItem(){} }
};

vm.createContext(context);
const code = fs.readFileSync(require('path').join(__dirname, 'ui/admin.js'), 'utf8').replaceAll('checkAuth();', 'void 0;');
vm.runInContext(code, context);

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
testFeedResponseOrdering().catch(err=>{console.error(err);process.exitCode=1;});
