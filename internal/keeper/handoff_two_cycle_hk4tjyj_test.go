// handoff_two_cycle_hk4tjyj_test.go — a handoff written in cycle N must still be
// on disk when cycle N+1 opens. Bead: hk-4tjyj.

package keeper_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_TwoCycles_HandoffSurvivesTheNextCycleOpening reproduces the FIELD
// failure (crew `chani`, events.jsonl 20:11:41→20:11:42) end to end.
//
// Cycle 1 completes normally: the crew writes HANDOFF-<agent>.md with prose plus
// the cycle-1 nonce, the keeper confirms it and drives /clear + agent brief.
// Under a second, cycle 2 opens on the same pane — and because cycle 1's own
// nonce is now "stale", stepStartCycle emits ActTruncateHandoff. The old
// effector zeroed the file at that instant, so the rebooting session (which runs
// `agent brief` seconds later) read 0 bytes and printed "(no handoff on record)".
//
// The assertion is taken at the exact moment cycle 2 injects its directive — the
// same window the rebooting session reads in. Against the old truncate
// implementation the captured content is empty and both checks below fail.
func TestCycler_TwoCycles_HandoffSurvivesTheNextCycleOpening(t *testing.T) {
	t.Parallel()

	const agent = "two-cycle-agent"
	s1, s2 := reactiveSIDs()

	project := t.TempDir()
	handoffPath := filepath.Join(project, "HANDOFF-"+agent+".md")

	// The body the crew authors during cycle 1. It must still be readable when
	// cycle 2 opens.
	const cycle1Body = "# HANDOFF-two-cycle-agent\n\n" +
		"## Decisions\n\nLocked the review gate on hk-xyz; do not reopen.\n\n" +
		"## Next\n\nDrain the queue, then mail the captain.\n"

	sess := &twoCycleSession{
		handoffPath: handoffPath,
		body:        cycle1Body,
		seedSID:     s1,
		clearedSID:  s2,
		gauge:       keeper.CtxFile{Pct: 95.0, SessionID: s1},
	}

	// Distinct cycle ids: cycle 2's nonce differs from cycle 1's, which is what
	// makes cycle 1's marker read as STALE and fires the scrub.
	var idMu sync.Mutex
	idN := 0
	cycleIDGen := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		idN++
		return "cyc-two-" + string(rune('0'+idN))
	}

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managed string

	cfg := keeper.CyclerConfig{
		AgentName:            agent,
		ProjectDir:           project,
		TmuxTarget:           "fake-pane",
		IdleMarkerModTimeFn:  idleMarkerFreshNow,
		ActPct:               90.0,
		WarnPct:              80.0,
		HandoffTimeout:       300 * time.Millisecond,
		ClearSettle:          60 * time.Millisecond,
		PollInterval:         5 * time.Millisecond,
		ClearConfirmBackstop: 180 * time.Millisecond,
		ClearConfirmRetries:  5,
		CycleIDGen:           cycleIDGen,
		IsManagedFn:          func(_, _ string) bool { return true },
		// Real handoff file on disk + the PRODUCTION scrub effector (nil →
		// defaultScrubHandoffNonces). No fake stands between the test and the bug.
		InjectFn:          sess.inject,
		ReadGaugeFn:       sess.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sid string) error {
			managed = sid
			return nil
		},
	}
	cycler := keeper.NewCycler(cfg, em)
	ctx := context.Background()

	// ── Cycle 1: the crew writes a real handoff; the keeper clears and reboots.
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: 95.0, SessionID: s1}); err != nil {
		t.Fatalf("cycle 1 MaybeRun: %v", err)
	}
	if managed != s2 {
		t.Fatalf("cycle 1 did not complete: managed binding = %q, want %q", managed, s2)
	}
	afterCycle1, err := os.ReadFile(handoffPath) //nolint:gosec // G304: test-local temp path
	if err != nil {
		t.Fatalf("read handoff after cycle 1: %v", err)
	}
	if !strings.Contains(string(afterCycle1), cycle1Body) {
		t.Fatalf("cycle 1 did not leave the crew's handoff on disk; got:\n%q", afterCycle1)
	}

	// ── Re-arm the anti-loop gate: a below-warn reading on the NEW session id.
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: 8.0, SessionID: s2}); err != nil {
		t.Fatalf("re-arm tick: %v", err)
	}

	// ── Cycle 2 opens on the post-reboot session. The crew has NOT answered yet
	// — exactly the window in which the rebooting `agent brief` reads the file.
	sess.setGauge(keeper.CtxFile{Pct: 95.0, SessionID: s2})
	sess.stopWritingHandoff()
	if err := cycler.MaybeRun(ctx, &keeper.CtxFile{Pct: 95.0, SessionID: s2}); err != nil {
		t.Fatalf("cycle 2 MaybeRun: %v", err)
	}

	seen, sampled := sess.handoffSeenAtCycle2Inject()
	if !sampled {
		t.Fatal("cycle 2 never injected a /session-handoff directive — nothing was sampled")
	}
	if len(seen) == 0 {
		t.Fatalf("the handoff was ZERO BYTES when cycle 2 opened — cycle 1's handoff was destroyed (hk-4tjyj)")
	}
	if !strings.Contains(seen, cycle1Body) {
		t.Errorf("cycle 1's handoff body was DESTROYED when cycle 2 opened (hk-4tjyj).\n"+
			"want to still contain:\n%q\ngot:\n%q", cycle1Body, seen)
	}
	if strings.Contains(seen, "<!-- KEEPER:") {
		t.Errorf("cycle 1's stale nonce survived into cycle 2's poll window; got:\n%q", seen)
	}
}

