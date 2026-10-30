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
