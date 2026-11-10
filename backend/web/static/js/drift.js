const SchemaDriftPage = {
    async render(container) {
        container.innerHTML = `
            <div class="card mb-8">
                <div class="flex-between" style="gap:12px;flex-wrap:wrap">
                    <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                        <span class="mono-xs dim">profile</span>
                        <div id="drift-profile-sel"></div>
                        <span class="mono-xs dim">source</span>
                        <select id="drift-source" class="db-select"></select>
                        <span class="mono-xs dim">target</span>
                        <select id="drift-target" class="db-select"></select>
                    </div>
                    <button class="btn btn-sm btn-primary" id="btn-run-drift">
                        <i data-lucide="git-compare" class="icon-sm"></i> compare
                    </button>
                </div>
                <div class="mono-xs dim" style="margin-top:8px">Compare tables, columns, indexes, and installed extensions across two databases.</div>
            </div>
            <div id="drift-summary"></div>
            <div id="drift-sections"></div>
        `;
        await ProfileSelector.renderInto(document.getElementById('drift-profile-sel'), 'drift');
        await this.renderSelectors();
        document.getElementById('btn-run-drift').addEventListener('click', () => this.load());
        lucide.createIcons();
    },

    async renderSelectors() {
        const dbs = await DbSelector.load();
        const sourceEl = document.getElementById('drift-source');
        const targetEl = document.getElementById('drift-target');
        if (!sourceEl || !targetEl) return;

        const selected = DbSelector.getSelected();
        const source = sessionStorage.getItem('pgaio_drift_source') || selected || dbs[0] || '';
        let target = sessionStorage.getItem('pgaio_drift_target') || '';
        if (!target || target === source) {
            target = dbs.find(db => db !== source) || source;
        }

        const options = dbs.map(db => `<option value="${escHtml(db)}">${escHtml(db)}</option>`).join('');
        sourceEl.innerHTML = options;
        targetEl.innerHTML = options;
        sourceEl.value = source;
        targetEl.value = target;

        sourceEl.addEventListener('change', () => {
            sessionStorage.setItem('pgaio_drift_source', sourceEl.value);
        });
        targetEl.addEventListener('change', () => {
            sessionStorage.setItem('pgaio_drift_target', targetEl.value);
        });

        if (source && target) {
            await this.load();
        }
    },

    async load() {
        const source = document.getElementById('drift-source')?.value || '';
        const target = document.getElementById('drift-target')?.value || '';
        const profile = await ProfileSelector.ensureSelected('drift');
        const summaryEl = document.getElementById('drift-summary');
        const sectionsEl = document.getElementById('drift-sections');

        if (!source || !target) {
            summaryEl.innerHTML = '<div class="card"><span class="mono-xs dim">select both databases</span></div>';
            sectionsEl.innerHTML = '';
            return;
        }
        if (source === target) {
            summaryEl.innerHTML = '<div class="card"><span class="mono-xs yellow">choose two different databases</span></div>';
            sectionsEl.innerHTML = '';
            return;
        }

        summaryEl.innerHTML = '<div class="card"><span class="mono-xs dim">comparing schemas...</span></div>';
        sectionsEl.innerHTML = '';
        try {
            const res = await api(`/schema/drift?source=${encodeURIComponent(source)}&target=${encodeURIComponent(target)}&profile=${encodeURIComponent(profile)}`);
            const data = res.data || {};
            const summary = data.summary || {};
            const cards = [
                ['tables', countSummary(summary, 'table')],
                ['columns', countSummary(summary, 'column')],
                ['indexes', countSummary(summary, 'index')],
                ['extensions', countSummary(summary, 'extension')],
            ];
            summaryEl.innerHTML = `
                <div class="grid-4" style="gap:8px">
                    ${cards.map(([label, counts]) => `
                        <div class="card">
                            <div class="card-title">${label}</div>
                            <div class="mono-xs dim">source only: ${counts.sourceOnly}</div>
                            <div class="mono-xs dim">target only: ${counts.targetOnly}</div>
                            <div class="mono-xs dim">changed: ${counts.changed}</div>
                        </div>
                    `).join('')}
                </div>
            `;

            sectionsEl.innerHTML = [
                this.renderSection('table', summary),
                this.renderSection('column', summary),
                this.renderSection('index', summary),
                this.renderSection('extension', summary),
            ].join('');
        } catch (e) {
            summaryEl.innerHTML = `<div class="card"><span class="mono-xs red">${escHtml(e.message)}</span></div>`;
            sectionsEl.innerHTML = '';
        }
    },

    renderSection(kind, summary) {
        const sourceOnly = summary[`${kind}OnlyInSource`] || [];
        const targetOnly = summary[`${kind}OnlyInTarget`] || [];
        const changed = summary[`${kind}Changed`] || [];
        return `
            <div class="card mb-8">
                <div class="card-title">${kind} drift</div>
                ${this.renderList(`${kind} only in source`, sourceOnly)}
                ${this.renderList(`${kind} only in target`, targetOnly)}
                ${this.renderChanged(`${kind} changed`, changed)}
            </div>
        `;
    },

    renderList(title, items) {
        return `
            <div style="margin-top:10px">
                <div class="mono-xs dim" style="margin-bottom:4px">${title} (${items.length})</div>
                ${items.length ? `
                    <div class="overflow-auto" style="max-height:180px">
                        <table class="data-table">
                            <thead><tr><th>name</th><th>detail</th></tr></thead>
                            <tbody>${items.map(item => `
                                <tr>
                                    <td class="mono-xs">${escHtml(item.name)}</td>
                                    <td class="mono-xs dim">${escHtml(item.detail || '')}</td>
                                </tr>
                            `).join('')}</tbody>
                        </table>
                    </div>
                ` : '<div class="mono-xs green">no differences</div>'}
            </div>
        `;
    },

    renderChanged(title, items) {
        return `
            <div style="margin-top:10px">
                <div class="mono-xs dim" style="margin-bottom:4px">${title} (${items.length})</div>
                ${items.length ? `
                    <div class="overflow-auto" style="max-height:220px">
                        <table class="data-table">
                            <thead><tr><th>name</th><th>source</th><th>target</th></tr></thead>
                            <tbody>${items.map(item => `
                                <tr>
                                    <td class="mono-xs">${escHtml(item.name)}</td>
                                    <td class="mono-xs dim">${escHtml(item.sourceDetail || '')}</td>
                                    <td class="mono-xs dim">${escHtml(item.targetDetail || '')}</td>
                                </tr>
                            `).join('')}</tbody>
                        </table>
                    </div>
                ` : '<div class="mono-xs green">no changed objects</div>'}
            </div>
        `;
    },
};

function countSummary(summary, prefix) {
    return {
        sourceOnly: (summary[`${prefix}OnlyInSource`] || []).length,
        targetOnly: (summary[`${prefix}OnlyInTarget`] || []).length,
        changed: (summary[`${prefix}Changed`] || []).length,
    };
}