// twoCycleSession is a minimal reactive session backed by a REAL handoff file on
// disk, so the production scrub effector is the code under test. The existing
// reactiveSession harness keeps the handoff in memory behind its own
// TruncateHandoffFn fake, which would hide the defect.
type twoCycleSession struct {
	mu          sync.Mutex
	handoffPath string
	body        string
	seedSID     string
	clearedSID  string
	gauge       keeper.CtxFile

	writeOnHandoff bool // cycle 1 answers the directive; cycle 2 does not
	cycle2Seen     string
	cycle2Sampled  bool
	sawFirstWrite  bool
}

func (s *twoCycleSession) inject(_ context.Context, _, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case strings.Contains(text, "/session-handoff"):
		// Sample the file EXACTLY as the rebooting session would see it.
		data, err := os.ReadFile(s.handoffPath) //nolint:gosec // G304: test-local temp path
		if err == nil && s.sawFirstWrite {
			s.cycle2Seen = string(data)
			s.cycle2Sampled = true
		}
		if !s.writeOnHandoff && !s.sawFirstWrite {
			// Cycle 1: the crew writes prose + the verbatim nonce.
			nonce := nonceLineRE.FindString(text)
			if nonce == "" {
				return nil
			}
			content := s.body + "\n" + nonce + "\n"
			if err := os.WriteFile(s.handoffPath, []byte(content), 0o600); err != nil {
				return err
			}
			s.sawFirstWrite = true
		}
	case text == "/clear":
		if s.gauge.SessionID != s.clearedSID {
			s.gauge.SessionID = s.clearedSID
			s.gauge.Pct = 8.0
		}
	}
	return nil
}

func (s *twoCycleSession) readGauge(_, _ string) (*keeper.CtxFile, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.gauge
	return &cp, time.Now(), nil
}

func (s *twoCycleSession) setGauge(cf keeper.CtxFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gauge = cf
}

// stopWritingHandoff models the crew not having answered cycle 2's directive yet.
func (s *twoCycleSession) stopWritingHandoff() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writeOnHandoff = true
}

func (s *twoCycleSession) handoffSeenAtCycle2Inject() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cycle2Seen, s.cycle2Sampled
}
