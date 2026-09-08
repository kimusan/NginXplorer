package alerting

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kimusan/nginxplorer/internal/config"
	"github.com/kimusan/nginxplorer/internal/metrics"
)

// Engine manages alert rule evaluation, state transitions, and notification dispatching.
type Engine struct {
	mu           sync.RWMutex
	cfg          config.AlertsConfig
	store        *metrics.Store
	cooldown     time.Duration
	rules        []*AlertRule
	dispatchers  []Dispatcher
	dashboardURL string

	// State
	activeAlerts map[string]*AlertEvent // key: rule.Name + ":" + rule.VHost
	recentEvents []AlertEvent
	maxEvents    int
	stopCh       chan struct{}
}

// NewEngine creates and configures an Alerting Engine.
func NewEngine(cfg config.AlertsConfig, store *metrics.Store) *Engine {
	cooldown := 10 * time.Minute
	if cfg.Cooldown != "" {
		if d, err := time.ParseDuration(cfg.Cooldown); err == nil && d > 0 {
			cooldown = d
		}
	}

	var dispatchers []Dispatcher
	for _, ch := range cfg.Channels {
		switch stringsToLower(ch.Type) {
		case "ntfy":
			if ch.URL != "" {
				dispatchers = append(dispatchers, NewNtfyDispatcher(ch.URL, ch.Token))
			}
		case "pushbullet":
			token := ch.APIToken
			if token == "" {
				token = ch.Token
			}
			if token != "" {
				dispatchers = append(dispatchers, NewPushbulletDispatcher(token, ch.DeviceID))
			}
		case "slack", "discord":
			if ch.URL != "" {
				dispatchers = append(dispatchers, NewSlackDispatcher(ch.URL))
			}
		case "webhook":
			if ch.URL != "" {
				dispatchers = append(dispatchers, NewGenericWebhookDispatcher(ch.URL, ch.Token))
			}
		}
	}

	var rules []*AlertRule
	for _, r := range cfg.Rules {
		dur := 1 * time.Minute
		if r.Duration != "" {
			if d, err := time.ParseDuration(r.Duration); err == nil && d > 0 {
				dur = d
			}
		}

		vhost := r.VHost
		if vhost == "" {
			vhost = "all"
		}

		minReqs := r.MinRequests
		minErrs := r.MinErrors
		if r.Metric == "error_rate" {
			if minReqs <= 0 {
				minReqs = 20 // default: require at least 20 requests in window before evaluating error %
			}
			if minErrs <= 0 {
				minErrs = 5 // default: require at least 5 errors
			}
		}

		rules = append(rules, &AlertRule{
			Name:        r.Name,
			VHost:       vhost,
			Metric:      r.Metric,
			Threshold:   r.Threshold,
			Duration:    dur,
			MinRequests: minReqs,
			MinErrors:   minErrs,
		})
	}

	return &Engine{
		cfg:          cfg,
		store:        store,
		cooldown:     cooldown,
		rules:        rules,
		dispatchers:  dispatchers,
		dashboardURL: cfg.DashboardURL,
		activeAlerts: make(map[string]*AlertEvent),
		recentEvents: make([]AlertEvent, 0, 50),
		maxEvents:    50,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the alert evaluation loop.
func (e *Engine) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	slog.Info("alerting engine started", "rules_count", len(e.rules), "channels_count", len(e.dispatchers))

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case now := <-ticker.C:
			e.evaluate(now)
		}
	}
}

// Stop terminates the evaluation loop.
func (e *Engine) Stop() {
	close(e.stopCh)
}

