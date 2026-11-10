// PGAIO — Job Center

const JobsPage = {
    _filters: {
        type: '',
        status: '',
        search: '',
    },

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                    <span class="card-title" style="margin:0">job center</span>
                    <select id="jobs-type" class="db-select">
                        <option value="">all types</option>
                        <option value="backup">backup</option>
                        <option value="restore">restore</option>
                        <option value="export">export</option>
                        <option value="import">import</option>
                        <option value="vacuum">vacuum</option>
                        <option value="repack">repack</option>
                    </select>
                    <select id="jobs-status" class="db-select">
                        <option value="">all status</option>
                        <option value="running">running</option>
                        <option value="succeeded">succeeded</option>
                        <option value="failed">failed</option>
                        <option value="canceled">canceled</option>
                    </select>
                    <input id="jobs-search" type="text" placeholder="search target / db / detail"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);padding:4px 8px;font-size:11px;font-family:var(--font);width:220px">
                </div>
                <button onclick="JobsPage.load()" class="btn btn-sm"><i data-lucide="refresh-cw" class="icon-sm"></i> refresh</button>
            </div>
            <div id="jobs-content"><div class="card"><span class="dim mono-xs">loading...</span></div></div>
        `;
        lucide.createIcons();
        document.getElementById('jobs-type').addEventListener('change', (e) => { this._filters.type = e.target.value; this.load(); });
        document.getElementById('jobs-status').addEventListener('change', (e) => { this._filters.status = e.target.value; this.load(); });
        document.getElementById('jobs-search').addEventListener('input', (e) => { this._filters.search = e.target.value.toLowerCase(); this.load(); });
        await this.load();
    },

    async load() {
        const el = document.getElementById('jobs-content');
        if (!el) return;
        try {
            const query = this._filters.type ? `?type=${encodeURIComponent(this._filters.type)}` : '';
            const res = await api(`/jobs${query}`);
            let jobs = res.data || [];

            if (this._filters.status) jobs = jobs.filter(j => j.status === this._filters.status);
            if (this._filters.search) {
                jobs = jobs.filter(j => [j.target, j.database, j.message, j.details].join(' ').toLowerCase().includes(this._filters.search));
            }

            if (!jobs.length) {
                el.innerHTML = '<div class="card"><span class="dim mono-xs">no jobs match the current filters</span></div>';
                return;
            }

            el.innerHTML = `
                <div class="mono-xs dim mb-8">${jobs.length} job${jobs.length !== 1 ? 's' : ''}</div>
                <div class="card" style="padding:0;overflow-x:auto;height:calc(100vh - 138px);overflow-y:auto">
                    <table class="data-table" style="table-layout:fixed;width:100%">
                        <thead><tr>
                            <th style="width:90px">type</th>
                            <th style="width:90px">status</th>
                            <th>target</th>
                            <th style="width:100px">database</th>
                            <th style="width:120px">started</th>
                            <th style="width:120px">finished</th>
                            <th style="width:110px">artifact</th>
                            <th>details</th>
                        </tr></thead>
                        <tbody>
                            ${jobs.map(job => `
                                <tr>
                                    <td>${escHtml(job.type)}</td>
                                    <td><span class="mono-xs ${jobStatusClass(job.status)}">${escHtml(job.status)}</span></td>
                                    <td title="${escHtml(job.target || '')}">${escHtml(job.target || '-')}</td>
                                    <td>${escHtml(job.database || '-')}</td>
                                    <td class="dim">${job.startedAt ? timeAgo(job.startedAt) : '-'}</td>
                                    <td class="dim">${job.finishedAt ? timeAgo(job.finishedAt) : '-'}</td>
                                    <td>
                                        ${job.artifact ? `<button class="btn btn-sm" style="font-size:9px" onclick="JobUI.download('${job.id}')">download</button>` : '<span class="dim mono-xs">-</span>'}
                                    </td>
                                    <td title="${escHtml(job.details || job.message || '')}" style="white-space:pre-wrap">${escHtml(job.details || job.message || '-')}</td>
                                </tr>
                            `).join('')}
                        </tbody>
                    </table>
                </div>
            `;
        } catch (e) {
            el.innerHTML = `<div class="card"><span class="red mono-xs">${escHtml(e.message)}</span></div>`;
        }
    },
};
