// Run with Playwright available on NODE_PATH: node services/web/library_browser_test.js
// Uses a temporary local server and mocked API responses; never contacts a live Podsync server.
const {chromium} = require('playwright');
const assert = require('node:assert/strict');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const screenshots = fs.mkdtempSync(path.join(os.tmpdir(), 'podsync-ui-'));
const server = http.createServer((req, res) => {
    const file = new URL(req.url, 'http://localhost').pathname.slice(1) || 'index.html';
    if (!['index.html', 'admin.js', 'admin.css', 'theme.js'].includes(file)) { res.writeHead(404).end(); return; }
    res.setHeader('Content-Type', file.endsWith('.js') ? 'application/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html');
    res.end(fs.readFileSync(path.join(__dirname, 'ui', file)));
});
(async()=>{
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({headless:true, ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ? {executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE} : {})});
let page;
try {
page=await browser.newPage({viewport:{width:1440,height:1050}});
async function screenshot(name, fullPage = true) {
 await page.waitForTimeout(350);
 await page.screenshot({path:path.join(screenshots,name),fullPage});
}
const errors=[];page.on('pageerror',e=>errors.push(e.message));
const feeds=['The Curious Mind','История без границ','Design Notes','Наука в деталях','Slow Conversations','Глава за главой'].map((title,i)=>({id:'podcast-'+i,title,author:['Alex Morgan','Лекторий','Studio North','Научный клуб','Sunday Radio','Аудиокнига'][i],provider:i===5?'audiobook':i%2?'rutube':'youtube',url:i===5?'audiobook://book':'https://youtube.com/@test',feed_url:'http://localhost:8080/podcast-'+i+'.xml',apple_url:'podcast://test',pocketcasts_url:'pktc://test',overcast_url:'overcast://test',cover_art:origin+'/test-cover-'+i+'.svg',downloaded_count:12+i*3,episode_count:100,total_size:(i+1)*234567890,updating:i===2,error_count:i===3?2:0,last_update:'2026-10-20T10:00:00Z',config:{format:'audio',quality:'high',cron_schedule:'@every 12h',custom:{title,author:'Author'},clean:{keep_last:50}}}));
const episodes=Array.from({length:103},(_,i)=>({id:'episode-'+i,title:'Episode '+(i+1)+' — A closer look at the world',status:i===2?'error':'downloaded',duration:2000,size:12345678,media_url:'/audio/test.mp3',pub_date:'2026-10-20T10:00:00Z',error:i===2?'Source unavailable':undefined}));
const requests=[];
await page.route('**/test-cover-*.svg',r=>{const n=Number(r.request().url().match(/cover-(\d+)/)[1]);const colors=['#cba6f7','#89b4fa','#fab387','#a6e3a1','#f2cdcd','#b4befe'];return r.fulfill({contentType:'image/svg+xml',body:`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160"><rect width="160" height="160" fill="${colors[n]}"/><circle cx="110" cy="45" r="65" fill="#1e1e2e" opacity=".18"/><path d="M0 160L160 0V160Z" fill="#1e1e2e" opacity=".25"/><text x="18" y="125" font-size="64" font-family="sans-serif" fill="#1e1e2e">${['C','И','D','Н','S','Г'][n]}</text></svg>`})});
await page.route('**/api/v1/**',async route=>{
 const u=new URL(route.request().url());const path=u.pathname;requests.push(path);
 let data={};
 if(path.endsWith('/auth/status')) data={enabled:true,authenticated:true,username:'Admin'};
 else if(path==='/api/v1/feeds') data=feeds;
 else if(path==='/api/v1/feeds/status') data=feeds.map(f=>({id:f.id,status:f.updating?'running':'idle'}));
 else if(path.startsWith('/api/v1/feeds/')) {
 const feed=feeds.find(f=>f.id===path.split('/')[4]);
 if (route.request().method() === 'PUT') { const changes=route.request().postDataJSON(); feed.title=changes.custom.title; feed.author=changes.custom.author; feed.config={...feed.config,...changes}; }
 data={feed,episodes};
 }
 else if(path==='/api/v1/system/logs') data={entries:[{timestamp:'2026-10-21T10:00:00Z',level:'info',message:'Feed updated successfully'}],current_level:'info'};
 else if(path==='/api/v1/tokens') data=[];
 await route.fulfill({json:data});
});
await page.addInitScript(()=>localStorage.setItem('podsync-theme','mocha'));
await page.goto(origin);
await page.waitForSelector('.feed-card');

assert.deepEqual(requests.filter(p=>!p.endsWith('/status') || p.includes('/auth/')), ['/api/v1/auth/status', '/api/v1/feeds']);
assert.equal(await page.locator('.feed-card').count(), 6);
assert.deepEqual(await page.locator('#main-content > .tab-pane').evaluateAll(es=>es.map(e=>e.id)), ['tab-feeds','tab-audiobooks','tab-tokens','tab-system','tab-downloader','tab-mcp','tab-logs','episodesModal']);
await screenshot('mocha-desktop.png', true);
await page.getByRole('button',{name:'List view',exact:true}).click();
assert.equal(await page.locator('#feeds-container').getAttribute('data-view'),'list');
await screenshot('mocha-list.png', true);
await page.getByRole('button',{name:'Needs attention',exact:false}).click();
assert.equal(await page.locator('.feed-card').count(),1);
await page.locator('[data-status="all"]').click();
await page.locator('#feed-search').fill('Curious');
assert.equal(await page.locator('.feed-card').count(),1);
await page.locator('.feed-card').getByRole('button',{name:'Open',exact:true}).click();
await page.waitForSelector('#episodes-list .list-group-item');
assert.equal(await page.locator('#episodes-list .list-group-item').count(),40);
assert.equal(await page.locator('.modal.show').count(),0);
assert(page.url().endsWith('#feed/podcast-0/episodes'));
await page.locator('#episodes-next').click();
assert.match(await page.locator('#episodes-list').innerText(),/Episode 41/);
await page.locator('#episodes-search').fill('Episode 103');
assert.equal(await page.locator('#episodes-list .list-group-item').count(),1);
await page.locator('#episodes-search').fill('');
await screenshot('episodes.png', false);
await page.locator('[data-section="settings"]').click();
await page.waitForFunction(()=>document.getElementById('fs-feed-id').value==='podcast-0');
await page.locator('#fs-btn-custom').click();
await page.locator('#fs-custom-title').fill('The Curious Mind — Updated');
await page.locator('#feed-settings-form button[type="submit"]').click();
await page.waitForFunction(()=>document.getElementById('episodes-feed-title').textContent==='The Curious Mind — Updated');
await page.getByRole('button',{name:'Back to library',exact:true}).click();
assert.equal(await page.locator('#feed-search').inputValue(),'Curious');
assert.match(await page.locator('.feed-card-title').innerText(),/Updated/);
await page.locator('.feed-card').getByRole('button',{name:'Subscribe',exact:true}).click();
await page.waitForSelector('#subscribeModal.show');
assert.equal(await page.locator('#subscribe-body a').count(),4);
await page.locator('#subscribeModal .btn-close').click();
await page.waitForSelector('#subscribeModal.show',{state:'hidden'});
await page.locator('.feed-card').getByRole('button',{name:'Subscribe',exact:true}).click();
await page.waitForSelector('#subscribeModal.show');
await page.getByRole('button',{name:'Show QR code',exact:true}).click();
await page.waitForSelector('#qrModal.show');
assert.equal(await page.locator('.modal.show').count(),1);
await page.locator('#qrModal button').click();
await page.waitForSelector('#qrModal.show',{state:'hidden'});
await page.locator('.feed-card-title').click();
await page.waitForSelector('#episodes-list .list-group-item');
await page.goBack();
await page.waitForSelector('#tab-feeds.active');
assert.equal(await page.locator('#feed-search').inputValue(),'Curious');
await page.evaluate(()=>resetFeedFilters());
await page.getByRole('button',{name:'Card view',exact:true}).click();
// Switching sections issues only one request to each endpoint.
let before=requests.filter(p=>p==='/api/v1/downloader').length;
await page.locator('#mainTabs [data-bs-target="#tab-downloader"]').click();
await page.waitForFunction(()=>currentTab==='tab-downloader');
await page.waitForTimeout(200);
assert.equal(requests.filter(p=>p==='/api/v1/downloader').length-before,1);
before=requests.filter(p=>p==='/api/v1/system/logs').length;
await page.locator('#mainTabs [data-bs-target="#tab-logs"]').click();
await page.waitForSelector('#logs-terminal .log-line');
assert.equal(requests.filter(p=>p==='/api/v1/system/logs').length-before,1);
await page.locator('#logs-autorefresh-select').selectOption('5000');
assert.equal(await page.evaluate(()=>logsAutoRefreshTimer!==null),true);
await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:true});document.dispatchEvent(new Event('visibilitychange'));});
assert.equal(await page.evaluate(()=>logsAutoRefreshTimer===null),true);
await page.evaluate(()=>{Object.defineProperty(document,'hidden',{configurable:true,value:false});document.dispatchEvent(new Event('visibilitychange'));});
assert.equal(await page.evaluate(()=>logsAutoRefreshTimer!==null),true);
await page.locator('#mainTabs [data-bs-target="#tab-feeds"]').click();
assert.equal(await page.evaluate(()=>logsAutoRefreshTimer===null),true);
// The short add form keeps every advanced field available when requested.
await page.locator('.library-heading [data-bs-target="#addFeedModal"]').click();
await page.waitForSelector('#addFeedModal.show');
assert.equal(await page.locator('#new-feed-playlist-sort').isVisible(),false);
assert.equal(await page.locator('#new-feed-cron-schedule').isVisible(),true);
await page.locator('#add-advanced-toggle').click();
assert.equal(await page.locator('#new-feed-playlist-sort').isVisible(),true);
await page.locator('[data-bs-target="#af-tab-filters"]').click();
await page.locator('#add-advanced-toggle').click();
assert.equal(await page.locator('#new-feed-format').isVisible(),true);
await page.locator('#addFeedModal .btn-close').click();
await page.waitForSelector('#addFeedModal.show',{state:'hidden'});
// A direct link survives reload and audiobook settings keep channel-only options hidden.
await page.goto(origin+'/#feed/podcast-5/settings');
await page.waitForFunction(()=>document.getElementById('fs-feed-id').value==='podcast-5');
assert.equal(await page.locator('#fs-channel-media-row').isVisible(),false);
assert.equal(await page.locator('#fs-nav-cleanup').isVisible(),false);
await page.locator('[data-section="history"]').click();
await page.waitForSelector('#feed-history-list .history-entry');
await page.getByRole('button',{name:'Back to library',exact:true}).click();
await page.evaluate(()=>toggleTheme());
assert.equal(await page.locator('html').getAttribute('data-theme'),'latte');
await screenshot('latte-desktop.png', true);
for (const width of [390,320]) {
 await page.setViewportSize({width,height:844});
 assert.equal(await page.locator('.app-sidebar').isVisible(),false);
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
 await page.getByRole('button',{name:'Toggle navigation',exact:true}).click();
 assert.equal(await page.locator('.app-sidebar').isVisible(),true);
 await page.getByRole('button',{name:'Toggle navigation',exact:true}).click();
 await screenshot(`latte-mobile-${width}.png`, true);
}
await page.evaluate(()=>toggleTheme());
await page.setViewportSize({width:390,height:844});
await screenshot('mocha-mobile.png', true);
// A large library retains unchanged cards and keeps search focused without extra requests.
await page.setViewportSize({width:1440,height:1050});
for (let i=6; i<300; i++) feeds.push({...feeds[0],id:'bulk-'+i,title:'Library item '+i,updating:false,error_count:0});
await page.evaluate(()=>loadFeeds());
assert.equal(await page.locator('#feeds-container .feed-card').count(),300);
const mutations = await page.evaluate(async()=>{
 let count=0; const observer=new MutationObserver(records=>count+=records.length);
 observer.observe(document.getElementById('feeds-container'),{childList:true,subtree:true});
 filterFeeds(); await Promise.resolve(); observer.disconnect(); return count;
});
assert.equal(mutations,0,'unchanged library data should reuse existing DOM');
const requestsBeforeSearch=requests.length;
await page.locator('#feed-search').fill('Library item 299');
assert.equal(await page.locator('#feeds-container .feed-card').count(),1);
assert.equal(await page.locator('#feed-search').evaluate(el=>el===document.activeElement),true);
assert.equal(requests.length,requestsBeforeSearch,'search should not fetch feeds or RSS covers');
await page.locator('#feed-search').fill('');
const card=page.locator('#feeds-container .feed-card').nth(30);
await card.scrollIntoViewIfNeeded();
const scrollBefore=await page.evaluate(()=>scrollY);
await card.getByRole('button',{name:'Open',exact:true}).click();
await page.waitForSelector('#episodes-list .list-group-item');
await page.getByRole('button',{name:'Back to library',exact:true}).click();
await page.waitForFunction(()=>Math.abs(scrollY-libraryScroll)<2);
assert(Math.abs(await page.evaluate(()=>scrollY)-scrollBefore)<2,'library scroll should be restored within browser pixel rounding');
assert.deepEqual(errors,[]);
console.log('Browser checks passed: navigation, filters, pagination, settings save, subscriptions, lazy requests, log polling, add form, audiobook protection, both themes, 320/390px layouts, 300-feed library, DOM reuse, scroll restoration.');
console.log('Screenshots: '+screenshots);
} catch (err) {
 await page.screenshot({path:path.join(screenshots,'failure.png'),fullPage:true});
 console.error('Failure state:', await page.evaluate(()=>({hash:location.hash,currentTab,detailClass:document.getElementById('episodesModal').className,active:[...document.querySelectorAll('#main-content > .active')].map(e=>e.id),buttons:[...document.querySelectorAll('button')].filter(e=>e.textContent.includes('Back to library')).map(e=>({text:e.textContent,html:e.outerHTML,display:getComputedStyle(e).display,visible:e.getBoundingClientRect().toJSON()}))})));
 console.error('Screenshots:',screenshots); throw err;
} finally { await browser.close(); }
})().catch(err=>{console.error(err);process.exitCode=1;}).finally(()=>server.close());
