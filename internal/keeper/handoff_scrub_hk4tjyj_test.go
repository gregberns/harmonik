// handoff_scrub_hk4tjyj_test.go — the keeper's stale-nonce clear must SCRUB the
// marker, never destroy the crew's handoff body. Bead: hk-4tjyj.

package keeper_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_StaleNonceScrub_PreservesHandoffBody is THE regression for the
// silent fleet-wide handoff destruction (hk-4tjyj).
//
// stepStartCycle emits ActTruncateHandoff whenever the handoff file carries a
// keeper nonce from a PRIOR cycle. The effector used to be
// `os.WriteFile(path, []byte{}, 0600)` — it zeroed the entire file. Because
// every SUCCESSFUL cycle leaves its own nonce behind, that predicate is true on
// essentially every cycle after the first, so the keeper deleted the crew's
// prose, decisions, and next steps on nearly every restart. The rebooted session
// then read a 0-byte file and printed "(no handoff on record)".
//
// This test seeds a handoff with real prose PLUS a stale marker, runs a cycle,
// and asserts the prose survives BYTE-FOR-BYTE while only the marker is gone.
// Against the old truncate implementation the file is empty here and every
// content assertion below fails.
func TestCycler_StaleNonceScrub_PreservesHandoffBody(t *testing.T) {
	t.Parallel()

	const (
		agent   = "scrub-agent"
		cycleID = "cyc-scrub-001"
		sid     = "sess-scrub"
	)

	project := t.TempDir()
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")

	// The crew's real handoff: prose the keeper must never touch, plus a marker
	// left behind by the PREVIOUS cycle.
	const body = "# HANDOFF-scrub-agent\n\n" +
		"## Where I am\n\nMid-review on hk-abc; the daemon merged hk-def at 19:04.\n\n" +
		"## Next\n\n1. Re-run the l0 gate.\n2. Mail the captain.\n"
	seed := body + "\n<!-- KEEPER:cyc-OLD-000042 -->\n"
	if err := os.WriteFile(handoffPath, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	cfg := keeper.CyclerConfig{
		AgentName:           agent,
		ProjectDir:          project,
		TmuxTarget:          "", // no pane: the cycle runs to its handoff timeout
		IdleMarkerModTimeFn: idleMarkerFreshNow,
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      60 * time.Millisecond,
		ClearSettle:         30 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		// Real file paths + the REAL production scrub effector (TruncateHandoffFn
		// left nil so applyDefaults binds defaultScrubHandoffNonces) — that is the
		// code under test.
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
		},
		InjectFn:          spy.inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{Pct: 95.0, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	after, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read handoff after cycle: %v", err)
	}
	got := string(after)

	// (a) The file must not have been emptied. This is the whole bug.
	if len(got) == 0 {
		t.Fatalf("handoff was TRUNCATED to 0 bytes — the crew's handoff was destroyed (hk-4tjyj)")
	}

	// (b) The crew's prose survives BYTE-FOR-BYTE.
	if !strings.HasPrefix(got, body) {
		t.Errorf("handoff body was not preserved byte-for-byte.\nwant prefix:\n%q\ngot:\n%q", body, got)
	}

	// (c) The stale marker is gone (it must not pre-satisfy the next nonce poll).
	if strings.Contains(got, "<!-- KEEPER:") {
		t.Errorf("stale keeper nonce survived the scrub; got:\n%q", got)
	}
}

// TestScrubHandoffNonces_MissingFileIsNoOp pins the effector contract for an
// absent handoff: a no-op, NOT an error and NOT a file creation. The reactor
// emits ActTruncateHandoff only when it sampled content, but the effector runs
// best-effort and must not manufacture a zero-byte handoff — that is exactly the
// state hk-4tjyj made indistinguishable from "never written".
func TestScrubHandoffNonces_MissingFileIsNoOp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "HANDOFF-absent.md")

	cfg := keeper.CyclerConfig{}
	cfg = resolveTruncateFn(cfg)
	if err := cfg.TruncateHandoffFn(path); err != nil {
		t.Fatalf("scrub on a missing file returned an error: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("scrub created the handoff file; want it left absent (stat err = %v)", err)
	}
}

