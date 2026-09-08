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
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/kim/nginxplorer/internal/api"
	"github.com/kim/nginxplorer/internal/collector"
	"github.com/kim/nginxplorer/internal/config"
	"github.com/kim/nginxplorer/internal/metrics"
	"github.com/kim/nginxplorer/internal/storage"
	"github.com/kim/nginxplorer/internal/tui"
)

var version = "dev"

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	tuiMode := flag.Bool("tui", false, "Start in TUI mode (terminal dashboard)")
	debugMode := flag.Bool("debug", false, "Enable debug logging")
	bindAddr := flag.String("bind", "", "Override bind address (e.g., 0.0.0.0:9100)")
	connectAddr := flag.String("connect", "http://127.0.0.1:9100", "Daemon address to connect to (TUI mode)")
	authToken := flag.String("token", "", "Authentication token for TUI mode")
	flag.Parse()

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
		if err := tui.Run(*connectAddr, *authToken); err != nil {
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

	// Start log stream collector
	logCollector := collector.NewLogStreamCollector(
		cfg.Nginx.LogSocket,
		cfg.Nginx.LogFile,
		store,
		cfg.Privacy.AnonymizeIPs,
		cfg.Privacy.StripQueryStrings,
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

	// Start HTTP server
	server := api.NewServer(api.ServerConfig{
		Bind:  cfg.Server.Bind,
		Auth:  authCfg,
		Store: store,
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
	reader := bufio.NewReader(os.Stdin)
	password, err := reader.ReadString('\n')
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password = strings.TrimSpace(password)

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
