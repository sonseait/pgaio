// PGAIO — Log Stream Module

const LogStream = {
    _ws: null,
    _autoScroll: true,

    async render(container) {
        container.innerHTML = `
            <div class="flex-between mb-8">
                <span class="card-title" style="margin:0">postgresql log stream</span>
                <div class="flex gap-4">
                    <label class="flex-center gap-4 mono-xs dim">
                        <input type="checkbox" id="log-autoscroll" checked /> auto-scroll
                    </label>
                    <button onclick="LogStream.clear()" class="btn btn-sm">
                        <i data-lucide="trash-2" class="icon-sm"></i> clear
                    </button>
                </div>
            </div>
            <div class="card" id="log-card" style="padding:0">
                <pre id="log-output" style="
                    margin:0; padding:8px; font-size:11px; line-height:1.6;
                    height:calc(100vh - 116px); overflow:auto;
                    color:var(--text-1); white-space:pre-wrap; word-break:break-all;
                ">loading...</pre>
            </div>
        `;
        lucide.createIcons();

        document.getElementById('log-autoscroll').addEventListener('change', (e) => {
            this._autoScroll = e.target.checked;
            if (this._autoScroll) this.scrollToBottom();
        });

        await this.loadRecent();
        this.connectWs();
    },

    async loadRecent() {
        try {
            const res = await api('/logs?n=500');
            const lines = res.data || [];
            const el = document.getElementById('log-output');
            if (el) {
                el.innerHTML = lines.map(l => this.colorize(l)).join('') || 'no logs';
                this.scrollToBottom();
            }
        } catch (e) {
            const el = document.getElementById('log-output');
            if (el) el.textContent = 'error loading logs: ' + e.message;
        }
    },

    connectWs() {
        if (this._ws) { this._ws.close(); this._ws = null; }
        const proto = location.protocol === 'https:' ? 'wss' : 'ws';
        try {
            this._ws = new WebSocket(`${proto}://${location.host}/api/logs/ws`);
        } catch (e) { return; }

        this._ws.onmessage = (e) => {
            try {
                const data = JSON.parse(e.data);
                if (data.line) this.appendLine(data.line);
            } catch (err) { /* skip */ }
        };
        this._ws.onclose = () => { this._ws = null; };
    },

    appendLine(line) {
        const el = document.getElementById('log-output');
        if (!el) return;
        const colored = this.colorize(line);
        el.insertAdjacentHTML('beforeend', colored);
        // Limit to ~2000 lines
        const lines = el.childNodes;
        while (lines.length > 2000) lines[0].remove();
        this.scrollToBottom();
    },

    colorize(line) {
        let cls = '';
        if (/ERROR|FATAL|PANIC/i.test(line)) cls = 'red';
        else if (/WARNING|WARN/i.test(line)) cls = 'yellow';
        else if (/LOG|NOTICE/i.test(line)) cls = 'dim';
        else if (/STATEMENT/i.test(line)) cls = 'accent';

        const escaped = line.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
        return `<span class="${cls}">${escaped}</span>\n`;
    },

    scrollToBottom() {
        if (!this._autoScroll) return;
        requestAnimationFrame(() => {
            const el = document.getElementById('log-output');
            if (el) el.scrollTop = el.scrollHeight;
        });
    },

    clear() {
        const el = document.getElementById('log-output');
        if (el) el.innerHTML = '';
    },

    destroy() {
        if (this._ws) { this._ws.onclose = null; this._ws.close(); this._ws = null; }
    },
};

