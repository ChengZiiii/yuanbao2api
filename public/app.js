const App = {
    config: { deepThinking: false, internetSearch: false, defaultModel: 'deep_seek_v3' },
    apiKey: '',
    _messages: [],
    currentTab: 'dashboard',

    // Helper: return headers with auth if API key is set
    _authHeaders(extra) {
        const h = { 'Content-Type': 'application/json', ...(extra || {}) };
        if (this.apiKey) h['Authorization'] = 'Bearer ' + this.apiKey;
        return h;
    },

    init() {
        this.loadConfig();
        this.checkStatus();
        this.loadStatus();
        this.loadLogs();
        this.loadEnv();  // pre-load API key for test auth
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
        if (name === 'config') { this.loadEnv(); }
        if (name === 'testing') {
            const ts = document.getElementById('testModelSelect');
            if (ts) ts.value = this.config.defaultModel;
        }
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
        const ts = document.getElementById('testModelSelect');
        if (ts) ts.value = this.config.defaultModel;
        const ai = document.getElementById('agentIdInput');
        if (ai) ai.value = this.config.agentId || '';
        const mc = document.getElementById('maxConcurrencyInput');
        if (mc) mc.value = this.config.maxConcurrency ?? '';
        const qt = document.getElementById('queueTimeoutInput');
        if (qt) qt.value = this.config.queueTimeoutSeconds ?? '';
        const cd = document.getElementById('cooldownInput');
        if (cd) cd.value = this.config.requestCooldownMs ?? '';
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

        // Read selected model from the dropdown
        const testModel = document.getElementById('testModelSelect')?.value || 'deep_seek_v3';
        const stream = document.getElementById('streamToggle')?.checked || false;
        const compare = document.getElementById('compareToggle')?.checked || false;
        const multiTurn = document.getElementById('multiTurnToggle')?.checked || false;

        const dsEl = document.getElementById('dsResult');
        const hyEl = document.getElementById('hyResult');
        const dsStatus = document.getElementById('dsStatus');
        const hyStatus = document.getElementById('hyStatus');
        const hyBox = document.getElementById('hyBox');

        // Model display names for headers
        const modelNames = { deep_seek_v3: 'DeepSeek', hunyuan: 'Hunyuan' };
        const secondaryModel = testModel === 'deep_seek_v3' ? 'hunyuan' : 'deep_seek_v3';

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

        // Update result box headers (text only, preserve status span element)
        const dsHeader = dsEl.previousElementSibling;
        if (dsHeader && dsHeader.childNodes[0]) {
            dsHeader.childNodes[0].textContent = '← ' + (modelNames[testModel] || testModel) + ' ';
        }
        if (hyEl) {
            const hyHeader = hyEl.previousElementSibling;
            if (hyHeader && hyHeader.childNodes[0]) {
                hyHeader.childNodes[0].textContent = '← ' + (modelNames[secondaryModel] || secondaryModel) + ' ';
            }
            hyBox.style.display = compare ? 'block' : 'none';
        }

        // Show request in progress
        dsEl.textContent = '请求中...';
        dsStatus.textContent = '';
        if (hyEl) hyEl.textContent = compare ? '请求中...' : '';
        if (hyStatus) hyStatus.textContent = '';

        try {
            // Primary model request
            const dsRes = await fetch('/v1/chat/completions', {
                method: 'POST', headers: this._authHeaders(),
                body: makeBody(testModel)
            });
            const dsData = await dsRes.json();
            const dsContent = dsData.choices?.[0]?.message?.content || JSON.stringify(dsData);
            dsEl.textContent = dsContent;
            if (dsStatus) {
                dsStatus.textContent = dsRes.status;
                dsStatus.style.color = dsRes.status === 200 ? '#0f0' : '#f44';
            }

            if (compare) {
                // Secondary model request (opposite of selected)
                const hyRes = await fetch('/v1/chat/completions', {
                    method: 'POST', headers: this._authHeaders(),
                    body: makeBody(secondaryModel)
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

    async loadEnv() {
        try {
            const res = await fetch('/api/env');
            const data = await res.json();
            this.apiKey = data.apiKey || '';
            document.getElementById('envCookie').textContent = data.yuanbaoCookie || '-';
            document.getElementById('envAgentId').textContent = data.yuanbaoAgentId || '-';
            document.getElementById('envApiKey').textContent = data.apiKey ? data.apiKey.substring(0, 8) + '****' : '-';
            document.getElementById('envPort').textContent = data.port || '-';
            document.getElementById('envGinMode').textContent = data.ginMode || '-';
            document.getElementById('envMaxC').textContent = data.maxConcurrency ?? '-';
            document.getElementById('envQTimeout').textContent = data.queueTimeoutSeconds ?? '-';
            document.getElementById('envCooldown').textContent = (data.requestCooldownMs ?? '-') + 'ms';
        } catch(e) {}
    },

    async saveAgentId() {
        const input = document.getElementById('agentIdInput');
        if (!input || !input.value.trim()) return;
        try {
            await fetch('/api/config', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ agentId: input.value.trim() })
            });
            alert('Agent ID 已更新');
        } catch(e) {
            alert('保存失败: ' + e.message);
        }
    },

    async checkCookie() {
        const el = document.getElementById('cookieResult');
        if (!el) return;
        el.textContent = '检测中...';
        el.style.color = '#888';
        try {
            const res = await fetch('/v1/chat/completions', {
                method: 'POST',
                headers: this._authHeaders(),
                body: JSON.stringify({
                    model: 'deep_seek_v3',
                    messages: [{ role: 'user', content: 'ping' }],
                })
            });
            if (res.status === 200) {
                el.textContent = '✅ 有效';
                el.style.color = '#0f0';
            } else if (res.status === 401) {
                el.textContent = this.apiKey ? '❌ API Key 无效或 Cookie 过期' : '❌ Cookie 过期';
                el.style.color = '#f44';
            } else {
                el.textContent = '⚠️ 返回 ' + res.status;
                el.style.color = '#ffa500';
            }
        } catch(e) {
            el.textContent = '❌ 请求失败';
            el.style.color = '#f44';
        }
    },

    copyEndpoint(btn, text) {
        navigator.clipboard.writeText(text).then(() => {
            btn.textContent = '✅';
            setTimeout(() => btn.textContent = '📋', 1500);
        }).catch(() => {
            // Fallback for non-HTTPS
            const input = btn.previousElementSibling;
            if (input && input.select) {
                input.select();
                document.execCommand('copy');
            }
        });
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

    async saveConcurrency() {
        const maxC = parseInt(document.getElementById('maxConcurrencyInput').value);
        const qTimeout = parseInt(document.getElementById('queueTimeoutInput').value);
        const cooldown = parseInt(document.getElementById('cooldownInput').value);

        if (!maxC || maxC < 1) {
            alert('MAX_CONCURRENCY 必须 ≥ 1');
            return;
        }
        if (!qTimeout || qTimeout < 1) {
            alert('QUEUE_TIMEOUT_SECONDS 必须 ≥ 1');
            return;
        }
        if (isNaN(cooldown) || cooldown < 0) {
            alert('REQUEST_COOLDOWN_MS 必须 ≥ 0');
            return;
        }

        try {
            const res = await fetch('/api/config', {
                method: 'POST',
                headers: this._authHeaders(),
                body: JSON.stringify({
                    maxConcurrency: maxC,
                    queueTimeoutSeconds: qTimeout,
                    requestCooldownMs: cooldown,
                }),
            });
            if (!res.ok) {
                alert('保存失败: HTTP ' + res.status);
                return;
            }
            alert('已保存。点击"重启服务"按钮生效。');
        } catch (e) {
            alert('保存失败: ' + e.message);
        }
    },

    async restartService() {
        if (!confirm('确认重启服务？所有进行中的请求会被中断。')) return;

        const statusEl = document.getElementById('restartStatus');
        statusEl.textContent = '重启中...';
        statusEl.style.color = '#ffa500';

        try {
            const res = await fetch('/api/restart', {
                method: 'POST',
                headers: this._authHeaders(),
            });
            if (!res.ok) {
                statusEl.textContent = '重启请求失败: HTTP ' + res.status;
                statusEl.style.color = '#f44';
                return;
            }
        } catch (e) {
            // 网络中断属于正常情况——服务可能已退出
            console.log('重启请求网络中断（预期行为）:', e.message);
        }

        // 轮询 /health 检测恢复
        const deadline = Date.now() + 30000;
        const poll = async () => {
            if (Date.now() > deadline) {
                statusEl.textContent = '重启超时（30s），请手动检查';
                statusEl.style.color = '#f44';
                return;
            }
            try {
                const r = await fetch('/health', { cache: 'no-store' });
                if (r.ok) {
                    statusEl.textContent = '✅ 服务已恢复';
                    statusEl.style.color = '#0f0';
                    // 重新加载配置 + 状态
                    this.loadConfig();
                    this.loadStatus();
                    return;
                }
            } catch (e) {
                // 服务还启动中
            }
            setTimeout(poll, 1000);
        };
        setTimeout(poll, 1500); // 给重启 bat 留启动时间
    },
};

// modelSelect change handler
document.addEventListener('DOMContentLoaded', () => {
    App.init();
    document.getElementById('modelSelect')?.addEventListener('change', function() {
        App.config.defaultModel = this.value;
        App.saveConfig();
        // Sync testing panel model selector too
        const ts = document.getElementById('testModelSelect');
        if (ts) ts.value = this.value;
    });
});
