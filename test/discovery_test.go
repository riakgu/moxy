package test

import (
	"testing"

	"github.com/riakgu/moxy/internal/gateway/netns"
)

func TestParseIPFromOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected string
		wantErr  bool
	}{
		{"valid IPv4", "140.213.106.32", "140.213.106.32", false},
		{"IPv4 with newline", "140.213.106.32\n", "140.213.106.32", false},
		{"empty output", "", "", true},
		{"whitespace only", "  \n  ", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, err := netns.ParseIPFromOutput(tt.output)
			if tt.wantErr && err == nil {
				t.Error("expected error but got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if ip != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, ip)
			}
		})
	}
}
