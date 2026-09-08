package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/kimusan/nginxplorer/internal/metrics"
)

// Braille dot patterns for sparkline rendering.
// Each character cell is a 2×4 dot matrix (8 dots).
// We use vertical bars for simplicity and visual clarity.
var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

// Model is the Bubble Tea model for the NginXplorer TUI.
type Model struct {
	client      *SSEClient
	width       int
	height      int
	ready       bool
	quitting    bool
	connected   bool
	lastError   string
	connectAddr string

	// Current state
	snapshot    *metrics.Snapshot
	vhosts      []string
	activeVHost int // index into vhosts; 0 = "all"
	timeRange   string // "live", "1h", "24h", "7d"
	historyData *metrics.VHostHistory

	// History ring for sparklines (last 60 data points)
	rpsHistory     []float64
	latHistory     []float64
	errHistory     []float64

	// Alerts tracking
	activeAlerts   int
	lastAlertCheck time.Time

	// Time tracking
	lastUpdate time.Time
}

// Messages
type snapshotMsg struct{ snap *metrics.Snapshot }
type vhostMsg struct{ vhosts []string }
type historyMsg struct{ hist *metrics.VHostHistory }
type alertsMsg struct{ count int }
type errMsg struct{ err error }
type tickMsg struct{}

// NewModel creates a new TUI model connected to the daemon.
func NewModel(addr, token, username, password string) Model {
	client := NewSSEClient(addr, token, username, password)

	return Model{
		client:      client,
		connectAddr: addr,
		activeVHost: 0, // "all"
		timeRange:   "live",
		rpsHistory:  make([]float64, 0, 60),
		latHistory:  make([]float64, 0, 60),
		errHistory:  make([]float64, 0, 60),
	}
}

// Init starts the SSE client and returns the initial commands.
func (m Model) Init() tea.Cmd {
	m.client.Connect()
	return tea.Batch(
		waitForSnapshot(m.client),
		waitForVHosts(m.client),
		waitForError(m.client),
		fetchAlertsCmd(m.client),
	)
}

// Update handles messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			m.client.Close()
			return m, tea.Quit

		case "r":
			// Cycle time range: live -> 1h -> 24h -> 7d -> live
			switch m.timeRange {
			case "live":
				m.timeRange = "1h"
			case "1h":
				m.timeRange = "24h"
			case "24h":
				m.timeRange = "7d"
			default:
				m.timeRange = "live"
			}

			if m.timeRange == "live" {
				m.historyData = nil
				m.rpsHistory = m.rpsHistory[:0]
				m.latHistory = m.latHistory[:0]
				m.errHistory = m.errHistory[:0]
				return m, nil
			}

			vhost := "all"
			if m.activeVHost > 0 && m.activeVHost-1 < len(m.vhosts) {
				vhost = m.vhosts[m.activeVHost-1]
			}
			return m, fetchHistoryCmd(m.client, vhost, m.timeRange)

		case "tab":
			// Cycle to next vhost
			if len(m.vhosts) > 0 {
				m.activeVHost = (m.activeVHost + 1) % (len(m.vhosts) + 1) // +1 for "all"
			}
			// Clear history on vhost switch
			m.rpsHistory = m.rpsHistory[:0]
			m.latHistory = m.latHistory[:0]
			m.errHistory = m.errHistory[:0]
			if m.timeRange != "live" {
				vhost := "all"
				if m.activeVHost > 0 && m.activeVHost-1 < len(m.vhosts) {
					vhost = m.vhosts[m.activeVHost-1]
				}
				return m, fetchHistoryCmd(m.client, vhost, m.timeRange)
			}
			return m, nil

		case "shift+tab":
			// Cycle to previous vhost
			total := len(m.vhosts) + 1
			if total > 0 {
				m.activeVHost = (m.activeVHost - 1 + total) % total
			}
			m.rpsHistory = m.rpsHistory[:0]
			m.latHistory = m.latHistory[:0]
			m.errHistory = m.errHistory[:0]
			if m.timeRange != "live" {
				vhost := "all"
				if m.activeVHost > 0 && m.activeVHost-1 < len(m.vhosts) {
					vhost = m.vhosts[m.activeVHost-1]
				}
				return m, fetchHistoryCmd(m.client, vhost, m.timeRange)
			}
			return m, nil
		}

	case historyMsg:
		m.historyData = msg.hist
		if msg.hist != nil {
			m.loadHistorySparklines(msg.hist)
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil

	case snapshotMsg:
		m.snapshot = msg.snap
		m.connected = true
		m.lastUpdate = time.Now()
		m.lastError = ""
		if msg.snap != nil && len(msg.snap.VHosts) > 0 {
			m.syncVHosts(msg.snap.VHosts)
		}
		m.updateHistory()

		var cmd tea.Cmd = waitForSnapshot(m.client)
		if time.Since(m.lastAlertCheck) >= 10*time.Second {
			m.lastAlertCheck = time.Now()
			cmd = tea.Batch(cmd, fetchAlertsCmd(m.client))
		}
		return m, cmd

	case alertsMsg:
		m.activeAlerts = msg.count
		return m, nil

	case vhostMsg:
		m.vhosts = msg.vhosts
		return m, waitForVHosts(m.client)

	case errMsg:
		m.lastError = msg.err.Error()
		m.connected = false
		return m, waitForError(m.client)
	}

	return m, nil
}

