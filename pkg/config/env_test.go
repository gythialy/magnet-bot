package config

import (
	"testing"

	"github.com/gythialy/magnet/pkg/constant"
)

// TestScheduleInterval guards the fallback: an invalid interval must not reach
// the scheduler, which cannot handle a zero one.
func TestScheduleInterval(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
	}{
		{"unset", "", defaultScheduleInterval},
		{"valid", "2", 2},
		{"zero", "0", defaultScheduleInterval},
		{"negative", "-3", defaultScheduleInterval},
		{"not a number", "soon", defaultScheduleInterval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.ScheduleInterval, tt.value)
			if got := ScheduleInterval(); got != tt.want {
				t.Fatalf("ScheduleInterval() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestTelegramWebhookEnabled guards the default: a missing or misspelled value
// must leave the feature on instead of silently falling back to polling.
func TestTelegramWebhookEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset", "", true},
		{"one", "1", true},
		{"true", "true", true},
		{"upper case", "TRUE", true},
		{"on", "on", true},
		{"padded on", " on ", true},
		{"zero", "0", false},
		{"false", "false", false},
		{"off", "off", false},
		{"invalid", "maybe", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.TelegramWebhookEnabled, tt.value)
			if got := TelegramWebhookEnabled(); got != tt.want {
				t.Fatalf("TelegramWebhookEnabled() = %t, want %t", got, tt.want)
			}
		})
	}
}

// TestMiniAppEnabled mirrors the webhook default: only an explicit false may
// turn the Mini App off.
func TestMiniAppEnabled(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset", "", true},
		{"one", "1", true},
		{"true", "true", true},
		{"on", "on", true},
		{"zero", "0", false},
		{"false", "false", false},
		{"upper case off", "OFF", false},
		{"invalid", "disabled", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.MiniAppEnabled, tt.value)
			if got := MiniAppEnabled(); got != tt.want {
				t.Fatalf("MiniAppEnabled() = %t, want %t", got, tt.want)
			}
		})
	}
}
