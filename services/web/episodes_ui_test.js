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
    document: {getElementById:el, documentElement:{setAttribute(){}},addEventListener(){},querySelectorAll(){return [];}},
    window:{location:{href:'http://localhost/admin/'}},
    bootstrap:{Toast:class {},Modal:{getOrCreateInstance(){return {show(){}};}}},
    URL, console, setInterval(){}, clearInterval(){}, setTimeout(){}, navigator:{},
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
    context.renderEpisodeList();
    assert(el('episodes-list').innerHTML.includes('Error title'));
    assert(!el('episodes-list').innerHTML.includes('Pending title'));
    el('episodes-search').value = 'no match';
    context.renderEpisodeList();
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

    context.fetch = async()=>({ok:true,json:async()=>({feed:{title:'Book',provider:'audiobook'},episodes:[
        {id:'01.mp3',title:'Chapter',order:'1',status:'downloaded',pub_date:'2020-01-01',media_url:'/book/01.mp3'}
    ]})});
    await context.showEpisodes('book');
    assert(el('episodes-list').innerHTML.includes('1. Chapter'));
    assert(!el('episodes-list').innerHTML.includes('2020'));
    console.log('Episode rendering, filtering, media links, and request ordering tests passed');
}
run().catch(err=>{console.error(err);process.exitCode=1;});
