// Package geoip provides IP-to-Country resolution using MaxMind/DB-IP MMDB format
// with automatic downloading and zero CGO dependencies.
package geoip

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
)

const (
	// DatabaseMaxAge is the duration after which an existing database file is refreshed.
	DatabaseMaxAge = 30 * 24 * time.Hour
)

// CurrentDBIPURL returns the expected DB-IP Country Lite download URL for the given time.
func CurrentDBIPURL(t time.Time) string {
	return fmt.Sprintf("https://download.db-ip.com/free/dbip-country-lite-%s.mmdb.gz", t.Format("2006-01"))
}

// Provider provides thread-safe IP-to-Country lookups.
type Provider struct {
	mu     sync.RWMutex
	db     *geoip2.Reader
	dbPath string
}

// NewProvider creates a new GeoIP Provider instance.
func NewProvider(dbPath string) *Provider {
	p := &Provider{
		dbPath: dbPath,
	}
	p.tryLoad(dbPath)
	return p
}

// tryLoad attempts to open an MMDB database file.
func (p *Provider) tryLoad(path string) bool {
	if path == "" {
		return false
	}

	reader, err := geoip2.Open(path)
	if err != nil {
		return false
	}

	p.mu.Lock()
	if p.db != nil {
		p.db.Close()
	}
	p.db = reader
	p.mu.Unlock()

	slog.Info("loaded GeoIP database", "path", path)
	return true
}

// Lookup resolves an IP address into a 2-letter ISO country code, country name, and emoji flag.
// Returns empty strings if unresolvable, private, or if database is not loaded.
func (p *Provider) Lookup(ipStr string) (code string, name string, flag string) {
	p.mu.RLock()
	reader := p.db
	p.mu.RUnlock()

	if reader == nil || ipStr == "" {
		return "", "", ""
	}

	ip := net.ParseIP(ipStr)
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return "", "", ""
	}

	record, err := reader.Country(ip)
	if err != nil || record == nil {
		return "", "", ""
	}

	iso := strings.ToUpper(record.Country.IsoCode)
	if iso == "" {
		iso = strings.ToUpper(record.RegisteredCountry.IsoCode)
	}
	if iso == "" {
		return "", "", ""
	}

	countryName := record.Country.Names["en"]
	if countryName == "" {
		countryName = record.RegisteredCountry.Names["en"]
	}
	if countryName == "" {
		countryName = iso
	}

	return iso, countryName, CountryFlag(iso)
}

// CountryFlag converts an ISO 3166-1 alpha-2 country code to its Unicode flag emoji.
func CountryFlag(isoCode string) string {
	isoCode = strings.ToUpper(strings.TrimSpace(isoCode))
	if len(isoCode) != 2 {
		return "🌐"
	}

	r1 := rune(isoCode[0])
	r2 := rune(isoCode[1])

	if r1 < 'A' || r1 > 'Z' || r2 < 'A' || r2 > 'Z' {
		return "🌐"
	}

	// Unicode Regional Indicator Symbols (0x1F1E6 = 🇦)
	return string([]rune{0x1F1E6 + (r1 - 'A'), 0x1F1E6 + (r2 - 'A')})
}

// Close releases the underlying MMDB database reader.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.db != nil {
		err := p.db.Close()
		p.db = nil
		return err
	}
	return nil
}

// AutoUpdate checks if the database needs downloading or updating and downloads it in background.
func (p *Provider) AutoUpdate(ctx context.Context, downloadURL string) {
	needsDownload := false
	info, err := os.Stat(p.dbPath)
	if os.IsNotExist(err) {
		needsDownload = true
	} else if err == nil {
		if time.Since(info.ModTime()) > DatabaseMaxAge {
			needsDownload = true
		}
	}

	if !needsDownload && p.db != nil {
		return
	}

	slog.Info("fetching GeoIP country database in background...", "destination", p.dbPath)

	go func() {
		urls := []string{}
		if downloadURL != "" {
			urls = append(urls, downloadURL)
		} else {
			now := time.Now()
			// Try current month, then last month (if start of month before DB-IP release)
			urls = append(urls, CurrentDBIPURL(now), CurrentDBIPURL(now.AddDate(0, -1, 0)))
		}

		var lastErr error
		for _, u := range urls {
			err := p.downloadAndExtract(ctx, u)
			if err == nil {
				p.tryLoad(p.dbPath)
				return
			}
			lastErr = err
		}

		slog.Warn("failed to auto-download GeoIP database (country resolution disabled until available)", "error", lastErr)
	}()
}

func (p *Provider) downloadAndExtract(ctx context.Context, downloadURL string) error {
	dir := filepath.Dir(p.dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "NginXplorer-AutoUpdater/1.0")

	client := &http.Client{
		Timeout: 3 * time.Minute,
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status: %s", resp.Status)
	}

	tmpFile := p.dbPath + ".tmp"
	outFile, err := os.Create(tmpFile)
	if err != nil {
		return fmt.Errorf("creating temp file %s: %w", tmpFile, err)
	}
	defer func() {
		outFile.Close()
		os.Remove(tmpFile)
	}()

	var srcReader io.Reader = resp.Body
	if strings.HasSuffix(downloadURL, ".gz") {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("initializing gzip reader: %w", err)
		}
		defer gzReader.Close()
		srcReader = gzReader
	}

	if _, err := io.Copy(outFile, srcReader); err != nil {
		return fmt.Errorf("writing decompressed data: %w", err)
	}
	outFile.Close()

	// Verify reader can open the file before renaming
	testReader, err := geoip2.Open(tmpFile)
	if err != nil {
		return fmt.Errorf("verifying downloaded mmdb: %w", err)
	}
	testReader.Close()

	if err := os.Rename(tmpFile, p.dbPath); err != nil {
		return fmt.Errorf("atomic rename to %s: %w", p.dbPath, err)
	}

	slog.Info("GeoIP database download completed successfully", "path", p.dbPath)
	return nil
}
