package format

import (
	"testing"
	"time"
)

func TestDuration_Seconds(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"1 second", 1 * time.Second, "1s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"59 seconds", 59 * time.Second, "59s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Duration(tt.duration)
			if result != tt.expected {
				t.Errorf("Duration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestDuration_Minutes(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"1 minute", 1 * time.Minute, "1m"},
		{"5 minutes", 5 * time.Minute, "5m"},
		{"1 minute 30 seconds", 1*time.Minute + 30*time.Second, "1m 30s"},
		{"10 minutes", 10 * time.Minute, "10m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Duration(tt.duration)
			if result != tt.expected {
				t.Errorf("Duration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestDuration_Hours(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"1 hour", 1 * time.Hour, "1h"},
		{"2 hours", 2 * time.Hour, "2h"},
		{"1 hour 30 minutes", 1*time.Hour + 30*time.Minute, "1h 30m"},
		{"1 hour 1 minute 1 second", 1*time.Hour + 1*time.Minute + 1*time.Second, "1h 1m 1s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Duration(tt.duration)
			if result != tt.expected {
				t.Errorf("Duration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestDuration_Days(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"1 day", 24 * time.Hour, "1d"},
		{"2 days", 48 * time.Hour, "2d"},
		{"1 day 12 hours", 36 * time.Hour, "1d 12h"},
		{"3 days 2 hours 30 minutes", 3*24*time.Hour + 2*time.Hour + 30*time.Minute, "3d 2h 30m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Duration(tt.duration)
			if result != tt.expected {
				t.Errorf("Duration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestDuration_Zero(t *testing.T) {
	result := Duration(0)
	if result != "0s" {
		t.Errorf("Duration(0) = %q, want %q", result, "0s")
	}
}

func TestDuration_NegativeValues(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"negative 1 second", -1 * time.Second, "1s"},
		{"negative 5 minutes", -5 * time.Minute, "5m"},
		{"negative 1 hour 30 minutes", -1*time.Hour - 30*time.Minute, "1h 30m"},
		{"negative 2 days", -48 * time.Hour, "2d"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Duration(tt.duration)
			if result != tt.expected {
				t.Errorf("Duration(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestDuration_SubSecond(t *testing.T) {
	// Durations less than 1 second should show 0s
	result := Duration(500 * time.Millisecond)
	if result != "0s" {
		t.Errorf("Duration(500ms) = %q, want %q", result, "0s")
	}
}

func TestDuration_LargeValues(t *testing.T) {
	// 100 days
	d := 100 * 24 * time.Hour
	result := Duration(d)
	if result != "100d" {
		t.Errorf("Duration(100 days) = %q, want %q", result, "100d")
	}
}
