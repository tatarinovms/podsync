// Run from tests/e2e: npm ci && npx playwright install chromium && npm test
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
    if (!['index.html', 'admin.js', 'admin.css', 'theme.js', 'select.js'].includes(file)) { res.writeHead(404).end(); return; }
    res.setHeader('Content-Type', file.endsWith('.js') ? 'application/javascript' : file.endsWith('.css') ? 'text/css' : 'text/html');
    res.end(fs.readFileSync(path.join(__dirname, '..', '..', 'services', 'web', 'ui', file)));
});
(async()=>{
await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
const origin = `http://127.0.0.1:${server.address().port}`;
const browser = await chromium.launch({headless:true, ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE ? {executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE} : {})});
let page;
try {
page=await browser.newPage({viewport:{width:1440,height:1050}});
// Serve the UI's pinned CDN dependencies locally, including icon fonts.
const cdnFiles = Object.fromEntries([
 ['bootstrap@5.3.3', 'dist/css/bootstrap.min.css'],
 ['bootstrap@5.3.3', 'dist/js/bootstrap.bundle.min.js'],
 ['bootstrap-icons@1.11.3', 'font/bootstrap-icons.css'],
 ['bootstrap-icons@1.11.3', 'font/fonts/bootstrap-icons.woff2'],
 ['bootstrap-icons@1.11.3', 'font/fonts/bootstrap-icons.woff'],
 ['qrious@4.0.2', 'dist/qrious.min.js'],
].map(([pkg, file]) => ['/npm/'+pkg+'/'+file, require.resolve(pkg.split('@')[0]+'/'+file)]));
await page.route('https://cdn.jsdelivr.net/**', route => {
 const file = cdnFiles[new URL(route.request().url()).pathname];
 return file ? route.fulfill({path:file}) : route.abort();
});
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
 else if(path==='/api/v1/personal-feeds') data=[];
 else if(path==='/api/v1/feeds/status') data=feeds.map(f=>({id:f.id,status:f.updating?'running':'idle'}));
 else if(path.startsWith('/api/v1/feeds/')) {
 const feed=feeds.find(f=>f.id===path.split('/')[4]);
 if (route.request().method() === 'PUT') { const changes=route.request().postDataJSON(); feed.title=changes.custom.title; feed.author=changes.custom.author; feed.config={...feed.config,...changes}; }
 if (u.searchParams.has('limit')) {
  const q = (u.searchParams.get('q') || '').toLowerCase();
  const status = u.searchParams.get('status');
  const matching = episodes.filter(ep => (!q || (ep.title+' '+ep.id).toLowerCase().includes(q)) && (!status || status==='all' || ep.status===status));
  const limit = Number(u.searchParams.get('limit')) || 40;
  const offset = Math.max(0, Number(u.searchParams.get('offset')) || 0);
  data={feed,episodes:matching.slice(offset,offset+limit),total:episodes.length,matching:matching.length,offset,limit,has_more:offset+limit<matching.length,status_counts:{downloaded:102,error:1}};
 } else data={feed,episodes};
 }
 else if(path==='/api/v1/system/logs') data={entries:[{timestamp:'2026-10-21T10:00:00Z',level:'info',message:'Feed updated successfully'}],current_level:'info'};
 else if(path==='/api/v1/tokens') data=[];
 await route.fulfill({json:data});
});
await page.addInitScript(()=>localStorage.setItem('podsync-theme','mocha'));
await page.goto(origin);
await page.waitForSelector('.feed-card');
await page.addStyleTag({content:'*{transition:none!important;animation:none!important;scroll-behavior:auto!important}'});

await page.addScriptTag({path:require.resolve('axe-core/axe.min.js')});
async function assertAccessible(context) {
 const violations = await page.evaluate(async () => (await axe.run(document, {
  runOnly: {type:'tag',values:['wcag2a','wcag2aa','wcag21aa','wcag22aa']}
 })).violations.map(v => ({id:v.id,targets:v.nodes.map(n=>n.target)})));
 assert.deepEqual(violations, [], context);
}

