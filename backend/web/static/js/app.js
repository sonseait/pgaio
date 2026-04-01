// PGAIO — Core Application Module
// SPA Router, WebSocket, API utilities

const API_BASE = '/api';
let ws = null;
let wsReconnectTimer = null;

// ========================
// API Helper
// ========================
async function api(path, options = {}) {
    try {
        const res = await fetch(API_BASE + path, {
            headers: { 'Content-Type': 'application/json' },
            ...options,
        });
        if (!res.ok) {
            const err = await res.json().catch(() => ({ error: res.statusText }));
            const msg = err.error || res.statusText;
            showToast(msg, 'error');
            throw new Error(msg);
        }
        return await res.json();
    } catch (e) {
        if (e.message !== 'Failed to fetch') console.error('API:', e);
        throw e;
    }
}

// Session-protected API call — uses stored session, prompts login if needed
function apiProtected(path, options = {}) {
    return new Promise((resolve, reject) => {
        const tryRequest = (sid) => {
            const headers = {
                'Content-Type': 'application/json',
                'X-Session-ID': sid,
                ...(options.headers || {}),
            };
            fetch(API_BASE + path, { ...options, headers })
                .then(async res => {
                    if (res.status === 401) {
                        // Session expired or missing — prompt login
                        sessionStorage.removeItem('pgaio_session');
                        showLoginModal((newSid) => tryRequest(newSid), reject);
                        return;
                    }
                    if (!res.ok) {
                        const err = await res.json().catch(() => ({ error: res.statusText }));
                        const msg = err.error || res.statusText;
                        showToast(msg, 'error');
                        throw new Error(msg);
                    }
                    return res.json();
                })
                .then(data => { if (data) resolve(data); })
                .catch(reject);
        };

        const sessionId = sessionStorage.getItem('pgaio_session');
        if (sessionId) {
            tryRequest(sessionId);
        } else {
            showLoginModal((sid) => tryRequest(sid), reject);
        }
    });
}

