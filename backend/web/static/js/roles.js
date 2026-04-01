// PGAIO — Roles & Privileges

const RolesPage = {
    _data: null,
    _tab: 'roles',
    _search: '',

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                    <span class="card-title" style="margin:0">roles & privileges</span>
                    <div style="display:flex;gap:2px">
                        <button class="btn btn-sm" id="roles-tab-roles" onclick="RolesPage.switchTab('roles')" style="font-size:9px">roles</button>
                        <button class="btn btn-sm" id="roles-tab-memberships" onclick="RolesPage.switchTab('memberships')" style="font-size:9px">memberships</button>
                        <button class="btn btn-sm" id="roles-tab-grants" onclick="RolesPage.switchTab('grants')" style="font-size:9px">grants</button>
                        <button class="btn btn-sm" id="roles-tab-defaults" onclick="RolesPage.switchTab('defaults')" style="font-size:9px">default privs</button>
                    </div>
                    <input id="roles-search" type="text" placeholder="search role / object / privilege"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);padding:4px 8px;font-size:11px;font-family:var(--font);width:240px">
                </div>
                <button onclick="RolesPage.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="roles-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        document.getElementById('roles-search').addEventListener('input', (e) => {
            this._search = e.target.value.toLowerCase();
            this.renderCurrentTab();
        });
        await this.load();
    },

    switchTab(tab) {
        this._tab = tab;
        ['roles', 'memberships', 'grants', 'defaults'].forEach(name => {
            const btn = document.getElementById(`roles-tab-${name}`);
            if (btn) btn.style.opacity = name === tab ? '1' : '0.5';
        });
        this.renderCurrentTab();
    },

    async load() {
        const el = document.getElementById('roles-content');
        if (!el) return;
        try {
            const res = await api('/roles/overview');
            this._data = res.data || { roles: [], memberships: [], grants: [], defaultPrivileges: [] };
            this.switchTab(this._tab);
        } catch (e) {
            el.innerHTML = `<div class="card"><span class="red mono-xs">${escHtml(e.message)}</span></div>`;
        }
    },

    renderCurrentTab() {
        const el = document.getElementById('roles-content');
        if (!el || !this._data) return;
        if (this._tab === 'memberships') return this.renderMemberships(el);
        if (this._tab === 'grants') return this.renderGrants(el, this._data.grants || []);
        if (this._tab === 'defaults') return this.renderGrants(el, this._data.defaultPrivileges || []);
        return this.renderRoles(el);
    },

    renderRoles(el) {
        let roles = this._data.roles || [];
        if (this._search) {
            roles = roles.filter(role => [role.name, ...(role.memberOf || [])].join(' ').toLowerCase().includes(this._search));
        }
        if (!roles.length) {
            el.innerHTML = '<div class="card"><span class="dim mono-xs">no roles match the current filters</span></div>';
            return;
        }
        el.innerHTML = `
            <div class="mono-xs dim mb-8">${roles.length} roles</div>
            <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th style="width:160px">role</th>
                        <th style="width:60px">login</th>
                        <th style="width:60px">super</th>
                        <th style="width:60px">db</th>
                        <th style="width:60px">role</th>
                        <th style="width:60px">repl</th>
                        <th style="width:60px">bypass</th>
                        <th style="width:80px">conn limit</th>
                        <th>member of</th>
                    </tr></thead>
                    <tbody>
                        ${roles.map(role => `
                            <tr>
                                <td>${escHtml(role.name)}</td>
                                <td class="${role.canLogin ? 'green' : 'dim'}">${role.canLogin ? 'yes' : 'no'}</td>
                                <td class="${role.superuser ? 'red' : 'dim'}">${role.superuser ? 'yes' : 'no'}</td>
                                <td class="${role.createDb ? 'yellow' : 'dim'}">${role.createDb ? 'yes' : 'no'}</td>
                                <td class="${role.createRole ? 'yellow' : 'dim'}">${role.createRole ? 'yes' : 'no'}</td>
                                <td class="${role.replication ? 'accent' : 'dim'}">${role.replication ? 'yes' : 'no'}</td>
                                <td class="${role.bypassRls ? 'red' : 'dim'}">${role.bypassRls ? 'yes' : 'no'}</td>
                                <td>${role.connLimit}</td>
                                <td>${(role.memberOf || []).length ? role.memberOf.map(escHtml).join(', ') : '<span class="dim mono-xs">-</span>'}</td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    },

    renderMemberships(el) {
        let memberships = this._data.memberships || [];
        if (this._search) {
            memberships = memberships.filter(m => [m.member, m.role].join(' ').toLowerCase().includes(this._search));
        }
        if (!memberships.length) {
            el.innerHTML = '<div class="card"><span class="dim mono-xs">no memberships match the current filters</span></div>';
            return;
        }
        el.innerHTML = `
            <div class="mono-xs dim mb-8">${memberships.length} membership link${memberships.length !== 1 ? 's' : ''}</div>
            <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr><th>member</th><th>parent role</th><th style="width:100px">admin option</th></tr></thead>
                    <tbody>
                        ${memberships.map(m => `
                            <tr>
                                <td>${escHtml(m.member)}</td>
                                <td>${escHtml(m.role)}</td>
                                <td class="${m.adminOption ? 'yellow' : 'dim'}">${m.adminOption ? 'yes' : 'no'}</td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    },

    renderGrants(el, grants) {
        let filtered = grants || [];
        if (this._search) {
            filtered = filtered.filter(g => [g.grantee, g.objectType, g.database, g.schema, g.objectName, g.privilege].join(' ').toLowerCase().includes(this._search));
        }
        if (!filtered.length) {
            el.innerHTML = '<div class="card"><span class="dim mono-xs">no grants match the current filters</span></div>';
            return;
        }
        el.innerHTML = `
            <div class="mono-xs dim mb-8">${filtered.length} grant row${filtered.length !== 1 ? 's' : ''}</div>
            <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
                <table class="data-table" style="table-layout:fixed;width:100%">
                    <thead><tr>
                        <th style="width:140px">grantee</th>
                        <th style="width:130px">object type</th>
                        <th style="width:120px">database</th>
                        <th style="width:120px">schema</th>
                        <th>object</th>
                        <th style="width:120px">privilege</th>
                        <th style="width:100px">grantable</th>
                    </tr></thead>
                    <tbody>
                        ${filtered.map(g => `
                            <tr>
                                <td>${escHtml(g.grantee || '-')}</td>
                                <td>${escHtml(g.objectType || '-')}</td>
                                <td>${escHtml(g.database || '-')}</td>
                                <td>${escHtml(g.schema || '-')}</td>
                                <td>${escHtml(g.objectName || '-')}</td>
                                <td>${escHtml(g.privilege || '-')}</td>
                                <td class="${g.isGrantable ? 'green' : 'dim'}">${g.isGrantable ? 'yes' : 'no'}</td>
                            </tr>
                        `).join('')}
                    </tbody>
                </table>
            </div>
        `;
    },
};
