// Package storage provides SQLite-based historical data persistence for NginXplorer.
// It stores aggregated metrics at various resolutions (1-minute, 5-minute, hourly, daily)
// and handles automatic rollup and retention.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kim/nginxplorer/internal/metrics"

	_ "modernc.org/sqlite"
)

// SQLiteStore persists aggregated metrics to a SQLite database for
// historical queries beyond the in-memory ring buffer window.
type SQLiteStore struct {
	db            *sql.DB
	mu            sync.Mutex
	retentionDays int
}

// NewSQLiteStore opens or creates a SQLite database at the given path.
func NewSQLiteStore(dbPath string, retentionDays int) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Set connection pool limits
	db.SetMaxOpenConns(1) // SQLite supports single writer
	db.SetMaxIdleConns(1)

	store := &SQLiteStore{
		db:            db,
		retentionDays: retentionDays,
	}

	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return store, nil
}

// migrate creates the database schema if it doesn't exist.
func (s *SQLiteStore) migrate() error {
	schema := `
		CREATE TABLE IF NOT EXISTS metrics_1m (
			timestamp INTEGER NOT NULL,
			vhost     TEXT NOT NULL,
			requests  INTEGER DEFAULT 0,
			s2xx      INTEGER DEFAULT 0,
			s3xx      INTEGER DEFAULT 0,
			s4xx      INTEGER DEFAULT 0,
			s5xx      INTEGER DEFAULT 0,
			latency_avg   REAL DEFAULT 0,
			latency_p95   REAL DEFAULT 0,
			latency_p99   REAL DEFAULT 0,
			bytes_in      INTEGER DEFAULT 0,
			bytes_out     INTEGER DEFAULT 0,
			unique_visitors INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost)
		);

		CREATE TABLE IF NOT EXISTS metrics_1h (
			timestamp INTEGER NOT NULL,
			vhost     TEXT NOT NULL,
			requests  INTEGER DEFAULT 0,
			s2xx      INTEGER DEFAULT 0,
			s3xx      INTEGER DEFAULT 0,
			s4xx      INTEGER DEFAULT 0,
			s5xx      INTEGER DEFAULT 0,
			latency_avg   REAL DEFAULT 0,
			latency_p95   REAL DEFAULT 0,
			latency_p99   REAL DEFAULT 0,
			bytes_in      INTEGER DEFAULT 0,
			bytes_out     INTEGER DEFAULT 0,
			unique_visitors INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost)
		);

		CREATE TABLE IF NOT EXISTS metrics_1d (
			timestamp INTEGER NOT NULL,
			vhost     TEXT NOT NULL,
			requests  INTEGER DEFAULT 0,
			s2xx      INTEGER DEFAULT 0,
			s3xx      INTEGER DEFAULT 0,
			s4xx      INTEGER DEFAULT 0,
			s5xx      INTEGER DEFAULT 0,
			latency_avg   REAL DEFAULT 0,
			latency_p95   REAL DEFAULT 0,
			latency_p99   REAL DEFAULT 0,
			bytes_in      INTEGER DEFAULT 0,
			bytes_out     INTEGER DEFAULT 0,
			unique_visitors INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost)
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_1m_vhost ON metrics_1m(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_1h_vhost ON metrics_1h(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_1d_vhost ON metrics_1d(vhost, timestamp);
	`

	_, err := s.db.Exec(schema)
	return err
}

// Record inserts a 1-minute aggregated data point.
func (s *SQLiteStore) Record(ctx context.Context, timestamp time.Time, vhost string, snap metrics.VHostMetrics) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Round timestamp to minute boundary
	ts := timestamp.Truncate(time.Minute).Unix()

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_1m 
			(timestamp, vhost, requests, s2xx, s3xx, s4xx, s5xx, 
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, vhost,
		int64(snap.RPS*60), // approximate total requests in this minute
		snap.StatusCodes.S2xx, snap.StatusCodes.S3xx,
		snap.StatusCodes.S4xx, snap.StatusCodes.S5xx,
		snap.Latency.Avg, snap.Latency.P95, snap.Latency.P99,
		snap.Bandwidth.In, snap.Bandwidth.Out,
		snap.UniqueVisitors,
	)

	return err
}

