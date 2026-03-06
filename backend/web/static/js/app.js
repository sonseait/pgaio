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

// TOTP-protected API call — prompts for 6-digit code then sends with X-TOTP-Code header
function apiProtected(path, options = {}) {
    return new Promise((resolve, reject) => {
        showTOTPModal((code) => {
            const headers = {
                'Content-Type': 'application/json',
                'X-TOTP-Code': code,
                ...(options.headers || {}),
            };
            fetch(API_BASE + path, { ...options, headers })
                .then(async res => {
                    if (!res.ok) {
                        const err = await res.json().catch(() => ({ error: res.statusText }));
                        const msg = err.error || res.statusText;
                        showToast(msg, 'error');
                        throw new Error(msg);
                    }
                    return res.json();
                })
                .then(resolve)
                .catch(reject);
        }, reject);
    });
}

// ========================
// TOTP Modal
// ========================
function showTOTPModal(onConfirm, onCancel) {
    // Remove existing modal
    const old = document.getElementById('totp-modal');
    if (old) old.remove();

    const modal = document.createElement('div');
    modal.id = 'totp-modal';
    modal.className = 'totp-overlay';
    modal.innerHTML = `
        <div class="totp-dialog">
            <div class="totp-title">authentication required</div>
            <div class="totp-desc">enter 6-digit TOTP code from your authenticator app</div>
            <input type="text" id="totp-input" class="totp-input" maxlength="6"
                   pattern="[0-9]*" inputmode="numeric" autocomplete="one-time-code"
                   placeholder="000000" autofocus>
            <div class="totp-actions">
                <button class="btn btn-sm" id="totp-cancel">cancel</button>
                <button class="btn btn-sm btn-primary" id="totp-confirm">verify</button>
            </div>
        </div>
    `;
    document.body.appendChild(modal);

    const input = document.getElementById('totp-input');
    input.focus();

    const cleanup = () => modal.remove();

    const submit = () => {
        const code = input.value.trim();
        if (code.length !== 6 || !/^\d+$/.test(code)) {
            input.style.borderColor = 'var(--red)';
            input.focus();
            return;
        }
        cleanup();
        onConfirm(code);
    };

    document.getElementById('totp-confirm').addEventListener('click', submit);
    document.getElementById('totp-cancel').addEventListener('click', () => {
        cleanup();
        if (onCancel) onCancel(new Error('cancelled'));
    });
    input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') submit();
        if (e.key === 'Escape') { cleanup(); if (onCancel) onCancel(new Error('cancelled')); }
    });
    // Auto-submit when 6 digits entered
    input.addEventListener('input', () => {
        if (input.value.trim().length === 6) submit();
    });
}

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
    s3: {
        title: 's3 browser', sub: 'browse backup storage',
        render: (el) => { if (typeof S3Browser !== 'undefined') S3Browser.render(el); },
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
    auth: {
        title: 'totp setup', sub: 'authenticator configuration',
        render: async (el) => {
            el.innerHTML = '<div class="totp-setup"><div class="dim">loading...</div></div>';
            try {
                const res = await api('/auth/setup');
                const info = res.data;
                el.innerHTML = `
                    <div class="totp-setup">
                        <div class="card" style="padding:24px">
                            <div class="card-title" style="margin-bottom:16px">TOTP authenticator setup</div>
                            <p class="dim" style="font-size:11px;margin-bottom:16px">
                                Scan the QR code below with Google Authenticator, Authy, or any TOTP app.
                                Or manually enter the secret key.
                            </p>
                            <div class="qr-placeholder">
                                <img src="https://api.qrserver.com/v1/create-qr-code/?size=180x180&data=${encodeURIComponent(info.url)}" alt="QR" width="180" height="180">
                            </div>
                            <div class="secret-display mono-xs">${escHtml(info.secret)}</div>
                            <p class="dim" style="font-size:10px;margin:8px 0 16px">
                                issuer: ${escHtml(info.issuer)} · account: ${escHtml(info.account)}
                            </p>
                            <div style="display:flex;gap:8px;justify-content:center;align-items:center">
                                <input type="text" id="totp-test-input" class="totp-input" maxlength="6"
                                    pattern="[0-9]*" inputmode="numeric" placeholder="000000"
                                    style="width:160px;margin:0;font-size:16px">
                                <button onclick="TOTPSetup.verify()" class="btn btn-sm btn-primary">
                                    verify
                                </button>
                            </div>
                            <div id="totp-test-result" class="mono-xs" style="margin-top:8px"></div>
                        </div>
                    </div>
                `;
            } catch (e) {
                el.innerHTML = `<div class="dim">error loading TOTP setup: ${escHtml(e.message)}</div>`;
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
// Init
// ========================
const TOTPSetup = {
    async verify() {
        const input = document.getElementById('totp-test-input');
        const result = document.getElementById('totp-test-result');
        if (!input || !result) return;
        const code = input.value.trim();
        if (code.length !== 6) { result.innerHTML = '<span class="red">enter 6 digits</span>'; return; }
        try {
            await api('/auth/verify', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ code }),
            });
            result.innerHTML = '<span class="green">✓ valid — authenticator is configured correctly</span>';
        } catch (e) {
            result.innerHTML = '<span class="red">✗ invalid code — check your authenticator app</span>';
        }
    }
};

document.addEventListener('DOMContentLoaded', () => {
    lucide.createIcons();
    navigate();
    window.addEventListener('hashchange', navigate);
    const btn = document.getElementById('btn-refresh');
    if (btn) btn.addEventListener('click', navigate);
});
