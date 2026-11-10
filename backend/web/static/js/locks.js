// PGAIO — Lock Monitor

const LockMonitor = {
    _tab: 'conflicts',

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">lock monitor</span>
                    <div style="display:flex;gap:2px">
                        <button onclick="LockMonitor.switchTab('conflicts')" class="btn btn-sm" id="locks-tab-conflicts" style="font-size:9px">conflicts</button>
                        <button onclick="LockMonitor.switchTab('tree')" class="btn btn-sm" id="locks-tab-tree" style="font-size:9px">blocking tree</button>
                    </div>
                </div>
                <button onclick="LockMonitor.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="locks-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    switchTab(tab) {
        this._tab = tab;
        document.getElementById('locks-tab-conflicts').style.opacity = tab === 'conflicts' ? '1' : '0.5';
        document.getElementById('locks-tab-tree').style.opacity = tab === 'tree' ? '1' : '0.5';
        this.load();
    },

    async load() {
        if (this._tab === 'tree') return this.loadTree();
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

    async loadTree() {
        const el = document.getElementById('locks-content');
        if (!el) return;
        try {
            const res = await api('/locks/tree');
            const roots = res.data?.roots || [];
            if (!roots.length) {
                el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no blocking tree to render</span></div>';
                return;
            }

            el.innerHTML = `
                <div class="mono-xs yellow mb-8">blocking chains from root blockers to waiting sessions</div>
                <div class="grid" style="gap:12px">${roots.map(root => this.renderNode(root, null)).join('')}</div>
            `;
        } catch (e) {
            el.innerHTML = `<div class="card"><span class="red mono-xs">${escHtml(e.message)}</span></div>`;
        }
    },

    renderNode(node, edge) {
        const edgeInfo = edge ? `
            <div class="mono-xs dim" style="margin:4px 0 8px">
                waits on ${escHtml(edge.lockType || 'lock')}${edge.relationName ? ` · relation ${escHtml(edge.relationName)}` : ''}${edge.duration ? ` · ${escHtml(edge.duration)}` : ''}
            </div>
        ` : '';
        return `
            <div class="card" style="padding:10px 12px">
                ${edgeInfo}
                <div class="flex-between" style="align-items:flex-start;gap:12px">
                    <div>
                        <div>
                            <span class="accent mono-xs">pid ${node.pid}</span>
                            <span class="dim mono-xs">${escHtml(node.user || '-')} @ ${escHtml(node.database || '-')}</span>
                        </div>
                        <div class="mono-xs dim" style="margin-top:4px">
                            ${escHtml(node.state || 'blocking')}
                            ${node.waitEventType ? ` · ${escHtml(node.waitEventType)}` : ''}
                            ${node.waitEvent ? `/${escHtml(node.waitEvent)}` : ''}
                            ${node.blockedDuration ? ` · ${escHtml(node.blockedDuration)}` : ''}
                        </div>
                        <div style="margin-top:6px;white-space:pre-wrap">${escHtml(node.query || '-')}</div>
                    </div>
                    <button onclick="LockMonitor.killBlocker(${node.pid})" class="btn btn-sm" style="font-size:9px">kill ${node.pid}</button>
                </div>
                ${node.children && node.children.length ? `
                    <div style="margin-top:10px;padding-left:14px;border-left:1px solid var(--border);display:grid;gap:10px">
                        ${node.children.map((child, idx) => this.renderNode(child, node.edges ? node.edges[idx] : null)).join('')}
                    </div>
                ` : ''}
            </div>
        `;
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