// QueryHistory retrieves historical data for a vhost within the given time range.
// It automatically selects the appropriate resolution table based on the range.
func (s *SQLiteStore) QueryHistory(ctx context.Context, vhost string, from, to time.Time) (*metrics.VHostHistory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	duration := to.Sub(from)

	// Select appropriate table based on duration
	table := "metrics_1m"
	switch {
	case duration > 30*24*time.Hour:
		table = "metrics_1d"
	case duration > 24*time.Hour:
		table = "metrics_1h"
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT timestamp, requests, s2xx, s3xx, s4xx, s5xx,
		       latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors
		FROM %s
		WHERE vhost = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, table), vhost, from.Unix(), to.Unix())
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	history := &metrics.VHostHistory{
		RPS:        make([]metrics.HistoryPoint, 0),
		LatencyP95: make([]metrics.HistoryPoint, 0),
		ErrorRate:  make([]metrics.HistoryPoint, 0),
		Bandwidth:  make([]metrics.HistoryPoint, 0),
	}

	for rows.Next() {
		var ts int64
		var requests, s2xx, s3xx, s4xx, s5xx int64
		var latAvg, latP95, latP99 float64
		var bytesIn, bytesOut, visitors int64

		if err := rows.Scan(&ts, &requests, &s2xx, &s3xx, &s4xx, &s5xx,
			&latAvg, &latP95, &latP99, &bytesIn, &bytesOut, &visitors); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}

		total := s2xx + s3xx + s4xx + s5xx
		errRate := 0.0
		if total > 0 {
			errRate = float64(s4xx+s5xx) / float64(total) * 100
		}

		// Convert requests count to RPS based on table resolution
		rps := float64(requests)
		switch table {
		case "metrics_1m":
			rps = rps / 60
		case "metrics_1h":
			rps = rps / 3600
		case "metrics_1d":
			rps = rps / 86400
		}

		history.RPS = append(history.RPS, metrics.HistoryPoint{Timestamp: ts, Value: rps})
		history.LatencyP95 = append(history.LatencyP95, metrics.HistoryPoint{Timestamp: ts, Value: latP95})
		history.ErrorRate = append(history.ErrorRate, metrics.HistoryPoint{Timestamp: ts, Value: errRate})
		history.Bandwidth = append(history.Bandwidth, metrics.HistoryPoint{Timestamp: ts, Value: float64(bytesOut)})
	}

	return history, rows.Err()
}

// Rollup aggregates 1-minute data into hourly and daily buckets.
// Should be called periodically (e.g., every 5 minutes).
func (s *SQLiteStore) Rollup(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rollup 1m -> 1h (for data older than 24 hours)
	cutoff24h := time.Now().Add(-24 * time.Hour).Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_1h 
			(timestamp, vhost, requests, s2xx, s3xx, s4xx, s5xx,
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors)
		SELECT 
			(timestamp / 3600) * 3600 as hour_ts,
			vhost,
			SUM(requests),
			SUM(s2xx), SUM(s3xx), SUM(s4xx), SUM(s5xx),
			AVG(latency_avg), MAX(latency_p95), MAX(latency_p99),
			SUM(bytes_in), SUM(bytes_out),
			MAX(unique_visitors)
		FROM metrics_1m
		WHERE timestamp < ?
		GROUP BY hour_ts, vhost
	`, cutoff24h)
	if err != nil {
		return fmt.Errorf("hourly rollup: %w", err)
	}

	// Delete rolled-up 1m data
	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_1m WHERE timestamp < ?", cutoff24h)
	if err != nil {
		return fmt.Errorf("cleaning 1m data: %w", err)
	}

	// Rollup 1h -> 1d (for data older than 7 days)
	cutoff7d := time.Now().Add(-7 * 24 * time.Hour).Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_1d
			(timestamp, vhost, requests, s2xx, s3xx, s4xx, s5xx,
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors)
		SELECT 
			(timestamp / 86400) * 86400 as day_ts,
			vhost,
			SUM(requests),
			SUM(s2xx), SUM(s3xx), SUM(s4xx), SUM(s5xx),
			AVG(latency_avg), MAX(latency_p95), MAX(latency_p99),
			SUM(bytes_in), SUM(bytes_out),
			MAX(unique_visitors)
		FROM metrics_1h
		WHERE timestamp < ?
		GROUP BY day_ts, vhost
	`, cutoff7d)
	if err != nil {
		return fmt.Errorf("daily rollup: %w", err)
	}

	// Delete rolled-up 1h data
	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_1h WHERE timestamp < ?", cutoff7d)
	if err != nil {
		return fmt.Errorf("cleaning 1h data: %w", err)
	}

	// Apply retention policy
	if s.retentionDays > 0 {
		cutoffRetention := time.Now().Add(-time.Duration(s.retentionDays) * 24 * time.Hour).Unix()
		_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_1d WHERE timestamp < ?", cutoffRetention)
		if err != nil {
			return fmt.Errorf("applying retention: %w", err)
		}
	}

	slog.Debug("rollup completed")
	return nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// RecordLoop starts a background loop that periodically records
// snapshots from the metrics store and performs rollups.
func (s *SQLiteStore) RecordLoop(ctx context.Context, store *metrics.Store) {
	recordTicker := time.NewTicker(1 * time.Minute)
	rollupTicker := time.NewTicker(5 * time.Minute)
	defer recordTicker.Stop()
	defer rollupTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-recordTicker.C:
			snapshot := store.Snapshot()
			for vhost, vhMetrics := range snapshot.VHosts {
				if err := s.Record(ctx, snapshot.Timestamp, vhost, vhMetrics); err != nil {
					slog.Error("failed to record metrics", "vhost", vhost, "error", err)
				}
			}

		case <-rollupTicker.C:
			if err := s.Rollup(ctx); err != nil {
				slog.Error("rollup failed", "error", err)
			}
		}
	}
}
