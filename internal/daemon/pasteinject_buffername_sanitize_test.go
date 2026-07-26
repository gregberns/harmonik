package daemon_test

// pasteinject_buffername_sanitize_test.go — regression guard for the
// unsanitized bufferName helper.
//
// internal/daemon's bufferName was a bare fmt.Sprintf with no sanitization,
// while internal/lifecycle/tmux enforces bufferNameRe
// (^harmonik-[a-z0-9-]+-[a-z0-9-]+$) inside OSAdapter.LoadBuffer and
// OSAdapter.PasteBuffer, BEFORE tmux is ever invoked. Five call sites fed raw
// session ids through it: crewstart.go ("crew-init"), dot_gate.go ("gate") and
// pasteinject.go itself ("task" x2, "review").
//
// It was not firing only because session ids happen to be minted as lowercase
// UUIDs. A "20060102T150405Z"-style id fails immediately on the uppercase 'T'
// and 'Z' — and the failure mode is a DROPPED PAYLOAD and a wedged dispatch,
// not a clean error. That is exactly how hk-lckbv wedged the daemon and how
// hk-9hvr0 wedged the tmux substrate.
//
// The fix routes bufferName through tmux.BufferName, which is valid by
// construction. These tests assert against tmux.ValidBufferName — the REAL
// validator — rather than a restated copy of the regex, which is the same
// reason ValidBufferName is exported at all.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// TestBufferName_HostileSessionIDStillValid feeds ids that FAIL bufferNameRe as
// written and asserts the constructed buffer name is nevertheless accepted by
// the production validator. Against the pre-fix fmt.Sprintf implementation every
// case in this table fails.
func TestBufferName_HostileSessionIDStillValid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		sessionID string
		purpose   string
	}{
		{"rfc3339-style timestamp id (hk-lckbv shape)", "synthetic-claude-session-20260528T150405Z", "task"},
		{"compact timestamp id", "20060102T150405Z", "review"},
		{"uppercase uuid", "01HWXYZ-ABC123", "gate"},
		{"underscores", "run_session_42", "crew-init"},
		{"dotted id", "session.4.2.1", "task"},
		{"slashes and colons", "worker/host:1234", "review"},
		{"unicode", "sessión-ünïcode", "task"},
		{"empty id", "", "task"},
		{"all-punctuation id", "...", "task"},
		{"hostile purpose too", "01hwxyz", "Crew Init!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := daemon.ExportedBufferName(tc.sessionID, tc.purpose)
			if !tmux.ValidBufferName(got) {
				t.Errorf("bufferName(%q, %q) = %q; REJECTED by the production validator "+
					"(tmux.ValidBufferName). LoadBuffer/PasteBuffer would return ErrStructural "+
					"and the payload would be dropped before tmux is invoked — a wedged dispatch, "+
					"not a clean error.", tc.sessionID, tc.purpose, got)
			}
			if !strings.HasPrefix(got, "harmonik-") {
				t.Errorf("bufferName(%q, %q) = %q; lost the PL-021d harmonik- prefix",
					tc.sessionID, tc.purpose, got)
			}
		})
	}
}

// TestBufferName_MatchesTmuxConstructor pins the daemon helper to the shared
// constructor rather than to a restated format string, so the two cannot drift.
func TestBufferName_MatchesTmuxConstructor(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"01hwxyz-abc123", "20060102T150405Z", "", "A_B.C"} {
		for _, purpose := range []string{"task", "feedback", "review", "gate", "crew-init"} {
			if got, want := daemon.ExportedBufferName(id, purpose), tmux.BufferName(id, purpose); got != want {
				t.Errorf("bufferName(%q, %q) = %q; tmux.BufferName gives %q — the daemon helper has drifted",
					id, purpose, got, want)
			}
		}
	}
}

// TestBufferName_AlreadyValidIDsUnchanged is the no-regression half: the ids
// production actually mints today (lowercase UUIDv7) must pass through byte for
// byte, so routing through the sanitizer changes nothing that works now.
func TestBufferName_AlreadyValidIDsUnchanged(t *testing.T) {
	t.Parallel()

	cases := []struct{ sessionID, purpose, want string }{
		{"01hwxyz-abc123", "task", "harmonik-01hwxyz-abc123-task"},
		{"01hwxyz-abc123", "feedback", "harmonik-01hwxyz-abc123-feedback"},
		{"01hwxyz-abc123", "review", "harmonik-01hwxyz-abc123-review"},
		{"0198f2a1-4c3d-7e11-9a02-6b5c8d1e2f30", "gate", "harmonik-0198f2a1-4c3d-7e11-9a02-6b5c8d1e2f30-gate"},
	}
	for _, tc := range cases {
		if got := daemon.ExportedBufferName(tc.sessionID, tc.purpose); got != tc.want {
			t.Errorf("bufferName(%q, %q) = %q, want %q", tc.sessionID, tc.purpose, got, tc.want)
		}
	}
}
