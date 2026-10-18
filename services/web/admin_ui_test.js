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

console.log('UI search, filter, and rendering tests passed!');
