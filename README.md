# ⚡ NginXplorer

> **Real-time Nginx statistics visualization tool** — Monitor traffic, connection states, response times, and per-virtual-host metrics with zero-overhead streaming.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://golang.org)
[![Status](https://img.shields.io/badge/Status-Active%20Development-brightgreen)](#roadmap)

---

## 📸 Screenshots

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ NginXplorer v0.1.0                    [Status: ONLINE]          Up: 4d 12h 31m 15s     │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ Active: 142   │ Reading: 4   │ Writing: 38   │ Waiting: 100   │ Req/s: 1,284.5         │
├────────────────────────────────────────────────────────────────────────────────────────┤
│ [Requests / Second (Last 60s)]                                                         │
│  2.0k ┤                  ╭──╮                                                          │
│  1.5k ┤        ╭──╮  ╭───╯  ╰──╮                                                       │
│  1.0k ┤───╭────╯  ╰──╯         ╰──╮                                                    │
│  500  ┤   │                       ╰─────────                                           │
├───────────────────────────────┬────────────────────────────────────────────────────────┤
│ Virtual Hosts (Top by Req/s)  │ Status Codes Breakdown (Live)                          │
│ • api.example.com    (842 r/s)│  2xx Success:  ■■■■■■■■■■■■■■■■■■■■■■■■ 94.2%          │
│ • www.example.com    (312 r/s)│  3xx Redirect: ■■ 3.8%                                 │
│ • auth.example.com   (130 r/s)│  4xx Client:   ■ 1.6%                                  │
│                               │  5xx Server:   ▫ 0.4%                                  │
├───────────────────────────────┴────────────────────────────────────────────────────────┤
│ Top Endpoints (p95 Latency)                                                            │
│ • POST /api/v1/checkout   — 124 r/s — p50: 18ms  p90: 42ms  p99: 110ms                 │
│ • GET  /api/v1/products   — 340 r/s — p50: 4ms   p90: 12ms  p99: 28ms                  │
│ • GET  /static/bundle.js  — 480 r/s — p50: 1ms   p90: 2ms   p99: 5ms                   │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

*(Web dashboard & TUI screenshots coming soon)*

---

## ✨ Features

- **⚡ Real-Time Performance Metrics**:
  - Live requests-per-second (RPS) and active connections (reading, writing, waiting)
  - Connection rate, total accepted/handled counts, and drop detection
  - Request latency distributions with percentiles ($p_{50}$, $p_{90}$, $p_{99}$)
  - Upstream response times and backend server health

- **🌐 Virtual Host & Endpoint Analytics**:
  - Auto-discovery and traffic segmentation across all Nginx virtual hosts (`server_name`)
  - Real-time HTTP status breakdown ($2xx$, $3xx$, $4xx$, $5xx$)
  - Top active request paths per virtual host with hit counts and latency metrics
  - Bandwidth consumption tracking (incoming request bytes and egress payload)

- **🖥️ Dual Interface (Web & TUI)**:
  - **Embedded Web UI**: Lightweight single-binary dashboard with real-time updates via Server-Sent Events (SSE)
  - **Terminal UI (TUI)**: Terminal dashboard built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) for remote SSH sessions and headless servers

- **💾 Historical Persistence**:
  - Embedded SQLite database for zero-config long-term metrics storage
  - Automated database retention windows and background aggregation rollups

- **🔒 Security & Privacy Built-In**:
  - Bcrypt-hashed user authentication for web access
  - IP anonymization (masks client IP octets for GDPR/compliance)
  - Query string scrubbing to prevent leaking sensitive request parameters
  - Unix domain socket listener for secure, local-only log ingestion

- **🚀 Zero Disk I/O Overhead**:
  - Ingests structured JSON logs directly from Nginx over a local Unix domain socket (`syslog:server=unix:...`)
  - No disk churn or log parsing bottlenecks

---

## 🚀 Quick Start

Get NginXplorer up and running in 3 simple steps:

### 1. Install NginXplorer

Clone the repository and build the binary:

```bash
git clone https://github.com/kim/nginxplorer.git
cd nginxplorer
make build
sudo make install
```

### 2. Configure Nginx

Copy the provided Nginx configuration snippets into your Nginx configuration directory (e.g. `/etc/nginx/conf.d/`):

```bash
# 1. Enable the stub_status endpoint (internal only)
sudo cp configs/nginx/nginxplorer-status.conf /etc/nginx/conf.d/

# 2. Enable real-time JSON log streaming
sudo cp configs/nginx/nginxplorer-log.conf /etc/nginx/conf.d/

# Test and reload Nginx
sudo nginx -t
sudo nginx -s reload
```

### 3. Run NginXplorer

Copy the example configuration, set up your credentials, and start the daemon:

```bash
# Create configuration directory
sudo mkdir -p /etc/nginxplorer /var/lib/nginxplorer
sudo cp configs/nginxplorer.example.yaml /etc/nginxplorer/config.yaml

# Run NginXplorer
nginxplorer --config /etc/nginxplorer/config.yaml
```

Open your browser at **`http://127.0.0.1:9100`** and log in with:
- **Username**: `admin`
- **Password**: `changeme` *(Be sure to change this! See [Security & Authentication](#-security-section))*

---

## 📦 Detailed Installation

### Prerequisites

- Linux (x86_64, aarch64, armv7)
- Nginx 1.13+ (compiled with `--with-http_stub_status_module`, standard in all major distributions)
- Go 1.22+ (only required if building from source)

### Building from Source

```bash
# Clone repository
git clone https://github.com/kim/nginxplorer.git
cd nginxplorer

# Build binary
make build

# Run unit tests
make test

# Install binary and sample configs
sudo make install
```

### Running as a Systemd Service

Create a systemd unit file at `/etc/systemd/system/nginxplorer.service`:

```ini
[Unit]
Description=NginXplorer - Real-time Nginx Statistics
After=network.target nginx.service
Requires=nginx.service

[Service]
Type=simple
User=root
Group=root
ExecStart=/usr/local/bin/nginxplorer --config /etc/nginxplorer/config.yaml
Restart=on-failure
RestartSec=5s
LimitNOFILE=65535

# Sandboxing
ProtectSystem=full
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

Enable and start the service:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nginxplorer
sudo systemctl status nginxplorer
```

---

## ⚙️ Configuration Guide

NginXplorer searches for configuration in the following order:
1. File passed via `--config <path>`
2. `/etc/nginxplorer/config.yaml`
3. `~/.config/nginxplorer/config.yaml`
4. `./configs/nginxplorer.example.yaml` (fallback)

### Configuration Reference (`config.yaml`)

```yaml
# NginXplorer Configuration
# Copy this to /etc/nginxplorer/config.yaml or ~/.config/nginxplorer/config.yaml

server:
  # Address to bind the web dashboard
  bind: "127.0.0.1:9100"
  # Set to 0.0.0.0:9100 to expose remotely (requires auth)

auth:
  # Enable authentication (required when bind is not localhost)
  enabled: true
  # Username and password for web dashboard login
  # Password is stored as bcrypt hash. Generate with:
  #   nginxplorer hash-password
  users:
    - username: admin
      # Default password: 'changeme' — CHANGE THIS!
      password_hash: "$2a$10$K0E.nADuD0f39ELn1mV72uq75kL8whLrKex0g4Q9lE4CuRSRLpXQ2"

nginx:
  # stub_status URL (usually on localhost)
  stub_status_url: "http://127.0.0.1:8099/nginx_status"
  # How often to poll stub_status (milliseconds)
  stub_status_interval: 500
  
  # VTS module status URL (optional, leave empty to disable)
  vts_status_url: ""
  
  # Unix socket to receive JSON access logs from Nginx syslog
  log_socket: "/var/run/nginxplorer.sock"
  # Alternative: path to access log file to tail
  # log_file: "/var/log/nginx/access.log"

storage:
  # Path to SQLite database for historical data
  db_path: "/var/lib/nginxplorer/data.db"
  # Retention: how many days of historical data to keep
  retention_days: 30

metrics:
  # Maximum number of vhosts to track (memory sizing)
  max_vhosts: 50
  # Maximum number of top paths to track per vhost
  max_top_paths: 100
  # Maximum number of unique visitors window (minutes)
  visitor_window_minutes: 5

# Privacy settings
privacy:
  # Anonymize IP addresses (mask last octet)
  anonymize_ips: false
  # Don't log query strings
  strip_query_strings: true

# Logging
log:
  level: "info"   # debug, info, warn, error
  format: "text"  # text or json
```

### Options Description

| Parameter | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `server.bind` | string | `127.0.0.1:9100` | Address and port for web dashboard and API. |
| `auth.enabled` | boolean | `true` | Enforces session authentication for web and API access. |
| `auth.users` | list | `admin` | List of allowed users and their bcrypt password hashes. |
| `nginx.stub_status_url` | string | `http://127.0.0.1:8099/nginx_status` | URL of the Nginx `stub_status` endpoint. |
| `nginx.stub_status_interval`| int | `500` | Polling interval for `stub_status` in milliseconds. |
| `nginx.vts_status_url` | string | `""` | Optional URL for `nginx-module-vts` JSON status endpoint. |
| `nginx.log_socket` | string | `/var/run/nginxplorer.sock` | Unix domain socket path for zero-I/O syslog streaming. |
| `nginx.log_file` | string | `""` | Optional fallback path for file tailing. |
| `storage.db_path` | string | `/var/lib/nginxplorer/data.db` | Path to SQLite database for persistent metrics. |
| `storage.retention_days` | int | `30` | Number of days to preserve historical metric rollups. |
| `metrics.max_vhosts` | int | `50` | Maximum number of virtual hosts to track in memory. |
| `metrics.max_top_paths` | int | `100` | Maximum number of top request paths tracked per host. |
| `metrics.visitor_window_minutes`| int | `5` | Sliding window duration for active visitor tracking. |
| `privacy.anonymize_ips` | boolean | `false` | Masks the final octet of IPv4 / last 80 bits of IPv6 addresses. |
| `privacy.strip_query_strings` | boolean | `true` | Strips `?query=...` arguments before tracking paths. |
| `log.level` | string | `info` | Application log verbosity (`debug`, `info`, `warn`, `error`). |
| `log.format` | string | `text` | Log output format (`text` or `json`). |

---

## 🔧 Nginx Setup Guide

NginXplorer combines two high-efficiency telemetry streams:
1. **`stub_status`**: Provides instantaneous connection states (active, reading, writing, waiting) with minimal CPU overhead.
2. **JSON Log Streaming (`syslog`)**: Delivers rich per-request telemetry (vhost, response status, latency, upstream timing) over a Unix domain socket without disk I/O.

### 1. Configure `stub_status` Endpoint

Include `configs/nginx/nginxplorer-status.conf` in your Nginx configuration:

```nginx
# Internal-only server for status endpoints
server {
    listen 127.0.0.1:8099;
    server_name 127.0.0.1;

    # Basic stub_status (always available in open-source Nginx)
    location = /nginx_status {
        stub_status;
        allow 127.0.0.1;
        deny all;
        access_log off;
    }
}
```

### 2. Configure Structured JSON Log Streaming

Include `configs/nginx/nginxplorer-log.conf` inside your `http { ... }` block:

```nginx
# Structured JSON log format for NginXplorer
log_format nginxplorer escape=json '{'
    '"ts":"$time_iso8601",'
    '"host":"$server_name",'
    '"addr":"$remote_addr",'
    '"method":"$request_method",'
    '"uri":"$uri",'
    '"args":"$args",'
    '"status":$status,'
    '"bytes":$bytes_sent,'
    '"body_bytes":$body_bytes_sent,'
    '"req_len":$request_length,'
    '"req_time":$request_time,'
    '"upstream_time":"$upstream_response_time",'
    '"upstream_addr":"$upstream_addr",'
    '"upstream_status":"$upstream_status",'
    '"ua":"$http_user_agent",'
    '"ref":"$http_referer",'
    '"ssl_proto":"$ssl_protocol",'
    '"conn":"$connection",'
    '"conn_reqs":"$connection_requests"'
'}';

# Stream logs to NginXplorer via Unix domain socket (zero disk I/O)
access_log syslog:server=unix:/var/run/nginxplorer.sock,tag=nginx nginxplorer;
```

> [!NOTE]
> `access_log` directives in Nginx are additive. Streaming to NginXplorer will not interfere with or replace your existing access log files.

### 3. Optional: Virtual Host Traffic Status (VTS) Module

If you have compiled Nginx with [`nginx-module-vts`](https://github.com/vozlt/nginx-module-vts):

1. Add `vhost_traffic_status_zone;` to your `http {}` block in `nginx.conf`.
2. Uncomment the `/vts_status` location block in `nginxplorer-status.conf`:

```nginx
location /vts_status {
    vhost_traffic_status_display;
    vhost_traffic_status_display_format json;
    allow 127.0.0.1;
    deny all;
    access_log off;
}
```
3. Set `vts_status_url: "http://127.0.0.1:8099/vts_status"` in `config.yaml`.

---

## 🔐 Security Section

### 1. Authentication & Password Hashing

NginXplorer enforces bcrypt password verification. **Never commit raw passwords.**

Generate a password hash using the built-in command:

```bash
nginxplorer hash-password
# Or via make:
make hash-password
```

Paste the resulting hash string into the `password_hash` field under `auth.users` in `config.yaml`.

### 2. Network Binding Isolation

- **Local-Only (Recommended)**: Keep `server.bind: "127.0.0.1:9100"`. Access the dashboard via SSH port forwarding:
  ```bash
  ssh -L 9100:127.0.0.1:9100 user@remote-server
  ```
- **Remote Exposure**: If setting `server.bind: "0.0.0.0:9100"`, ensure `auth.enabled: true` is set, and place NginXplorer behind a reverse proxy handling TLS.

### 3. Reverse Proxy with TLS (Nginx Example)

To expose NginXplorer over HTTPS with TLS termination:

```nginx
server {
    listen 443 ssl http2;
    server_name nginxplorer.example.com;

    ssl_certificate     /etc/letsencrypt/live/nginxplorer.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/nginxplorer.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:9100;
        proxy_http_version 1.1;
        
        # Required for Server-Sent Events (SSE) live streaming
        proxy_set_header Connection '';
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 24h;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 4. Unix Socket Permissions

The Unix domain socket (`/var/run/nginxplorer.sock`) receives syslog messages from the Nginx worker processes. Ensure the Nginx user (e.g. `www-data` or `nginx`) has write permissions:

```bash
# If NginXplorer runs as root or nginx group:
sudo chown root:www-data /var/run/nginxplorer.sock
sudo chmod 660 /var/run/nginxplorer.sock
```

---

## 🏗️ Architecture Overview

```
 ┌────────────────────────────────────────────────────────┐
 │                      Nginx Master                      │
 │    ┌──────────────────┐        ┌──────────────────┐    │
 │    │   stub_status    │        │  syslog stream   │    │
 │    │ (HTTP /status)   │        │ (Unix Domain Sk) │    │
 └────┴────────┬─────────┴────────┴────────┬─────────┴────┘
               │ Polling                   │ Push
               ▼                           ▼
 ┌────────────────────────────────────────────────────────┐
 │                      NginXplorer                       │
 │  ┌──────────────────────────────────────────────────┐  │
 │  │                 Collector Layer                  │  │
 │  │  • StubStatusPoller    • LogStreamListener       │  │
 │  │  • VTSPoller (opt)     • FileTailer (fallback)   │  │
 │  └──────────────────────────┬───────────────────────┘  │
 │                             ▼                          │
 │  ┌──────────────────────────────────────────────────┐  │
 │  │             Metrics Store & Engine               │  │
 │  │  • Real-time sliding windows (1m, 5m, 1h)        │  │
 │  │  • Percentile calculations (p50, p90, p99)       │  │
 │  │  • Per-vhost counters & top path frequency       │  │
 │  └──────────────┬────────────────────────┬──────────┘  │
 │                 │ Persist                │ Broadcast   │
 │                 ▼                        ▼             │
 │  ┌──────────────────────────┐ ┌─────────────────────┐  │
 │  │     SQLite Storage       │ │   SSE Hub & API     │  │
 │  │  • Historical rollups    │ │  • /api/v1/live     │  │
 │  │  • Retention cleaner     │ │  • /api/v1/metrics  │  │
 │  └──────────────────────────┘ └──────────┬──────────┘  │
 └──────────────────────────────────────────┼─────────────┘
                                            │
                     ┌──────────────────────┴──────────────────────┐
                     ▼                                             ▼
          ┌─────────────────────┐                       ┌─────────────────────┐
          │ Embedded Web UI     │                       │ Bubble Tea TUI      │
          │ (Single-Page App)   │                       │ (Terminal Console)  │
          └─────────────────────┘                       └─────────────────────┘
```

### Components

- **Collector Layer** (`internal/collector/`): Decoupled collectors ingest telemetry from Nginx via HTTP (`stub_status`, VTS) or Unix domain sockets (`syslog` JSON log streaming).
- **Metrics Store** (`internal/metrics/`): Concurrent in-memory ring buffers and sliding-window buckets aggregate requests per second, calculate latencies using HDR Histograms, and track top paths.
- **Storage Layer** (`internal/storage/`): Embedded SQLite database persists metrics rollups (1m, 5m, 1h aggregates) with automated pruning based on configured retention.
- **API & SSE Hub** (`internal/api/`): Fast HTTP API serving JSON snapshots and a Server-Sent Events (SSE) channel for sub-second web updates.
- **Visualization** (`web/` & `internal/tui/`): Embedded web dashboard and terminal interface consuming the unified metrics stream.

---

## 💻 Development

### Prerequisites

Ensure Go 1.22+ is installed and available in your environment:

```bash
export PATH="/home/linuxbrew/.linuxbrew/bin:$PATH"
go version
```

### Development Commands

The included `Makefile` provides standard development targets:

```bash
# Compile binary
make build

# Run in development mode with example configuration
make dev

# Run unit and race tests
make test

# Run metrics engine benchmarks
make bench

# Format codebase
make fmt

# Run static analysis linter
make lint

# Clean build artifacts
make clean
```

---

## 🗺️ Roadmap

- [x] **Phase 1: Core Engine & Web Dashboard**
  - [x] Configuration parser (`internal/config`)
  - [x] `stub_status` HTTP poller (`internal/collector`)
  - [x] Unix socket JSON log listener (`internal/collector`)
  - [x] In-memory sliding window metric aggregator (`internal/metrics`)
  - [x] SQLite historical persistence (`internal/storage`)
  - [x] Real-time Server-Sent Events (SSE) broadcaster (`internal/api`)
  - [x] Embedded responsive web dashboard (`web/`)

- [ ] **Phase 2: Terminal UI (TUI)**
  - [ ] Interactive Bubble Tea dashboard (`internal/tui`)
  - [ ] Terminal sparklines and real-time ASCII charts
  - [ ] Keyboard navigation for inspecting vhosts and drill-downs
  - [ ] Headless/SSH remote attachment mode

- [ ] **Phase 3: Alerting & Ecosystem**
  - [ ] Real-time threshold alerting (5xx spikes, latency degradation, upstream outages)
  - [ ] Notification webhooks (Slack, Discord, Telegram, generic HTTP)
  - [ ] Prometheus `/metrics` exporter endpoint
  - [ ] Mobile-optimized PWA layout

---

## 🤝 Contributing

Contributions are welcome and appreciated! To contribute:

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'feat: add amazing feature'`)
4. Verify tests and linting (`make test && make lint`)
5. Push to the branch (`git push origin feature/amazing-feature`)
6. Open a Pull Request

Please ensure your code follows Go best practices and includes tests where applicable.

---

## 📄 License

This project is licensed under the MIT License — see the [LICENSE](LICENSE) file for details.
