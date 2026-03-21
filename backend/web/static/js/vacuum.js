// PGAIO — Vacuum Monitor + Bloat Analysis + pg_repack

const VacuumMonitor = {
    _tab: 'vacuum',
    _repackTimer: null,

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">vacuum & bloat</span>
                    <div id="vacuum-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                    <div style="display:flex;gap:2px">
                        <button onclick="VacuumMonitor.switchTab('vacuum')" class="btn btn-sm" id="tab-vacuum" style="font-size:9px">vacuum stats</button>
                        <button onclick="VacuumMonitor.switchTab('bloat')" class="btn btn-sm" id="tab-bloat" style="font-size:9px">bloat analysis</button>
                        <button onclick="VacuumMonitor.switchTab('repack')" class="btn btn-sm" id="tab-repack" style="font-size:9px">pg_repack</button>
                    </div>
                </div>
                <button onclick="VacuumMonitor.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="vacuum-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('vacuum-db-sel'), () => this.load());
        await this.load();
    },

    switchTab(tab) {
        this._tab = tab;
        document.getElementById('tab-vacuum').style.opacity = tab === 'vacuum' ? '1' : '0.5';
        document.getElementById('tab-bloat').style.opacity = tab === 'bloat' ? '1' : '0.5';
        document.getElementById('tab-repack').style.opacity = tab === 'repack' ? '1' : '0.5';
        if (this._repackTimer) { clearInterval(this._repackTimer); this._repackTimer = null; }
        this.load();
    },

    async load() {
        if (this._tab === 'bloat') return this.loadBloat();
        if (this._tab === 'repack') return this.loadRepack();
        return this.loadVacuum();
    },

    async loadVacuum() {
        try {
            const res = await api('/vacuum/stats' + DbSelector.getParam());
            const stats = res.data || [];
            const el = document.getElementById('vacuum-content');
            if (!el) return;

            if (stats.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no user tables</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs dim mb-8">${stats.length} tables</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
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
            const res = await api('/vacuum/bloat' + DbSelector.getParam());
            const stats = res.data || [];
            const el = document.getElementById('vacuum-content');
            if (!el) return;

            if (stats.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no bloated tables detected — all clean!</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs dim mb-8">${stats.length} tables with dead tuples</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
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

    // ===== pg_repack Tab =====
    async loadRepack() {
        const el = document.getElementById('vacuum-content');
        if (!el) return;

        try {
            const [tablesRes, statusRes] = await Promise.all([
                api('/repack/tables' + DbSelector.getParam()),
                api('/repack/status'),
            ]);

            const tables = tablesRes.data || [];
            const status = statusRes.data || {};

            let statusHtml = '';
            if (status.status === 'running') {
                statusHtml = `
                    <div class="card mb-8" style="border-left:3px solid var(--accent);padding:8px 12px">
                        <div class="flex-between">
                            <div>
                                <span class="mono-xs accent">pg_repack running</span>
                                <span class="mono-xs dim" style="margin-left:8px">${escHtml(status.table)} — ${escHtml(status.elapsed)}</span>
                            </div>
                            <button onclick="VacuumMonitor.cancelRepack()" class="btn btn-sm btn-danger" style="font-size:9px">cancel</button>
                        </div>
                    </div>`;
                if (!this._repackTimer) {
                    this._repackTimer = setInterval(() => this.loadRepack(), 5000);
                }
            } else {
                if (this._repackTimer) { clearInterval(this._repackTimer); this._repackTimer = null; }
            }

            if (tables.length === 0) {
                el.innerHTML = statusHtml + '<div class="card"><span class="dim mono-xs">no user tables</span></div>';
                return;
            }

            el.innerHTML = `${statusHtml}
                <div class="card mb-8" style="padding:8px 12px;background:var(--bg-2);border:1px solid var(--border)">
                    <span class="mono-xs dim">⚠ pg_repack requires ~2x free disk space of the target table. Holds minimal locks but needs temporary space.</span>
                </div>
                <div class="mono-xs dim mb-8">${tables.length} tables</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - ${status.status === 'running' ? '220' : '180'}px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th>table</th>
                        <th style="width:90px">size</th>
                        <th style="width:80px">live</th>
                        <th style="width:80px">dead</th>
                        <th style="width:70px">bloat %</th>
                        <th style="width:100px">last vacuum</th>
                        <th style="width:80px">actions</th>
                    </tr></thead>
                    <tbody>${tables.map(t => {
                        const cls = t.bloatPct > 30 ? 'red' : t.bloatPct > 10 ? 'yellow' : '';
                        const tableFull = t.schema === 'public' ? t.table : t.schema + '.' + t.table;
                        const disabled = status.status === 'running' ? 'disabled style="opacity:0.3;font-size:9px;padding:2px 6px"' : 'style="font-size:9px;padding:2px 6px"';
                        return `<tr>
                            <td>${tableFull}</td>
                            <td>${escHtml(t.totalSize)}</td>
                            <td>${fmtNum(t.liveTuples)}</td>
                            <td class="${cls}">${fmtNum(t.deadTuples)}</td>
                            <td class="${cls}">${t.bloatPct}%</td>
                            <td class="dim">${t.lastVacuum ? timeAgo(t.lastVacuum) : '-'}</td>
                            <td>
                                <button onclick="VacuumMonitor.triggerRepack('${escHtml(t.schema)}','${escHtml(t.table)}')" class="btn btn-sm" ${disabled}>repack</button>
                            </td>
                        </tr>`;
                    }).join('')}</tbody>
                </table></div>`;
        } catch (e) {
            el.innerHTML = '<div class="card"><span class="red mono-xs">failed to load repack data</span></div>';
        }
    },

    async triggerRepack(schema, table) {
        const tableName = schema === 'public' ? table : schema + '.' + table;
        if (!await showConfirm('pg_repack', `Run pg_repack on "${tableName}"?\n\nThis will compact the table online with minimal locks.\nRequires ~2x free disk space of the table.`, { confirmText: 'run pg_repack' })) return;

        try {
            await apiProtected('/repack/run', {
                method: 'POST',
                body: JSON.stringify({ schema, table, database: DbSelector.getSelected() }),
            });
            showToast(`pg_repack started on ${tableName}`, 'success');
            setTimeout(() => this.loadRepack(), 1000);
        } catch (e) {
            showToast(`repack failed: ${e.message}`, 'error');
        }
    },

    async cancelRepack() {
        if (!await showConfirm('cancel repack', 'Cancel the running pg_repack operation?', { danger: true, confirmText: 'cancel' })) return;
        try {
            await apiProtected('/repack/cancel', { method: 'POST' });
            showToast('repack cancelled', 'success');
            if (this._repackTimer) { clearInterval(this._repackTimer); this._repackTimer = null; }
            setTimeout(() => this.loadRepack(), 500);
        } catch (e) {
            showToast(`cancel failed: ${e.message}`, 'error');
        }
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
                body: JSON.stringify({ schema, table, full, database: DbSelector.getSelected() })
            });
            showToast(`${cmd} started on ${tableName}`, 'success');
            setTimeout(() => this.load(), 3000);
        } catch (e) {
            showToast(`vacuum failed: ${e.message}`, 'error');
        }
    },
};
