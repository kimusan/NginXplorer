package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kimusan/nginxplorer/internal/metrics"
)

// SSEClient connects to the NginXplorer daemon's SSE stream and
// delivers parsed metric snapshots to the TUI via a channel.
type SSEClient struct {
	url      string
	token    string
	Snapshots chan *metrics.Snapshot
	VHosts    chan []string
	Errors    chan error
	done     chan struct{}
}

// NewSSEClient creates a new SSE client targeting the given daemon URL.
func NewSSEClient(url, token string) *SSEClient {
	return &SSEClient{
		url:       url,
		token:     token,
		Snapshots: make(chan *metrics.Snapshot, 10),
		VHosts:    make(chan []string, 1),
		Errors:    make(chan error, 5),
		done:      make(chan struct{}),
	}
}

// Connect establishes the SSE connection and starts reading events.
func (c *SSEClient) Connect() {
	go c.connectLoop()
}

// Close terminates the SSE connection.
func (c *SSEClient) Close() {
	close(c.done)
}

func (c *SSEClient) connectLoop() {
	backoff := 1 * time.Second

	for {
		select {
		case <-c.done:
			return
		default:
		}

		err := c.stream()
		if err != nil {
			select {
			case c.Errors <- err:
			default:
			}
		}

		// Wait before reconnecting
		select {
		case <-c.done:
			return
		case <-time.After(backoff):
		}

		// Exponential backoff up to 10s
		backoff = backoff * 2
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}
	}
}

func (c *SSEClient) stream() error {
	streamURL := c.url + "/api/v1/stream"
	if c.token != "" {
		streamURL += "?token=" + c.token
	}

	req, err := http.NewRequest("GET", streamURL, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	client := &http.Client{
		Timeout: 0, // No timeout for SSE
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", c.url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication required (401) — use --token flag")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d from %s", resp.StatusCode, c.url)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024) // 256KB buffer

	var eventType string

	for scanner.Scan() {
		select {
		case <-c.done:
			return nil
		default:
		}

		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			c.handleEvent(eventType, data)
			eventType = ""
			continue
		}

		// Empty line or comment — skip
	}

	return scanner.Err()
}

func (c *SSEClient) handleEvent(eventType, data string) {
	switch eventType {
	case "snapshot":
		// Initial snapshot with vhost list and current state
		var initial struct {
			VHosts  []string          `json:"vhosts"`
			Current *metrics.Snapshot `json:"current"`
		}
		if err := json.Unmarshal([]byte(data), &initial); err != nil {
			return
		}
		if initial.VHosts != nil {
			select {
			case c.VHosts <- initial.VHosts:
			default:
			}
		}
		if initial.Current != nil {
			select {
			case c.Snapshots <- initial.Current:
			default:
			}
		}

	case "metrics":
		var snap metrics.Snapshot
		if err := json.Unmarshal([]byte(data), &snap); err != nil {
			return
		}
		select {
		case c.Snapshots <- &snap:
		default:
			// Drop if TUI isn't consuming fast enough
		}
	}
}
