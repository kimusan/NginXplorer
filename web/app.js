// Data and State
const state = {
    currentVHost: 'all',
    timeRange: 'live',
    isPaused: false,
    theme: localStorage.getItem('theme') || 'dark',
    token: null,
    history: {
        rps: { times: [], values: [] },
        latency: { times: [], p50: [], p95: [], p99: [] }
    },
    vhosts: new Set(),
    eventSource: null,
    lastStatusCodes: null,
    lastLatencyBuckets: null,
    lastBotTraffic: null
};

// UI Elements
const els = {
    themeToggle: document.getElementById('theme-toggle'),
    themeToggleTopbar: document.getElementById('theme-toggle-topbar'),
    vhostList: document.getElementById('vhost-list'),
    vhostSelect: document.getElementById('vhost-select'),
    pauseToggle: document.getElementById('pause-toggle'),
    timeRangeBtns: document.querySelectorAll('.time-ranges button'),
    loginModal: document.getElementById('login-modal'),
    loginForm: document.getElementById('login-form'),
    loginError: document.getElementById('login-error'),
    
    // Stats
    statRps: document.getElementById('stat-rps'),
    trendRps: document.getElementById('trend-rps'),
    statErrors: document.getElementById('stat-errors'),
    trendErrors: document.getElementById('trend-errors'),
    statLatency: document.getElementById('stat-latency'),
    trendLatency: document.getElementById('trend-latency'),
    statConnections: document.getElementById('stat-connections'),
    trendConnections: document.getElementById('trend-connections'),
    statUv: document.getElementById('stat-uv'),
    
    // Tables
    topPathsThead: document.querySelector('#top-paths-table thead tr'),
    topPathsTbody: document.querySelector('#top-paths-table tbody'),
    topCountriesThead: document.querySelector('#top-countries-table thead tr'),
    topCountriesTbody: document.querySelector('#top-countries-table tbody'),

    // Alerts
    alertsBtn: document.getElementById('alerts-btn'),
    alertsBadge: document.getElementById('alerts-badge'),
    alertsModal: document.getElementById('alerts-modal'),
    closeAlertsBtn: document.getElementById('close-alerts-btn'),
    testAlertBtn: document.getElementById('test-alert-btn'),
    activeAlertsList: document.getElementById('active-alerts-list'),
    recentAlertsList: document.getElementById('recent-alerts-list')
};

// Charts
let rpsChart, latencyChart, statusChart, latencyHistChart, clientTypeChart;

function getChartHeight() {
    return window.innerWidth <= 768 ? (window.innerWidth <= 420 ? 195 : 220) : 280;
}

// Initialize
function init() {
    initTheme();
    initCharts();
    setupEventListeners();
    initPwaInstall();
    checkAuth();
}

function initTheme() {
    document.documentElement.setAttribute('data-theme', state.theme);
}

function getThemeColors() {
    const isDark = (state.theme !== 'light');
    return {
        isDark,
        // High contrast axis text: light gray (#9ca3af) in dark mode, dark slate (#4b5563) in light mode
        textSecondary: isDark ? '#9ca3af' : '#4b5563',
        textPrimary: isDark ? '#f3f4f6' : '#111827',
        bgSurface: isDark ? '#1e1e1e' : '#ffffff',
        gridLine: isDark ? 'rgba(255, 255, 255, 0.08)' : 'rgba(0, 0, 0, 0.08)',
        axisLine: isDark ? 'rgba(255, 255, 255, 0.2)' : 'rgba(0, 0, 0, 0.2)',
    };
}