// evaluate checks all rules against the latest snapshot.
func (e *Engine) evaluate(now time.Time) {
	if e.store == nil {
		return
	}

	snap := e.store.Snapshot()
	if snap == nil {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	for _, rule := range e.rules {
		vm, ok := e.getMetricsForRule(snap, rule.VHost)
		if !ok {
			continue
		}

		val, totalReqs, totalErrs := e.extractMetric(rule.Metric, vm)
		isBreached := e.checkThreshold(rule, val, totalReqs, totalErrs)

		ruleKey := fmt.Sprintf("%s:%s", rule.Name, rule.VHost)

		if isBreached {
			if rule.breachSince.IsZero() {
				rule.breachSince = now
			}

			// Must sustain breach for rule.Duration
			if now.Sub(rule.breachSince) >= rule.Duration {
				shouldFire := false
				if rule.activeEvent == nil {
					shouldFire = true
				} else if now.Sub(rule.lastFiring) >= e.cooldown {
					shouldFire = true
				}

				if shouldFire {
					event := AlertEvent{
						ID:        fmt.Sprintf("alert-%d", now.UnixNano()),
						RuleName:  rule.Name,
						VHost:     rule.VHost,
						Metric:    rule.Metric,
						Threshold: rule.Threshold,
						Value:     val,
						State:     StateFiring,
						StartedAt: now,
						Message:   fmt.Sprintf("%s breached threshold: %.1f (threshold: %.1f)", rule.Name, val, rule.Threshold),
						Details:   fmt.Sprintf("VHost: %s | Total Requests: %d | Errors: %d", rule.VHost, totalReqs, totalErrs),
					}

					rule.activeEvent = &event
					rule.lastFiring = now
					e.activeAlerts[ruleKey] = &event
					e.recordEvent(event)

					// Dispatch notification
					e.dispatchAsync(event)
				}
			}
		} else {
			rule.breachSince = time.Time{}

			// Check if recovering from active alert
			if rule.activeEvent != nil && rule.activeEvent.State == StateFiring {
				resolvedEvent := *rule.activeEvent
				resolvedEvent.State = StateResolved
				resolvedEvent.ResolvedAt = &now
				resolvedEvent.Message = fmt.Sprintf("%s has recovered. Current value: %.1f (threshold: %.1f)", rule.Name, val, rule.Threshold)

				delete(e.activeAlerts, ruleKey)
				rule.activeEvent = nil
				e.recordEvent(resolvedEvent)

				// Dispatch resolution notification
				e.dispatchAsync(resolvedEvent)
			}
		}
	}
}

// getMetricsForRule returns the VHostMetrics to evaluate based on target vhost.
func (e *Engine) getMetricsForRule(snap *metrics.Snapshot, targetVHost string) (metrics.VHostMetrics, bool) {
	if targetVHost == "all" || targetVHost == "" {
		agg := metrics.VHostMetrics{}
		count := 0
		for _, vm := range snap.VHosts {
			agg.RPS += vm.RPS
			agg.StatusCodes.S2xx += vm.StatusCodes.S2xx
			agg.StatusCodes.S3xx += vm.StatusCodes.S3xx
			agg.StatusCodes.S4xx += vm.StatusCodes.S4xx
			agg.StatusCodes.S5xx += vm.StatusCodes.S5xx
			agg.Latency.Avg += vm.Latency.Avg
			agg.Latency.P95 += vm.Latency.P95
			agg.Latency.P99 += vm.Latency.P99
			count++
		}
		if count > 0 {
			agg.Latency.Avg /= float64(count)
			agg.Latency.P95 /= float64(count)
			agg.Latency.P99 /= float64(count)
		}
		total := agg.StatusCodes.Total()
		if total > 0 {
			agg.ErrorRate = float64(agg.StatusCodes.S4xx+agg.StatusCodes.S5xx) / float64(total) * 100
		}
		return agg, true
	}

	vm, ok := snap.VHosts[targetVHost]
	return vm, ok
}

// extractMetric returns the metric value, total requests, and total errors.
func (e *Engine) extractMetric(metricName string, vm metrics.VHostMetrics) (float64, int64, int64) {
	totalReqs := vm.StatusCodes.Total()
	totalErrs := vm.StatusCodes.S4xx + vm.StatusCodes.S5xx

	switch metricName {
	case "error_rate":
		return vm.ErrorRate, totalReqs, totalErrs
	case "latency_p95":
		return vm.Latency.P95, totalReqs, totalErrs
	case "latency_avg":
		return vm.Latency.Avg, totalReqs, totalErrs
	case "zero_traffic":
		return vm.RPS, totalReqs, totalErrs
	default:
		return 0, totalReqs, totalErrs
	}
}

// checkThreshold checks if the metric value breaches the threshold, applying min_requests and min_errors filters.
func (e *Engine) checkThreshold(rule *AlertRule, val float64, totalReqs, totalErrs int64) bool {
	// Low-traffic filter for error rate
	if rule.Metric == "error_rate" {
		if rule.MinRequests > 0 && totalReqs < rule.MinRequests {
			return false // Not enough requests to be statistically meaningful
		}
		if rule.MinErrors > 0 && totalErrs < rule.MinErrors {
			return false // Minimum error count not met
		}
		return val > rule.Threshold
	}

	if rule.Metric == "zero_traffic" {
		return val <= rule.Threshold
	}

	// Latency thresholds
	if rule.MinRequests > 0 && totalReqs < rule.MinRequests {
		return false
	}
	return val > rule.Threshold
}

func (e *Engine) recordEvent(event AlertEvent) {
	e.recentEvents = append(e.recentEvents, event)
	if len(e.recentEvents) > e.maxEvents {
		e.recentEvents = e.recentEvents[len(e.recentEvents)-e.maxEvents:]
	}
}

func (e *Engine) dispatchAsync(event AlertEvent) {
	dispatchers := make([]Dispatcher, len(e.dispatchers))
	copy(dispatchers, e.dispatchers)
	dashboardURL := e.dashboardURL

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		for _, d := range dispatchers {
			if err := d.Send(ctx, event, dashboardURL); err != nil {
				slog.Error("failed to dispatch alert", "channel", d.Name(), "rule", event.RuleName, "error", err)
			}
		}
	}()
}

