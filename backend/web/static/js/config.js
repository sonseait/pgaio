// PGAIO — PostgreSQL Config Editor

const ConfigViewer = {
    _data: [],

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">postgresql configuration</span>
                <div class="flex gap-4">
                    <input type="text" id="config-search" placeholder="search settings..."
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 8px;font-size:11px;font-family:var(--font);width:200px" />
                    <select id="config-source-filter"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 8px;font-size:11px;font-family:var(--font)">
                        <option value="">all sources</option>
                    </select>
                    <button onclick="ConfigViewer.reloadPg()" class="btn btn-sm" title="pg_reload_conf()">
                        <i data-lucide="refresh-cw" class="icon-sm"></i> reload
                    </button>
                    <button onclick="ConfigViewer.restartPg()" class="btn btn-sm btn-danger" title="pg_ctl restart">
                        <i data-lucide="power" class="icon-sm"></i> restart
                    </button>
                </div>
            </div>
            <div id="config-content">
                <div class="card"><span class="dim mono-xs">loading...</span></div>
            </div>
        `;

        lucide.createIcons();
        document.getElementById('config-search').addEventListener('input', () => this.filterAndRender());
        document.getElementById('config-source-filter').addEventListener('change', () => this.filterAndRender());

        await this.loadConfig();
    },

    async loadConfig() {
        try {
            const res = await api('/config');
            this._data = res.data || [];
            const sources = [...new Set(this._data.map(i => i.source))].sort();
            const sel = document.getElementById('config-source-filter');
            if (sel) {
                sources.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s; opt.textContent = s;
                    sel.appendChild(opt);
                });
            }
            this.filterAndRender();
        } catch (e) {
            document.getElementById('config-content').innerHTML =
                `<div class="card"><span class="red mono-xs">error: ${e.message}</span></div>`;
        }
    },

    filterAndRender() {
        const search = (document.getElementById('config-search')?.value || '').toLowerCase();
        const source = document.getElementById('config-source-filter')?.value || '';

        let filtered = this._data;
        if (search) filtered = filtered.filter(i =>
            i.name.toLowerCase().includes(search) ||
            i.desc.toLowerCase().includes(search) ||
            i.category.toLowerCase().includes(search)
        );
        if (source) filtered = filtered.filter(i => i.source === source);

        const groups = {};
        filtered.forEach(i => {
            if (!groups[i.category]) groups[i.category] = [];
            groups[i.category].push(i);
        });

        const container = document.getElementById('config-content');
        if (!container) return;

        if (filtered.length === 0) {
            container.innerHTML = '<div class="card"><span class="dim mono-xs">no matching settings</span></div>';
            return;
        }

        let html = `<div class="mono-xs dim mb-8">${filtered.length} settings</div>`;
        for (const [cat, items] of Object.entries(groups)) {
            html += `<div class="card mb-8" style="padding:0">
                <div style="padding:6px 10px;border-bottom:1px solid var(--border);background:var(--bg-2)">
                    <span class="accent" style="font-size:11px;font-weight:600">${this.esc(cat)}</span>
                    <span class="dim" style="font-size:10px;margin-left:6px">(${items.length})</span>
                </div>
                <div style="overflow-x:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th style="width:200px">name</th>
                        <th style="width:120px">value</th>
                        <th style="width:50px">unit</th>
                        <th>description</th>
                        <th style="width:90px">context</th>
                        <th style="width:80px">action</th>
                    </tr></thead>
                    <tbody>`;
            items.forEach(i => {
                const modified = i.source !== 'default' && i.source !== 'override';
                const cls = modified ? 'accent' : '';
                const editable = i.context !== 'internal';
                const ctxCls = i.context === 'postmaster' ? 'red' : i.context === 'sighup' ? 'green' : 'dim';
                html += `<tr>
                    <td class="${cls}" style="word-break:break-all">${this.esc(i.name)}</td>
                    <td id="val-${i.name}" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${this.esc(i.setting)}">${this.esc(i.setting)}</td>
                    <td class="dim">${i.unit || ''}</td>
                    <td class="dim" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${this.esc(i.desc)}">${this.esc(i.desc)}</td>
                    <td><span class="mono-xs ${ctxCls}">${this.esc(i.context)}</span></td>
                    <td>${editable ? `<button onclick="ConfigViewer.edit('${this.esc(i.name)}','${this.esc(i.setting)}','${this.esc(i.context)}')" class="btn btn-sm" style="font-size:9px">edit</button>` : '<span class="dim mono-xs">-</span>'}</td>
                </tr>`;
            });
            html += `</tbody></table></div></div>`;
        }
        container.innerHTML = html;
    },

    async edit(name, currentVal, context) {
        // Remove existing modal if any
        document.getElementById('config-modal')?.remove();

        const isRestart = context === 'postmaster';
        const overlay = document.createElement('div');
        overlay.id = 'config-modal';
        overlay.className = 'modal-overlay';
        overlay.innerHTML = `
            <div class="modal-dialog">
                <div class="modal-header">
                    <span class="modal-title">edit parameter</span>
                    <button class="modal-close" onclick="document.getElementById('config-modal').remove()">&times;</button>
                </div>
                <div class="modal-body">
                    <label>parameter</label>
                    <div class="mono-xs accent" style="margin-bottom:10px">${this.esc(name)}</div>
                    <label>current value</label>
                    <div class="mono-xs dim" style="margin-bottom:10px">${this.esc(currentVal)}</div>
                    ${isRestart ? '<div class="mono-xs red" style="margin-bottom:10px">⚠ requires postgresql restart</div>' : '<div class="mono-xs green" style="margin-bottom:10px">requires reload only</div>'}
                    <label>new value</label>
                    <input type="text" id="config-modal-input" value="${this.esc(currentVal)}" autofocus />
                </div>
                <div class="modal-footer">
                    <button class="btn btn-sm" onclick="document.getElementById('config-modal').remove()">cancel</button>
                    <button class="btn btn-sm btn-primary" id="config-modal-save">save</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);

        // Focus input and select all text
        const input = document.getElementById('config-modal-input');
        setTimeout(() => { input.focus(); input.select(); }, 50);

        // Close on overlay click
        overlay.addEventListener('click', (e) => {
            if (e.target === overlay) overlay.remove();
        });

        // Enter key = save
        input.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') document.getElementById('config-modal-save').click();
            if (e.key === 'Escape') overlay.remove();
        });

        // Save handler
        document.getElementById('config-modal-save').addEventListener('click', async () => {
            const newVal = input.value;
            if (newVal === currentVal) { overlay.remove(); return; }

            try {
                await apiProtected(`/config/set?name=${encodeURIComponent(name)}&value=${encodeURIComponent(newVal)}`, { method: 'POST' });
                showToast(`${name} = ${newVal}` + (isRestart ? ' (restart required)' : ' (reloaded)'), 'success');
                overlay.remove();
                await this.loadConfig();
            } catch (e) {
                overlay.remove();
            }
        });
    },

    async reloadPg() {
        try {
            await apiProtected('/config/set?name=__reload__&value=1', { method: 'POST' }).catch(() => {});
            // Just call pg_reload_conf via SQL
            await apiProtected('/sql/execute', {
                method: 'POST',
                body: JSON.stringify({ query: 'SELECT pg_reload_conf()' })
            });
            showToast('PostgreSQL configuration reloaded', 'success');
            await this.loadConfig();
        } catch (e) { /* handled */ }
    },

    async restartPg() {
        if (!await showConfirm('restart postgresql', 'Restart PostgreSQL?\n\nThis will briefly disconnect all clients.\nRequired for "postmaster" context settings.', { danger: true, confirmText: 'restart' })) return;
        try {
            await apiProtected('/config/restart', { method: 'POST' });
            showToast('PostgreSQL restart initiated — reconnecting in 5s...', 'info');
            setTimeout(async () => {
                try { await this.loadConfig(); showToast('PostgreSQL restarted', 'success'); }
                catch { showToast('Still restarting — try refreshing', 'warning'); }
            }, 5000);
        } catch (e) { /* handled */ }
    },

    esc(s) { return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/'/g, '&#39;').replace(/"/g, '&quot;'); },
};
