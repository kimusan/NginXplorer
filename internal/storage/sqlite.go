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

	"github.com/kimusan/nginxplorer/internal/metrics"

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
			bot_human     INTEGER DEFAULT 0,
			bot_good      INTEGER DEFAULT 0,
			bot_bad       INTEGER DEFAULT 0,
			lat_b0        INTEGER DEFAULT 0,
			lat_b1        INTEGER DEFAULT 0,
			lat_b2        INTEGER DEFAULT 0,
			lat_b3        INTEGER DEFAULT 0,
			lat_b4        INTEGER DEFAULT 0,
			lat_b5        INTEGER DEFAULT 0,
			lat_b6        INTEGER DEFAULT 0,
			lat_b7        INTEGER DEFAULT 0,
			lat_b8        INTEGER DEFAULT 0,
			lat_b9        INTEGER DEFAULT 0,
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
			bot_human     INTEGER DEFAULT 0,
			bot_good      INTEGER DEFAULT 0,
			bot_bad       INTEGER DEFAULT 0,
			lat_b0        INTEGER DEFAULT 0,
			lat_b1        INTEGER DEFAULT 0,
			lat_b2        INTEGER DEFAULT 0,
			lat_b3        INTEGER DEFAULT 0,
			lat_b4        INTEGER DEFAULT 0,
			lat_b5        INTEGER DEFAULT 0,
			lat_b6        INTEGER DEFAULT 0,
			lat_b7        INTEGER DEFAULT 0,
			lat_b8        INTEGER DEFAULT 0,
			lat_b9        INTEGER DEFAULT 0,
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
			bot_human     INTEGER DEFAULT 0,
			bot_good      INTEGER DEFAULT 0,
			bot_bad       INTEGER DEFAULT 0,
			lat_b0        INTEGER DEFAULT 0,
			lat_b1        INTEGER DEFAULT 0,
			lat_b2        INTEGER DEFAULT 0,
			lat_b3        INTEGER DEFAULT 0,
			lat_b4        INTEGER DEFAULT 0,
			lat_b5        INTEGER DEFAULT 0,
			lat_b6        INTEGER DEFAULT 0,
			lat_b7        INTEGER DEFAULT 0,
			lat_b8        INTEGER DEFAULT 0,
			lat_b9        INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost)
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_1m_vhost ON metrics_1m(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_1h_vhost ON metrics_1h(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_1d_vhost ON metrics_1d(vhost, timestamp);

		CREATE TABLE IF NOT EXISTS metrics_country_1m (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			country_code TEXT NOT NULL,
			country_name TEXT DEFAULT '',
			flag         TEXT DEFAULT '',
			requests     INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, country_code)
		);

		CREATE TABLE IF NOT EXISTS metrics_country_1h (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			country_code TEXT NOT NULL,
			country_name TEXT DEFAULT '',
			flag         TEXT DEFAULT '',
			requests     INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, country_code)
		);

		CREATE TABLE IF NOT EXISTS metrics_country_1d (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			country_code TEXT NOT NULL,
			country_name TEXT DEFAULT '',
			flag         TEXT DEFAULT '',
			requests     INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, country_code)
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_country_1m_vhost ON metrics_country_1m(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_country_1h_vhost ON metrics_country_1h(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_country_1d_vhost ON metrics_country_1d(vhost, timestamp);

		CREATE TABLE IF NOT EXISTS metrics_path_1m (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			path         TEXT NOT NULL,
			requests     INTEGER DEFAULT 0,
			total_time   REAL DEFAULT 0,
			s2xx         INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, path)
		);

		CREATE TABLE IF NOT EXISTS metrics_path_1h (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			path         TEXT NOT NULL,
			requests     INTEGER DEFAULT 0,
			total_time   REAL DEFAULT 0,
			s2xx         INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, path)
		);

		CREATE TABLE IF NOT EXISTS metrics_path_1d (
			timestamp    INTEGER NOT NULL,
			vhost        TEXT NOT NULL,
			path         TEXT NOT NULL,
			requests     INTEGER DEFAULT 0,
			total_time   REAL DEFAULT 0,
			s2xx         INTEGER DEFAULT 0,
			PRIMARY KEY (timestamp, vhost, path)
		);

		CREATE INDEX IF NOT EXISTS idx_metrics_path_1m_vhost ON metrics_path_1m(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_path_1h_vhost ON metrics_path_1h(vhost, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_path_1d_vhost ON metrics_path_1d(vhost, timestamp);
	`

	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Safe column migration for existing databases
	tables := []string{"metrics_1m", "metrics_1h", "metrics_1d"}
	cols := []string{
		"bot_human INTEGER DEFAULT 0",
		"bot_good INTEGER DEFAULT 0",
		"bot_bad INTEGER DEFAULT 0",
		"lat_b0 INTEGER DEFAULT 0",
		"lat_b1 INTEGER DEFAULT 0",
		"lat_b2 INTEGER DEFAULT 0",
		"lat_b3 INTEGER DEFAULT 0",
		"lat_b4 INTEGER DEFAULT 0",
		"lat_b5 INTEGER DEFAULT 0",
		"lat_b6 INTEGER DEFAULT 0",
		"lat_b7 INTEGER DEFAULT 0",
		"lat_b8 INTEGER DEFAULT 0",
		"lat_b9 INTEGER DEFAULT 0",
	}
	for _, table := range tables {
		for _, col := range cols {
			s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", table, col))
		}
	}

	return nil
}

// Record inserts a 1-minute aggregated data point.
func (s *SQLiteStore) Record(ctx context.Context, timestamp time.Time, vhost string, snap metrics.VHostMetrics, latBuckets ...[10]int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Round timestamp to minute boundary
	ts := timestamp.Truncate(time.Minute).Unix()

	requests := snap.StatusCodes.Total()
	if requests == 0 && snap.RPS > 0 {
		requests = int64(snap.RPS * 60)
	}

	var latB [10]int64
	if len(latBuckets) > 0 {
		latB = latBuckets[0]
	} else if len(snap.LatencyBuckets) == 10 {
		for i := 0; i < 10; i++ {
			latB[i] = snap.LatencyBuckets[i]
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_1m 
			(timestamp, vhost, requests, s2xx, s3xx, s4xx, s5xx, 
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors,
			 bot_human, bot_good, bot_bad,
			 lat_b0, lat_b1, lat_b2, lat_b3, lat_b4, lat_b5, lat_b6, lat_b7, lat_b8, lat_b9)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ts, vhost,
		requests,
		snap.StatusCodes.S2xx, snap.StatusCodes.S3xx,
		snap.StatusCodes.S4xx, snap.StatusCodes.S5xx,
		snap.Latency.Avg, snap.Latency.P95, snap.Latency.P99,
		snap.Bandwidth.In, snap.Bandwidth.Out,
		snap.UniqueVisitors,
		snap.BotTraffic.HumanRequests, snap.BotTraffic.GoodBotRequests, snap.BotTraffic.BadBotRequests,
		latB[0], latB[1], latB[2], latB[3], latB[4], latB[5], latB[6], latB[7], latB[8], latB[9],
	)

	return err
}

// RecordPaths inserts 1-minute aggregated path metrics for a vhost.
func (s *SQLiteStore) RecordPaths(ctx context.Context, timestamp time.Time, vhost string, paths []metrics.PathStats) error {
	if len(paths) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := timestamp.Truncate(time.Minute).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO metrics_path_1m
			(timestamp, vhost, path, requests, total_time, s2xx)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range paths {
		if p.Path == "" || p.Count <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, ts, vhost, p.Path, p.Count, p.TotalTime, p.S2xxCount); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// RecordCountries inserts 1-minute aggregated country metrics for a vhost.
func (s *SQLiteStore) RecordCountries(ctx context.Context, timestamp time.Time, vhost string, countries []metrics.CountryStats) error {
	if len(countries) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ts := timestamp.Truncate(time.Minute).Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO metrics_country_1m
			(timestamp, vhost, country_code, country_name, flag, requests)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range countries {
		if c.CountryCode == "" || c.Count <= 0 {
			continue
		}
		if _, err := stmt.ExecContext(ctx, ts, vhost, c.CountryCode, c.CountryName, c.Flag, c.Count); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// QueryHistory retrieves historical data for a vhost within the given time range.
// QueryHistory retrieves aggregated time-series data for a vhost within a time range.
// If vhost is "all" or "", it aggregates across all vhosts.
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

	var query string
	var args []interface{}

	if vhost == "all" || vhost == "_all" || vhost == "" {
		query = fmt.Sprintf(`
			SELECT timestamp, SUM(requests), SUM(s2xx), SUM(s3xx), SUM(s4xx), SUM(s5xx),
			       AVG(latency_avg), MAX(latency_p95), MAX(latency_p99), SUM(bytes_in), SUM(bytes_out), SUM(unique_visitors),
			       SUM(bot_human), SUM(bot_good), SUM(bot_bad),
			       SUM(lat_b0), SUM(lat_b1), SUM(lat_b2), SUM(lat_b3), SUM(lat_b4),
			       SUM(lat_b5), SUM(lat_b6), SUM(lat_b7), SUM(lat_b8), SUM(lat_b9)
			FROM %s
			WHERE timestamp >= ? AND timestamp <= ?
			GROUP BY timestamp
			ORDER BY timestamp ASC
		`, table)
		args = []interface{}{from.Unix(), to.Unix()}
	} else {
		query = fmt.Sprintf(`
			SELECT timestamp, requests, s2xx, s3xx, s4xx, s5xx,
			       latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors,
			       bot_human, bot_good, bot_bad,
			       lat_b0, lat_b1, lat_b2, lat_b3, lat_b4, lat_b5, lat_b6, lat_b7, lat_b8, lat_b9
			FROM %s
			WHERE vhost = ? AND timestamp >= ? AND timestamp <= ?
			ORDER BY timestamp ASC
		`, table)
		args = []interface{}{vhost, from.Unix(), to.Unix()}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying history: %w", err)
	}
	defer rows.Close()

	history := &metrics.VHostHistory{
		RPS:        make([]metrics.HistoryPoint, 0),
		LatencyP95: make([]metrics.HistoryPoint, 0),
		ErrorRate:  make([]metrics.HistoryPoint, 0),
		Bandwidth:  make([]metrics.HistoryPoint, 0),
		Summary:    &metrics.HistorySummary{},
	}

	var totalReqs, total2xx, total3xx, total4xx, total5xx int64
	var totalBytesIn, totalBytesOut, maxVisitors int64
	var totalBotHuman, totalBotGood, totalBotBad int64
	var totalLatBuckets [10]int64
	var latSum float64
	var pointCount int64

	for rows.Next() {
		var ts int64
		var requests, s2xx, s3xx, s4xx, s5xx int64
		var latAvg, latP95, latP99 float64
		var bytesIn, bytesOut, visitors int64
		var botHuman, botGood, botBad int64
		var latB [10]int64

		if err := rows.Scan(&ts, &requests, &s2xx, &s3xx, &s4xx, &s5xx,
			&latAvg, &latP95, &latP99, &bytesIn, &bytesOut, &visitors,
			&botHuman, &botGood, &botBad,
			&latB[0], &latB[1], &latB[2], &latB[3], &latB[4],
			&latB[5], &latB[6], &latB[7], &latB[8], &latB[9]); err != nil {
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

		// Accumulate summary totals
		totalReqs += requests
		total2xx += s2xx
		total3xx += s3xx
		total4xx += s4xx
		total5xx += s5xx
		totalBytesIn += bytesIn
		totalBytesOut += bytesOut
		if visitors > maxVisitors {
			maxVisitors = visitors
		}
		totalBotHuman += botHuman
		totalBotGood += botGood
		totalBotBad += botBad
		for b := 0; b < 10; b++ {
			totalLatBuckets[b] += latB[b]
		}
		if latAvg > 0 {
			latSum += latAvg
			pointCount++
		}
	}
	queryErr := rows.Err()
	rows.Close()

	totalCodes := total2xx + total3xx + total4xx + total5xx
	var overallErrRate float64
	if totalCodes > 0 {
		overallErrRate = float64(total4xx+total5xx) / float64(totalCodes) * 100
	}

	var overallAvgLat float64
	if pointCount > 0 {
		overallAvgLat = latSum / float64(pointCount)
	}

	var overallAvgRPS float64
	secs := duration.Seconds()
	if secs > 0 {
		overallAvgRPS = float64(totalReqs) / secs
	}

	history.Summary = &metrics.HistorySummary{
		TotalRequests: totalReqs,
		AvgRPS:        overallAvgRPS,
		AvgLatency:    overallAvgLat,
		ErrorRate:     overallErrRate,
		StatusCodes: metrics.StatusCodes{
			S2xx: total2xx,
			S3xx: total3xx,
			S4xx: total4xx,
			S5xx: total5xx,
		},
		TotalBytesIn:   totalBytesIn,
		TotalBytesOut:  totalBytesOut,
		UniqueVisitors: maxVisitors,
		BotTraffic: metrics.BotTrafficStats{
			HumanRequests:   totalBotHuman,
			GoodBotRequests: totalBotGood,
			BadBotRequests:  totalBotBad,
		},
		LatencyBuckets: totalLatBuckets[:],
	}

	// Query top countries for the period
	countryTable := "metrics_country_1m"
	switch table {
	case "metrics_1d":
		countryTable = "metrics_country_1d"
	case "metrics_1h":
		countryTable = "metrics_country_1h"
	}

	var countryQuery string
	var countryArgs []interface{}
	if vhost == "all" || vhost == "_all" || vhost == "" {
		countryQuery = fmt.Sprintf(`
			SELECT country_code, country_name, flag, SUM(requests) as req_count
			FROM %s
			WHERE timestamp >= ? AND timestamp <= ?
			GROUP BY country_code
			ORDER BY req_count DESC
			LIMIT 10
		`, countryTable)
		countryArgs = []interface{}{from.Unix(), to.Unix()}
	} else {
		countryQuery = fmt.Sprintf(`
			SELECT country_code, country_name, flag, SUM(requests) as req_count
			FROM %s
			WHERE vhost = ? AND timestamp >= ? AND timestamp <= ?
			GROUP BY country_code
			ORDER BY req_count DESC
			LIMIT 10
		`, countryTable)
		countryArgs = []interface{}{vhost, from.Unix(), to.Unix()}
	}

	topCountries := make([]metrics.CountryStats, 0)
	cRows, err := s.db.QueryContext(ctx, countryQuery, countryArgs...)
	if err == nil {
		var grandCountryCount int64
		for cRows.Next() {
			var code, name, flag string
			var count int64
			if err := cRows.Scan(&code, &name, &flag, &count); err == nil {
				grandCountryCount += count
				topCountries = append(topCountries, metrics.CountryStats{
					CountryCode: code,
					CountryName: name,
					Flag:        flag,
					Count:       count,
				})
			}
		}
		cRows.Close()

		for i := range topCountries {
			if secs > 0 {
				topCountries[i].RPS = float64(topCountries[i].Count) / secs
			}
			denom := grandCountryCount
			if denom == 0 && totalReqs > 0 {
				denom = totalReqs
			}
			if denom > 0 {
				topCountries[i].Percentage = (float64(topCountries[i].Count) / float64(denom)) * 100
			}
		}
	}
	history.Summary.TopCountries = topCountries

	// Query top paths for the period
	pathTable := "metrics_path_1m"
	switch table {
	case "metrics_1d":
		pathTable = "metrics_path_1d"
	case "metrics_1h":
		pathTable = "metrics_path_1h"
	}

	var pathQuery string
	var pathArgs []interface{}
	if vhost == "all" || vhost == "_all" || vhost == "" {
		pathQuery = fmt.Sprintf(`
			SELECT vhost, path, SUM(requests) as req_count, SUM(total_time) as tot_time, SUM(s2xx) as s2xx_count
			FROM %s
			WHERE timestamp >= ? AND timestamp <= ?
			GROUP BY vhost, path
			ORDER BY req_count DESC
			LIMIT 10
		`, pathTable)
		pathArgs = []interface{}{from.Unix(), to.Unix()}
	} else {
		pathQuery = fmt.Sprintf(`
			SELECT vhost, path, SUM(requests) as req_count, SUM(total_time) as tot_time, SUM(s2xx) as s2xx_count
			FROM %s
			WHERE vhost = ? AND timestamp >= ? AND timestamp <= ?
			GROUP BY vhost, path
			ORDER BY req_count DESC
			LIMIT 10
		`, pathTable)
		pathArgs = []interface{}{vhost, from.Unix(), to.Unix()}
	}

	topPaths := make([]metrics.PathStats, 0)
	pRows, err := s.db.QueryContext(ctx, pathQuery, pathArgs...)
	if err == nil {
		for pRows.Next() {
			var vh, path string
			var count, s2xx int64
			var totTime float64
			if err := pRows.Scan(&vh, &path, &count, &totTime, &s2xx); err == nil {
				avgLat := 0.0
				if count > 0 {
					avgLat = (totTime / float64(count)) * 1000
				}
				s2xxPct := 0.0
				if count > 0 {
					s2xxPct = (float64(s2xx) / float64(count)) * 100
				}
				rps := 0.0
				if secs > 0 {
					rps = float64(count) / secs
				}
				topPaths = append(topPaths, metrics.PathStats{
					VHost:      vh,
					Path:       path,
					Count:      count,
					RPS:        rps,
					AvgLatency: avgLat,
					Status2xx:  s2xxPct,
					TotalTime:  totTime,
					S2xxCount:  s2xx,
				})
			}
		}
		pRows.Close()
	}
	history.Summary.TopPaths = topPaths

	return history, queryErr
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
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors,
			 bot_human, bot_good, bot_bad,
			 lat_b0, lat_b1, lat_b2, lat_b3, lat_b4, lat_b5, lat_b6, lat_b7, lat_b8, lat_b9)
		SELECT 
			(timestamp / 3600) * 3600 as hour_ts,
			vhost,
			SUM(requests),
			SUM(s2xx), SUM(s3xx), SUM(s4xx), SUM(s5xx),
			AVG(latency_avg), MAX(latency_p95), MAX(latency_p99),
			SUM(bytes_in), SUM(bytes_out),
			MAX(unique_visitors),
			SUM(bot_human), SUM(bot_good), SUM(bot_bad),
			SUM(lat_b0), SUM(lat_b1), SUM(lat_b2), SUM(lat_b3), SUM(lat_b4),
			SUM(lat_b5), SUM(lat_b6), SUM(lat_b7), SUM(lat_b8), SUM(lat_b9)
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

	// Rollup path 1m -> 1h (for data older than 24 hours)
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_path_1h
			(timestamp, vhost, path, requests, total_time, s2xx)
		SELECT 
			(timestamp / 3600) * 3600 as hour_ts,
			vhost,
			path,
			SUM(requests),
			SUM(total_time),
			SUM(s2xx)
		FROM metrics_path_1m
		WHERE timestamp < ?
		GROUP BY hour_ts, vhost, path
	`, cutoff24h)
	if err != nil {
		return fmt.Errorf("hourly path rollup: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_path_1m WHERE timestamp < ?", cutoff24h)
	if err != nil {
		return fmt.Errorf("cleaning 1m path data: %w", err)
	}

	// Rollup country 1m -> 1h (for data older than 24 hours)
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_country_1h
			(timestamp, vhost, country_code, country_name, flag, requests)
		SELECT 
			(timestamp / 3600) * 3600 as hour_ts,
			vhost,
			country_code,
			country_name,
			flag,
			SUM(requests)
		FROM metrics_country_1m
		WHERE timestamp < ?
		GROUP BY hour_ts, vhost, country_code
	`, cutoff24h)
	if err != nil {
		return fmt.Errorf("hourly country rollup: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_country_1m WHERE timestamp < ?", cutoff24h)
	if err != nil {
		return fmt.Errorf("cleaning 1m country data: %w", err)
	}

	// Rollup 1h -> 1d (for data older than 7 days)
	cutoff7d := time.Now().Add(-7 * 24 * time.Hour).Unix()
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_1d
			(timestamp, vhost, requests, s2xx, s3xx, s4xx, s5xx,
			 latency_avg, latency_p95, latency_p99, bytes_in, bytes_out, unique_visitors,
			 bot_human, bot_good, bot_bad,
			 lat_b0, lat_b1, lat_b2, lat_b3, lat_b4, lat_b5, lat_b6, lat_b7, lat_b8, lat_b9)
		SELECT 
			(timestamp / 86400) * 86400 as day_ts,
			vhost,
			SUM(requests),
			SUM(s2xx), SUM(s3xx), SUM(s4xx), SUM(s5xx),
			AVG(latency_avg), MAX(latency_p95), MAX(latency_p99),
			SUM(bytes_in), SUM(bytes_out),
			MAX(unique_visitors),
			SUM(bot_human), SUM(bot_good), SUM(bot_bad),
			SUM(lat_b0), SUM(lat_b1), SUM(lat_b2), SUM(lat_b3), SUM(lat_b4),
			SUM(lat_b5), SUM(lat_b6), SUM(lat_b7), SUM(lat_b8), SUM(lat_b9)
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

	// Rollup path 1h -> 1d (for data older than 7 days)
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_path_1d
			(timestamp, vhost, path, requests, total_time, s2xx)
		SELECT 
			(timestamp / 86400) * 86400 as day_ts,
			vhost,
			path,
			SUM(requests),
			SUM(total_time),
			SUM(s2xx)
		FROM metrics_path_1h
		WHERE timestamp < ?
		GROUP BY day_ts, vhost, path
	`, cutoff7d)
	if err != nil {
		return fmt.Errorf("daily path rollup: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_path_1h WHERE timestamp < ?", cutoff7d)
	if err != nil {
		return fmt.Errorf("cleaning 1h path data: %w", err)
	}

	// Rollup country 1h -> 1d (for data older than 7 days)
	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO metrics_country_1d
			(timestamp, vhost, country_code, country_name, flag, requests)
		SELECT 
			(timestamp / 86400) * 86400 as day_ts,
			vhost,
			country_code,
			country_name,
			flag,
			SUM(requests)
		FROM metrics_country_1h
		WHERE timestamp < ?
		GROUP BY day_ts, vhost, country_code
	`, cutoff7d)
	if err != nil {
		return fmt.Errorf("daily country rollup: %w", err)
	}

	_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_country_1h WHERE timestamp < ?", cutoff7d)
	if err != nil {
		return fmt.Errorf("cleaning 1h country data: %w", err)
	}

	// Apply retention policy
	if s.retentionDays > 0 {
		cutoffRetention := time.Now().Add(-time.Duration(s.retentionDays) * 24 * time.Hour).Unix()
		_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_1d WHERE timestamp < ?", cutoffRetention)
		if err != nil {
			return fmt.Errorf("applying retention: %w", err)
		}
		_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_path_1d WHERE timestamp < ?", cutoffRetention)
		if err != nil {
			return fmt.Errorf("applying path retention: %w", err)
		}
		_, err = s.db.ExecContext(ctx, "DELETE FROM metrics_country_1d WHERE timestamp < ?", cutoffRetention)
		if err != nil {
			return fmt.Errorf("applying country retention: %w", err)
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
			nowUnix := time.Now().Unix()
			now := time.Now()
			for vhost, vhMetrics := range snapshot.VHosts {
				minuteLatBuckets := store.MinuteLatencyBuckets(vhost, now)
				if err := s.Record(ctx, snapshot.Timestamp, vhost, vhMetrics, minuteLatBuckets); err != nil {
					slog.Error("failed to record metrics", "vhost", vhost, "error", err)
				}
				minuteCountries := store.MinuteCountries(vhost, nowUnix)
				if len(minuteCountries) > 0 {
					if err := s.RecordCountries(ctx, snapshot.Timestamp, vhost, minuteCountries); err != nil {
						slog.Error("failed to record country metrics", "vhost", vhost, "error", err)
					}
				}
				minutePaths := store.MinutePaths(vhost, nowUnix)
				if len(minutePaths) > 0 {
					if err := s.RecordPaths(ctx, snapshot.Timestamp, vhost, minutePaths); err != nil {
						slog.Error("failed to record path metrics", "vhost", vhost, "error", err)
					}
				}
			}

		case <-rollupTicker.C:
			if err := s.Rollup(ctx); err != nil {
				slog.Error("rollup failed", "error", err)
			}
		}
	}
}
