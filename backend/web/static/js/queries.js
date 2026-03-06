// PGAIO — Slow Queries + Explain Analyze

const SlowQueries = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">slow queries</span>
                <div style="display:flex;gap:6px">
                    <button onclick="SlowQueries.resetStats()" class="btn btn-sm btn-danger" title="Reset statistics">
                        <i data-lucide="trash-2" class="icon-sm"></i> reset
                    </button>
                    <button onclick="SlowQueries.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
                </div>
            </div>
            <div id="queries-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    async load() {
        try {
            const res = await api('/queries/slow');
            const d = res.data;
            const el = document.getElementById('queries-content');
            if (!el) return;

            if (!d.available) {
                el.innerHTML = `<div class="card"><span class="yellow mono-xs">⚠ ${d.message}</span></div>`;
                return;
            }

            const queries = d.queries || [];
            if (queries.length === 0) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no slow queries recorded</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs dim mb-8">${queries.length} queries</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 137px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th>query</th>
                        <th style="width:80px">calls</th>
                        <th style="width:90px">avg (ms)</th>
                        <th style="width:100px">total (ms)</th>
                        <th style="width:80px">rows</th>
                        <th style="width:80px">hit ratio</th>
                        <th style="width:80px">actions</th>
                    </tr></thead>
                    <tbody>${queries.map((q, i) => {
                        const hitRatio = q.sharedBlksHit + q.sharedBlksRead > 0
                            ? ((q.sharedBlksHit / (q.sharedBlksHit + q.sharedBlksRead)) * 100).toFixed(1) + '%'
                            : '-';
                        const isSelect = q.query.trim().toUpperCase().startsWith('SELECT') || q.query.trim().toUpperCase().startsWith('WITH');
                        return `<tr>
                            <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(q.query)}">${escHtml(q.query)}</td>
                            <td>${fmtNum(q.calls)}</td>
                            <td class="${q.meanMs > 100 ? 'red' : q.meanMs > 10 ? 'yellow' : ''}">${q.meanMs.toFixed(2)}</td>
                            <td>${q.totalMs.toFixed(0)}</td>
                            <td>${fmtNum(q.rows)}</td>
                            <td>${hitRatio}</td>
                            <td>${isSelect ? `<button onclick="SlowQueries.explain(${i})" class="btn btn-sm" title="Explain Analyze">explain</button>` : '<span class="dim">-</span>'}</td>
                        </tr>`;
                    }).join('')}</tbody>
                </table></div>`;

            // Store queries for explain reference
            this._queries = queries;
        } catch (e) { /* handled */ }
    },

    _queries: [],

    async explain(index) {
        const q = this._queries[index];
        if (!q) return;

        // Show modal with explain result
        document.getElementById('explain-modal')?.remove();
        const overlay = document.createElement('div');
        overlay.id = 'explain-modal';
        overlay.className = 'modal-overlay';
        overlay.innerHTML = `
            <div class="modal-dialog" style="width:700px;max-height:80vh">
                <div class="modal-header">
                    <span class="modal-title">explain analyze</span>
                    <button class="modal-close" id="explain-close">&times;</button>
                </div>
                <div class="modal-body" style="overflow:auto;max-height:60vh">
                    <div class="mono-xs dim mb-8" style="word-break:break-all">${escHtml(q.query)}</div>
                    <div id="explain-result" style="margin-top:8px">
                        <span class="dim mono-xs">running EXPLAIN...</span>
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-sm" id="explain-close-btn">close</button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);

        const cleanup = () => overlay.remove();
        document.getElementById('explain-close').addEventListener('click', cleanup);
        document.getElementById('explain-close-btn').addEventListener('click', cleanup);
        overlay.addEventListener('click', (e) => { if (e.target === overlay) cleanup(); });

        try {
            const res = await apiProtected('/queries/explain', {
                method: 'POST',
                body: JSON.stringify({ query: q.query })
            });
            const { mode, plan } = res.data || {};
            const resultEl = document.getElementById('explain-result');
            if (resultEl && plan) {
                const modeBadge = mode === 'estimated'
                    ? '<span class="badge badge-yellow" style="margin-bottom:8px;display:inline-block">estimated plan (parameterized query)</span>'
                    : '<span class="badge badge-green" style="margin-bottom:8px;display:inline-block">analyzed (actual execution)</span>';
                const planStr = JSON.stringify(plan, null, 2);
                resultEl.innerHTML = `${modeBadge}<pre class="mono-xs" style="background:var(--bg-0);border:1px solid var(--border);
                    padding:12px;border-radius:4px;overflow:auto;max-height:400px;white-space:pre-wrap;
                    font-size:10px;line-height:1.5">${escHtml(planStr)}</pre>`;
            }
        } catch (e) {
            const resultEl = document.getElementById('explain-result');
            if (resultEl) resultEl.innerHTML = `<span class="red mono-xs">error: ${escHtml(e.message)}</span>`;
        }
    },

    async resetStats() {
        if (!await showConfirm('reset statistics', 'Reset pg_stat_statements? This clears all query statistics.', { danger: true, confirmText: 'reset' })) return;
        try {
            await apiProtected('/queries/reset', { method: 'POST' });
            showToast('statistics reset', 'success');
            await this.load();
        } catch (e) { /* handled */ }
    },
};
