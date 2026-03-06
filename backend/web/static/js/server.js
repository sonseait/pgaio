// PGAIO — Server Overview

const ServerOverview = {
    async render(container) {
        container.innerHTML = `
            <div class="card-title mb-8">server overview</div>
            <div id="server-content">
                <div class="card"><span class="dim mono-xs">loading...</span></div>
            </div>
        `;
        await this.loadOverview();
    },

    async loadOverview() {
        const container = document.getElementById('server-content');
        if (!container) return;

        try {
            const res = await api('/server/overview');
            const d = res.data;
            if (!d) { container.innerHTML = '<div class="card"><span class="red">no data</span></div>'; return; }

            let html = '';

            // Server info bar
            html += `<div class="flex gap-8 mb-8">
                <div class="card" style="flex:1">
                    <div class="mono-xs dim">version</div>
                    <div class="mono-xs" style="margin-top:4px">${this.esc(d.version?.split(',')[0] || '')}</div>
                </div>
                <div class="card" style="flex:0 0 180px">
                    <div class="mono-xs dim">start time</div>
                    <div class="mono-xs" style="margin-top:4px">${this.esc(d.startTime?.substring(0, 19) || '')}</div>
                </div>
                <div class="card" style="flex:0 0 100px">
                    <div class="mono-xs dim">databases</div>
                    <div style="font-size:20px;font-weight:700;margin-top:2px">${d.totalDbs}</div>
                </div>
                <div class="card" style="flex:0 0 100px">
                    <div class="mono-xs dim">tables</div>
                    <div style="font-size:20px;font-weight:700;margin-top:2px">${d.totalTables}</div>
                </div>
            </div>`;

            // Databases
            if (d.databases && d.databases.length > 0) {
                d.databases.forEach(db => {
                    html += `<div class="card mb-8" style="padding:0">
                        <div style="padding:8px 10px;background:var(--bg-2);display:flex;align-items:center;gap:8px">
                            <i data-lucide="database" style="width:14px;height:14px;color:var(--accent)"></i>
                            <span style="font-weight:600;font-size:12px">${this.esc(db.name)}</span>
                            <span class="dim mono-xs">${this.esc(db.size)}</span>
                            <span class="dim mono-xs">· ${this.esc(db.owner)}</span>
                            <span class="dim mono-xs">· ${this.esc(db.encoding)}</span>
                            <span class="dim mono-xs" style="margin-left:auto">${db.tableCount} tables</span>
                        </div>`;

                    if (db.schemas && db.schemas.length > 0) {
                        db.schemas.forEach(schema => {
                            html += `<div style="padding:0">
                                <div style="padding:4px 10px 4px 24px;display:flex;align-items:center;gap:6px;background:var(--bg-1)">
                                    <i data-lucide="folder" style="width:11px;height:11px;color:var(--text-2)"></i>
                                    <span class="mono-xs">${this.esc(schema.name)}</span>
                                    <span class="dim mono-xs">${schema.size}</span>
                                    <span class="dim mono-xs">· ${schema.tableCount} tables</span>
                                </div>`;

                            if (schema.tables && schema.tables.length > 0) {
                                html += `<table class="data-table">
                                    <thead><tr>
                                        <th style="padding-left:38px">table</th>
                                        <th style="width:100px">rows</th>
                                        <th style="width:100px">size</th>
                                    </tr></thead><tbody>`;
                                schema.tables.forEach(t => {
                                    html += `<tr>
                                        <td style="padding-left:38px">
                                            <i data-lucide="table" style="width:10px;height:10px;color:var(--text-2);margin-right:4px;vertical-align:middle"></i>
                                            ${this.esc(t.name)}
                                        </td>
                                        <td class="dim">${t.rows.toLocaleString()}</td>
                                        <td class="dim">${this.esc(t.size)}</td>
                                    </tr>`;
                                });
                                html += `</tbody></table>`;
                            }
                            html += `</div>`;
                        });
                    } else {
                        html += `<div style="padding:8px 10px 8px 24px" class="dim mono-xs">no schema info (cross-database queries not supported)</div>`;
                    }

                    html += `</div>`;
                });
            }

            container.innerHTML = html;
            lucide.createIcons();
        } catch (e) {
            container.innerHTML = `<div class="card"><span class="red mono-xs">error: ${e.message}</span></div>`;
        }
    },

    esc(s) { return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;'); },
};
