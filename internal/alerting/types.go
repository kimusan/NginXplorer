package alerting

import (
	"context"
	"time"
)

// AlertState represents the status of an alert.
type AlertState string

const (
	StateOK       AlertState = "ok"
	StateFiring   AlertState = "firing"
	StateResolved AlertState = "resolved"
)

// AlertEvent represents a single firing or resolved alert incident.
type AlertEvent struct {
	ID         string     `json:"id"`
	RuleName   string     `json:"rule_name"`
	VHost      string     `json:"vhost"`
	Metric     string     `json:"metric"`
	Threshold  float64    `json:"threshold"`
	Value      float64    `json:"value"`
	State      AlertState `json:"state"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	Message    string     `json:"message"`
	Details    string     `json:"details"`
}

// AlertRule represents a compiled rule ready for evaluation.
type AlertRule struct {
	Name        string
	VHost       string        // "all" or specific vhost
	Metric      string        // "error_rate", "latency_p95", "latency_avg", "zero_traffic"
	Threshold   float64
	Duration    time.Duration
	MinRequests int64
	MinErrors   int64

	// Internal state tracking
	lastFiring  time.Time
	activeEvent *AlertEvent
	breachSince time.Time
}

// Dispatcher defines the interface for sending alert notifications to a channel.
type Dispatcher interface {
	Name() string
	Send(ctx context.Context, event AlertEvent, dashboardURL string) error
}