// updateHistory appends the current metrics to the sparkline history buffers.
func (m *Model) updateHistory() {
	if m.timeRange != "live" {
		return
	}
	if m.snapshot == nil {
		return
	}

	vm := m.getActiveMetrics()

	m.rpsHistory = appendCapped(m.rpsHistory, vm.RPS, 60)
	m.latHistory = appendCapped(m.latHistory, vm.Latency.P95, 60)
	m.errHistory = appendCapped(m.errHistory, vm.ErrorRate, 60)
}

// syncVHosts updates the known vhost list from the incoming snapshot map
func (m *Model) syncVHosts(vhostMap map[string]metrics.VHostMetrics) {
	existing := make(map[string]bool)
	for _, v := range m.vhosts {
		existing[v] = true
	}
	changed := false
	for name := range vhostMap {
		if !existing[name] {
			m.vhosts = append(m.vhosts, name)
			existing[name] = true
			changed = true
		}
	}
	if changed {
		sort.Strings(m.vhosts)
	}
}

func appendCapped(s []float64, v float64, max int) []float64 {
	s = append(s, v)
	if len(s) > max {
		s = s[len(s)-max:]
	}
	return s
}

// getActiveMetrics returns the metrics for the currently selected vhost.
func (m *Model) getActiveMetrics() metrics.VHostMetrics {
	if m.snapshot == nil {
		return metrics.VHostMetrics{}
	}

	if m.activeVHost == 0 {
		// Aggregate all vhosts
		return m.aggregateAll()
	}

	idx := m.activeVHost - 1
	if idx >= 0 && idx < len(m.vhosts) {
		name := m.vhosts[idx]
		if vm, ok := m.snapshot.VHosts[name]; ok {
			return vm
		}
	}

	return metrics.VHostMetrics{}
}

// aggregateAll sums metrics across all vhosts.
func (m *Model) aggregateAll() metrics.VHostMetrics {
	agg := metrics.VHostMetrics{}
	count := 0

	for _, vm := range m.snapshot.VHosts {
		agg.RPS += vm.RPS
		agg.StatusCodes.S2xx += vm.StatusCodes.S2xx
		agg.StatusCodes.S3xx += vm.StatusCodes.S3xx
		agg.StatusCodes.S4xx += vm.StatusCodes.S4xx
		agg.StatusCodes.S5xx += vm.StatusCodes.S5xx
		agg.Bandwidth.In += vm.Bandwidth.In
		agg.Bandwidth.Out += vm.Bandwidth.Out
		agg.UniqueVisitors += vm.UniqueVisitors
		agg.Latency.P50 += vm.Latency.P50
		agg.Latency.P95 += vm.Latency.P95
		agg.Latency.P99 += vm.Latency.P99
		agg.Latency.Avg += vm.Latency.Avg
		count++

		// Merge top paths
		agg.TopPaths = append(agg.TopPaths, vm.TopPaths...)
	}

	// Sort merged top paths across all vhosts by RPS descending
	sort.Slice(agg.TopPaths, func(i, j int) bool {
		return agg.TopPaths[i].RPS > agg.TopPaths[j].RPS
	})
	if len(agg.TopPaths) > 10 {
		agg.TopPaths = agg.TopPaths[:10]
	}

	if count > 0 {
		agg.Latency.P50 /= float64(count)
		agg.Latency.P95 /= float64(count)
		agg.Latency.P99 /= float64(count)
		agg.Latency.Avg /= float64(count)
	}

	total := agg.StatusCodes.Total()
	if total > 0 {
		agg.ErrorRate = float64(agg.StatusCodes.S4xx+agg.StatusCodes.S5xx) / float64(total) * 100
	}

	return agg
}

