package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// NtfyDispatcher sends alerts to ntfy.sh topics.
type NtfyDispatcher struct {
	url    string
	token  string
	client *http.Client
}

// NewNtfyDispatcher creates a new NtfyDispatcher.
func NewNtfyDispatcher(url, token string) *NtfyDispatcher {
	return &NtfyDispatcher{
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *NtfyDispatcher) Name() string { return "ntfy" }

func (d *NtfyDispatcher) Send(ctx context.Context, event AlertEvent, dashboardURL string) error {
	var title, priority, tags string
	if event.State == StateFiring {
		title = fmt.Sprintf("🚨 [FIRING] %s (%s)", event.RuleName, event.VHost)
		priority = "urgent"
		tags = "warning,rotating_light"
	} else {
		title = fmt.Sprintf("✅ [RESOLVED] %s (%s)", event.RuleName, event.VHost)
		priority = "default"
		tags = "white_check_mark,green_circle"
	}

	bodyText := event.Message
	if event.Details != "" {
		bodyText += "\n" + event.Details
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.url, strings.NewReader(bodyText))
	if err != nil {
		return fmt.Errorf("creating ntfy request: %w", err)
	}

	req.Header.Set("Title", title)
	req.Header.Set("Priority", priority)
	req.Header.Set("Tags", tags)
	if dashboardURL != "" {
		req.Header.Set("Click", dashboardURL)
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending ntfy notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("alert notification dispatched via ntfy", "rule", event.RuleName, "vhost", event.VHost, "state", event.State)
	return nil
}

// PushbulletDispatcher sends alerts via Pushbullet API.
type PushbulletDispatcher struct {
	apiToken string
	deviceID string
	client   *http.Client
}

// NewPushbulletDispatcher creates a new PushbulletDispatcher.
func NewPushbulletDispatcher(apiToken, deviceID string) *PushbulletDispatcher {
	return &PushbulletDispatcher{
		apiToken: apiToken,
		deviceID: deviceID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *PushbulletDispatcher) Name() string { return "pushbullet" }

func (d *PushbulletDispatcher) Send(ctx context.Context, event AlertEvent, dashboardURL string) error {
	var title string
	if event.State == StateFiring {
		title = fmt.Sprintf("🚨 [FIRING] %s (%s)", event.RuleName, event.VHost)
	} else {
		title = fmt.Sprintf("✅ [RESOLVED] %s (%s)", event.RuleName, event.VHost)
	}

	bodyText := event.Message
	if event.Details != "" {
		bodyText += "\n" + event.Details
	}
	if dashboardURL != "" {
		bodyText += "\nDashboard: " + dashboardURL
	}

	payload := map[string]string{
		"type":  "note",
		"title": title,
		"body":  bodyText,
	}
	if d.deviceID != "" {
		payload["device_iden"] = d.deviceID
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling pushbullet payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.pushbullet.com/v2/pushes", bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("creating pushbullet request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Access-Token", d.apiToken)

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending pushbullet notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pushbullet returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("alert notification dispatched via pushbullet", "rule", event.RuleName, "vhost", event.VHost, "state", event.State)
	return nil
}

// SlackDispatcher sends alerts to Slack, Discord, or Mattermost incoming webhooks.
type SlackDispatcher struct {
	url    string
	client *http.Client
}

// NewSlackDispatcher creates a new Slack/Discord dispatcher.
func NewSlackDispatcher(url string) *SlackDispatcher {
	// Discord incoming webhooks accept Slack-compatible payload when appending /slack
	if strings.Contains(url, "discord.com/api/webhooks") && !strings.HasSuffix(url, "/slack") {
		url += "/slack"
	}

	return &SlackDispatcher{
		url:    url,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *SlackDispatcher) Name() string { return "slack" }

func (d *SlackDispatcher) Send(ctx context.Context, event AlertEvent, dashboardURL string) error {
	color := "#e02424" // Red
	header := fmt.Sprintf("🚨 *[FIRING] %s* on `%s`", event.RuleName, event.VHost)
	if event.State == StateResolved {
		color = "#0e9f6e" // Green
		header = fmt.Sprintf("✅ *[RESOLVED] %s* on `%s`", event.RuleName, event.VHost)
	}

	text := fmt.Sprintf("%s\n%s", header, event.Message)
	if event.Details != "" {
		text += "\n" + event.Details
	}
	if dashboardURL != "" {
		text += fmt.Sprintf("\n<%s|View Dashboard>", dashboardURL)
	}

	payload := map[string]interface{}{
		"text": text,
		"attachments": []map[string]interface{}{
			{
				"color": color,
				"text":  event.Message,
			},
		},
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.url, bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("creating slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending slack notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("slack returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("alert notification dispatched via slack/discord", "rule", event.RuleName, "vhost", event.VHost, "state", event.State)
	return nil
}

// GenericWebhookDispatcher posts alert JSON payloads to any HTTP endpoint.
type GenericWebhookDispatcher struct {
	url    string
	token  string
	client *http.Client
}

// NewGenericWebhookDispatcher creates a new GenericWebhookDispatcher.
func NewGenericWebhookDispatcher(url, token string) *GenericWebhookDispatcher {
	return &GenericWebhookDispatcher{
		url:    url,
		token:  token,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (d *GenericWebhookDispatcher) Name() string { return "webhook" }

func (d *GenericWebhookDispatcher) Send(ctx context.Context, event AlertEvent, dashboardURL string) error {
	payload := map[string]interface{}{
		"event":         event,
		"dashboard_url": dashboardURL,
		"timestamp":     time.Now().Unix(),
	}

	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", d.url, bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("creating webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	slog.Info("alert notification dispatched via generic webhook", "rule", event.RuleName, "vhost", event.VHost, "state", event.State)
	return nil
}
