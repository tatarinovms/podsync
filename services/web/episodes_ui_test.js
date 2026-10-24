const fs = require('node:fs');
const vm = require('node:vm');
const assert = require('node:assert/strict');
const elements = new Map();
function el(id) {
    if (!elements.has(id)) elements.set(id, {
        value: id === 'episodes-status-filter' ? 'all' : '', innerHTML: '', innerText: '',
        dataset: {}, style: {}, classList: {add(){},remove(){},contains(){return false;}},
        addEventListener(){}, querySelectorAll(){return [];}
    });
    return elements.get(id);
}
const context = {
    document: {getElementById:el, documentElement:{setAttribute(){}},querySelector(){return null;},addEventListener(){},querySelectorAll(){return [];}},
    window:{location:{href:'http://localhost/admin/'}},
    bootstrap:{Toast:class {},Modal:{getOrCreateInstance(){return {show(){}};}}},
    URL, console, setInterval(){}, clearInterval(){}, setTimeout(){}, clearTimeout(){}, navigator:{},
    localStorage:{getItem(){return 'mocha';},setItem(){}}
};
vm.createContext(context);
vm.runInContext(fs.readFileSync(__dirname+'/ui/admin.js','utf8').replaceAll('checkAuth();','void 0;'),context);
async function run() {
    const episodes = [
        {id:'1',title:'<img src=x onerror=alert(1)>',status:'downloaded',duration:3661,size:2048,media_url:'/podcasts/show/1.mp3',video_url:'https://example.org/video',pub_date:'2026-01-01'},
        {id:'2',title:'Pending title',status:'new'},
        {id:'3',title:'Error title',status:'error',error:'<script>bad</script>'},
        {id:'4',title:'Cleaned title',status:'cleaned',media_url:'/must-not-play.mp3'}
    ];
    context.fetch = async () => ({ok:true,json:async()=>({feed:{title:'Podcast'},episodes})});
    await context.showEpisodes('show');
    let html = el('episodes-list').innerHTML;
    assert(!html.includes('<img src=x'));
    assert(!html.includes('<script>'));
    assert(html.includes('/podcasts/show/1.mp3'));
    assert(!html.includes('/must-not-play.mp3'));
    assert(html.includes('1:01:01'));
    assert(html.includes('Pending'));
    assert(!html.includes('>Queued<'));
    assert(el('episodes-feed-subtitle').innerText.includes('Cleaned: 1'));
    el('episodes-status-filter').value = 'error';
    let requestedURL;
    context.fetch = async url => {
        requestedURL = new URL(url, 'http://localhost');
        return {ok:true,json:async()=>({feed:{title:'Podcast'},episodes:[episodes[2]],total:4,matching:1,status_counts:{downloaded:1,new:1,error:1,cleaned:1}})};
    };
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('status'), 'error');
    assert(el('episodes-list').innerHTML.includes('Error title'));
    assert(!el('episodes-list').innerHTML.includes('Pending title'));
    el('episodes-search').value = 'no match';
    context.fetch = async url => {
        requestedURL = new URL(url, 'http://localhost');
        return {ok:true,json:async()=>({episodes:[],total:4,matching:0})};
    };
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('q'), 'no match');
    assert(el('episodes-list').innerHTML.includes('No episodes match'));

    const pending = [];
    context.fetch = () => new Promise(resolve=>pending.push(resolve));
    const slow = context.showEpisodes('slow');
    const fast = context.showEpisodes('fast');
    pending[1]({ok:true,json:async()=>({feed:{title:'Fast'},episodes:[]})});
    await fast;
    pending[0]({ok:true,json:async()=>({feed:{title:'Slow'},episodes})});
    await slow;
    assert.equal(el('episodes-feed-title').innerText,'Podcast: Fast');

    // Test sorting and duration filtering
    const sortEpisodes = [
        {id:'ep-old',title:'Old Episode',status:'downloaded',duration:300,pub_date:'2024-01-01T00:00:00Z'},
        {id:'ep-new',title:'New Episode',status:'downloaded',duration:5400,pub_date:'2026-05-01T00:00:00Z'},
        {id:'ep-mid',title:'Mid Episode',status:'downloaded',duration:1800,pub_date:'2025-06-01T00:00:00Z'}
    ];
    let serverEpisodes = sortEpisodes;
    context.fetch = async url => {
        requestedURL = new URL(url, 'http://localhost');
        return {ok:true,json:async()=>({feed:{title:'Sort Test'},episodes:serverEpisodes,total:3,matching:serverEpisodes.length})};
    };
    await context.showEpisodes('sort-feed');

    // Date descending: New -> Mid -> Old
    el('episodes-sort').value = 'date-desc';
    serverEpisodes = [sortEpisodes[1], sortEpisodes[2], sortEpisodes[0]];
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('sort'), 'date-desc');
    let idxNew = el('episodes-list').innerHTML.indexOf('New Episode');
    let idxMid = el('episodes-list').innerHTML.indexOf('Mid Episode');
    let idxOld = el('episodes-list').innerHTML.indexOf('Old Episode');
    assert(idxNew < idxMid && idxMid < idxOld, 'date-desc sort failed');

    // Date ascending: Old -> Mid -> New
    el('episodes-sort').value = 'date-asc';
    serverEpisodes = [sortEpisodes[0], sortEpisodes[2], sortEpisodes[1]];
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('sort'), 'date-asc');
    idxNew = el('episodes-list').innerHTML.indexOf('New Episode');
    idxMid = el('episodes-list').innerHTML.indexOf('Mid Episode');
    idxOld = el('episodes-list').innerHTML.indexOf('Old Episode');
    assert(idxOld < idxMid && idxMid < idxNew, 'date-asc sort failed');

    // Duration descending: New (5400) -> Mid (1800) -> Old (300)
    el('episodes-sort').value = 'duration-desc';
    serverEpisodes = [sortEpisodes[1], sortEpisodes[2], sortEpisodes[0]];
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('sort'), 'duration-desc');
    idxNew = el('episodes-list').innerHTML.indexOf('New Episode');
    idxMid = el('episodes-list').innerHTML.indexOf('Mid Episode');
    idxOld = el('episodes-list').innerHTML.indexOf('Old Episode');
    assert(idxNew < idxMid && idxMid < idxOld, 'duration-desc sort failed');

    // Duration filter: short (< 15m / 900s) -> only Old (300)
    el('episodes-duration-filter').value = 'short';
    serverEpisodes = [sortEpisodes[0]];
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('duration'), 'short');
    assert(el('episodes-list').innerHTML.includes('Old Episode'));
    assert(!el('episodes-list').innerHTML.includes('Mid Episode'));
    assert(!el('episodes-list').innerHTML.includes('New Episode'));

    // Duration filter: long (> 60m / 3600s) -> only New (5400)
    el('episodes-duration-filter').value = 'long';
    serverEpisodes = [sortEpisodes[1]];
    await context.fetchAndRenderEpisodes();
    assert.equal(requestedURL.searchParams.get('duration'), 'long');
    assert(!el('episodes-list').innerHTML.includes('Old Episode'));
    assert(!el('episodes-list').innerHTML.includes('Mid Episode'));
    assert(el('episodes-list').innerHTML.includes('New Episode'));

    // Reset duration filter
    el('episodes-duration-filter').value = 'all';
    el('episodes-sort').value = 'default';

    // A background refresh preserves the current page and active filters.
    context.fetch = async url => {
        requestedURL = new URL(url, 'http://localhost');
        return {ok:true,json:async()=>({feed:{title:'Sort Test'},episodes:[sortEpisodes[1]],total:100,matching:100,offset:40,has_more:true})};
    };
    await context.changeEpisodePage(1);
    el('episodes-search').value = 'Go & Rust';
    el('episodes-status-filter').value = 'downloaded';
    await context.showEpisodes('sort-feed', false);
    assert.equal(requestedURL.searchParams.get('offset'), '40');
    assert.equal(requestedURL.searchParams.get('q'), 'Go & Rust');
    assert.equal(requestedURL.searchParams.get('status'), 'downloaded');
    assert.equal(el('episodes-page-label').innerText, 'Page 2 of 3');
    // If the last page disappears, use the offset returned by the server.
    context.fetch = async () => ({ok:true,json:async()=>({episodes:[sortEpisodes[0]],total:1,matching:1,offset:0,has_more:false})});
    await context.showEpisodes('sort-feed', false);
    assert.equal(el('episodes-page-label').innerText, 'Page 1 of 1');
    assert.equal(el('episodes-next').disabled, true);

    context.fetch = async()=>({ok:true,json:async()=>({feed:{title:'Book',provider:'audiobook'},episodes:[
        {id:'01.mp3',title:'Chapter',order:'1',status:'downloaded',pub_date:'2020-01-01',media_url:'/book/01.mp3'}
    ]})});
    await context.showEpisodes('book');
    assert(el('episodes-list').innerHTML.includes('1. Chapter'));
    assert(!el('episodes-list').innerHTML.includes('2020'));
    // Test Cyrillic transliteration and Feed ID slug generation
    assert.equal(context.toValidFeedSlug("клан_бобров", true, "_"), "KLAN_BOBROV");
    assert.equal(context.toValidFeedSlug("ДТКД (Для тех, кто дома)", true, "_"), "DTKD_DLYA_TEKH_KTO_DOMA");
    assert.equal(context.toValidFeedSlug("Война и мир", false, "-"), "voyna-i-mir");
    assert.equal(context.toValidFeedSlug("Київський щоденник", false, "-"), "kiyivskiy-shchodennik");
    assert.equal(context.toValidFeedSlug("Uncut - Завтракаст #392", true, "_"), "UNCUT_ZAVTRAKAST_392");
    assert.equal(context.toValidFeedSlug("@clanbeaver", true, "_"), "CLANBEAVER");

    console.log('Episode rendering, filtering, sorting, media links, and request ordering tests passed');
    console.log('Cyrillic transliteration and Feed ID slug tests passed');
}
run().catch(err=>{console.error(err);process.exitCode=1;});
