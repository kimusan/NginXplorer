package collector

import (
	"testing"
)

func TestParseStubStatus(t *testing.T) {
	body := `Active connections: 291 
server accepts handled requests
 16630948 16630948 31070465 
Reading: 6 Writing: 179 Waiting: 106 
`
	status, err := parseStubStatus(body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if status.ActiveConnections != 291 {
		t.Errorf("ActiveConnections = %d, want 291", status.ActiveConnections)
	}
	if status.Accepts != 16630948 {
		t.Errorf("Accepts = %d, want 16630948", status.Accepts)
	}
	if status.Handled != 16630948 {
		t.Errorf("Handled = %d, want 16630948", status.Handled)
	}
	if status.Requests != 31070465 {
		t.Errorf("Requests = %d, want 31070465", status.Requests)
	}
	if status.Reading != 6 {
		t.Errorf("Reading = %d, want 6", status.Reading)
	}
	if status.Writing != 179 {
		t.Errorf("Writing = %d, want 179", status.Writing)
	}
	if status.Waiting != 106 {
		t.Errorf("Waiting = %d, want 106", status.Waiting)
	}
}

func TestParseStubStatus_Invalid(t *testing.T) {
	_, err := parseStubStatus("invalid data")
	if err == nil {
		t.Error("expected error for invalid input")
	}
}

func TestParseStubStatus_Empty(t *testing.T) {
	_, err := parseStubStatus("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}
