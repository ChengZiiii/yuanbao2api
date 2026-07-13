const App = {
    config: { deepThinking: false, internetSearch: false, defaultModel: 'deep_seek_v3' },
    _messages: [],
    currentTab: 'dashboard',

    init() {
        this.loadConfig();
        this.checkStatus();
        this.loadStatus();
        this.loadLogs();
        setInterval(() => this.checkStatus(), 30000);
        setInterval(() => this.loadStatus(), 2000);
        // tab switching
        document.querySelectorAll('.tab').forEach(tab => {
            tab.addEventListener('click', (e) => this.switchTab(tab.dataset.panel));
        });
    },

    switchTab(name) {
        this.currentTab = name;
        document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.panel === name));
        document.querySelectorAll('.panel').forEach(p => p.classList.toggle('active', p.id === 'panel-' + name));
        if (name === 'dashboard') { this.loadStatus(); this.loadLogs(); }
    },

    async loadConfig() {
        try {
            const res = await fetch('/api/config');
            this.config = await res.json();
            this.applyConfigToUI();
        } catch(e) {}
    },

    async saveConfig() {
        try {
            await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(this.config)
            });
        } catch(e) {}
    },

    applyConfigToUI() {
        const dt = document.getElementById('deepThinkingToggle');
        const is = document.getElementById('internetSearchToggle');
        if (dt) dt.classList.toggle('active', this.config.deepThinking);
        if (is) is.classList.toggle('active', this.config.internetSearch);
        const ms = document.getElementById('modelSelect');
        if (ms) ms.value = this.config.defaultModel;
    },

    async checkStatus() {
        const statusEl = document.getElementById('status');
        if (!statusEl) return;
        try {
            const response = await fetch('/health');
            if (response.ok) {
                statusEl.className = 'status online';
                statusEl.innerHTML = '<span class="status-dot"></span><span>服务运行中</span>';
            } else {
                statusEl.className = 'status';
                statusEl.innerHTML = '<span class="status-dot"></span><span>服务异常</span>';
            }
        } catch (error) {
            statusEl.className = 'status';
            statusEl.innerHTML = '<span class="status-dot"></span><span>无法连接</span>';
        }
    },

    toggleFeature(feature) {
        this.config[feature] = !this.config[feature];
        const toggle = document.getElementById(feature + 'Toggle');
        if (toggle) toggle.classList.toggle('active', this.config[feature]);
        this.saveConfig();
    },

    async testAPI() {
        const message = document.getElementById('testMessage');
        if (!message || !message.value.trim()) return;

        const stream = document.getElementById('streamToggle')?.checked || false;
        const compare = document.getElementById('compareToggle')?.checked || false;
        const multiTurn = document.getElementById('multiTurnToggle')?.checked || false;

        const dsEl = document.getElementById('dsResult');
        const hyEl = document.getElementById('hyResult');
        const dsStatus = document.getElementById('dsStatus');
        const hyStatus = document.getElementById('hyStatus');
        const hyBox = document.getElementById('hyBox');

        // Multi-turn: manage message history
        let messages;
        if (multiTurn) {
            messages = [...this._messages, { role: 'user', content: message.value }];
        } else {
            messages = [{ role: 'user', content: message.value }];
            this._messages = [];
        }

        const makeBody = (model) => JSON.stringify({
            model: model,
            messages: messages,
        });

        // Show DeepSeek result box, optionally show Hunyuan
        dsEl.textContent = '请求中...';
        dsStatus.textContent = '';
        if (hyBox) {
            hyBox.style.display = compare ? 'block' : 'none';
        }
        if (hyEl) hyEl.textContent = compare ? '请求中...' : '';
        if (hyStatus) hyStatus.textContent = '';

        try {
            // DeepSeek request
            const dsRes = await fetch('/v1/chat/completions', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: makeBody('deep_seek_v3')
            });
            const dsData = await dsRes.json();
            const dsContent = dsData.choices?.[0]?.message?.content || JSON.stringify(dsData);
            dsEl.textContent = dsContent;
            if (dsStatus) {
                dsStatus.textContent = dsRes.status;
                dsStatus.style.color = dsRes.status === 200 ? '#0f0' : '#f44';
            }

            if (compare) {
                // Hunyuan request (only if compare mode)
                const hyRes = await fetch('/v1/chat/completions', {
                    method: 'POST', headers: { 'Content-Type': 'application/json' },
                    body: makeBody('hunyuan')
                });
                const hyData = await hyRes.json();
                const hyContent = hyData.choices?.[0]?.message?.content || JSON.stringify(hyData);
                hyEl.textContent = hyContent;
                if (hyStatus) {
                    hyStatus.textContent = hyRes.status;
                    hyStatus.style.color = hyRes.status === 200 ? '#0f0' : '#f44';
                }
            }

            // Multi-turn: save the conversation
            if (multiTurn) {
                this._messages.push({ role: 'user', content: message.value });
                const content = dsData.choices?.[0]?.message?.content || '';
                if (content) {
                    this._messages.push({ role: 'assistant', content: content });
                }
            }
        } catch(e) {
            dsEl.textContent = '请求失败: ' + e.message;
        }
    },

    async loadStatus() {
        try {
            const res = await fetch('/api/status');
            const data = await res.json();
            document.getElementById('inflightNum').textContent = data.inflight ?? 0;
            document.getElementById('maxConcurrency').textContent = data.maxConcurrency ?? 1;
            document.getElementById('waitingNum').textContent = data.waiting ?? 0;
            document.getElementById('cooldownNum').textContent = (data.requestCooldownMs ?? 0) + 'ms';
            const maxC = data.maxConcurrency || 1;
            const pct = Math.round((data.inflight || 0) / maxC * 100);
            document.getElementById('usageBar').style.width = Math.min(pct, 100) + '%';
            document.getElementById('usagePct').textContent = Math.min(pct, 100) + '%';
        } catch(e) {}
    },

    async loadLogs() {
        try {
            const res = await fetch('/api/logs');
            const logs = await res.json();
            const tbody = document.getElementById('logBody');
            if (!tbody) return;
            if (!logs || logs.length === 0) {
                tbody.innerHTML = '<tr><td colspan="6" style="color:#666;text-align:center;">暂无数据</td></tr>';
                return;
            }
            tbody.innerHTML = logs.map(log => {
                const cls = log.status >= 400 ? 'status-bad' : log.status >= 300 ? 'status-warn' : 'status-ok';
                return '<tr><td>' + (log.time || '') + '</td><td><span class="method-tag">' + (log.method || '') + '</span></td><td>' + (log.model || '-') + '</td><td><span class="' + cls + '">' + (log.status || '') + '</span></td><td>' + (log.duration || '') + '</td><td style="color:#666;">' + (log.note || '') + '</td></tr>';
            }).join('');
        } catch(e) {}
    },
};

// modelSelect change handler
document.addEventListener('DOMContentLoaded', () => {
    App.init();
    document.getElementById('modelSelect')?.addEventListener('change', function() {
        App.config.defaultModel = this.value;
        App.saveConfig();
    });
});
