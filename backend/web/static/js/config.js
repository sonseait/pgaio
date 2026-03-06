// PGAIO — PostgreSQL Config Viewer

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
                </div>
            </div>
            <div id="config-content">
                <div class="card"><span class="dim mono-xs">loading...</span></div>
            </div>
        `;

        document.getElementById('config-search').addEventListener('input', () => this.filterAndRender());
        document.getElementById('config-source-filter').addEventListener('change', () => this.filterAndRender());

        await this.loadConfig();
    },

    async loadConfig() {
        try {
            const res = await api('/config');
            this._data = res.data || [];
            // Populate source filter
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

        // Group by category
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
                        <th style="width:22%">name</th>
                        <th style="width:12%">value</th>
                        <th style="width:5%">unit</th>
                        <th style="width:51%">description</th>
                        <th style="width:10%">source</th>
                    </tr></thead>
                    <tbody>`;
            items.forEach(i => {
                const modified = i.source !== 'default' && i.source !== 'override';
                const cls = modified ? 'accent' : '';
                html += `<tr>
                    <td class="${cls}" style="word-break:break-all">${this.esc(i.name)}</td>
                    <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${this.esc(i.setting)}">${this.esc(i.setting)}</td>
                    <td class="dim">${i.unit || ''}</td>
                    <td class="dim" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${this.esc(i.desc)}">${this.esc(i.desc)}</td>
                    <td><span class="mono-xs ${i.source === 'default' ? 'dim' : 'yellow'}">${this.esc(i.source)}</span></td>
                </tr>`;
            });
            html += `</tbody></table></div></div>`;
        }
        container.innerHTML = html;
    },

    esc(s) { return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); },
};
