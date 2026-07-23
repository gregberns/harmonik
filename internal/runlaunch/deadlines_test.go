package runlaunch

// deadlines_test.go — hk-96d7w (LOCAL slice of hk-5z1f0):
// the agent_ready timeout is now a configurable knob with a separate, longer
// default for REMOTE (SSH worker) dispatch — Config.RemoteAgentReadyTimeout /
// --remote-agent-ready-timeout, resolved per-dispatch by
// EffectiveAgentReadyTimeout. In-package (package runlaunch) because it pins
// the compiled-in default constants directly.
//
// Relocated from internal/daemon/agentreadyremote_hk96d7w_test.go by P2 unit
// E5 RT19b, which moved the HC-056 deadline family out of internal/daemon.
//
// This is a pure-function unit test of the resolver only — no daemon spawn,
// no worker, no network. It does not depend on a live remote worker
// (gb-mbp); the remote canary that motivated the longer default is tracked
// separately at hk-5z1f0.

import (
	"testing"
	"time"
)

// TestHK96D7W_DefaultRemoteAgentReadyTimeoutIsLongerThanLocal pins the
// relationship the bead asks for: remote workers get a longer default window
// than local dispatch, because a remote spawn additionally clears reverse-
// SSH-tunnel readiness (and, for the reviewer node, competes with a resident
// implementer agent for CPU/disk on the same worker).
func TestHK96D7W_DefaultRemoteAgentReadyTimeoutIsLongerThanLocal(t *testing.T) {
	t.Parallel()
	if DefaultRemoteAgentReadyTimeout <= DefaultAgentReadyTimeout {
		t.Fatalf("DefaultRemoteAgentReadyTimeout (%v) must be longer than DefaultAgentReadyTimeout (%v) (hk-96d7w)",
			DefaultRemoteAgentReadyTimeout, DefaultAgentReadyTimeout)
	}
	if got, want := DefaultRemoteAgentReadyTimeout, 210*time.Second; got != want {
		t.Fatalf("DefaultRemoteAgentReadyTimeout = %v, want %v (hk-96d7w)", got, want)
	}
}

// TestHK96D7W_EffectiveAgentReadyTimeout exercises EffectiveAgentReadyTimeout
// across the local/remote x configured/unconfigured matrix.
func TestHK96D7W_EffectiveAgentReadyTimeout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		local    time.Duration
		remote   time.Duration
		isRemote bool
		want     time.Duration
	}{
		{
			name:     "local dispatch, no overrides configured falls back to local default",
			local:    0,
			remote:   0,
			isRemote: false,
			want:     DefaultAgentReadyTimeout,
		},
		{
			name:     "remote dispatch, no overrides configured falls back to remote default",
			local:    0,
			remote:   0,
			isRemote: true,
			want:     DefaultRemoteAgentReadyTimeout,
		},
		{
			name:     "local dispatch honors an explicit local override",
			local:    45 * time.Second,
			remote:   0,
			isRemote: false,
			want:     45 * time.Second,
		},
		{
			name:     "remote dispatch honors an explicit remote override",
			local:    0,
			remote:   300 * time.Second,
			isRemote: true,
			want:     300 * time.Second,
		},
		{
			name:     "remote dispatch ignores a local override",
			local:    45 * time.Second,
			remote:   0,
			isRemote: true,
			want:     DefaultRemoteAgentReadyTimeout,
		},
		{
			name:     "local dispatch ignores a remote override",
			local:    0,
			remote:   300 * time.Second,
			isRemote: false,
			want:     DefaultAgentReadyTimeout,
		},
		{
			name:     "negative override treated as unset",
			local:    -1,
			remote:   -1,
			isRemote: false,
			want:     DefaultAgentReadyTimeout,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := EffectiveAgentReadyTimeout(tc.local, tc.remote, tc.isRemote)
			if got != tc.want {
				t.Fatalf("EffectiveAgentReadyTimeout(local=%v, remote=%v, isRemote=%v) = %v, want %v (hk-96d7w)",
					tc.local, tc.remote, tc.isRemote, got, tc.want)
			}
		})
	}
}