// ========================
// Login Modal (session-based)
// ========================
function showLoginModal(onSuccess, onCancel) {
    const old = document.getElementById('login-modal');
    if (old) old.remove();

    const modal = document.createElement('div');
    modal.id = 'login-modal';
    modal.className = 'totp-overlay';
    modal.innerHTML = `
        <div class="totp-dialog">
            <div class="totp-title">authentication required</div>
            <div class="totp-desc">enter 6-digit code from your authenticator app</div>
            <input type="text" id="login-otp-input" class="totp-input" maxlength="6"
                   pattern="[0-9]*" inputmode="numeric" autocomplete="one-time-code"
                   placeholder="000000" autofocus>
            <div id="login-error" class="mono-xs red" style="margin-bottom:8px;min-height:14px"></div>
            <div class="totp-actions">
                <button class="btn btn-sm" id="login-cancel">cancel</button>
                <button class="btn btn-sm btn-primary" id="login-confirm">login</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);

    const input = document.getElementById('login-otp-input');
    input.focus();

    const cleanup = () => modal.remove();
    const errEl = document.getElementById('login-error');

    const submit = async () => {
        const code = input.value.trim();
        if (code.length !== 6 || !/^\d+$/.test(code)) {
            input.style.borderColor = 'var(--red)';
            errEl.textContent = 'enter 6 digits';
            input.focus();
            return;
        }
        try {
            const res = await fetch(API_BASE + '/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ code }),
            });
            const data = await res.json();
            if (!res.ok) {
                errEl.textContent = data.error || 'invalid code';
                input.value = '';
                input.style.borderColor = 'var(--red)';
                input.focus();
                return;
            }
            const sid = data.data.sessionId;
            sessionStorage.setItem('pgaio_session', sid);
            IdleTracker.reset();
            cleanup();
            onSuccess(sid);
        } catch (e) {
            errEl.textContent = 'connection error';
        }
    };

    document.getElementById('login-confirm').addEventListener('click', submit);
    document.getElementById('login-cancel').addEventListener('click', () => {
        cleanup();
        if (onCancel) onCancel(new Error('cancelled'));
    });
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') submit();
        if (e.key === 'Escape') { cleanup(); if (onCancel) onCancel(new Error('cancelled')); }
    });
    input.addEventListener('input', () => {
        if (input.value.trim().length === 6) submit();
    });
}

// ========================
// Idle Tracker (15 min session timeout)
// ========================
const IdleTracker = {
    _timer: null,
    _timeout: 15 * 60 * 1000, // 15 minutes

    start() {
        ['mousemove', 'keydown', 'click', 'scroll', 'touchstart'].forEach(evt =>
            document.addEventListener(evt, () => this.reset(), { passive: true })
        );
        this.reset();
    },

    reset() {
        if (this._timer) clearTimeout(this._timer);
        this._timer = setTimeout(() => this.expire(), this._timeout);
    },

    expire() {
        const sid = sessionStorage.getItem('pgaio_session');
        if (sid) {
            sessionStorage.removeItem('pgaio_session');
            showToast('session expired — please login again', 'info');
        }
    },
};

// ========================
// Toast
// ========================
function showToast(msg, type = 'info') {
    const c = document.getElementById('toast-container');
    if (!c) return;
    const t = document.createElement('div');
    t.className = `toast toast-${type}`;
    t.textContent = msg;
    c.appendChild(t);
    setTimeout(() => {
        t.style.opacity = '0';
        setTimeout(() => t.remove(), 300);
    }, 3500);
}

// Confirm modal — returns Promise<boolean>
function showConfirm(title, message, { danger = false, confirmText = 'confirm', cancelText = 'cancel' } = {}) {
    return new Promise((resolve) => {
        document.getElementById('confirm-modal')?.remove();
        const overlay = document.createElement('div');
        overlay.id = 'confirm-modal';
        overlay.className = 'modal-overlay';
        const msgHtml = message.replace(/\n/g, '<br>');
        overlay.innerHTML = `
            <div class="modal-dialog" style="width:380px">
                <div class="modal-header">
                    <span class="modal-title">${title}</span>
                    <button class="modal-close" id="confirm-close">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="mono-xs" style="line-height:1.6">${msgHtml}</div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-sm" id="confirm-cancel">${cancelText}</button>
                    <button class="btn btn-sm ${danger ? 'btn-danger' : 'btn-primary'}" id="confirm-ok">${confirmText}</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);

        const cleanup = (result) => { overlay.remove(); resolve(result); };
        document.getElementById('confirm-ok').addEventListener('click', () => cleanup(true));
        document.getElementById('confirm-cancel').addEventListener('click', () => cleanup(false));
        document.getElementById('confirm-close').addEventListener('click', () => cleanup(false));
        overlay.addEventListener('click', (e) => { if (e.target === overlay) cleanup(false); });
        document.addEventListener('keydown', function handler(e) {
            if (e.key === 'Escape') { document.removeEventListener('keydown', handler); cleanup(false); }
            if (e.key === 'Enter') { document.removeEventListener('keydown', handler); cleanup(true); }
        });
        document.getElementById('confirm-ok').focus();
    });
}

// ========================
// Formatting
// ========================
function formatBytes(b) {
    if (!b) return '0 B';
    const k = 1024, s = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(b) / Math.log(k));
    return parseFloat((b / Math.pow(k, i)).toFixed(1)) + ' ' + s[i];
}

function timeAgo(d) {
    const s = Math.floor((Date.now() - new Date(d)) / 1000);
    if (s < 60) return s + 's ago';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm ago';
    const h = Math.floor(m / 60);
    if (h < 24) return h + 'h ago';
    return Math.floor(h / 24) + 'd ago';
}

function formatDuration(ms) {
    if (!ms || ms < 0) return '-';
    const s = Math.floor(ms / 1000);
    if (s < 60) return s + 's';
    const m = Math.floor(s / 60);
    if (m < 60) return m + 'm ' + (s % 60) + 's';
    const h = Math.floor(m / 60);
    return h + 'h ' + (m % 60) + 'm';
}

function fmtNum(n) {
    if (n == null) return '0';
    if (n >= 1e9) return (n / 1e9).toFixed(1) + 'B';
    if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
    if (n >= 1e3) return (n / 1e3).toFixed(1) + 'K';
    return String(n);
}

