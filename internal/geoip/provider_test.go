package geoip

import (
	"testing"
)

func TestCountryFlag(t *testing.T) {
	tests := []struct {
		code     string
		expected string
	}{
		{"DK", "🇩🇰"},
		{"dk", "🇩🇰"},
		{"US", "🇺🇸"},
		{"DE", "🇩🇪"},
		{"IE", "🇮🇪"},
		{"", "🌐"},
		{"XYZ", "🌐"},
		{"12", "🌐"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := CountryFlag(tt.code)
			if got != tt.expected {
				t.Errorf("CountryFlag(%q) = %q, want %q", tt.code, got, tt.expected)
			}
		})
	}
}

func TestProvider_NilDbSafe(t *testing.T) {
	p := NewProvider("/nonexistent/path/country.mmdb")
	defer p.Close()

	code, name, flag := p.Lookup("8.8.8.8")
	if code != "" || name != "" || flag != "" {
		t.Errorf("expected empty lookup from missing db, got %q, %q, %q", code, name, flag)
	}

	code, name, flag = p.Lookup("127.0.0.1")
	if code != "" || name != "" || flag != "" {
		t.Errorf("expected empty lookup for loopback, got %q, %q, %q", code, name, flag)
	}
}
