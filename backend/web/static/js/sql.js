// PGAIO — SQL Editor

const SQLEditor = {
    _snippets: [],

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">sql editor</span>
                <div class="flex gap-4">
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

            <div class="card mb-8" style="padding:0">
                <textarea id="sql-input" spellcheck="false" style="
                    width:100%;min-height:120px;max-height:300px;resize:vertical;
                    background:var(--bg-1);color:var(--text-1);border:none;
                    padding:10px;font-size:12px;line-height:1.5;
                    font-family:var(--font);outline:none;
                " placeholder="SELECT * FROM pg_stat_activity LIMIT 10;"></textarea>
            </div>

            <div id="sql-status" class="mono-xs dim mb-8" style="display:none"></div>
            <div id="sql-result"></div>
        `;

        lucide.createIcons();

        // Keyboard shortcut
        document.getElementById('sql-input').addEventListener('keydown', (e) => {
            if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
                e.preventDefault();
                this.execute();
            }
            // Tab key inserts spaces
            if (e.key === 'Tab') {
                e.preventDefault();
                const ta = e.target;
                const start = ta.selectionStart;
                ta.value = ta.value.substring(0, start) + '  ' + ta.value.substring(ta.selectionEnd);
                ta.selectionStart = ta.selectionEnd = start + 2;
            }
        });

        // Snippets
        document.getElementById('sql-snippets').addEventListener('change', (e) => {
            if (e.target.value) {
                document.getElementById('sql-input').value = e.target.value;
                e.target.selectedIndex = 0;
            }
        });

        await this.loadSnippets();
    },

    async loadSnippets() {
        try {
            const res = await api('/sql/snippets');
            this._snippets = res.data || [];
            const sel = document.getElementById('sql-snippets');
            if (!sel) return;

            // Group by category
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
                body: JSON.stringify({ query }),
            });
            const elapsed = ((performance.now() - startTime) / 1000).toFixed(3);

            if (!data.success) {
                status.textContent = `error (${elapsed}s)`;
                status.className = 'mono-xs red mb-8';
                result.innerHTML = `<div class="card"><pre class="red mono-xs" style="margin:0;white-space:pre-wrap">${this.esc(data.error)}</pre></div>`;
                return;
            }

            const d = data.data;
            status.textContent = `${d.rowCount} rows · ${elapsed}s`;
            status.className = 'mono-xs accent mb-8';

            if (d.columns.length === 0 || d.rowCount === 0) {
                result.innerHTML = '<div class="card"><span class="dim mono-xs">no results</span></div>';
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

    esc(s) { return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); },
};
