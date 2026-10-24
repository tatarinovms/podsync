
        function isValidAuthor(author) {
            if (!author) return false;
            const str = String(author).trim().toLowerCase();
            return str !== "" && str !== "<notfound>" && str !== "notfound" && !str.includes("notfound");
        }

        // Cyrillic & Slug Transliteration for Feed IDs
        const CYRILLIC_TO_LATIN = {
            "а": "a", "б": "b", "в": "v", "г": "g", "д": "d", "е": "e", "ё": "yo", "ж": "zh",
            "з": "z", "и": "i", "й": "y", "к": "k", "л": "l", "м": "m", "н": "n", "о": "o",
            "п": "p", "р": "r", "с": "s", "т": "t", "у": "u", "ф": "f", "х": "kh", "ц": "ts",
            "ч": "ch", "ш": "sh", "щ": "shch", "ъ": "", "ы": "y", "ь": "", "э": "e", "ю": "yu",
            "я": "ya",
            "є": "ye", "і": "i", "ї": "yi", "ґ": "g", "ў": "u"
        };

        function transliterateText(str) {
            if (!str) return "";
            return String(str).split("").map(char => {
                const lower = char.toLowerCase();
                if (CYRILLIC_TO_LATIN[lower] !== undefined) {
                    const mapped = CYRILLIC_TO_LATIN[lower];
                    return char === lower ? mapped : (mapped.length > 1 ? mapped.charAt(0).toUpperCase() + mapped.slice(1) : mapped.toUpperCase());
                }
                return char;
            }).join("").normalize("NFD").replace(/[\u0300-\u036f]/g, "");
        }

        function toValidFeedSlug(str, uppercase = true, sep = "_") {
            if (!str) return "";
            let s = transliterateText(str);
            s = s.replace(/[^a-zA-Z0-9_-]+/g, sep);
            s = s.replace(/[_-]+/g, sep);
            s = s.replace(/^[_-]+|[_-]+$/g, "");
            return uppercase ? s.toUpperCase() : s.toLowerCase();
        }

        function setupFeedIdValidation(inputEl, previewEl, options = {}) {
            if (!inputEl) return;
            const uppercase = options.uppercase !== false;
            const sep = options.separator || "_";

            function updateState() {
                const raw = inputEl.value;
                const val = raw.trim();
                const hasDisallowed = !/^[a-zA-Z0-9_-]*$/.test(raw);
                const isEmpty = val.length === 0;

                if (hasDisallowed || (inputEl.dataset.touched === "true" && isEmpty)) {
                    inputEl.classList.add("is-invalid");
                    inputEl.classList.remove("is-valid");
                } else if (!isEmpty) {
                    inputEl.classList.remove("is-invalid");
                    inputEl.classList.add("is-valid");
                } else {
                    inputEl.classList.remove("is-invalid");
                    inputEl.classList.remove("is-valid");
                }

                if (previewEl) {
                    previewEl.textContent = val || (uppercase ? "CLANBEAVER" : "feed-slug");
                }
            }

            inputEl.addEventListener("input", () => {
                inputEl.dataset.manual = "true";
                inputEl.dataset.touched = "true";

                // Real-time Cyrillic transliteration
                if (/[а-яА-ЯёЁіІїЇєЄґҐўЎ]/.test(inputEl.value)) {
                    const start = inputEl.selectionStart;
                    const original = inputEl.value;
                    let trans = transliterateText(original);
                    if (uppercase) trans = trans.toUpperCase();
                    inputEl.value = trans;
                    const diff = trans.length - original.length;
                    const newPos = Math.max(0, (start || 0) + diff);
                    try { inputEl.setSelectionRange(newPos, newPos); } catch (e) {}
                }
                updateState();
            });

            inputEl.addEventListener("paste", (e) => {
                inputEl.dataset.manual = "true";
                inputEl.dataset.touched = "true";
                const clipboard = (e.clipboardData || window.clipboardData)?.getData("text");
                if (clipboard && (/[а-яА-ЯёЁіІїЇєЄґҐўЎ\s@/\\#]/.test(clipboard) || !/^[a-zA-Z0-9_-]+$/.test(clipboard))) {
                    e.preventDefault();
                    const cleanSlug = toValidFeedSlug(clipboard, uppercase, sep);
                    const start = inputEl.selectionStart || 0;
                    const end = inputEl.selectionEnd || 0;
                    const curr = inputEl.value;
                    let next = (start === 0 && end === curr.length) ? cleanSlug : (curr.slice(0, start) + cleanSlug + curr.slice(end));
                    if (uppercase) next = next.toUpperCase();
                    inputEl.value = next;
                    updateState();
                }
            });

            inputEl.addEventListener("blur", () => {
                inputEl.dataset.touched = "true";
                let val = inputEl.value.trim();
                if (val) {
                    val = val.replace(/^[_-]+|[_-]+$/g, "");
                    if (uppercase) val = val.toUpperCase();
                    inputEl.value = val;
                }
                updateState();
            });
        }
        // State
        let allFeeds = [];
        let qrInstance = null;
        let currentTab = "tab-feeds";
        const tabDirty = {
            "tab-feeds": false,
            "tab-audiobooks": false
        };
        let feedsFetchSeq = 0;
        let statusFetchSeq = 0;
        let pollFailureCount = 0;
        const toastEl = new bootstrap.Toast(document.getElementById('app-toast'), { delay: 2500 });

        function escapeHtml(str) {
            if (!str) return '';
            return String(str)
                .replace(/&/g, '&amp;')
                .replace(/</g, '&lt;')
                .replace(/>/g, '&gt;')
                .replace(/"/g, '&quot;')
                .replace(/'/g, '&#039;');
        }

        function actionAttrs(action, ...args) {
            const values = args.map((value, index) => ` data-ui-arg${index + 1}="${escapeHtml(value)}"`).join('');
            return `data-ui-action="${action}"${values}`;
        }

        document.addEventListener('click', (event) => {
            const target = event.target.closest('[data-ui-action]');
            if (!target) return;
            const arg1 = target.dataset.uiArg1 || '';
            const arg2 = target.dataset.uiArg2 || '';
            const arg3 = target.dataset.uiArg3 || '';
            switch (target.dataset.uiAction) {
                case 'copy': copyToClipboard(arg1); break;
                case 'qr': {
                    const subscription = document.getElementById('subscribeModal');
                    if (subscription?.classList.contains('show')) {
                        subscription.addEventListener('hidden.bs.modal', () => showQRCode(arg1, arg2), {once:true});
                        closeModal(subscription);
                    } else showQRCode(arg1, arg2);
                    break;
                }
                case 'subscribe': showSubscription(arg1); break;
                case 'personal-open': personalOpenFromLibrary(arg1); break;
                case 'personal-edit': personalEdit(arg1); break;
                case 'personal-delete': personalConfirmDeleteFeed(arg1); break;
                case 'episodes': showEpisodes(arg1); break;
                case 'settings': openFeedSettings(arg1); break;
                case 'update': triggerUpdate(arg1); break;
                case 'delete-feed': deleteFeed(arg1); break;
                case 'logs': showLogsForFeed(arg1); break;
                case 'errors': showFeedErrors(arg1); break;
                case 'retry-error': retryFeedError(arg1, arg2); break;
                case 'retry': retryEpisode(arg1, arg2); break;
                case 'delete-token': deleteSingleToken(arg1, Number(arg2), arg3); break;
                case 'add-token': addToken(arg1); break;
                case 'clear-tokens': deleteTokens(arg1); break;
                case 'filter-log': filterByTag(arg1, arg2); break;
                case 'delete-book': deleteAudiobookDir(arg1, arg2); break;
                case 'import-book': openImportModal(arg1, arg2, arg3); break;
                case 'reset-filters': resetFeedFilters(); break;
            }
        });

        document.addEventListener('input', (event) => {
            const input = event.target.closest('[data-ui-token-provider]');
            if (input) handleTokenInput(input, input.dataset.uiTokenProvider);
        });
        document.addEventListener('keydown', (event) => {
            const input = event.target.closest('[data-ui-token-provider]');
            if (input && event.key === 'Enter') {
                event.preventDefault();
                addToken(input.dataset.uiTokenProvider);
            }
        });

        function safeUrl(value, schemes = ['http:', 'https:']) {
            try {
                const parsed = new URL(String(value || ''), window.location.href);
                return schemes.includes(parsed.protocol) ? escapeHtml(parsed.href) : '';
            } catch (_) {
                return '';
            }
        }

        function showToast(msg) {
            document.getElementById('toast-message').innerText = msg;
            toastEl.show();
        }

        function closeModal(modalId) {
            const el = typeof modalId === 'string' ? document.getElementById(modalId) : modalId;
            if (!el) return;
            try {
                const modal = bootstrap.Modal.getOrCreateInstance(el);
                const existingBackdrops = new Set(document.querySelectorAll('.modal-backdrop'));
                // Bootstrap ignores hide() during its opening transition. Retry once shown.
                const hideAfterOpening = () => modal.hide();
                el.addEventListener('shown.bs.modal', hideAfterOpening, {once: true});
                el.addEventListener('hidden.bs.modal', () => {
                    el.removeEventListener('shown.bs.modal', hideAfterOpening);
                }, {once: true});
                modal.hide();
                setTimeout(() => {
                    el.removeEventListener('shown.bs.modal', hideAfterOpening);
                    // Clean up an orphaned backdrop only when no other dialog is open.
                    if (!document.querySelector('.modal.show') && Array.from(document.querySelectorAll('.modal-backdrop')).every(backdrop => existingBackdrops.has(backdrop))) {
                        document.querySelectorAll('.modal-backdrop').forEach(backdrop => backdrop.remove());
                        document.body.classList.remove('modal-open');
                        document.body.style.removeProperty('overflow');
                        document.body.style.removeProperty('padding-right');
                    }
                }, 700);
            } catch (err) {
                console.warn('Modal hide failed:', err);
            }
        }

        // Auth
        async function checkAuth() {
            try {
                const res = await fetch('/api/v1/auth/status');
                const data = await res.json();
                if (!data.enabled) {
                    // Admin is not enabled
                    document.body.innerHTML = `
                        <div class="min-vh-100 d-flex align-items-center justify-content-center p-3 text-center">
                            <div>
                                <i aria-hidden="true" class="bi bi-lock-fill text-muted display-1"></i>
                                <h3 class="fw-bold mt-3">Admin Console is Disabled</h3>
                                <p class="text-muted">Enable [server.admin] enabled = true in config.toml</p>
                            </div>
                        </div>`;
                    return;
                }

                if (data.authenticated) {
                    document.getElementById('login-view').classList.add('d-none');
                    document.getElementById('dashboard-view').classList.remove('d-none');
                    document.getElementById('nav-username').innerText = data.username || 'Admin';
                    const sysAuthUser = document.getElementById('sys-auth-username');
                    if (sysAuthUser) sysAuthUser.innerText = data.username || 'admin';
                    if (data.rate_limit) {
                        const attemptsEl = document.getElementById('sec-max-attempts-text');
                        if (attemptsEl) attemptsEl.innerText = `${data.rate_limit.max_attempts} attempts / ${data.rate_limit.window}`;
                        const lockoutEl = document.getElementById('sec-lockout-text');
                        if (lockoutEl) lockoutEl.innerHTML = `HTTP 429 + <code>Retry-After</code> (${data.rate_limit.lockout})`;
                    }
                    loadDashboard();
                } else {
                    document.getElementById('login-view').classList.remove('d-none');
                    document.getElementById('dashboard-view').classList.add('d-none');
                }
            } catch (err) {
                console.error("Auth check failed:", err);
            }
        }

        document.getElementById('login-form').addEventListener('submit', async (e) => {
            e.preventDefault();
            const btn = document.getElementById('login-btn');
            const spinner = document.getElementById('login-spinner');
            const errBox = document.getElementById('login-error');

            errBox.classList.add('d-none');
            spinner.classList.remove('d-none');
            btn.disabled = true;

            const username = document.getElementById('login-username').value;
            const password = document.getElementById('login-password').value;

            try {
                const res = await fetch('/api/v1/auth/login', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ username, password })
                });

                if (res.ok) {
                    checkAuth();
                } else {
                    const data = await res.json();
                    errBox.innerText = data.error || 'Login failed';
                    errBox.classList.remove('d-none');
                }
            } catch (err) {
                errBox.innerText = 'Network error: ' + err.message;
                errBox.classList.remove('d-none');
            } finally {
                spinner.classList.add('d-none');
                btn.disabled = false;
            }
        });

        
        async function submitChangePassword(e) {
            e.preventDefault();
            const oldPw = document.getElementById('input-old-pw').value;
            const newPw = document.getElementById('input-new-pw').value;
            const confirmPw = document.getElementById('input-confirm-pw').value;
            const errAlert = document.getElementById('pw-error-alert');
            const submitBtn = document.getElementById('btn-save-pw');

            errAlert.classList.add('d-none');

            if (newPw !== confirmPw) {
                errAlert.innerText = 'New passwords do not match';
                errAlert.classList.remove('d-none');
                return;
            }

            submitBtn.disabled = true;
            submitBtn.innerHTML = '<span class="spinner-border spinner-border-sm me-1"></span>Saving...';

            try {
                const res = await fetch('/api/v1/auth/password', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ old_password: oldPw, new_password: newPw })
                });

                const data = await res.json();
                if (!res.ok) {
                    throw new Error(data.error || 'Failed to update password');
                }

                // Close modal
                closeModal('changePasswordModal');
                document.getElementById('changePasswordForm').reset();

                showToast('Password updated successfully!');
            } catch (err) {
                errAlert.innerText = err.message;
                errAlert.classList.remove('d-none');
            } finally {
                submitBtn.disabled = false;
                submitBtn.innerHTML = '<i aria-hidden="true" class="bi bi-check-lg me-1"></i>Update Password';
            }
        }

        async function submitSecuritySettings(e) {
            e.preventDefault();
            const submitBtn = document.getElementById('sec-settings-save-btn');
            const errAlert = document.getElementById('sec-settings-error');
            const attemptsVal = parseInt(document.getElementById('sec-input-max-attempts').value, 10);
            const windowVal = document.getElementById('sec-input-window').value;
            const lockoutVal = document.getElementById('sec-input-lockout').value;

            if (errAlert) errAlert.classList.add('d-none');
            if (submitBtn) {
                submitBtn.disabled = true;
                submitBtn.innerHTML = '<span class="spinner-border spinner-border-sm me-1"></span>Saving...';
            }

            try {
                const res = await fetch('/api/v1/auth/security', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        max_attempts: attemptsVal,
                        window: windowVal,
                        lockout: lockoutVal
                    })
                });

                const data = await res.json();
                if (!res.ok) {
                    throw new Error(data.error || 'Failed to update security settings');
                }

                closeModal('securitySettingsModal');
                showToast('Security settings updated successfully!');
                await loadAuthSecurity();
            } catch (err) {
                if (errAlert) {
                    errAlert.innerText = err.message;
                    errAlert.classList.remove('d-none');
                }
            } finally {
                if (submitBtn) {
                    submitBtn.disabled = false;
                    submitBtn.innerHTML = '<i aria-hidden="true" class="bi bi-check-lg me-1"></i>Save Settings';
                }
            }
        }

        async function loadAuthSecurity() {
            try {
                const res = await fetch('/api/v1/auth/security');
                if (!res.ok) return;
                const data = await res.json();
                const attempts = data.max_attempts || 5;
                const win = data.window || '5m';
                const lockout = data.lockout || '5m';

                const attemptsEl = document.getElementById('sec-max-attempts-text');
                if (attemptsEl) {
                    attemptsEl.innerText = `${attempts} attempts / ${win}`;
                }

                const lockoutEl = document.getElementById('sec-lockout-text');
                if (lockoutEl) {
                    lockoutEl.innerHTML = `HTTP 429 + <code>Retry-After</code> (${lockout})`;
                }

                const inputAttempts = document.getElementById('sec-input-max-attempts');
                if (inputAttempts) inputAttempts.value = attempts;

                const inputWindow = document.getElementById('sec-input-window');
                if (inputWindow) {
                    ensureOptionAndSelect(inputWindow, win);
                }

                const inputLockout = document.getElementById('sec-input-lockout');
                if (inputLockout) {
                    ensureOptionAndSelect(inputLockout, lockout);
                }
            } catch (err) {
                console.warn('Failed to load auth security settings:', err);
            }
        }

        function ensureOptionAndSelect(selectEl, val) {
            if (!selectEl) return;
            let found = false;
            for (let i = 0; i < selectEl.options.length; i++) {
                if (selectEl.options[i].value === val) {
                    selectEl.selectedIndex = i;
                    found = true;
                    break;
                }
            }
            if (!found) {
                const opt = document.createElement('option');
                opt.value = val;
                opt.text = val;
                selectEl.appendChild(opt);
                selectEl.value = val;
            }
        }


        async function logout() {
            if (updatePollTimer) {
                clearInterval(updatePollTimer);
                updatePollTimer = null;
            }
            if (typeof logsAutoRefreshTimer !== "undefined" && logsAutoRefreshTimer) {
                clearInterval(logsAutoRefreshTimer);
                logsAutoRefreshTimer = null;
            }
            try {
                await fetch('/api/v1/auth/logout', { method: 'POST' });
            } catch (e) {}
            location.reload();
        }

        // Dashboard Data
        async function loadDashboard() {
            restoreLibraryPreferences();
            await loadFeeds();
            if (typeof loadPersonalFeeds === 'function') {
                loadPersonalFeeds().catch(() => {});
            }
            restoreRoute();
        }

        // Feeds
        function showConnectionWarning(msg) {
            const el = document.getElementById("feeds-connection-alert");
            if (el) {
                if (msg) {
                    const span = el.querySelector("span");
                    if (span) span.textContent = msg;
                }
                el.classList.remove("d-none");
            }
        }

        function hideConnectionWarning() {
            const el = document.getElementById("feeds-connection-alert");
            if (el) el.classList.add("d-none");
        }

        function setFeedButtonUpdating(feedId, isUpdating) {
            const btn = document.getElementById(`btn-update-${feedId}`);
            if (btn) {
                if (isUpdating) {
                    btn.disabled = true;
                    btn.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Updating...`;
                } else {
                    btn.disabled = false;
                    btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i> Update`;
                }
            }
        }

        
        function syncPersonalIntoAllFeeds() {
            if (!Array.isArray(allFeeds)) return;
            const nonPersonal = allFeeds.filter(f => f.provider !== 'personal' && f.type !== 'personal');
            const pFeeds = (typeof personalFeeds !== 'undefined' && Array.isArray(personalFeeds) ? personalFeeds : []).map(pf => ({
                id: pf.id,
                title: pf.title,
                description: pf.description,
                author: 'Personal',
                provider: 'personal',
                cover_art: pf.cover_art,
                feed_url: pf.feed_url,
                episode_count: pf.episode_count || 0,
                downloaded_count: pf.episode_count || 0,
                total_size: 0,
                last_update: pf.updated_at || pf.created_at,
                private_feed: pf.private_feed,
                type: 'personal'
            }));
            allFeeds = [...nonPersonal, ...pFeeds];
            updateLibraryCounts();
            const countEl = document.getElementById("feeds-count");
            if (countEl) countEl.innerText = allFeeds.length;
            if (currentTab === "tab-feeds") filterFeeds();
        }

        function personalOpenFromLibrary(id) {
            const tabBtn = document.querySelector('#mainTabs [data-bs-target="#tab-personal"]');
            if (tabBtn) bootstrap.Tab.getOrCreateInstance(tabBtn).show();
            if (typeof personalOpen === 'function') personalOpen(id);
        }

        function personalConfirmDeleteFeed(id) {
            personalCurrentID = id;
            if (typeof personalDeleteFeed === 'function') personalDeleteFeed();
        }

        async function loadFeeds() {
            const seq = ++feedsFetchSeq;
            try {
                const res = await fetch("/api/v1/feeds");
                if (seq !== feedsFetchSeq) return;
                if (!res.ok) throw new Error(await res.text());
                const feeds = await res.json();
                if (seq !== feedsFetchSeq) return;
                allFeeds = feeds;
                syncPersonalIntoAllFeeds();
                hideConnectionWarning();
                updateLibraryCounts();
                if (currentTab === 'feed-detail') {
                    const feed = allFeeds.find(f => f.id === document.getElementById('episodesModal').dataset.feedId);
                    if (feed && episodeView?.feedId === feed.id) episodeView.title = feed.title || feed.id;
                    renderDetailHeader(feed);
                }

                const activeIds = new Set(allFeeds.filter(f => f.updating).map(f => f.id));
                updatingFeeds.clear();
                activeIds.forEach(id => updatingFeeds.add(id));
                if (activeIds.size > 0) startUpdatePolling();

                const countEl = document.getElementById("feeds-count");
                if (countEl) countEl.innerText = allFeeds.length;

                if (currentTab === "tab-feeds") {
                    filterFeeds();
                } else {
                    tabDirty["tab-feeds"] = true;
                }

                if (typeof renderAudiobookFeeds === "function") {
                    if (currentTab === "tab-audiobooks") {
                        renderAudiobookFeeds();
                    } else {
                        tabDirty["tab-audiobooks"] = true;
                    }
                }
            } catch (err) {
                if (seq !== feedsFetchSeq) return;
                showConnectionWarning('Could not refresh your library. Showing the last available data.');
                const container = document.getElementById("feeds-container");
                if (container && (!allFeeds || allFeeds.length === 0)) {
                    container.innerHTML = `<div class="col-12 text-center py-5 text-danger">Failed to load feeds: ${escapeHtml(err.message)}</div>`;
                }
            }
        }

        function formatBytes(bytes) {
            if (!bytes || bytes === 0) return "0 B";
            const k = 1024;
            const sizes = ["B", "KB", "MB", "GB", "TB"];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
        }

        function handleCoverError(img) {
            img.style.display = "none";
            const placeholder = img.parentElement ? img.parentElement.querySelector(".feed-cover-placeholder") : null;
            if (placeholder) {
                placeholder.classList.remove("d-none");
            }
        }

        const updatingFeeds = new Set();
        let updatePollTimer = null;
        let updatePollBusy = false;

        async function pollFeedStatuses() {
            if (updatePollBusy) return;
            updatePollBusy = true;
            const seq = ++statusFetchSeq;
            try {
                const res = await fetch("/api/v1/feeds/status");
                if (seq !== statusFetchSeq) return;
                if (!res.ok) throw new Error(`HTTP ${res.status}`);
                const statuses = await res.json();
                if (seq !== statusFetchSeq) return;

                pollFailureCount = 0;
                hideConnectionWarning();

                const completedFeeds = [];
                statuses.forEach(item => {
                    const isActive = item.status === "running" || item.status === "queued";
                    if (isActive) {
                        if (!updatingFeeds.has(item.id)) {
                            updatingFeeds.add(item.id);
                            setFeedButtonUpdating(item.id, true);
                        }
                    } else if (updatingFeeds.has(item.id)) {
                        completedFeeds.push(item.id);
                    }
                });

                completedFeeds.forEach(id => {
                    updatingFeeds.delete(id);
                    setFeedButtonUpdating(id, false);
                });

                if (currentTab === 'tab-feeds') filterFeeds();
                else tabDirty['tab-feeds'] = true;
                if (completedFeeds.length > 0) {
                    await loadFeeds();
                    const epModal = document.getElementById("episodesModal");
                    if (epModal && currentTab === 'feed-detail' && detailSection === 'episodes') {
                        const feedId = epModal.dataset.feedId;
                        if (feedId && completedFeeds.includes(feedId)) {
                            await showEpisodes(feedId, false);
                        }
                    }
                }

                if (updatingFeeds.size === 0) {
                    if (updatePollTimer) {
                        clearInterval(updatePollTimer);
                        updatePollTimer = null;
                    }
                    const allBtn = document.getElementById("btn-update-all-feeds");
                    if (allBtn) {
                        allBtn.disabled = false;
                        allBtn.innerHTML = "<i aria-hidden=\"true\" class=\"bi bi-arrow-repeat me-1\"></i> Update all";
                    }
                }
            } catch (err) {
                pollFailureCount++;
                if (pollFailureCount >= 2) {
                    showConnectionWarning("Connection lost to Podsync server, retrying...");
                }
            } finally {
                updatePollBusy = false;
            }
        }

        function startUpdatePolling() {
            if (updatePollTimer || document.hidden) return;
            updatePollTimer = setInterval(pollFeedStatuses, 2000);
        }

        function renderFeedCardHTML(feed) {
            const title = feed.title || feed.id || 'Untitled podcast';
            const isUpdating = updatingFeeds.has(feed.id);
            const hasError = feed.error_count > 0 || !!feed.last_error;
            const state = isUpdating
                ? '<span class="feed-state running"><i aria-hidden="true" class="bi bi-arrow-repeat"></i>Updating</span>'
                : hasError
                    ? `<button class="feed-state error" role="button" title="Click to view error log" ${actionAttrs('errors', feed.id)}><i aria-hidden="true" class="bi bi-exclamation-circle"></i>${feed.error_count > 0 ? feed.error_count + ' errors' : 'Update failed'}</button>`
                    : `<span class="feed-state"><i aria-hidden="true" class="bi bi-check2-circle"></i>${(feed.downloaded_count > 0 || feed.episode_count > 0) ? 'Ready' : 'No downloads yet'}</span>`;
            const isBook = feed.provider === 'audiobook';
            const isPersonal = feed.provider === 'personal' || feed.type === 'personal';
            const countLabel = isBook ? 'chapters' : 'episodes';
            const countIcon = isBook ? 'bi-file-earmark-music' : (isPersonal ? 'bi-collection-play' : 'bi-headphones');

            if (isPersonal) {
                return `<article class="card feed-card h-100">
                <div class="feed-card-top">
                    <div class="feed-art">${feed.cover_art && safeUrl(feed.cover_art) ? `<img src="${safeUrl(feed.cover_art)}" class="feed-cover" alt="" loading="lazy" onerror="handleCoverError(this)">` : ''}<div class="feed-cover-placeholder ${feed.cover_art && safeUrl(feed.cover_art) ? 'd-none' : ''}"><i aria-hidden="true" class="bi bi-bookmark-heart"></i></div></div>
                    <div class="flex-grow-1 min-w-0"><a class="feed-card-title" href="#personal/${encodeURIComponent(feed.id)}" ${actionAttrs('personal-open', feed.id)} title="${escapeHtml(title)}">${escapeHtml(title)}</a><div class="feed-card-author text-truncate">${escapeHtml(feed.id)}.xml</div></div>
                    <div class="dropdown feed-overflow"><button class="btn" data-bs-toggle="dropdown" aria-label="Actions for ${escapeHtml(title)}"><i aria-hidden="true" class="bi bi-three-dots"></i></button><ul class="dropdown-menu dropdown-menu-end">
                        <li><button class="dropdown-item" ${actionAttrs('personal-open', feed.id)}>Open</button></li>
                        <li><button class="dropdown-item" ${actionAttrs('personal-edit', feed.id)}>Settings</button></li>
                        <li><hr class="dropdown-divider"></li><li><button class="dropdown-item text-danger" ${actionAttrs('personal-delete', feed.id)}>Delete feed</button></li>
                    </ul></div>
                </div>
                <div class="feed-card-meta"><span><i aria-hidden="true" class="bi ${countIcon} me-1"></i>${feed.episode_count ?? feed.downloaded_count ?? 0} ${countLabel}</span><span class="badge bg-secondary-subtle text-secondary border">Universal</span><span class="feed-provider">Personal</span></div>
                <div class="feed-card-bottom"><span class="feed-state"><i aria-hidden="true" class="bi bi-check2-circle"></i>Ready</span><button class="btn btn-light" ${actionAttrs('personal-open', feed.id)}>Open</button><button class="btn btn-light" ${actionAttrs('subscribe', feed.id)}><i aria-hidden="true" class="bi bi-rss me-1"></i>Subscribe</button></div>
            </article>`;
            }

            return `<article class="card feed-card h-100">
                <div class="feed-card-top">
                    <div class="feed-art">${feed.cover_art && safeUrl(feed.cover_art) ? `<img src="${safeUrl(feed.cover_art)}" class="feed-cover" alt="" loading="lazy" onerror="handleCoverError(this)">` : ''}<div class="feed-cover-placeholder ${feed.cover_art && safeUrl(feed.cover_art) ? 'd-none' : ''}">${escapeHtml(title.trim()[0] || 'P')}</div></div>
                    <div class="flex-grow-1 min-w-0"><a class="feed-card-title" href="#feed/${encodeURIComponent(feed.id)}/episodes" title="${escapeHtml(title)}">${escapeHtml(title)}</a><div class="feed-card-author text-truncate">${escapeHtml(isValidAuthor(feed.author) ? feed.author : feed.provider || 'Podcast')}</div></div>
                    <div class="dropdown feed-overflow"><button class="btn" data-bs-toggle="dropdown" aria-label="Actions for ${escapeHtml(title)}"><i aria-hidden="true" class="bi bi-three-dots"></i></button><ul class="dropdown-menu dropdown-menu-end">
                        <li><button class="dropdown-item" id="btn-update-${escapeHtml(feed.id)}" ${actionAttrs('update', feed.id)} ${isUpdating ? 'disabled' : ''}>${isUpdating ? 'Updating…' : (isBook ? 'Scan chapters' : 'Update now')}</button></li>
                        <li><button class="dropdown-item" ${actionAttrs('settings', feed.id)}>Settings</button></li>
                        <li><button class="dropdown-item" ${actionAttrs('logs', feed.id)}>View activity</button></li>
                        <li><hr class="dropdown-divider"></li><li><button class="dropdown-item text-danger" ${actionAttrs('delete-feed', feed.id)}>${isBook ? 'Remove audiobook' : 'Delete podcast'}</button></li>
                    </ul></div>
                </div>
                <div class="feed-card-meta"><span><i aria-hidden="true" class="bi ${countIcon} me-1"></i>${feed.downloaded_count ?? 0} ${countLabel}</span><span>${formatBytes(feed.total_size)}</span><span class="feed-provider">${escapeHtml(feed.provider || '')}</span></div>
                <div class="feed-card-bottom">${state}<button class="btn btn-light" ${actionAttrs('episodes', feed.id)}>${isBook ? 'Chapters' : 'Open'}</button><button class="btn btn-light" ${actionAttrs('subscribe', feed.id)}><i aria-hidden="true" class="bi bi-rss me-1"></i>Subscribe</button></div>
            </article>`;
        }

        function renderFeeds(feeds) {
            const container = document.getElementById('feeds-container');
            if (!container) return;
            updateLibraryCounts();
            if (!feeds.length) {
                const filtered = allFeeds.length > 0;
                container.innerHTML = `<div class="library-empty"><i aria-hidden="true" class="bi ${filtered ? 'bi-search' : 'bi-collection-play'} fs-1 text-muted"></i><h2 class="h5 mt-3">${filtered ? 'No matching podcasts' : 'No configured podcasts'}</h2><p class="text-muted">${filtered ? 'Try another search or reset your filters.' : 'Add a channel or playlist to start your library.'}</p>${filtered ? `<button class="btn btn-light" ${actionAttrs('reset-filters')}>Reset filters</button>` : '<button class="btn btn-brand" data-bs-toggle="modal" data-bs-target="#addFeedModal">Add podcast</button>'}</div>`;
                return;
            }
            const existing = new Map(Array.from(container.querySelectorAll('[data-feed-id]')).map(el => [el.getAttribute('data-feed-id'), el]));
            if (!existing.size) {
                container.innerHTML = feeds.map(feed => `<div data-feed-id="${escapeHtml(feed.id)}" data-render-key="${escapeHtml(feedRenderKey(feed))}">${renderFeedCardHTML(feed)}</div>`).join('');
                return;
            }
            const ids = new Set(feeds.map(f => f.id));
            existing.forEach((el, id) => { if (!ids.has(id)) el.remove(); });
            let previous = null;
            for (const feed of feeds) {
                let el = existing.get(feed.id);
                const key = feedRenderKey(feed);
                if (!el) {
                    el = document.createElement('div');
                    el.setAttribute('data-feed-id', feed.id);
                }
                if (el.getAttribute('data-render-key') !== key) {
                    el.innerHTML = renderFeedCardHTML(feed);
                    el.setAttribute('data-render-key', key);
                }
                const expected = previous ? previous.nextElementSibling : container.firstElementChild;
                if (expected !== el) container.insertBefore(el, expected);
                previous = el;
            }
        }

        function feedRenderKey(feed) {
            return JSON.stringify([feed.id, feed.title, feed.author, feed.provider, feed.cover_art, feed.downloaded_count, feed.episode_count, feed.total_size, feed.error_count, feed.last_error, updatingFeeds.has(feed.id)]);
        }

        function getFilteredFeeds() {
            const query = (document.getElementById("feed-search")?.value || "").toLowerCase().trim();
            const typeFilter = document.getElementById("feed-type-filter")?.value || "all";

            return (allFeeds || []).filter(f => {
                const matchesQuery = !query || (
                    (f.id && f.id.toLowerCase().includes(query)) ||
                    (f.title && f.title.toLowerCase().includes(query)) ||
                    (f.author && f.author !== "<notfound>" && f.author.toLowerCase().includes(query)) ||
                    (f.provider && f.provider.toLowerCase().includes(query))
                );
                if (!matchesQuery) return false;
                if (feedStatusFilter === 'updating' && !updatingFeeds.has(f.id)) return false;
                if (feedStatusFilter === 'errors' && !(f.error_count > 0 || f.last_error)) return false;

                const isAudiobook = (f.provider === "audiobook") || (f.url && f.url.startsWith("audiobook://"));
                const isPersonal = (f.provider === "personal") || (f.type === "personal");
                if (typeFilter === "channels") return !isAudiobook && !isPersonal;
                if (typeFilter === "personal") return isPersonal;
                if (typeFilter === "audiobooks") return isAudiobook;
                return true;
            }).sort((a, b) => {
                const sort = document.getElementById('feed-sort')?.value || 'title';
                if (sort === 'size') return (b.total_size || 0) - (a.total_size || 0) || a.id.localeCompare(b.id);
                if (sort === 'updated') return (Date.parse(b.last_update) || 0) - (Date.parse(a.last_update) || 0) || a.id.localeCompare(b.id);
                return (a.title || a.id).localeCompare(b.title || b.id, undefined, {numeric: true}) || a.id.localeCompare(b.id);
            });
        }

        function filterFeeds() {
            const searchInput = document.getElementById("feed-search");
            const selStart = searchInput ? searchInput.selectionStart : null;
            const selEnd = searchInput ? searchInput.selectionEnd : null;
            const hadFocus = searchInput ? (document.activeElement === searchInput) : false;

            const filtered = getFilteredFeeds();
            renderFeeds(filtered);

            const query = (document.getElementById("feed-search")?.value || "").trim();
            const typeFilter = document.getElementById("feed-type-filter")?.value || "all";
            const isFiltered = query !== "" || typeFilter !== "all" || feedStatusFilter !== 'all';
            saveLibraryPreferences();
            const filterInfoEl = document.getElementById("feeds-filter-info");
            if (filterInfoEl) {
                if (isFiltered && allFeeds && allFeeds.length > 0) {
                    filterInfoEl.textContent = `Showing ${filtered.length} of ${allFeeds.length}`;
                    filterInfoEl.classList.remove("d-none");
                } else {
                    filterInfoEl.classList.add("d-none");
                }
            }

            if (hadFocus && searchInput) {
                searchInput.focus();
                if (selStart !== null && selEnd !== null) {
                    searchInput.setSelectionRange(selStart, selEnd);
                }
            }
        }

        function resetFeedFilters() {
            const searchInput = document.getElementById("feed-search");
            const typeFilter = document.getElementById("feed-type-filter");
            if (searchInput) searchInput.value = "";
            if (typeFilter) typeFilter.value = "all";
            setFeedStatus('all');
        }

        function copyFeedUrl(url) {
            copyToClipboard(url);
        }

        function copyToClipboard(text) {
            if (navigator.clipboard && navigator.clipboard.writeText) {
                navigator.clipboard.writeText(text).then(() => {
                    showToast("Link copied to clipboard!");
                }).catch(() => {
                    fallbackCopyText(text);
                });
            } else {
                fallbackCopyText(text);
            }
        }

        function fallbackCopyText(text) {
            try {
                const input = document.createElement('textarea');
                input.value = text;
                input.style.position = 'fixed';
                input.style.left = '-9999px';
                input.style.top = '-9999px';
                document.body.appendChild(input);
                input.focus();
                input.select();
                const successful = document.execCommand('copy');
                document.body.removeChild(input);
                if (successful) {
                    showToast("Link copied to clipboard!");
                } else {
                    prompt("Copy link:", text);
                }
            } catch (err) {
                prompt("Copy link:", text);
            }
        }

        function showQRCode(title, url) {
            document.getElementById('qr-feed-title').innerText = title;
            document.getElementById('qr-feed-url').innerText = url;
            
            const canvas = document.getElementById('qr-canvas');
            if (!qrInstance) {
                qrInstance = new QRious({
                    element: canvas,
                    size: 220,
                    value: url,
                    level: 'M'
                });
            } else {
                qrInstance.set({ value: url });
            }

            const modal = new bootstrap.Modal(document.getElementById('qrModal'));
            modal.show();
        }

        async function triggerUpdate(feedId) {
            const btn = document.getElementById(`btn-update-${feedId}`);
            updatingFeeds.add(feedId);

            if (btn) {
                btn.disabled = true;
                btn.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Updating...`;
            }

            try {
                const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}/update`, { method: 'POST' });
                if (res.ok) {
                    showToast(`Update triggered for ${feedId}`);
                    startUpdatePolling();
                } else {
                    showToast(`Failed to trigger update: ${await res.text()}`);
                    updatingFeeds.delete(feedId);
                    if (btn) {
                        btn.disabled = false;
                        btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i> Update`;
                    }
                }
            } catch (err) {
                showToast(`Network error: ${err.message}`);
                updatingFeeds.delete(feedId);
                if (btn) {
                    btn.disabled = false;
                    btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i> Update`;
                }
            }
        }

        async function triggerUpdateAll() {
            const btn = document.getElementById('btn-update-all-feeds');
            if (btn) {
                btn.disabled = true;
                btn.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Updating all...`;
            }

            try {
                const res = await fetch('/api/v1/feeds/update', { method: 'POST' });
                if (res.ok) {
                    const data = await res.json().catch(() => ({}));
                    const count = data.count !== undefined ? data.count : (data.feeds ? data.feeds.length : 0);
                    showToast(count > 0 ? `Update triggered for all ${count} podcasts` : 'Update triggered for all podcasts');

                    if (Array.isArray(data.feeds)) {
                        data.feeds.forEach(id => {
                            updatingFeeds.add(id);
                            const b = document.getElementById(`btn-update-${id}`);
                            if (b) {
                                b.disabled = true;
                                b.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Updating...`;
                            }
                        });
                    }

                    startUpdatePolling();
                } else {
                    showToast(`Failed to trigger update all: ${await res.text()}`);
                    if (btn) {
                        btn.disabled = false;
                        btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i> Update all`;
                    }
                }
            } catch (err) {
                showToast(`Network error: ${err.message}`);
                if (btn) {
                    btn.disabled = false;
                    btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i> Update all`;
                }
            }
        }

        async function triggerRetryAll() {
            const btn = document.getElementById('btn-retry-all-feeds');
            if (btn) {
                btn.disabled = true;
                btn.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Retrying...`;
            }

            try {
                const res = await fetch('/api/v1/system/errors?limit=1000');
                if (!res.ok) {
                    throw new Error(await res.text() || 'Failed to fetch error list');
                }
                const errorItems = await res.json();
                if (!errorItems || errorItems.length === 0) {
                    showToast('No failed episodes found to retry');
                    return;
                }

                let successCount = 0;
                for (const item of errorItems) {
                    if (!item.feed_id || !item.episode_id) continue;
                    try {
                        const retryRes = await fetch(`/api/v1/feeds/${encodeURIComponent(item.feed_id)}/episodes/${encodeURIComponent(item.episode_id)}/retry`, {
                            method: 'POST'
                        });
                        if (retryRes.ok) {
                            successCount++;
                        }
                    } catch (_) {}
                }

                showToast(`Reset ${successCount} episode(s), will retry on next update`);
                await loadFeeds();
            } catch (err) {
                showToast(`Retry all failed: ${err.message}`);
            } finally {
                if (btn) {
                    btn.disabled = false;
                    btn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-counterclockwise me-1"></i> Retry all`;
                }
            }
        }


        // Feed Settings Management
        let settingsRequestId = 0;
        async function openFeedSettings(feedId, activate = true) {
            const requestId = ++settingsRequestId;
            if (activate) {
                openFeedScreen(feedId);
                showFeedSection('settings', false);
            }
            const form = document.getElementById('feed-settings-form');
            const status = document.getElementById('settings-load-status');
            form.inert = true;
            document.getElementById('fs-feed-id').value = '';
            status.textContent = 'Loading settings…';
            status.classList.remove('d-none');
            let feed = allFeeds.find(f => f.id === feedId);
            if (!feed || !feed.config) {
                try {
                    const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}`);
                    if (!res.ok) throw new Error(await res.text());
                    const data = await res.json();
                    feed = data.feed;
                } catch (e) {
                    feed = null;
                    console.error('Failed to fetch feed details', e);
                }
            }
            if (requestId !== settingsRequestId || document.getElementById('episodesModal').dataset.feedId !== feedId) return;
            if (!feed || !feed.config) {
                status.textContent = 'Could not load settings. Select Settings to retry.';
                return;
            }
            status.classList.add('d-none');
            form.inert = false;

            const cfg = feed.config || {};
            const filters = cfg.filters || {};
            const custom = cfg.custom || {};
            const clean = cfg.clean || {};

            document.getElementById('fs-feed-id').value = feed.id;
            document.getElementById('feed-settings-subtitle').innerHTML = `Feed ID: <code>${escapeHtml(feed.id)}</code> &bull; ${escapeHtml(feed.title || '')}`;
            document.getElementById('fs-url').value = feed.url || cfg.url || '';

            // General & Schedule
            document.getElementById('fs-format').value = cfg.format || feed.format || 'audio';
            document.getElementById('fs-quality').value = cfg.quality || feed.quality || 'high';
            document.getElementById('fs-max-height').value = cfg.max_height || 0;
            document.getElementById('fs-page-size').value = cfg.page_size || feed.page_size || 50;
            document.getElementById('fs-cron-schedule').value = cfg.cron_schedule || feed.cron_schedule || '@every 12h';
            document.getElementById('fs-playlist-sort').value = cfg.playlist_sort || '';
            document.getElementById('fs-opml').checked = cfg.opml !== false;
            const isPrivate = (cfg.private_feed !== undefined) ? !!cfg.private_feed : !!feed.private_feed;
            document.getElementById('fs-private-feed').checked = isPrivate;
            const bookPrivate = document.getElementById('fs-book-private-feed');
            if (bookPrivate) bookPrivate.checked = isPrivate;

            document.getElementById('fs-private-feed').onchange = function() {
                const bp = document.getElementById('fs-book-private-feed');
                if (bp) bp.checked = this.checked;
            };
            if (bookPrivate) {
                bookPrivate.onchange = function() {
                    document.getElementById('fs-private-feed').checked = this.checked;
                };
            }

            toggleMaxHeight();

            // Retention
            document.getElementById('fs-keep-last').value = clean.keep_last !== undefined ? clean.keep_last : (feed.keep_last || 0);

            // Filters
            document.getElementById('fs-filter-title').value = filters.title || '';
            document.getElementById('fs-filter-not-title').value = filters.not_title || '';
            document.getElementById('fs-filter-desc').value = filters.description || '';
            document.getElementById('fs-filter-not-desc').value = filters.not_description || '';
            document.getElementById('fs-min-duration').value = filters.min_duration || '';
            document.getElementById('fs-max-duration').value = filters.max_duration || '';
            document.getElementById('fs-max-age').value = filters.max_age || '';

            // Custom Overrides
            document.getElementById('fs-custom-title').value = custom.title || '';
            document.getElementById('fs-custom-author').value = custom.author || '';
            document.getElementById('fs-custom-desc').value = custom.description || '';
            document.getElementById('fs-custom-cover').value = custom.cover_art || '';
            updateCoverPreview('fs');
            document.getElementById('fs-custom-category').value = custom.category || '';
            document.getElementById('fs-custom-lang').value = custom.lang || '';
            document.getElementById('fs-custom-explicit').checked = !!custom.explicit;

            window._initialFeedLanguage = custom.lang || '';
            const redownloadCb = document.getElementById('fs-redownload');
            if (redownloadCb) {
                redownloadCb.checked = false;
            }
            const langInput = document.getElementById('fs-custom-lang');
            if (langInput) {
                langInput.oninput = function() {
                    if (redownloadCb && this.value.trim() !== window._initialFeedLanguage) {
                        redownloadCb.checked = true;
                    }
                };
            }

            // Advanced
            const args = cfg.youtube_dl_args || [];
            document.getElementById('fs-ytdl-args').value = Array.isArray(args) ? args.join(' ') : (args || '');
            document.getElementById('fs-filename-template').value = cfg.filename_template || '';

            const cFile = (downloaderConfig && downloaderConfig.cookies_file) || 'data/cookies.txt';
            const fsHint = document.getElementById('fs-cookies-hint');
            if (fsHint) fsHint.innerText = `Global: ${cFile}`;
            const afHint = document.getElementById('new-feed-cookies-hint');
            if (afHint) afHint.innerText = `Global: ${cFile}`;

            const isAudiobook = (feed.provider === 'audiobook') || (feed.url && feed.url.startsWith('audiobook://'));
            window._isEditingAudiobook = isAudiobook;

            const titleEl = document.getElementById('feed-settings-title');
            const urlLabelEl = document.getElementById('fs-url-label');
            const btnGeneral = document.getElementById('fs-btn-general');
            const btnCustom = document.getElementById('fs-btn-custom');
            const navCleanup = document.getElementById('fs-nav-cleanup');
            const navFilters = document.getElementById('fs-nav-filters');
            const navAdvanced = document.getElementById('fs-nav-advanced');
            const channelMediaRow = document.getElementById('fs-channel-media-row');
            const channelPageSizeCol = document.getElementById('fs-channel-page-size-col');
            const channelSortRow = document.getElementById('fs-channel-sort-row');
            const channelRedownloadRow = document.getElementById('fs-channel-redownload-row');
            const channelYtdlArgsRow = document.getElementById('fs-channel-ytdl-args-row');
            const cronCol = document.getElementById('fs-cron-col');
            const cronHelp = document.getElementById('fs-cron-help');

            if (isAudiobook) {
                if (titleEl) titleEl.innerHTML = '<i aria-hidden="true" class="bi bi-book text-primary me-2"></i>Audiobook Settings';
                if (urlLabelEl) urlLabelEl.textContent = 'Audiobook Folder Path';
                if (btnCustom) btnCustom.innerHTML = '<i aria-hidden="true" class="bi bi-book me-1"></i>Book Metadata';
                if (btnGeneral) btnGeneral.innerHTML = '<i aria-hidden="true" class="bi bi-clock me-1"></i>General &amp; Access';
                document.getElementById('fs-cron-schedule').value = '';

                if (navCleanup) navCleanup.classList.add('d-none');
                if (navFilters) navFilters.classList.add('d-none');
                if (navAdvanced) navAdvanced.classList.add('d-none');
                if (channelMediaRow) channelMediaRow.classList.add('d-none');
                if (channelPageSizeCol) channelPageSizeCol.classList.add('d-none');
                if (channelSortRow) channelSortRow.classList.add('d-none');
                if (channelRedownloadRow) channelRedownloadRow.classList.add('d-none');
                if (channelYtdlArgsRow) channelYtdlArgsRow.classList.add('d-none');
                if (cronCol) cronCol.className = 'col-sm-8 d-none';

                // Activate Book Metadata tab first
                if (btnCustom) {
                    bootstrap.Tab.getOrCreateInstance(btnCustom).show();
                }
            } else {
                if (titleEl) titleEl.innerHTML = '<i aria-hidden="true" class="bi bi-sliders text-primary me-2"></i>Podcast Settings';
                if (urlLabelEl) urlLabelEl.textContent = 'Source URL';
                if (btnCustom) btnCustom.innerHTML = '<i aria-hidden="true" class="bi bi-card-text me-1"></i>RSS Overrides';
                if (btnGeneral) btnGeneral.innerHTML = '<i aria-hidden="true" class="bi bi-clock me-1"></i>General &amp; Schedule';
                if (cronHelp) cronHelp.textContent = 'Schedule to periodically check for new media files.';

                if (navCleanup) navCleanup.classList.remove('d-none');
                if (navFilters) navFilters.classList.remove('d-none');
                if (navAdvanced) navAdvanced.classList.remove('d-none');
                if (channelMediaRow) channelMediaRow.classList.remove('d-none');
                if (channelPageSizeCol) channelPageSizeCol.classList.remove('d-none');
                if (channelSortRow) channelSortRow.classList.remove('d-none');
                if (channelRedownloadRow) channelRedownloadRow.classList.remove('d-none');
                if (channelYtdlArgsRow) channelYtdlArgsRow.classList.remove('d-none');
                if (cronCol) cronCol.className = 'col-sm-8';

                // Activate General tab first
                if (btnGeneral) {
                    bootstrap.Tab.getOrCreateInstance(btnGeneral).show();
                }
            }

            renderDetailHeader(feed);
        }

        function toggleMaxHeight() {
            const format = document.getElementById('fs-format').value;
            const col = document.getElementById('fs-max-height-col');
            if (col) {
                col.style.display = format === 'video' ? 'block' : 'none';
            }
        }

        function setPresetCron(val) {
            document.getElementById('fs-cron-schedule').value = val;
        }

        function insertGlobalCookiesToArgs(prefix) {
            const input = document.getElementById(prefix + '-ytdl-args');
            if (!input) return;
            const cFile = (downloaderConfig && downloaderConfig.cookies_file) || 'data/cookies.txt';
            let val = input.value.trim();
            if (val.includes('--cookies')) {
                val = val.replace(/--cookies(\s+|=)[^\s]+/, `--cookies ${cFile}`);
            } else {
                val = val ? `${val} --cookies ${cFile}` : `--cookies ${cFile}`;
            }
            input.value = val;
        }

        function updateCoverPreview(prefix) {
            const input = document.getElementById(`${prefix}-custom-cover`);
            const preview = document.getElementById(`${prefix}-cover-preview`);
            const previewImg = document.getElementById(`${prefix}-cover-preview-img`);
            if (!input || !preview || !previewImg) return;
            const url = input.value.trim();
            if (url && safeUrl(url)) {
                previewImg.src = safeUrl(url);
                previewImg.onload = () => preview.classList.remove('d-none');
                previewImg.onerror = () => preview.classList.add('d-none');
            } else {
                preview.classList.add('d-none');
            }
        }

        async function uploadCoverArt(prefix) {
            const fileInput = document.getElementById(`${prefix}-cover-file`);
            const coverInput = document.getElementById(`${prefix}-custom-cover`);
            if (!fileInput || !fileInput.files || !fileInput.files[0]) return;
            const file = fileInput.files[0];

            let feedId = '';
            if (prefix === 'fs') {
                feedId = document.getElementById('fs-feed-id')?.value || '';
            } else if (prefix === 'new-feed') {
                feedId = document.getElementById('new-feed-id')?.value || '';
            }

            const formData = new FormData();
            formData.append('file', file);
            if (feedId) formData.append('feed_id', feedId);

            const uploadBtn = document.querySelector(`label[for="${prefix}-cover-file"]`);
            const origHtml = uploadBtn ? uploadBtn.innerHTML : '';
            if (uploadBtn) {
                uploadBtn.classList.add('disabled');
                uploadBtn.innerHTML = '<span class="spinner-border spinner-border-sm me-1" role="status"></span>Uploading...';
            }

            try {
                const res = await fetch('/api/v1/covers/upload', {
                    method: 'POST',
                    body: formData
                });
                if (!res.ok) {
                    throw new Error(await res.text());
                }
                const data = await res.json();
                if (data && data.url) {
                    if (coverInput) {
                        coverInput.value = data.url;
                        updateCoverPreview(prefix);
                    }
                    showToast('Cover art uploaded successfully');
                    if (prefix === 'fs' && feedId) {
                        const detailCover = document.getElementById('detail-cover');
                        if (detailCover) {
                            detailCover.innerHTML = `<img src="${safeUrl(data.url)}" alt="" onerror="this.style.display='none'">`;
                        }
                    }
                }
            } catch (err) {
                alert('Failed to upload cover art: ' + err.message);
            } finally {
                if (uploadBtn) {
                    uploadBtn.classList.remove('disabled');
                    uploadBtn.innerHTML = origHtml;
                }
                fileInput.value = '';
            }
        }

        async function saveFeedSettings(event) {
            if (event) event.preventDefault();
            const feedId = document.getElementById('fs-feed-id').value;
            if (!feedId) return;

            const saveBtn = event?.submitter || document.querySelector('#feed-settings-form button[type="submit"]');
            if (saveBtn) {
                saveBtn.disabled = true;
                saveBtn.innerHTML = '<span class="spinner-border spinner-border-sm me-1" role="status"></span>Saving...';
            }

            const ytdlArgsVal = document.getElementById('fs-ytdl-args').value.trim();
            const ytdlArgs = ytdlArgsVal ? ytdlArgsVal.split(/\s+/).filter(Boolean) : [];

            const isAudiobook = !!window._isEditingAudiobook;
            const opmlChecked = document.getElementById('fs-opml').checked;
            const updatePayload = {
                format: isAudiobook ? 'audio' : document.getElementById('fs-format').value,
                quality: isAudiobook ? 'high' : document.getElementById('fs-quality').value,
                max_height: isAudiobook ? 0 : (parseInt(document.getElementById('fs-max-height').value) || 0),
                page_size: isAudiobook ? 50 : (isNaN(parseInt(document.getElementById('fs-page-size').value, 10)) ? 50 : parseInt(document.getElementById('fs-page-size').value, 10)),
                cron_schedule: isAudiobook ? '' : document.getElementById('fs-cron-schedule').value.trim(),
                playlist_sort: isAudiobook ? '' : document.getElementById('fs-playlist-sort').value,
                opml: opmlChecked,
                private_feed: document.getElementById('fs-private-feed').checked || (document.getElementById('fs-book-private-feed') ? document.getElementById('fs-book-private-feed').checked : false),
                keep_last: isAudiobook ? 0 : (parseInt(document.getElementById('fs-keep-last').value) || 0),
                language: document.getElementById('fs-custom-lang').value.trim(),
                redownload: isAudiobook ? false : (document.getElementById('fs-redownload') ? document.getElementById('fs-redownload').checked : false),
                filters: isAudiobook ? {} : {
                    title: document.getElementById('fs-filter-title').value.trim(),
                    not_title: document.getElementById('fs-filter-not-title').value.trim(),
                    description: document.getElementById('fs-filter-desc').value.trim(),
                    not_description: document.getElementById('fs-filter-not-desc').value.trim(),
                    min_duration: parseInt(document.getElementById('fs-min-duration').value) || 0,
                    max_duration: parseInt(document.getElementById('fs-max-duration').value) || 0,
                    max_age: parseInt(document.getElementById('fs-max-age').value) || 0
                },
                custom: {
                    title: document.getElementById('fs-custom-title').value.trim(),
                    author: document.getElementById('fs-custom-author').value.trim(),
                    description: document.getElementById('fs-custom-desc').value.trim(),
                    cover_art: document.getElementById('fs-custom-cover').value.trim(),
                    category: document.getElementById('fs-custom-category').value.trim(),
                    lang: document.getElementById('fs-custom-lang').value.trim(),
                    explicit: document.getElementById('fs-custom-explicit').checked
                },
                youtube_dl_args: isAudiobook ? [] : ytdlArgs,
                filename_template: isAudiobook ? '' : document.getElementById('fs-filename-template').value.trim()
            };

            try {
                const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(updatePayload)
                });

                if (res.ok) {

                    showToast(`Settings for ${feedId} saved successfully`);
                    await loadFeeds();
                    renderAudiobookFeeds();

                    const isRedownloading = document.getElementById('fs-redownload') ? document.getElementById('fs-redownload').checked : false;
                    if (isRedownloading) {
                        showToast(`Settings for ${feedId} saved. Redownloading episodes with updated language...`);
                        updatingFeeds.add(feedId);
                        const b = document.getElementById(`btn-update-${feedId}`);
                        if (b) {
                            b.disabled = true;
                            b.innerHTML = `<span class="spinner-border spinner-border-sm me-1" role="status"></span> Updating...`;
                        }
                        startUpdatePolling();
                    }
                } else {
                    alert(`Failed to save settings: ${await res.text()}`);
                }
            } catch (err) {
                alert(`Error: ${err.message}`);
            } finally {
                if (saveBtn) {
                    saveBtn.disabled = false;
                    saveBtn.innerHTML = '<i aria-hidden="true" class="bi bi-check-lg me-1"></i>Save Settings';
                }
            }
        }

        async function deleteFeed(feedId) {
            if (!confirm(`Are you sure you want to delete podcast "${feedId}"?`)) return;
            const deleteFiles = confirm(`Also delete downloaded media files for podcast "${feedId}" from disk?`);

            try {
                const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}?delete_files=${deleteFiles}`, { method: 'DELETE' });
                if (res.ok) {
                    showToast(`Podcast ${feedId} deleted`);
                    if (currentTab === 'feed-detail') {
                        returnToLibrary();
                    }
                    loadFeeds();
                } else {
                    alert(`Failed to delete: ${await res.text()}`);
                }
            } catch (err) {
                alert(`Network error: ${err.message}`);
            }
        }

        // Add Feed
        let feedPreviewDebounceTimer = null;

        function handleFeedUrlInput(url) {
            suggestFeedId(url);
            if (feedPreviewDebounceTimer) clearTimeout(feedPreviewDebounceTimer);
            feedPreviewDebounceTimer = setTimeout(() => {
                triggerFeedPreview();
            }, 600);
        }

        async function triggerFeedPreview() {
            const urlInput = document.getElementById('new-feed-url');
            const previewCard = document.getElementById('rss-preview-card');
            if (!urlInput || !previewCard) return;

            const url = urlInput.value.trim();
            if (!url || !url.startsWith('http')) {
                previewCard.classList.add('d-none');
                return;
            }

            try {
                const res = await fetch('/api/v1/feeds/preview', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url: url })
                });

                if (!res.ok) {
                    previewCard.classList.add('d-none');
                    return;
                }

                const data = await res.json();
                if (data.provider === 'rss') {
                    previewCard.classList.remove('d-none');
                    document.getElementById('rss-preview-title').textContent = data.title || 'Podcast Feed';
                    document.getElementById('rss-preview-author').textContent = data.author || '';
                    document.getElementById('rss-preview-episodes').textContent = data.total_episodes || '0';
                    document.getElementById('rss-preview-size').textContent = formatBytes(data.total_bytes);

                    const artEl = document.getElementById('rss-preview-art');
                    if (data.cover_art) {
                        artEl.innerHTML = `<img src="${safeUrl(data.cover_art)}" class="w-100 h-100 object-fit-cover" alt="" onerror="this.parentElement.innerHTML='<i class=\"bi bi-rss fs-3\"></i>'">`;
                    } else {
                        artEl.innerHTML = `<i class="bi bi-rss fs-3"></i>`;
                    }



                    // For RSS podcasts, force audio format
                    const formatSelect = document.getElementById('new-feed-format');
                    if (formatSelect) {
                        formatSelect.value = 'audio';
                        toggleAddMaxHeight();
                    }

                    // Auto-suggest feed ID from podcast title if not manually entered
                    const idInputPreview = document.getElementById('new-feed-id');
                    if (idInputPreview && idInputPreview.dataset.manual !== 'true' && (!idInputPreview.value || idInputPreview.value === 'CLANBEAVER')) {
                        const titleSlug = toValidFeedSlug(data.title, true, '_');
                        if (titleSlug) {
                            idInputPreview.value = titleSlug;
                            idInputPreview.dispatchEvent(new Event('input'));
                        }
                    }

                    // Synchronize scope with current checked radio or default to 25
                    const checkedScope = document.querySelector('input[name="rss-scope"]:checked');
                    if (checkedScope) {
                        setRssScope(checkedScope.value);
                    } else {
                        setRssScope(25);
                    }
                } else {
                    previewCard.classList.add('d-none');
                }
            } catch (err) {
                previewCard.classList.add('d-none');
            }
        }

        function setRssScope(scope) {
            const pageSizeEl = document.getElementById('new-feed-pagesize');
            const keepLastEl = document.getElementById('new-feed-keeplast');
            if (scope === 'all') {
                if (pageSizeEl) pageSizeEl.value = '0';
                if (keepLastEl) keepLastEl.value = '0';
            } else {
                const count = parseInt(scope, 10) || 25;
                if (pageSizeEl) pageSizeEl.value = count;
                if (keepLastEl) keepLastEl.value = count;
            }
            const radio = document.getElementById(`rss-scope-${scope}`);
            if (radio) radio.checked = true;
        }

        function suggestFeedId(url) {
            if (!url) return;
            const idInput = document.getElementById('new-feed-id');
            if (idInput.value && idInput.dataset.manual === 'true') return;

            // Extract sensible slug
            try {
                const parsed = new URL(url);
                const path = parsed.pathname.replace(/^\/+/g, '').replace(/\/+$/g, '');
                const parts = path.split('/');
                let slug = parts[parts.length - 1] || '';
                slug = slug.replace(/\.(xml|rss|atom)$/i, '');
                if (slug.toLowerCase() === 'feed' || slug.toLowerCase() === 'rss' || slug.toLowerCase() === 'podcast') {
                    if (parts.length > 1) {
                        slug = parts[parts.length - 2];
                    } else {
                        slug = parsed.hostname.split('.')[0];
                    }
                }
                slug = toValidFeedSlug(slug, true, '_');
                if (slug) {
                    idInput.value = slug;
                    idInput.dispatchEvent(new Event('input'));
                }
            } catch (e) {}
        }

        function toggleAddMaxHeight() {
            const format = document.getElementById('new-feed-format').value;
            const videoCol = document.getElementById('new-feed-max-height-col');
            const audioCol = document.getElementById('new-feed-audio-quality-col');
            if (videoCol) {
                videoCol.style.display = format === 'video' ? 'block' : 'none';
            }
            if (audioCol) {
                audioCol.style.display = format === 'audio' ? 'block' : 'none';
            }
        }

        function setAddPresetCron(val) {
            document.getElementById('new-feed-cron-schedule').value = val;
        }

        const newFeedIdEl = document.getElementById('new-feed-id');
        const newFeedSlugPrev = document.getElementById('new-feed-slug-preview');
        if (newFeedIdEl) {
            setupFeedIdValidation(newFeedIdEl, newFeedSlugPrev, { uppercase: true, separator: '_' });
        }

        const importFeedIdEl = document.getElementById('import-feed-id');
        const importFeedSlugPrev = document.getElementById('import-feed-slug-preview');
        if (importFeedIdEl) {
            setupFeedIdValidation(importFeedIdEl, importFeedSlugPrev, { uppercase: false, separator: '-' });
        }

        let keepLastManual = false;
        const newFeedPageSizeEl = document.getElementById('new-feed-pagesize');
        const newFeedKeepLastEl = document.getElementById('new-feed-keeplast');
        const addFeedModalEl = document.getElementById('addFeedModal');
        const addFeedFormEl = document.getElementById('add-feed-form');

        if (newFeedPageSizeEl && newFeedKeepLastEl) {
            newFeedPageSizeEl.addEventListener('input', () => {
                if (!keepLastManual) {
                    newFeedKeepLastEl.value = newFeedPageSizeEl.value;
                }
            });

            newFeedKeepLastEl.addEventListener('input', () => {
                keepLastManual = true;
            });
        }

        if (addFeedModalEl) {
            addFeedModalEl.addEventListener('show.bs.modal', () => {
                const form = document.getElementById('add-feed-form');
                if (form) form.reset();
                const previewCard = document.getElementById('rss-preview-card');
                if (previewCard) previewCard.classList.add('d-none');
                updateCoverPreview('new-feed');
                keepLastManual = false;
                if (newFeedPageSizeEl && newFeedKeepLastEl) {
                    newFeedKeepLastEl.value = newFeedPageSizeEl.value || '50';
                }
                toggleAddMaxHeight();
                const idInput = document.getElementById('new-feed-id');
                if (idInput) {
                    idInput.dataset.manual = 'false';
                    idInput.dataset.touched = 'false';
                    idInput.classList.remove('is-invalid', 'is-valid');
                }
                const slugPrev = document.getElementById('new-feed-slug-preview');
                if (slugPrev) slugPrev.textContent = 'CLANBEAVER';
            });
        }

        if (addFeedFormEl) {
            addFeedFormEl.addEventListener('reset', () => {
                keepLastManual = false;
                updateCoverPreview('new-feed');
                setTimeout(() => {
                    if (newFeedPageSizeEl && newFeedKeepLastEl) {
                        newFeedKeepLastEl.value = newFeedPageSizeEl.value || '50';
                    }
                    toggleAddMaxHeight();
                    const idInput = document.getElementById('new-feed-id');
                    if (idInput) {
                        idInput.dataset.manual = 'false';
                        idInput.dataset.touched = 'false';
                        idInput.classList.remove('is-invalid', 'is-valid');
                    }
                    const slugPrev = document.getElementById('new-feed-slug-preview');
                    if (slugPrev) slugPrev.textContent = 'CLANBEAVER';
                }, 0);
            });
        }

        document.getElementById('add-feed-form').addEventListener('submit', async (e) => {
            e.preventDefault();
            const feedIdInput = document.getElementById('new-feed-id');
            const feedIdVal = feedIdInput ? feedIdInput.value.trim() : '';
            if (!feedIdVal || !/^[a-zA-Z0-9_-]+$/.test(feedIdVal)) {
                if (feedIdInput) {
                    feedIdInput.classList.add('is-invalid');
                    feedIdInput.focus();
                }
                showToast('Please provide a valid Feed ID using only English letters, numbers, dashes, and underscores.', 'warning');
                return;
            }
            const btn = document.getElementById('add-feed-btn');
            const spinner = document.getElementById('add-feed-spinner');
            const errBox = document.getElementById('add-feed-error');

            errBox.classList.add('d-none');
            spinner.classList.remove('d-none');
            btn.disabled = true;

            const ytdlArgsVal = (document.getElementById('new-feed-ytdl-args') ? document.getElementById('new-feed-ytdl-args').value.trim() : '');
            const ytdlArgs = ytdlArgsVal ? ytdlArgsVal.split(/\s+/).filter(Boolean) : [];

            const format = document.getElementById('new-feed-format').value;
            let quality = 'high';
            let maxHeight = 0;
            if (format === 'video') {
                quality = 'high';
                maxHeight = parseInt(document.getElementById('new-feed-max-height').value, 10) || 720;
            } else {
                quality = document.getElementById('new-feed-quality') ? document.getElementById('new-feed-quality').value : 'high';
                maxHeight = 0;
            }

            const payload = {
                id: document.getElementById('new-feed-id').value.trim(),
                url: document.getElementById('new-feed-url').value.trim(),
                format: format,
                quality: quality,
                max_height: maxHeight,
                page_size: isNaN(parseInt(document.getElementById('new-feed-pagesize').value, 10)) ? 50 : parseInt(document.getElementById('new-feed-pagesize').value, 10),
                cron_schedule: document.getElementById('new-feed-cron-schedule').value.trim(),
                playlist_sort: document.getElementById('new-feed-playlist-sort').value,
                opml: document.getElementById('new-feed-opml').checked,
                private_feed: document.getElementById('new-feed-private-feed').checked,
                keep_last: parseInt(document.getElementById('new-feed-keeplast').value, 10) || 0,
                language: document.getElementById('new-feed-custom-lang').value.trim(),
                filters: {
                    title: document.getElementById('new-feed-filter-title').value.trim(),
                    not_title: document.getElementById('new-feed-filter-nottitle').value.trim(),
                    description: (document.getElementById('new-feed-filter-desc') ? document.getElementById('new-feed-filter-desc').value.trim() : ''),
                    not_description: (document.getElementById('new-feed-filter-notdesc') ? document.getElementById('new-feed-filter-notdesc').value.trim() : ''),
                    min_duration: parseInt(document.getElementById('new-feed-min-duration').value) || 0,
                    max_duration: parseInt(document.getElementById('new-feed-max-duration').value) || 0,
                    max_age: parseInt(document.getElementById('new-feed-max-age').value) || 0
                },
                custom: {
                    title: document.getElementById('new-feed-custom-title').value.trim(),
                    author: document.getElementById('new-feed-custom-author').value.trim(),
                    description: document.getElementById('new-feed-custom-desc').value.trim(),
                    cover_art: document.getElementById('new-feed-custom-cover').value.trim(),
                    category: document.getElementById('new-feed-custom-category').value.trim(),
                    lang: document.getElementById('new-feed-custom-lang').value.trim(),
                    explicit: document.getElementById('new-feed-custom-explicit').checked
                },
                youtube_dl_args: ytdlArgs,
                filename_template: document.getElementById('new-feed-filename-template').value.trim()
            };

            try {
                const res = await fetch('/api/v1/feeds', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });

                if (res.ok) {
                    closeModal('addFeedModal');
                    document.getElementById('add-feed-form').reset();
                    keepLastManual = false;
                    if (newFeedPageSizeEl && newFeedKeepLastEl) {
                        newFeedKeepLastEl.value = newFeedPageSizeEl.value || '50';
                    }
                    toggleAddMaxHeight();
                    document.getElementById('new-feed-id').dataset.manual = 'false';
                    const firstTab = document.querySelector('#addFeedTabs button[data-bs-target="#af-tab-general"]');
                    if (firstTab) {
                        bootstrap.Tab.getOrCreateInstance(firstTab).show();
                    }
                    showToast('Podcast ' + payload.id + ' successfully added!');
                    loadFeeds();
                } else {
                    const data = await res.json();
                    errBox.innerText = data.error || 'Failed to add feed';
                    errBox.classList.remove('d-none');
                }
            } catch (err) {
                errBox.innerText = 'Network error: ' + err.message;
                errBox.classList.remove('d-none');
            } finally {
                spinner.classList.add('d-none');
                btn.disabled = false;
            }
        });

        // Episodes
        
        function viewFeedEpisodes(feedId) {
            return showEpisodes(feedId);
        }
        let episodesRequestId = 0;
        let episodeView = null;
        let episodePage = 0;
        const episodePageSize = 40;
        let episodeSearchTimer = null;

        function resetEpisodePage() {
            episodePage = 0;
            if (episodeSearchTimer) clearTimeout(episodeSearchTimer);
            episodeSearchTimer = setTimeout(() => {
                fetchAndRenderEpisodes();
            }, 250);
        }

        function changeEpisodePage(delta) {
            episodePage = Math.max(0, episodePage + delta);
            return fetchAndRenderEpisodes();
        }

        async function fetchAndRenderEpisodes() {
            if (!episodeView) return;
            const view = episodeView;
            const { feedId } = view;
            const requestId = ++episodesRequestId;
            const listEl = document.getElementById('episodes-list');
            const query = (document.getElementById('episodes-search')?.value || '').trim();
            const status = document.getElementById('episodes-status-filter')?.value || 'all';
            const duration = document.getElementById('episodes-duration-filter')?.value || 'all';
            const sort = document.getElementById('episodes-sort')?.value || 'default';

            listEl.innerHTML = `<div class="p-4 text-center text-muted"><div class="spinner-border spinner-border-sm text-primary mb-2"></div><div>Loading...</div></div>`;

            try {
                const offset = episodePage * episodePageSize;
                const url = `/api/v1/feeds/${encodeURIComponent(feedId)}?limit=${episodePageSize}&offset=${offset}&status=${encodeURIComponent(status)}&q=${encodeURIComponent(query)}&sort=${encodeURIComponent(sort)}&duration=${encodeURIComponent(duration)}`;
                const res = await fetch(url);
                if (!res.ok) throw new Error(await res.text());
                const data = await res.json();
                if (requestId !== episodesRequestId || episodeView !== view || document.getElementById('episodesModal').dataset.feedId !== feedId) return;

                view.episodes = Array.isArray(data.episodes) ? data.episodes : [];
                view.total = data.total ?? view.episodes.length;
                view.matching = data.matching ?? view.episodes.length;
                view.hasMore = !!data.has_more;
                view.statusCounts = data.status_counts || (view.total === view.episodes.length
                    ? view.episodes.reduce((counts, ep) => { counts[ep.status] = (counts[ep.status] || 0) + 1; return counts; }, {}) : {});
                if (data.offset != null) episodePage = Math.floor(data.offset / episodePageSize);
                if (data.feed) {
                    view.feedData = data.feed;
                    view.title = data.feed.title || feedId;
                    view.isAudiobook = data.feed.provider === 'audiobook';
                    renderDetailHeader(data.feed);
                }

                renderEpisodeList();
            } catch (err) {
                if (requestId !== episodesRequestId || episodeView !== view) return;
                listEl.innerHTML = `<div class="p-4 text-center text-danger">Error: ${escapeHtml(err.message)}</div>`;
            }
        }

        async function showEpisodes(feedId, openModal = true) {
            if (episodeSearchTimer) clearTimeout(episodeSearchTimer);
            if (!openModal && episodeView?.feedId === feedId) return fetchAndRenderEpisodes();
            const modalEl = document.getElementById('episodesModal');
            modalEl.dataset.feedId = feedId;
            const listEl = document.getElementById('episodes-list');

            const f = (allFeeds || []).find(x => x.id === feedId);
            const isAudiobook = f && ((f.provider === 'audiobook') || (f.url && f.url.startsWith('audiobook://')));
            document.getElementById('episodes-feed-title').innerText = isAudiobook ? `Audiobook Chapters: ${f?.title || feedId}` : `Podcast: ${f?.title || feedId}`;
            if (openModal) {
                episodePage = 0;
                openFeedScreen(feedId);
                showFeedSection('episodes', false);
                document.getElementById('episodes-search').value = '';
                document.getElementById('episodes-status-filter').value = 'all';
                if (document.getElementById('episodes-duration-filter')) document.getElementById('episodes-duration-filter').value = 'all';
                if (document.getElementById('episodes-sort')) document.getElementById('episodes-sort').value = 'default';
                document.getElementById('episodes-feed-subtitle').innerText = isAudiobook ? 'Loading chapters...' : 'Loading episodes...';
                listEl.innerHTML = `<div class="p-4 text-center text-muted"><div class="spinner-border spinner-border-sm text-primary mb-2"></div><div>Loading...</div></div>`;
            }

            episodeView = { feedId, title: f?.title || feedId, isAudiobook: !!isAudiobook, episodes: [], total: 0, matching: 0, feedData: f };
            return fetchAndRenderEpisodes();
        }

        function renderEpisodeList() {
            if (!episodeView) return;
            const { feedId, title, isAudiobook, episodes, total, matching, hasMore, statusCounts = {} } = episodeView;
            const listEl = document.getElementById('episodes-list');

            const parts = [];
            parts.push(`${isAudiobook ? 'Available' : 'Downloaded'}: ${statusCounts.downloaded || 0}`);
            for (const [value, label] of [['new', 'Pending'], ['error', 'Errors'], ['cleaned', 'Cleaned']]) {
                if (statusCounts[value]) parts.push(`${label}: ${statusCounts[value]}`);
            }
            parts.push(`Matching: ${matching} / ${total}`);

            const pages = Math.max(1, Math.ceil(matching / episodePageSize));
            episodePage = Math.min(episodePage, pages - 1);
            if (episodePage < 0) episodePage = 0;

            document.getElementById('episodes-page-label').innerText = `Page ${episodePage + 1} of ${pages}`;
            document.getElementById('episodes-prev').disabled = episodePage === 0;
            document.getElementById('episodes-next').disabled = !hasMore;
            document.getElementById('episode-pagination').classList[pages > 1 ? 'remove' : 'add']('d-none');
            document.getElementById('episodes-feed-subtitle').innerText = parts.join(' • ');
            document.getElementById('episodes-feed-title').innerText =
                `${isAudiobook ? 'Audiobook Chapters' : 'Podcast'}: ${title}`;

            if (!episodes.length) {
                listEl.innerHTML = total
                    ? '<div class="p-4 text-center text-muted">No episodes match the search or status filter.</div>'
                    : `<div class="p-4 text-center text-muted">No ${isAudiobook ? 'chapters' : 'episodes'} yet.
                        <div><button class="btn btn-outline-primary btn-sm mt-2" ${actionAttrs("update", feedId)}>Scan / Update Feed Now</button></div></div>`;
                return;
            }

            listEl.innerHTML = episodes.map(ep => {
                let badge = '<span class="badge bg-secondary">Unknown</span>';
                let size = formatBytes(ep.size);
                let actions = '';
                if (ep.status === 'downloaded') {
                    badge = `<span class="badge bg-success">${isAudiobook ? 'Available' : 'Downloaded'}</span>`;
                    if (ep.media_url) actions += `<a href="${safeUrl(ep.media_url)}" target="_blank" rel="noopener noreferrer" class="btn btn-outline-primary btn-sm" title="Play downloaded media"><i aria-hidden="true" class="bi bi-play-fill"></i> Play</a>`;
                } else if (ep.status === 'new') {
                    badge = '<span class="badge bg-primary">Pending</span>';
                    size = 'Not downloaded';
                } else if (ep.status === 'error') {
                    badge = '<span class="badge bg-danger">Error</span>';
                    size = 'Not downloaded';
                    actions += `<button class="btn btn-outline-danger btn-sm" ${actionAttrs("retry", feedId, ep.id)}><i aria-hidden="true" class="bi bi-arrow-clockwise"></i> Retry</button>`;
                } else if (ep.status === 'cleaned') {
                    badge = '<span class="badge bg-secondary">Cleaned</span>';
                    size = 'Deleted from disk';
                }
                if (!isAudiobook && /^https?:\/\//i.test(ep.video_url || '')) {
                    actions += `<a href="${safeUrl(ep.video_url)}" target="_blank" rel="noopener noreferrer" class="btn btn-outline-secondary btn-sm">Source</a>`;
                }
                const error = ep.status === 'error' && ep.error
                    ? `<div class="mt-2 p-2 rounded episode-error small text-break">${escapeHtml(ep.error)}</div>` : '';
                const date = isAudiobook ? '' : `<span><i aria-hidden="true" class="bi bi-calendar"></i> ${escapeHtml(formatDate(ep.pub_date))}</span>`;
                return `<div class="list-group-item p-3">
                    <div class="d-flex flex-column flex-sm-row align-items-start justify-content-between gap-3">
                        <div class="min-w-0 flex-grow-1">
                            <h6 class="fw-semibold mb-1 text-break">${isAudiobook && ep.order ? escapeHtml(ep.order) + '. ' : ''}${escapeHtml(ep.title || ep.id)}</h6>
                            <div class="text-muted small d-flex flex-wrap gap-3">
                                <span><i aria-hidden="true" class="bi bi-clock"></i> ${formatDuration(ep.duration)}</span>
                                <span>${size}</span>${date}
                            </div>${error}
                        </div>
                        <div class="flex-shrink-0 text-sm-end">
                            ${badge}<div class="d-flex flex-wrap gap-1 mt-2 justify-content-sm-end">${actions}</div>
                        </div>
                    </div>
                </div>`;
            }).join('');
        }

        async function retryEpisode(feedId, episodeId) {
            try {
                const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}/episodes/${encodeURIComponent(episodeId)}/retry`, { method: 'POST' });
                if (res.ok) {
                    showToast("Status reset, episode will be retried on next update");
                    showEpisodes(feedId, false);
                } else {
                    alert("Retry failed: " + await res.text());
                }
            } catch (e) {
                alert("Network error: " + e.message);
            }
        }

        function formatDuration(sec) {
            if (!sec) return '0:00';
            const h = Math.floor(sec / 3600);
            const m = Math.floor((sec % 3600) / 60);
            const s = Math.floor(sec % 60);
            return h ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`;
        }

        function formatDate(dateStr) {
            if (!dateStr) return '';
            try {
                const d = new Date(dateStr);
                return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString();
            } catch (e) {
                return dateStr;
            }
        }

        // Tokens
        async function loadTokens() {
            const listEl = document.getElementById('tokens-list');
            try {
                const res = await fetch('/api/v1/tokens');
                const tokens = await res.json();
                const list = Array.isArray(tokens) ? tokens : [];

                listEl.innerHTML = list.map(item => {
                    const tokenList = Array.isArray(item.tokens) ? item.tokens : [];
                    const count = tokenList.length;

                    let tokensBadge = '<span class="badge bg-secondary ms-2">Not set</span>';
                    if (count === 1) {
                        tokensBadge = `<span class="badge bg-success ms-2 font-monospace">${escapeHtml(tokenList[0])}</span>`;
                    } else if (count > 1) {
                        tokensBadge = `<span class="badge bg-primary-subtle text-primary border ms-2"><i aria-hidden="true" class="bi bi-arrow-repeat me-1"></i>${count} keys (Rotation)</span>`;
                    }

                    return `
                    <div class="border rounded p-3" style="background-color: var(--ctp-crust);">
                        <div class="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-2">
                            <div class="d-flex flex-wrap align-items-center">
                                <span class="fw-bold">${escapeHtml(item.provider.toUpperCase())}</span>
                                ${tokensBadge}
                            </div>
                            ${item.from_env ? '<span class="badge bg-info">From ENV</span>' : '<span class="badge bg-light text-muted border">From config.toml</span>'}
                        </div>

                        ${count > 0 ? `
                        <div class="d-flex flex-column gap-1 mb-2">
                            ${tokenList.map((tok, idx) => `
                                <div class="input-group input-group-sm">
                                    <span class="input-group-text font-monospace small bg-body-secondary text-muted" style="min-width: 44px;">#${idx + 1}</span>
                                    <input type="text" class="form-control form-control-sm font-monospace text-body-secondary" value="${escapeHtml(tok)}" readonly>
                                    ${!item.from_env ? `
                                        <button class="btn btn-outline-danger btn-sm" ${actionAttrs("delete-token", item.provider, idx, tok)} title="Delete token #${idx + 1} (${escapeHtml(tok)})">
                                            <i aria-hidden="true" class="bi bi-x-lg"></i>
                                        </button>
                                    ` : ''}
                                </div>
                            `).join('')}
                        </div>` : ''}

                        ${!item.from_env ? `
                        <div class="d-flex gap-2">
                            <div class="input-group input-group-sm">
                                <span class="input-group-text bg-body-secondary"><i aria-hidden="true" class="bi bi-key"></i></span>
                                <input type="text" id="token-input-${escapeHtml(item.provider)}" class="form-control form-control-sm font-monospace" placeholder="${count > 0 ? 'Add another key to rotation...' : (item.provider === 'vkvideo' ? 'Paste full URL from address bar or token...' : 'Enter API key(s) separated by space...')}" data-ui-token-provider="${escapeHtml(item.provider)}">
                                <button class="btn btn-primary btn-sm" ${actionAttrs("add-token", item.provider)}>
                                    <i aria-hidden="true" class="bi bi-plus-lg me-1"></i>Add
                                </button>
                            </div>
                            ${count > 0 ? `
                                <button class="btn btn-outline-danger btn-sm text-nowrap" ${actionAttrs("clear-tokens", item.provider)} title="Delete all tokens for ${escapeHtml(item.provider)}">
                                    <i aria-hidden="true" class="bi bi-trash3 me-1"></i>Clear All
                                </button>
                            ` : ''}
                        </div>
                        <div class="form-text small text-muted mt-1">
                            <i aria-hidden="true" class="bi bi-info-circle me-1"></i>Add single key or multiple space-separated keys for automatic round-robin rotation.
                        </div>` : `
                        <div class="text-muted small">
                            <i aria-hidden="true" class="bi bi-lock me-1"></i>Tokens are configured via environment variable (${escapeHtml(item.provider.toUpperCase())}_API_KEY) and cannot be edited via web UI.
                        </div>`}
                    </div>
                `;
                }).join('');
            } catch (err) {
                listEl.innerHTML = `<div class="text-danger small">Failed to load tokens: ${escapeHtml(err.message)}</div>`;
            }
        }

        
        async function deleteSingleToken(provider, index, maskedVal) {
            if (!confirm(`Are you sure you want to delete key #${index + 1} (${maskedVal || ''}) for ${provider.toUpperCase()}?`)) {
                return;
            }

            try {
                const res = await fetch(`/api/v1/tokens?provider=${encodeURIComponent(provider)}&index=${index}`, {
                    method: 'DELETE'
                });

                if (res.ok) {
                    showToast(`Key #${index + 1} for ${provider.toUpperCase()} deleted`);
                    loadTokens();
                } else {
                    alert(`Failed to delete key: ${await res.text()}`);
                }
            } catch (err) {
                alert(`Error: ${err.message}`);
            }
        }

        async function deleteTokens(provider) {
            if (!confirm(`Are you sure you want to delete and clear ALL configured API tokens for ${provider.toUpperCase()}?`)) {
                return;
            }

            try {
                const res = await fetch(`/api/v1/tokens?provider=${encodeURIComponent(provider)}`, {
                    method: 'DELETE'
                });

                if (res.ok) {
                    showToast(`All tokens for ${provider.toUpperCase()} cleared`);
                    const input = document.getElementById(`token-input-${provider}`);
                    if (input) input.value = '';
                    loadTokens();
                } else {
                    alert(`Failed to delete tokens: ${await res.text()}`);
                }
            } catch (err) {
                alert(`Error: ${err.message}`);
            }
        }

        function extractAccessToken(val) {
            if (!val) return '';
            val = val.trim();
            const match = val.match(/(?:[#?&]|\b)access_token=([^& \t\r\n#]+)/i);
            if (match) {
                return match[1];
            }
            return val;
        }

        function handleTokenInput(input, provider) {
            if (!input || !input.value) return;
            const original = input.value;
            const extracted = extractAccessToken(original);
            if (extracted !== original) {
                input.value = extracted;
                showToast(`Extracted ${provider.toUpperCase()} access token automatically!`);
            }
        }

        async function addToken(provider) {
            const input = document.getElementById(`token-input-${provider}`);
            let val = input.value.trim();
            if (!val) {
                alert('Please enter a token before adding');
                return;
            }
            const tokens = val.split(/\s+/).map(t => extractAccessToken(t)).filter(Boolean);

            try {
                const res = await fetch('/api/v1/tokens', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ provider, tokens, append: true })
                });

                if (res.ok) {
                    showToast(`Token for ${provider.toUpperCase()} added`);
                    input.value = '';
                    loadTokens();
                } else {
                    alert(`Failed to add: ${await res.text()}`);
                }
            } catch (err) {
                alert(`Error: ${err.message}`);
            }
        }

        function saveTokens(provider) {
            return addToken(provider);
        }

        // System
        async function loadSystem() {
            try {
                const res = await fetch('/api/v1/system');
                const data = await res.json();

                function normalizeVersion(ver) {
                    if (!ver || ver === 'dev' || ver === 'nightly') return 'v2.13.9';
                    return ver.startsWith('v') ? ver : `v${ver}`;
                }
                const ver = normalizeVersion(data.version);
                const navVerEl = document.getElementById('nav-version');
                if (navVerEl) navVerEl.innerText = ver;

                const sysVerEl = document.getElementById('sys-ver');
                if (sysVerEl) {
                    const commit = (data.commit && data.commit !== 'none' && data.commit !== 'local') ? ` (${data.commit})` : '';
                    sysVerEl.innerText = `${ver}${commit}`;
                }
                document.getElementById('sys-uptime').innerText = data.uptime || '-';
                document.getElementById('sys-ram').innerText = (data.memory_mb || 0).toFixed(1) + ' MB';
                document.getElementById('sys-goroutines').innerText = data.goroutines || 0;
                document.getElementById('sys-datadir').innerText = data.data_dir || '/app/data';
                document.getElementById('sys-storage').innerText = data.storage_type || 'local';

                // Disk
                const total = data.disk_total_bytes || 1;
                const used = data.disk_used_bytes || 0;
                const free = data.disk_free_bytes || 0;
                const pct = Math.round((used / total) * 100);

                document.getElementById('disk-used-text').innerText = formatBytes(used) + ' used';
                document.getElementById('disk-total-text').innerText = formatBytes(free);
                const pbar = document.getElementById('disk-progress');
                pbar.style.width = pct + '%';
                if (pct > 85) {
                    pbar.className = 'progress-bar bg-danger';
                } else if (pct > 65) {
                    pbar.className = 'progress-bar bg-warning';
                } else {
                    pbar.className = 'progress-bar bg-primary';
                }

                // Tools
                const ytdlpBadge = document.getElementById('badge-ytdlp');
                if (data.has_ytdlp) {
                    ytdlpBadge.className = 'badge bg-success';
                    ytdlpBadge.innerText = 'Available';
                } else {
                    ytdlpBadge.className = 'badge bg-danger';
                    ytdlpBadge.innerText = 'Not found';
                }

                const ffmpegBadge = document.getElementById('badge-ffmpeg');
                if (data.has_ffmpeg) {
                    ffmpegBadge.className = 'badge bg-success';
                    ffmpegBadge.innerText = 'Available';
                } else {
                    ffmpegBadge.className = 'badge bg-danger';
                    ffmpegBadge.innerText = 'Not found';
                }

            } catch (err) {
                console.error("System load error:", err);
            }
        }

        
        // Catppuccin Theme Management (Latte / Mocha)
        function initTheme() {
            const saved = localStorage.getItem('podsync-theme') || 
                (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'mocha' : 'latte');
            applyTheme(saved);
        }

        function applyTheme(theme) {
            document.documentElement.setAttribute('data-theme', theme);
            document.documentElement.setAttribute('data-bs-theme', theme === 'mocha' ? 'dark' : 'light');
            localStorage.setItem('podsync-theme', theme);

            const isMocha = theme === 'mocha';
            document.querySelectorAll('.theme-toggle-icon').forEach(el => {
                el.className = isMocha ? 'theme-toggle-icon bi bi-moon-stars-fill text-info' : 'theme-toggle-icon bi bi-sun-fill text-warning';
            });
            document.querySelectorAll('.theme-toggle-name').forEach(el => {
                el.innerText = isMocha ? 'Mocha' : 'Latte';
            });
        }

        function toggleTheme() {
            const current = document.documentElement.getAttribute('data-theme') || 'latte';
            applyTheme(current === 'mocha' ? 'latte' : 'mocha');
        }

        // Init theme immediately
        initTheme();

        // Init

        // Downloader & Cookies Management
        let downloaderConfig = null;

        async function loadDownloaderConfig() {
            try {
                const res = await fetch('/api/v1/downloader');
                if (!res.ok) throw new Error('Failed to load downloader config');
                downloaderConfig = await res.json();
                renderDownloaderConfig();

                // Also load cookies content
                const cookiesRes = await fetch('/api/v1/downloader/cookies');
                if (cookiesRes.ok) {
                    const data = await cookiesRes.json();
                    document.getElementById('dl-cookies-content').value = data.content || '';
                }
            } catch (e) {
                console.error('Error loading downloader config:', e);
            }
        }

        function renderDownloaderConfig() {
            if (!downloaderConfig) return;

            document.getElementById('dl-timeout').value = downloaderConfig.timeout || 10;
            document.getElementById('dl-proxy').value = downloaderConfig.proxy || '';
            document.getElementById('dl-language').value = downloaderConfig.language || '';
            document.getElementById('dl-custom-binary').value = downloaderConfig.custom_binary || '';
            document.getElementById('dl-self-update').checked = !!downloaderConfig.self_update;

            // Browser cookies parse
            const browserVal = downloaderConfig.cookies_from_browser || '';
            let browser = '', profile = '';
            if (browserVal.includes(':')) {
                const parts = browserVal.split(':');
                browser = parts[0];
                profile = parts.slice(1).join(':');
            } else {
                browser = browserVal;
            }
            document.getElementById('dl-browser-select').value = browser;
            document.getElementById('dl-browser-profile').value = profile;

            // Status Badges (yt-dlp & ffmpeg)
            const badgeText = document.getElementById('dl-badge-text');
            const dlBadge = document.getElementById('dl-status-badge');
            if (downloaderConfig.binary_version) {
                badgeText.innerText = `yt-dlp ${downloaderConfig.binary_version}`;
                if (dlBadge) dlBadge.className = 'badge mcp-status-pill px-3 py-2 fs-6 shadow-sm';
            } else if (downloaderConfig.binary_path) {
                badgeText.innerText = 'yt-dlp Ready';
                if (dlBadge) dlBadge.className = 'badge mcp-status-pill px-3 py-2 fs-6 shadow-sm';
            } else {
                badgeText.innerText = 'yt-dlp Not Found';
                if (dlBadge) dlBadge.className = 'badge bg-danger px-3 py-2 fs-6 shadow-sm';
            }

            const ffmpegText = document.getElementById('ffmpeg-badge-text');
            const ffmpegBadge = document.getElementById('ffmpeg-status-badge');
            if (downloaderConfig.ffmpeg_version) {
                if (ffmpegText) ffmpegText.innerText = `ffmpeg ${downloaderConfig.ffmpeg_version}`;
                if (ffmpegBadge) ffmpegBadge.className = 'badge mcp-status-pill px-3 py-2 fs-6 shadow-sm';
            } else if (downloaderConfig.ffmpeg_path) {
                if (ffmpegText) ffmpegText.innerText = 'ffmpeg Ready';
                if (ffmpegBadge) ffmpegBadge.className = 'badge mcp-status-pill px-3 py-2 fs-6 shadow-sm';
            } else {
                if (ffmpegText) ffmpegText.innerText = 'ffmpeg Not Found';
                if (ffmpegBadge) ffmpegBadge.className = 'badge bg-danger px-3 py-2 fs-6 shadow-sm';
            }

            const dlEnginePill = document.getElementById('dl-engine-pill');
            if (dlEnginePill) {
                dlEnginePill.innerText = downloaderConfig.binary_version ? `yt-dlp v${downloaderConfig.binary_version}` : (downloaderConfig.binary_path ? 'yt-dlp ready' : 'yt-dlp missing');
            }
            const dlFfmpegPill = document.getElementById('dl-ffmpeg-pill');
            if (dlFfmpegPill) {
                dlFfmpegPill.innerText = downloaderConfig.ffmpeg_version ? `ffmpeg v${downloaderConfig.ffmpeg_version}` : (downloaderConfig.ffmpeg_path ? 'ffmpeg ready' : 'ffmpeg missing');
            }

            // Cookies file status
            const statusInd = document.getElementById('cookies-status-indicator');
            const pathElem = document.getElementById('cookies-file-path');
            const sizeElem = document.getElementById('cookies-file-size');
            const linesElem = document.getElementById('cookies-file-lines');
            const modElem = document.getElementById('cookies-last-mod');

            if (downloaderConfig.has_cookies_file) {
                statusInd.className = 'badge bg-success';
                statusInd.innerText = 'Cookies Active';
                pathElem.innerText = downloaderConfig.cookies_file || 'data/cookies.txt';
                sizeElem.innerText = formatBytes(downloaderConfig.cookies_file_size || 0);
                linesElem.innerText = `${downloaderConfig.cookies_file_lines || 0} lines`;
                modElem.innerText = downloaderConfig.cookies_last_modified || '-';
            } else {
                statusInd.className = 'badge bg-secondary';
                statusInd.innerText = 'Not Installed';
                pathElem.innerText = downloaderConfig.cookies_file || 'None configured';
                sizeElem.innerText = '-';
                linesElem.innerText = '-';
                modElem.innerText = '-';
            }
        }

        async function saveDownloaderSettings(e) {
            e.preventDefault();
            const timeout = parseInt(document.getElementById('dl-timeout').value, 10) || 10;
            const proxy = document.getElementById('dl-proxy').value.trim();
            const language = document.getElementById('dl-language').value.trim();
            const customBinary = document.getElementById('dl-custom-binary').value.trim();
            const selfUpdate = document.getElementById('dl-self-update').checked;

            const browser = document.getElementById('dl-browser-select').value;
            const profile = document.getElementById('dl-browser-profile').value.trim();
            let browserVal = browser;
            if (browser && profile) {
                browserVal = `${browser}:${profile}`;
            }

            try {
                const res = await fetch('/api/v1/downloader', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        timeout: timeout,
                        proxy: proxy,
                        language: language,
                        custom_binary: customBinary,
                        self_update: selfUpdate,
                        cookies_from_browser: browserVal,
                    })
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Failed to update downloader settings');
                }
                alert('Downloader settings saved successfully!');
                await loadDownloaderConfig();
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function saveCookiesContent() {
            const content = document.getElementById('dl-cookies-content').value;
            try {
                const res = await fetch('/api/v1/downloader/cookies', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: content })
                });
                if (!res.ok) {
                    const err = await res.json();
                    throw new Error(err.error || 'Failed to save cookies');
                }
                alert('Cookies saved and applied to downloader & feeds!');
                await loadDownloaderConfig();
                await loadFeeds();
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        async function clearCookiesContent() {
            if (!confirm('Are you sure you want to clear/delete the cookies file?')) return;
            try {
                const res = await fetch('/api/v1/downloader/cookies', { method: 'DELETE' });
                if (!res.ok) throw new Error('Failed to delete cookies');
                document.getElementById('dl-cookies-content').value = '';
                alert('Cookies cleared successfully.');
                await loadDownloaderConfig();
                await loadFeeds();
            } catch (err) {
                alert('Error: ' + err.message);
            }
        }

        function uploadCookiesFile(event) {
            const file = event.target.files[0];
            if (!file) return;
            const reader = new FileReader();
            reader.onload = async function(e) {
                document.getElementById('dl-cookies-content').value = e.target.result;
                await saveCookiesContent();
            };
            reader.readAsText(file);
        }

        async function testDownloaderLive() {
            const urlInput = document.getElementById('dl-test-url');
            const url = urlInput.value.trim();
            const btn = document.getElementById('btn-test-downloader');
            const spinner = document.getElementById('test-dl-spinner');
            const resultContainer = document.getElementById('dl-test-result');

            btn.disabled = true;
            spinner.classList.remove('d-none');
            resultContainer.className = 'd-none';

            try {
                const res = await fetch('/api/v1/downloader/test', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url: url })
                });
                const data = await res.json();
                resultContainer.classList.remove('d-none');

                if (data.success) {
                    resultContainer.className = 'alert alert-success mt-3 small shadow-sm';
                    resultContainer.innerHTML = `
                        <div class="fw-bold mb-1"><i aria-hidden="true" class="bi bi-check-circle-fill me-1"></i> Success! yt-dlp extracted video metadata:</div>
                        <div><strong>Title:</strong> ${escapeHtml(data.title || 'Unknown')}</div>
                        <div><strong>Channel:</strong> ${escapeHtml(data.channel || 'Unknown')}</div>
                        <div class="text-muted mt-1 small">Cookies &amp; proxy verified. YouTube bot protection bypassed successfully!</div>
                    `;
                } else {
                    resultContainer.className = 'alert alert-danger mt-3 small shadow-sm';
                    resultContainer.innerHTML = `
                        <div class="fw-bold mb-1"><i aria-hidden="true" class="bi bi-exclamation-triangle-fill me-1"></i> yt-dlp reported an error:</div>
                        <pre class="p-2 my-1 font-monospace small" style="max-height: 200px; overflow-y: auto;">${escapeHtml(data.error || 'Unknown error')}</pre>
                        <div class="mt-1 small">Tip: Ensure your <code>cookies.txt</code> is fresh and exported while logged into YouTube.</div>
                    `;
                }
            } catch (err) {
                resultContainer.classList.remove('d-none');
                resultContainer.className = 'alert alert-danger mt-3 small shadow-sm';
                resultContainer.innerHTML = `<strong>Error:</strong> ${escapeHtml(err.message)}`;
            } finally {
                btn.disabled = false;
                spinner.classList.add('d-none');
            }
        }

        checkAuth();

        // MCP Configuration & Generators
        let mcpConfigData = null;
        let currentOpencodeMode = 'remote';

        async function loadMCPConfig() {
            try {
                const res = await fetch('/api/v1/mcp/config');
                if (!res.ok) throw new Error('Failed to load MCP config');
                mcpConfigData = await res.json();
                renderMCPConfig();
            } catch (e) {
                console.error('Error loading MCP config:', e);
            }
        }

        function renderMCPConfig() {
            if (!mcpConfigData) return;
            const origin = window.location.origin;
            const sseUrl = `${origin}/mcp/sse`;
            const username = mcpConfigData.username || 'admin';
            const password = mcpConfigData.password || '';
            const basicAuth = mcpConfigData.basic_auth_header || ('Basic ' + btoa(username + ':' + password));
            const binPath = mcpConfigData.binary_path || '/path/to/bin/podsync';
            const cfgPath = mcpConfigData.config_path || '/path/to/config.toml';
            const cmdLine = `${binPath} --config ${cfgPath} --mcp`;

            const sseElem = document.getElementById('mcp-sse-url');
            if (sseElem) sseElem.value = sseUrl;

            const userElem = document.getElementById('mcp-username');
            if (userElem) userElem.value = username;

            const passElem = document.getElementById('mcp-password');
            if (passElem) passElem.value = password;

            const authElem = document.getElementById('mcp-basic-auth');
            if (authElem) authElem.value = basicAuth;

            const binElem = document.getElementById('mcp-binary-path');
            if (binElem) binElem.value = binPath;

            const cfgElem = document.getElementById('mcp-config-path');
            if (cfgElem) cfgElem.value = cfgPath;

            const cmdElem = document.getElementById('mcp-cmdline');
            if (cmdElem) cmdElem.value = cmdLine;

            // Skill path
            let projectDir = cfgPath;
            if (projectDir.lastIndexOf('/') !== -1) {
                projectDir = projectDir.substring(0, projectDir.lastIndexOf('/'));
            } else if (projectDir.lastIndexOf('\\') !== -1) {
                projectDir = projectDir.substring(0, projectDir.lastIndexOf('\\'));
            }
            const skillPath = `${projectDir}/.agents/skills/podsync/SKILL.md`;
            const skillElem = document.getElementById('mcp-skill-path');
            if (skillElem) skillElem.value = skillPath;

            // Render client snippets
            renderOpencodeSnippet();
            renderClaudeSnippet();
            renderCursorSnippet();
        }

        function setOpencodeMode(mode) {
            currentOpencodeMode = mode;
            const btnRemote = document.getElementById('btn-opencode-remote');
            const btnLocal = document.getElementById('btn-opencode-local');
            if (btnRemote && btnLocal) {
                if (mode === 'remote') {
                    btnRemote.className = 'btn btn-primary';
                    btnLocal.className = 'btn btn-outline-secondary';
                } else {
                    btnRemote.className = 'btn btn-outline-secondary';
                    btnLocal.className = 'btn btn-primary';
                }
            }
            renderOpencodeSnippet();
        }

        function renderOpencodeSnippet() {
            if (!mcpConfigData) return;
            const origin = window.location.origin;
            const sseUrl = `${origin}/mcp/sse`;
            const basicAuth = mcpConfigData.basic_auth_header || ('Basic ' + btoa((mcpConfigData.username || 'admin') + ':' + (mcpConfigData.password || '')));
            const binPath = mcpConfigData.binary_path || '/path/to/bin/podsync';
            const cfgPath = mcpConfigData.config_path || '/path/to/config.toml';

            let configObj = {};
            if (currentOpencodeMode === 'remote') {
                configObj = {
                    "$schema": "https://opencode.ai/config.json",
                    "mcp": {
                        "servers": {
                            "podsync": {
                                "type": "remote",
                                "url": sseUrl,
                                "headers": {
                                    "Authorization": basicAuth
                                },
                                "enabled": true
                            }
                        }
                    }
                };
            } else {
                configObj = {
                    "$schema": "https://opencode.ai/config.json",
                    "mcp": {
                        "servers": {
                            "podsync": {
                                "type": "local",
                                "command": [binPath, "--config", cfgPath, "--mcp"],
                                "enabled": true
                            }
                        }
                    }
                };
            }
            const snippet = document.getElementById('opencode-snippet');
            if (snippet) snippet.innerText = JSON.stringify(configObj, null, 2);
        }

        function renderClaudeSnippet() {
            if (!mcpConfigData) return;
            const binPath = mcpConfigData.binary_path || '/path/to/bin/podsync';
            const cfgPath = mcpConfigData.config_path || '/path/to/config.toml';
            const claudeObj = {
                "mcpServers": {
                    "podsync": {
                        "command": binPath,
                        "args": [
                            "--config",
                            cfgPath,
                            "--mcp"
                        ]
                    }
                }
            };
            const snippet = document.getElementById('claude-snippet');
            if (snippet) snippet.innerText = JSON.stringify(claudeObj, null, 2);
        }

        function renderCursorSnippet() {
            if (!mcpConfigData) return;
            const binPath = mcpConfigData.binary_path || '/path/to/bin/podsync';
            const cfgPath = mcpConfigData.config_path || '/path/to/config.toml';
            const cursorObj = {
                "name": "podsync",
                "type": "command",
                "command": `${binPath} --config ${cfgPath} --mcp`
            };
            const snippet = document.getElementById('cursor-snippet');
            if (snippet) snippet.innerText = JSON.stringify(cursorObj, null, 2);
        }

        function togglePasswordVisibility(inputId, btn) {
            const input = document.getElementById(inputId);
            if (!input) return;
            if (input.type === 'password') {
                input.type = 'text';
                btn.innerHTML = '<i aria-hidden="true" class="bi bi-eye-slash"></i>';
            } else {
                input.type = 'password';
                btn.innerHTML = '<i aria-hidden="true" class="bi bi-eye"></i>';
            }
        }

        function copyInput(inputId, btn) {
            const input = document.getElementById(inputId);
            if (!input) return;
            navigator.clipboard.writeText(input.value).then(() => {
                showCopyFeedback(btn);
            });
        }

        function copyPre(preId, btn) {
            const pre = document.getElementById(preId);
            if (!pre) return;
            navigator.clipboard.writeText(pre.innerText).then(() => {
                showCopyFeedback(btn);
            });
        }

        function showCopyFeedback(btn) {
            const origHTML = btn.innerHTML;
            btn.innerHTML = '<i aria-hidden="true" class="bi bi-check2 text-success"></i> Copied!';
            setTimeout(() => {
                btn.innerHTML = origHTML;
            }, 1500);
        }


        // Logs Management State & Logic
        let logsAutoRefreshTimer = null;
        let logsDebounceTimer = null;
        let currentLogsLevel = 'all';
        let currentLogsFeed = '';
        let lastLoadedLogEntries = [];
        let logsRequestId = 0;
        let logsBusy = false;
        let renderedLogsKey = '';
        let renderedLogRows = [];

        function setLogsLevel(lvl, reload = true) {
            currentLogsLevel = lvl;
            const group = document.getElementById('logs-level-group');
            if (group) {
                Array.from(group.querySelectorAll('button')).forEach(btn => {
                    btn.classList.remove('active');
                });
                const activeBtn = group.querySelector(`button[onclick*="'${lvl}'"]`);
                if (activeBtn) activeBtn.classList.add('active');
            }
            if (reload) loadLogs(true);
        }

        function onLogsFeedChange() {
            currentLogsFeed = document.getElementById('logs-feed-select').value;
            loadLogs(true);
        }

        function debounceLogsFilter() {
            clearTimeout(logsDebounceTimer);
            logsDebounceTimer = setTimeout(() => {
                loadLogs(true);
            }, 300);
        }

        function clearLogsSearch() {
            const input = document.getElementById('logs-search-input');
            if (input) input.value = '';
            loadLogs(true);
        }

        function setLogsAutoRefresh(intervalMs) {
            if (logsAutoRefreshTimer) {
                clearInterval(logsAutoRefreshTimer);
                logsAutoRefreshTimer = null;
            }
            const dot = document.getElementById('logs-live-dot');
            const ms = parseInt(intervalMs, 10);
            if (ms > 0 && !document.hidden && currentTab === 'tab-logs') {
                if (dot) dot.classList.remove('d-none');
                logsAutoRefreshTimer = setInterval(() => {
                    const logsTab = document.getElementById('tab-logs');
                    if (logsTab && logsTab.classList.contains('active')) {
                        loadLogs(false);
                    }
                }, ms);
            } else {
                if (dot) dot.classList.add('d-none');
            }
        }

        async function loadLogs(manual = false) {
            if (logsBusy && !manual) return;
            logsBusy = true;
            const requestId = ++logsRequestId;
            const terminal = document.getElementById('logs-terminal');
            const countBadge = document.getElementById('logs-count-badge');
            const statusText = document.getElementById('logs-status-text');
            const levelLabel = document.getElementById('logs-server-level-label');
            const debugSwitch = document.getElementById('logs-server-debug');
            const searchInput = document.getElementById('logs-search-input');
            const limitSelect = document.getElementById('logs-limit-select');
            const feedSelect = document.getElementById('logs-feed-select');

            if (feedSelect && feedSelect.options.length <= 1 && allFeeds.length > 0) {
                allFeeds.forEach(f => {
                    const opt = document.createElement('option');
                    opt.value = f.id;
                    opt.innerText = f.title ? `${f.title} (${f.id})` : f.id;
                    feedSelect.appendChild(opt);
                });
                if (currentLogsFeed) {
                    feedSelect.value = currentLogsFeed;
                }
            }

            const limit = limitSelect ? limitSelect.value : 250;
            const search = searchInput ? searchInput.value.trim() : '';
            const feedId = feedSelect ? feedSelect.value : currentLogsFeed;

            let query = `?limit=${limit}`;
            if (currentLogsLevel && currentLogsLevel !== 'all') {
                query += `&level=${encodeURIComponent(currentLogsLevel)}`;
            }
            if (search) {
                query += `&search=${encodeURIComponent(search)}`;
            }
            if (feedId) {
                query += `&feed_id=${encodeURIComponent(feedId)}`;
            }

            if (statusText) statusText.innerText = manual ? 'Refreshing...' : 'Updating...';

            try {
                const res = await fetch(`/api/v1/system/logs${query}`);
                if (!res.ok) throw new Error(await res.text());
                const data = await res.json();
                if (requestId !== logsRequestId) return;

                const entries = data.entries || [];
                lastLoadedLogEntries = entries;

                if (countBadge) {
                    countBadge.innerText = `${entries.length} entries` + (data.total ? ` (buffer: ${data.total})` : '');
                }

                if (levelLabel && data.current_level) {
                    levelLabel.innerHTML = `Server log level: <strong class="text-uppercase ${data.current_level === 'debug' ? 'text-info' : 'text-primary'}">${escapeHtml(data.current_level)}</strong>`;
                }

                if (debugSwitch) {
                    debugSwitch.checked = (data.current_level === 'debug');
                }

                let errorCount = 0;
                entries.forEach(e => {
                    if (e.level === 'error' || e.level === 'fatal' || e.level === 'panic') {
                        errorCount++;
                    }
                });
                const navBadge = document.getElementById('nav-logs-badge');
                if (navBadge) {
                    if (errorCount > 0) {
                        navBadge.innerText = errorCount;
                        navBadge.classList.remove('d-none');
                    } else {
                        navBadge.classList.add('d-none');
                    }
                }

                const newLogsKey = JSON.stringify(entries);
                if (newLogsKey === renderedLogsKey) {
                    if (statusText) statusText.innerText = 'Last checked: ' + new Date().toLocaleTimeString();
                    return;
                }
                renderedLogsKey = newLogsKey;
                if (entries.length === 0) {
                    renderedLogRows = [];
                    terminal.innerHTML = `
                        <div class="text-muted text-center py-5">
                            <i aria-hidden="true" class="bi bi-journal-x fs-1 mb-2 d-block opacity-50"></i>
                            <div>No log entries found matching criteria.</div>
                            <div class="small mt-1">Try changing level, clearing search filter, or re-triggering podcast update.</div>
                        </div>`;
                } else {
                    const linesHtml = entries.map(e => {
                        const lvl = (e.level || 'info').toLowerCase();
                        let badgeClass = 'log-badge-info';
                        if (lvl === 'error' || lvl === 'fatal' || lvl === 'panic') badgeClass = 'log-badge-error';
                        else if (lvl === 'warn' || lvl === 'warning') badgeClass = 'log-badge-warn';
                        else if (lvl === 'debug') badgeClass = 'log-badge-debug';

                        const timeStr = formatLogTimestamp(e.timestamp);

                        let fieldsHtml = '';
                        if (e.fields && Object.keys(e.fields).length > 0) {
                            fieldsHtml = Object.entries(e.fields).map(([k, v]) => {
                                return `<span class="log-tag" ${actionAttrs("filter-log", k, String(v))} title="Filter by ${escapeHtml(k)}">${escapeHtml(k)}=${escapeHtml(String(v))}</span>`;
                            }).join('');
                        }

                        return `
                            <div class="log-line">
                                <span class="log-time font-monospace">${escapeHtml(timeStr)}</span>
                                <span class="log-badge ${badgeClass}">${escapeHtml(lvl)}</span>
                                <div class="log-msg">
                                    ${fieldsHtml}
                                    <span class="log-text">${escapeHtml(e.message)}</span>
                                </div>
                            </div>
                        `;
                    }).join('');

                    const newRows = entries.map(e => JSON.stringify(e));
                    const canAppend = renderedLogRows.length > 0 && newRows.length >= renderedLogRows.length && renderedLogRows.every((row, i) => row === newRows[i]);
                    if (canAppend) {
                        const fragment = document.createElement('div');
                        fragment.innerHTML = linesHtml;
                        Array.from(fragment.children).slice(renderedLogRows.length).forEach(el => terminal.appendChild(el));
                    } else {
                        const scrollTop = terminal.scrollTop;
                        terminal.innerHTML = linesHtml;
                        terminal.scrollTop = scrollTop;
                    }
                    renderedLogRows = newRows;

                    const autoscroll = document.getElementById('logs-autoscroll');
                    if (autoscroll && autoscroll.checked) {
                        terminal.scrollTop = terminal.scrollHeight;
                    }
                }

                if (statusText) statusText.innerText = 'Last updated: ' + new Date().toLocaleTimeString();

            } catch (err) {
                if (requestId !== logsRequestId) return;
                if (statusText) statusText.innerText = 'Error: ' + err.message;
                if (manual) {
                    showToast('Failed to load logs: ' + err.message);
                }
            } finally {
                if (requestId === logsRequestId) logsBusy = false;
            }
        }

        function formatLogTimestamp(isoStr) {
            if (!isoStr) return '';
            try {
                const d = new Date(isoStr);
                const pad = (n) => n < 10 ? '0' + n : n;
                return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
            } catch (e) {
                return isoStr;
            }
        }

        function filterByTag(key, val) {
            if (key === 'feed_id') {
                const feedSelect = document.getElementById('logs-feed-select');
                if (feedSelect) {
                    feedSelect.value = val;
                    currentLogsFeed = val;
                }
            } else {
                const searchInput = document.getElementById('logs-search-input');
                if (searchInput) {
                    searchInput.value = val;
                }
            }
            loadLogs(true);
        }

        function showLogsForFeed(feedId, level = 'all') {
            currentLogsFeed = feedId;
            const feedSelect = document.getElementById('logs-feed-select');
            if (feedSelect) feedSelect.value = feedId;
            setLogsLevel(level || 'all', false);
            const triggerEl = document.querySelector('button[data-bs-target="#tab-logs"]');
            if (currentTab === 'tab-logs') loadLogs(true);
            else if (triggerEl) bootstrap.Tab.getOrCreateInstance(triggerEl).show();
        }

        async function showFeedErrors(feedId) {
            const modalEl = document.getElementById('feedErrorsModal');
            if (!modalEl) return;

            const titleEl = document.getElementById('feed-errors-title');
            const subEl = document.getElementById('feed-errors-subtitle');
            const bodyEl = document.getElementById('feed-errors-body');
            const logsBtn = document.getElementById('btn-open-server-logs');
            const retryAllBtn = document.getElementById('btn-retry-all-errors');

            const feed = (allFeeds || []).find(f => f.id === feedId);
            const feedTitle = feed ? (feed.title || feed.id) : feedId;

            if (titleEl) {
                titleEl.innerHTML = `<i aria-hidden="true" class="bi bi-exclamation-triangle-fill me-1"></i> Error Log &mdash; ${escapeHtml(feedTitle)}`;
            }
            if (subEl) {
                subEl.innerHTML = `Podcast ID: <code>${escapeHtml(feedId)}</code>`;
            }
            if (retryAllBtn) {
                retryAllBtn.style.display = 'none';
                retryAllBtn.onclick = null;
            }
            if (logsBtn) {
                logsBtn.onclick = () => {
                    const inst = bootstrap.Modal.getInstance(modalEl);
                    if (inst) inst.hide();
                    showLogsForFeed(feedId, 'error');
                };
            }

            if (bodyEl) {
                bodyEl.innerHTML = `
                    <div class="text-center py-4 text-muted">
                        <div class="spinner-border spinner-border-sm text-danger me-2" role="status"></div>
                        Loading error details...
                    </div>`;
            }

            const modalInstance = bootstrap.Modal.getOrCreateInstance(modalEl);
            modalInstance.show();

            try {
                const [errRes, logRes] = await Promise.all([
                    fetch(`/api/v1/system/errors?feed_id=${encodeURIComponent(feedId)}&limit=50`),
                    fetch(`/api/v1/system/logs?feed_id=${encodeURIComponent(feedId)}&level=error&limit=50`)
                ]);

                let errorItems = [];
                if (errRes.ok) {
                    const data = await errRes.json();
                    errorItems = Array.isArray(data) ? data : (data?.errors || []);
                }

                let logEntries = [];
                if (logRes.ok) {
                    const logData = await logRes.json();
                    logEntries = logData.entries || [];
                }

                if (!bodyEl) return;

                const hasLastError = feed && feed.last_error;
                const hasEpisodeErrors = errorItems.length > 0;
                const hasLogs = logEntries.length > 0;

                if (!hasLastError && !hasEpisodeErrors && !hasLogs) {
                    bodyEl.innerHTML = `
                        <div class="text-center py-4 text-muted">
                            <i aria-hidden="true" class="bi bi-check-circle text-success fs-1 mb-2 d-block"></i>
                            <h6 class="fw-bold text-dark">No active errors</h6>
                            <p class="small mb-0">All episodes downloaded successfully and no error logs were recorded for this podcast.</p>
                        </div>`;
                    return;
                }

                let html = '';

                // 1. Last feed update error
                if (hasLastError) {
                    html += `
                        <div class="alert alert-danger mb-3 p-3">
                            <div class="d-flex align-items-center justify-content-between mb-1">
                                <strong class="d-flex align-items-center gap-1">
                                    <i aria-hidden="true" class="bi bi-exclamation-octagon-fill"></i> Last Feed Update Error
                                </strong>
                                <button class="btn btn-sm btn-outline-danger py-0 px-2" onclick="copyToClipboard('${escapeHtml(feed.last_error)}')">
                                    <i aria-hidden="true" class="bi bi-clipboard me-1"></i> Copy
                                </button>
                            </div>
                            <div class="small font-monospace text-break mb-0">${escapeHtml(feed.last_error)}</div>
                        </div>`;
                }

                // 2. Failed episodes
                if (hasEpisodeErrors) {
                    if (retryAllBtn) {
                        retryAllBtn.style.display = '';
                        retryAllBtn.onclick = () => retryAllErrorsForFeed(feedId, errorItems);
                    }

                    html += `
                        <div class="d-flex align-items-center justify-content-between mb-2">
                            <h6 class="fw-bold mb-0 text-danger d-flex align-items-center gap-1">
                                <i aria-hidden="true" class="bi bi-file-earmark-x-fill"></i> Failed Episodes (${errorItems.length})
                            </h6>
                            <span class="text-muted small">Click Retry to attempt re-download</span>
                        </div>
                        <div class="d-flex flex-column gap-2 mb-3">`;

                    errorItems.forEach(item => {
                        const errDate = item.error_at ? formatDate(item.error_at) : (item.pub_date ? formatDate(item.pub_date) : '');
                        html += `
                            <div class="card p-2.5 border-danger-subtle bg-body-tertiary">
                                <div class="d-flex align-items-start justify-content-between gap-2">
                                    <div class="flex-grow-1 min-w-0">
                                        <div class="fw-bold text-break small">${escapeHtml(item.episode_title || item.episode_id)}</div>
                                        <div class="text-muted small d-flex flex-wrap align-items-center gap-2 mt-1">
                                            <span>ID: <code>${escapeHtml(item.episode_id)}</code></span>
                                            ${errDate ? `<span>&bull; ${escapeHtml(errDate)}</span>` : ''}
                                            ${item.video_url ? `<span>&bull; <a href="${escapeHtml(item.video_url)}" target="_blank" rel="noopener noreferrer" class="text-decoration-none">Source <i aria-hidden="true" class="bi bi-box-arrow-up-right"></i></a></span>` : ''}
                                        </div>
                                    </div>
                                    <button class="btn btn-sm btn-outline-danger py-1 px-2 flex-shrink-0" ${actionAttrs('retry-error', feedId, item.episode_id)}>
                                        <i aria-hidden="true" class="bi bi-arrow-clockwise me-1"></i> Retry
                                    </button>
                                </div>
                                <pre class="bg-dark text-danger-subtle p-2 rounded small text-break mt-2 mb-1 user-select-all font-monospace" style="max-height: 140px; overflow-y: auto;"><code>${escapeHtml(item.error || 'Unknown download error')}</code></pre>
                                ${item.recommendation ? `
                                    <div class="text-muted small mt-1 d-flex align-items-start gap-1">
                                        <i aria-hidden="true" class="bi bi-lightbulb text-warning flex-shrink-0 mt-1"></i>
                                        <span>${escapeHtml(item.recommendation)}</span>
                                    </div>` : ''}
                            </div>`;
                    });

                    html += `</div>`;
                }

                // 3. Server error logs
                html += `
                    <div class="d-flex align-items-center justify-content-between mb-2 pt-2 border-top">
                        <h6 class="fw-bold mb-0 text-secondary d-flex align-items-center gap-1">
                            <i aria-hidden="true" class="bi bi-terminal-fill"></i> Server Error Logs (${logEntries.length})
                        </h6>
                    </div>`;

                if (hasLogs) {
                    html += `
                        <div class="bg-dark text-light p-2.5 rounded font-monospace small" style="max-height: 220px; overflow-y: auto;">`;
                    logEntries.forEach(e => {
                        const ts = e.timestamp ? e.timestamp.slice(11, 19) : '';
                        const lvl = (e.level || 'error').toUpperCase();
                        html += `
                            <div class="text-break mb-1 lh-sm">
                                <span class="text-secondary">[${escapeHtml(ts)}]</span>
                                <span class="text-danger fw-bold">[${escapeHtml(lvl)}]</span>
                                <span>${escapeHtml(e.message)}</span>
                            </div>`;
                    });
                    html += `</div>`;
                } else {
                    html += `
                        <div class="text-muted small fst-italic p-2 bg-body-tertiary rounded border">
                            No server log entries with ERROR level currently in buffer for this podcast.
                        </div>`;
                }

                bodyEl.innerHTML = html;

            } catch (err) {
                if (bodyEl) {
                    bodyEl.innerHTML = `
                        <div class="alert alert-danger mb-0">
                            <i aria-hidden="true" class="bi bi-exclamation-triangle me-1"></i>
                            Failed to load error log: ${escapeHtml(err.message)}
                        </div>`;
                }
            }
        }

        async function retryFeedError(feedId, episodeId) {
            try {
                const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}/episodes/${encodeURIComponent(episodeId)}/retry`, { method: 'POST' });
                if (res.ok) {
                    showToast("Status reset, episode will be retried on next update");
                    await showFeedErrors(feedId);
                    await loadFeeds();
                } else {
                    alert("Retry failed: " + await res.text());
                }
            } catch (e) {
                alert("Network error: " + e.message);
            }
        }

        async function retryAllErrorsForFeed(feedId, errorItems) {
            if (!errorItems || errorItems.length === 0) return;
            const retryBtn = document.getElementById('btn-retry-all-errors');
            if (retryBtn) {
                retryBtn.disabled = true;
                retryBtn.innerHTML = `<span class="spinner-border spinner-border-sm me-1"></span> Retrying...`;
            }

            try {
                let successCount = 0;
                for (const item of errorItems) {
                    try {
                        const res = await fetch(`/api/v1/feeds/${encodeURIComponent(feedId)}/episodes/${encodeURIComponent(item.episode_id)}/retry`, { method: 'POST' });
                        if (res.ok) successCount++;
                    } catch (_) {}
                }
                showToast(`Reset ${successCount} episode(s), will retry on next update`);
                await showFeedErrors(feedId);
                await loadFeeds();
            } finally {
                if (retryBtn) {
                    retryBtn.disabled = false;
                    retryBtn.innerHTML = `<i aria-hidden="true" class="bi bi-arrow-clockwise me-1"></i> Retry All`;
                }
            }
        }

        async function toggleServerDebug(enabled) {
            const level = enabled ? 'debug' : 'info';
            try {
                const res = await fetch('/api/v1/system/logs/level', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ level: level })
                });
                if (!res.ok) throw new Error(await res.text());
                showToast(`Server log level changed to ${level.toUpperCase()}`);
                loadLogs(true);
            } catch (err) {
                alert('Failed to change log level: ' + err.message);
                const debugSwitch = document.getElementById('logs-server-debug');
                if (debugSwitch) debugSwitch.checked = !enabled;
            }
        }

        async function clearLogsBuffer() {
            if (!confirm('Are you sure you want to clear the in-memory log buffer?')) return;
            try {
                const res = await fetch('/api/v1/system/logs', { method: 'DELETE' });
                if (!res.ok) throw new Error(await res.text());
                showToast('Log buffer cleared');
                loadLogs(true);
            } catch (err) {
                alert('Failed to clear logs: ' + err.message);
            }
        }

        function copyLogsToClipboard() {
            if (lastLoadedLogEntries.length === 0) {
                showToast('No logs to copy');
                return;
            }
            const lines = lastLoadedLogEntries.map(e => {
                let fieldsStr = '';
                if (e.fields && Object.keys(e.fields).length > 0) {
                    fieldsStr = ' (' + Object.entries(e.fields).map(([k, v]) => `${k}=${v}`).join(' ') + ')';
                }
                return `${e.timestamp} [${(e.level || 'info').toUpperCase()}]${fieldsStr} ${e.message}`;
            }).join('\n');

            navigator.clipboard.writeText(lines).then(() => {
                showToast(`Copied ${lastLoadedLogEntries.length} log lines to clipboard!`);
            }).catch(err => {
                alert('Could not copy: ' + err.message);
            });
        }

        // Audiobooks Tab Logic
        let cachedAudiobookDirs = [];

        async function loadAudiobooksTab() {
            try {
                const cfgRes = await fetch('/api/v1/audiobooks/config');
                if (cfgRes.ok) {
                    const cfgData = await cfgRes.json();
                    if (cfgData.audiobooks_dir) {
                        const disp = document.getElementById('audiobooks-dir-display');
                        if (disp) disp.textContent = cfgData.audiobooks_dir;
                    }
                }
            } catch (e) {
                console.warn('Failed to load audiobooks config', e);
            }
            await loadAudiobookDirs();
            renderAudiobookFeeds();
        }

        async function loadAudiobookDirs() {
            const tbody = document.getElementById('audiobook-dirs-tbody');
            if (tbody) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-muted py-4"><div class="spinner-border spinner-border-sm text-primary me-2"></div>Scanning folders...</td></tr>`;
            }
            try {
                const res = await fetch('/api/v1/audiobooks/dirs');
                if (!res.ok) throw new Error(await res.text());
                const data = await res.json();
                cachedAudiobookDirs = Array.isArray(data) ? data : [];
                renderAudiobookDirsTable(cachedAudiobookDirs);
            } catch (err) {
                if (tbody) {
                    tbody.innerHTML = `<tr><td colspan="6" class="text-center text-danger py-4"><i aria-hidden="true" class="bi bi-exclamation-triangle me-2"></i>Failed to read audiobooks: ${escapeHtml(err.message)}</td></tr>`;
                }
            }
        }

        function renderAudiobookDirsTable(dirs) {
            if (!Array.isArray(dirs)) dirs = [];
            const tbody = document.getElementById('audiobook-dirs-tbody');
            const countBadge = document.getElementById('audiobook-dirs-count');
            if (countBadge) countBadge.textContent = `${dirs.length} folder${dirs.length === 1 ? '' : 's'}`;
            if (!tbody) return;

            if (dirs.length === 0) {
                tbody.innerHTML = `<tr><td colspan="6" class="text-center text-muted py-4"><i aria-hidden="true" class="bi bi-inbox me-2"></i>No audiobook directories found in storage. Click "Upload audiobook" to add your first book!</td></tr>`;
                return;
            }

            tbody.innerHTML = dirs.map(d => {
                const sizeMB = (d.total_size / (1024 * 1024)).toFixed(1);
                const coverBadge = d.has_cover 
                    ? `<span class="badge bg-success-subtle text-success border border-success-subtle"><i aria-hidden="true" class="bi bi-image me-1"></i>Found</span>`
                    : `<span class="badge bg-secondary-subtle text-secondary border border-secondary-subtle">None (or ID3)</span>`;
                
                let statusBadge = '';
                let actionBtn = '';
                const escapedDir = escapeHtml(d.name);
                const escapedTitle = escapeHtml(d.suggested_title || d.name);
                const escapedAuthor = escapeHtml(d.suggested_author || '');
                if (d.linked) {
                    statusBadge = `<span class="badge bg-primary-subtle text-primary border border-primary-subtle"><i aria-hidden="true" class="bi bi-link-45deg me-1"></i>Linked: ${escapeHtml(d.feed_id)}</span>`;
                    actionBtn = `
                        <div class="d-inline-flex gap-1">
                            <button class="btn btn-sm btn-outline-secondary" ${actionAttrs("episodes", d.feed_id)}><i aria-hidden="true" class="bi bi-eye me-1"></i>Chapters</button>
                            <button class="btn btn-sm btn-outline-danger" ${actionAttrs("delete-book", d.name, d.feed_id)} title="Delete folder and linked feed from disk"><i aria-hidden="true" class="bi bi-trash"></i></button>
                        </div>`;
                } else {
                    statusBadge = `<span class="badge bg-warning-subtle text-warning border border-warning-subtle"><i aria-hidden="true" class="bi bi-clock me-1"></i>Unlinked</span>`;
                    actionBtn = `
                        <div class="d-inline-flex gap-1">
                            <button class="btn btn-sm btn-brand" ${actionAttrs("import-book", d.name, d.suggested_title || d.name, d.suggested_author || '')}><i aria-hidden="true" class="bi bi-plus-circle me-1"></i>Import</button>
                            <button class="btn btn-sm btn-outline-danger" ${actionAttrs("delete-book", d.name, '')} title="Delete folder from disk"><i aria-hidden="true" class="bi bi-trash"></i></button>
                        </div>`;
                }

                return `
                    <tr>
                        <td class="fw-semibold">
                            <i aria-hidden="true" class="bi bi-folder-fill text-warning me-2"></i>${escapeHtml(d.name)}
                            ${d.suggested_title && d.suggested_author ? `<div class="text-muted small fw-normal ms-4">${escapeHtml(d.suggested_author)} — ${escapeHtml(d.suggested_title)}</div>` : ''}
                        </td>
                        <td><span class="badge bg-body-secondary text-body">${d.file_count} chapters</span></td>
                        <td class="text-muted small">${sizeMB} MB</td>
                        <td>${coverBadge}</td>
                        <td>${statusBadge}</td>
                        <td class="text-end">${actionBtn}</td>
                    </tr>
                `;
            }).join('');
        }

        
        async function deleteAudiobookDir(dirName, linkedFeedId) {
            let msg = "Are you sure you want to delete audiobook folder \"" + dirName + "\" and all its audio files from disk?";
            if (linkedFeedId) {
                msg += "\n\nWarning: This folder is linked to active podcast feed \"" + linkedFeedId + "\". Deleting the folder will also remove the podcast feed.";
            }
            if (!confirm(msg)) return;

            try {
                const res = await fetch("/api/v1/audiobooks/dirs?name=" + encodeURIComponent(dirName), {
                    method: "DELETE"
                });
                if (!res.ok) {
                    const err = await res.json().catch(() => ({ error: res.statusText }));
                    throw new Error(err.error || "Failed to delete folder");
                }
                showToast("Folder \"" + dirName + "\" deleted from disk");
                await loadAudiobookDirs();
                await loadFeeds();
                renderAudiobookFeeds();
            } catch (err) {
                alert("Delete error: " + err.message);
            }
        }

        function renderAudiobookFeeds() {
            const container = document.getElementById('audiobook-feeds-container');
            if (!container) return;
            if (currentTab !== 'tab-audiobooks') {
                tabDirty['tab-audiobooks'] = true;
                return;
            }
            const audioFeeds = (allFeeds || []).filter(f => f.provider === 'audiobook');
            if (audioFeeds.length === 0) {
                container.innerHTML = `
                    <div class="library-empty">
                        <i aria-hidden="true" class="bi bi-book fs-1 text-muted"></i>
                        <h2 class="h5 mt-3">No audiobook feeds yet</h2>
                        <p class="text-muted">Import a discovered local folder above to generate an audiobook podcast feed.</p>
                    </div>
                `;
                return;
            }

            const existing = new Map(Array.from(container.querySelectorAll('[data-feed-id]')).map(el => [el.getAttribute('data-feed-id'), el]));
            if (!existing.size) {
                container.innerHTML = audioFeeds.map(feed => `<div data-feed-id="${escapeHtml(feed.id)}" data-render-key="${escapeHtml(feedRenderKey(feed))}">${renderFeedCardHTML(feed)}</div>`).join('');
                return;
            }
            const ids = new Set(audioFeeds.map(f => f.id));
            existing.forEach((el, id) => { if (!ids.has(id)) el.remove(); });
            let previous = null;
            for (const feed of audioFeeds) {
                let el = existing.get(feed.id);
                const key = feedRenderKey(feed);
                if (!el) {
                    el = document.createElement('div');
                    el.setAttribute('data-feed-id', feed.id);
                }
                if (el.getAttribute('data-render-key') !== key) {
                    el.innerHTML = renderFeedCardHTML(feed);
                    el.setAttribute('data-render-key', key);
                }
                const expected = previous ? previous.nextElementSibling : container.firstElementChild;
                if (expected !== el) container.insertBefore(el, expected);
                previous = el;
            }
        }

        function openImportModal(dirName, title, author) {
            document.getElementById('import-dir-name').value = dirName;
            document.getElementById('import-feed-title').value = title || '';
            document.getElementById('import-feed-author').value = author || '';
            const slug = toValidFeedSlug(title || dirName, false, '-').slice(0, 40);
            const impIdEl = document.getElementById('import-feed-id');
            if (impIdEl) {
                impIdEl.value = slug;
                impIdEl.dataset.touched = 'false';
                impIdEl.dispatchEvent(new Event('input'));
            }
            document.getElementById('import-feed-desc').value = '';
            const importPriv = document.getElementById('import-feed-private');
            if (importPriv) importPriv.checked = false;
            const modal = new bootstrap.Modal(document.getElementById('importAudiobookModal'));
            modal.show();
        }

        async function submitImportAudiobook() {
            const impIdEl = document.getElementById('import-feed-id');
            const impIdVal = impIdEl ? impIdEl.value.trim() : '';
            if (!impIdVal || !/^[a-zA-Z0-9_-]+$/.test(impIdVal)) {
                if (impIdEl) {
                    impIdEl.classList.add('is-invalid');
                    impIdEl.focus();
                }
                showToast('Please provide a valid Feed ID using only English letters, numbers, dashes, and underscores.', 'warning');
                return;
            }
            const btn = document.getElementById('btn-submit-import-audiobook');
            btn.disabled = true;
            btn.innerHTML = `<span class="spinner-border spinner-border-sm me-1"></span>Creating...`;

            const payload = {
                dir_name: document.getElementById('import-dir-name').value,
                id: document.getElementById('import-feed-id').value,
                title: document.getElementById('import-feed-title').value,
                author: document.getElementById('import-feed-author').value,
                description: document.getElementById('import-feed-desc').value,
                private_feed: document.getElementById('import-feed-private') ? document.getElementById('import-feed-private').checked : false
            };

            try {
                const res = await fetch('/api/v1/audiobooks/import', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                if (!res.ok) {
                    const err = await res.json().catch(() => ({ error: res.statusText }));
                    throw new Error(err.error || 'Failed to import audiobook');
                }
                const data = await res.json();
                closeModal('importAudiobookModal');
                showToast(`Book ${data.feed_id} imported; RSS generation queued.`);
                await loadFeeds();
                await loadAudiobookDirs();
                renderAudiobookFeeds();
            } catch (err) {
                alert('Import error: ' + err.message);
            } finally {
                btn.disabled = false;
                btn.innerHTML = `<i aria-hidden="true" class="bi bi-check-lg me-1"></i> Create Feed`;
            }
        }

        async function submitUploadAudiobook() {
            const bookDir = document.getElementById('upload-book-dir').value.trim();
            const audioFiles = document.getElementById('upload-audio-files').files;
            const coverFile = document.getElementById('upload-cover-file').files[0];
            const autoImport = document.getElementById('upload-auto-import').checked;

            if (!bookDir) {
                alert('Please enter a folder name');
                return;
            }
            if (!audioFiles || audioFiles.length === 0) {
                alert('Please select audio files to upload');
                return;
            }

            const files = [...audioFiles, ...(coverFile ? [coverFile] : [])];
            if (files.some(file => file.size > 1024 ** 3 - 1024 ** 2)) {
                alert('Each file must be smaller than 1 GiB. Use folder import for larger files.');
                return;
            }
            const progressContainer = document.getElementById('upload-progress-container');
            const progressBar = document.getElementById('upload-progress-bar');
            const progressPct = document.getElementById('upload-progress-pct');
            const progressStatus = document.getElementById('upload-progress-status');
            const submitBtn = document.getElementById('btn-submit-upload-audiobook');
            progressContainer.classList.remove('d-none');
            submitBtn.disabled = true;
            let completed = 0;
            try {
                for (const file of files) {
                    const formData = new FormData();
                    formData.append('book_dir', bookDir);
                    formData.append('auto_import', autoImport && completed === files.length - 1 ? '1' : '0');
                    formData.append('files', file);
                    progressStatus.textContent = `Uploading ${completed + 1} / ${files.length}: ${file.name}`;
                    await new Promise((resolve, reject) => {
                        const xhr = new XMLHttpRequest();
                        xhr.open('POST', '/api/v1/audiobooks/upload', true);
                        xhr.upload.onprogress = e => {
                            if (!e.lengthComputable) return;
                            const percent = Math.floor((completed + e.loaded / e.total) / files.length * 100);
                            progressBar.style.width = percent + '%';
                            progressPct.textContent = percent + '%';
                            if (e.loaded === e.total) progressStatus.textContent = 'Processing files / preparing book...';
                        };
                        xhr.onload = () => {
                            let result;
                            try { result = JSON.parse(xhr.responseText); }
                            catch { reject(new Error('Invalid response; retry the upload to check saved files.')); return; }
                            if (xhr.status >= 200 && xhr.status < 300) resolve(result);
                            else reject(new Error(result.error || 'Upload failed'));
                        };
                        xhr.onerror = () => reject(new Error('Network error. Retry with the same files; completed identical files will be skipped.'));
                        xhr.send(formData);
                    });
                    completed++;
                }
                closeModal('uploadAudiobookModal');
                showToast(autoImport ? 'Files uploaded; book queued for RSS generation.' : 'Files uploaded. Import the folder or update its existing feed.');
                document.getElementById('upload-audiobook-form').reset();
                progressContainer.classList.add('d-none');
            } catch (err) {
                progressStatus.textContent = err.message;
                alert(err.message);
            } finally {
                submitBtn.disabled = false;
                await loadFeeds();
                await loadAudiobookDirs();
                renderAudiobookFeeds();
            }
        }

        document.addEventListener('visibilitychange', () => {
            if (document.hidden) {
                if (updatePollTimer) { clearInterval(updatePollTimer); updatePollTimer = null; }
                if (logsAutoRefreshTimer) { clearInterval(logsAutoRefreshTimer); logsAutoRefreshTimer = null; }
            } else {
                if (updatingFeeds.size > 0) { pollFeedStatuses(); startUpdatePolling(); }
                else loadFeeds();
                if (currentTab === 'tab-logs') {
                    loadLogs();
                    setLogsAutoRefresh(document.getElementById('logs-autorefresh-select').value);
                }
            }
        });

        document.getElementById('mainTabs')?.addEventListener('shown.bs.tab', (e) => {
            const target = e.target.getAttribute('data-bs-target')?.replace('#', '');
            if (!target) return;
            currentTab = target;
            document.getElementById('episodesModal').classList.remove('show', 'active');
            closeNavigation();
            setRoute('#' + target.replace('tab-', ''));
            if (logsAutoRefreshTimer) { clearInterval(logsAutoRefreshTimer); logsAutoRefreshTimer = null; }
            if (target === 'tab-feeds') {
                tabDirty[target] = false;
                filterFeeds();
                window.scrollTo?.(0, libraryScroll);
            } else if (target === 'tab-personal') {
                loadPersonalFeeds();
            } else if (target === 'tab-audiobooks') {
                loadAudiobooksTab();
                window.scrollTo?.(0, libraryScroll);
            } else if (target === 'tab-tokens') {
                loadTokens();
            } else if (target === 'tab-downloader') {
                loadDownloaderConfig();
            } else if (target === 'tab-mcp') {
                loadMCPConfig();
            } else if (target === 'tab-logs') {
                loadLogs(true);
                setLogsAutoRefresh(document.getElementById('logs-autorefresh-select').value);
            } else if (target === 'tab-system') {
                loadSystem(); loadAuthSecurity();
            }
        });

        document.getElementById('securitySettingsModal')?.addEventListener('show.bs.modal', () => {
            loadAuthSecurity();
            const errAlert = document.getElementById('sec-settings-error');
            if (errAlert) errAlert.classList.add('d-none');
        });

        // Library navigation and view preferences are independent of server configuration.
        let feedStatusFilter = 'all';
        let libraryView = 'grid';
        let libraryScroll = 0;
        let libraryOrigin = 'tab-feeds';
        let detailSection = 'episodes';
        let restoringRoute = false;
        let feedHistoryRequestId = 0;

        function updateLibraryCounts() {
            const counts = {all: allFeeds.length, updating: allFeeds.filter(f => updatingFeeds.has(f.id)).length, errors: allFeeds.filter(f => f.error_count > 0 || f.last_error).length};
            for (const [name, count] of Object.entries(counts)) {
                const el = document.getElementById('count-' + name);
                if (el) el.textContent = count;
            }
        }

        function setFeedStatus(status) {
            feedStatusFilter = ['all', 'updating', 'errors'].includes(status) ? status : 'all';
            document.querySelectorAll('[data-status]').forEach(el => {
                const active = el.dataset.status === feedStatusFilter;
                el.classList[active ? 'add' : 'remove']('active');
                el.setAttribute('aria-pressed', String(active));
            });
            filterFeeds();
        }

        function setLibraryView(view) {
            libraryView = view === 'list' ? 'list' : 'grid';
            document.getElementById('feeds-container')?.setAttribute('data-view', libraryView);
            for (const name of ['grid', 'list']) {
                const button = document.getElementById('view-' + name);
                if (!button) continue;
                button.classList[name === libraryView ? 'add' : 'remove']('active');
                button.setAttribute('aria-pressed', String(name === libraryView));
            }
            saveLibraryPreferences();
        }

        function saveLibraryPreferences() {
            try {
                localStorage.setItem('podsync-library', JSON.stringify({
                    view: libraryView, status: feedStatusFilter,
                    search: document.getElementById('feed-search')?.value || '',
                    type: document.getElementById('feed-type-filter')?.value || 'all',
                    sort: document.getElementById('feed-sort')?.value || 'title'
                }));
            } catch (_) { /* Browsers may disable storage; the current view still works. */ }
        }

        function restoreLibraryPreferences() {
            try {
                const saved = JSON.parse(localStorage.getItem('podsync-library') || '{}');
                document.getElementById('feed-search').value = typeof saved.search === 'string' ? saved.search : '';
                document.getElementById('feed-type-filter').value = ['all', 'channels', 'personal', 'audiobooks'].includes(saved.type) ? saved.type : 'all';
                document.getElementById('feed-sort').value = ['title', 'updated', 'size'].includes(saved.sort) ? saved.sort : 'title';
                setLibraryView(saved.view);
                setFeedStatus(saved.status);
            } catch (_) { setLibraryView('grid'); }
        }

        function toggleNavigation() {
            const layout = document.querySelector('.app-layout');
            if (!layout) return;
            const open = !layout.classList.contains('navigation-open');
            layout.classList[open ? 'add' : 'remove']('navigation-open');
            document.querySelector('.mobile-menu-toggle')?.setAttribute('aria-expanded', String(open));
        }

        function closeNavigation() {
            document.querySelector('.app-layout')?.classList.remove('navigation-open');
            document.querySelector('.mobile-menu-toggle')?.setAttribute('aria-expanded', 'false');
        }

        document.addEventListener('click', (e) => {
            const layout = document.querySelector('.app-layout');
            if (layout?.classList.contains('navigation-open')) {
                const sidebar = document.querySelector('.app-sidebar');
                const toggle = document.querySelector('.mobile-menu-toggle');
                if (sidebar && !sidebar.contains(e.target) && toggle && !toggle.contains(e.target)) {
                    closeNavigation();
                }
            }
        });

        document.addEventListener('keydown', (e) => {
            if (e.key === 'Escape') closeNavigation();
        });

        function setRoute(hash) {
            if (!restoringRoute && window.location.hash !== hash) window.history?.pushState(null, '', hash);
        }

        function openFeedScreen(feedId) {
            if (currentTab !== 'feed-detail') {
                libraryScroll = window.scrollY || 0;
                libraryOrigin = currentTab === 'tab-audiobooks' ? currentTab : 'tab-feeds';
            }
            currentTab = 'feed-detail';
            if (logsAutoRefreshTimer) { clearInterval(logsAutoRefreshTimer); logsAutoRefreshTimer = null; }
            document.querySelectorAll('#main-content > .tab-pane').forEach(el => el.classList.remove('show', 'active'));
            document.querySelectorAll('#mainTabs .nav-link').forEach(el => {
                el.classList.remove('active'); el.setAttribute('aria-selected', 'false');
            });
            const screen = document.getElementById('episodesModal');
            screen.dataset.feedId = feedId;
            screen.classList.add('show', 'active');
            renderDetailHeader(allFeeds.find(f => f.id === feedId) || {id: feedId, title: feedId});
            closeNavigation();
            window.scrollTo?.(0, 0);
        }

        function renderDetailHeader(feed) {
            if (!feed) return;
            document.getElementById('episodes-feed-title').innerText = feed.title || feed.id;
            document.getElementById('detail-provider').innerText = feed.provider || 'Podcast';
            if (detailSection !== 'episodes') document.getElementById('episodes-feed-subtitle').innerText = [isValidAuthor(feed.author) ? feed.author : '', feed.downloaded_count != null ? `${feed.downloaded_count} episodes` : '', feed.total_size ? formatBytes(feed.total_size) : ''].filter(Boolean).join(' · ');
            const cover = document.getElementById('detail-cover');
            if (cover) cover.innerHTML = feed.cover_art && safeUrl(feed.cover_art)
                ? `<img src="${safeUrl(feed.cover_art)}" alt="" onerror="this.style.display='none'">`
                : `<i aria-hidden="true" class="bi ${feed.provider === 'audiobook' ? 'bi-book' : 'bi-broadcast'}"></i>`;
        }

        function showFeedSection(section, load = true) {
            detailSection = ['episodes', 'settings', 'history'].includes(section) ? section : 'episodes';
            const feedId = document.getElementById('episodesModal').dataset.feedId;
            for (const name of ['episodes', 'settings', 'history']) {
                document.getElementById('detail-' + name)?.classList[name === detailSection ? 'remove' : 'add']('d-none');
            }
            document.querySelectorAll('[data-section]').forEach(button => {
                const active = button.dataset.section === detailSection;
                button.classList[active ? 'add' : 'remove']('active');
                if (active) button.setAttribute('aria-current', 'page');
                else button.removeAttribute('aria-current');
            });
            setRoute('#feed/' + encodeURIComponent(feedId) + '/' + detailSection);
            if (!load) return;
            if (detailSection === 'settings') openFeedSettings(feedId, false);
            else if (detailSection === 'history') loadFeedHistory();
            else if (!episodeView || episodeView.feedId !== feedId) showEpisodes(feedId, false);
            else renderEpisodeList();
        }

        function returnToLibrary() {
            const button = document.querySelector(`#mainTabs [data-bs-target="#${libraryOrigin}"]`);
            if (button) bootstrap.Tab.getOrCreateInstance(button).show();
        }

        function restoreRoute() {
            const hash = window.location.hash || '#feeds';
            restoringRoute = true;
            try {
                if (hash.startsWith('#personal/')) {
                    const id = decodeURIComponent(hash.slice(10).split('/')[0] || '');
                    if (id) personalOpenFromLibrary(id);
                } else if (hash.startsWith('#feed/')) {
                    const [id, section] = hash.slice(6).split('/');
                    const feedId = decodeURIComponent(id || '');
                    if (!feedId) return;
                    if (section === 'settings') openFeedSettings(feedId);
                    else if (section === 'history') { openFeedScreen(feedId); showFeedSection('history'); }
                    else if (currentTab === 'feed-detail' && episodeView?.feedId === feedId) showFeedSection('episodes');
                    else showEpisodes(feedId);
                } else {
                    const allowed = ['feeds', 'personal', 'audiobooks', 'tokens', 'system', 'downloader', 'mcp', 'logs'];
                    const tab = allowed.includes(hash.slice(1)) ? hash.slice(1) : 'feeds';
                    const button = document.querySelector(`#mainTabs [data-bs-target="#tab-${tab}"]`);
                    if (button) bootstrap.Tab.getOrCreateInstance(button).show();
                }
            } catch (_) { returnToLibrary(); }
            finally { restoringRoute = false; }
        }
        window.addEventListener?.('hashchange', restoreRoute);

        async function loadFeedHistory() {
            const feedId = document.getElementById('episodesModal').dataset.feedId;
            const requestId = ++feedHistoryRequestId;
            const list = document.getElementById('feed-history-list');
            list.innerHTML = '<p class="text-muted">Loading activity…</p>';
            try {
                const res = await fetch(`/api/v1/system/logs?feed_id=${encodeURIComponent(feedId)}&limit=100`);
                if (!res.ok) throw new Error(await res.text());
                const data = await res.json();
                if (requestId !== feedHistoryRequestId || document.getElementById('episodesModal').dataset.feedId !== feedId) return;
                list.innerHTML = (data.entries || []).map(entry => `<div class="history-entry"><div class="small text-muted mb-1">${escapeHtml(formatDate(entry.timestamp))} · ${escapeHtml(entry.level)}</div><div>${escapeHtml(entry.message)}</div></div>`).join('') || '<p class="text-muted mb-0">No recent activity for this podcast.</p>';
            } catch (err) {
                if (requestId === feedHistoryRequestId && document.getElementById('episodesModal').dataset.feedId === feedId) list.textContent = 'Could not load activity: ' + err.message;
            }
        }

        function showSubscription(feedId) {
            const feed = allFeeds.find(f => f.id === feedId) || (typeof personalFeeds !== 'undefined' && personalFeeds.find(f => f.id === feedId));
            if (!feed) { showToast('This podcast is no longer in the library. Refresh the list.'); return; }
            document.getElementById('subscribe-title').innerText = 'Subscribe to ' + (feed.title || feed.id);
            const feedURL = feed.feed_url || '';
            const cleanURL = feedURL.replace(/^https?:\/\//, '');
            const appleURL = feed.apple_url || (cleanURL ? `podcast://${cleanURL}` : '');
            const pocketcastsURL = feed.pocketcasts_url || (cleanURL ? `pktc://subscribe/${cleanURL}` : '');
            const overcastURL = feed.overcast_url || (feedURL ? `overcast://x-callback-url/add?url=${encodeURIComponent(feedURL)}` : '');
            const links = [[appleURL, ['podcast:'], 'bi-apple', 'Apple Podcasts'], [pocketcastsURL, ['pktc:'], 'bi-play-circle', 'Pocket Casts'], [overcastURL, ['overcast:'], 'bi-soundwave', 'Overcast'], [feedURL, ['http:', 'https:'], 'bi-rss', 'Open RSS feed']];
            document.getElementById('subscribe-body').innerHTML = `<p class="text-muted small">Copy the feed link into your podcast app, or choose an app below.</p><div class="subscription-url"><input class="form-control" aria-label="RSS feed URL" readonly value="${escapeHtml(feed.feed_url || '')}"><button class="btn btn-brand" ${actionAttrs('copy', feed.feed_url)}>Copy</button><button class="btn btn-light" aria-label="Show QR code" ${actionAttrs('qr', feed.title || feed.id, feed.feed_url)}><i aria-hidden="true" class="bi bi-qr-code"></i></button></div><div class="subscription-links">${links.filter(([url, schemes]) => url && safeUrl(url, schemes)).map(([url, schemes, icon, label]) => `<a href="${safeUrl(url, schemes)}"><i aria-hidden="true" class="bi ${icon} me-2"></i>${label}<i aria-hidden="true" class="bi bi-arrow-up-right float-end"></i></a>`).join('')}</div>`;
            bootstrap.Modal.getOrCreateInstance(document.getElementById('subscribeModal')).show();
        }

        function toggleAddAdvanced(button) {
            const expanded = button.getAttribute('aria-expanded') !== 'true';
            button.setAttribute('aria-expanded', String(expanded));
            document.getElementById('addFeedTabs').classList[expanded ? 'remove' : 'add']('d-none');
            document.querySelectorAll('.add-advanced-field').forEach(el => el.classList[expanded ? 'remove' : 'add']('d-none'));
            if (!expanded) bootstrap.Tab.getOrCreateInstance(document.querySelector('[data-bs-target="#af-tab-general"]')).show();
        }

        // Personal feeds are a separate library, with feed-scoped membership actions.
        let personalFeeds = [];
        let personalCurrentID = '';
        let personalEditingID = '';
        let personalTransferAction = null;
        let personalDeleteAction = null;
        let personalRequestID = 0;
        let personalEpisodeSnapshot = '';

        async function personalAPI(path = '', options = {}) {
            const response = await fetch('/api/v1/personal-feeds' + path, options);
            if (response.status === 204) return null;
            const data = await response.json();
            if (!response.ok) throw new Error(data.error || 'Request failed');
            return data;
        }
        function personalJSON(method, body) { return {method, headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)}; }
        function personalError(error, id = 'personal-error') {
            const el = document.getElementById(id);
            if (!el) return;
            if (error) {
                el.textContent = error.message || String(error);
                el.classList.remove('d-none');
            } else {
                el.textContent = '';
                el.classList.add('d-none');
            }
        }
        function personalRenderCards() {
            const list = document.getElementById('personal-list');
            if (!list) return;
            if (!personalFeeds.length) {
                list.innerHTML = `<div class="library-empty text-center py-5 text-muted col-12" style="grid-column: 1 / -1;">
                    <i aria-hidden="true" class="bi bi-bookmark-heart display-4 text-muted mb-3 d-block"></i>
                    <h5 class="fw-semibold">No personal feeds yet</h5>
                    <p class="small text-muted mb-3">Click \"Create feed\" to add your first curated feed.</p>
                    <button class="btn btn-brand btn-sm" type="button" onclick="personalEdit()">
                        <i aria-hidden="true" class="bi bi-plus-lg me-1"></i>Create feed
                    </button>
                </div>`;
                return;
            }
            list.innerHTML = personalFeeds.map(f => {
                const coverArt = f.cover_art && safeUrl(f.cover_art)
                    ? `<img class="feed-cover" src="${safeUrl(f.cover_art)}" alt="" loading="lazy">`
                    : `<div class="feed-cover-placeholder d-flex align-items-center justify-content-center"><i aria-hidden="true" class="bi bi-bookmark-heart"></i></div>`;
                return `<article class="card feed-card h-100">
                    <div class="feed-card-top">
                        <div class="feed-art">${coverArt}</div>
                        <div class="flex-grow-1 min-w-0">
                            <a class="feed-card-title" href="javascript:void(0)" onclick="personalOpen('${escapeHtml(f.id)}')">${escapeHtml(f.title)}</a>
                            <div class="feed-card-author text-truncate">${escapeHtml(f.id)}.xml</div>
                        </div>
                    </div>
                    ${f.description ? `<p class="text-muted small mb-0 feed-card-description" style="display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;">${escapeHtml(f.description)}</p>` : ''}
                    <div class="feed-card-meta">
                        <span><i aria-hidden="true" class="bi bi-collection-play me-1"></i>${f.episode_count || 0} episodes</span>
                        <span><i aria-hidden="true" class="bi bi-stars me-1"></i>Universal</span>
                        <span class="feed-provider">Personal</span>
                    </div>
                    <div class="feed-card-bottom">
                        <button class="btn btn-light" type="button" onclick="personalOpen('${escapeHtml(f.id)}')">Open</button>
                        <button class="btn btn-light" type="button" onclick="showSubscription('${escapeHtml(f.id)}')"><i aria-hidden="true" class="bi bi-rss me-1"></i>Subscribe</button>
                        <button class="btn btn-light" type="button" onclick="personalEdit('${escapeHtml(f.id)}')"><i aria-hidden="true" class="bi bi-gear me-1"></i>Settings</button>
                    </div>
                </article>`;
            }).join('');
        }
        async function loadPersonalFeeds() {
            try {
                personalFeeds = await personalAPI();
                personalRenderCards();
                syncPersonalIntoAllFeeds();
                if (personalCurrentID && !personalFeeds.some(f => f.id === personalCurrentID)) personalBack();
                if (personalCurrentID) await personalOpen(personalCurrentID, false);
                personalError(null);
            } catch (error) { personalError(error); }
        }
        function personalBack() {
            personalCurrentID = ''; personalRequestID++;
            document.getElementById('personal-detail')?.classList.add('d-none');
            document.getElementById('personal-main-view')?.classList.remove('d-none');
        }
        async function personalOpen(id, reset = true) {
            personalCurrentID = id;
            const request = ++personalRequestID;
            try {
                const data = await personalAPI('/' + encodeURIComponent(id));
                if (request !== personalRequestID || personalCurrentID !== id) return;
                document.getElementById('personal-main-view')?.classList.add('d-none');
                document.getElementById('personal-detail')?.classList.remove('d-none');
                document.getElementById('personal-title').textContent = data.feed.title;
                document.getElementById('personal-description').textContent = data.feed.description || '';
                const cover = document.getElementById('personal-detail-cover');
                if (cover) {
                    cover.innerHTML = data.feed.cover_art && safeUrl(data.feed.cover_art)
                        ? `<img src="${safeUrl(data.feed.cover_art)}" alt="" onerror="this.style.display='none'">`
                        : `<i aria-hidden="true" class="bi bi-bookmark-heart"></i>`;
                }
                const rss = document.getElementById('personal-rss');
                if (rss) {
                    rss.href = safeUrl(data.feed.feed_url);
                }
                const countBadge = document.getElementById('personal-episodes-count');
                if (countBadge) countBadge.textContent = `${(data.episodes || []).length} episodes`;

                const snapshot = JSON.stringify(data.episodes);
                if (reset || snapshot !== personalEpisodeSnapshot) {
                    personalEpisodeSnapshot = snapshot;
                    const labels = {
                        new: '<span class="badge bg-primary">Queued</span>',
                        downloaded: '<span class="badge bg-success">Ready</span>',
                        error: '<span class="badge bg-danger">Error</span>',
                        cleaned: '<span class="badge bg-secondary">Cleaned</span>'
                    };
                    const container = document.getElementById('personal-episodes');
                    if (container) {
                        container.innerHTML = data.episodes.length ? data.episodes.map(ep => {
                            const url = safeUrl(ep.media_url || '');
                            const isVid = (ep.media_type && ep.media_type.startsWith('video/')) || ep.format === 'video';
                            const player = ep.status === 'downloaded' && url
                                ? (isVid
                                    ? `<video controls preload="none" class="w-100 rounded mt-2" style="max-height: 240px; background: var(--ctp-crust);" src="${url}"></video>`
                                    : `<audio controls preload="none" class="w-100 mt-2" src="${url}"></audio>`)
                                : '';
                            let actions = `<button type="button" class="btn btn-outline-secondary btn-sm" data-personal-action="rename" data-personal-id="${escapeHtml(ep.id)}" data-personal-title="${escapeHtml(ep.title || '')}"><i aria-hidden="true" class="bi bi-pencil me-1"></i>Rename</button>
<button type="button" class="btn btn-outline-secondary btn-sm" data-personal-action="move" data-personal-id="${escapeHtml(ep.id)}"><i aria-hidden="true" class="bi bi-arrows-move me-1"></i>Move</button>
<button type="button" class="btn btn-outline-secondary btn-sm" data-personal-action="copy" data-personal-id="${escapeHtml(ep.id)}"><i aria-hidden="true" class="bi bi-copy me-1"></i>Copy</button>`;
                            if (ep.status === 'error') {
                                actions += ` <button type="button" class="btn btn-outline-danger btn-sm" data-personal-action="retry" data-personal-id="${escapeHtml(ep.id)}"><i aria-hidden="true" class="bi bi-arrow-clockwise me-1"></i>Retry</button>`;
                            }
                            actions += ` <button type="button" class="btn btn-outline-danger btn-sm" data-personal-action="delete" data-personal-id="${escapeHtml(ep.id)}"><i aria-hidden="true" class="bi bi-trash3 me-1"></i>Delete</button>`;

                            const dateStr = ep.pub_date ? new Date(ep.pub_date).toLocaleString() : '';
                            const errorHtml = ep.error ? `<div class="mt-2 p-2 rounded episode-error small text-danger text-break">${escapeHtml(ep.error)}</div>` : '';

                            return `<div class="list-group-item p-3">
                                <div class="d-flex flex-column flex-sm-row align-items-start justify-content-between gap-3">
                                    <div class="min-w-0 flex-grow-1">
                                        <h6 class="fw-semibold mb-1 text-break">${escapeHtml(ep.title || ep.id)}</h6>
                                        <div class="text-muted small d-flex flex-wrap gap-3">
                                            ${dateStr ? `<span><i aria-hidden="true" class="bi bi-calendar me-1"></i>${escapeHtml(dateStr)}</span>` : ''}
                                        </div>
                                        ${errorHtml}
                                        ${player}
                                    </div>
                                    <div class="flex-shrink-0 text-sm-end">
                                        ${labels[ep.status] || `<span class="badge bg-secondary">${escapeHtml(ep.status)}</span>`}
                                        <div class="d-flex flex-wrap gap-1 mt-2 justify-content-sm-end">${actions}</div>
                                    </div>
                                </div>
                            </div>`;
                        }).join('') : `<div class="p-4 text-center text-muted">
                            <i aria-hidden="true" class="bi bi-inbox display-6 text-muted mb-2 d-block"></i>
                            <div>Add a link or upload a file above to add episodes to this feed.</div>
                        </div>`;
                    }
                }
                personalError(null);
            } catch (error) { if (request === personalRequestID) personalError(error); }
        }
        function personalEdit(id = '') {
            personalEditingID = id;
            const f = personalFeeds.find(f => f.id === id) || {};
            document.getElementById('personal-edit-heading').textContent = id ? 'Feed Settings' : 'Create Feed';
            document.getElementById('personal-edit-title').value = f.title || '';
            document.getElementById('personal-edit-description').value = f.description || '';
            document.getElementById('personal-edit-cover').value = f.cover_art || '';
            document.getElementById('personal-edit-cover-file').value = '';
            document.getElementById('personal-edit-sort').value = f.playlist_sort || 'desc';
            document.getElementById('personal-edit-private').checked = !!f.private_feed;
            personalError(null, 'personal-edit-error');
            bootstrap.Modal.getOrCreateInstance(document.getElementById('personalEditModal')).show();
        }
        async function personalSave(event) {
            event.preventDefault(); const button = event.submitter; if (button) button.disabled = true;
            try {
                let cover = document.getElementById('personal-edit-cover').value;
                const file = document.getElementById('personal-edit-cover-file').files[0];
                if (file) {
                    const form = new FormData(); form.append('file',file);
                    const response = await fetch('/api/v1/covers/upload',{method:'POST',body:form});
                    const data = await response.json(); if (!response.ok) throw new Error(data.error || 'Failed to upload cover art'); cover = data.url;
                }
                const payload = {title:document.getElementById('personal-edit-title').value,description:document.getElementById('personal-edit-description').value,cover_art:cover,playlist_sort:document.getElementById('personal-edit-sort').value,private_feed:document.getElementById('personal-edit-private').checked};
                const f = await personalAPI(personalEditingID ? '/' + encodeURIComponent(personalEditingID) : '',personalJSON(personalEditingID ? 'PATCH' : 'POST',payload));
                closeModal('personalEditModal'); await loadPersonalFeeds(); await personalOpen(f.id);
            } catch (error) { personalError(error, 'personal-edit-error'); }
            finally { if (button) button.disabled = false; }
        }
        function togglePersonalUrlQuality() {
            const fmt = document.getElementById('personal-url-format')?.value;
            const vCol = document.getElementById('personal-url-video-quality-col');
            const aCol = document.getElementById('personal-url-audio-quality-col');
            if (vCol) vCol.style.display = (fmt === 'video') ? 'block' : 'none';
            if (aCol) aCol.style.display = (fmt === 'audio' || fmt === 'auto') ? 'block' : 'none';
        }
        async function personalAddURL(event) {
            event.preventDefault(); const id = personalCurrentID; const button = event.submitter; if (button) button.disabled = true;
            try {
                const body = {
                    url: document.getElementById('personal-url').value,
                    title: document.getElementById('personal-url-title')?.value || ''
                };
                const formatVal = document.getElementById('personal-url-format')?.value;
                if (formatVal) body.format = formatVal;
                const qualityVal = document.getElementById('personal-url-quality')?.value;
                if (qualityVal) body.quality = qualityVal;
                const maxHeightVal = document.getElementById('personal-url-max-height')?.value;
                if (maxHeightVal !== undefined && maxHeightVal !== '') {
                    body.max_height = parseInt(maxHeightVal, 10) || 0;
                }
                await personalAPI('/'+encodeURIComponent(id)+'/episodes',personalJSON('POST',body));
                event.target.reset();
                togglePersonalUrlQuality();
                if (personalCurrentID === id) await personalOpen(id);
                await loadPersonalFeeds();
            } catch (error) { personalError(error); } finally { if (button) button.disabled = false; }
        }
        function togglePersonalUploadQuality() {
            const fmt = document.getElementById('personal-upload-format')?.value;
            const vCol = document.getElementById('personal-upload-video-quality-col');
            const aCol = document.getElementById('personal-upload-audio-quality-col');
            if (vCol) vCol.style.display = (fmt === 'video') ? 'block' : 'none';
            if (aCol) aCol.style.display = (fmt === 'audio' || fmt === 'auto') ? 'block' : 'none';
        }
        function autoDetectUploadFormat(input) {
            if (!input || !input.files || !input.files[0]) return;
            const name = input.files[0].name.toLowerCase();
            const fmtSelect = document.getElementById('personal-upload-format');
            if (!fmtSelect) return;
            if (name.endsWith('.mp4') || name.endsWith('.mkv') || name.endsWith('.mov') || name.endsWith('.webm') || name.endsWith('.m4v')) {
                fmtSelect.value = 'video';
            } else {
                fmtSelect.value = 'audio';
            }
            togglePersonalUploadQuality();
        }
        async function personalUpload(event) {
            event.preventDefault(); const id = personalCurrentID; const button = event.submitter; if (button) button.disabled = true;
            const prog = document.getElementById('personal-progress');
            if (prog) {
                prog.textContent = 'Uploading file…';
                prog.className = 'alert alert-info py-2 px-3 small mb-3';
            }
            try {
                const data = new FormData();
                const fileInput = document.getElementById('personal-file');
                if (fileInput && fileInput.files[0]) data.append('file', fileInput.files[0]);
                const titleVal = document.getElementById('personal-upload-title')?.value || document.getElementById('personal-file-title')?.value;
                if (titleVal) data.append('title', titleVal);
                const formatVal = document.getElementById('personal-upload-format')?.value || document.getElementById('personal-file-format')?.value;
                if (formatVal) data.append('format', formatVal);
                const qualityVal = document.getElementById('personal-upload-quality')?.value;
                if (qualityVal) data.append('quality', qualityVal);
                const maxHeightVal = document.getElementById('personal-upload-max-height')?.value;
                if (maxHeightVal !== undefined && maxHeightVal !== '') {
                    data.append('max_height', maxHeightVal);
                }
                await personalAPI('/'+encodeURIComponent(id)+'/upload',{method:'POST',body:data});
                event.target.reset();
                togglePersonalUploadQuality();
                if (prog) {
                    prog.textContent = 'File uploaded. Processing episode…';
                    prog.className = 'alert alert-success py-2 px-3 small mb-3';
                    setTimeout(() => { if (prog) prog.className = 'alert alert-info py-2 px-3 small d-none mb-3'; }, 4000);
                }
                if (personalCurrentID === id) await personalOpen(id);
                await loadPersonalFeeds();
            } catch (error) {
                personalError(error);
                if (prog) prog.className = 'alert alert-info py-2 px-3 small d-none mb-3';
            }
            finally { if (button) button.disabled = false; }
        }
        function personalDeleteFeed() { personalConfirmDelete(''); }
        function personalConfirmDelete(episodeID) {
            personalDeleteAction = {feedID:personalCurrentID,episodeID};
            document.getElementById('personal-delete-text').textContent = episodeID ? 'Remove this episode from the feed?' : 'Delete feed and all its episodes? Its RSS subscription will stop working.';
            document.getElementById('personal-delete-files').checked = true;
            personalError(null, 'personal-delete-error');
            bootstrap.Modal.getOrCreateInstance(document.getElementById('personalDeleteModal')).show();
        }
        async function personalDeleteSave(event) {
            event.preventDefault(); const action = personalDeleteAction; if (!action) return;
            const button = event.submitter; if (button) button.disabled = true;
            try {
                await personalAPI('/'+encodeURIComponent(action.feedID)+(action.episodeID ? '/episodes/'+encodeURIComponent(action.episodeID) : '')+'?confirm=true&delete_files='+document.getElementById('personal-delete-files').checked,{method:'DELETE'});
                closeModal('personalDeleteModal'); if (!action.episodeID) personalBack(); await loadPersonalFeeds();
            } catch (error) { personalError(error, 'personal-delete-error'); } finally { if (button) button.disabled = false; }
        }
        function personalTransfer(episodeID, mode) {
            const targets = personalFeeds.filter(f => f.id !== personalCurrentID);
            if (!targets.length) { personalError(new Error('Create another personal feed first.')); return; }
            personalTransferAction = {feedID:personalCurrentID,episodeID,mode};
            document.getElementById('personal-transfer-heading').textContent = mode === 'move' ? 'Move Episode' : 'Copy Episode';
            document.getElementById('personal-target').innerHTML = targets.map(f=>`<option value="${escapeHtml(f.id)}">${escapeHtml(f.title)}</option>`).join('');
            personalError(null,'personal-transfer-error'); bootstrap.Modal.getOrCreateInstance(document.getElementById('personalTransferModal')).show();
        }
        async function personalTransferSave(event) {
            event.preventDefault(); const action = personalTransferAction; if (!action) return;
            const button = event.submitter; if (button) button.disabled = true;
            try {
                await personalAPI('/'+encodeURIComponent(action.feedID)+'/episodes/'+encodeURIComponent(action.episodeID)+'/'+action.mode,personalJSON('POST',{target_feed_id:document.getElementById('personal-target').value}));
                closeModal('personalTransferModal'); await loadPersonalFeeds();
            } catch (error) { personalError(error,'personal-transfer-error'); } finally { if (button) button.disabled = false; }
        }
        let personalRenameAction = null;
        function personalRename(episodeID, currentTitle) {
            personalRenameAction = { feedID: personalCurrentID, episodeID };
            const input = document.getElementById('personal-rename-title');
            if (input) input.value = currentTitle || '';
            personalError(null, 'personal-rename-error');
            bootstrap.Modal.getOrCreateInstance(document.getElementById('personalRenameModal')).show();
        }
        async function personalRenameSave(event) {
            event.preventDefault();
            const action = personalRenameAction;
            if (!action) return;
            const titleInput = document.getElementById('personal-rename-title');
            const newTitle = titleInput ? titleInput.value.trim() : '';
            if (!newTitle) return;
            const button = event.submitter;
            if (button) button.disabled = true;
            try {
                await personalAPI('/'+encodeURIComponent(action.feedID)+'/episodes/'+encodeURIComponent(action.episodeID), personalJSON('PATCH', { title: newTitle }));
                closeModal('personalRenameModal');
                await loadPersonalFeeds();
            } catch (error) {
                personalError(error, 'personal-rename-error');
            } finally {
                if (button) button.disabled = false;
            }
        }
        document.getElementById('tab-personal')?.addEventListener('click', async event => {
            const button = event.target.closest('[data-personal-action]'); if (!button) return;
            const {personalAction:action, personalId:id} = button.dataset;
            if (action === 'open') return personalOpen(id);
            if (action === 'rename') return personalRename(id, button.dataset.personalTitle || '');
            if (action === 'copy' || action === 'move') return personalTransfer(id,action);
            if (action === 'delete') return personalConfirmDelete(id);
            if (action === 'retry') {
                button.disabled = true;
                try { await personalAPI('/'+encodeURIComponent(personalCurrentID)+'/episodes/'+encodeURIComponent(id)+'/retry',{method:'POST'}); await personalOpen(personalCurrentID); }
                catch (error) { personalError(error); } finally { button.disabled = false; }
            }
        });
        setInterval(() => {
            if (currentTab === 'tab-personal' && !document.hidden) {
                // Preserve a playing media element while polling download status.
                const playing = Array.from(document.querySelectorAll('#personal-episodes audio, #personal-episodes video')).some(el => !el.paused);
                if (!playing) loadPersonalFeeds();
            }
        }, 5000);
