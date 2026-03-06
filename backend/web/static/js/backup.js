// PGAIO — Backup Module

const Backup = {
    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-12">
                <span class="card-title" style="margin:0">wal-g backups</span>
                <button class="btn btn-primary" id="btn-trigger-backup">
                    <i data-lucide="play" class="icon-sm"></i> trigger backup
                </button>
            </div>
            <div class="card">
                <div class="overflow-auto" style="height:calc(100vh - 142px); max-height: none;">
                    <table>
                        <thead><tr><th>name</th><th style="width:80px">size</th><th style="width:90px">modified</th><th style="width:60px">method</th><th>wal</th><th style="width:60px">actions</th></tr></thead>
                        <tbody id="backup-list"><tr><td colspan="6" class="text-center dim py-16">loading...</td></tr></tbody>
                    </table>
                </div>
            </div>
        `;
        lucide.createIcons();

        document.getElementById('btn-trigger-backup').addEventListener('click', () => this.triggerBackup());
        this.loadBackups();
    },

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
        // First confirm
        if (!confirm(`⚠️ DANGER: Restore from backup "${name}"?\n\nThis will:\n1. Stop PostgreSQL\n2. Delete current data\n3. Restore from this backup\n4. Restart PostgreSQL\n\nAll current data will be LOST.`)) return;

        // Double confirm — type backup name
        const typed = prompt(`Type the backup name to confirm restore:\n\n${name}`);
        if (typed !== name) {
            showToast('restore cancelled — name did not match', 'warning');
            return;
        }

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
};
