package views

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0μs"},
		{"sub-millisecond", 500 * time.Microsecond, "500μs"},
		{"just under a millisecond", 999 * time.Microsecond, "999μs"},
		{"exactly one millisecond", time.Millisecond, "1ms"},
		{"just under a second", 999 * time.Millisecond, "999ms"},
		{"exactly one second", time.Second, "1.00s"},
		{"multiple seconds", 2500 * time.Millisecond, "2.50s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.d); got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
			}
		})
	}
}
