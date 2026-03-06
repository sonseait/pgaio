// PGAIO — Table Size Browser

const TableBrowser = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">table sizes</span>
                <button onclick="TableBrowser.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="tables-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    async load() {
        try {
            const res = await api('/tables/sizes');
            const tables = res.data || [];
            const el = document.getElementById('tables-content');
            if (!el) return;

            if (tables.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no user tables</span></div>';
                return;
            }

            const maxTotal = Math.max(...tables.map(t => t.totalBytes), 1);

            el.innerHTML = `<div class="mono-xs dim mb-8">${tables.length} tables</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 130px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th>table</th>
                        <th style="width:90px">total</th>
                        <th style="width:80px">table</th>
                        <th style="width:80px">index</th>
                        <th style="width:70px">toast</th>
                        <th style="width:80px">rows</th>
                        <th style="width:200px">distribution</th>
                    </tr></thead>
                    <tbody>${tables.map(t => {
                        const tablePct = t.totalBytes > 0 ? (t.tableBytes / t.totalBytes * 100).toFixed(0) : 0;
                        const indexPct = t.totalBytes > 0 ? (t.indexBytes / t.totalBytes * 100).toFixed(0) : 0;
                        const toastPct = t.totalBytes > 0 ? (t.toastBytes / t.totalBytes * 100).toFixed(0) : 0;
                        const widthPct = (t.totalBytes / maxTotal * 100).toFixed(0);
                        return `<tr>
                            <td title="${t.schema}.${t.table}">${t.schema === 'public' ? '' : t.schema + '.'}${t.table}</td>
                            <td>${formatBytes(t.totalBytes)}</td>
                            <td>${formatBytes(t.tableBytes)}</td>
                            <td>${formatBytes(t.indexBytes)}</td>
                            <td>${formatBytes(t.toastBytes)}</td>
                            <td>${fmtNum(t.rowEstimate)}</td>
                            <td><div style="display:flex;height:12px;width:${widthPct}%;min-width:4px">
                                <div style="background:var(--accent);width:${tablePct}%" title="table ${tablePct}%"></div>
                                <div style="background:var(--green);width:${indexPct}%" title="index ${indexPct}%"></div>
                                <div style="background:var(--yellow);width:${toastPct}%" title="toast ${toastPct}%"></div>
                            </div></td>
                        </tr>`;
                    }).join('')}</tbody>
                </table></div>`;
        } catch (e) { /* handled */ }
    },
};