// TestScrubHandoffNonces_NoMarkerLeavesFileUntouched proves a genuine handoff
// with no keeper marker is not rewritten at all — content AND mtime preserved.
// The mtime matters: the hk-fi78d freshness sampler compares the handoff mtime
// against the cycle's injection instant, so a gratuitous rewrite would be a
// second, subtler defect.
func TestScrubHandoffNonces_NoMarkerLeavesFileUntouched(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "HANDOFF-plain.md")
	const body = "# HANDOFF-plain\n\nNo keeper marker anywhere in this file.\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	cfg := resolveTruncateFn(keeper.CyclerConfig{})
	if err := cfg.TruncateHandoffFn(path); err != nil {
		t.Fatalf("scrub: %v", err)
	}

	got, err := os.ReadFile(path) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != body {
		t.Errorf("content changed.\nwant: %q\ngot:  %q", body, string(got))
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("mtime changed (%v → %v); a marker-free handoff must not be rewritten",
			before.ModTime(), after.ModTime())
	}
}

// resolveTruncateFn returns cfg with the production TruncateHandoffFn bound, via
// the same applyDefaults path production uses. Keeps the tests honest: they
// exercise the real effector, not a re-declaration of it.
func resolveTruncateFn(cfg keeper.CyclerConfig) keeper.CyclerConfig {
	resolved := keeper.ResolveCyclerDefaultsForTest()
	cfg.TruncateHandoffFn = resolved.TruncateHandoffFn
	return cfg
}

// TestCycler_EmptyTarget_ScrubbedStaleHandoff_StillAborts pins the freshness
// invariant that preserving the handoff body now rests on (hk-4tjyj, review
// finding 4).
//
// Zeroing the file used to make the non-empty-content guard in
// sampleHandoffFreshness reject a stale handoff outright. Now the body survives
// the scrub, so `mtime >= handoffInjectedAt` is the sole discriminator between
// "written for THIS cycle" and "left over from an earlier one". The scrub
// REWRITES the file, bumping its mtime to ~now — so if the per-cycle anchor were
// ever stamped before the scrub (or not stamped at all), a leftover handoff would
// read as fresh, take the recovery path, and /clear a session that never wrote a
// handoff.
//
// This runs the EMPTY-TARGET path deliberately: it emits no ActInjectHandoffCmd,
// so the anchor comes from executeArmTimer's hk-fi78d parity block instead. Real
// file, real scrub effector, no fakes in between. The cycle must ABORT.
func TestCycler_EmptyTarget_ScrubbedStaleHandoff_StillAborts(t *testing.T) {
	t.Parallel()

	const (
		agent   = "scrub-freshness-agent"
		cycleID = "cyc-scrub-fresh-001"
		sid     = "sess-scrub-fresh"
	)

	project := t.TempDir()
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")

	// A handoff from a PRIOR cycle: real body + that cycle's nonce. The scrub
	// fires (the nonce is stale) and rewrites the file with a fresh mtime.
	const body = "# HANDOFF-scrub-freshness-agent\n\nPrior cycle's decisions.\n"
	if err := os.WriteFile(handoffPath, []byte(body+"\n<!-- KEEPER:cyc-PRIOR -->\n"), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}

	cfg := keeper.CyclerConfig{
		AgentName:           agent,
		ProjectDir:          project,
		TmuxTarget:          "", // no inject action → anchor comes from executeArmTimer
		IdleMarkerModTimeFn: idleMarkerFreshNow,
		ActPct:              90.0,
		WarnPct:             80.0,
		HandoffTimeout:      80 * time.Millisecond,
		ClearSettle:         30 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		// Everything below the handoff file is REAL: default HandoffFilePath,
		// ReadHandoff, HandoffModTimeFn and TruncateHandoffFn (the scrub).
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
		},
		InjectFn:          (&cycleSpyInjector{}).inject,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, em)

	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{Pct: 95.0, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// The body must have survived (that is the fix) …
	after, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read handoff: %v", err)
	}
	if !strings.HasPrefix(string(after), body) {
		t.Errorf("handoff body was not preserved; got %q", string(after))
	}

	// … and preserving it must NOT have turned a leftover handoff into a
	// "recovery": the cycle must abort, never reach /clear.
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases — the cycle did not fire")
	}
	if last := phases[len(phases)-1]; last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\" — a scrub-rewritten stale handoff "+
			"must not read as fresh (hk-4tjyj freshness invariant). phases=%v", last, phases)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)); n != 0 {
		t.Errorf("want 0 cycle_recovered; got %d — the stale handoff was mistaken for a fresh one", n)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete on abort; got %d", n)
	}
}
