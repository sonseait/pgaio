// PGAIO — Index Advisor

const IndexAdvisor = {
    _tab: 'missing',

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">index advisor</span>
                    <div id="indexes-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                </div>
                <div class="flex gap-4">
                    <button onclick="IndexAdvisor.switchTab('missing')" class="btn btn-sm" id="tab-missing">missing</button>
                    <button onclick="IndexAdvisor.switchTab('unused')" class="btn btn-sm" id="tab-unused">unused</button>
                    <button onclick="IndexAdvisor.switchTab('duplicates')" class="btn btn-sm" id="tab-duplicates">duplicates</button>
                </div>
            </div>
            <div id="indexes-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('indexes-db-sel'), () => this.load());
        await this.load();
    },

    switchTab(tab) {
        this._tab = tab;
        document.querySelectorAll('[id^="tab-"]').forEach(b => b.classList.remove('btn-primary'));
        const el = document.getElementById('tab-' + tab);
        if (el) el.classList.add('btn-primary');
        this.renderTab();
    },

    async load() {
        try {
            const res = await api('/indexes/advice' + DbSelector.getParam());
            this._data = res.data || {};
            this.switchTab(this._tab);
        } catch (e) { /* handled */ }
    },

    renderTab() {
        const el = document.getElementById('indexes-content');
        if (!el || !this._data) return;

        if (this._tab === 'missing') {
            const items = this._data.missing || [];
            if (items.length === 0) { el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no missing indexes detected</span></div>'; return; }
            el.innerHTML = `<div class="mono-xs dim mb-8">${items.length} tables with excessive sequential scans</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 130px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%"><thead><tr>
                    <th>table</th><th style="width:90px">seq scans</th><th style="width:90px">idx scans</th><th style="width:90px">difference</th><th style="width:80px">size</th>
                </tr></thead><tbody>${items.map(i => `<tr>
                    <td>${i.schema}.${i.table}</td>
                    <td class="red">${fmtNum(i.seqScan)}</td>
                    <td class="green">${fmtNum(i.idxScan)}</td>
                    <td class="yellow">${fmtNum(i.diff)}</td>
                    <td>${i.size}</td>
                </tr>`).join('')}</tbody></table></div>`;
        } else if (this._tab === 'unused') {
            const items = this._data.unused || [];
            if (items.length === 0) { el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no unused indexes</span></div>'; return; }
            el.innerHTML = `<div class="mono-xs dim mb-8">${items.length} unused indexes (0 scans)</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 130px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%"><thead><tr>
                    <th style="width:200px">table</th><th>index</th><th style="width:80px">scans</th><th style="width:80px">size</th>
                </tr></thead><tbody>${items.map(i => `<tr>
                    <td>${i.schema}.${i.table}</td>
                    <td class="yellow">${i.index}</td>
                    <td class="red">0</td>
                    <td>${i.size}</td>
                </tr>`).join('')}</tbody></table></div>`;
        } else {
            const items = this._data.duplicates || [];
            if (items.length === 0) { el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no duplicate indexes</span></div>'; return; }
            el.innerHTML = `<div class="mono-xs dim mb-8">${items.length} duplicate index group(s)</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 130px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%"><thead><tr>
                    <th style="width:200px">table</th><th style="width:250px">indexes</th><th>definition</th>
                </tr></thead><tbody>${items.map(i => `<tr>
                    <td>${i.table}</td>
                    <td class="red">${escHtml(i.indexes)}</td>
                    <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(i.indexDef)}">${escHtml(i.indexDef)}</td>
                </tr>`).join('')}</tbody></table></div>`;
        }
    },
};
