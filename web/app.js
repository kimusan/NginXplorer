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
    eventSource: null
};

// UI Elements
const els = {
    themeToggle: document.getElementById('theme-toggle'),
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
    topPathsTbody: document.querySelector('#top-paths-table tbody')
};

// Charts
let rpsChart, latencyChart, statusChart, latencyHistChart;

// Initialize
function init() {
    initTheme();
    initCharts();
    setupEventListeners();
    checkAuth();
}

function initTheme() {
    document.documentElement.setAttribute('data-theme', state.theme);
}

function toggleTheme() {
    state.theme = state.theme === 'dark' ? 'light' : 'dark';
    document.documentElement.setAttribute('data-theme', state.theme);
    localStorage.setItem('theme', state.theme);
    
    // Re-render charts for theme
    if (statusChart) statusChart.resize();
    if (latencyHistChart) latencyHistChart.resize();
}

function initCharts() {
    // uPlot RPS
    const rpsOpts = {
        width: els.statRps.parentElement.parentElement.parentElement.querySelector('#chart-rps').clientWidth || 400,
        height: 300,
        series: [
            {},
            {
                show: true,
                stroke: "#3b82f6",
                fill: "rgba(59, 130, 246, 0.2)",
                width: 2,
            }
        ],
        axes: [
            {
                grid: { show: true, stroke: "rgba(128,128,128,0.2)" },
                font: "12px system-ui",
                stroke: "var(--text-secondary)"
            },
            {
                grid: { show: true, stroke: "rgba(128,128,128,0.2)" },
                font: "12px system-ui",
                stroke: "var(--text-secondary)"
            }
        ]
    };
    rpsChart = new uPlot(rpsOpts, [[], []], document.getElementById('chart-rps'));

    // uPlot Latency
    const latencyOpts = {
        width: els.statRps.parentElement.parentElement.parentElement.querySelector('#chart-latency').clientWidth || 400,
        height: 300,
        series: [
            {},
            { label: "p50", stroke: "#10b981", width: 2 },
            { label: "p95", stroke: "#f59e0b", width: 2 },
            { label: "p99", stroke: "#ef4444", width: 2 }
        ],
        axes: [
            { grid: { stroke: "rgba(128,128,128,0.2)" }, stroke: "var(--text-secondary)" },
            { grid: { stroke: "rgba(128,128,128,0.2)" }, stroke: "var(--text-secondary)" }
        ]
    };
    latencyChart = new uPlot(latencyOpts, [[], [], [], []], document.getElementById('chart-latency'));

    // ECharts
    statusChart = echarts.init(document.getElementById('chart-status'));
    latencyHistChart = echarts.init(document.getElementById('chart-latency-hist'));

    // Resize handlers
    window.addEventListener('resize', () => {
        const rpsWidth = document.getElementById('chart-rps').clientWidth;
        rpsChart.setSize({ width: rpsWidth, height: 300 });
        const latWidth = document.getElementById('chart-latency').clientWidth;
        latencyChart.setSize({ width: latWidth, height: 300 });
        statusChart.resize();
        latencyHistChart.resize();
    });
}

function updateECharts(statusCodes, latencyBuckets) {
    if (!statusCodes) statusCodes = {};
    
    // Status
    const statusData = [
        { value: statusCodes['2xx'] || 0, name: '2xx', itemStyle: { color: '#10b981' } },
        { value: statusCodes['3xx'] || 0, name: '3xx', itemStyle: { color: '#3b82f6' } },
        { value: statusCodes['4xx'] || 0, name: '4xx', itemStyle: { color: '#f59e0b' } },
        { value: statusCodes['5xx'] || 0, name: '5xx', itemStyle: { color: '#ef4444' } }
    ];
    statusChart.setOption({
        tooltip: { trigger: 'item' },
        series: [{
            type: 'pie',
            radius: ['40%', '70%'],
            data: statusData,
            label: { color: 'var(--text-primary)' }
        }]
    });

    // Histogram
    const latData = latencyBuckets || [0, 0, 0, 0, 0, 0, 0, 0, 0, 0];
    const latAxis = ['<10ms', '10-20', '20-50', '50-100', '100-200', '200-500', '500-1s', '1-2s', '2-5s', '>5s'];
    latencyHistChart.setOption({
        tooltip: { trigger: 'axis' },
        xAxis: { type: 'category', data: latAxis, axisLabel: { color: 'var(--text-secondary)' } },
        yAxis: { type: 'value', axisLabel: { color: 'var(--text-secondary)' }, splitLine: { lineStyle: { color: 'rgba(128,128,128,0.2)' } } },
        series: [{ type: 'bar', data: latData, itemStyle: { color: '#8b5cf6' } }]
    });
}

