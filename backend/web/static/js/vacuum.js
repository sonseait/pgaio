// PGAIO — Vacuum Monitor + Bloat Analysis

const VacuumMonitor = {
    _tab: 'vacuum',

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">vacuum & bloat</span>
                    <div style="display:flex;gap:2px">
                        <button onclick="VacuumMonitor.switchTab('vacuum')" class="btn btn-sm" id="tab-vacuum" style="font-size:9px">vacuum stats</button>
                        <button onclick="VacuumMonitor.switchTab('bloat')" class="btn btn-sm" id="tab-bloat" style="font-size:9px">bloat analysis</button>
                    </div>
                </div>
                <button onclick="VacuumMonitor.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="vacuum-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    switchTab(tab) {
        this._tab = tab;
        document.getElementById('tab-vacuum').style.opacity = tab === 'vacuum' ? '1' : '0.5';
        document.getElementById('tab-bloat').style.opacity = tab === 'bloat' ? '1' : '0.5';
        this.load();
    },

    async load() {
        if (this._tab === 'bloat') return this.loadBloat();
        return this.loadVacuum();
    },

    async loadVacuum() {
        try {
            const res = await api('/vacuum/stats');
            const stats = res.data || [];
            const el = document.getElementById('vacuum-content');
            if (!el) return;

            if (stats.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no user tables</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs dim mb-8">${stats.length} tables</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 137px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th>table</th>
                        <th style="width:80px">live</th>
                        <th style="width:80px">dead</th>
                        <th style="width:70px">dead %</th>
                        <th style="width:100px">last vacuum</th>
                        <th style="width:100px">last auto</th>
                        <th style="width:70px">v #</th>
                        <th style="width:80px">actions</th>
                    </tr></thead>
                    <tbody>${stats.map(s => {
                        const deadCls = s.deadPct > 20 ? 'red' : s.deadPct > 5 ? 'yellow' : '';
                        const tableFull = s.schema === 'public' ? s.table : s.schema + '.' + s.table;
                        return `<tr>
                            <td>${tableFull}</td>
                            <td>${fmtNum(s.liveTuples)}</td>
                            <td class="${deadCls}">${fmtNum(s.deadTuples)}</td>
                            <td class="${deadCls}">${s.deadPct}%</td>
                            <td class="dim">${s.lastVacuum ? timeAgo(s.lastVacuum) : '-'}</td>
                            <td class="dim">${s.lastAutovacuum ? timeAgo(s.lastAutovacuum) : '-'}</td>
                            <td>${s.vacuumCount}</td>
                            <td>
                                <button onclick="VacuumMonitor.triggerVacuum('${escHtml(s.schema)}','${escHtml(s.table)}',false)" class="btn btn-sm" title="VACUUM ANALYZE" style="font-size:9px;padding:2px 4px">vac</button>
                                <button onclick="VacuumMonitor.triggerVacuum('${escHtml(s.schema)}','${escHtml(s.table)}',true)" class="btn btn-sm btn-danger" title="VACUUM FULL (locks table!)" style="font-size:9px;padding:2px 4px">full</button>
                            </td>
                        </tr>`;
                    }).join('')}</tbody>
                </table></div>`;
        } catch (e) { /* handled */ }
    },

    async loadBloat() {
        try {
            const res = await api('/vacuum/bloat');
            const stats = res.data || [];
            const el = document.getElementById('vacuum-content');
            if (!el) return;

            if (stats.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no bloated tables detected — all clean!</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs dim mb-8">${stats.length} tables with dead tuples</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 137px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th>table</th>
                        <th style="width:90px">total size</th>
                        <th style="width:80px">live</th>
                        <th style="width:80px">dead</th>
                        <th style="width:80px">bloat %</th>
                        <th style="width:80px">actions</th>
                    </tr></thead>
                    <tbody>${stats.map(s => {
                        const cls = s.bloatPct > 30 ? 'red' : s.bloatPct > 10 ? 'yellow' : '';
                        const tableFull = s.schema === 'public' ? s.table : s.schema + '.' + s.table;
                        return `<tr>
                            <td>${tableFull}</td>
                            <td>${escHtml(s.totalSize)}</td>
                            <td>${fmtNum(s.liveTuples)}</td>
                            <td class="${cls}">${fmtNum(s.deadTuples)}</td>
                            <td class="${cls}">${s.bloatPct}%</td>
                            <td>
                                <button onclick="VacuumMonitor.triggerVacuum('${escHtml(s.schema)}','${escHtml(s.table)}',false)" class="btn btn-sm" title="VACUUM ANALYZE" style="font-size:9px">vacuum</button>
                            </td>
                        </tr>`;
                    }).join('')}</tbody>
                </table></div>`;
        } catch (e) { /* handled */ }
    },

    async triggerVacuum(schema, table, full) {
        const cmd = full ? 'VACUUM FULL ANALYZE' : 'VACUUM ANALYZE';
        const tableName = schema === 'public' ? table : schema + '.' + table;

        if (full) {
            if (!await showConfirm('vacuum full', `⚠ VACUUM FULL on "${tableName}" will LOCK the table until complete.\n\nThis can take a long time for large tables. Use during low-traffic periods.`, { danger: true, confirmText: 'run vacuum full' })) return;
        }

        try {
            await apiProtected('/vacuum/trigger', {
                method: 'POST',
                body: JSON.stringify({ schema, table, full })
            });
            showToast(`${cmd} started on ${tableName}`, 'success');
            setTimeout(() => this.load(), 3000);
        } catch (e) {
            showToast(`vacuum failed: ${e.message}`, 'error');
        }
    },
};
