package daemon_test

// reviewloop_resume_realsession_hkza5mz_test.go — regression test for hk-za5mz:
// the review-loop's iteration-2 never makes progress because the real Claude
// session_id is never captured under the tmux substrate.
//
// # Root cause (evidence-backed, hk-za5mz)
//
// Under the tmux substrate, handler.launchViaSubstrate returns early when
// subSess.Stdout() is nil (internal/handler/handler.go:303), so it NEVER calls
// spec.StdoutWrapper — the SessionIDInterceptor wired at reviewloop.go. Therefore
// the iteration-1 capture block at reviewloop.go reads nothing from
// sessionIDFromCapabilities and falls back to rlSynthesiseClaudeSessionID(),
// producing e.g. "syntheticclaudesession20260530114306". Iteration 2 then launches
// `claude --resume <synthetic>` against a session that NEVER EXISTED, so the
// resumed implementer lands no commit, the diff hash is unchanged, and the loop
// terminates with no_progress_detected.
//
// # The fix
//
// The real Claude session_id IS available without the stdout interceptor: it is
// the minted UUIDv7 passed to `claude --session-id <uuid>` (implArtifacts.
// claudeSessionID). The hook-relay's CHB-012 mismatch guard
// (bridge_session_id_mismatch) GUARANTEES Claude's real session_id equals this
// minted value — any hook payload with a different session_id is rejected. So when
// the interceptor never fires, the iteration-1 capture block must fall back to the
// real minted id (which `--resume` can target) rather than synthesizing a bogus
// one.
//
// # What this test asserts
//
//   - rlResolveIter1ClaudeSessionID prefers the interceptor id when present.
//   - When the interceptor never fired (id==""), it returns the REAL minted id
//     (not a synthetic one) so iteration-2 --resume targets a live session.
//   - It only synthesizes when BOTH the interceptor id and the minted id are
//     empty (degenerate twin-binary test path with no --session-id).
//
// Bead: hk-za5mz.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// syntheticPrefix is the prefix rlSynthesiseClaudeSessionID() emits. A resolved
// session id carrying this prefix means the daemon will `--resume` a session that
// never existed — the hk-za5mz bug.
const syntheticPrefix = "syntheticclaudesession"

// TestResolveIter1SessionID_InterceptorPresent verifies the ideal CHB-023 path:
// when the stdout interceptor captured the session id, it is used verbatim.
func TestResolveIter1SessionID_InterceptorPresent(t *testing.T) {
	t.Parallel()
	const interceptorID = "0192f000-1111-7abc-8def-000000000001"
	const mintedID = "0192f000-2222-7abc-8def-000000000002"

	got := daemon.ExportedResolveIter1ClaudeSessionID(interceptorID, mintedID)
	if got != interceptorID {
		t.Errorf("interceptor present: got %q; want interceptor id %q", got, interceptorID)
	}
	if strings.HasPrefix(got, syntheticPrefix) {
		t.Errorf("interceptor present: resolved id is synthetic %q; must use the captured id", got)
	}
}

// TestResolveIter1SessionID_TmuxFallbackUsesRealMintedID is the core hk-za5mz
// regression: under the tmux substrate the interceptor never fires (id==""), but
// the minted `--session-id` value is the REAL resumable session. The resolver must
// return that real id, NOT a synthetic one.
//
// Before the fix this test is RED: the resolver returns rlSynthesiseClaudeSessionID()
// whenever the interceptor id is empty, so `--resume` later targets a dead session.
func TestResolveIter1SessionID_TmuxFallbackUsesRealMintedID(t *testing.T) {
	t.Parallel()
	const mintedID = "0192f000-3333-7abc-8def-000000000003"

	// Interceptor never fired under tmux (Stdout()==nil → StdoutWrapper skipped).
	got := daemon.ExportedResolveIter1ClaudeSessionID("", mintedID)

	if strings.HasPrefix(got, syntheticPrefix) {
		t.Fatalf("hk-za5mz: interceptor absent under tmux but resolver returned a SYNTHETIC id %q; "+
			"iteration-2 --resume would target a session that never existed → no_progress. "+
			"Must fall back to the real minted --session-id value.", got)
	}
	if got != mintedID {
		t.Errorf("hk-za5mz: interceptor absent: got %q; want the real minted id %q so --resume targets a live session",
			got, mintedID)
	}
}

// TestResolveIter1SessionID_DegenerateSynthesis verifies the only remaining
// synthesis path: BOTH the interceptor id and the minted id are empty (a twin-binary
// test path that launched without --session-id). Synthesis is correct here because
// there is no real session to resume.
func TestResolveIter1SessionID_DegenerateSynthesis(t *testing.T) {
	t.Parallel()
	got := daemon.ExportedResolveIter1ClaudeSessionID("", "")
	if !strings.HasPrefix(got, syntheticPrefix) {
		t.Errorf("both ids empty: got %q; want a synthetic id (no real session exists to resume)", got)
	}
}