function setupEventListeners() {
    els.themeToggle.addEventListener('click', toggleTheme);
    
    els.pauseToggle.addEventListener('click', () => {
        state.isPaused = !state.isPaused;
        els.pauseToggle.textContent = state.isPaused ? '▶️ Resume' : '⏸️ Pause';
    });

    els.timeRangeBtns.forEach(btn => {
        btn.addEventListener('click', (e) => {
            els.timeRangeBtns.forEach(b => b.classList.remove('active'));
            e.target.classList.add('active');
            state.timeRange = e.target.dataset.range;
            if (state.timeRange !== 'live') {
                fetchHistory();
            } else {
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
    
    if (state.timeRange !== 'live') {
        fetchHistory();
    }
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
    
    if (data.rps) {
        state.history.rps.times = data.rps.map(p => p[0]);
        state.history.rps.values = data.rps.map(p => p[1]);
        rpsChart.setData([state.history.rps.times, state.history.rps.values]);
    }
    
    if (data.latency_p50 && data.latency_p95 && data.latency_p99) {
        state.history.latency.times = data.latency_p50.map(p => p[0]);
        state.history.latency.p50 = data.latency_p50.map(p => p[1]);
        state.history.latency.p95 = data.latency_p95.map(p => p[1]);
        state.history.latency.p99 = data.latency_p99.map(p => p[1]);
        latencyChart.setData([state.history.latency.times, state.history.latency.p50, state.history.latency.p95, state.history.latency.p99]);
    }
}

function processMetrics(data) {
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
    updateECharts(metrics.status_codes, null);
    updateTable(metrics.top_paths);
}

function aggregateVHosts(vhosts) {
    const agg = { rps: 0, error_rate: 0, latency: { p50:0, p95:0, p99:0, avg:0 }, status_codes: { '2xx': 0, '3xx': 0, '4xx': 0, '5xx': 0 }, unique_visitors: 0, top_paths: [] };
    if (!vhosts) return agg;
    
    let totalErrors = 0, totalReqs = 0;
    
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

        if (v.top_paths) {
            v.top_paths.forEach(tp => {
                agg.top_paths.push({
                    ...tp,
                    vhost: tp.vhost || name
                });
            });
        }
    }
    
    if (totalReqs > 0) agg.error_rate = totalErrors / totalReqs;

    // Sort combined top paths across all vhosts by RPS descending and limit to top 10
    agg.top_paths.sort((a, b) => (b.rps || 0) - (a.rps || 0));
    agg.top_paths = agg.top_paths.slice(0, 10);
    
    return agg;
}

function updateCards(m) {
    els.statRps.textContent = (m.rps || 0).toFixed(1);
    els.statErrors.textContent = (m.error_rate || 0).toFixed(2) + '%';
    els.statLatency.textContent = (m.latency?.avg || 0).toFixed(1) + 'ms';
    els.statConnections.textContent = m.active_connections || 0;
    els.statUv.textContent = m.unique_visitors || 0;
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
    
    if (rpsHist.times.length > 60) {
        rpsHist.times.shift();
        rpsHist.values.shift();
        
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
    
    // Update headers dynamically
    if (els.topPathsThead) {
        if (isAll) {
            els.topPathsThead.innerHTML = `
                <th>VHost</th>
                <th>Path</th>
                <th>Req/s</th>
                <th>Avg Latency</th>
                <th>2xx %</th>
            `;
        } else {
            els.topPathsThead.innerHTML = `
                <th>Path</th>
                <th>Req/s</th>
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
    els.topPathsTbody.innerHTML = paths.slice(0, limit).map(p => `
        <tr>
            ${isAll ? `<td style="font-weight:600; color:var(--accent);">${p.vhost || '-'}</td>` : ''}
            <td>${p.path}</td>
            <td>${(p.rps || 0).toFixed(1)}</td>
            <td>${(p.avg_latency || 0).toFixed(1)}ms</td>
            <td>${(p.status_2xx || 0).toFixed(0)}%</td>
        </tr>
    `).join('');
}

// Start
document.addEventListener('DOMContentLoaded', init);
