package metrics

import (
	"testing"
)

func TestClassifyClient(t *testing.T) {
	tests := []struct {
		name     string
		ua       string
		expected ClientType
	}{
		{
			name:     "Desktop Chrome",
			ua:       "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			expected: ClientHuman,
		},
		{
			name:     "Mobile Safari iPhone",
			ua:       "Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1",
			expected: ClientHuman,
		},
		{
			name:     "Googlebot",
			ua:       "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			expected: ClientGoodBot,
		},
		{
			name:     "Bingbot",
			ua:       "Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)",
			expected: ClientGoodBot,
		},
		{
			name:     "UptimeRobot",
			ua:       "Mozilla/5.0+(compatible; UptimeRobot/2.0; http://www.uptimerobot.com/)",
			expected: ClientGoodBot,
		},
		{
			name:     "Sqlmap scanner",
			ua:       "sqlmap/1.6#stable (https://sqlmap.org)",
			expected: ClientBadBot,
		},
		{
			name:     "Nikto web scanner",
			ua:       "Nikto/2.1.6",
			expected: ClientBadBot,
		},
		{
			name:     "Python requests probe",
			ua:       "python-requests/2.28.1",
			expected: ClientBadBot,
		},
		{
			name:     "Empty UA",
			ua:       "",
			expected: ClientBadBot,
		},
		{
			name:     "Dash UA",
			ua:       "-",
			expected: ClientBadBot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyClient(tt.ua)
			if got != tt.expected {
				t.Errorf("ClassifyClient(%q) = %v, want %v", tt.ua, got, tt.expected)
			}
		})
	}
}
