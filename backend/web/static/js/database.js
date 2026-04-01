// PGAIO — Database Export / Import

const DatabaseIO = {
    _exportJobId: null,
    _importJobId: null,
    _poller: null,

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">database export / import</span>
            </div>
            <div class="mb-8" id="dbio-job-status"></div>

            <!-- Export Card -->
            <div class="card mb-8">
                <div class="card-title" style="margin-bottom:8px">
                    <i data-lucide="download" class="icon-sm"></i> export (pg_dump)
                </div>
                <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap">
                    <div>
                        <label class="mono-xs dim" style="display:block;margin-bottom:4px">database</label>
                        <select id="export-db"
                            style="background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                            padding:4px 8px;font-size:11px;font-family:var(--font);min-width:150px">
                            <option>loading...</option>
                        </select>
                    </div>
                    <div>
                        <label class="mono-xs dim" style="display:block;margin-bottom:4px">format</label>
                        <select id="export-format"
                            style="background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                            padding:4px 8px;font-size:11px;font-family:var(--font)">
                            <option value="custom">custom (.dump) — recommended</option>
                            <option value="sql">plain SQL (.sql)</option>
                        </select>
                    </div>
                    <button onclick="DatabaseIO.exportDB()" class="btn btn-primary">
                        <i data-lucide="download" class="icon-sm"></i> download dump
                    </button>
                </div>
                <div style="margin-top:8px;display:flex;align-items:center;gap:6px">
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="export-data-only">
                        <span>data only</span>
                    </label>
                    <span class="mono-xs dim">— skip schema (CREATE TABLE, etc.), export only INSERT/COPY data. use when your app manages schema via migrations.</span>
                </div>
                <div class="mono-xs dim" style="margin-top:6px;line-height:1.5">
                    custom format is compressed and supports parallel restore.
                    plain SQL is human-readable but larger.
                </div>
            </div>

            <!-- Import Card -->
            <div class="card">
                <div class="card-title" style="margin-bottom:8px">
                    <i data-lucide="upload" class="icon-sm"></i> import (pg_restore / psql)
                </div>
                <div style="display:flex;gap:12px;align-items:flex-end;flex-wrap:wrap;margin-bottom:12px">
                    <div>
                        <label class="mono-xs dim" style="display:block;margin-bottom:4px">target database</label>
                        <select id="import-db"
                            style="background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                            padding:4px 8px;font-size:11px;font-family:var(--font);min-width:150px">
                            <option>loading...</option>
                        </select>
                    </div>
                </div>
                <div style="margin-top:8px;display:flex;flex-direction:column;gap:4px">
                    <div class="mono-xs dim" style="margin-bottom:2px">import options:</div>
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="import-data-only">
                        <span>data only</span>
                        <span class="dim">— skip schema, only insert data rows</span>
                    </label>
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="import-disable-triggers" checked>
                        <span>disable triggers</span>
                        <span class="dim">— avoid FK constraint errors during import</span>
                    </label>
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="import-single-tx">
                        <span>single transaction</span>
                        <span class="dim">— atomic: rollback all on any error</span>
                    </label>
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="import-clean">
                        <span>clean (truncate)</span>
                        <span class="dim">— <span class="red">truncate all tables</span> before import</span>
                    </label>
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px;cursor:pointer">
                        <input type="checkbox" id="import-no-tablespaces" checked>
                        <span>no tablespaces</span>
                        <span class="dim">— skip tablespace assignments (Docker safe)</span>
                    </label>
                </div>
                <div id="import-dropzone"
                    style="margin-top: 8px;border:2px dashed var(--border);border-radius:6px;padding:32px;
                    text-align:center;cursor:pointer;transition:all 0.2s">
                    <i data-lucide="upload-cloud" style="width:32px;height:32px;margin:0 auto 8px;display:block;opacity:0.4"></i>
                    <div class="mono-xs dim">drag & drop a .sql or .dump file here</div>
                    <div class="mono-xs dim" style="margin-top:4px">or click to browse (max 512MB)</div>
                    <input type="file" id="import-file" accept=".sql,.dump,.backup"
                        style="display:none">
                </div>
                <div id="import-status" style="margin-top:8px"></div>
            </div>
        `;
        lucide.createIcons();
        this._setupDropzone();
        await this._loadDatabases();
        this.renderJobStatus();
    },

    async _loadDatabases() {
        try {
            const res = await api('/database/list');
            const dbs = res.data || [];
            ['export-db', 'import-db'].forEach(id => {
                const sel = document.getElementById(id);
                if (sel) {
                    sel.innerHTML = dbs.map(d => `<option value="${escHtml(d)}">${escHtml(d)}</option>`).join('');
                }
            });
        } catch (e) { /* handled */ }
    },

    _setupDropzone() {
        const dropzone = document.getElementById('import-dropzone');
        const fileInput = document.getElementById('import-file');
        if (!dropzone || !fileInput) return;

        dropzone.addEventListener('click', () => fileInput.click());
        dropzone.addEventListener('dragover', (e) => {
            e.preventDefault();
            dropzone.style.borderColor = 'var(--accent)';
            dropzone.style.background = 'rgba(255,255,255,0.02)';
        });
        dropzone.addEventListener('dragleave', () => {
            dropzone.style.borderColor = 'var(--border)';
            dropzone.style.background = '';
        });
        dropzone.addEventListener('drop', (e) => {
            e.preventDefault();
            dropzone.style.borderColor = 'var(--border)';
            dropzone.style.background = '';
            if (e.dataTransfer.files.length) this._uploadFile(e.dataTransfer.files[0]);
        });
        fileInput.addEventListener('change', () => {
            if (fileInput.files.length) this._uploadFile(fileInput.files[0]);
        });
    },

    async exportDB() {
        const db = document.getElementById('export-db')?.value;
        const format = document.getElementById('export-format')?.value || 'custom';
        const dataOnly = document.getElementById('export-data-only')?.checked ? 'true' : 'false';
        if (!db) { showToast('select a database', 'error'); return; }

        const doExport = async (sid) => {
            try {
                showToast('starting export...', 'info');
                const res = await fetch(`${API_BASE}/database/export?database=${encodeURIComponent(db)}&format=${format}&dataOnly=${dataOnly}`, {
                    headers: { 'X-Session-ID': sid },
                });
                if (res.status === 401) {
                    sessionStorage.removeItem('pgaio_session');
                    showLoginModal((newSid) => doExport(newSid));
                    return;
                }
                if (!res.ok) throw new Error('export failed: ' + res.statusText);
                const data = await res.json();
                this._exportJobId = data.data?.jobId || null;
                this.trackJobs();
                showToast('export started', 'success');
            } catch (e) {
                showToast('export failed: ' + e.message, 'error');
            }
        };

        const sid = sessionStorage.getItem('pgaio_session');
        if (sid) { doExport(sid); }
        else { showLoginModal((newSid) => doExport(newSid)); }
    },

    async _uploadFile(file) {
        const db = document.getElementById('import-db')?.value;
        if (!db) { showToast('select a target database', 'error'); return; }

        const ext = file.name.split('.').pop().toLowerCase();
        if (!['sql', 'dump', 'backup'].includes(ext)) {
            showToast('only .sql, .dump, or .backup files supported', 'error');
            return;
        }

        if (!await showConfirm('import database', `Import "${file.name}" (${formatBytes(file.size)}) into database "${db}"?\n\nExisting data may be overwritten.`, { danger: true, confirmText: 'import' })) return;

        const statusEl = document.getElementById('import-status');
        statusEl.innerHTML = '<span class="mono-xs yellow">uploading...</span>';

        const doImport = async (sid) => {
            try {
                const formData = new FormData();
                formData.append('file', file);
                formData.append('database', db);
                const checkOpt = (id, key) => {
                    if (document.getElementById(id)?.checked) formData.append(key, 'true');
                };
                checkOpt('import-data-only', 'dataOnly');
                checkOpt('import-disable-triggers', 'disableTriggers');
                checkOpt('import-single-tx', 'singleTransaction');
                checkOpt('import-clean', 'clean');
                checkOpt('import-no-tablespaces', 'noTablespaces');

                const res = await fetch(`${API_BASE}/database/import`, {
                    method: 'POST',
                    headers: { 'X-Session-ID': sid },
                    body: formData,
                });
                if (res.status === 401) {
                    sessionStorage.removeItem('pgaio_session');
                    showLoginModal((newSid) => doImport(newSid));
                    return;
                }
                if (!res.ok) {
                    const err = await res.json().catch(() => ({}));
                    throw new Error(err.error || res.statusText);
                }
                const data = await res.json();
                this._importJobId = data.data?.jobId || null;
                this.trackJobs();
                statusEl.innerHTML = `<span class="mono-xs green">✓ ${escHtml(data.data?.message || 'import started')}</span>`;
                showToast('import started', 'success');
            } catch (e) {
                statusEl.innerHTML = `<span class="mono-xs red">✗ ${escHtml(e.message)}</span>`;
                showToast('import failed: ' + e.message, 'error');
            }
        };

        const sid = sessionStorage.getItem('pgaio_session');
        if (sid) { doImport(sid); }
        else { showLoginModal((newSid) => doImport(newSid), () => { statusEl.innerHTML = ''; }); }
    },

    async renderJobStatus() {
        const el = document.getElementById('dbio-job-status');
        if (!el) return;

        const ids = [this._exportJobId, this._importJobId].filter(Boolean);
        if (!ids.length) {
            el.innerHTML = '';
            return;
        }

        const parts = [];
        for (const id of ids) {
            try {
                const job = await JobUI.get(id);
                parts.push(renderJobSummary(job, { showDownload: job.type === 'export' }));
            } catch (e) {
                parts.push(`<div class="card"><span class="mono-xs red">${escHtml(e.message)}</span></div>`);
            }
        }
        el.innerHTML = parts.join('');
    },

    trackJobs() {
        this.renderJobStatus();
        if (this._poller) clearInterval(this._poller);
        this._poller = setInterval(async () => {
            await this.renderJobStatus();
            const activeIds = [this._exportJobId, this._importJobId].filter(Boolean);
            if (!activeIds.length) {
                clearInterval(this._poller);
                this._poller = null;
                return;
            }
            const states = await Promise.all(activeIds.map(id => JobUI.get(id).catch(() => null)));
            if (states.every(job => !job || job.status !== 'running')) {
                clearInterval(this._poller);
                this._poller = null;
            }
        }, 3000);
    },
};