function toggleTheme() {
    state.theme = state.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', state.theme);
    localStorage.setItem('theme', state.theme);
    
    // Re-draw uPlot charts with updated axis stroke & grid
    if (rpsChart) rpsChart.redraw();
    if (latencyChart) latencyChart.redraw();

    // Re-render ECharts with new theme colors
    updateECharts(state.lastStatusCodes || {}, state.lastLatencyBuckets || [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
    if (clientTypeChart) {
        updateClientTypeChart(state.lastBotTraffic || { human: 0, good_bot: 0, bad_bot: 0 });
    }

    if (statusChart) statusChart.resize();
    if (latencyHistChart) latencyHistChart.resize();
    if (clientTypeChart) clientTypeChart.resize();
}

function initCharts() {
    const rpsEl = document.getElementById('chart-rps');
    const latencyEl = document.getElementById('chart-latency');
    const initialHeight = getChartHeight();
    const initialWidth = (rpsEl && rpsEl.clientWidth) ? rpsEl.clientWidth : (window.innerWidth <= 768 ? window.innerWidth - 48 : 400);

    // uPlot RPS
    const rpsOpts = {
        width: initialWidth,
        height: initialHeight,
        scales: {
            x: { time: true },
            y: {
                auto: true,
                range: (u, min, max) => [0, Math.max(max * 1.15, 1)]
            }
        },
        series: [
            {},
            {
                show: true,
                stroke: "#3b82f6",
                fill: "rgba(59, 130, 246, 0.2)",
                width: 2,
                points: { show: (u, seriesIdx) => Boolean(u.data[seriesIdx] && u.data[seriesIdx].length <= 5) }
            }
        ],
        axes: [
            {
                grid: { show: true, stroke: () => getThemeColors().gridLine },
                ticks: { show: true, stroke: () => getThemeColors().axisLine },
                font: "11px system-ui",
                stroke: () => getThemeColors().textSecondary
            },
            {
                grid: { show: true, stroke: () => getThemeColors().gridLine },
                ticks: { show: true, stroke: () => getThemeColors().axisLine },
                font: "11px system-ui",
                stroke: () => getThemeColors().textSecondary,
                values: (u, vals) => vals.map(v => v != null ? v.toFixed(1) : "")
            }
        ]
    };
    rpsChart = new uPlot(rpsOpts, [[], []], rpsEl);

    // uPlot Latency
    const latencyOpts = {
        width: (latencyEl && latencyEl.clientWidth) ? latencyEl.clientWidth : initialWidth,
        height: initialHeight,
        scales: {
            x: { time: true },
            y: {
                auto: true,
                range: (u, min, max) => [0, Math.max(max * 1.15, 10)]
            }
        },
        series: [
            {},
            { label: "p50", stroke: "#10b981", width: 2, points: { show: (u, seriesIdx) => Boolean(u.data[seriesIdx] && u.data[seriesIdx].length <= 5) } },
            { label: "p95", stroke: "#f59e0b", width: 2, points: { show: (u, seriesIdx) => Boolean(u.data[seriesIdx] && u.data[seriesIdx].length <= 5) } },
            { label: "p99", stroke: "#ef4444", width: 2, points: { show: (u, seriesIdx) => Boolean(u.data[seriesIdx] && u.data[seriesIdx].length <= 5) } }
        ],
        axes: [
            {
                grid: { stroke: () => getThemeColors().gridLine },
                ticks: { stroke: () => getThemeColors().axisLine },
                font: "11px system-ui",
                stroke: () => getThemeColors().textSecondary
            },
            {
                grid: { stroke: () => getThemeColors().gridLine },
                ticks: { stroke: () => getThemeColors().axisLine },
                font: "11px system-ui",
                stroke: () => getThemeColors().textSecondary,
                values: (u, vals) => vals.map(v => v != null ? Math.round(v) + "ms" : "")
            }
        ]
    };
    latencyChart = new uPlot(latencyOpts, [[], [], [], []], latencyEl);

    // ECharts
    statusChart = echarts.init(document.getElementById('chart-status'));
    latencyHistChart = echarts.init(document.getElementById('chart-latency-hist'));
    const clientTypeEl = document.getElementById('chart-client-type');
    if (clientTypeEl) {
        clientTypeChart = echarts.init(clientTypeEl);
    }
    updateECharts({}, [0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);

    // ResizeObserver for fluid, robust responsive resizing without overflow
    const ro = new ResizeObserver((entries) => {
        const height = getChartHeight();
        for (let entry of entries) {
            const width = Math.floor(entry.contentRect.width);
            if (width <= 0) continue;
            if (entry.target.id === 'chart-rps' && rpsChart) {
                rpsChart.setSize({ width, height });
            } else if (entry.target.id === 'chart-latency' && latencyChart) {
                latencyChart.setSize({ width, height });
            } else if (entry.target.id === 'chart-status' && statusChart) {
                statusChart.resize();
            } else if (entry.target.id === 'chart-latency-hist' && latencyHistChart) {
                latencyHistChart.resize();
            } else if (entry.target.id === 'chart-client-type' && clientTypeChart) {
                clientTypeChart.resize();
            }
        }
    });

    if (rpsEl) ro.observe(rpsEl);
    if (latencyEl) ro.observe(latencyEl);
    const statusEl = document.getElementById('chart-status');
    if (statusEl) ro.observe(statusEl);
    const histEl = document.getElementById('chart-latency-hist');
    if (histEl) ro.observe(histEl);
    if (clientTypeEl) ro.observe(clientTypeEl);

    // Fallback on window resize
    window.addEventListener('resize', () => {
        const h = getChartHeight();
        if (rpsEl && rpsChart) rpsChart.setSize({ width: rpsEl.clientWidth || initialWidth, height: h });
        if (latencyEl && latencyChart) latencyChart.setSize({ width: latencyEl.clientWidth || initialWidth, height: h });
        if (statusChart) statusChart.resize();
        if (latencyHistChart) latencyHistChart.resize();
        if (clientTypeChart) clientTypeChart.resize();
    });
}

function updateClientTypeChart(botTraffic) {
    if (!clientTypeChart) return;
    if (botTraffic) state.lastBotTraffic = botTraffic;
    const currentTraffic = botTraffic || state.lastBotTraffic || { human: 0, good_bot: 0, bad_bot: 0 };
    const theme = getThemeColors();

    const human = currentTraffic.human || 0;
    const goodBot = currentTraffic.good_bot || 0;
    const badBot = currentTraffic.bad_bot || 0;
    const total = human + goodBot + badBot;

    const data = [
        { value: human, name: 'Human', itemStyle: { color: '#10b981' } },
        { value: goodBot, name: 'Good Bots', itemStyle: { color: '#3b82f6' } },
        { value: badBot, name: 'Scanners / Bad', itemStyle: { color: '#ef4444' } }
    ];

    clientTypeChart.setOption({
        tooltip: {
            trigger: 'item',
            formatter: '{b}: {c} reqs ({d}%)'
        },
        legend: {
            orient: 'horizontal',
            bottom: 0,
            itemWidth: 10,
            itemHeight: 10,
            textStyle: { color: theme.textSecondary, fontSize: 11 }
        },
        series: [{
            name: 'Client Types',
            type: 'pie',
            radius: ['40%', '68%'],
            center: ['50%', '42%'],
            avoidLabelOverlap: false,
            itemStyle: {
                borderRadius: 6,
                borderColor: theme.bgSurface,
                borderWidth: 2
            },
            label: {
                show: total > 0,
                formatter: '{d}%',
                color: theme.textPrimary,
                fontSize: 11
            },
            data: total > 0 ? data : [
                { value: 1, name: 'Awaiting traffic', itemStyle: { color: 'rgba(128,128,128,0.2)' } }
            ]
        }]
    });
}

function updateECharts(statusCodes, latencyBuckets) {
    const theme = getThemeColors();
    if (statusCodes) state.lastStatusCodes = statusCodes;
    if (latencyBuckets) state.lastLatencyBuckets = latencyBuckets;

    const currentStatusCodes = statusCodes || state.lastStatusCodes;
    const currentLatencyBuckets = latencyBuckets || state.lastLatencyBuckets;

    if (statusChart && currentStatusCodes) {
        const statusData = [
            { value: currentStatusCodes['2xx'] || 0, name: '2xx', itemStyle: { color: '#10b981' } },
            { value: currentStatusCodes['3xx'] || 0, name: '3xx', itemStyle: { color: '#3b82f6' } },
            { value: currentStatusCodes['4xx'] || 0, name: '4xx', itemStyle: { color: '#f59e0b' } },
            { value: currentStatusCodes['5xx'] || 0, name: '5xx', itemStyle: { color: '#ef4444' } }
        ];
        statusChart.setOption({
            tooltip: { trigger: 'item' },
            series: [{
                type: 'pie',
                radius: ['40%', '70%'],
                data: statusData,
                itemStyle: {
                    borderRadius: 4,
                    borderColor: theme.bgSurface,
                    borderWidth: 2
                },
                label: { color: theme.textPrimary }
            }]
        });
    }

    if (latencyHistChart && currentLatencyBuckets) {
        const latAxis = ['<10ms', '10-20', '20-50', '50-100', '100-200', '200-500', '500-1s', '1-2s', '2-5s', '>5s'];
        latencyHistChart.setOption({
            tooltip: {
                trigger: 'axis',
                formatter: (params) => {
                    const p = params[0];
                    return `${p.name}: <b>${(p.value || 0).toLocaleString()}</b> reqs`;
                }
            },
            grid: {
                top: 20,
                bottom: 35,
                left: 45,
                right: 15
            },
            xAxis: {
                type: 'category',
                data: latAxis,
                axisLabel: {
                    color: theme.textSecondary,
                    fontSize: 10,
                    interval: 0,
                    rotate: 25
                },
                axisLine: { lineStyle: { color: theme.axisLine } }
            },
            yAxis: {
                type: 'value',
                minInterval: 1,
                axisLabel: { color: theme.textSecondary, fontSize: 10 },
                splitLine: { lineStyle: { color: theme.gridLine } }
            },
            series: [{
                type: 'bar',
                data: currentLatencyBuckets,
                itemStyle: {
                    color: '#8b5cf6',
                    borderRadius: [4, 4, 0, 0]
                }
            }]
        });
    }
}

function setupEventListeners() {
    if (els.themeToggle) els.themeToggle.addEventListener('click', toggleTheme);
    if (els.themeToggleTopbar) els.themeToggleTopbar.addEventListener('click', toggleTheme);
    
    els.pauseToggle.addEventListener('click', () => {
        state.isPaused = !state.isPaused;
        const icon = state.isPaused ? '▶️' : '⏸️';
        const label = state.isPaused ? ' Resume' : ' Pause';
        els.pauseToggle.innerHTML = `${icon}<span class="btn-text">${label}</span>`;
    });

    els.timeRangeBtns.forEach(btn => {
        btn.addEventListener('click', (e) => {
            els.timeRangeBtns.forEach(b => b.classList.remove('active'));
            e.target.classList.add('active');
            state.timeRange = e.target.dataset.range;
            if (state.timeRange !== 'live') {
                updateCountriesTable([]);
                updateTable([]);
                fetchHistory();
            } else {
                const connTitle = els.statConnections.parentElement.querySelector('.stat-title');
                if (connTitle) connTitle.textContent = 'Active Connections';
                const uvTitle = els.statUv.parentElement.querySelector('.stat-title');
                if (uvTitle) uvTitle.textContent = 'Visitors (5m)';
                state.history = {
                    rps: { times: [], values: [] },
                    latency: { times: [], p50: [], p95: [], p99: [] }
                };
            }
        });
    });

    els.vhostSelect.addEventListener('change', (e) => {
        setVHost(e.target.value);
    });

    els.loginForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const username = document.getElementById('username').value;
        const password = document.getElementById('password').value;
        try {
            const res = await fetch('/api/v1/auth/login', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, password })
            });
            if (res.ok) {
                els.loginModal.classList.remove('show');
                connectSSE();
            } else {
                els.loginError.textContent = 'Invalid credentials';
            }
        } catch (err) {
            els.loginError.textContent = 'Connection error';
        }
    });

    if (els.alertsBtn) {
        els.alertsBtn.addEventListener('click', () => {
            if (els.alertsModal) {
                els.alertsModal.classList.add('show');
                fetchAlerts();
            }
        });
    }

    if (els.closeAlertsBtn) {
        els.closeAlertsBtn.addEventListener('click', () => {
            if (els.alertsModal) els.alertsModal.classList.remove('show');
        });
    }

    if (els.testAlertBtn) {
        els.testAlertBtn.addEventListener('click', sendTestAlert);
    }

    window.addEventListener('click', (e) => {
        if (els.alertsModal && e.target === els.alertsModal) {
            els.alertsModal.classList.remove('show');
        }
    });
}

