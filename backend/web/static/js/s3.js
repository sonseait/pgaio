// PGAIO — S3 Browser Module

const S3Browser = {
    currentPrefix: '',

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-12">
                <div class="breadcrumb" id="s3-breadcrumb">
                    <a href="#" onclick="S3Browser.navigate('');return false">root</a>
                </div>
            </div>
            <div class="card">
                <div class="overflow-auto" style="height:calc(100vh - 134px); max-height: none;">
                    <table>
                        <thead><tr><th>name</th><th style="width:80px">size</th><th style="width:100px">modified</th><th style="width:60px"></th></tr></thead>
                        <tbody id="s3-list"><tr><td colspan="4" class="text-center dim py-16">loading...</td></tr></tbody>
                    </table>
                </div>
            </div>
        `;
        this.currentPrefix = '';
        this.loadObjects();
    },

    async loadObjects() {
        const tbody = document.getElementById('s3-list');
        try {
            const res = await api(`/s3/objects?prefix=${encodeURIComponent(this.currentPrefix)}`);
            const data = res.data || {};
            const items = data.objects || [];

            this.renderBreadcrumb();

            // Filter out self-reference and sort: dirs first, then files
            const filtered = items.filter(obj => {
                const name = obj.key.replace(this.currentPrefix, '');
                return name && name !== '/';
            });
            const dirs = filtered.filter(o => o.isDir);
            const files = filtered.filter(o => !o.isDir);
            const sorted = [...dirs, ...files];

            if (!sorted.length) {
                tbody.innerHTML = '<tr><td colspan="4" class="text-center dim py-16">empty</td></tr>';
                return;
            }

            tbody.innerHTML = sorted.map(obj => {
                const name = obj.key.replace(this.currentPrefix, '');
                if (obj.isDir) {
                    return `<tr>
                        <td><a href="#" onclick="S3Browser.navigate('${escHtml(obj.key)}');return false" class="accent">
                            <i data-lucide="folder" style="width:12px;height:12px;display:inline-block;vertical-align:middle;margin-right:4px"></i>${escHtml(name)}
                        </a></td>
                        <td class="dim">-</td>
                        <td class="dim">-</td>
                        <td></td>
                    </tr>`;
                }
                return `<tr>
                    <td><i data-lucide="file" style="width:12px;height:12px;display:inline-block;vertical-align:middle;margin-right:4px;color:var(--text-2)"></i>${escHtml(name)}</td>
                    <td>${formatBytes(obj.size)}</td>
                    <td>${obj.lastModified && obj.lastModified !== '0001-01-01T00:00:00Z' ? timeAgo(obj.lastModified) : '-'}</td>
                    <td>
                        <button onclick="S3Browser.download('${escHtml(obj.key)}')" class="btn btn-sm btn-ghost" title="download">
                            <i data-lucide="download" class="icon-sm"></i>
                        </button>
                        <button onclick="S3Browser.deleteObj('${escHtml(obj.key)}')" class="btn btn-sm btn-ghost btn-danger" title="delete">
                            <i data-lucide="trash-2" class="icon-sm"></i>
                        </button>
                    </td>
                </tr>`;
            }).join('');
            lucide.createIcons();
        } catch (e) {
            tbody.innerHTML = `<tr><td colspan="4" class="dim text-center py-16">error: ${escHtml(e.message)}</td></tr>`;
        }
    },

    renderBreadcrumb() {
        const el = document.getElementById('s3-breadcrumb');
        if (!el) return;
        const parts = this.currentPrefix.split('/').filter(Boolean);
        let html = '<a href="#" onclick="S3Browser.navigate(\'\');return false">root</a>';
        let path = '';
        parts.forEach(p => {
            path += p + '/';
            const pp = path;
            html += ` <span class="dim">/</span> <a href="#" onclick="S3Browser.navigate('${escHtml(pp)}');return false">${escHtml(p)}</a>`;
        });
        el.innerHTML = html;
    },

    navigate(prefix) {
        this.currentPrefix = prefix;
        this.loadObjects();
    },

    async download(key) {
        try {
            const res = await api(`/s3/download?key=${encodeURIComponent(key)}`);
            if (res.data && res.data.url) window.open(res.data.url, '_blank');
        } catch (e) { /* handled */ }
    },

    async deleteObj(key) {
        if (!confirm(`Delete "${key}"?`)) return;
        try {
            await apiProtected(`/s3/objects?key=${encodeURIComponent(key)}`, { method: 'DELETE' });
            showToast('deleted', 'success');
            this.loadObjects();
        } catch (e) { /* handled */ }
    },
};
