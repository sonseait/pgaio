// PGAIO — SQL Editor with Query History

const SQLEditor = {
    _snippets: [],
    _showHistory: false,

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <div style="display:flex;gap:8px;align-items:center">
                    <span class="card-title" style="margin:0">sql editor</span>
                    <div id="sql-db-sel" class="db-bar" style="display:inline-flex;margin:0"></div>
                </div>
                <div class="flex gap-4">
                    <button onclick="SQLEditor.toggleHistory()" class="btn btn-sm" id="btn-history">
                        <i data-lucide="history" class="icon-sm"></i> history
                    </button>
                    <select id="sql-snippets"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:4px 8px;font-size:11px;font-family:var(--font);width:200px">
                        <option value="">-- snippets --</option>
                    </select>
                    <button onclick="SQLEditor.execute()" class="btn btn-sm" id="sql-run-btn">
                        <i data-lucide="play" class="icon-sm"></i> run
                    </button>
                </div>
            </div>

            <div id="sql-history-panel" style="display:none;margin-bottom:8px">
                <div class="card" style="padding:0;max-height:200px;overflow-y:auto">
                    <div class="flex-between" style="padding:6px 10px;background:var(--bg-2);border-bottom:1px solid var(--border)">
                        <span class="mono-xs accent">query history</span>
                        <button onclick="SQLEditor.clearHistory()" class="btn btn-sm" style="font-size:9px">clear</button>
                    </div>
                    <div id="history-list"><span class="dim mono-xs" style="padding:8px;display:block">loading...</span></div>
                </div>
            </div>

            <div class="card mb-8" style="padding:0">
                <textarea id="sql-input" spellcheck="false" style="
                    width:100%;min-height:120px;max-height:300px;resize:vertical;
                    background:var(--bg-1);color:var(--text-1);border:none;
                    padding:10px;font-size:12px;line-height:1.5;
                    font-family:var(--font);outline:none;box-sizing:border-box;
                " placeholder="SELECT * FROM pg_stat_activity LIMIT 10;"></textarea>
            </div>

            <div id="sql-status" class="mono-xs dim mb-8" style="display:none"></div>
            <div id="sql-result"></div>
        `;

        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('sql-db-sel'), () => {});

        document.getElementById('sql-input').addEventListener('keydown', (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault();
                this.execute();
            }
            if (e.key === 'Tab') {
                e.preventDefault();
                const ta = e.target;
                const start = ta.selectionStart;
                ta.value = ta.value.substring(0, start) + '  ' + ta.value.substring(ta.selectionEnd);
                ta.selectionStart = ta.selectionEnd = start + 2;
            }
        });

        document.getElementById('sql-snippets').addEventListener('change', (e) => {
            if (e.target.value) {
                document.getElementById('sql-input').value = e.target.value;
                e.target.selectedIndex = 0;
            }
        });

        await this.loadSnippets();
    },

    // ===== History =====
    async toggleHistory() {
        this._showHistory = !this._showHistory;
        const panel = document.getElementById('sql-history-panel');
        if (!panel) return;
        panel.style.display = this._showHistory ? 'block' : 'none';
        if (this._showHistory) await this.loadHistory();
    },

    async loadHistory() {
        try {
            const res = await api('/sql/history');
            const list = res.data || [];
            const el = document.getElementById('history-list');
            if (!el) return;

            if (list.length === 0) {
                el.innerHTML = '<span class="dim mono-xs" style="padding:8px;display:block">no history</span>';
                return;
            }

            el.innerHTML = `<table class="data-table" style="table-layout:fixed;width:100%">
                <thead><tr>
                    <th>query</th>
                    <th style="width:100px">time</th>
                    <th style="width:80px">duration</th>
                    <th style="width:60px">rows</th>
                    <th style="width:60px">status</th>
                </tr></thead>
                <tbody>${list.slice().reverse().map(h => `<tr style="cursor:pointer" onclick="SQLEditor.useHistory(this)" data-query="${this.esc(h.query)}">
                    <td style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap" title="${this.esc(h.query)}">${this.esc(h.query)}</td>
                    <td class="dim">${timeAgo(h.time)}</td>
                    <td class="dim">${h.duration.toFixed(1)}ms</td>
                    <td>${h.rowCount}</td>
                    <td>${h.error ? '<span class="red">error</span>' : '<span class="green">ok</span>'}</td>
                </tr>`).join('')}</tbody>
            </table>`;
        } catch (e) { /* handled */ }
    },

    useHistory(row) {
        const query = row.getAttribute('data-query');
        if (query) {
            const decoded = query.replace(/&amp;/g,'&').replace(/&lt;/g,'<').replace(/&gt;/g,'>').replace(/&#39;/g,"'").replace(/&quot;/g,'"');
            document.getElementById('sql-input').value = decoded;
        }
    },

    async clearHistory() {
        try {
            await apiProtected('/sql/history', { method: 'DELETE' });
            showToast('history cleared', 'success');
            await this.loadHistory();
        } catch (e) { /* handled */ }
    },

    // ===== Snippets =====
    async loadSnippets() {
        try {
            const res = await api('/sql/snippets');
            this._snippets = res.data || [];
            const sel = document.getElementById('sql-snippets');
            if (!sel) return;

            const groups = {};
            this._snippets.forEach(s => {
                if (!groups[s.category]) groups[s.category] = [];
                groups[s.category].push(s);
            });

            for (const [cat, items] of Object.entries(groups)) {
                const optGroup = document.createElement('optgroup');
                optGroup.label = cat;
                items.forEach(s => {
                    const opt = document.createElement('option');
                    opt.value = s.query;
                    opt.textContent = s.name;
                    optGroup.appendChild(opt);
                });
                sel.appendChild(optGroup);
            }
        } catch (e) { /* silent */ }
    },

    // ===== Execute =====
    async execute() {
        const input = document.getElementById('sql-input');
        const query = input?.value?.trim();
        if (!query) return;

        const btn = document.getElementById('sql-run-btn');
        const status = document.getElementById('sql-status');
        const result = document.getElementById('sql-result');

        btn.disabled = true;
        btn.innerHTML = '<i data-lucide="loader" class="icon-sm"></i> running...';
        status.style.display = 'block';
        status.textContent = 'executing...';
        status.className = 'mono-xs dim mb-8';

        const startTime = performance.now();

        try {
            const data = await apiProtected('/sql/execute', {
                method: 'POST',
                body: JSON.stringify({ query, database: DbSelector.getSelected() }),
            });
            const elapsed = ((performance.now() - startTime) / 1000).toFixed(3);

            if (!data.success) {
                status.textContent = `error (${elapsed}s)`;
                status.className = 'mono-xs red mb-8';
                result.innerHTML = `<div class="card"><pre class="red mono-xs" style="margin:0;white-space:pre-wrap">${this.esc(data.error)}</pre></div>`;
                if (this._showHistory) this.loadHistory();
                return;
            }

            const d = data.data;
            status.textContent = `${d.rowCount} rows · ${elapsed}s`;
            status.className = 'mono-xs accent mb-8';

            if (d.columns.length === 0 || d.rowCount === 0) {
                result.innerHTML = '<div class="card"><span class="dim mono-xs">no results</span></div>';
                if (this._showHistory) this.loadHistory();
                return;
            }

            let html = `<div class="card" style="padding:0;overflow-x:auto">
                <table class="data-table">
                    <thead><tr>${d.columns.map(c => `<th>${this.esc(c)}</th>`).join('')}</tr></thead>
                    <tbody>`;

            d.rows.forEach(row => {
                html += '<tr>';
                d.columns.forEach(col => {
                    const val = row[col];
                    const display = val === null ? '<span class="dim">NULL</span>' : this.esc(String(val));
                    html += `<td>${display}</td>`;
                });
                html += '</tr>';
            });

            html += '</tbody></table></div>';
            result.innerHTML = html;

            // Refresh history if visible
            if (this._showHistory) this.loadHistory();

        } catch (e) {
            const elapsed = ((performance.now() - startTime) / 1000).toFixed(3);
            status.textContent = `error (${elapsed}s)`;
            status.className = 'mono-xs red mb-8';
            result.innerHTML = `<div class="card"><span class="red mono-xs">${this.esc(e.message)}</span></div>`;
        } finally {
            btn.disabled = false;
            btn.innerHTML = '<i data-lucide="play" class="icon-sm"></i> run';
            lucide.createIcons();
        }
    },

    esc(s) { return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/'/g, '&#39;').replace(/"/g, '&quot;'); },
};
