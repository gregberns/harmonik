package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
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
		// Match production: the wake path EvalSymlinks-resolves dir before hashing
		// (hk-z365), and t.TempDir() on macOS is itself under a /var→/private/var
		// symlink, so the expected hash MUST be the resolved-path hash.
		hash := tmuxSessionNameHash(dir)

		got := commsWakePaneCandidates(dir, "captain")

		wantBare := lifecycle.TmuxSessionName(hash, "captain")      // harmonik-<hash>-captain
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
		// Resolved-path hash to match the wake path's EvalSymlinks step (hk-z365).
		hash := tmuxSessionNameHash(dir)

		got := commsWakePaneCandidates(dir, "beta")

		wantCrew := lifecycle.TmuxSessionName(hash, "crew-beta")
		if !containsString(got, wantCrew) {
			t.Fatalf("unregistered crew candidates %v missing crew-convention session %q", got, wantCrew)
		}
	})
}

// tmuxSessionNameHash replicates how a tmux session name's project hash is
// derived in production — filepath.EvalSymlinks(dir) with a fallback to the raw
// path on error, then lifecycle.ComputeProjectHash — mirroring
// internal/lifecycle/tmux/subcommand.go tmuxStartHashDir and
// internal/keeper/tmuxresolve.go HarmonikSessionName. The wake codepath MUST
// agree with this so it targets the real, live session pane.
func tmuxSessionNameHash(dir string) core.ProjectHash {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	return lifecycle.ComputeProjectHash(resolved)
}

// TestCommsWakePaneCandidates_SymlinkedProjectMatchesTmuxSessionHash is the
// hk-z365 regression guard. When the project path is (or contains) a symlink,
// the wake pane-name hash MUST equal the tmux session-name's project hash —
// both resolve the symlink with filepath.EvalSymlinks. Before the fix, the wake
// path hashed the unresolved (filepath.Abs) path, so on a symlinked project root
// the two hashes diverged and --wake targeted a "harmonik-<wrongHash>-..." pane
// that never existed, silently failing crew/captain wake.
func TestCommsWakePaneCandidates_SymlinkedProjectMatchesTmuxSessionHash(t *testing.T) {
	t.Parallel()

	// real is the physical project directory; link is a symlink pointing at it.
	// We feed the SYMLINK path into the wake derivation and assert it produces
	// the same hash as a tmux session spawned from the symlink path (which
	// EvalSymlinks-resolves to `real`).
	base := t.TempDir()
	real := filepath.Join(base, "real-project")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir real: %v", err)
	}
	link := filepath.Join(base, "link-project")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Sanity: the symlink path and its resolved target must actually differ,
	// otherwise the test would pass vacuously.
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatalf("EvalSymlinks(link): %v", err)
	}
	if resolved == link {
		t.Fatalf("test setup is vacuous: EvalSymlinks(%q) did not differ from the input", link)
	}

	wantHash := tmuxSessionNameHash(link) // == ComputeProjectHash(resolved)
	wantCrew := lifecycle.TmuxSessionName(wantHash, "crew-paul")
	wantBare := lifecycle.TmuxSessionName(wantHash, "paul")

	got := commsWakePaneCandidates(link, "paul")

	if !containsString(got, wantCrew) {
		t.Errorf("symlinked project: wake candidates %v missing EvalSymlinks-hashed crew pane %q (tmux session hash)", got, wantCrew)
	}
	if !containsString(got, wantBare) {
		t.Errorf("symlinked project: wake candidates %v missing EvalSymlinks-hashed bare pane %q (tmux session hash)", got, wantBare)
	}

	// And the bug-reproducing negative: the OLD (Abs, unresolved) hash must NOT
	// appear — that is exactly the nonexistent pane the old code targeted.
	wrongHash := lifecycle.ComputeProjectHash(link) // unresolved symlink path
	wrongCrew := lifecycle.TmuxSessionName(wrongHash, "crew-paul")
	if containsString(got, wrongCrew) {
		t.Errorf("symlinked project: wake candidates %v still contain the unresolved-path pane %q (the hk-z365 bug)", got, wrongCrew)
	}
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
