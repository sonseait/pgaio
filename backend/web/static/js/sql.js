// PGAIO — SQL Editor with CodeMirror 6 (Syntax Highlighting + Autocomplete)

const SQLEditor = {
    _snippets: [],
    _showHistory: false,
    _editor: null,
    _schemaCache: null,
    _cm: null,               // CodeMirror module reference
    _sqlCompartment: null,    // Compartment for dynamic SQL lang reconfiguration

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
                    <span class="mono-xs dim" style="align-self:center">ctrl+enter</span>
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

            <div class="card mb-8" style="padding:0;overflow:hidden">
                <div id="sql-cm-editor"></div>
            </div>

            <div id="sql-status" class="mono-xs dim mb-8" style="display:none"></div>
            <div id="sql-result"></div>
        `;

        lucide.createIcons();
        await DbSelector.renderInto(document.getElementById('sql-db-sel'), () => {
            this._reloadSchema();
        });

        document.getElementById('sql-snippets').addEventListener('change', (e) => {
            if (e.target.value && this._editor) {
                this._editor.dispatch({
                    changes: { from: 0, to: this._editor.state.doc.length, insert: e.target.value }
                });
                e.target.selectedIndex = 0;
                this._editor.focus();
            }
        });

        await this.loadSnippets();
        await this._initCodeMirror();
    },

    // ===== CodeMirror 6 Initialization =====
    async _initCodeMirror() {
        const container = document.getElementById('sql-cm-editor');
        if (!container) return;

        // Import from local pre-built bundle (single file, no duplicate instances)
        try {
            const CM = await import('/js/codemirror-sql.min.js');
            this._cm = CM; // store for _reloadSchema

            const { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter,
                    highlightSpecialChars, drawSelection, placeholder, EditorState, Compartment,
                    defaultHighlightStyle, syntaxHighlighting, indentOnInput, bracketMatching,
                    foldGutter, foldKeymap, HighlightStyle, sql, PostgreSQL,
                    autocompletion, completionKeymap, closeBrackets, closeBracketsKeymap,
                    searchKeymap, highlightSelectionMatches,
                    defaultKeymap, historyKeymap, history, tags } = CM;

            // Create compartment for dynamic SQL reconfiguration
            this._sqlCompartment = new Compartment();

            // Fetch schema for autocomplete
            const schema = await this._fetchSchema();
            const sqlConfig = this._buildSqlConfig(schema);

            // Dark theme matching PGAIO design
            const pgaioTheme = EditorView.theme({
                '&': {
                    backgroundColor: '#111',
                    color: '#aaa',
                    fontSize: '12px',
                    fontFamily: '"JetBrains Mono", "Fira Code", "Consolas", monospace',
                },
                '.cm-content': {
                    caretColor: '#4af',
                    minHeight: '120px',
                    padding: '10px 0',
                },
                '.cm-cursor, .cm-dropCursor': { borderLeftColor: '#4af' },
                '&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection': {
                    backgroundColor: '#264f78',
                },
                '.cm-activeLine': { backgroundColor: '#1a1a1a' },
                '.cm-selectionMatch': { backgroundColor: '#aafe661a' },
                '&.cm-focused .cm-matchingBracket, &.cm-focused .cm-nonmatchingBracket': {
                    backgroundColor: '#bad0f847',
                    outline: '1px solid #515a6b',
                },
                '.cm-gutters': {
                    backgroundColor: '#111',
                    color: '#444',
                    border: 'none',
                    borderRight: '1px solid #2a2a2a',
                },
                '.cm-activeLineGutter': {
                    backgroundColor: '#1a1a1a',
                    color: '#666',
                },
                '.cm-foldPlaceholder': {
                    backgroundColor: 'transparent',
                    border: 'none',
                    color: '#666',
                },
                '.cm-tooltip': {
                    border: '1px solid #2a2a2a',
                    backgroundColor: '#1a1a1a',
                    color: '#aaa',
                },
                '.cm-tooltip-autocomplete': {
                    '& > ul > li': {
                        fontFamily: '"JetBrains Mono", monospace',
                        fontSize: '11px',
                        padding: '2px 8px',
                    },
                    '& > ul > li[aria-selected]': {
                        backgroundColor: '#264f78',
                        color: '#e0e0e0',
                    },
                },
                '.cm-completionLabel': { fontSize: '11px' },
                '.cm-completionDetail': { fontSize: '10px', color: '#666', fontStyle: 'normal' },
                '.cm-panels': { backgroundColor: '#1a1a1a', color: '#aaa' },
                '.cm-panels.cm-panels-top': { borderBottom: '1px solid #2a2a2a' },
                '.cm-panels.cm-panels-bottom': { borderTop: '1px solid #2a2a2a' },
                '.cm-searchMatch': { backgroundColor: '#72a1ff59', outline: '1px solid #457dff' },
                '.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: '#6199ff2f' },
            }, { dark: true });

            // Syntax highlighting colors (VS Code Dark+)
            const pgaioHighlight = HighlightStyle.define([
                { tag: tags.keyword, color: '#569cd6', fontWeight: '600' },
                { tag: tags.typeName, color: '#4ec9b0' },
                { tag: tags.string, color: '#ce9178' },
                { tag: tags.number, color: '#b5cea8' },
                { tag: tags.bool, color: '#569cd6' },
                { tag: tags.null, color: '#569cd6' },
                { tag: tags.operator, color: '#d4d4d4' },
                { tag: tags.punctuation, color: '#808080' },
                { tag: tags.comment, color: '#6a9955', fontStyle: 'italic' },
                { tag: tags.variableName, color: '#9cdcfe' },
                { tag: tags.function(tags.variableName), color: '#dcdcaa' },
                { tag: tags.definition(tags.variableName), color: '#4fc1ff' },
                { tag: tags.propertyName, color: '#9cdcfe' },
            ]);

            // Ctrl+Enter to execute
            const executeKeymap = keymap.of([{
                key: 'Ctrl-Enter',
                mac: 'Cmd-Enter',
                run: () => { this.execute(); return true; },
            }]);

            this._editor = new EditorView({
                state: EditorState.create({
                    doc: '',
                    extensions: [
                        lineNumbers(),
                        highlightActiveLineGutter(),
                        highlightSpecialChars(),
                        history(),
                        foldGutter(),
                        drawSelection(),
                        EditorState.allowMultipleSelections.of(true),
                        indentOnInput(),
                        syntaxHighlighting(pgaioHighlight),
                        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
                        bracketMatching(),
                        closeBrackets(),
                        highlightActiveLine(),
                        highlightSelectionMatches(),
                        this._sqlCompartment.of(sql(sqlConfig)),
                        autocompletion(),
                        placeholder('SELECT * FROM pg_stat_activity LIMIT 10;'),
                        keymap.of([
                            ...closeBracketsKeymap,
                            ...defaultKeymap,
                            ...historyKeymap,
                            ...completionKeymap,
                            ...searchKeymap,
                            ...foldKeymap,
                        ]),
                        executeKeymap,
                        pgaioTheme,
                        EditorView.lineWrapping,
                    ],
                }),
                parent: container,
            });

            this._editor.focus();

        } catch (e) {
            console.error('CodeMirror init failed, falling back to textarea:', e);
            this._initFallback(container);
        }
    },

    // Build SQL dialect config from schema info
    _buildSqlConfig(schema) {
        const { PostgreSQL } = this._cm;
        const config = { dialect: PostgreSQL };
        if (schema && schema.tables) {
            const schemaObj = {};
            schema.tables.forEach(t => {
                const cols = (schema.columns || [])
                    .filter(c => c.table === t.name)
                    .map(c => c.name);
                schemaObj[t.name] = cols;
            });
            config.schema = schemaObj;
        }
        return config;
    },

    // Reload schema when database changes (called by DbSelector onChange)
    async _reloadSchema() {
        this._schemaCache = null;
        if (!this._editor || !this._cm || !this._sqlCompartment) return;
        try {
            const schema = await this._fetchSchema();
            const sqlConfig = this._buildSqlConfig(schema);
            this._editor.dispatch({
                effects: this._sqlCompartment.reconfigure(this._cm.sql(sqlConfig)),
            });
        } catch (e) {
            console.error('Failed to reload schema:', e);
        }
    },

    // Fallback to plain textarea if CDN fails
    _initFallback(container) {
        container.innerHTML = `
            <textarea id="sql-input" spellcheck="false" style="
                width:100%;min-height:120px;max-height:300px;resize:vertical;
                background:var(--bg-1);color:var(--text-1);border:none;
                padding:10px;font-size:12px;line-height:1.5;
                font-family:var(--font);outline:none;box-sizing:border-box;
            " placeholder="SELECT * FROM pg_stat_activity LIMIT 10;"></textarea>
        `;
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
    },

    // Fetch table/column info for autocomplete
    async _fetchSchema() {
        if (this._schemaCache) return this._schemaCache;
        try {
            const db = DbSelector.getParam();
            const res = await api(`/sql/schema${db}`);
            if (res.success && res.data) {
                this._schemaCache = res.data;
                return res.data;
            }
        } catch (e) { /* autocomplete without schema */ }
        return null;
    },

    // Get editor content
    _getQuery() {
        if (this._editor) {
            return this._editor.state.doc.toString().trim();
        }
        return document.getElementById('sql-input')?.value?.trim() || '';
    },

    // Set editor content
    _setQuery(text) {
        if (this._editor) {
            this._editor.dispatch({
                changes: { from: 0, to: this._editor.state.doc.length, insert: text }
            });
        } else {
            const input = document.getElementById('sql-input');
            if (input) input.value = text;
        }
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
            this._setQuery(decoded);
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
        const query = this._getQuery();
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
