// PGAIO — Lock Monitor

const LockMonitor = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">lock monitor</span>
                <button onclick="LockMonitor.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="locks-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    async load() {
        try {
            const res = await api('/locks');
            const locks = res.data || [];
            const el = document.getElementById('locks-content');
            if (!el) return;

            if (locks.length === 0) {
                el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no lock conflicts detected</span></div>';
                return;
            }

            el.innerHTML = `<div class="mono-xs yellow mb-8">⚠ ${locks.length} lock conflict(s)</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 130px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th style="width:100px">blocker</th>
                        <th>blocking query</th>
                        <th style="width:100px">blocked</th>
                        <th>blocked query</th>
                        <th style="width:80px">action</th>
                    </tr></thead>
                    <tbody>${locks.map(l => `<tr>
                        <td class="red">${l.blockingPid} <span class="dim">(${escHtml(l.blockingUser)})</span></td>
                        <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(l.blockingQuery)}">${escHtml(l.blockingQuery)}</td>
                        <td>${l.blockedPid} <span class="dim">(${escHtml(l.blockedUser)})</span></td>
                        <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${escHtml(l.blockedQuery)}">${escHtml(l.blockedQuery)}</td>
                        <td><button onclick="LockMonitor.killBlocker(${l.blockingPid})" class="btn btn-sm" style="font-size:9px">kill ${l.blockingPid}</button></td>
                    </tr>`).join('')}</tbody>
                </table></div>`;
        } catch (e) { /* handled */ }
    },

    async killBlocker(pid) {
        if (!await showConfirm('terminate process', `Terminate PID ${pid}?`, { danger: true, confirmText: 'terminate' })) return;
        try {
            await apiProtected('/dashboard/terminate/' + pid, { method: 'POST' });
            showToast('PID ' + pid + ' terminated', 'success');
            setTimeout(() => this.load(), 1000);
        } catch (e) { /* handled */ }
    },
};