// View renders the complete TUI.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	if !m.ready {
		return "\n  Initializing NginXplorer TUI..."
	}

	var b strings.Builder

	// Header
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	if !m.connected && m.snapshot == nil {
		b.WriteString(m.renderConnecting())
		b.WriteString("\n")
		b.WriteString(m.renderHelp())
		return b.String()
	}

	// Stat cards row
	b.WriteString(m.renderStatCards())
	b.WriteString("\n")

	// Sparklines
	b.WriteString(m.renderSparklines())
	b.WriteString("\n")

	// Status codes + VHost traffic side by side
	b.WriteString(m.renderMiddleRow())
	b.WriteString("\n")

	// Top paths table
	b.WriteString(m.renderTopPaths())
	b.WriteString("\n")

	// Help bar
	b.WriteString(m.renderHelp())

	return b.String()
}

// renderHeader renders the title bar.
func (m Model) renderHeader() string {
	vhostName := "All VHosts"
	if m.activeVHost > 0 && m.activeVHost-1 < len(m.vhosts) {
		vhostName = m.vhosts[m.activeVHost-1]
	}

	status := lipgloss.NewStyle().Foreground(colorSuccess).Render("● Live")
	if m.timeRange != "live" {
		status = lipgloss.NewStyle().Foreground(colorWarning).Render("📅 " + m.timeRange + " (Hist)")
	}
	if !m.connected {
		status = lipgloss.NewStyle().Foreground(colorDanger).Render("● Disconnected")
	}

	timeStr := ""
	if !m.lastUpdate.IsZero() {
		timeStr = m.lastUpdate.Format("15:04:05")
	}

	title := titleStyle.Render("📡 NginXplorer")
	vhost := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(vhostName)
	sep := lipgloss.NewStyle().Foreground(colorMuted).Render(" │ ")

	left := title + sep + vhost + sep + status
	if m.activeAlerts > 0 {
		alertBadge := lipgloss.NewStyle().Foreground(colorDanger).Bold(true).Render(fmt.Sprintf("🚨 %d Alert", m.activeAlerts))
		left += sep + alertBadge
	}
	right := lipgloss.NewStyle().Foreground(colorTextDim).Render(timeStr)

	// Calculate padding
	leftLen := lipgloss.Width(left)
	rightLen := lipgloss.Width(right)
	pad := m.width - leftLen - rightLen
	if pad < 1 {
		pad = 1
	}

	return headerStyle.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
}

// renderConnecting shows a connection status message.
func (m Model) renderConnecting() string {
	msg := fmt.Sprintf("\n  Connecting to %s...", m.connectAddr)
	if m.lastError != "" {
		msg += "\n\n  " + lipgloss.NewStyle().Foreground(colorDanger).Render("Error: "+m.lastError)
		msg += "\n  Retrying..."
	}
	return msg
}

