package policy_test

// autoresume_test.go — pure backoff-math tests for policy.BackoffDuration.
// These exercise the exponential-backoff arithmetic in isolation (value-in /
// value-out); the timer/goroutine/Diagnose/Resume and functional flap-window
// coverage stays in package daemon (handlerpause_autoresume_0otqs_test.go).
//
// Spec ref: specs/handler-pause.md §1.2.

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/policy"
)

func TestBackoffDuration(t *testing.T) {
	t.Parallel()

	const base = 20 * time.Millisecond

	cases := []struct {
		name   string
		params policy.AutoResumeParams
		want   time.Duration
	}{
		{
			name:   "no attempts returns base uncapped",
			params: policy.AutoResumeParams{Base: base, Attempts: 0, MaxBackoff: 5 * time.Millisecond},
			want:   base, // Attempts<=0 short-circuits before the cap
		},
		{
			name:   "negative attempts returns base",
			params: policy.AutoResumeParams{Base: base, Attempts: -3, MaxBackoff: time.Hour},
			want:   base,
		},
		{
			name:   "one attempt doubles",
			params: policy.AutoResumeParams{Base: base, Attempts: 1, MaxBackoff: time.Hour},
			want:   40 * time.Millisecond,
		},
		{
			name:   "three attempts is base*2^3",
			params: policy.AutoResumeParams{Base: base, Attempts: 3, MaxBackoff: time.Hour},
			want:   160 * time.Millisecond,
		},
		{
			name:   "capped at MaxBackoff",
			params: policy.AutoResumeParams{Base: base, Attempts: 10, MaxBackoff: 50 * time.Millisecond},
			want:   50 * time.Millisecond,
		},
		{
			name:   "zero MaxBackoff applies default cap",
			params: policy.AutoResumeParams{Base: 20 * time.Minute, Attempts: 8, MaxBackoff: 0},
			want:   policy.DefaultAutoResumeMaxBackoff, // 30m
		},
		{
			name:   "negative MaxBackoff applies default cap",
			params: policy.AutoResumeParams{Base: 20 * time.Minute, Attempts: 8, MaxBackoff: -1},
			want:   policy.DefaultAutoResumeMaxBackoff,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := policy.BackoffDuration(tc.params)
			if got != tc.want {
				t.Errorf("BackoffDuration(%+v)=%s, want %s", tc.params, got, tc.want)
			}
		})
	}
}

// TestBackoffDuration_DefaultCapConstant pins the default at 30m.
func TestBackoffDuration_DefaultCapConstant(t *testing.T) {
	t.Parallel()
	if policy.DefaultAutoResumeMaxBackoff != 30*time.Minute {
		t.Errorf("DefaultAutoResumeMaxBackoff=%s, want 30m", policy.DefaultAutoResumeMaxBackoff)
	}
}