async function checkAuth() {
    try {
        const res = await fetch('/api/v1/auth/check');
        if (res.status === 401) {
            els.loginModal.classList.add('show');
        } else {
            connectSSE();
        }
    } catch (err) {
        console.error('Auth check failed', err);
        setTimeout(checkAuth, 5000);
    }
}

function connectSSE() {
    if (state.eventSource) {
        state.eventSource.close();
    }
    
    let url = '/api/v1/stream';
    if (state.token) {
        url += `?token=${state.token}`;
    }

    state.eventSource = new EventSource(url);

    state.eventSource.addEventListener('snapshot', (e) => {
        try {
            const data = JSON.parse(e.data);
            updateVHostList(data.vhosts);
            if (data.history && data.history[state.currentVHost]) {
                processHistory(data.history[state.currentVHost]);
            }
        } catch (err) {
            console.error('Error parsing snapshot', err);
        }
    });

    state.eventSource.addEventListener('metrics', (e) => {
        if (state.isPaused) return;
        try {
            const data = JSON.parse(e.data);
            if (data.vhosts) {
                const names = Object.keys(data.vhosts);
                const hasNew = names.some(name => !state.vhosts.has(name));
                if (hasNew) {
                    updateVHostList(Array.from(new Set([...state.vhosts, ...names])));
                }
            }
            processMetrics(data);
        } catch (err) {
            console.error('Error parsing metrics', err);
        }
    });

    state.eventSource.addEventListener('alerts', (e) => {
        try {
            const data = JSON.parse(e.data);
            renderAlerts(data);
        } catch (err) {
            console.error('Error parsing alerts event', err);
        }
    });

    state.eventSource.onerror = (e) => {
        console.error('SSE Error', e);
        state.eventSource.close();
        setTimeout(connectSSE, 5000);
    };
}

