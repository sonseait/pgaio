// PGAIO — Slow Queries + Explain Analyze

const SlowQueries = {
    _savedPlans: [],
    _compare: [],

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">slow queries</span>
                    <div id="queries-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                    <div id="queries-profile-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                </div>
                <div style="display:flex;gap:6px">
                    <button onclick="SlowQueries.resetStats()" class="btn btn-sm btn-danger" title="Reset statistics">
                        <i data-lucide="trash-2" class="icon-sm"></i> reset
                    </button>
                    <button onclick="SlowQueries.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
                </div>
            </div>
            <div id="queries-plans" class="mb-8"></div>
            <div id="queries-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('queries-db-sel'), () => this.load());
        await ProfileSelector.renderInto(document.getElementById('queries-profile-sel'), 'queries', () => this.load());
        await this.load();
    },

    async load() {
        try {
            const params = new URLSearchParams();
            if (DbSelector.getSelected()) params.set('database', DbSelector.getSelected());
            const profile = await ProfileSelector.ensureSelected('queries');
            if (profile) params.set('profile', profile);
            const [res, plansRes] = await Promise.all([
                api(`/queries/slow?${params.toString()}`),
                api('/queries/plans'),
            ]);
            const d = res.data;
            this._savedPlans = plansRes.data || [];
            const el = document.getElementById('queries-content');
            if (!el) return;
            this.renderSavedPlans();

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
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
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
    renderSavedPlans() {
        const el = document.getElementById('queries-plans');
        if (!el) return;
        if (!this._savedPlans.length) {
            el.innerHTML = '';
            return;
        }
        el.innerHTML = `
            <div class="card">
                <div class="flex-between" style="margin-bottom:8px">
                    <span class="card-title" style="margin:0">saved plans</span>
                    <button class="btn btn-sm" style="font-size:9px" onclick="SlowQueries.compareSelected()">compare selected</button>
                </div>
                <div style="display:grid;gap:8px">
                    ${this._savedPlans.slice(0, 8).map(plan => `
                        <label class="mono-xs" style="display:flex;align-items:flex-start;gap:8px">
                            <input type="checkbox" ${this._compare.includes(plan.id) ? 'checked' : ''} onchange="SlowQueries.toggleCompare('${plan.id}', this.checked)">
                            <span>
                                <span class="${plan.mode === 'analyzed' ? 'green' : 'yellow'}">${escHtml(plan.name || plan.id)}</span>
                                <span class="dim">${escHtml(plan.database || '-')} · ${escHtml(plan.profile || 'direct')} · ${timeAgo(plan.createdAt)}</span>
                            </span>
                        </label>
                    `).join('')}
                </div>
            </div>
        `;
    },

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
                    <button class="btn btn-sm btn-primary" id="explain-save-btn">save plan</button>
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
            const profile = await ProfileSelector.ensureSelected('queries');
            const res = await apiProtected('/queries/explain', {
                method: 'POST',
                body: JSON.stringify({ query: q.query, database: DbSelector.getSelected(), profile })
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
            document.getElementById('explain-save-btn').addEventListener('click', async () => {
                try {
                    const saveRes = await apiProtected('/queries/explain', {
                        method: 'POST',
                        body: JSON.stringify({
                            query: q.query,
                            database: DbSelector.getSelected(),
                            profile,
                            save: true,
                            name: q.query.slice(0, 60),
                        })
                    });
                    showToast(`plan saved as ${saveRes.data?.planId}`, 'success');
                    await this.load();
                } catch (e) {
                    showToast(`save failed: ${e.message}`, 'error');
                }
            });
        } catch (e) {
            const resultEl = document.getElementById('explain-result');
            if (resultEl) resultEl.innerHTML = `<span class="red mono-xs">error: ${escHtml(e.message)}</span>`;
        }
    },

    async resetStats() {
        if (!await showConfirm('reset statistics', 'Reset pg_stat_statements? This clears all query statistics.', { danger: true, confirmText: 'reset' })) return;
        try {
            const params = new URLSearchParams();
            if (DbSelector.getSelected()) params.set('database', DbSelector.getSelected());
            const profile = await ProfileSelector.ensureSelected('queries');
            if (profile) params.set('profile', profile);
            await apiProtected('/queries/reset?' + params.toString(), { method: 'POST' });
            showToast('statistics reset', 'success');
            await this.load();
        } catch (e) { /* handled */ }
    },

    toggleCompare(id, checked) {
        if (checked) {
            if (this._compare.length >= 2) this._compare.shift();
            this._compare.push(id);
        } else {
            this._compare = this._compare.filter(x => x !== id);
        }
        this.renderSavedPlans();
    },

    async compareSelected() {
        if (this._compare.length !== 2) {
            showToast('select exactly two saved plans', 'error');
            return;
        }
        const [a, b] = await Promise.all(this._compare.map(id => api(`/queries/plans/${encodeURIComponent(id)}`)));
        const left = a.data;
        const right = b.data;

        document.getElementById('plan-compare-modal')?.remove();
        const overlay = document.createElement('div');
        overlay.id = 'plan-compare-modal';
        overlay.className = 'modal-overlay';
        overlay.innerHTML = `
            <div class="modal-dialog" style="width:920px;max-height:84vh">
                <div class="modal-header">
                    <span class="modal-title">compare saved plans</span>
                    <button class="modal-close" id="plan-compare-close">&times;</button>
                </div>
                <div class="modal-body" style="max-height:70vh;overflow:auto">
                    <div class="grid grid-2" style="gap:12px">
                        ${[left, right].map(plan => `
                            <div class="card">
                                <div class="mono-xs ${plan.mode === 'analyzed' ? 'green' : 'yellow'}" style="margin-bottom:6px">${escHtml(plan.name || plan.id)}</div>
                                <div class="mono-xs dim" style="margin-bottom:8px">${escHtml(plan.database || '-')} · ${escHtml(plan.profile || 'direct')} · ${timeAgo(plan.createdAt)}</div>
                                <div class="mono-xs dim" style="margin-bottom:8px;white-space:pre-wrap">${escHtml(plan.query)}</div>
                                <pre class="mono-xs" style="background:var(--bg-0);border:1px solid var(--border);padding:8px;border-radius:4px;white-space:pre-wrap">${escHtml(JSON.stringify(plan.plan, null, 2))}</pre>
                            </div>
                        `).join('')}
                    </div>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);
        document.getElementById('plan-compare-close').addEventListener('click', () => overlay.remove());
        overlay.addEventListener('click', (e) => { if (e.target === overlay) overlay.remove(); });
    },
};
