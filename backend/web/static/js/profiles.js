// PGAIO — Connection Profiles

const ProfilesPage = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">connection profiles</span>
                <div style="display:flex;gap:6px">
                    <button onclick="ProfilesPage.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
                    <button onclick="ProfilesPage.save()" class="btn btn-sm btn-primary"><i data-lucide="save" class="icon-sm"></i> save</button>
                </div>
            </div>
            <div id="profiles-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        await this.load();
    },

    async load() {
        const el = document.getElementById('profiles-content');
        if (!el) return;
        try {
            const res = await api('/settings');
            this._config = res.data || {};
            const profiles = this._config.connections?.profiles || [];
            const routes = this._config.connections?.featureRoutes || {};
            el.innerHTML = `
                <div class="card mb-8">
                    <div class="card-title">feature routing</div>
                    <div class="grid grid-3" style="gap:12px">
                        ${['sql', 'queries', 'maintenance'].map(feature => `
                            <div>
                                <label class="mono-xs dim" style="display:block;margin-bottom:4px">${feature}</label>
                                <select id="route-${feature}" class="db-select" style="width:100%">
                                    ${profiles.map(profile => `<option value="${escHtml(profile.name)}" ${routes[feature] === profile.name ? 'selected' : ''}>${escHtml(profile.label || profile.name)}</option>`).join('')}
                                </select>
                            </div>
                        `).join('')}
                    </div>
                </div>
                <div class="grid" style="gap:12px">
                    ${profiles.map((profile, idx) => `
                        <div class="card">
                            <div class="flex-between" style="margin-bottom:8px">
                                <span class="card-title" style="margin:0">${escHtml(profile.label || profile.name)}</span>
                                <span class="mono-xs ${profile.type === 'pgbouncer' ? 'yellow' : 'green'}">${escHtml(profile.type)}</span>
                            </div>
                            <div class="grid grid-2" style="gap:10px">
                                ${this.input(`profile-label-${idx}`, 'label', profile.label || profile.name)}
                                ${this.input(`profile-host-${idx}`, 'host', profile.host || '')}
                                ${this.input(`profile-port-${idx}`, 'port', profile.port || 0, 'number')}
                                ${this.input(`profile-db-${idx}`, 'database', profile.database || '')}
                                ${this.input(`profile-ssl-${idx}`, 'ssl mode', profile.sslMode || 'disable')}
                                ${this.input(`profile-desc-${idx}`, 'description', profile.description || '')}
                            </div>
                            <div class="mono-xs dim" style="margin-top:8px">name: ${escHtml(profile.name)}</div>
                        </div>
                    `).join('')}
                </div>
            `;
        } catch (e) {
            el.innerHTML = `<div class="card"><span class="red mono-xs">${escHtml(e.message)}</span></div>`;
        }
    },

    input(id, label, value, type = 'text') {
        return `
            <div>
                <label class="mono-xs dim" style="display:block;margin-bottom:4px">${label}</label>
                <input id="${id}" type="${type}" value="${escHtml(String(value ?? ''))}"
                    style="width:100%;background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);padding:6px 8px;font-size:11px;font-family:var(--font)">
            </div>
        `;
    },

    async save() {
        if (!this._config) return;
        const cfg = structuredClone(this._config);
        cfg.connections = cfg.connections || {};
        cfg.connections.featureRoutes = cfg.connections.featureRoutes || {};
        ['sql', 'queries', 'maintenance'].forEach(feature => {
            cfg.connections.featureRoutes[feature] = document.getElementById(`route-${feature}`)?.value || cfg.connections.featureRoutes[feature];
        });
        (cfg.connections.profiles || []).forEach((profile, idx) => {
            profile.label = document.getElementById(`profile-label-${idx}`)?.value || profile.label;
            profile.host = document.getElementById(`profile-host-${idx}`)?.value || profile.host;
            profile.port = parseInt(document.getElementById(`profile-port-${idx}`)?.value || profile.port, 10) || profile.port;
            profile.database = document.getElementById(`profile-db-${idx}`)?.value || profile.database;
            profile.sslMode = document.getElementById(`profile-ssl-${idx}`)?.value || profile.sslMode;
            profile.description = document.getElementById(`profile-desc-${idx}`)?.value || profile.description;
        });

        try {
            await apiProtected('/settings', { method: 'POST', body: JSON.stringify(cfg) });
            ProfileSelector.resetCache();
            showToast('connection profiles updated', 'success');
            await this.load();
        } catch (e) {
            showToast(`save failed: ${e.message}`, 'error');
        }
    },
};