function updateVHostList(vhosts) {
    if (!vhosts) return;
    
    const newVHosts = new Set(vhosts);
    if (newVHosts.size === 0) return;
    
    els.vhostList.innerHTML = `<li data-vhost="all" class="${state.currentVHost === 'all' ? 'active' : ''}">All VHosts</li>`;
    els.vhostSelect.innerHTML = `<option value="all">All VHosts</option>`;
    
    newVHosts.forEach(v => {
        state.vhosts.add(v);
        
        const li = document.createElement('li');
        li.dataset.vhost = v;
        li.textContent = v;
        if (state.currentVHost === v) li.classList.add('active');
        els.vhostList.appendChild(li);
        
        const opt = document.createElement('option');
        opt.value = v;
        opt.textContent = v;
        if (state.currentVHost === v) opt.selected = true;
        els.vhostSelect.appendChild(opt);
    });

    els.vhostList.querySelectorAll('li').forEach(li => {
        li.addEventListener('click', () => {
            setVHost(li.dataset.vhost);
        });
    });
}

function setVHost(vhost) {
    state.currentVHost = vhost;
    
    els.vhostList.querySelectorAll('li').forEach(li => {
        li.classList.toggle('active', li.dataset.vhost === vhost);
    });
    els.vhostSelect.value = vhost;
    
    state.history.rps.times = [];
    state.history.rps.values = [];
    state.history.latency.times = [];
    state.history.latency.p50 = [];
    state.history.latency.p95 = [];
    state.history.latency.p99 = [];
    
    fetchHistory();
}

async function fetchHistory() {
    try {
        const res = await fetch(`/api/v1/history?vhost=${state.currentVHost}&range=${state.timeRange}`);
        if (res.ok) {
            const data = await res.json();
            processHistory(data);
        }
    } catch (err) {
        console.error('Failed to fetch history', err);
    }
}

