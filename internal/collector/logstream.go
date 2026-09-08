package collector

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kimusan/nginxplorer/internal/metrics"
)

// LogStreamCollector receives JSON access logs from Nginx via Unix domain socket or tailing a log file.
type LogStreamCollector struct {
	socketPath   string
	logFile      string
	store        *metrics.Store
	anonymizeIPs bool
	stripQuery   bool
}

// NewLogStreamCollector creates a new LogStreamCollector.
func NewLogStreamCollector(socketPath string, logFile string, store *metrics.Store, anonymizeIPs bool, stripQuery bool) *LogStreamCollector {
	return &LogStreamCollector{
		socketPath:   socketPath,
		logFile:      logFile,
		store:        store,
		anonymizeIPs: anonymizeIPs,
		stripQuery:   stripQuery,
	}
}

// Start launches the collector, listening on a socket or tailing a file.
func (c *LogStreamCollector) Start(ctx context.Context) {
	if c.socketPath != "" {
		go c.listenSocket(ctx)
	} else if c.logFile != "" {
		go c.tailFile(ctx)
	}
}

func (c *LogStreamCollector) listenSocket(ctx context.Context) {
	os.Remove(c.socketPath)
	addr, err := net.ResolveUnixAddr("unixgram", c.socketPath)
	if err != nil {
		slog.Error("failed to resolve unix address", "error", err)
		return
	}

	conn, err := net.ListenUnixgram("unixgram", addr)
	if err != nil {
		slog.Error("failed to listen on socket", "error", err)
		return
	}
	defer conn.Close()

	if err := os.Chmod(c.socketPath, 0666); err != nil {
		slog.Error("failed to chmod socket", "error", err)
	}

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 65536)
	for {
		n, _, err := conn.ReadFromUnix(buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("error reading from socket", "error", err)
			continue
		}
		c.processLine(buf[:n])
	}
}

func (c *LogStreamCollector) tailFile(ctx context.Context) {
	file, err := os.Open(c.logFile)
	if err != nil {
		slog.Error("failed to open log file", "error", err)
		return
	}
	defer file.Close()

	if _, err := file.Seek(0, os.SEEK_END); err != nil {
		slog.Error("failed to seek to end of log file", "error", err)
		return
	}

	reader := bufio.NewReader(file)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := reader.ReadBytes('\n')
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		c.processLine(line)
	}
}

type logPayload struct {
	Ts           time.Time `json:"ts"`
	Host         string    `json:"host"`
	Addr         string    `json:"addr"`
	Method       string    `json:"method"`
	Uri          string    `json:"uri"`
	Status       int       `json:"status"`
	Bytes        int64     `json:"bytes"`
	BodyBytes    int64     `json:"body_bytes"`
	ReqLen       int64     `json:"req_len"`
	ReqTime      float64   `json:"req_time"`
	UpstreamTime string    `json:"upstream_time"`
	UpstreamAddr string    `json:"upstream_addr"`
	Ua           string    `json:"ua"`
	Ref          string    `json:"ref"`
}

func (c *LogStreamCollector) processLine(data []byte) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return
	}

	// Find the start of the JSON payload (after the syslog headers)
	idx := bytes.IndexByte(data, '{')
	if idx == -1 {
		return
	}
	jsonBytes := data[idx:]

	var p logPayload
	if err := json.Unmarshal(jsonBytes, &p); err != nil {
		slog.Debug("failed to parse log json", "error", err)
		return
	}

	var upTime float64
	if p.UpstreamTime != "-" && p.UpstreamTime != "" {
		if val, err := strconv.ParseFloat(p.UpstreamTime, 64); err == nil {
			upTime = val
		}
	}

	addr := p.Addr
	if c.anonymizeIPs {
		if lastDot := strings.LastIndexByte(addr, '.'); lastDot != -1 {
			addr = addr[:lastDot] + ".0"
		}
	}

	uri := p.Uri
	if c.stripQuery {
		if qIdx := strings.IndexByte(uri, '?'); qIdx != -1 {
			uri = uri[:qIdx]
		}
	}

	entry := &metrics.LogEntry{
		Timestamp:    p.Ts,
		Host:         p.Host,
		RemoteAddr:   addr,
		Method:       p.Method,
		URI:          uri,
		Status:       p.Status,
		BytesSent:    p.Bytes,
		BodyBytes:    p.BodyBytes,
		RequestLen:   p.ReqLen,
		RequestTime:  p.ReqTime,
		UpstreamTime: upTime,
		UpstreamAddr: p.UpstreamAddr,
		UserAgent:    p.Ua,
		Referer:      p.Ref,
	}

	c.store.RecordEntry(entry)
}
