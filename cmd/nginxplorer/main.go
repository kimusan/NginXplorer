// NginXplorer — Real-time Nginx statistics visualization.
//
// Usage:
//
//	nginxplorer                     Start the dashboard daemon
//	nginxplorer --config FILE       Use a specific config file
//	nginxplorer --tui               Start in TUI mode (Phase 2)
//	nginxplorer hash-password       Generate a bcrypt password hash
//
// NginXplorer collects metrics from Nginx's stub_status endpoint and
// structured JSON access logs, aggregates them per virtual host, and
// serves a real-time web dashboard with historical data.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kimusan/nginxplorer/internal/alerting"
	"github.com/kimusan/nginxplorer/internal/api"
	"github.com/kimusan/nginxplorer/internal/collector"
	"github.com/kimusan/nginxplorer/internal/config"
	"github.com/kimusan/nginxplorer/internal/geoip"
	"github.com/kimusan/nginxplorer/internal/metrics"
	"github.com/kimusan/nginxplorer/internal/storage"
	"github.com/kimusan/nginxplorer/internal/tui"
	"golang.org/x/term"
)

var version = "dev"

func main() {
	api.Version = version

	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	tuiMode := flag.Bool("tui", false, "Start in TUI mode (terminal dashboard)")
	debugMode := flag.Bool("debug", false, "Enable debug logging")
	bindAddr := flag.String("bind", "", "Override bind address (e.g., 0.0.0.0:9100)")
	connectAddr := flag.String("connect", "http://127.0.0.1:9100", "Daemon address to connect to (TUI mode)")
	authToken := flag.String("token", "", "Authentication token for TUI mode")
	authUser := flag.String("user", "", "Username for TUI authentication")
	authPass := flag.String("pass", "", "Password for TUI authentication")
	showVersion := flag.Bool("v", false, "Print version and exit")
	showVersionLong := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *showVersion || *showVersionLong {
		fmt.Printf("nginxplorer %s\n", version)
		return
	}

	// Handle subcommands
	if flag.NArg() > 0 {
		switch flag.Arg(0) {
		case "hash-password":
			hashPasswordCommand()
			return
		case "version":
			fmt.Printf("nginxplorer %s\n", version)
			return
		default:
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n", flag.Arg(0))
			os.Exit(1)
		}
	}

	// TUI mode — connect to running daemon and display terminal dashboard
	if *tuiMode {
		user := *authUser
		pass := *authPass
		token := *authToken

		// If username is provided without a password or token, securely prompt for password (masked input)
		if user != "" && pass == "" && token == "" {
			fmt.Printf("Enter password for %s: ", user)
			bytePassword, err := term.ReadPassword(int(syscall.Stdin))
			fmt.Println() // newline after hidden input
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
				os.Exit(1)
			}
			pass = strings.TrimSpace(string(bytePassword))
		}

		if err := tui.Run(*connectAddr, token, user, pass); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration
	cfg, err := loadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Override bind address if specified on command line
	if *bindAddr != "" {
		cfg.Server.Bind = *bindAddr
	}

	// Set up logging
	setupLogging(cfg.Log, *debugMode)

	slog.Info("starting NginXplorer",
		"version", version,
		"bind", cfg.Server.Bind,
	)

	// Create the central metrics store
	store := metrics.NewStore(
		cfg.Metrics.MaxVHosts,
		cfg.Metrics.MaxTopPaths,
		cfg.Metrics.VisitorWindowMinutes,
	)
	store.Start()

	// Set up context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start stub_status collector
	stubCollector := collector.NewStubStatusCollector(
		cfg.Nginx.StubStatusURL,
		cfg.Nginx.StubStatusInterval,
		store,
	)
	go stubCollector.Start(ctx)

	// Initialize GeoIP provider (if enabled)
	var geoIPProvider *geoip.Provider
	if cfg.GeoIP.Enabled {
		geoIPPath := cfg.GeoIP.DBPath
		if geoIPPath == "" {
			geoIPPath = filepath.Join(filepath.Dir(cfg.Storage.DBPath), "geoip-country.mmdb")
		}
		geoIPProvider = geoip.NewProvider(geoIPPath)
		defer geoIPProvider.Close()

		if cfg.GeoIP.AutoDownload {
			geoIPProvider.AutoUpdate(ctx, "")
		}
	}

	// Start log stream collector
	logCollector := collector.NewLogStreamCollector(
		cfg.Nginx.LogSocket,
		cfg.Nginx.LogFile,
		store,
		cfg.Privacy.AnonymizeIPs,
		cfg.Privacy.StripQueryStrings,
		cfg.Filter.IgnoreHosts,
		cfg.Filter.IgnorePaths,
		geoIPProvider,
	)
	go logCollector.Start(ctx)

	// Start VTS collector (if configured)
	if cfg.Nginx.VTSStatusURL != "" {
		vtsCollector := collector.NewVTSCollector(cfg.Nginx.VTSStatusURL, store)
		go vtsCollector.Start(ctx)
	}

	// Start SQLite storage
	dbDir := filepath.Dir(cfg.Storage.DBPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		slog.Warn("could not create database directory, using local path",
			"dir", dbDir, "error", err,
		)
		cfg.Storage.DBPath = "nginxplorer.db"
	}

	sqlStore, err := storage.NewSQLiteStore(cfg.Storage.DBPath, cfg.Storage.RetentionDays)
	if err != nil {
		slog.Error("failed to open database — historical data will not be persisted",
			"path", cfg.Storage.DBPath,
			"error", err,
		)
	} else {
		defer sqlStore.Close()
		go sqlStore.RecordLoop(ctx, store)
		slog.Info("historical storage initialized", "path", cfg.Storage.DBPath)
	}

	// Build auth configuration
	authCfg := api.AuthConfig{
		Enabled: cfg.Auth.Enabled,
	}
	for _, u := range cfg.Auth.Users {
		authCfg.Users = append(authCfg.Users, api.UserEntry{
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
		})
	}

	// Start Alerting Engine
	var alertEngine *alerting.Engine
	if cfg.Alerts.Enabled {
		alertEngine = alerting.NewEngine(cfg.Alerts, store)
		go alertEngine.Start(ctx)
		defer alertEngine.Stop()
		slog.Info("alerting engine initialized", "rules", len(cfg.Alerts.Rules), "channels", len(cfg.Alerts.Channels))
	}

	// Start HTTP server
	server := api.NewServer(api.ServerConfig{
		Bind:        cfg.Server.Bind,
		Auth:        authCfg,
		Store:       store,
		SQLStore:    sqlStore,
		AlertEngine: alertEngine,
	})

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		slog.Info("received shutdown signal", "signal", sig)
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*1000*1000*1000) // 10s
		defer shutdownCancel()

		if err := server.Stop(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
		store.Stop()
	}()

	if err := server.Start(); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	slog.Info("NginXplorer stopped")
}