function processHistory(data) {
    if (!data) return;
    
    if (data.rps && data.rps.length > 0) {
        let times = data.rps.map(p => p.ts || p[0]);
        let values = data.rps.map(p => p.value !== undefined ? p.value : p[1]);
        if (state.timeRange === 'live' && times.length > 60) {
            times = times.slice(-60);
            values = values.slice(-60);
        }
        state.history.rps.times = times;
        state.history.rps.values = values;
        rpsChart.setData([times, values]);
    } else if (state.timeRange !== 'live') {
        state.history.rps.times = [];
        state.history.rps.values = [];
        rpsChart.setData([[], []]);
    }
    
    if (data.latency_p95 && data.latency_p95.length > 0) {
        let times = data.latency_p95.map(p => p.ts || p[0]);
        let p95Vals = data.latency_p95.map(p => p.value !== undefined ? p.value : p[1]);
        let p50Vals = data.latency_p50 ? data.latency_p50.map(p => p.value !== undefined ? p.value : p[1]) : p95Vals.map(v => v * 0.7);
        let p99Vals = data.latency_p99 ? data.latency_p99.map(p => p.value !== undefined ? p.value : p[1]) : p95Vals.map(v => v * 1.3);

        if (state.timeRange === 'live' && times.length > 60) {
            times = times.slice(-60);
            p50Vals = p50Vals.slice(-60);
            p95Vals = p95Vals.slice(-60);
            p99Vals = p99Vals.slice(-60);
        }

        state.history.latency.times = times;
        state.history.latency.p50 = p50Vals;
        state.history.latency.p95 = p95Vals;
        state.history.latency.p99 = p99Vals;
        latencyChart.setData([times, p50Vals, p95Vals, p99Vals]);
    } else if (state.timeRange !== 'live') {
        state.history.latency.times = [];
        state.history.latency.p50 = [];
        state.history.latency.p95 = [];
        state.history.latency.p99 = [];
        latencyChart.setData([[], [], [], []]);
    }

    // Update cards and status breakdown from historical summary
    if (data.summary) {
        const s = data.summary;
        els.statRps.textContent = (s.avg_rps || 0).toFixed(1);
        els.statErrors.textContent = (s.error_rate || 0).toFixed(2) + '%';
        els.statLatency.textContent = (s.avg_latency || 0).toFixed(1) + 'ms';
        els.statConnections.textContent = s.total_requests ? s.total_requests.toLocaleString() : '0';
        // Label active connections card as Total Requests during historical view
        const connTitle = els.statConnections.parentElement.querySelector('.stat-title');
        if (connTitle) connTitle.textContent = state.timeRange === 'live' ? 'Active Connections' : 'Total Requests';
        const uvFormatted = s.unique_visitors ? s.unique_visitors.toLocaleString() : '0';
        els.statUv.textContent = uvFormatted;
        const uvTitle = els.statUv.parentElement.querySelector('.stat-title');
        if (uvTitle) uvTitle.textContent = state.timeRange === 'live' ? 'Visitors (5m)' : 'Unique Visitors';

        if (s.status_codes || s.latency_buckets) {
            updateECharts(s.status_codes, s.latency_buckets || null);
        }
        if (s.bot_traffic) {
            updateClientTypeChart(s.bot_traffic);
        }
        if (s.top_countries && s.top_countries.length > 0) {
            updateCountriesTable(s.top_countries);
        } else {
            updateCountriesTable([]);
        }
        if (s.top_paths && s.top_paths.length > 0) {
            updateTable(s.top_paths);
        } else {
            updateTable([]);
        }
    } else {
        updateCountriesTable([]);
        updateTable([]);
    }
}

function processMetrics(data) {
    if (state.timeRange !== 'live') return;

    const ts = new Date(data.timestamp).getTime() / 1000;
    
    let metrics;
    if (state.currentVHost === 'all') {
        metrics = aggregateVHosts(data.vhosts);
        if (data.global) metrics.active_connections = data.global.active_connections;
    } else {
        metrics = data.vhosts && data.vhosts[state.currentVHost];
        if (!metrics) return;
        if (data.global) metrics.active_connections = data.global.active_connections;
    }

    updateCards(metrics);
    updateTimeSeries(ts, metrics);
    updateECharts(metrics.status_codes, metrics.latency_buckets);
    updateClientTypeChart(metrics.bot_traffic);
    updateTable(metrics.top_paths);
    updateCountriesTable(metrics.top_countries);
}

