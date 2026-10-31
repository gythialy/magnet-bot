package config

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
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

// TestManagerIds covers the comma-separated administrator list: trimming,
// skipped empty entries, ignored invalid entries, de-duplication and the
// unset/invalid fallbacks.
func TestManagerIds(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []int64
	}{
		{"unset", "", nil},
		{"single id", "10001", []int64{10001}},
		{"several ids", "10001,10002", []int64{10001, 10002}},
		{"padded ids", " 10001 , 10002 ", []int64{10001, 10002}},
		{"empty entries", "10001,,10002,", []int64{10001, 10002}},
		{"invalid entries ignored", "abc,10001", []int64{10001}},
		{"non-positive ignored", "-5,0,10001", []int64{10001}},
		{"duplicates removed", "10001,10001,10002", []int64{10001, 10002}},
		{"all invalid", "abc,def", nil},
		{"blank value", "   ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.ManagerId, tt.value)
			if got := ManagerIds(); !slices.Equal(got, tt.want) {
				t.Fatalf("ManagerIds() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestManagerIdKeepsFirstValue guards the legacy accessor: a single value must
// produce exactly the historic result, and the new list form degrades to its
// first administrator instead of returning 0 (which would leave the bot with no
// usable admin command while call sites are migrated).
func TestManagerIdKeepsFirstValue(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int64
	}{
		{"unset", "", 0},
		{"single id", "10001", 10001},
		{"list", "10001,10002", 10001},
		{"invalid", "abc", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(constant.ManagerId, tt.value)
			if got := ManagerId(); got != tt.want {
				t.Fatalf("ManagerId() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestAllowedUserIDs covers the optional seed list with the same parser rules.
func TestAllowedUserIDs(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []int64
	}{
		{"unset", "", nil},
		{"single id", "20002", []int64{20002}},
		{"several ids", " 20002 , 20003 ", []int64{20002, 20003}},
		{"invalid entries ignored", "abc,20002", []int64{20002}},
		{"duplicates removed", "20002,20002", []int64{20002}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(allowedUserIDsEnv, tt.value)
			// MANAGER_ID must not leak into this list.
			t.Setenv(constant.ManagerId, "10001")
			if got := AllowedUserIDs(); !slices.Equal(got, tt.want) {
				t.Fatalf("AllowedUserIDs() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestManagerIdsWarnsAboutInvalidEntries proves an operator gets a warning
// naming the variable instead of a silently shortened administrator list.
func TestManagerIdsWarnsAboutInvalidEntries(t *testing.T) {
	previous := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	t.Setenv(constant.ManagerId, "abc,10001")

	if got := ManagerIds(); !slices.Equal(got, []int64{10001}) {
		t.Fatalf("ManagerIds() = %v, want the valid id only", got)
	}
	if !strings.Contains(logs.String(), "ignoring invalid user id") ||
		!strings.Contains(logs.String(), constant.ManagerId) {
		t.Fatalf("log output = %q, want a warning naming %s", logs.String(), constant.ManagerId)
	}
}

// TestNewServiceConfigAccessControl pins the fields the access-control bootstrap
// reads, including the legacy ManagerId view.
func TestNewServiceConfigAccessControl(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		t.Setenv(constant.ManagerId, "10001, 10002")
		t.Setenv(allowedUserIDsEnv, "20002")

		cfg := NewServiceConfig()
		if !slices.Equal(cfg.ManagerIDs, []int64{10001, 10002}) {
			t.Fatalf("ManagerIDs = %v, want [10001 10002]", cfg.ManagerIDs)
		}
		if cfg.ManagerId != 10001 {
			t.Fatalf("ManagerId = %d, want the first administrator 10001", cfg.ManagerId)
		}
		if !slices.Equal(cfg.AllowedUserIDs, []int64{20002}) {
			t.Fatalf("AllowedUserIDs = %v, want [20002]", cfg.AllowedUserIDs)
		}
	})

	t.Run("unset", func(t *testing.T) {
		t.Setenv(constant.ManagerId, "")
		t.Setenv(allowedUserIDsEnv, "")

		cfg := NewServiceConfig()
		if len(cfg.ManagerIDs) != 0 {
			t.Fatalf("ManagerIDs = %v, want empty", cfg.ManagerIDs)
		}
		if cfg.ManagerId != 0 {
			t.Fatalf("ManagerId = %d, want 0", cfg.ManagerId)
		}
		if len(cfg.AllowedUserIDs) != 0 {
			t.Fatalf("AllowedUserIDs = %v, want empty", cfg.AllowedUserIDs)
		}
	})
}
