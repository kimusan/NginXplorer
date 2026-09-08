package tui

import (
	"bufio"
	"bytes"
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
	url       string
	token     string
	username  string
	password  string
	Snapshots chan *metrics.Snapshot
	VHosts    chan []string
	Errors    chan error
	done      chan struct{}
}

// NewSSEClient creates a new SSE client targeting the given daemon URL.
func NewSSEClient(url, token, username, password string) *SSEClient {
	return &SSEClient{
		url:       url,
		token:     token,
		username:  username,
		password:  password,
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

func (c *SSEClient) login() error {
	if c.username == "" || c.password == "" {
		return nil
	}

	payload, err := json.Marshal(map[string]string{
		"username": c.username,
		"password": c.password,
	})
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(c.url+"/api/v1/auth/login", "application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login failed: invalid credentials (status %d)", resp.StatusCode)
	}

	var res struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return fmt.Errorf("decoding login response: %w", err)
	}

	c.token = res.Token
	return nil
}

func (c *SSEClient) stream() error {
	if c.token == "" && c.username != "" && c.password != "" {
		if err := c.login(); err != nil {
			return err
		}
	}

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
		return fmt.Errorf("auth required (401) — pass -user and -pass, or -token")
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