function aggregateVHosts(vhosts) {
    const agg = {
        rps: 0,
        error_rate: 0,
        latency: { p50: 0, p95: 0, p99: 0, avg: 0 },
        latency_buckets: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0],
        status_codes: { '2xx': 0, '3xx': 0, '4xx': 0, '5xx': 0 },
        unique_visitors: 0,
        bot_traffic: { human: 0, good_bot: 0, bad_bot: 0 },
        top_paths: [],
        top_countries: []
    };
    if (!vhosts) return agg;
    
    let totalErrors = 0, totalReqs = 0;
    let maxP50 = 0, maxP95 = 0, maxP99 = 0;
    let weightedAvgLatencySum = 0, latencyWeight = 0;
    
    for (const [name, v] of Object.entries(vhosts)) {
        agg.rps += v.rps || 0;
        totalReqs += v.rps || 0;
        totalErrors += ((v.error_rate || 0) * (v.rps || 0));
        agg.unique_visitors += v.unique_visitors || 0;
        
        if (v.status_codes) {
            agg.status_codes['2xx'] += v.status_codes['2xx'] || 0;
            agg.status_codes['3xx'] += v.status_codes['3xx'] || 0;
            agg.status_codes['4xx'] += v.status_codes['4xx'] || 0;
            agg.status_codes['5xx'] += v.status_codes['5xx'] || 0;
        }

        if (v.bot_traffic) {
            agg.bot_traffic.human += v.bot_traffic.human || 0;
            agg.bot_traffic.good_bot += v.bot_traffic.good_bot || 0;
            agg.bot_traffic.bad_bot += v.bot_traffic.bad_bot || 0;
        }

        if (v.latency_buckets) {
            for (let i = 0; i < 10; i++) {
                agg.latency_buckets[i] += (v.latency_buckets[i] || 0);
            }
        }

        if (v.latency) {
            const w = v.rps > 0 ? v.rps : 1;
            if ((v.latency.avg || 0) > 0 || (v.latency.p95 || 0) > 0) {
                weightedAvgLatencySum += (v.latency.avg || 0) * w;
                latencyWeight += w;
                if ((v.latency.p50 || 0) > maxP50) maxP50 = v.latency.p50;
                if ((v.latency.p95 || 0) > maxP95) maxP95 = v.latency.p95;
                if ((v.latency.p99 || 0) > maxP99) maxP99 = v.latency.p99;
            }
        }

        if (v.top_paths) {
            v.top_paths.forEach(tp => {
                agg.top_paths.push({
                    ...tp,
                    vhost: tp.vhost || name
                });
            });
        }

        if (v.top_countries) {
            v.top_countries.forEach(tc => {
                let existing = agg.top_countries.find(item => item.code === tc.code);
                if (existing) {
                    existing.count += tc.count || 0;
                    existing.rps += tc.rps || 0;
                } else {
                    agg.top_countries.push({ ...tc });
                }
            });
        }
    }
    
    if (totalReqs > 0) agg.error_rate = totalErrors / totalReqs;
    if (latencyWeight > 0) {
        agg.latency = {
            p50: maxP50,
            p95: maxP95,
            p99: maxP99,
            avg: weightedAvgLatencySum / latencyWeight
        };
    }

    // Sort combined top paths across all vhosts by RPS descending and limit to top 10
    agg.top_paths.sort((a, b) => (b.rps || 0) - (a.rps || 0));
    agg.top_paths = agg.top_paths.slice(0, 10);

    // Compute percentage and sort top countries
    const totalCountryCount = agg.top_countries.reduce((acc, c) => acc + (c.count || 0), 0);
    agg.top_countries.forEach(c => {
        c.percentage = totalCountryCount > 0 ? (c.count / totalCountryCount) * 100 : 0;
    });
    agg.top_countries.sort((a, b) => (b.count || 0) - (a.count || 0));
    agg.top_countries = agg.top_countries.slice(0, 10);
    
    return agg;
}

function updateCards(m) {
    els.statRps.textContent = (m.rps || 0).toFixed(1);
    els.statErrors.textContent = (m.error_rate || 0).toFixed(2) + '%';
    els.statLatency.textContent = (m.latency?.avg || 0).toFixed(1) + 'ms';
    els.statConnections.textContent = m.active_connections || 0;
    const uvVal = m.unique_visitors || 0;
    els.statUv.textContent = uvVal;
}

function updateTimeSeries(ts, m) {
    if (state.timeRange !== 'live') return;
    
    const rpsHist = state.history.rps;
    rpsHist.times.push(ts);
    rpsHist.values.push(m.rps || 0);
    
    const latHist = state.history.latency;
    latHist.times.push(ts);
    latHist.p50.push(m.latency?.p50 || 0);
    latHist.p95.push(m.latency?.p95 || 0);
    latHist.p99.push(m.latency?.p99 || 0);
    
    while (rpsHist.times.length > 60) {
        rpsHist.times.shift();
        rpsHist.values.shift();
    }
    while (latHist.times.length > 60) {
        latHist.times.shift();
        latHist.p50.shift();
        latHist.p95.shift();
        latHist.p99.shift();
    }
    
    rpsChart.setData([rpsHist.times, rpsHist.values]);
    latencyChart.setData([latHist.times, latHist.p50, latHist.p95, latHist.p99]);
}

function updateTable(paths) {
    const isAll = state.currentVHost === 'all';
    const isLive = state.timeRange === 'live';
    const reqHeader = isLive ? 'Req/s' : 'Visits';
    
    // Update headers dynamically
    if (els.topPathsThead) {
        if (isAll) {
            els.topPathsThead.innerHTML = `
                <th>VHost</th>
                <th>Path</th>
                <th>${reqHeader}</th>
                <th>Avg Latency</th>
                <th>2xx %</th>
            `;
        } else {
            els.topPathsThead.innerHTML = `
                <th>Path</th>
                <th>${reqHeader}</th>
                <th>Avg Latency</th>
                <th>2xx %</th>
            `;
        }
    }

    if (!paths || paths.length === 0) {
        const colSpan = isAll ? 5 : 4;
        els.topPathsTbody.innerHTML = `<tr><td colspan="${colSpan}">No data</td></tr>`;
        return;
    }
    
    const limit = isAll ? 10 : 6;
    els.topPathsTbody.innerHTML = paths.slice(0, limit).map(p => {
        const countVal = (p.count !== undefined && p.count !== null) ? Number(p.count).toLocaleString() : (p.rps ? p.rps.toFixed(1) : '0');
        const metricDisplay = isLive ? (p.rps || 0).toFixed(1) : countVal;
        return `
        <tr>
            ${isAll ? `<td style="font-weight:600; color:var(--accent);">${p.vhost || '-'}</td>` : ''}
            <td>${p.path}</td>
            <td>${metricDisplay}</td>
            <td>${(p.avg_latency || 0).toFixed(1)}ms</td>
            <td>${(p.status_2xx || 0).toFixed(0)}%</td>
        </tr>
    `;
    }).join('');
}

