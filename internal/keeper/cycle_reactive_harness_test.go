package keeper_test

// cycle_reactive_harness_test.go — an OFFLINE, DETERMINISTIC reactive fake for
// the session keeper's clear->restart cycle.
//
// The existing cycle tests (e.g. TestCycler_HappyPath) FAKE the loop: the spy
// InjectFn merely records "/clear" as a string, and the post-clear session_id
// flip is faked on a fixed gauge call-count (gaugeReturnsNewSIDAfter) — nothing
// actually REACTS to the injected command. That leaves a gap: the test never
// proves the session-id flip is CAUSED by /clear.
//
// reactiveSession closes that loop. It holds MUTABLE gauge state (a CtxFile read
// back through the Cycler's ReadGaugeFn) plus a HANDOFF body, and its InjectFn
// pattern-matches the injected text and mutates that state the way a real claude
// session would:
//
//   - text contains "/session-handoff" -> extract the verbatim nonce
//     "<!-- KEEPER:<cycleID> -->" from the injected string and WRITE that exact
//     line into the HANDOFF body (this is what real claude's handoff skill does).
//     Toggleable via writeNonce: when false, the nonce is never written, so the
//     cycle's nonce poll times out and the cycle ABORTS before any /clear.
//   - text contains "/clear" -> rotate the gauge SessionID from the seed S1 to a
//     fresh UUIDv4 S2 (distinct, NOT a UUIDv7 — so waitForNewSessionID accepts
//     it) and drop pct/tokens below warn. Toggleable via flipOnClear.
//   - text contains "/session-resume" -> keep S2, steady low pct.
//
// ALL shared scenario helpers live in THIS ONE file so later scenario authors
// reuse them without redeclare collisions when the suite fans out.

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// nonceLineRE extracts the verbatim keeper nonce line from injected text.
// Must match the production format emitted by nonceMarker(): "<!-- KEEPER:<id> -->".
var nonceLineRE = regexp.MustCompile(`<!-- KEEPER:[^>]*-->`)

// reactiveSession is an in-process fake of a claude session that REACTS to
// injected commands by mutating shared gauge + handoff state. It is the seam
// that makes the clear->session-id-flip causal rather than time-faked.
type reactiveSession struct {
	mu sync.Mutex

	// gauge is the mutable CtxFile returned by ReadGaugeFn. Seeded with S1 and
	// rotated to S2 by the /clear reaction.
	gauge keeper.CtxFile

	// handoffBody is the mutable HANDOFF-<agent>.md content returned by
	// ReadHandoff. The /session-handoff reaction writes the nonce line here.
	handoffBody string

	// seedSID / clearedSID are the before/after session ids (S1 -> S2).
	seedSID    string // S1
	clearedSID string // S2 (UUIDv4; never a UUIDv7)

	// Reaction toggles (scenario knobs).
	writeNonce  bool // /session-handoff writes the nonce into handoffBody when true
	flipOnClear bool // /clear rotates SID S1->S2 + drops context when true

	// Observability / causality tracking.
	injected    []string // every injected command, in order
	clearedSeen bool     // set true the moment "/clear" is injected

	// sidFlipCause records the injected command DURING WHICH the gauge SID first
	// changed away from the seed. This is the load-bearing causality witness: the
	// SID mutation happens INSIDE inject(), so whichever command's reaction
	// rotated it is captured here verbatim. The full-cycle test asserts this is
	// exactly "/clear" — proving the flip is CAUSED by /clear, not merely
	// observed after it in time. Empty until the SID actually changes.
	sidFlipCause string
	// gaugeReadAfterClearOnly records, for each ReadGaugeFn call, whether a new
	// (non-seed) SID was observed BEFORE /clear had been injected. Any true
	// entry is a causality violation (SID flipped without /clear causing it).
	sidFlippedBeforeClear bool
}

// newReactiveSession seeds the harness over the act threshold on S1.
// seedSID should be the pre-clear session id (UUIDv4 or any non-empty string);
// clearedSID MUST be a UUIDv4 (version nibble 4) so waitForNewSessionID accepts
// it (it rejects UUIDv7 daemon-spawned ids).
func newReactiveSession(seedSID, clearedSID string, writeNonce, flipOnClear bool) *reactiveSession {
	return &reactiveSession{
		gauge: keeper.CtxFile{
			Pct:        95.0,
			Tokens:     320_000,
			WindowSize: 1_000_000,
			SessionID:  seedSID,
		},
		seedSID:     seedSID,
		clearedSID:  clearedSID,
		writeNonce:  writeNonce,
		flipOnClear: flipOnClear,
	}
}

