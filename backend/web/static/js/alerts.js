// PGAIO — Alerts & Settings

const AlertsPage = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">alerts & notifications</span>
                <button onclick="AlertsPage.save()" class="btn btn-sm btn-primary"><i data-lucide="save" class="icon-sm"></i> save</button>
            </div>
            <div id="alerts-settings"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
            <div class="card-title" style="margin-top:16px">alert history</div>
            <div id="alerts-history"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    async load() {
        try {
            const [settingsRes, alertsRes] = await Promise.all([
                api('/settings'),
                api('/alerts'),
            ]);
            this._config = settingsRes.data;
            this._alerts = alertsRes.data;
            this.renderSettings();
            this.renderHistory();
        } catch (e) { /* handled */ }
    },

    renderSettings() {
        const el = document.getElementById('alerts-settings');
        if (!el || !this._config) return;
        const a = this._config.alerts;

        el.innerHTML = `
            <div class="grid-3" style="gap:8px">
                <div class="card">
                    <div class="card-title">general</div>
                    <label class="flex gap-4 mono-xs" style="align-items:center;margin-bottom:8px">
                        <input type="checkbox" id="alert-enabled" ${a.enabled ? 'checked' : ''}>
                        <span>enable alerting</span>
                    </label>
                </div>
                <div class="card mt-2">
                    <div class="card-title">telegram</div>
                    <div class="mono-xs dim mb-4">bot token</div>
                    <input type="text" id="alert-tg-token" value="${escHtml(a.telegram?.botToken || '')}"
                        style="width:100%;background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 6px;font-size:11px;font-family:var(--font);margin-bottom:8px;box-sizing:border-box">
                    <div class="mono-xs dim mb-4">chat id</div>
                    <input type="text" id="alert-tg-chat" value="${escHtml(a.telegram?.chatId || '')}"
                        style="width:100%;background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 6px;font-size:11px;font-family:var(--font);margin-bottom:8px;box-sizing:border-box">
                    <button onclick="AlertsPage.testNotification()" class="btn btn-sm" style="font-size:9px">test</button>
                </div>
                <div class="card mt-2">
                    <div class="card-title">thresholds</div>
                    ${this.thresholdInput('disk usage %', 'alert-disk', a.thresholds?.diskUsagePct || 80)}
                    ${this.thresholdInput('connections %', 'alert-conn', a.thresholds?.connectionsPct || 80)}
                    ${this.thresholdInput('repl lag (s)', 'alert-repl', a.thresholds?.replicationLagSec || 30)}
                    ${this.thresholdInput('backup age (h)', 'alert-bkp', a.thresholds?.backupMaxAgeHours || 24)}
                </div>
            </div>
        `;
    },

    thresholdInput(label, id, value) {
        return `<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:6px">
            <span class="mono-xs dim">${label}</span>
            <input type="number" id="${id}" value="${value}" min="0" max="100"
                style="width:60px;background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                padding:2px 4px;font-size:11px;font-family:var(--font);text-align:right">
        </div>`;
    },

    renderHistory() {
        const el = document.getElementById('alerts-history');
        if (!el || !this._alerts) return;
        const history = this._alerts.history || [];

        if (history.length === 0) {
            el.innerHTML = '<div class="card"><span class="dim mono-xs">no alerts</span></div>';
            return;
        }

        el.innerHTML = `<div class="card" style="padding:0;max-height:300px;overflow-y:auto">
            <table class="data-table" style="table-layout:fixed;width:100%"><thead><tr>
                <th style="width:150px">time</th><th style="width:70px">level</th>
                <th style="width:100px">metric</th><th>message</th>
            </tr></thead><tbody>${history.map(e => `<tr>
                <td class="dim">${new Date(e.time).toLocaleString()}</td>
                <td class="${e.level === 'critical' ? 'red' : 'yellow'}">${e.level}</td>
                <td>${e.metric}</td>
                <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap">${escHtml(e.message)}</td>
            </tr>`).join('')}</tbody></table></div>`;
    },

    async save() {
        if (!this._config) return;
        this._config.alerts = {
            enabled: document.getElementById('alert-enabled')?.checked || false,
            telegram: {
                botToken: document.getElementById('alert-tg-token')?.value || '',
                chatId: document.getElementById('alert-tg-chat')?.value || '',
            },
            thresholds: {
                diskUsagePct: parseInt(document.getElementById('alert-disk')?.value) || 80,
                connectionsPct: parseInt(document.getElementById('alert-conn')?.value) || 80,
                replicationLagSec: parseInt(document.getElementById('alert-repl')?.value) || 30,
                backupMaxAgeHours: parseInt(document.getElementById('alert-bkp')?.value) || 24,
            },
        };
        try {
            await apiProtected('/settings', { method: 'POST', body: JSON.stringify(this._config) });
            showToast('settings saved', 'success');
        } catch (e) { /* handled */ }
    },

    async testNotification() {
        try {
            await apiProtected('/alerts/test', { method: 'POST' });
            showToast('test notification sent', 'success');
        } catch (e) { /* handled */ }
    },
};
