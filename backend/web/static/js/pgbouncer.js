// PGAIO — PgBouncer Module

const PgBouncerUI = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-12">
                <span class="card-title" style="margin:0">pgbouncer control</span>
                <div class="flex gap-4">
                    <button onclick="PgBouncerUI.action('reload')" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> reload</button>
                    <button onclick="PgBouncerUI.action('pause')" class="btn btn-sm"><i data-lucide="pause" class="icon-sm"></i> pause</button>
                    <button onclick="PgBouncerUI.action('resume')" class="btn btn-sm"><i data-lucide="play" class="icon-sm"></i> resume</button>
                    <button onclick="PgBouncerUI.action('kill')" class="btn btn-sm btn-danger"><i data-lucide="x" class="icon-sm"></i> kill</button>
                </div>
            </div>
            <div class="grid grid-2 mb-12">
                <div class="card">
                    <div class="card-title">pool stats</div>
                    <div id="pgb-stats" class="dim text-center py-16">loading...</div>
                </div>
                <div class="card">
                    <div class="card-title">server stats</div>
                    <div id="pgb-servers" class="dim text-center py-16">loading...</div>
                </div>
            </div>
        `;
        lucide.createIcons();
        this.loadStats();
    },

    async loadStats() {
        try {
            const res = await api('/pgbouncer/stats');
            const data = res.data || {};
            this.renderPoolStats(data.pools || []);
            this.renderServerStats(data.stats || []);
        } catch (e) {
            document.getElementById('pgb-stats').innerHTML = `<div class="dim text-center py-16">error: ${escHtml(e.message)}</div>`;
        }
    },

    renderPoolStats(pools) {
        const el = document.getElementById('pgb-stats');
        if (!el) return;
        if (!pools.length) { el.innerHTML = '<div class="dim text-center py-16">no pools</div>'; return; }

        el.innerHTML = `<div style="overflow-x:auto"><table>
            <colgroup>
                <col style="width:22%"><col style="width:18%">
                <col style="width:12%"><col style="width:12%">
                <col style="width:12%"><col style="width:12%"><col style="width:12%">
            </colgroup>
            <thead><tr><th>db</th><th>user</th><th>cl_active</th><th>cl_waiting</th><th>sv_active</th><th>sv_idle</th><th>mode</th></tr></thead>
            <tbody>${pools.map(p => `<tr>
                <td>${p.database}</td>
                <td>${p.user}</td>
                <td class="accent">${p.clActive}</td>
                <td class="${p.clWaiting > 0 ? 'yellow' : ''}">${p.clWaiting}</td>
                <td class="green">${p.svActive}</td>
                <td>${p.svIdle}</td>
                <td><span class="badge badge-gray">${p.poolMode}</span></td>
            </tr>`).join('')}</tbody>
        </table></div>`;
    },

    renderServerStats(stats) {
        const el = document.getElementById('pgb-servers');
        if (!el) return;
        if (!stats.length) { el.innerHTML = '<div class="dim text-center py-16">no stats</div>'; return; }

        el.innerHTML = `<div style="overflow-x:auto"><table>
            <colgroup>
                <col style="width:20%"><col style="width:14%">
                <col style="width:14%"><col style="width:18%">
                <col style="width:18%"><col style="width:16%">
            </colgroup>
            <thead><tr><th>db</th><th>xacts</th><th>queries</th><th>bytes_recv</th><th>bytes_sent</th><th>query_time</th></tr></thead>
            <tbody>${stats.map(s => `<tr>
                <td>${s.database}</td>
                <td>${fmtNum(s.totalXactCount)}</td>
                <td>${fmtNum(s.totalQueryCount)}</td>
                <td>${formatBytes(s.totalReceived)}</td>
                <td>${formatBytes(s.totalSent)}</td>
                <td>${s.avgQueryTime || 0}µs</td>
            </tr>`).join('')}</tbody>
        </table></div>`;
    },

    async action(act) {
        if (act === 'kill' && !await showConfirm('kill connections', 'Kill all PgBouncer connections?', { danger: true, confirmText: 'kill all' })) return;
        try {
            await apiProtected(`/pgbouncer/${act}`, { method: 'POST' });
            showToast(`pgbouncer ${act} ok`, 'success');
            setTimeout(() => this.loadStats(), 1000);
        } catch (e) { /* handled */ }
    },
};