// inject is the reactive InjectFn wired into CyclerConfig.InjectFn. It records
// the command and mutates shared state to mimic a real session's response.
func (rs *reactiveSession) inject(_ context.Context, _ /*target*/, text string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.injected = append(rs.injected, text)
	sidBefore := rs.gauge.SessionID

	switch {
	case containsSubstr(text, "/session-handoff"):
		// Real claude's handoff skill writes the verbatim nonce line into the
		// handoff file. Extract it from the injected directive and persist it.
		if rs.writeNonce {
			if m := nonceLineRE.FindString(text); m != "" {
				rs.handoffBody = "# Handoff (reactive fake)\n\n" + m + "\n\nrestored context.\n"
			}
		}
	case text == "/clear":
		rs.clearedSeen = true
		if rs.flipOnClear {
			// Rotate to the post-clear session and drop context below warn —
			// this is the CAUSAL effect the real /clear has on the gauge.
			rs.gauge.SessionID = rs.clearedSID
			rs.gauge.Pct = 8.0
			rs.gauge.Tokens = 12_000
		}
	case containsSubstr(text, "/session-resume"):
		// Resume keeps the post-clear session and steady-low context. No-op on
		// the gauge beyond what /clear already set.
	}

	// Causality witness: if THIS command's reaction changed the SID away from the
	// seed for the first time, record the command verbatim. The full-cycle test
	// asserts the cause is exactly "/clear".
	if rs.sidFlipCause == "" && rs.gauge.SessionID != sidBefore && rs.gauge.SessionID != rs.seedSID {
		rs.sidFlipCause = text
	}
	return nil
}

// readGauge is the reactive ReadGaugeFn. It returns a COPY of the live gauge and
// records a causality check: if a non-seed SID is observed before /clear was
// injected, that is a violation (the flip must be caused by /clear).
func (rs *reactiveSession) readGauge(_ /*projectDir*/, _ /*agent*/ string) (*keeper.CtxFile, time.Time, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.gauge.SessionID != rs.seedSID && !rs.clearedSeen {
		rs.sidFlippedBeforeClear = true
	}
	cp := rs.gauge
	return &cp, time.Now(), nil
}

// readHandoff is the reactive ReadHandoff. Returns the current handoff body;
// before the /session-handoff reaction writes the nonce, the body is empty.
func (rs *reactiveSession) readHandoff(_ /*path*/ string) (string, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.handoffBody, nil
}

// truncate is the reactive TruncateHandoffFn — clears the handoff body the way
// runCycle's pre-poll truncation does, so a stale nonce cannot pre-satisfy.
func (rs *reactiveSession) truncate(_ /*path*/ string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.handoffBody = ""
	return nil
}

// snapshotInjected returns a copy of the injected-command sequence.
func (rs *reactiveSession) snapshotInjected() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]string, len(rs.injected))
	copy(out, rs.injected)
	return out
}

// liveSID returns the current gauge session id.
func (rs *reactiveSession) liveSID() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.gauge.SessionID
}

// sawClear reports whether "/clear" was ever injected.
func (rs *reactiveSession) sawClear() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.clearedSeen
}

// sidViolatedCausality reports whether a non-seed SID was ever observed in the
// gauge before /clear was injected (i.e. the flip was NOT caused by /clear).
func (rs *reactiveSession) sidViolatedCausality() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.sidFlippedBeforeClear
}

// flipCause returns the injected command whose reaction first rotated the gauge
// SID away from the seed. Empty if the SID never changed.
func (rs *reactiveSession) flipCause() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.sidFlipCause
}

// newReactiveCycler builds a Cycler wired to drive rs. The seed CtxFile passed
// to MaybeRun should be rs.gauge (over the act threshold on S1). ClearSettle /
// HandoffTimeout / PollInterval are shrunk for fast deterministic unit runs.
//
// managedSet is set to true by SetManagedSessionFn so the test can assert the
// final binding == S2 without touching disk (mirrors the IdentityPinned test's
// capture-the-arg idiom).
func newReactiveCycler(
	agent, projectDir, cycleID string,
	rs *reactiveSession,
	em keeper.Emitter,
	jc *journalCapture,
	managedSet *string,
	handoffTimeout, clearSettle time.Duration,
) *keeper.Cycler {
	var mu sync.Mutex
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane", // non-empty so injection branches run
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: handoffTimeout,
		ClearSettle:    clearSettle,
		PollInterval:   5 * time.Millisecond,
		CycleIDGen:     func() string { return cycleID },
		IsManagedFn:    func(_, _ string) bool { return true },
		HandoffFilePath: func(_, a string) string {
			return "/tmp/HANDOFF-" + a + ".md"
		},
		ReadHandoff:       rs.readHandoff,
		TruncateHandoffFn: rs.truncate,
		InjectFn:          rs.inject,
		ReadGaugeFn:       rs.readGauge,
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		AppendHandoffFn:   func(_, _ string) error { return nil },
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
		SetManagedSessionFn: func(_, _, sid string) error {
			mu.Lock()
			defer mu.Unlock()
			*managedSet = sid
			return nil
		},
	}
	return keeper.NewCycler(cfg, em)
}
