package main

// scenario_comms_wake_pane_hkmpel5_test.go — T1d L0: commsWakePaneCandidates
// candidate order + resolveProjectPath symlink-hash (scenario 6 L0 half,
// design doc §3 G3, bead hk-mpel5).
//
// L0 pure-projection: call commsWakePaneCandidates and resolveProjectPath
// directly — zero goroutines, zero daemon, zero socket. The seam under test
// is the pure candidate-derivation + path-resolution logic that the --wake
// path exercises before any tmux call is attempted.
//
// Scenario 6 (G3) L0 half asserts two properties:
//
//  1. Candidate order: the returned slice is always crew-registry →
//     crew-convention → captain-bare. The captain's bare session name
//     "harmonik-<hash>-<name>" MUST be the last candidate so that a stalled
//     captain can be roused (M10 / reference_comms_wake_captain_pane_mismatch).
//
//  2. Symlink-hash parity: when the project directory is (or contains) a
//     symlink, resolveProjectPath EvalSymlinks-resolves it so that the derived
//     hash matches the hash the tmux session was spawned with. A candidate
//     derived from the UNRESOLVED symlink path hashes differently and would
//     target a nonexistent pane (hk-z365 regression).
//
// Table covers:
//   A. captain  (no registry entry) — bare candidate is the last
//   B. registered crew (has Handle) — handle pane is first
//   C. unregistered crew            — crew convention is present
//   D. symlinked project dir        — resolved hash matches; wrong hash absent
//
// Bead: hk-mpel5. Part-of: comms-test-harness.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

// scenarioResolvedHash returns the project hash that a real tmux session name
// would use, mirroring lifecycle/tmux/subcommand.go tmuxStartHashDir and
// internal/keeper/tmuxresolve.go HarmonikSessionName (both EvalSymlinks then
// hash). resolveProjectPath in comms.go MUST agree with this.
func scenarioResolvedHash(dir string) core.ProjectHash {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		resolved = dir
	}
	return lifecycle.ComputeProjectHash(resolved)
}

