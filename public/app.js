const App = {
    config: { deepThinking: false, internetSearch: false, defaultModel: 'deep_seek_v3' },
    currentTab: 'dashboard',

    init() {
        this.loadConfig();
        this.checkStatus();
        setInterval(() => this.checkStatus(), 30000);
        // tab switching
        document.querySelectorAll('.tab').forEach(tab => {
            tab.addEventListener('click', (e) => this.switchTab(tab.dataset.panel));
        });
    },

    switchTab(name) {
        this.currentTab = name;
        document.querySelectorAll('.tab').forEach(t => t.classList.toggle('active', t.dataset.panel === name));
        document.querySelectorAll('.panel').forEach(p => p.classList.toggle('active', p.id === 'panel-' + name));
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
        if (!message) return;
        const requestBody = {
            model: this.config.defaultModel,
            messages: [{ role: 'user', content: message.value }],
            stream: false
        };
        document.getElementById('requestResult').textContent = JSON.stringify(requestBody, null, 2);
        document.getElementById('responseResult').textContent = '请求中...';
        try {
            const response = await fetch('/v1/chat/completions', {
                method: 'POST', headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(requestBody)
            });
            const data = await response.json();
            document.getElementById('responseResult').textContent = JSON.stringify(data, null, 2);
        } catch (error) {
            document.getElementById('responseResult').textContent = '错误: ' + error.message;
        }
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
