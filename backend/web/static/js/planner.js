// PGAIO — Maintenance Planner

const MaintenancePlanner = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                    <span class="card-title" style="margin:0">maintenance planner</span>
                    <div id="planner-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                    <div id="planner-profile-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                </div>
                <button onclick="MaintenancePlanner.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="planner-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('planner-db-sel'), () => this.load());
        await ProfileSelector.renderInto(document.getElementById('planner-profile-sel'), 'maintenance', () => this.load());
        await this.load();
    },

    async load() {
        const el = document.getElementById('planner-content');
        if (!el) return;
        try {
            const params = new URLSearchParams();
            const db = DbSelector.getSelected();
            const profile = await ProfileSelector.ensureSelected('maintenance');
            if (db) params.set('database', db);
            if (profile) params.set('profile', profile);

            const res = await api(`/maintenance/plan?${params.toString()}`);
            const recommendations = res.data?.recommendations || [];
            if (!recommendations.length) {
                el.innerHTML = '<div class="card"><span class="green mono-xs">✓ no immediate maintenance recommendations</span></div>';
                return;
            }

            el.innerHTML = `
                <div class="mono-xs dim mb-8">${recommendations.length} recommendation${recommendations.length !== 1 ? 's' : ''}</div>
                <div class="grid" style="gap:12px">
                    ${recommendations.map(rec => `
                        <div class="card" style="padding:10px 12px">
                            <div class="flex-between" style="margin-bottom:6px">
                                <div>
                                    <span class="mono-xs ${rec.priority === 'high' ? 'red' : rec.priority === 'medium' ? 'yellow' : 'green'}">${escHtml(rec.priority)}</span>
                                    <span class="mono-xs dim">${escHtml(rec.category)}</span>
                                </div>
                                <button class="btn btn-sm" style="font-size:9px" onclick="MaintenancePlanner.sendToSql('${encodeURIComponent(rec.sql)}')">use in sql</button>
                            </div>
                            <div style="font-weight:600">${escHtml(rec.object)}</div>
                            <div class="mono-xs accent" style="margin:4px 0 6px">${escHtml(rec.action)}</div>
                            <div class="mono-xs dim" style="margin-bottom:8px">${escHtml(rec.reason)}</div>
                            <pre class="mono-xs" style="background:var(--bg-0);border:1px solid var(--border);padding:8px;border-radius:4px;white-space:pre-wrap">${escHtml(rec.sql)}</pre>
                        </div>
                    `).join('')}
                </div>
            `;
        } catch (e) {
            el.innerHTML = `<div class="card"><span class="red mono-xs">${escHtml(e.message)}</span></div>`;
        }
    },

    sendToSql(sql) {
        sessionStorage.setItem('pgaio_sql_draft', decodeURIComponent(sql));
        location.hash = '#sql';
        navigate();
    },
};