// renderStatCards renders the 5 stat summary cards.
func (m Model) renderStatCards() string {
	vm := m.getActiveMetrics()

	rpsLabel := "Requests/s"
	rps := fmt.Sprintf("%.1f", vm.RPS)
	errRate := fmt.Sprintf("%.2f%%", vm.ErrorRate)
	errRateVal := vm.ErrorRate
	latLabel := "Latency p95"
	latency := fmt.Sprintf("%.1fms", vm.Latency.P95)
	connLabel := "Connections"
	conns := "0"
	connDetail := ""
	if m.snapshot != nil {
		g := m.snapshot.Global
		conns = fmt.Sprintf("%d", g.ActiveConnections)
		connDetail = fmt.Sprintf("R:%d W:%d I:%d", g.Reading, g.Writing, g.Waiting)
	}
	visitorLabel := "Visitors"
	visitors := fmt.Sprintf("%d", vm.UniqueVisitors)
	visitorDetail := "5min"

	if m.timeRange != "live" && m.historyData != nil && m.historyData.Summary != nil {
		s := m.historyData.Summary
		rpsLabel = "Avg Req/s"
		rps = fmt.Sprintf("%.1f", s.AvgRPS)
		errRate = fmt.Sprintf("%.2f%%", s.ErrorRate)
		errRateVal = s.ErrorRate
		latLabel = "Avg Latency"
		latency = fmt.Sprintf("%.1fms", s.AvgLatency)
		connLabel = "Total Requests"
		conns = fmt.Sprintf("%d", s.TotalRequests)
		connDetail = m.timeRange
		visitorLabel = "Visitors"
		visitors = fmt.Sprintf("%d", s.UniqueVisitors)
		visitorDetail = m.timeRange
	}

	// Color the error rate based on value
	errStyle := statGoodStyle
	if errRateVal > 1 {
		errStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	}
	if errRateVal > 5 {
		errStyle = statBadStyle
	}

	// Build each card
	cardWidth := (m.width - 6) / 5
	if cardWidth < 14 {
		cardWidth = 14
	}

	card := func(label, value string, style lipgloss.Style, detail string) string {
		v := style.Render(value)
		l := statLabelStyle.Render(label)
		d := ""
		if detail != "" {
			d = "\n" + lipgloss.NewStyle().Foreground(colorTextDim).Render(detail)
		}
		return lipgloss.NewStyle().Width(cardWidth).Render(l + "\n" + v + d)
	}

	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		card(rpsLabel, rps, statValueStyle, ""),
		card("Error Rate", errRate, errStyle, ""),
		card(latLabel, latency, statValueStyle, ""),
		card(connLabel, conns, statValueStyle, connDetail),
		card(visitorLabel, visitors, statValueStyle, visitorDetail),
	)

	return "\n" + cards
}

// renderSparklines renders Braille sparkline charts for RPS, latency, and error rate.
func (m Model) renderSparklines() string {
	sparkWidth := m.width - 16 // leave room for labels
	if sparkWidth < 20 {
		sparkWidth = 20
	}
	if sparkWidth > 60 {
		sparkWidth = 60
	}

	lines := []string{
		m.renderSingleSparkline("RPS", m.rpsHistory, sparkWidth, colorPrimary),
		m.renderSingleSparkline("Lat", m.latHistory, sparkWidth, colorWarning),
		m.renderSingleSparkline("Err", m.errHistory, sparkWidth, colorDanger),
	}

	return "\n" + strings.Join(lines, "\n")
}