// loadConfig loads the configuration from a file or returns defaults.
func loadConfig(path string) (*config.Config, error) {
	if path != "" {
		return config.Load(path)
	}

	// Try to find a config file in standard locations
	found := config.FindConfigFile()
	if found != "" {
		slog.Info("found config file", "path", found)
		return config.Load(found)
	}

	// No config file found — use defaults
	slog.Info("no config file found, using defaults")
	return config.DefaultConfig(), nil
}

// setupLogging configures the slog default logger.
func setupLogging(cfg config.LogConfig, debug bool) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	} else {
		switch strings.ToLower(cfg.Level) {
		case "debug":
			level = slog.LevelDebug
		case "warn", "warning":
			level = slog.LevelWarn
		case "error":
			level = slog.LevelError
		}
	}

	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.Format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// hashPasswordCommand implements the `nginxplorer hash-password` subcommand.
func hashPasswordCommand() {
	fmt.Print("Enter password: ")
	bytePassword, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password := strings.TrimSpace(string(bytePassword))

	if password == "" {
		fmt.Fprintln(os.Stderr, "Password cannot be empty")
		os.Exit(1)
	}

	hash, err := api.HashPassword(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nBcrypt hash: %s\n", hash)
	fmt.Println("\nAdd this to your config.yaml:")
	fmt.Println("  auth:")
	fmt.Println("    enabled: true")
	fmt.Println("    users:")
	fmt.Printf("      - username: admin\n")
	fmt.Printf("        password_hash: \"%s\"\n", hash)
}