function escHtml(s) {
    if (!s) return '';
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

function jobStatusClass(status) {
    switch (status) {
        case 'succeeded': return 'green';
        case 'failed': return 'red';
        case 'canceled': return 'yellow';
        default: return 'accent';
    }
}

function renderJobSummary(job, { showDownload = false } = {}) {
    if (!job) return '<span class="mono-xs dim">no recent job</span>';
    const statusCls = jobStatusClass(job.status);
    const finished = job.finishedAt ? ` · finished ${timeAgo(job.finishedAt)}` : '';
    const detail = job.details ? `<div class="mono-xs dim" style="margin-top:4px;white-space:pre-wrap">${escHtml(job.details)}</div>` : '';
    const artifact = showDownload && job.status === 'succeeded' && job.artifact
        ? `<button class="btn btn-sm" onclick="JobUI.download('${job.id}')" style="font-size:9px;margin-left:8px">download</button>`
        : '';
    return `
        <div class="card" style="padding:8px 12px">
            <div class="flex-between" style="gap:12px">
                <div>
                    <span class="mono-xs ${statusCls}">${escHtml(job.type)} ${escHtml(job.status)}</span>
                    <span class="mono-xs dim">${escHtml(job.message || job.target || '')}${finished}</span>
                </div>
                ${artifact}
            </div>
            ${detail}
        </div>
    `;
}

const JobUI = {
    async get(jobId) {
        const res = await apiProtected(`/jobs/${encodeURIComponent(jobId)}`);
        return res.data;
    },

    async download(jobId) {
        const doDownload = async (sid) => {
            const res = await fetch(`${API_BASE}/jobs/${encodeURIComponent(jobId)}/download`, {
                headers: { 'X-Session-ID': sid },
            });
            if (res.status === 401) {
                sessionStorage.removeItem('pgaio_session');
                showLoginModal((newSid) => doDownload(newSid));
                return;
            }
            if (!res.ok) {
                const err = await res.json().catch(() => ({ error: res.statusText }));
                throw new Error(err.error || res.statusText);
            }

            const blob = await res.blob();
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = res.headers.get('Content-Disposition')?.match(/filename="(.+)"/)?.[1] || 'artifact.bin';
            a.click();
            URL.revokeObjectURL(url);
        };

        try {
            const sid = sessionStorage.getItem('pgaio_session');
            if (sid) await doDownload(sid);
            else showLoginModal((newSid) => doDownload(newSid));
        } catch (e) {
            showToast(`download failed: ${e.message}`, 'error');
        }
    },
};

const ProfileSelector = {
    _settings: null,

    async _loadSettings() {
        if (this._settings) return this._settings;
        const res = await api('/settings');
        this._settings = res.data || {};
        return this._settings;
    },

    async getProfiles() {
        const settings = await this._loadSettings();
        const profiles = settings.connections?.profiles || [];
        return profiles.filter(p => p.enabled);
    },

    async getDefault(feature) {
        const settings = await this._loadSettings();
        return settings.connections?.featureRoutes?.[feature] || 'direct-postgres';
    },

    getSelected(feature) {
        return sessionStorage.getItem(`pgaio_profile_${feature}`) || '';
    },

    setSelected(feature, profile) {
        sessionStorage.setItem(`pgaio_profile_${feature}`, profile);
    },

    async ensureSelected(feature) {
        const selected = this.getSelected(feature);
        if (selected) return selected;
        const fallback = await this.getDefault(feature);
        this.setSelected(feature, fallback);
        return fallback;
    },

    async renderInto(container, feature, onChange) {
        if (!container) return;
        const profiles = await this.getProfiles();
        const selected = await this.ensureSelected(feature);
        container.innerHTML = `
            <select class="db-select" title="connection profile">
                ${profiles.map(p => `<option value="${escHtml(p.name)}" ${p.name === selected ? 'selected' : ''}>${escHtml(p.label || p.name)}</option>`).join('')}
            </select>
        `;
        const select = container.querySelector('select');
        if (!select) return;
        select.addEventListener('change', (e) => {
            this.setSelected(feature, e.target.value);
            if (onChange) onChange(e.target.value);
        });
    },

    getParam(feature) {
        const selected = this.getSelected(feature);
        return selected ? `&profile=${encodeURIComponent(selected)}` : '';
    },

    resetCache() {
        this._settings = null;
    },
};
// ========================
// Database Selector
// ========================
const DbSelector = {
    _dbs: null,

    async load() {
        if (this._dbs) return this._dbs;
        try {
            const res = await api('/database/list');
            this._dbs = res.data || [];
        } catch { this._dbs = []; }
        return this._dbs;
    },

    getSelected() { return sessionStorage.getItem('pgaio_selected_db') || ''; },
    setSelected(db) { sessionStorage.setItem('pgaio_selected_db', db); },

    /** Returns "?database=name" or "" if default */
    getParam() {
        const db = this.getSelected();
        return db ? `?database=${encodeURIComponent(db)}` : '';
    },

    /** Render a select dropdown into container. onChange is called with db name. */
    async renderInto(container, onChange) {
        const dbs = await this.load();
        const sel = this.getSelected();
        container.innerHTML = `
            <select id="db-selector" class="db-select" title="select database">
                <option value="">default database</option>
                ${dbs.map(d => `<option value="${escHtml(d)}" ${d === sel ? 'selected' : ''}>${escHtml(d)}</option>`).join('')}
            </select>
        `;
        container.querySelector('#db-selector').addEventListener('change', (e) => {
            this.setSelected(e.target.value);
            if (onChange) onChange(e.target.value);
        });
    },
};

// ========================
// WebSocket
// ========================
function connectWebSocket() {
    if (ws && ws.readyState === WebSocket.OPEN) return;
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    try {
        ws = new WebSocket(`${proto}://${location.host}/api/dashboard/ws?interval=1`);
    } catch (e) { scheduleReconnect(); return; }

    ws.onopen = () => {
        updateWsStatus(true);
        if (wsReconnectTimer) { clearTimeout(wsReconnectTimer); wsReconnectTimer = null; }
    };
    ws.onmessage = (e) => {
        try {
            const data = JSON.parse(e.data);
            if (typeof Dashboard !== 'undefined' && Dashboard.onData) Dashboard.onData(data);
            const el = document.getElementById('last-update');
            if (el) el.textContent = new Date().toLocaleTimeString();
        } catch (err) { console.error('WS parse:', err); }
    };
    ws.onclose = () => { updateWsStatus(false); scheduleReconnect(); };
    ws.onerror = () => { ws.close(); };
}

function scheduleReconnect() {
    if (wsReconnectTimer) return;
    wsReconnectTimer = setTimeout(() => { wsReconnectTimer = null; connectWebSocket(); }, 3000);
}

function disconnectWebSocket() {
    if (wsReconnectTimer) { clearTimeout(wsReconnectTimer); wsReconnectTimer = null; }
    if (ws) { ws.onclose = null; ws.close(); ws = null; }
    updateWsStatus(false);
}

function updateWsStatus(on) {
    const dot = document.getElementById('ws-indicator');
    const txt = document.getElementById('ws-status');
    if (dot) dot.className = `ws-dot ${on ? 'online' : 'offline'}`;
    if (txt) txt.textContent = on ? 'connected' : 'disconnected';
}

// ========================
// Router
// ========================
const pages = {
    dashboard: {
        title: 'dashboard', sub: 'real-time monitoring',
        render: (el) => { if (typeof Dashboard !== 'undefined') Dashboard.render(el); },
        ws: true,
    },
    backups: {
        title: 'backups', sub: 'wal-g backup & restore',
        render: (el) => { if (typeof Backup !== 'undefined') Backup.render(el); },
    },
    jobs: {
        title: 'job center', sub: 'background operations & artifacts',
        render: (el) => { if (typeof JobsPage !== 'undefined') JobsPage.render(el); },
    },
    planner: {
        title: 'maintenance planner', sub: 'actionable table and index recommendations',
        render: (el) => { if (typeof MaintenancePlanner !== 'undefined') MaintenancePlanner.render(el); },
    },
    profiles: {
        title: 'connection profiles', sub: 'feature routing between direct postgres and pgbouncer',
        render: (el) => { if (typeof ProfilesPage !== 'undefined') ProfilesPage.render(el); },
    },
    drift: {
        title: 'schema drift', sub: 'compare object drift between databases',
        render: (el) => { if (typeof SchemaDriftPage !== 'undefined') SchemaDriftPage.render(el); },
    },
    s3: {
        title: 's3 browser', sub: 'browse backup storage',
        render: (el) => { if (typeof S3Browser !== 'undefined') S3Browser.render(el); },
    },
    database: {
        title: 'export / import', sub: 'pg_dump & pg_restore',
        render: (el) => { if (typeof DatabaseIO !== 'undefined') DatabaseIO.render(el); },
    },
    logs: {
        title: 'logs', sub: 'postgresql log stream',
        render: (el) => { if (typeof LogStream !== 'undefined') LogStream.render(el); },
    },
    config: {
        title: 'config', sub: 'postgresql settings',
        render: (el) => { if (typeof ConfigViewer !== 'undefined') ConfigViewer.render(el); },
    },
    server: {
        title: 'server', sub: 'databases & schemas overview',
        render: (el) => { if (typeof ServerOverview !== 'undefined') ServerOverview.render(el); },
    },
    sql: {
        title: 'sql editor', sub: 'execute queries',
        render: (el) => { if (typeof SQLEditor !== 'undefined') SQLEditor.render(el); },
    },
    pgbouncer: {
        title: 'pgbouncer', sub: 'connection pool',
        render: (el) => { if (typeof PgBouncerUI !== 'undefined') PgBouncerUI.render(el); },
    },
    queries: {
        title: 'slow queries', sub: 'pg_stat_statements analysis',
        render: (el) => { if (typeof SlowQueries !== 'undefined') SlowQueries.render(el); },
    },

    vacuum: {
        title: 'vacuum monitor', sub: 'dead tuples & maintenance',
        render: (el) => { if (typeof VacuumMonitor !== 'undefined') VacuumMonitor.render(el); },
    },
    locks: {
        title: 'lock monitor', sub: 'conflicts and blocking chains',
        render: (el) => { if (typeof LockMonitor !== 'undefined') LockMonitor.render(el); },
    },
    roles: {
        title: 'roles & privileges', sub: 'role graph, grants, default privileges',
        render: (el) => { if (typeof RolesPage !== 'undefined') RolesPage.render(el); },
    },
    indexes: {
        title: 'index advisor', sub: 'missing, unused & duplicate',
        render: (el) => { if (typeof IndexAdvisor !== 'undefined') IndexAdvisor.render(el); },
    },
    extensions: {
        title: 'extensions', sub: 'postgresql extension manager',
        render: (el) => { if (typeof ExtensionManager !== 'undefined') ExtensionManager.render(el); },
    },
    alerts: {
        title: 'alerts', sub: 'notifications & thresholds',
        render: (el) => { if (typeof AlertsPage !== 'undefined') AlertsPage.render(el); },
    },
    tuner: {
        title: 'db tuner', sub: 'optimization wizard',
        render: (el) => { if (typeof TunerWizard !== 'undefined') TunerWizard.render(el); },
    },
    auth: {
        title: 'totp setup', sub: 'authenticator configuration',
        render: async (el) => {
            // Check if already set up
            try {
                const status = await api('/auth/status');
                if (status.data?.setup) {
                    el.innerHTML = `
                        <div class="totp-setup">
                            <div class="card" style="padding:24px">
                                <div class="card-title" style="margin-bottom:16px">TOTP authenticator</div>
                                <p class="green mono-xs" style="margin-bottom:12px">✓ TOTP is configured and active</p>
                                <p class="dim" style="font-size:10px">
                                    Your authenticator app is linked. To reconfigure, delete the secret file
                                    inside the container and restart.
                                </p>
                            </div>
                        </div>
                    `;
                    return;
                }
            } catch(e) { /* continue to setup */ }

            el.innerHTML = '<div class="totp-setup"><div class="dim">loading...</div></div>';
            try {
                const res = await api('/auth/setup');
                const info = res.data;
                el.innerHTML = `
                    <div class="totp-setup">
                        <div class="card" style="padding:24px">
                            <div class="card-title" style="margin-bottom:16px">TOTP authenticator setup</div>
                            <p class="dim" style="font-size:11px;margin-bottom:16px">
                                Scan the QR code below with Google Authenticator, Authy, or any TOTP app.<br>
                                Then enter the 6-digit code to confirm and save.
                            </p>
                            <div class="qr-placeholder">
                                <img src="https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=${encodeURIComponent(info.url)}" alt="QR" width="180" height="180">
                            </div>
                            <div class="secret-display mono-xs">${escHtml(info.secret)}</div>
                            <p class="dim" style="font-size:10px;margin:8px 0 16px">
                                issuer: ${escHtml(info.issuer)} · account: ${escHtml(info.account)}
                            </p>
                            <div style="display:flex;gap:8px;justify-content:center;align-items:center">
                                <input type="text" id="setup-otp-input" class="totp-input" maxlength="6"
                                    pattern="[0-9]*" inputmode="numeric" placeholder="000000"
                                    style="width:160px;margin:0;font-size:16px">
                                <button onclick="TOTPSetup.confirm()" class="btn btn-sm btn-primary">
                                    confirm & save
                                </button>
                            </div>
                            <div id="setup-result" class="mono-xs" style="margin-top:8px"></div>
                        </div>
                    </div>
                `;
            } catch (e) {
                el.innerHTML = `<div class="dim">error: ${escHtml(e.message)}</div>`;
            }
        },
    },
};

function navigate() {
    const hash = location.hash.replace('#', '') || 'dashboard';
    const page = pages[hash] || pages.dashboard;

    // Cleanup previous page
    if (typeof LogStream !== 'undefined') LogStream.destroy();

    document.getElementById('page-title').textContent = page.title;
    document.getElementById('page-subtitle').textContent = page.sub;

    document.querySelectorAll('.nav-link').forEach(l => {
        l.classList.toggle('active', l.getAttribute('data-page') === hash);
    });

    const content = document.getElementById('page-content');
    content.innerHTML = '';
    page.render(content);

    if (page.ws) connectWebSocket();
    else disconnectWebSocket();
}

// ========================
// TOTP Setup (first-time only)
// ========================
const TOTPSetup = {
    async confirm() {
        const input = document.getElementById('setup-otp-input');
        const result = document.getElementById('setup-result');
        if (!input || !result) return;
        const code = input.value.trim();
        if (code.length !== 6) { result.innerHTML = '<span class="red">enter 6 digits</span>'; return; }
        try {
            const res = await fetch(API_BASE + '/auth/setup/confirm', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ code }),
            });
            const data = await res.json();
            if (!res.ok) {
                result.innerHTML = `<span class="red">✗ ${escHtml(data.error || 'invalid code')}</span>`;
                input.value = '';
                input.focus();
                return;
            }
            // Save session from confirm
            sessionStorage.setItem('pgaio_session', data.data.sessionId);
            IdleTracker.reset();
            result.innerHTML = '<span class="green">✓ saved! redirecting...</span>';
            showToast('TOTP configured successfully', 'success');
            setTimeout(() => { location.hash = '#dashboard'; navigate(); }, 1000);
        } catch (e) {
            result.innerHTML = '<span class="red">connection error</span>';
        }
    }
};

// ========================
// Init
// ========================
document.addEventListener('DOMContentLoaded', async () => {
    lucide.createIcons();
    IdleTracker.start();

    // Check auth status
    try {
        const res = await api('/auth/status');
        const { setup } = res.data || {};
        if (!setup) {
            // Force to setup page
            location.hash = '#auth';
        }
    } catch (e) { /* continue */ }

    navigate();
    window.addEventListener('hashchange', navigate);
    const btn = document.getElementById('btn-refresh');
    if (btn) btn.addEventListener('click', navigate);
});