function updateCountriesTable(countries) {
    if (!els.topCountriesTbody) return;

    const isLive = state.timeRange === 'live';
    if (els.topCountriesThead) {
        els.topCountriesThead.innerHTML = `
            <th>Country</th>
            <th>${isLive ? 'Req/s' : 'Visits'}</th>
            <th>Share</th>
        `;
    }

    if (!countries || countries.length === 0) {
        els.topCountriesTbody.innerHTML = `<tr><td colspan="3" class="text-muted" style="text-align: center; padding: 1rem; color: var(--text-muted);">No country data yet</td></tr>`;
        return;
    }

    const limit = 8;
    els.topCountriesTbody.innerHTML = countries.slice(0, limit).map(c => {
        const countVal = (c.count !== undefined && c.count !== null) ? Number(c.count).toLocaleString() : (c.rps ? c.rps.toFixed(1) : '0');
        const metricDisplay = isLive ? (c.rps || 0).toFixed(1) : countVal;
        return `
        <tr>
            <td>
                <span style="font-size: 1.1rem; margin-right: 0.35rem;">${c.flag || '🌐'}</span>
                <span style="font-weight: 500;">${c.name || c.code || 'Unknown'}</span>
                <span class="text-muted" style="font-size: 0.75rem; margin-left: 0.25rem; color: var(--text-muted);">(${c.code || '?'})</span>
            </td>
            <td>${metricDisplay}</td>
            <td style="min-width: 90px;">
                <div style="display: flex; align-items: center; gap: 0.4rem;">
                    <div style="flex: 1; height: 6px; background: var(--border-color); border-radius: 3px; overflow: hidden;">
                        <div style="width: ${Math.min(100, Math.max(0, c.percentage || 0))}%; height: 100%; background: var(--accent); border-radius: 3px;"></div>
                    </div>
                    <span style="font-size: 0.75rem; width: 35px; text-align: right; color: var(--text-secondary);">${(c.percentage || 0).toFixed(1)}%</span>
                </div>
            </td>
        </tr>
    `;
    }).join('');
}

function renderAlerts(data) {
    if (!data) return;
    
    const activeCount = data.active ? data.active.length : 0;
    if (els.alertsBadge) {
        els.alertsBadge.textContent = activeCount;
        if (activeCount > 0) {
            els.alertsBadge.classList.add('badge-firing');
        } else {
            els.alertsBadge.classList.remove('badge-firing');
        }
    }

    if (els.activeAlertsList) {
        if (!data.active || data.active.length === 0) {
            els.activeAlertsList.innerHTML = '<p class="text-muted" style="font-size: 0.85rem; color: var(--text-secondary);">No active alerts. All systems healthy.</p>';
        } else {
            els.activeAlertsList.innerHTML = data.active.map(a => `
                <div class="alert-item firing">
                    <div class="alert-item-header">
                        <span>🚨 ${a.rule_name} (${a.vhost})</span>
                        <span style="color: var(--color-5xx, #e02424); font-size: 0.75rem;">FIRING</span>
                    </div>
                    <div class="alert-item-body">
                        <div>${a.message}</div>
                        <div style="font-size: 0.72rem; margin-top: 0.2rem; color: var(--text-muted);">${a.details || ''} • Started ${new Date(a.started_at).toLocaleTimeString()}</div>
                    </div>
                </div>
            `).join('');
        }
    }

    if (els.recentAlertsList) {
        if (!data.recent || data.recent.length === 0) {
            els.recentAlertsList.innerHTML = '<p class="text-muted" style="font-size: 0.85rem; color: var(--text-secondary);">No recent alert events.</p>';
        } else {
            els.recentAlertsList.innerHTML = data.recent.slice(0, 10).map(a => {
                const isFiring = a.state === 'firing';
                const icon = isFiring ? '🚨' : '✅';
                const stateClass = isFiring ? 'firing' : 'resolved';
                const stateText = isFiring ? 'FIRING' : 'RESOLVED';
                const time = isFiring ? new Date(a.started_at).toLocaleTimeString() : (a.resolved_at ? new Date(a.resolved_at).toLocaleTimeString() : '');
                return `
                    <div class="alert-item ${stateClass}">
                        <div class="alert-item-header">
                            <span>${icon} ${a.rule_name} (${a.vhost})</span>
                            <span style="font-size: 0.75rem; color: ${isFiring ? 'var(--color-5xx, #e02424)' : 'var(--color-2xx, #0e9f6e)'}">${stateText}</span>
                        </div>
                        <div class="alert-item-body">
                            <div>${a.message}</div>
                            <div style="font-size: 0.72rem; margin-top: 0.2rem; color: var(--text-muted);">${time}</div>
                        </div>
                    </div>
                `;
            }).join('');
        }
    }
}

async function fetchAlerts() {
    try {
        const res = await fetch('/api/v1/alerts');
        if (!res.ok) return;
        const data = await res.json();
        renderAlerts(data);
    } catch (err) {
        console.error('Failed to fetch alerts', err);
    }
}