// ActiveAlerts returns all currently firing alerts.
func (e *Engine) ActiveAlerts() []AlertEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()

	res := make([]AlertEvent, 0, len(e.activeAlerts))
	for _, a := range e.activeAlerts {
		res = append(res, *a)
	}
	return res
}

// RecentEvents returns the recent alert history up to limit.
func (e *Engine) RecentEvents(limit int) []AlertEvent {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if limit <= 0 || limit > len(e.recentEvents) {
		limit = len(e.recentEvents)
	}

	res := make([]AlertEvent, limit)
	start := len(e.recentEvents) - limit
	for i := 0; i < limit; i++ {
		res[i] = e.recentEvents[start+limit-1-i] // newest first
	}
	return res
}

// SendTestNotification sends a test firing & resolved event through all configured channels.
func (e *Engine) SendTestNotification(ctx context.Context) error {
	e.mu.RLock()
	dispatchers := make([]Dispatcher, len(e.dispatchers))
	copy(dispatchers, e.dispatchers)
	dashboardURL := e.dashboardURL
	e.mu.RUnlock()

	if len(dispatchers) == 0 {
		return fmt.Errorf("no notification channels configured")
	}

	testEvent := AlertEvent{
		ID:        fmt.Sprintf("test-%d", time.Now().Unix()),
		RuleName:  "Test Notification",
		VHost:     "test.example.com",
		Metric:    "error_rate",
		Threshold: 5.0,
		Value:     12.5,
		State:     StateFiring,
		StartedAt: time.Now(),
		Message:   "This is a test alert from NginXplorer alerting engine.",
		Details:   "Testing channel connectivity and payload formatting.",
	}

	var firstErr error
	for _, d := range dispatchers {
		if err := d.Send(ctx, testEvent, dashboardURL); err != nil {
			slog.Error("test notification failed", "channel", d.Name(), "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func stringsToLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}
