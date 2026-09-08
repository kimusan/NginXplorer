// Package config provides configuration loading and validation for NginXplorer.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for NginXplorer.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Auth    AuthConfig    `yaml:"auth"`
	Nginx   NginxConfig   `yaml:"nginx"`
	Storage StorageConfig `yaml:"storage"`
	Metrics MetricsConfig `yaml:"metrics"`
	Privacy PrivacyConfig `yaml:"privacy"`
	Alerts  AlertsConfig  `yaml:"alerts"`
	Log     LogConfig     `yaml:"log"`
}

// ServerConfig controls the web dashboard HTTP server.
type ServerConfig struct {
	// Bind address for the web dashboard (e.g., "127.0.0.1:9100" or "0.0.0.0:9100")
	Bind string `yaml:"bind"`
}

// AuthConfig controls authentication for the web dashboard.
type AuthConfig struct {
	// Enabled enables authentication. Required when bind is not localhost.
	Enabled bool `yaml:"enabled"`
	// Users is the list of authorized users with bcrypt-hashed passwords.
	Users []UserConfig `yaml:"users"`
}

// UserConfig represents a single authorized user.
type UserConfig struct {
	Username     string `yaml:"username"`
	PasswordHash string `yaml:"password_hash"`
}

// NginxConfig controls how NginXplorer connects to Nginx for metrics.
type NginxConfig struct {
	// StubStatusURL is the URL to Nginx's stub_status endpoint.
	StubStatusURL string `yaml:"stub_status_url"`
	// StubStatusInterval is how often to poll stub_status (milliseconds).
	StubStatusInterval int `yaml:"stub_status_interval"`
	// VTSStatusURL is the optional URL to the VTS module status endpoint (JSON format).
	VTSStatusURL string `yaml:"vts_status_url"`
	// LogSocket is the Unix domain socket path for receiving syslog-formatted JSON access logs.
	LogSocket string `yaml:"log_socket"`
	// LogFile is an alternative: a file path to tail for access logs.
	LogFile string `yaml:"log_file"`
}

// StorageConfig controls historical data persistence.
type StorageConfig struct {
	// DBPath is the path to the SQLite database file.
	DBPath string `yaml:"db_path"`
	// RetentionDays is how many days of historical data to retain.
	RetentionDays int `yaml:"retention_days"`
}

// MetricsConfig controls in-memory metrics behavior.
type MetricsConfig struct {
	// MaxVHosts is the maximum number of vhosts to track.
	MaxVHosts int `yaml:"max_vhosts"`
	// MaxTopPaths is the maximum number of top paths per vhost.
	MaxTopPaths int `yaml:"max_top_paths"`
	// VisitorWindowMinutes is the window for counting unique visitors.
	VisitorWindowMinutes int `yaml:"visitor_window_minutes"`
}

// PrivacyConfig controls privacy-related settings.
type PrivacyConfig struct {
	// AnonymizeIPs masks the last octet of IPv4 addresses.
	AnonymizeIPs bool `yaml:"anonymize_ips"`
	// StripQueryStrings removes query strings from logged URIs.
	StripQueryStrings bool `yaml:"strip_query_strings"`
}

// LogConfig controls NginXplorer's own logging.
type LogConfig struct {
	// Level is the log level: debug, info, warn, error.
	Level string `yaml:"level"`
	// Format is the log format: text or json.
	Format string `yaml:"format"`
}

// AlertsConfig controls alert evaluation and notification dispatching.
type AlertsConfig struct {
	Enabled      bool                 `yaml:"enabled"`
	DashboardURL string               `yaml:"dashboard_url"` // e.g. "https://stats.dublin.hackspace.tech"
	Cooldown     string               `yaml:"cooldown"`      // e.g. "10m"
	Rules        []AlertRuleConfig    `yaml:"rules"`
	Channels     []AlertChannelConfig `yaml:"channels"`
}

// AlertRuleConfig defines an alert evaluation rule.
type AlertRuleConfig struct {
	Name        string  `yaml:"name"`
	VHost       string  `yaml:"vhost"`        // "all" or specific vhost name
	Metric      string  `yaml:"metric"`       // "error_rate", "latency_p95", "zero_traffic"
	Threshold   float64 `yaml:"threshold"`    // e.g. 5.0 (%) or 1000 (ms)
	Duration    string  `yaml:"duration"`     // evaluation window e.g. "1m"
	MinRequests int64   `yaml:"min_requests"` // minimum requests in window before evaluating percentage rules
	MinErrors   int64   `yaml:"min_errors"`   // minimum errors in window before firing
}

// AlertChannelConfig defines a notification destination.
type AlertChannelConfig struct {
	Type     string `yaml:"type"`        // "ntfy", "pushbullet", "slack", "discord", "webhook"
	URL      string `yaml:"url"`         // destination URL (for webhook, ntfy, slack, discord)
	Token    string `yaml:"token"`       // optional auth token (or pushbullet token)
	APIToken string `yaml:"api_token"`   // pushbullet API access token
	DeviceID string `yaml:"device_iden"` // optional pushbullet device iden
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Bind: "127.0.0.1:9100",
		},
		Auth: AuthConfig{
			Enabled: false,
		},
		Alerts: AlertsConfig{
			Enabled:  false,
			Cooldown: "10m",
		},
		Nginx: NginxConfig{
			StubStatusURL:      "http://127.0.0.1:8099/nginx_status",
			StubStatusInterval: 500,
			LogSocket:          "/var/run/nginxplorer.sock",
		},
		Storage: StorageConfig{
			DBPath:        "/var/lib/nginxplorer/data.db",
			RetentionDays: 30,
		},
		Metrics: MetricsConfig{
			MaxVHosts:            50,
			MaxTopPaths:          100,
			VisitorWindowMinutes: 5,
		},
		Privacy: PrivacyConfig{
			AnonymizeIPs:      false,
			StripQueryStrings: true,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Load reads a configuration file from disk and merges it with defaults.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file %s: %w", path, err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file %s: %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return cfg, nil
}

// Validate checks the configuration for logical errors.
func (c *Config) Validate() error {
	if c.Server.Bind == "" {
		return fmt.Errorf("server.bind is required")
	}

	if c.Nginx.StubStatusURL == "" {
		return fmt.Errorf("nginx.stub_status_url is required")
	}

	if c.Nginx.StubStatusInterval < 100 {
		return fmt.Errorf("nginx.stub_status_interval must be >= 100ms")
	}

	if c.Nginx.LogSocket == "" && c.Nginx.LogFile == "" {
		return fmt.Errorf("either nginx.log_socket or nginx.log_file is required")
	}

	if c.Storage.RetentionDays < 1 {
		return fmt.Errorf("storage.retention_days must be >= 1")
	}

	if c.Metrics.MaxVHosts < 1 {
		return fmt.Errorf("metrics.max_vhosts must be >= 1")
	}

	if c.Alerts.Enabled {
		for i, r := range c.Alerts.Rules {
			if r.Name == "" {
				return fmt.Errorf("alerts.rules[%d].name is required", i)
			}
			if r.Metric == "" {
				return fmt.Errorf("alerts.rules[%d].metric is required", i)
			}
		}
	}

	return nil
}

// FindConfigFile searches for a configuration file in standard locations.
func FindConfigFile() string {
	candidates := []string{
		"nginxplorer.yaml",
		"configs/nginxplorer.yaml",
	}

	// User config directory
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "nginxplorer", "config.yaml"))
	}

	// System config
	candidates = append(candidates, "/etc/nginxplorer/config.yaml")

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
