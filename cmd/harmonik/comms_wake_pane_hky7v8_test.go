package main

import (
	"testing"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// TestCommsWakePaneCandidates_CaptainAndCrew verifies the recipient → tmux pane
// derivation handles the crew-vs-captain naming asymmetry (hk-y7v8 / CE5, M10):
//
//   - The CAPTAIN session is named "harmonik-<hash>-captain" (NO "crew-" prefix)
//     and has no crew-registry record. Before the fix, --wake derived only
//     "harmonik-<hash>-crew-captain", which does not exist, so a stalled captain
//     could not be roused. The candidate list MUST now include the bare
//     "harmonik-<hash>-captain" pane.
//   - A CREW session is named "harmonik-<hash>-crew-<name>" and (when registered)
//     also exposes a registry Handle. The crew pane MUST still be tried first so
//     crew wake is unaffected.
func TestCommsWakePaneCandidates_CaptainAndCrew(t *testing.T) {
	t.Parallel()

	t.Run("captain falls back to the bare (non-crew) session name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no crew record written → crew.Load misses
		hash := lifecycle.ComputeProjectHash(dir)

		got := commsWakePaneCandidates(dir, "captain")

		wantBare := lifecycle.TmuxSessionName(hash, "captain")     // harmonik-<hash>-captain
		wantCrew := lifecycle.TmuxSessionName(hash, "crew-captain") // the WRONG one alone

		if !containsString(got, wantBare) {
			t.Fatalf("captain wake candidates %v missing bare session %q — a stalled captain cannot be roused", got, wantBare)
		}
		// The crew-prefixed candidate may also be present (tried first, misses),
		// but the bare one is the load-bearing addition. Assert it is reachable.
		if got[len(got)-1] != wantBare {
			t.Errorf("expected bare captain session %q to be the final fallback candidate; got %v (crew variant %q is fine as an earlier miss)", wantBare, got, wantCrew)
		}
	})

	t.Run("registered crew resolves to its registry handle pane first", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		rec := crew.Record{
			Name:      "alpha",
			SessionID: "sess-alpha",
			Queue:     "q-alpha",
			Handle:    "harmonik-deadbeef-crew-alpha:0",
		}
		if err := crew.Write(dir, rec); err != nil {
			t.Fatalf("crew.Write: %v", err)
		}

		got := commsWakePaneCandidates(dir, "alpha")

		if len(got) == 0 || got[0] != rec.Handle+".0" {
			t.Fatalf("crew wake first candidate = %v, want handle pane %q first", got, rec.Handle+".0")
		}
	})

	t.Run("unregistered crew still resolves via the crew-prefixed convention", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no record for "beta"
		hash := lifecycle.ComputeProjectHash(dir)

		got := commsWakePaneCandidates(dir, "beta")

		wantCrew := lifecycle.TmuxSessionName(hash, "crew-beta")
		if !containsString(got, wantCrew) {
			t.Fatalf("unregistered crew candidates %v missing crew-convention session %q", got, wantCrew)
		}
	})
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
