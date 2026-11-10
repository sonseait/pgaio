// PGAIO — Database Tuning Wizard
// 3-step wizard: System Detection → Configuration → Review & Apply

const TunerWizard = {
    _step: 1,
    _system: null,
    _result: null,
    _selected: {},

    async render(container) {
        container.innerHTML = `
            <div class="tuner-wizard">
                <div class="tuner-steps mb-8">
                    <div class="tuner-step active" data-step="1">
                        <span class="step-num">1</span>
                        <span class="step-label">system</span>
                    </div>
                    <div class="tuner-step-line"></div>
                    <div class="tuner-step" data-step="2">
                        <span class="step-num">2</span>
                        <span class="step-label">configure</span>
                    </div>
                    <div class="tuner-step-line"></div>
                    <div class="tuner-step" data-step="3">
                        <span class="step-num">3</span>
                        <span class="step-label">review</span>
                    </div>
                </div>
                <div id="tuner-content">
                    <div class="card"><span class="dim mono-xs">detecting system...</span></div>
                </div>
            </div>
        `;
        this._step = 1;
        this._selected = {};
        await this.renderStep1();
    },

    setStep(n) {
        this._step = n;
        document.querySelectorAll('.tuner-step').forEach(el => {
            const s = parseInt(el.dataset.step);
            el.classList.toggle('active', s === n);
            el.classList.toggle('done', s < n);
        });
        document.querySelectorAll('.tuner-step-line').forEach((el, i) => {
            el.classList.toggle('done', i + 1 < n);
        });
    },

    // ========================
    // Step 1: System Detection
    // ========================
    async renderStep1() {
        this.setStep(1);
        const content = document.getElementById('tuner-content');
        content.innerHTML = '<div class="card"><span class="dim mono-xs">detecting system configuration...</span></div>';

        try {
            const res = await api('/tuner/system');
            this._system = res.data;
            const sys = this._system;

            content.innerHTML = `
                <div class="card mb-8">
                    <div class="card-title">system information</div>
                    <div class="tuner-sys-grid">
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="cpu"></i></div>
                            <div>
                                <div class="tuner-sys-label">cpu cores</div>
                                <div class="tuner-sys-value">${sys.cpuCores}</div>
                            </div>
                        </div>
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="memory-stick"></i></div>
                            <div>
                                <div class="tuner-sys-label">total ram</div>
                                <div class="tuner-sys-value">${this.esc(sys.totalRamHR)}</div>
                            </div>
                        </div>
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="hard-drive"></i></div>
                            <div>
                                <div class="tuner-sys-label">disk type</div>
                                <div class="tuner-sys-value">${sys.diskType.toUpperCase()}</div>
                            </div>
                        </div>
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="database"></i></div>
                            <div>
                                <div class="tuner-sys-label">postgresql</div>
                                <div class="tuner-sys-value">${this.esc(sys.pgVersion || 'unknown')}</div>
                            </div>
                        </div>
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="monitor"></i></div>
                            <div>
                                <div class="tuner-sys-label">os</div>
                                <div class="tuner-sys-value">${this.esc(sys.osInfo || 'linux')}</div>
                            </div>
                        </div>
                        <div class="tuner-sys-item">
                            <div class="tuner-sys-icon"><i data-lucide="hdd"></i></div>
                            <div>
                                <div class="tuner-sys-label">disk total</div>
                                <div class="tuner-sys-value">${this.esc(sys.diskTotalHR)}</div>
                            </div>
                        </div>
                    </div>
                </div>
                <div class="flex-end">
                    <button class="btn btn-sm btn-primary" onclick="TunerWizard.goStep2()">
                        next: configure <i data-lucide="arrow-right" class="icon-sm"></i>
                    </button>
                </div>
            `;
            lucide.createIcons();
        } catch (e) {
            content.innerHTML = `<div class="card"><span class="red mono-xs">error detecting system: ${this.esc(e.message)}</span></div>`;
        }
    },

    // ========================
    // Step 2: Configuration
    // ========================
    goStep2() {
        this.setStep(2);
        const content = document.getElementById('tuner-content');

        content.innerHTML = `
            <div class="card mb-8">
                <div class="card-title">workload profile</div>
                <div class="mono-xs dim mb-8">select the primary use case for this database</div>
                <div class="tuner-profiles">
                    <label class="tuner-profile-card active" data-profile="web">
                        <input type="radio" name="profile" value="web" checked hidden>
                        <div class="profile-icon"><i data-lucide="globe"></i></div>
                        <div class="profile-name">web / oltp</div>
                        <div class="profile-desc">high concurrency, short transactions</div>
                    </label>
                    <label class="tuner-profile-card" data-profile="olap">
                        <input type="radio" name="profile" value="olap" hidden>
                        <div class="profile-icon"><i data-lucide="bar-chart-3"></i></div>
                        <div class="profile-name">olap</div>
                        <div class="profile-desc">analytical queries, large data scans</div>
                    </label>
                    <label class="tuner-profile-card" data-profile="mixed">
                        <input type="radio" name="profile" value="mixed" hidden>
                        <div class="profile-icon"><i data-lucide="layers"></i></div>
                        <div class="profile-name">mixed</div>
                        <div class="profile-desc">balanced oltp and analytics</div>
                    </label>
                    <label class="tuner-profile-card" data-profile="desktop">
                        <input type="radio" name="profile" value="desktop" hidden>
                        <div class="profile-icon"><i data-lucide="laptop"></i></div>
                        <div class="profile-name">desktop</div>
                        <div class="profile-desc">development, low resource usage</div>
                    </label>
                </div>
            </div>

            <div class="card mb-8">
                <div class="card-title">connection calculator</div>
                <div class="mono-xs dim mb-8">
                    enter the expected number of application connections.
                    pgbouncer will multiplex these into fewer postgresql connections.
                </div>
                <div class="tuner-conn-input">
                    <label class="mono-xs">expected app connections</label>
                    <input type="number" id="tuner-conn-count" value="200" min="10" max="50000"
                        style="background:var(--bg-2);border:1px solid var(--border);color:var(--text-1);
                        padding:6px 10px;font-size:13px;font-family:var(--font);width:140px" />
                </div>
                <div id="tuner-conn-preview" class="mt-8"></div>
            </div>

            <div class="flex-between">
                <button class="btn btn-sm" onclick="TunerWizard.renderStep1()">
                    <i data-lucide="arrow-left" class="icon-sm"></i> back
                </button>
                <button class="btn btn-sm btn-primary" id="tuner-analyze-btn" onclick="TunerWizard.analyze()">
                    <i data-lucide="sparkles" class="icon-sm"></i> analyze & recommend
                </button>
            </div>
        `;

        lucide.createIcons();

        // Profile selection
        document.querySelectorAll('.tuner-profile-card').forEach(card => {
            card.addEventListener('click', () => {
                document.querySelectorAll('.tuner-profile-card').forEach(c => c.classList.remove('active'));
                card.classList.add('active');
                card.querySelector('input').checked = true;
            });
        });

        // Connection preview
        const connInput = document.getElementById('tuner-conn-count');
        connInput.addEventListener('input', () => this.previewConnections());
        this.previewConnections();
    },

    previewConnections() {
        const conn = parseInt(document.getElementById('tuner-conn-count')?.value) || 200;
        const cpu = this._system?.cpuCores || 4;

        // Mirror backend logic
        let pgConn = cpu * 4;
        if (pgConn < 50) pgConn = 50;
        let cap = Math.floor(conn / 2);
        if (cap < 50) cap = 50;
        if (cap > 500) cap = 500;
        if (pgConn > cap) pgConn = cap;

        const reserved = 3;
        const poolSize = Math.max(5, Math.floor((pgConn - reserved) / 2));
        const minPool = Math.max(2, Math.floor(poolSize / 4));
        const reservePool = Math.max(2, Math.floor(poolSize / 4));
        const ratio = Math.floor(conn / pgConn);

        const el = document.getElementById('tuner-conn-preview');
        if (!el) return;

        el.innerHTML = `
            <div class="tuner-conn-grid">
                <div class="tuner-conn-section">
                    <div class="conn-section-title">postgresql</div>
                    <div class="conn-row">
                        <span class="dim">max_connections</span>
                        <span class="accent">${pgConn}</span>
                    </div>
                    <div class="conn-row">
                        <span class="dim">reserved (superuser)</span>
                        <span>${reserved}</span>
                    </div>
                </div>
                <div class="tuner-conn-section">
                    <div class="conn-section-title">pgbouncer</div>
                    <div class="conn-row">
                        <span class="dim">max_client_conn</span>
                        <span class="accent">${conn}</span>
                    </div>
                    <div class="conn-row">
                        <span class="dim">default_pool_size</span>
                        <span>${poolSize}</span>
                    </div>
                    <div class="conn-row">
                        <span class="dim">min_pool_size</span>
                        <span>${minPool}</span>
                    </div>
                    <div class="conn-row">
                        <span class="dim">reserve_pool_size</span>
                        <span>${reservePool}</span>
                    </div>
                    <div class="conn-row">
                        <span class="dim">max_db_connections</span>
                        <span>${pgConn - reserved}</span>
                    </div>
                </div>
            </div>
            <div class="tuner-conn-summary mono-xs">
                <i data-lucide="info" class="icon-sm"></i>
                multiplexing ratio: <span class="accent">${ratio}x</span> —
                ${conn} app connections → ${pgConn} pg connections via transaction pooling
            </div>
        `;
        lucide.createIcons();
    },

    // ========================
    // Step 3: Analyze & Review
    // ========================
    async analyze() {
        const btn = document.getElementById('tuner-analyze-btn');
        if (btn) { btn.disabled = true; btn.textContent = 'analyzing...'; }

        const profile = document.querySelector('input[name="profile"]:checked')?.value || 'web';
        const conn = parseInt(document.getElementById('tuner-conn-count')?.value) || 200;

        try {
            const res = await api('/tuner/analyze', {
                method: 'POST',
                body: JSON.stringify({ profile, expectedConnections: conn }),
            });
            this._result = res.data;
            this._selected = {};
            // Select all by default
            this._result.recommendations.forEach(r => { this._selected[r.name] = true; });
            this.renderStep3();
        } catch (e) {
            if (btn) { btn.disabled = false; btn.innerHTML = '<i data-lucide="sparkles" class="icon-sm"></i> analyze & recommend'; lucide.createIcons(); }
            showToast('analysis failed: ' + e.message, 'error');
        }
    },

    renderStep3() {
        this.setStep(3);
        const content = document.getElementById('tuner-content');
        const recs = this._result.recommendations;

        // Group by category
        const groups = {};
        recs.forEach(r => {
            if (!groups[r.category]) groups[r.category] = [];
            groups[r.category].push(r);
        });

        const hasRestart = recs.some(r => this._selected[r.name] && r.needRestart);
        const selectedCount = Object.values(this._selected).filter(Boolean).length;

        let html = `
            <div class="flex-between mb-8">
                <div>
                    <span class="card-title" style="margin:0">recommended changes</span>
                    <span class="mono-xs dim ml-4">${recs.length} parameters</span>
                </div>
                <div class="flex gap-4">
                    <label class="mono-xs flex gap-4" style="cursor:pointer;align-items:center">
                        <input type="checkbox" id="tuner-select-all" ${selectedCount === recs.length ? 'checked' : ''}
                            onchange="TunerWizard.toggleAll(this.checked)">
                        select all
                    </label>
                </div>
            </div>
        `;

        for (const [cat, items] of Object.entries(groups)) {
            html += `
                <div class="card mb-8" style="padding:0">
                    <div style="padding:6px 10px;border-bottom:1px solid var(--border);background:var(--bg-2)">
                        <span class="accent" style="font-size:11px;font-weight:600">${this.esc(cat)}</span>
                        <span class="dim" style="font-size:10px;margin-left:6px">(${items.length})</span>
                    </div>
                    <table class="data-table" style="table-layout:fixed;width:100%">
                        <thead><tr>
                            <th style="width:30px"></th>
                            <th style="width:210px">parameter</th>
                            <th style="width:120px">current</th>
                            <th style="width:20px"></th>
                            <th style="width:120px">recommended</th>
                            <th>description</th>
                            <th style="width:70px">context</th>
                        </tr></thead>
                        <tbody>`;

            items.forEach(r => {
                const checked = this._selected[r.name] ? 'checked' : '';
                const changed = r.currentValue !== r.newValue;
                const valueCls = changed ? 'accent' : 'dim';
                const ctxCls = r.needRestart ? 'red' : 'green';
                const ctxLabel = r.needRestart ? 'restart' : 'reload';
                html += `
                    <tr class="${changed ? '' : 'tuner-unchanged'}">
                        <td><input type="checkbox" ${checked} onchange="TunerWizard.toggle('${this.esc(r.name)}', this.checked)" /></td>
                        <td style="word-break:break-all">${this.esc(r.name)}</td>
                        <td class="dim">${this.esc(r.currentValue)}</td>
                        <td class="dim">→</td>
                        <td class="${valueCls}" style="font-weight:600">${this.esc(r.newValue)}</td>
                        <td class="dim" style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap"
                            title="${this.esc(r.description)}">${this.esc(r.description)}</td>
                        <td><span class="mono-xs ${ctxCls}">${ctxLabel}</span></td>
                    </tr>`;
            });

            html += `</tbody></table></div>`;
        }

        // Connection summary
        const conn = this._result.connections;
        html += `
            <div class="card mb-8">
                <div class="card-title">connection configuration</div>
                <div class="tuner-conn-grid">
                    <div class="tuner-conn-section">
                        <div class="conn-section-title">postgresql</div>
                        <div class="conn-row"><span class="dim">max_connections</span><span class="accent">${conn.pg.maxConnections}</span></div>
                        <div class="conn-row"><span class="dim">superuser_reserved</span><span>${conn.pg.superuserReservedConnections}</span></div>
                    </div>
                    <div class="tuner-conn-section">
                        <div class="conn-section-title">pgbouncer</div>
                        <div class="conn-row"><span class="dim">max_client_conn</span><span class="accent">${conn.pgbouncer.maxClientConn}</span></div>
                        <div class="conn-row"><span class="dim">default_pool_size</span><span>${conn.pgbouncer.defaultPoolSize}</span></div>
                        <div class="conn-row"><span class="dim">min_pool_size</span><span>${conn.pgbouncer.minPoolSize}</span></div>
                        <div class="conn-row"><span class="dim">reserve_pool_size</span><span>${conn.pgbouncer.reservePoolSize}</span></div>
                        <div class="conn-row"><span class="dim">max_db_connections</span><span>${conn.pgbouncer.maxDbConnections}</span></div>
                    </div>
                </div>
                <div class="mono-xs dim mt-8">${this.esc(conn.summary)}</div>
            </div>
        `;

        // Actions
        html += `
            ${hasRestart ? '<div class="tuner-warning mono-xs mb-8"><i data-lucide="alert-triangle" class="icon-sm"></i> some settings require a postgresql restart to take effect</div>' : ''}
            <div class="flex-between">
                <button class="btn btn-sm" onclick="TunerWizard.goStep2()">
                    <i data-lucide="arrow-left" class="icon-sm"></i> back
                </button>
                <div class="flex gap-4">
                    <button class="btn btn-sm btn-primary" onclick="TunerWizard.applySelected(false)">
                        <i data-lucide="check" class="icon-sm"></i>
                        apply selected (${selectedCount})
                    </button>
                    ${hasRestart ? `
                    <button class="btn btn-sm btn-danger" onclick="TunerWizard.applySelected(true)">
                        <i data-lucide="power" class="icon-sm"></i>
                        apply & restart
                    </button>` : ''}
                </div>
            </div>
        `;

        content.innerHTML = html;
        lucide.createIcons();
    },

    toggle(name, checked) {
        this._selected[name] = checked;
        this.updateApplyButton();
    },

    toggleAll(checked) {
        this._result.recommendations.forEach(r => { this._selected[r.name] = checked; });
        document.querySelectorAll('#tuner-content input[type="checkbox"]:not(#tuner-select-all)').forEach(cb => {
            cb.checked = checked;
        });
        this.updateApplyButton();
    },

    updateApplyButton() {
        const count = Object.values(this._selected).filter(Boolean).length;
        const btn = document.querySelector('#tuner-content .btn-primary');
        if (btn) btn.innerHTML = `<i data-lucide="check" class="icon-sm"></i> apply selected (${count})`;
        lucide.createIcons();
    },

    async applySelected(withRestart) {
        const recs = this._result.recommendations.filter(r => this._selected[r.name]);
        if (recs.length === 0) {
            showToast('no settings selected', 'warning');
            return;
        }

        const restartNeeded = recs.some(r => r.needRestart);
        const action = withRestart && restartNeeded ? 'apply and restart PostgreSQL' : 'apply configuration changes';
        const confirmed = await showConfirm(
            'apply tuning changes',
            `Apply ${recs.length} configuration change(s)?\n\n` +
            recs.map(r => `• ${r.name}: ${r.currentValue} → ${r.newValue}`).join('\n') +
            (restartNeeded && withRestart ? '\n\n⚠ PostgreSQL will restart — all connections will be briefly interrupted.' : ''),
            { danger: withRestart, confirmText: action }
        );
        if (!confirmed) return;

        try {
            const payload = {
                postgresSettings: recs.map(r => ({ name: r.name, value: r.newValue })),
                pgbouncerSettings: this._result.connections.pgbouncer,
            };
            const res = await apiProtected('/tuner/apply', {
                method: 'POST',
                body: JSON.stringify(payload),
            });

            const result = res.data;
            if (result.failed?.length > 0) {
                showToast(`${result.applied.length} applied, ${result.failed.length} failed`, 'warning');
            } else {
                showToast(result.message, 'success');
            }

            // Restart if requested
            if (withRestart && restartNeeded) {
                await apiProtected('/config/restart', { method: 'POST' });
                showToast('PostgreSQL restart initiated — reconnecting in 5s...', 'info');
                setTimeout(() => {
                    showToast('PostgreSQL restarted', 'success');
                    this.renderStep1();
                }, 5000);
            } else {
                // Re-analyze to show updated values
                setTimeout(() => this.analyze(), 1000);
            }
        } catch (e) {
            showToast('apply failed: ' + e.message, 'error');
        }
    },

    esc(s) {
        return (s || '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/'/g, '&#39;').replace(/"/g, '&quot;');
    },
};