// TestCommsWakePaneCandidates_Scenario6_L0 is the T1d acceptance-corpus test
// for scenario 6 (G3) L0 half. It asserts the candidate-ordering and
// symlink-hash-parity properties as a table-driven matrix.
func TestCommsWakePaneCandidates_Scenario6_L0(t *testing.T) {
	t.Parallel()

	t.Run("A: captain bare candidate is last fallback", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no crew record → registry miss
		hash := scenarioResolvedHash(dir)

		got := commsWakePaneCandidates(dir, "captain")

		wantBare := lifecycle.TmuxSessionName(hash, "captain")
		wantCrew := lifecycle.TmuxSessionName(hash, "crew-captain")

		// The crew-convention candidate appears (it's tried first, but misses at
		// runtime). The bare candidate MUST be present for waking the captain.
		if !scenarioCandidateContains(got, wantBare) {
			t.Errorf("A: bare captain candidate %q absent; got %v", wantBare, got)
		}
		if !scenarioCandidateContains(got, wantCrew) {
			t.Errorf("A: crew-captain candidate %q absent; got %v", wantCrew, got)
		}
		// Ordering: bare must be the LAST candidate (final fallback).
		if len(got) == 0 || got[len(got)-1] != wantBare {
			t.Errorf("A: bare captain %q must be the last candidate; got %v", wantBare, got)
		}
		// Ordering: crew-convention must appear BEFORE bare.
		crewIdx, bareIdx := -1, -1
		for i, s := range got {
			if s == wantCrew {
				crewIdx = i
			}
			if s == wantBare {
				bareIdx = i
			}
		}
		if crewIdx >= 0 && bareIdx >= 0 && crewIdx >= bareIdx {
			t.Errorf("A: crew-convention candidate (idx=%d) must precede bare candidate (idx=%d)", crewIdx, bareIdx)
		}
	})

	t.Run("B: registered crew handle is first candidate", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		rec := crew.Record{
			Name:      "admiral",
			SessionID: "sess-admiral",
			Queue:     "q-admiral",
			Handle:    "harmonik-deadbeef-crew-admiral:0",
		}
		if err := crew.Write(dir, rec); err != nil {
			t.Fatalf("B: crew.Write: %v", err)
		}

		got := commsWakePaneCandidates(dir, "admiral")

		// The handle pane (handle+".0") must be first.
		wantFirst := rec.Handle + ".0"
		if len(got) == 0 || got[0] != wantFirst {
			t.Errorf("B: first candidate = %q, want handle pane %q; full list: %v", got[0], wantFirst, got)
		}
	})

	t.Run("C: unregistered crew convention candidate present", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir() // no registry entry for "paul"
		hash := scenarioResolvedHash(dir)

		got := commsWakePaneCandidates(dir, "paul")

		wantCrew := lifecycle.TmuxSessionName(hash, "crew-paul")
		wantBare := lifecycle.TmuxSessionName(hash, "paul")

		if !scenarioCandidateContains(got, wantCrew) {
			t.Errorf("C: crew-convention candidate %q absent; got %v", wantCrew, got)
		}
		if !scenarioCandidateContains(got, wantBare) {
			t.Errorf("C: bare candidate %q absent; got %v", wantBare, got)
		}
	})

	t.Run("D: symlinked project produces resolved hash (hk-z365 regression)", func(t *testing.T) {
		t.Parallel()

		base := t.TempDir()
		real := filepath.Join(base, "real-project")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatalf("D: mkdir real: %v", err)
		}
		link := filepath.Join(base, "link-project")
		if err := os.Symlink(real, link); err != nil {
			// Symlink creation can fail on some CI sandbox environments.
			t.Skipf("D: symlink creation not supported: %v", err)
		}

		// Sanity: the symlink must actually differ from the resolved path,
		// otherwise this test passes vacuously.
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("D: EvalSymlinks: %v", err)
		}
		if resolved == link {
			t.Skipf("D: EvalSymlinks(%q) == input; test is vacuous on this OS", link)
		}

		// The candidates derived from the symlink path must use the RESOLVED hash.
		got := commsWakePaneCandidates(link, "paul")

		wantHash := scenarioResolvedHash(link) // == ComputeProjectHash(resolved)
		wantCrew := lifecycle.TmuxSessionName(wantHash, "crew-paul")
		wantBare := lifecycle.TmuxSessionName(wantHash, "paul")

		if !scenarioCandidateContains(got, wantCrew) {
			t.Errorf("D: resolved crew pane %q absent; got %v (hk-z365)", wantCrew, got)
		}
		if !scenarioCandidateContains(got, wantBare) {
			t.Errorf("D: resolved bare pane %q absent; got %v (hk-z365)", wantBare, got)
		}

		// The WRONG hash (unresolved symlink path) must NOT appear in any candidate.
		wrongHash := lifecycle.ComputeProjectHash(link) // raw symlink, not EvalSymlinks
		wrongCrew := lifecycle.TmuxSessionName(wrongHash, "crew-paul")
		wrongBare := lifecycle.TmuxSessionName(wrongHash, "paul")
		if scenarioCandidateContains(got, wrongCrew) {
			t.Errorf("D: unresolved-path crew pane %q should be absent; presence means hk-z365 is regressed", wrongCrew)
		}
		if scenarioCandidateContains(got, wrongBare) {
			t.Errorf("D: unresolved-path bare pane %q should be absent; presence means hk-z365 is regressed", wrongBare)
		}
	})
}

// TestResolveProjectPath_Scenario6_L0 directly tests the resolveProjectPath
// seam: EvalSymlinks-then-fallback. It is the unit complement to the
// integration view in TestCommsWakePaneCandidates_Scenario6_L0 case D.
func TestResolveProjectPath_Scenario6_L0(t *testing.T) {
	t.Parallel()

	t.Run("real dir returns itself", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		got := resolveProjectPath(dir)
		// filepath.EvalSymlinks on a real dir may resolve /var→/private/var on
		// macOS; the returned path must be the canonical form, not necessarily ==dir.
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			resolved = dir
		}
		if got != resolved {
			t.Errorf("real dir: resolveProjectPath(%q) = %q, want %q", dir, got, resolved)
		}
	})

	t.Run("symlink returns resolved target", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		real := filepath.Join(base, "real")
		if err := os.Mkdir(real, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		link := filepath.Join(base, "link")
		if err := os.Symlink(real, link); err != nil {
			t.Skipf("symlink creation not supported: %v", err)
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			t.Fatalf("EvalSymlinks: %v", err)
		}
		if resolved == link {
			t.Skipf("EvalSymlinks(%q) == input; test vacuous", link)
		}

		got := resolveProjectPath(link)
		if got != resolved {
			t.Errorf("symlink: resolveProjectPath(%q) = %q, want resolved %q", link, got, resolved)
		}
	})

	t.Run("nonexistent path returns input unchanged", func(t *testing.T) {
		t.Parallel()
		absent := filepath.Join(t.TempDir(), "does-not-exist")
		got := resolveProjectPath(absent)
		if got != absent {
			t.Errorf("absent path: resolveProjectPath(%q) = %q, want unchanged input", absent, got)
		}
	})
}

// scenarioCandidateContains reports whether ss contains want.
func scenarioCandidateContains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
