// PGAIO — Backup Module (Schedule + Retention + Backups + PITR)

const Backup = {
    async render(container) {
        container.innerHTML = `
            <div class="card mb-8" id="schedule-card"><span class="dim mono-xs">loading schedule...</span></div>
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">wal-g backups</span>
                <div style="display:flex;gap:6px">
                    <button class="btn btn-sm" id="btn-pitr" title="Point-In-Time Recovery">
                        <i data-lucide="clock" class="icon-sm"></i> pitr
                    </button>
                    <button class="btn btn-primary" id="btn-trigger-backup">
                        <i data-lucide="play" class="icon-sm"></i> trigger backup
                    </button>
                </div>
            </div>
            <div class="card" style="padding:0">
                <div class="overflow-auto" style="height:calc(100vh - 192px); max-height: none;">
                    <table class="data-table" style="table-layout:fixed;width:100%">
                        <thead><tr><th>name</th><th style="width:80px">size</th><th style="width:90px">modified</th><th style="width:60px">method</th><th>wal</th><th style="width:80px">actions</th></tr></thead>
                        <tbody id="backup-list"><tr><td colspan="6" class="text-center dim py-16">loading...</td></tr></tbody>
                    </table>
                </div>
            </div>
        `;
        lucide.createIcons();
        document.getElementById('btn-trigger-backup').addEventListener('click', () => this.triggerBackup());
        document.getElementById('btn-pitr').addEventListener('click', () => this.showPITRModal());
        await Promise.all([this.loadSchedule(), this.loadBackups()]);
    },

    // ===== Schedule + Retention (single card) =====
    async loadSchedule() {
        try {
            const [schedRes, settingsRes] = await Promise.all([
                api('/backups/schedule'),
                api('/settings'),
            ]);
            const d = schedRes.data || {};
            const cfg = settingsRes.data || {};
            const el = document.getElementById('schedule-card');
            if (!el) return;

            el.innerHTML = `
                <div class="card-title" style="margin-bottom:6px">backup schedule & retention</div>
                <div style="display:flex;align-items:center;gap:12px;flex-wrap:wrap">
                    <label class="mono-xs" style="display:flex;align-items:center;gap:4px">
                        <input type="checkbox" id="sched-enabled" ${d.enabled ? 'checked' : ''}>
                        <span>${d.enabled ? '<span class="green">enabled</span>' : '<span class="dim">disabled</span>'}</span>
                    </label>
                    <select id="sched-interval"
                        style="background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                        padding:2px 6px;font-size:11px;font-family:var(--font)">
                        ${[1,3,6,12,24].map(h => `<option value="${h}" ${d.intervalHours === h ? 'selected' : ''}>every ${h}h</option>`).join('')}
                    </select>
                    <span class="dim mono-xs">·</span>
                    <span class="mono-xs dim">keep</span>
                    <select id="retain-count"
                        style="background:var(--bg-0);border:1px solid var(--border);color:var(--text-1);
                        padding:2px 6px;font-size:11px;font-family:var(--font)">
                        ${[3,5,7,10,14,30].map(n => `<option value="${n}" ${cfg.backup?.retainCount === n ? 'selected' : ''}>${n} backups</option>`).join('')}
                    </select>
                    <button onclick="Backup.saveSchedule()" class="btn btn-sm" style="font-size:9px">save</button>
                    ${d.nextRun ? `<span class="mono-xs dim" style="margin-left:auto">next: ${new Date(d.nextRun).toLocaleString()}</span>` : ''}
                </div>
            `;
        } catch (e) { /* handled */ }
    },

    async saveSchedule() {
        try {
            const settingsRes = await api('/settings');
            const cfg = settingsRes.data;
            cfg.backup.enabled = document.getElementById('sched-enabled')?.checked || false;
            cfg.backup.intervalHours = parseInt(document.getElementById('sched-interval')?.value) || 6;
            cfg.backup.retainCount = parseInt(document.getElementById('retain-count')?.value) || 7;
            await apiProtected('/settings', { method: 'POST', body: JSON.stringify(cfg) });
            showToast('backup settings saved', 'success');
            await this.loadSchedule();
        } catch (e) { /* handled */ }
    },

    // ===== Backup List =====
    async loadBackups() {
        const tbody = document.getElementById('backup-list');
        try {
            const res = await api('/backups');
            const data = res.data || {};
            const list = data.backups || [];
            if (!list.length) {
                tbody.innerHTML = '<tr><td colspan="6" class="text-center dim py-16">no backups found</td></tr>';
                return;
            }
            tbody.innerHTML = list.slice().reverse().map(b => {
                const isDelta = b.backup_name && b.backup_name.includes('_D_');
                return `<tr>
                <td>${escHtml(b.backup_name)}</td>
                <td>${formatBytes(b.compressed_size || 0)}</td>
                <td>${b.start_time ? timeAgo(b.start_time) : '-'}</td>
                <td><span class="badge ${isDelta ? 'badge-yellow' : 'badge-blue'}">${isDelta ? 'delta' : 'full'}</span></td>
                <td class="mono-xs">${escHtml(b.wal_file_name) || '-'}</td>
                <td><button onclick="Backup.restore('${escHtml(b.backup_name)}')" class="btn btn-sm btn-danger">restore</button></td>
            </tr>`;
            }).join('');
        } catch (e) {
            tbody.innerHTML = `<tr><td colspan="6" class="dim text-center py-16">error: ${escHtml(e.message)}</td></tr>`;
        }
    },

    async triggerBackup() {
        try {
            showToast('starting backup...', 'info');
            await apiProtected('/backups/trigger', { method: 'POST' });
            showToast('backup started', 'success');
            setTimeout(() => this.loadBackups(), 2000);
        } catch (e) { /* handled */ }
    },

    async restore(name) {
        if (!await showConfirm('restore backup', `⚠ DANGER: Restore from "${name}"?\n\nThis will:\n1. Stop PostgreSQL\n2. Delete current data\n3. Restore from this backup\n4. Restart PostgreSQL\n\nAll current data will be LOST.`, { danger: true, confirmText: 'restore' })) return;
        try {
            showToast('restore started — PostgreSQL will restart...', 'info');
            await apiProtected('/backups/restore', {
                method: 'POST',
                body: JSON.stringify({ backupName: name })
            });
            showToast('restore in progress — check logs for status', 'success');
        } catch (e) {
            showToast('restore failed: ' + e.message, 'error');
        }
    },

    // ===== PITR Modal =====
    showPITRModal() {
        document.getElementById('pitr-modal')?.remove();

        const overlay = document.createElement('div');
        overlay.id = 'pitr-modal';
        overlay.className = 'modal-overlay';
        overlay.innerHTML = `
            <div class="modal-dialog" style="width:460px">
                <div class="modal-header">
                    <span class="modal-title">point-in-time recovery (PITR)</span>
                    <button class="modal-close" id="pitr-close">&times;</button>
                </div>
                <div class="modal-body">
                    <div class="mono-xs dim" style="margin-bottom:12px;line-height:1.6">
                        Restore your database to any specific point in time.<br>
                        The system will automatically select the best base backup and replay WAL logs.<br>
                        <span class="red">⚠ This is a DESTRUCTIVE operation. All current data will be replaced.</span>
                    </div>

                    <div style="margin-bottom:12px">
                        <label class="mono-xs dim" style="display:block;margin-bottom:4px">target date & time</label>
                        <input type="datetime-local" id="pitr-datetime" step="1"
                            style="width:100%;background:var(--bg-0);border:1px solid var(--border);
                            color:var(--text-1);padding:6px 8px;font-size:11px;font-family:var(--font)">
                    </div>

                    <div style="margin-bottom:12px">
                        <label class="mono-xs dim" style="display:block;margin-bottom:4px">
                            type <span class="red">PITR_CONFIRM</span> to proceed
                        </label>
                        <input type="text" id="pitr-confirm-input" placeholder="PITR_CONFIRM" autocomplete="off"
                            style="width:100%;background:var(--bg-0);border:1px solid var(--border);
                            color:var(--text-1);padding:6px 8px;font-size:11px;font-family:var(--font)">
                    </div>
                </div>
                <div class="modal-footer">
                    <button class="btn btn-sm" id="pitr-cancel">cancel</button>
                    <button class="btn btn-sm btn-danger" id="pitr-execute">
                        <i data-lucide="alert-triangle" class="icon-sm"></i> execute PITR
                    </button>
                </div>
            </div>
        `;
        document.body.appendChild(overlay);
        lucide.createIcons();

        // Set default datetime to current time
        const now = new Date();
        now.setMinutes(now.getMinutes() - now.getTimezoneOffset());
        document.getElementById('pitr-datetime').value = now.toISOString().slice(0, 19);

        const cleanup = () => overlay.remove();
        document.getElementById('pitr-close').addEventListener('click', cleanup);
        document.getElementById('pitr-cancel').addEventListener('click', cleanup);
        overlay.addEventListener('click', (e) => { if (e.target === overlay) cleanup(); });
        document.getElementById('pitr-execute').addEventListener('click', () => this._executePITR(cleanup));
    },

    async _executePITR(cleanup) {
        const targetTime = document.getElementById('pitr-datetime')?.value;
        const confirmText = document.getElementById('pitr-confirm-input')?.value?.trim();

        if (!targetTime) { showToast('select a target date & time', 'error'); return; }
        if (confirmText !== 'PITR_CONFIRM') {
            showToast('type PITR_CONFIRM to proceed', 'error');
            document.getElementById('pitr-confirm-input').style.borderColor = 'var(--red)';
            return;
        }

        // Format datetime for PostgreSQL
        const pgTimestamp = targetTime.replace('T', ' ');

        try {
            showToast('PITR restore initiated — PostgreSQL will restart...', 'info');
            await apiProtected('/backups/restore', {
                method: 'POST',
                body: JSON.stringify({ backupName: 'LATEST', targetTime: pgTimestamp })
            });
            showToast('PITR in progress — check logs for status', 'success');
            cleanup();
        } catch (e) {
            showToast('PITR failed: ' + e.message, 'error');
        }
    },
};