async function sendTestAlert() {
    if (!els.testAlertBtn) return;
    els.testAlertBtn.disabled = true;
    els.testAlertBtn.textContent = 'Sending...';
    try {
        const res = await fetch('/api/v1/alerts/test', { method: 'POST' });
        if (res.ok) {
            alert('Test notification sent successfully to all configured channels!');
            fetchAlerts();
        } else {
            const err = await res.json().catch(() => ({}));
            alert('Test notification failed: ' + (err.error || 'unknown error'));
        }
    } catch (err) {
        alert('Test notification request failed: ' + err);
    } finally {
        els.testAlertBtn.disabled = false;
        els.testAlertBtn.textContent = 'Send Test Notification';
    }
}

// PWA Mobile Install Controller
let deferredPrompt = null;

function isPwaStandalone() {
    return window.matchMedia('(display-mode: standalone)').matches ||
           window.navigator.standalone === true ||
           document.referrer.includes('android-app://');
}

function isMobileDevice() {
    return /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent) ||
           (window.matchMedia('(max-width: 768px)').matches && ('ontouchstart' in window || navigator.maxTouchPoints > 0));
}

function isIOS() {
    return /iPad|iPhone|iPod/.test(navigator.userAgent) && !window.MSStream;
}

function isInstallPromptDismissed() {
    const dismissed = localStorage.getItem('nginxplorer_pwa_dismissed');
    if (!dismissed) return false;
    const dismissedTime = parseInt(dismissed, 10);
    // Snooze for 7 days
    return (Date.now() - dismissedTime) < 7 * 24 * 60 * 60 * 1000;
}

function initPwaInstall() {
    const banner = document.getElementById('pwa-install-banner');
    const desc = document.getElementById('pwa-banner-desc');
    const installBtn = document.getElementById('pwa-install-btn');
    const dismissBtn = document.getElementById('pwa-dismiss-btn');
    const navInstall = document.getElementById('pwa-install-nav');

    if (!banner || !installBtn || !dismissBtn) return;

    // If already running in standalone / installed PWA mode, do nothing
    if (isPwaStandalone()) {
        if (navInstall) navInstall.style.display = 'none';
        return;
    }

    const showBanner = (isIosPrompt = false) => {
        if (isInstallPromptDismissed()) return;
        if (isIosPrompt) {
            if (desc) desc.innerHTML = 'Tap the Share button <b style="color:var(--text-primary);">⎋</b> and select <b style="color:var(--text-primary);">"Add to Home Screen ➕"</b>';
            installBtn.textContent = 'Got it';
        } else {
            if (desc) desc.textContent = 'Add to Home Screen for fast, fullscreen dashboard access';
            installBtn.textContent = 'Install';
        }
        banner.style.display = 'flex';
        // Trigger transition
        setTimeout(() => banner.classList.add('visible'), 50);
    };

    const hideBanner = (userDismissed = false) => {
        banner.classList.remove('visible');
        setTimeout(() => {
            banner.style.display = 'none';
        }, 400);
        if (userDismissed) {
            localStorage.setItem('nginxplorer_pwa_dismissed', Date.now().toString());
        }
    };

    // Chromium / Android beforeinstallprompt
    window.addEventListener('beforeinstallprompt', (e) => {
        e.preventDefault();
        deferredPrompt = e;

        // Show sidebar install button
        if (navInstall) navInstall.style.display = 'block';

        // Automatically prompt on mobile devices if not dismissed
        if (isMobileDevice() && !isInstallPromptDismissed()) {
            // Delay slightly so the user sees the dashboard before prompting
            setTimeout(() => {
                showBanner(false);
            }, 1500);
        }
    });

    // Installed event
    window.addEventListener('appinstalled', () => {
        deferredPrompt = null;
        hideBanner(false);
        if (navInstall) navInstall.style.display = 'none';
    });

    // Handle Install Button click
    installBtn.addEventListener('click', async () => {
        if (deferredPrompt) {
            deferredPrompt.prompt();
            const { outcome } = await deferredPrompt.userChoice;
            if (outcome === 'accepted') {
                hideBanner(false);
            } else {
                hideBanner(true);
            }
            deferredPrompt = null;
        } else if (isIOS()) {
            hideBanner(true);
        } else {
            hideBanner(true);
        }
    });

    // Handle Dismiss Button click
    dismissBtn.addEventListener('click', () => {
        hideBanner(true);
    });

    // Handle Sidebar "Install App" click
    if (navInstall) {
        navInstall.addEventListener('click', async () => {
            if (deferredPrompt) {
                deferredPrompt.prompt();
                const { outcome } = await deferredPrompt.userChoice;
                if (outcome === 'accepted') {
                    hideBanner(false);
                }
                deferredPrompt = null;
            } else if (isIOS()) {
                // Show iOS instructions banner
                showBanner(true);
            } else {
                alert('To install NginXplorer, use your browser menu (⋮ or ⎋) and select "Add to Home screen" or "Install App".');
            }
        });
    }

    // iOS mobile Safari handling: beforeinstallprompt is not supported
    if (isIOS() && isMobileDevice() && !isPwaStandalone()) {
        if (navInstall) navInstall.style.display = 'block';
        if (!isInstallPromptDismissed()) {
            setTimeout(() => {
                showBanner(true);
            }, 2500);
        }
    }
}

// Start
document.addEventListener('DOMContentLoaded', init);

// Register PWA Service Worker
if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
        navigator.serviceWorker.register('/sw.js').catch((err) => {
            console.warn('PWA service worker registration failed:', err);
        });
    });
}