assert.deepEqual(requests.filter(p=>!p.endsWith('/status') || p.includes('/auth/')), ['/api/v1/auth/status', '/api/v1/feeds', '/api/v1/personal-feeds']);
assert.equal(await page.locator('.feed-card').count(), 6);
assert.deepEqual(await page.locator('#main-content > .tab-pane').evaluateAll(es=>es.map(e=>e.id)), ['tab-feeds','tab-personal','tab-audiobooks','tab-tokens','tab-system','tab-downloader','tab-mcp','tab-logs','episodesModal']);
await screenshot('mocha-desktop.png', true);
await assertAccessible('Mocha library');
// A shared combobox keeps selection, focus and keyboard behavior consistent.
const typeCombo=page.locator('#feed-type-filter [role="combobox"]');
await typeCombo.focus();
await typeCombo.press('End');
assert.equal(await typeCombo.getAttribute('aria-expanded'),'true');
await assertAccessible('Open podcast type list');
assert.equal(await page.locator('#feed-type-filter [role="option"]').evaluateAll(elements=>elements.every(el=>el.getBoundingClientRect().height<=parseFloat(getComputedStyle(el).minHeight)+1)),true,'Short option labels fit on one line');
await screenshot('combobox-open.png', false);
await typeCombo.press('Escape');
assert.equal(await page.locator('#feed-type-filter').evaluate(el=>el.value),'all');
for (const key of 'audiobooks') await typeCombo.press(key);
await typeCombo.press('Enter');
assert.equal(await page.locator('.feed-card').count(),1);
assert.equal(await page.locator('#feed-type-filter').evaluate(el=>el.value),'audiobooks');
assert.equal(await typeCombo.evaluate(el=>el===document.activeElement),true);
await typeCombo.press('Home');
await typeCombo.press('Enter');
assert.equal(await page.locator('.feed-card').count(),6);
await page.locator('#feed-type-filter').evaluate(el=>{el.disabled=true;});
assert.equal(await typeCombo.isDisabled(),true);
await page.locator('#feed-type-filter').evaluate(el=>{el.disabled=false;});
await typeCombo.click();
await typeCombo.press('Tab');
assert.equal(await typeCombo.getAttribute('aria-expanded'),'false');
// Dynamic options, required validation and form reset use the same component.
await page.evaluate(()=>{
 const form=document.createElement('form');form.id='select-test-form';
 form.innerHTML='<label for="select-test">Test choice</label><podsync-select id="select-test" required><option value="">Choose</option><option value="second">Second</option></podsync-select>';
 document.body.append(form);
});
assert.equal(await page.locator('#select-test').evaluate(el=>el.checkValidity()),false);
await page.locator('#select-test').evaluate(el=>{el.value='second';});
assert.equal(await page.locator('#select-test').evaluate(el=>el.checkValidity()),true);
await page.locator('#select-test-form').evaluate(el=>el.reset());
assert.equal(await page.locator('#select-test').evaluate(el=>el.value),'');
await page.locator('#select-test').evaluate(el=>{el.innerHTML='<option value="replacement">Replacement</option>';});
await page.waitForFunction(()=>document.querySelector('#select-test .select-value').textContent==='Replacement');
await page.locator('#select-test').evaluate(el=>{
 const option=document.createElement('option');option.value='blocked';option.text='Blocked';option.disabled=true;el.append(option);
});
await page.locator('label[for="select-test"]').click();
assert.equal(await page.locator('#select-test [role="combobox"]').evaluate(el=>el===document.activeElement),true);
await page.locator('#select-test [role="combobox"]').press('End');
await page.locator('#select-test [role="combobox"]').press('Enter');
assert.equal(await page.locator('#select-test').evaluate(el=>el.value),'replacement','Keyboard skips disabled options');
await page.locator('#select-test-form').evaluate(el=>el.remove());
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
await page.waitForFunction(()=>document.getElementById('episodes-list').textContent.includes('Episode 41'));
assert.match(await page.locator('#episodes-list').innerText(),/Episode 41/);
await page.locator('#episodes-search').fill('Episode 103');
await page.waitForFunction(()=>document.querySelectorAll('#episodes-list .list-group-item').length===1);
assert.equal(await page.locator('#episodes-list .list-group-item').count(),1);
await page.locator('#episodes-search').fill('');
await page.waitForFunction(()=>document.querySelectorAll('#episodes-list .list-group-item').length===40);
await screenshot('episodes.png', false);
await page.locator('[data-section="settings"]').click();
await page.waitForFunction(()=>document.getElementById('fs-feed-id').value==='podcast-0');
await assertAccessible('Mocha feed settings');
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
await page.locator('#logs-autorefresh-select [role=combobox]').click();
await page.getByRole('option', {name:'Every 5s',exact:true}).click();
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
await assertAccessible('Mocha add podcast dialog');
const modalCombo=page.locator('#new-feed-format [role="combobox"]');
await modalCombo.click();
await modalCombo.press('Escape');
assert.equal(await page.locator('#addFeedModal.show').count(),1,'Escape closes the list before the dialog');
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
await assertAccessible('Latte library');
await page.locator('.feed-card').filter({hasText:'The Curious Mind'}).getByRole('button',{name:'Open',exact:true}).click();
await page.waitForSelector('#episodes-list .list-group-item');
await assertAccessible('Latte episodes and status badges');
await page.locator('[data-section="settings"]').click();
await page.waitForFunction(()=>document.getElementById('fs-feed-id').value==='podcast-0');
await assertAccessible('Latte feed settings');
await page.getByRole('button',{name:'Back to library',exact:true}).click();
await page.locator('.library-heading [data-bs-target="#addFeedModal"]').click();
await page.waitForSelector('#addFeedModal.show');
await assertAccessible('Latte add podcast dialog');
await page.setViewportSize({width:320,height:844});
for (const selector of ['#addFeedModal .btn-xs','#addFeedModal .btn-close','#btn-preview-feed']) {
 const sizes=await page.locator(selector).evaluateAll(elements=>elements.map(el=>({width:el.getBoundingClientRect().width,height:el.getBoundingClientRect().height})).filter(size=>size.width>0&&size.height>0));
 assert(sizes.every(size=>size.width>=44&&size.height>=44),`Small mobile target: ${selector}`);
}
await page.locator('#new-feed-id').fill('invalid!');
assert.equal(await page.locator('#new-feed-id').getAttribute('aria-invalid'),'true');
await assertAccessible('Latte mobile validation error');
await screenshot('latte-validation-mobile.png', false);
await page.locator('#new-feed-id').fill('valid-slug');
assert.equal(await page.locator('#new-feed-id').getAttribute('aria-invalid'),'false');
await page.locator('#addFeedModal .btn-close').click();
await page.waitForSelector('#addFeedModal.show',{state:'hidden'});
await page.setViewportSize({width:1440,height:1050});
await screenshot('latte-desktop.png', true);
for (const width of [280,320,390,576,768,951]) {
 await page.setViewportSize({width,height:844});
 assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,`Library overflow at ${width}px`);
 if(width<768){
  assert.equal(await page.locator('.app-sidebar').isVisible(),false);
  await page.getByRole('button',{name:'Toggle navigation',exact:true}).click();
  assert.equal(await page.locator('.app-sidebar').isVisible(),true);
  await page.getByRole('button',{name:'Toggle navigation',exact:true}).click();
  const smallButtons=await page.locator('button').evaluateAll(elements=>elements.filter(el=>{
   const r=el.getBoundingClientRect();return r.width>0&&r.height>0&&getComputedStyle(el).visibility!=='hidden'&&(r.width<44||r.height<44);
  }).map(el=>el.outerHTML));
  assert.deepEqual(smallButtons,[],`Small library targets at ${width}px`);
 }
 for(const id of ['feed-type-filter','feed-sort']) {
  assert.equal(await page.locator('#'+id+' .select-value').evaluate(el=>el.scrollWidth<=el.clientWidth),true,`Clipped filter ${id} at ${width}px`);
 }
 await screenshot(`latte-mobile-${width}.png`, true);
}
await page.evaluate(()=>toggleTheme());
await page.setViewportSize({width:375,height:667});
await screenshot('mocha-mobile.png', true);
// Personal-feed cards and the add-media forms use the same mobile width as podcasts.
const podcastCardLayout = await page.locator('#feeds-container .feed-card').first().evaluate(el=>{
 const r=el.getBoundingClientRect(); return {x:r.x,right:r.right};
});
assert(podcastCardLayout.right<=375,'Podcast card fits the mobile viewport');
await page.locator('#mainTabs [data-bs-target="#tab-personal"]').evaluate(el=>bootstrap.Tab.getOrCreateInstance(el).show());
await page.waitForFunction(()=>currentTab==='tab-personal');
await page.evaluate(()=>{
 personalFeeds=[{id:'personal-test',title:'A personal feed with a long title',description:'A curated feed for mobile layout checks.',episode_count:3,feed_url:'http://localhost:8080/personal-test.xml'}];
 personalRenderCards();
});
const personalCardLayout = await page.locator('#personal-list .feed-card').evaluate(el=>{
 const r=el.getBoundingClientRect(); return {x:r.x,right:r.right,width:r.width};
});
assert.equal(personalCardLayout.x,podcastCardLayout.x,'Personal-feed cards align with podcast cards');
assert(personalCardLayout.right<=375,'Personal-feed card fits the mobile viewport');
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'Personal-feed list does not overflow on mobile');
await page.evaluate(()=>{
 document.getElementById('personal-main-view').classList.add('d-none');
 document.getElementById('personal-detail').classList.remove('d-none');
});
const personalFormLayout = await page.locator('#personal-url-form').evaluate(form=>{
 const card=form.closest('.card'); const heading=card.querySelector('h6');
 const add=form.querySelector('button[type="submit"]'); const trigger=form.querySelector('.select-trigger');
 const box=card.getBoundingClientRect(); const rect=el=>el.getBoundingClientRect();
 return {card:box.toJSON(),heading:rect(heading).toJSON(),add:rect(add).toJSON(),trigger:rect(trigger).toJSON()};
});
assert(personalFormLayout.heading.x-personalFormLayout.card.x>=15,'Personal form heading has a mobile inset');
assert(personalFormLayout.add.x-personalFormLayout.card.x>=15&&personalFormLayout.card.right-personalFormLayout.add.right>=15,'Add URL button stays inside its card');
assert(personalFormLayout.trigger.x-personalFormLayout.card.x>=15&&personalFormLayout.card.right-personalFormLayout.trigger.right>=15,'Personal select has room on both sides');
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true,'Personal feed detail does not overflow on mobile');
await screenshot('mocha-personal-mobile.png', true);
await page.locator('#mainTabs [data-bs-target="#tab-feeds"]').evaluate(el=>bootstrap.Tab.getOrCreateInstance(el).show());
await page.waitForFunction(()=>currentTab==='tab-feeds');
await page.evaluate(()=>{ personalFeeds=[]; syncPersonalIntoAllFeeds(); });
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
console.log('Browser checks passed: navigation, filters, pagination, settings save, subscriptions, lazy requests, log polling, add form, audiobook protection, both themes, axe accessibility, combobox keyboard and form behavior, 280-951px layouts, 300-feed library, DOM reuse, scroll restoration.');
console.log('Screenshots: '+screenshots);
} catch (err) {
 await page.screenshot({path:path.join(screenshots,'failure.png'),fullPage:true});
 console.error('Failure state:', await page.evaluate(()=>({hash:location.hash,currentTab,detailClass:document.getElementById('episodesModal').className,active:[...document.querySelectorAll('#main-content > .active')].map(e=>e.id),buttons:[...document.querySelectorAll('button')].filter(e=>e.textContent.includes('Back to library')).map(e=>({text:e.textContent,html:e.outerHTML,display:getComputedStyle(e).display,visible:e.getBoundingClientRect().toJSON()}))})));
 console.error('Screenshots:',screenshots); throw err;
} finally { await browser.close(); }
})().catch(err=>{console.error(err);process.exitCode=1;}).finally(()=>server.close());
