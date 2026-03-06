// PGAIO — Dashboard (D3.js charts)

const Dashboard = {
    _data: [],
    _maxPoints: 40,
    _prevCommit: null,
    _prevRollback: null,

    render(container) {
        container.innerHTML = `
            <div class="grid grid-4 mb-12" id="stats-grid"></div>
            <div class="grid grid-2 mb-12">
                <div class="card">
                    <div class="card-title">transactions / interval</div>
                    <div class="chart-wrap" id="chart-tps"></div>
                </div>
                <div class="card">
                    <div class="card-title">connections</div>
                    <div class="chart-wrap" id="chart-conn"></div>
                </div>
            </div>
            <div class="grid grid-1-2 mb-12">
                <div class="card">
                    <div class="card-title">system</div>
                    <div id="sys-resources"></div>
                </div>
                <div class="card">
                    <div class="card-title">database</div>
                    <div class="grid grid-3" id="db-overview"></div>
                </div>
            </div>
            <div class="card">
                <div class="flex-between mb-8">
                    <span class="card-title" style="margin:0">active queries</span>
                    <span id="query-count" class="badge badge-blue">0</span>
                </div>
                <div class="overflow-auto">
                    <table>
                        <thead><tr><th>pid</th><th>user</th><th>db</th><th>state</th><th>dur</th><th>query</th><th></th></tr></thead>
                        <tbody id="queries-body"><tr><td colspan="7" class="text-center dim py-16">waiting...</td></tr></tbody>
                    </table>
                </div>
            </div>
        `;
        this._data = [];
        this._prevCommit = null;
        this._prevRollback = null;
        this.loadInitialData();
        lucide.createIcons();
    },

    async loadInitialData() {
        try {
            const res = await api('/dashboard/stats');
            if (res.success && res.data) this.updateDashboard(res.data);
        } catch (e) { /* handled */ }
    },

    onData(data) { this.updateDashboard(data); },

    updateDashboard(data) {
        const now = new Date().toLocaleTimeString();
        const db = data.database || {};
        const conn = data.connections || {};
        const act = data.activity || {};

        // Build time-series point
        let cd = 0, rd = 0;
        if (this._prevCommit !== null) {
            cd = Math.max(0, (db.txCommit || 0) - this._prevCommit);
            rd = Math.max(0, (db.txRollback || 0) - this._prevRollback);
        }
        this._prevCommit = db.txCommit || 0;
        this._prevRollback = db.txRollback || 0;

        this._data.push({
            time: now,
            commits: cd, rollbacks: rd,
            active: act.activeQueries || 0,
            idle: act.idleConnections || 0,
            waiting: act.waitingQueries || 0,
        });
        if (this._data.length > this._maxPoints) this._data.shift();

        this.renderStats(data);
        this.renderTpsChart();
        this.renderConnChart();
        this.renderSystem(data.system);
        this.renderDbOverview(db);
        this.renderQueries(act);
    },

    renderStats(data) {
        const g = document.getElementById('stats-grid');
        if (!g) return;
        const db = data.database || {};
        const conn = data.connections || {};
        const act = data.activity || {};
        const chr = db.cacheHitRatio || 0;
        const chrColor = chr >= 99 ? 'green' : chr >= 95 ? 'yellow' : 'red';

        g.innerHTML = `
            <div class="card">
                <div class="mono-xs dim">cache hit</div>
                <div class="stat-val ${chrColor}">${chr.toFixed(1)}%</div>
                <div class="stat-label">${db.name || '-'} · ${db.size || '-'}</div>
            </div>
            <div class="card">
                <div class="mono-xs dim">connections</div>
                <div class="stat-val">${conn.usedConnections || 0}<span class="dim" style="font-size:12px">/${conn.maxConnections || 0}</span></div>
                <div class="stat-label">${conn.availableConnections || 0} available</div>
            </div>
            <div class="card">
                <div class="mono-xs dim">active queries</div>
                <div class="stat-val accent">${act.activeQueries || 0}</div>
                <div class="stat-label">${act.waitingQueries || 0} waiting</div>
            </div>
            <div class="card">
                <div class="mono-xs dim">deadlocks</div>
                <div class="stat-val">${db.deadlocks || 0}</div>
                <div class="stat-label">${db.conflicts || 0} conflicts</div>
            </div>
        `;
    },

    renderTpsChart() {
        this._renderLineChart('#chart-tps', this._data, [
            { key: 'commits', color: '#4c6', label: 'commit' },
            { key: 'rollbacks', color: '#e55', label: 'rollback' },
        ]);
    },

    renderConnChart() {
        this._renderLineChart('#chart-conn', this._data, [
            { key: 'active', color: '#4af', label: 'active' },
            { key: 'idle', color: '#a7f', label: 'idle' },
            { key: 'waiting', color: '#ea3', label: 'waiting' },
        ]);
    },

    _chartInstances: {},

    _renderLineChart(selector, data, series) {
        const wrap = document.querySelector(selector);
        if (!wrap || data.length < 2) return;

        const w = wrap.clientWidth, h = wrap.clientHeight;
        const m = { t: 20, r: 8, b: 20, l: 30 };
        const iw = w - m.l - m.r, ih = h - m.t - m.b;

        // Reuse or create SVG
        let inst = this._chartInstances[selector];
        if (!inst || !wrap.querySelector('svg')) {
            wrap.innerHTML = '';
            const svg = d3.select(selector).append('svg')
                .attr('viewBox', `0 0 ${w} ${h}`)
                .attr('preserveAspectRatio', 'none');

            const g = svg.append('g').attr('transform', `translate(${m.l},${m.t})`);
            const gridG = g.append('g').attr('class', 'grid-g');
            const yLabelG = g.append('g').attr('class', 'ylabel-g');
            const xLabelG = g.append('g').attr('class', 'xlabel-g');

            // Create path elements for each series (area + line)
            const paths = {};
            series.forEach(s => {
                paths[s.key + '_area'] = g.append('path')
                    .attr('fill', s.color).attr('fill-opacity', 0.06);
                paths[s.key + '_line'] = g.append('path')
                    .attr('fill', 'none').attr('stroke', s.color).attr('stroke-width', 1.5);
            });

            // Legend
            const lg = svg.append('g').attr('transform', `translate(${m.l + 4}, 6)`);
            series.forEach((s, i) => {
                const offset = i * 60;
                lg.append('rect').attr('x', offset).attr('y', 0).attr('width', 8).attr('height', 8).attr('fill', s.color);
                lg.append('text').attr('x', offset + 12).attr('y', 7).attr('fill', '#888').attr('font-size', 9).text(s.label);
            });

            inst = { svg, g, gridG, yLabelG, xLabelG, paths };
            this._chartInstances[selector] = inst;
        }

        const { g, gridG, yLabelG, xLabelG, paths } = inst;

        // Scales
        const x = d3.scaleLinear().domain([0, data.length - 1]).range([0, iw]);
        const allVals = series.flatMap(s => data.map(d => d[s.key]));
        const yMax = Math.max(d3.max(allVals) || 1, 1);
        const y = d3.scaleLinear().domain([0, yMax]).range([ih, 0]);

        // Grid lines
        const ticks = y.ticks(4);
        const gridLines = gridG.selectAll('line').data(ticks);
        gridLines.exit().remove();
        const gridEnter = gridLines.enter().append('line')
            .attr('x1', 0).attr('x2', iw)
            .attr('stroke', '#1a1a1a').attr('stroke-width', 1);
        gridEnter.merge(gridLines)
            .attr('y1', d => y(d)).attr('y2', d => y(d));

        // Y labels
        const yLabels = yLabelG.selectAll('text').data(ticks);
        yLabels.exit().remove();
        const yEnter = yLabels.enter().append('text')
            .attr('x', -4).attr('dy', '0.35em').attr('text-anchor', 'end')
            .attr('fill', '#555').attr('font-size', 9);
        yEnter.merge(yLabels)
            .attr('y', d => y(d))
            .text(d => d);

        // X labels
        const xData = data.map((d, i) => ({ i, t: d.time })).filter((_, i) => i % 8 === 0 || i === data.length - 1);
        const xLabels = xLabelG.selectAll('text').data(xData, d => d.i);
        xLabels.exit().remove();
        const xEnter = xLabels.enter().append('text')
            .attr('y', ih + 14).attr('text-anchor', 'middle')
            .attr('fill', '#555').attr('font-size', 8);
        xEnter.merge(xLabels)
            .attr('x', d => x(d.i))
            .text(d => d.t);

        // Update paths instantly (no transitions — shifting data morphs badly)
        series.forEach(s => {
            const area = d3.area()
                .x((_, i) => x(i)).y0(ih).y1(d => y(d[s.key]))
                .curve(d3.curveMonotoneX);

            const line = d3.line()
                .x((_, i) => x(i)).y(d => y(d[s.key]))
                .curve(d3.curveMonotoneX);

            paths[s.key + '_area'].datum(data).attr('d', area);
            paths[s.key + '_line'].datum(data).attr('d', line);
        });
    },

    renderSystem(sys) {
        const el = document.getElementById('sys-resources');
        if (!el || !sys) return;

        const bars = [
            { label: 'cpu', val: sys.cpuUsage, color: sys.cpuUsage > 80 ? '#e55' : sys.cpuUsage > 50 ? '#ea3' : '#4c6' },
            { label: 'mem', val: sys.memUsage, color: sys.memUsage > 85 ? '#e55' : sys.memUsage > 60 ? '#ea3' : '#4af',
              detail: `${formatBytes(sys.memUsed)} / ${formatBytes(sys.memTotal)}` },
            { label: 'disk', val: sys.diskUsage, color: sys.diskUsage > 90 ? '#e55' : sys.diskUsage > 70 ? '#ea3' : '#a7f',
              detail: `${formatBytes(sys.diskUsed)} / ${formatBytes(sys.diskTotal)}` },
        ];

        el.innerHTML = bars.map(b => `
            <div style="margin-bottom:8px">
                <div class="flex-between" style="margin-bottom:2px">
                    <span class="mono-xs dim">${b.label}</span>
                    <span class="mono-xs">${(b.val || 0).toFixed(1)}%</span>
                </div>
                <div class="bar-track"><div class="bar-fill" style="width:${Math.min(b.val||0,100)}%;background:${b.color}"></div></div>
                ${b.detail ? `<div class="mono-xs dim mt-4">${b.detail}</div>` : ''}
            </div>
        `).join('') + `
            <div class="flex-between mono-xs dim" style="margin-top:6px">
                <span>load: ${(sys.loadAvg1||0).toFixed(2)} / ${(sys.loadAvg5||0).toFixed(2)} / ${(sys.loadAvg15||0).toFixed(2)}</span>
                <span>up: ${sys.uptime || '-'}</span>
            </div>
        `;
    },

    renderDbOverview(db) {
        const el = document.getElementById('db-overview');
        if (!el || !db) return;
        const stats = [
            ['commits', fmtNum(db.txCommit), '#4c6'],
            ['rollbacks', fmtNum(db.txRollback), '#e55'],
            ['returned', fmtNum(db.tupReturned), '#4af'],
            ['inserted', fmtNum(db.tupInserted), '#a7f'],
            ['updated', fmtNum(db.tupUpdated), '#ea3'],
            ['deleted', fmtNum(db.tupDeleted), '#f93'],
            ['tmp files', fmtNum(db.tempFiles), '#666'],
            ['tmp bytes', formatBytes(db.tempBytes||0), '#666'],
            ['blk read', fmtNum(db.blksRead), '#f93'],
        ];
        el.innerHTML = stats.map(([l, v, c]) => `
            <div class="mini-stat">
                <div class="mini-stat-label">${l}</div>
                <div class="mini-stat-val" style="color:${c}">${v}</div>
            </div>
        `).join('');
    },

    renderQueries(act) {
        const tbody = document.getElementById('queries-body');
        const count = document.getElementById('query-count');
        if (!tbody || !act) return;
        const q = act.queries || [];
        count.textContent = q.length;

        if (!q.length) {
            tbody.innerHTML = '<tr><td colspan="7" class="text-center dim py-16">no active queries</td></tr>';
            return;
        }

        tbody.innerHTML = q.map(r => {
            const stBadge = r.state === 'active' ? 'badge-green' : r.state === 'idle in transaction' ? 'badge-yellow' : 'badge-gray';
            return `<tr>
                <td>${r.pid}</td>
                <td>${r.user}</td>
                <td>${r.database}</td>
                <td><span class="badge ${stBadge}">${r.state}</span></td>
                <td>${formatDuration(r.duration)}</td>
                <td class="truncate" title="${escHtml(r.query)}">${escHtml(r.query)}</td>
                <td>
                    <div class="flex gap-4">
                        <button onclick="Dashboard.cancelQuery(${r.pid})" class="btn btn-sm btn-ghost" title="cancel">✕</button>
                        <button onclick="Dashboard.terminateQuery(${r.pid})" class="btn btn-sm btn-danger" title="kill">⚡</button>
                    </div>
                </td>
            </tr>`;
        }).join('');
    },

    async cancelQuery(pid) {
        try { await apiProtected(`/dashboard/cancel/${pid}`, { method: 'POST' }); showToast(`query ${pid} cancelled`, 'success'); }
        catch (e) { /* handled */ }
    },

    async terminateQuery(pid) {
        if (!await showConfirm('terminate process', `Terminate PID ${pid}?`, { danger: true, confirmText: 'terminate' })) return;
        try { await apiProtected(`/dashboard/terminate/${pid}`, { method: 'POST' }); showToast(`backend ${pid} terminated`, 'success'); }
        catch (e) { /* handled */ }
    },
};
