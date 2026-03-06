// PGAIO — Extension Manager

const ExtensionManager = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">extensions</span>
                    <div id="ext-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                </div>
                <div class="flex gap-4">
                    <input type="text" id="ext-search" placeholder="search..."
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 8px;font-size:11px;font-family:var(--font);width:200px" />
                    <select id="ext-filter"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 8px;font-size:11px;font-family:var(--font)">
                        <option value="all">all</option>
                        <option value="installed">installed</option>
                        <option value="available">available</option>
                    </select>
                </div>
            </div>
            <div id="ext-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        document.getElementById('ext-search').addEventListener('input', () => this.renderList());
        document.getElementById('ext-filter').addEventListener('change', () => this.renderList());
        await DbSelector.renderInto(document.getElementById('ext-db-sel'), () => this.load());
        await this.load();
    },

    async load() {
        try {
            const res = await api('/extensions' + DbSelector.getParam());
            this._data = res.data || [];
            this.renderList();
        } catch (e) { /* handled */ }
    },

    renderList() {
        const search = (document.getElementById('ext-search')?.value || '').toLowerCase();
        const filter = document.getElementById('ext-filter')?.value || 'all';
        let items = this._data || [];

        if (search) items = items.filter(e => e.name.includes(search) || (e.comment || '').toLowerCase().includes(search));
        if (filter === 'installed') items = items.filter(e => e.installed);
        if (filter === 'available') items = items.filter(e => !e.installed);

        const el = document.getElementById('ext-content');
        if (!el) return;

        el.innerHTML = `<div class="mono-xs dim mb-8">${items.length} extensions</div>
            <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 141px);overflow-y:auto">
            <table class="data-table" style="table-layout:fixed;width:100%">
                <thead><tr>
                    <th style="width:50px">status</th>
                    <th style="width:180px">name</th>
                    <th style="width:80px">version</th>
                    <th>description</th>
                    <th style="width:80px">action</th>
                </tr></thead>
                <tbody>${items.map(e => `<tr>
                    <td>${e.installed ? '<span class="green">●</span>' : '<span class="dim">○</span>'}</td>
                    <td class="${e.installed ? 'accent' : ''}">${escHtml(e.name)}</td>
                    <td class="dim">${e.installed ? e.installedVersion : e.defaultVersion}</td>
                    <td class="dim" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(e.comment || '')}">${escHtml(e.comment || '')}</td>
                    <td>${e.installed
                        ? `<button onclick="ExtensionManager.uninstall('${e.name}')" class="btn btn-sm" style="font-size:9px">uninstall</button>`
                        : `<button onclick="ExtensionManager.install('${e.name}')" class="btn btn-sm" style="font-size:9px">install</button>`
                    }</td>
                </tr>`).join('')}</tbody>
            </table></div>`;
    },

    async install(name) {
        try {
            await apiProtected('/extensions/install', { method: 'POST', body: JSON.stringify({ name, database: DbSelector.getSelected() }) });
            showToast(name + ' installed', 'success');
            await this.load();
        } catch (e) { /* handled */ }
    },

    async uninstall(name) {
        if (!await showConfirm('uninstall extension', `Uninstall ${name}?`, { danger: true, confirmText: 'uninstall' })) return;
        try {
            await apiProtected('/extensions/uninstall', { method: 'POST', body: JSON.stringify({ name, database: DbSelector.getSelected() }) });
            showToast(name + ' removed', 'success');
            await this.load();
        } catch (e) { /* handled */ }
    },
};
