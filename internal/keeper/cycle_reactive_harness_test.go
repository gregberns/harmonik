package keeper_test

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

var nonceLineRE = regexp.MustCompile(`<!-- KEEPER:[^>]*-->`)

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

	// clearDelay models a SLOW /clear: real /clear processing (a busy pane, a
	// slow re-mint of the session_id) that takes noticeably longer than a single
	// poll window. When > 0, the SID rotation triggered by "/clear" happens on a
	// background timer clearDelay after the command is injected, instead of
	// synchronously inside inject(). Zero (default) preserves the prior
	// synchronous-flip behavior. Refs: hk-vdqe2.
	clearDelay          time.Duration
	clearDelayScheduled bool // guards against scheduling more than one flip timer

	// writeHandoffNoNonce models the hk-fi78d bug shape: the agent writes a real,
	// fresh handoff body in response to /session-handoff but OMITS the verbatim
	// nonce line (echo garbled/forgotten). The nonce poll therefore times out, yet
	// a resumable handoff exists on disk. Only consulted when writeNonce is false.
	writeHandoffNoNonce bool

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

func (rs *reactiveSession) inject(_ context.Context, _ /*target*/, text string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.injected = append(rs.injected, text)
	sidBefore := rs.gauge.SessionID

	switch {
	case containsSubstr(text, "/session-handoff"):
		if rs.writeNonce {
			if m := nonceLineRE.FindString(text); m != "" {
				rs.handoffBody = "# Handoff (reactive fake)\n\n" + m + "\n\nrestored context.\n"
			}
		} else if rs.writeHandoffNoNonce {
			rs.handoffBody = "# Handoff (reactive fake)\n\nrestored context — NO nonce line echoed.\n"
		}
	case text == "/clear":
		rs.clearedSeen = true
		if rs.flipOnClear && rs.gauge.SessionID != rs.clearedSID {
			if rs.clearDelay > 0 {
				if !rs.clearDelayScheduled {
					rs.clearDelayScheduled = true
					delay := rs.clearDelay
					go func() {
						time.Sleep(delay)
						rs.mu.Lock()
						defer rs.mu.Unlock()
						if rs.gauge.SessionID != rs.clearedSID {
							rs.gauge.SessionID = rs.clearedSID
							rs.gauge.Pct = 8.0
							rs.gauge.Tokens = 12_000
							if rs.sidFlipCause == "" {
								rs.sidFlipCause = "/clear"
							}
						}
					}()
				}
			} else {
				rs.gauge.SessionID = rs.clearedSID
				rs.gauge.Pct = 8.0
				rs.gauge.Tokens = 12_000
			}
		}
	case containsSubstr(text, "agent brief"):
	}

	if rs.sidFlipCause == "" && rs.gauge.SessionID != sidBefore && rs.gauge.SessionID != rs.seedSID {
		rs.sidFlipCause = text
	}
	return nil
}

func (rs *reactiveSession) readGauge(_ /*projectDir*/, _ /*agent*/ string) (*keeper.CtxFile, time.Time, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.gauge.SessionID != rs.seedSID && !rs.clearedSeen {
		rs.sidFlippedBeforeClear = true
	}
	cp := rs.gauge
	return &cp, time.Now(), nil
}

func (rs *reactiveSession) readHandoff(_ /*path*/ string) (string, error) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.handoffBody, nil
}

func (rs *reactiveSession) handoffModTime(_ /*path*/ string) (time.Time, bool) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if rs.handoffBody == "" {
		return time.Time{}, false
	}
	return time.Now(), true
}

func (rs *reactiveSession) writeMarkedHandoff(cycleID string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.handoffBody = "# Handoff (late reactive fake)\n\n" + nonceMarkerForTest(cycleID) + "\n"
}

func nonceMarkerForTest(cycleID string) string { return "<!-- KEEPER:" + cycleID + " -->" }

func (rs *reactiveSession) truncate(_ /*path*/ string) error {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.handoffBody = nonceLineRE.ReplaceAllString(rs.handoffBody, "")
	return nil
}

func (rs *reactiveSession) snapshotInjected() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]string, len(rs.injected))
	copy(out, rs.injected)
	return out
}

func (rs *reactiveSession) liveSID() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.gauge.SessionID
}

func (rs *reactiveSession) sawClear() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.clearedSeen
}

func (rs *reactiveSession) sidViolatedCausality() bool {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.sidFlippedBeforeClear
}

func (rs *reactiveSession) flipCause() string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.sidFlipCause
}

func (rs *reactiveSession) withClearDelay(d time.Duration) *reactiveSession {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.clearDelay = d
	return rs
}

func newReactiveCycler(
	agent, projectDir, cycleID string,
	rs *reactiveSession,
	em keeper.Emitter,
	jc *journalCapture,
	managedSet *string,
	handoffTimeout, clearSettle time.Duration,
) *keeper.Cycler {
	return newReactiveCyclerWithBackstop(
		agent, projectDir, cycleID, rs, em, jc, managedSet,
		handoffTimeout, clearSettle, 3*clearSettle, 5,
	)
}

func newReactiveCyclerWithBackstop(
	agent, projectDir, cycleID string,
	rs *reactiveSession,
	em keeper.Emitter,
	jc *journalCapture,
	managedSet *string,
	handoffTimeout, clearSettle, clearConfirmBackstop time.Duration,
	clearConfirmRetries int,
) *keeper.Cycler {
	var mu sync.Mutex
	cfgOverrides := testCycleOverrides{CycleIDs:

	// non-empty so injection branches run

	func() string { return cycleID }, HandoffPath: func(_, a string) string {
		return "/tmp/HANDOFF-" + a + ".md"
	}, HandoffRead: rs.readHandoff, HandoffScrub: rs.truncate, Inject: rs.inject, Gauge: rs.readGauge, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:            agent,
		ProjectDir:           projectDir,
		TmuxTarget:           "fake-pane",
		ActPct:               90.0,
		WarnPct:              80.0,
		HandoffTimeout:       handoffTimeout,
		ClearSettle:          clearSettle,
		PollInterval:         5 * time.Millisecond,
		ClearConfirmBackstop: clearConfirmBackstop,
		ClearConfirmRetries:  clearConfirmRetries,
	}
	return mustNewCyclerWithOverridesAndDeps(cfg, em, cfgOverrides, func(deps *keeper.CycleDeps) {
		deps.Handoff = testHandoffWithModTime{HandoffDocument: deps.Handoff, modTime: rs.handoffModTime}
		deps.Activity = testActivityWithIdle{ActivityProbe: deps.Activity, idleMarker: func() (time.Time, bool) { return time.Now(), true }}
		deps.Context = testContextWithManaged{ContextStore: deps.Context, setManaged: func(sid string) error {
			mu.Lock()
			defer mu.Unlock()
			*managedSet = sid
			return nil
		}}
	})
}