// renderSingleSparkline renders one sparkline row.
func (m Model) renderSingleSparkline(label string, data []float64, width int, color lipgloss.Color) string {
	labelStr := sparkLabelStyle.Render(label + " ")

	if len(data) == 0 {
		return labelStr + lipgloss.NewStyle().Foreground(colorMuted).Render("▏" + strings.Repeat("─", width) + "▕")
	}

	// Find min/max for scaling
	minVal, maxVal := data[0], data[0]
	for _, v := range data {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	// Build sparkline
	var spark strings.Builder
	spark.WriteRune('▏')

	dataLen := len(data)
	for i := 0; i < width; i++ {
		// Map position to data index
		dataIdx := i * dataLen / width
		if dataIdx >= dataLen {
			dataIdx = dataLen - 1
		}

		val := data[dataIdx]

		// Scale to 0-8 (number of block levels)
		var level int
		if maxVal > minVal {
			level = int(math.Round((val - minVal) / (maxVal - minVal) * 8))
		} else if maxVal > 0 {
			level = 4 // flat line in middle
		}
		if level > 8 {
			level = 8
		}
		if level < 0 {
			level = 0
		}

		spark.WriteRune(sparkBlocks[level])
	}

	spark.WriteRune('▕')

	// Current value
	current := ""
	if len(data) > 0 {
		v := data[len(data)-1]
		if v >= 1000 {
			current = fmt.Sprintf(" %.0f", v)
		} else if v >= 10 {
			current = fmt.Sprintf(" %.1f", v)
		} else {
			current = fmt.Sprintf(" %.2f", v)
		}
	}

	sparkStr := lipgloss.NewStyle().Foreground(color).Render(spark.String())
	valStr := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(current)

	return labelStr + sparkStr + valStr
}

// renderMiddleRow renders status codes and vhost traffic side by side.
func (m Model) renderMiddleRow() string {
	vm := m.getActiveMetrics()

	// Status codes section
	statusCodes := vm.StatusCodes
	statusTitle := "  Status Codes"
	if m.timeRange != "live" && m.historyData != nil && m.historyData.Summary != nil {
		statusCodes = m.historyData.Summary.StatusCodes
		statusTitle = fmt.Sprintf("  Status Codes (%s)", m.timeRange)
	}
	total := statusCodes.Total()
	statusSection := sectionStyle.Render(statusTitle) + "\n"

	codes := []struct {
		label string
		count int64
		style lipgloss.Style
	}{
		{"2xx", statusCodes.S2xx, barFullStyle},
		{"3xx", statusCodes.S3xx, bar3xxStyle},
		{"4xx", statusCodes.S4xx, bar4xxStyle},
		{"5xx", statusCodes.S5xx, bar5xxStyle},
	}

	barMaxWidth := 20
	for _, c := range codes {
		pct := 0.0
		if total > 0 {
			pct = float64(c.count) / float64(total) * 100
		}

		barLen := 0
		if total > 0 {
			barLen = int(float64(c.count) / float64(total) * float64(barMaxWidth))
		}
		if barLen < 0 {
			barLen = 0
		}

		bar := c.style.Render(strings.Repeat("█", barLen)) +
			lipgloss.NewStyle().Foreground(colorMuted).Render(strings.Repeat("░", barMaxWidth-barLen))

		pctStr := fmt.Sprintf("%5.1f%%", pct)
		statusSection += fmt.Sprintf("  %s %s %s\n", c.style.Render(c.label), bar, pctStr)
	}

	// VHost traffic section (only when showing "all")
	vhostSection := ""
	if m.activeVHost == 0 && m.snapshot != nil && len(m.snapshot.VHosts) > 0 {
		vhostSection = sectionStyle.Render("  VHost Traffic") + "\n"

		// Find max RPS for scaling
		maxRPS := 0.0
		for _, vm := range m.snapshot.VHosts {
			if vm.RPS > maxRPS {
				maxRPS = vm.RPS
			}
		}

		for _, name := range m.vhosts {
			if vh, ok := m.snapshot.VHosts[name]; ok {
				barLen := 0
				if maxRPS > 0 {
					barLen = int(vh.RPS / maxRPS * float64(barMaxWidth))
				}
				if barLen < 1 && vh.RPS > 0 {
					barLen = 1
				}

				nameStr := lipgloss.NewStyle().Foreground(colorText).Width(20).Render(truncate(name, 19))
				rpsStr := fmt.Sprintf("%6.0f rps", vh.RPS)
				bar := lipgloss.NewStyle().Foreground(colorPrimary).Render(strings.Repeat("█", barLen)) +
					strings.Repeat(" ", barMaxWidth-barLen)

				vhostSection += fmt.Sprintf("  %s %s %s\n", nameStr, bar, rpsStr)
			}
		}
	}

	// Country traffic section (when countries data is available)
	countrySection := ""
	if len(vm.TopCountries) > 0 {
		countrySection = sectionStyle.Render("  Top Countries") + "\n"
		for i := 0; i < len(vm.TopCountries) && i < 5; i++ {
			c := vm.TopCountries[i]
			flag := c.Flag
			if flag == "" {
				flag = "🌐"
			}
			cName := c.CountryName
			if cName == "" {
				cName = c.CountryCode
			}
			label := fmt.Sprintf("%s %-12s", flag, truncate(cName, 12))
			pctStr := fmt.Sprintf("%5.1f%%", c.Percentage)
			rpsStr := fmt.Sprintf("%5.1frps", c.RPS)
			countrySection += fmt.Sprintf("  %s %s %s\n",
				lipgloss.NewStyle().Foreground(colorText).Render(label),
				lipgloss.NewStyle().Foreground(colorPrimary).Render(rpsStr),
				lipgloss.NewStyle().Foreground(colorMuted).Render(pctStr),
			)
		}
	}

	halfWidth := m.width / 2
	rightContent := vhostSection
	if rightContent == "" {
		rightContent = countrySection
	}

	if rightContent != "" {
		leftBox := lipgloss.NewStyle().Width(halfWidth).Render(statusSection)
		rightBox := lipgloss.NewStyle().Width(halfWidth).Render(rightContent)
		return lipgloss.JoinHorizontal(lipgloss.Top, leftBox, rightBox)
	}

	return statusSection
}

// renderTopPaths renders the top requested paths table.
func (m Model) renderTopPaths() string {
	vm := m.getActiveMetrics()

	header := sectionStyle.Render("  Top Paths")

	isAll := m.activeVHost == 0
	vhostW := 18
	pathW := m.width - 45
	if isAll {
		pathW = m.width - 45 - vhostW - 1
	}
	if pathW < 20 {
		pathW = 20
	}
	if pathW > 50 {
		pathW = 50
	}

	var hdr string
	if isAll {
		hdr = fmt.Sprintf("  %-*s %-*s %8s %10s %8s",
			vhostW, "VHost", pathW, "Path", "Req/s", "Avg Lat", "2xx %")
	} else {
		hdr = fmt.Sprintf("  %-*s %8s %10s %8s",
			pathW, "Path", "Req/s", "Avg Lat", "2xx %")
	}
	headerLine := tableHeaderStyle.Render(hdr)

	if len(vm.TopPaths) == 0 {
		return header + "\n" + headerLine + "\n  " +
			lipgloss.NewStyle().Foreground(colorMuted).Render("No data yet")
	}

	rows := ""
	limit := 8
	if limit > len(vm.TopPaths) {
		limit = len(vm.TopPaths)
	}

	for i := 0; i < limit; i++ {
		p := vm.TopPaths[i]
		var row string
		if isAll {
			vh := p.VHost
			if vh == "" {
				vh = "-"
			}
			row = fmt.Sprintf("  %-*s %-*s %8.1f %8.1fms %7.0f%%",
				vhostW, truncate(vh, vhostW-1),
				pathW, truncate(p.Path, pathW-1),
				p.RPS, p.AvgLatency, p.Status2xx)
		} else {
			row = fmt.Sprintf("  %-*s %8.1f %8.1fms %7.0f%%",
				pathW, truncate(p.Path, pathW-1),
				p.RPS, p.AvgLatency, p.Status2xx)
		}
		rows += tableRowStyle.Render(row) + "\n"
	}

	return header + "\n" + headerLine + "\n" + rows
}

// renderHelp renders the help bar at the bottom.
func (m Model) renderHelp() string {
	rangeDesc := "Range: " + m.timeRange
	keys := []struct{ key, desc string }{
		{"Tab", "Next VHost"},
		{"Shift+Tab", "Prev VHost"},
		{"r", rangeDesc},
		{"q", "Quit"},
	}

	var parts []string
	for _, k := range keys {
		parts = append(parts,
			helpKeyStyle.Render(k.key)+" "+helpDescStyle.Render(k.desc))
	}

	return "\n " + strings.Join(parts, "  ")
}

// Helper functions

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

// loadHistorySparklines populates the sparkline history buffers from historical query data.
func (m *Model) loadHistorySparklines(h *metrics.VHostHistory) {
	if h == nil {
		return
	}
	m.rpsHistory = m.rpsHistory[:0]
	m.latHistory = m.latHistory[:0]
	m.errHistory = m.errHistory[:0]

	for _, p := range h.RPS {
		m.rpsHistory = appendCapped(m.rpsHistory, p.Value, 60)
	}
	for _, p := range h.LatencyP95 {
		m.latHistory = appendCapped(m.latHistory, p.Value, 60)
	}
	for _, p := range h.ErrorRate {
		m.errHistory = appendCapped(m.errHistory, p.Value, 60)
	}
}

func fetchHistoryCmd(client *SSEClient, vhost, timeRange string) tea.Cmd {
	return func() tea.Msg {
		hist, err := client.FetchHistory(vhost, timeRange)
		if err != nil {
			return errMsg{err: err}
		}
		return historyMsg{hist: hist}
	}
}

func fetchAlertsCmd(client *SSEClient) tea.Cmd {
	return func() tea.Msg {
		alerts, err := client.FetchAlerts()
		if err != nil {
			return alertsMsg{count: 0}
		}
		return alertsMsg{count: len(alerts)}
	}
}

// Tea commands to receive messages from SSE channels
func waitForSnapshot(client *SSEClient) tea.Cmd {
	return func() tea.Msg {
		snap, ok := <-client.Snapshots
		if !ok {
			return nil
		}
		return snapshotMsg{snap: snap}
	}
}

func waitForVHosts(client *SSEClient) tea.Cmd {
	return func() tea.Msg {
		vhosts, ok := <-client.VHosts
		if !ok {
			return nil
		}
		return vhostMsg{vhosts: vhosts}
	}
}

func waitForError(client *SSEClient) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-client.Errors
		if !ok {
			return nil
		}
		return errMsg{err: err}
	}
}

// Run starts the TUI application.
func Run(addr, token, username, password string) error {
	model := NewModel(addr, token, username, password)
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
