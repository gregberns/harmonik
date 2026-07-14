package keepertwin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// CycleSummary mirrors the per-cycle golden summary.json produced by
// scripts/extract-keeper-corpus.py (measurement-design §1.4).
type CycleSummary struct {
	CKey             string   `json:"ckey"`       // "<agent_name>|<cycle_id>" — the D7 composite join key
	AgentName        string   `json:"agent_name"` //
	CycleID          string   `json:"cycle_id"`
	Outcome          string   `json:"outcome"`      // "complete" | "aborted" | "unterminated"
	AbortReason      string   `json:"abort_reason"` // "handoff_timeout" for all 79 baseline aborts
	ClearUnconfirmed bool     `json:"clear_unconfirmed"`
	StartedAt        string   `json:"started_at"` // RFC3339 handoff_started timestamp_wall
	TerminalAt       string   `json:"terminal_at"`
	SessionIDStart   string   `json:"session_id_start"`
	EventCount       int      `json:"event_count"`
	Types            []string `json:"types"`
}

// Stratum classifies a recorded cycle into one of the four synthesis strata
// (measurement-design §2.4 rows / §5 stratified sample).
type Stratum string

// The four strata. Every one of the 507 baseline cycles falls into exactly one.
const (
	StratumCleanComplete       Stratum = "clean_complete"        // complete, clear_unconfirmed:false (80 cycles)
	StratumDegradedComplete    Stratum = "degraded_complete"     // complete, clear_unconfirmed:true  (347 cycles)
	StratumAbortHandoffTimeout Stratum = "abort_handoff_timeout" // aborted{handoff_timeout}          (79 cycles)
	StratumUnterminated        Stratum = "unterminated"          // started, never terminal           (1 cycle)
)

// OldCorpusModelDoneSource is the SK-018 old-corpus ModelDone carve-out pin
// (D12; measurement-design §2.5). The boundary corpus predates the model_done
// signal, so the synthesizer schedules a ModelDone immediately after
// NonceObserved (zero virtual delay) — and it must carry a REAL Source enum
// value, because T8 removed the provisional "immediate" synthesis: the reactor
// enum is exactly {"idle_marker" | "transcript_turn" | "timeout"}.
//
// DECISION (pinned): "idle_marker".
//   - "idle_marker" is the PRIMARY real signal (SK-014): a genuinely idle pane
//     at zero delay is exactly what the pre-rebuild clear-immediately behavior
//     assumed, so old-corpus action goldens stay byte-identical to today's
//     behavior with degraded:false.
//   - "timeout" is wrong: it is reserved for the model_done_timeout fail-open
//     TimerFired edge, which stamps degraded:true — synthesizing it as an
//     external ModelDone event would fabricate a source/degraded combination
//     ("timeout", degraded:false) the reactor can never produce live.
//   - "transcript_turn" is the BACKSTOP detector; using the backstop for the
//     nominal old-corpus path would misrepresent 507 cycles as degraded-path.
//
// Only interior corpora (re-extracted after a post-EV-U1 baseline capture)
// schedule ModelDone at its real .idle-flip offset with the recorded source.
const OldCorpusModelDoneSource = "idle_marker"

// stimulusPct is the gauge percentage synthesized on the cycle-entry GaugeTick:
// at/above the default act threshold (90) so the ladder fires, below the
// default force threshold (95) so no forced-clear bookkeeping is triggered.
const stimulusPct = 92.0

// stepKind names a synthesized stimulus step (a keeper.Event constructor).
type stepKind string

const (
	stepGaugeTick      stepKind = "gauge_tick"
	stepNonceObserved  stepKind = "nonce_observed"
	stepModelDone      stepKind = "model_done"
	stepSessionChanged stepKind = "session_changed"
	stepTimerFired     stepKind = "timer_fired"
)

// stimStep is one row-cell of the synthesis decision table: which input event
// to schedule and at what virtual-time offset from cycle entry.
type stimStep struct {
	step  stepKind
	timer keeper.TimerKind // stepTimerFired only
	at    time.Duration    // virtual offset from cycle entry (t0)
}

// Virtual-time offsets used by the synthesis table. All are deterministic
// build-time constants; the recorded ~35 days of baseline replay in
// milliseconds of virtual time (measurement-design §2.4).
const (
	// dNonce is the virtual offset at which the agent's handoff-nonce echo is
	// observed (well inside the 300s handoff_timeout).
	dNonce = 5 * time.Second
	// dModelDone == dNonce: the D12/SK-018 old-corpus carve-out — ModelDone is
	// scheduled immediately after NonceObserved with ZERO virtual delay, so the
	// old-corpus action goldens match today's clear-immediately behavior.
	dModelDone = dNonce
	// dSessionChanged is the virtual offset of the post-/clear SID flip on the
	// clean-complete path (inside the first 10s clear_settle window, well
	// inside the 150s clear_backstop).
	dSessionChanged = dNonce + 5*time.Second
)

// synthesisTable is THE single reviewed decision table (measurement-design
// §2.4). FROZEN per 00b R6 (T11 decision, 2026-07-14): R6 requires this table
// be frozen against a GREEN differential before the transition scaffold is
// deleted. D13's live old-vs-new scaffold is OBSOLETE — T7 already deleted the
// old blocking Cycler (runCycle), which is entangled with pre-T7 code and not
// cleanly resurrectable; T7 landed with the full ~55-file keeper suite green
// (the regression catch the scaffold existed to provide). The "green
// differential" of record is therefore the L1 golden-vs-baseline corpus test
// (measurement-design §3's named PERMANENT net: internal/keepertest
// TestL1_* — new reactor reproduces all 507 recorded baseline outcomes) plus
// the T13 no-regress metrics (scripts/keeper-metrics.sh, all 9 anchors held).
// Both are green as of T13, so this table is frozen: do not edit the schedules
// without re-greening L1 + the metrics. It is the one choke point where
// "recorded output → plausible input" lives. Each stratum
// maps to the full, flat input-event schedule delivered through the Twin;
// timer firings that the recorded outcome requires are pre-scheduled at the
// exact virtual instant the shell's fake clock would have fired them
// (handoff_timeout at t0+300s; clear_backstop at t_clear+150s), which is
// well-defined because the reactor's arming offsets are deterministic.
//
//	stratum                | schedule                                            | expected terminal
//	-----------------------+-----------------------------------------------------+---------------------------------
//	clean_complete         | GaugeTick@0 → NonceObserved@5s → ModelDone@5s       | cycle_complete,
//	                       |   (idle_marker, D12 zero-delay)                     |   no clear_unconfirmed
//	                       |   → SessionChanged@10s                              |
//	degraded_complete      | GaugeTick@0 → NonceObserved@5s → ModelDone@5s       | cycle_complete WITH
//	                       |   → SessionChanged never →                          |   clear_unconfirmed
//	                       |   TimerFired(clear_backstop)@5s+150s                |
//	abort_handoff_timeout  | GaugeTick@0 → nonce NEVER, no HandoffFreshSeen →    | cycle_aborted{handoff_timeout};
//	                       |   TimerFired(handoff_timeout)@300s                  |   /clear never sent
//	unterminated           | the recorded SR9 hang: nonce+model-done land,       | KNOWN-DIVERGENCE (required FIX):
//	                       |   /clear sent, SessionChanged never →               |   OLD wedged (no terminal); NEW
//	                       |   TimerFired(clear_backstop)@5s+150s                |   MUST terminate within bound →
//	                       |                                                     |   cycle_complete + clear_unconfirmed
var synthesisTable = map[Stratum][]stimStep{
	StratumCleanComplete: {
		{step: stepGaugeTick, at: 0},
		{step: stepNonceObserved, at: dNonce},
		{step: stepModelDone, at: dModelDone},
		{step: stepSessionChanged, at: dSessionChanged},
	},
	StratumDegradedComplete: {
		{step: stepGaugeTick, at: 0},
		{step: stepNonceObserved, at: dNonce},
		{step: stepModelDone, at: dModelDone},
		{step: stepTimerFired, timer: keeper.TimerClearBackstop, at: dModelDone + keeper.DefaultClearConfirmBackstop},
	},
	StratumAbortHandoffTimeout: {
		{step: stepGaugeTick, at: 0},
		{step: stepTimerFired, timer: keeper.TimerHandoffTimeout, at: keeper.DefaultHandoffTimeout},
	},
	// The 1 recorded unterminated cycle (kk-test/cyc-20260610T215853-000004):
	// same stimulus as the degraded path — the NEW reactor's clear_backstop
	// converts the old wedge into a bounded degraded completion (SR9 fix; the
	// T11 differential asserts this divergence exists and is in the fix
	// direction, measurement-design §4).
	StratumUnterminated: {
		{step: stepGaugeTick, at: 0},
		{step: stepNonceObserved, at: dNonce},
		{step: stepModelDone, at: dModelDone},
		{step: stepTimerFired, timer: keeper.TimerClearBackstop, at: dModelDone + keeper.DefaultClearConfirmBackstop},
	},
}

// synthEpoch is the fallback virtual base time for summaries with a missing or
// unparsable started_at.
var synthEpoch = time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)

// Classify maps a recorded cycle summary onto its synthesis stratum. It errors
// on any combination outside the four baseline strata (e.g. an abort reason
// other than handoff_timeout) — an unknown stratum is a corpus finding, never
// silently synthesized.
func Classify(sum CycleSummary) (Stratum, error) {
	switch sum.Outcome {
	case "complete":
		if sum.ClearUnconfirmed {
			return StratumDegradedComplete, nil
		}
		return StratumCleanComplete, nil
	case "aborted":
		if sum.AbortReason != "handoff_timeout" {
			return "", fmt.Errorf("keepertwin: unknown abort reason %q for cycle %s", sum.AbortReason, sum.CKey)
		}
		return StratumAbortHandoffTimeout, nil
	case "unterminated":
		return StratumUnterminated, nil
	default:
		return "", fmt.Errorf("keepertwin: unknown outcome %q for cycle %s", sum.Outcome, sum.CKey)
	}
}

// SynthesizeStimulus turns a recorded cycle summary into the input-event
// schedule that would have produced that recorded outcome (build-time
// synthesis; measurement-design §2.4). Every event carries a virtual-clock At
// stamp offset from the cycle's recorded started_at (fallback: a fixed epoch),
// so replay is fully deterministic.
func SynthesizeStimulus(sum CycleSummary) ([]keeper.Event, error) {
	stratum, err := Classify(sum)
	if err != nil {
		return nil, err
	}

	base := synthEpoch
	if sum.StartedAt != "" {
		if t, perr := time.Parse(time.RFC3339, sum.StartedAt); perr == nil {
			base = t.UTC()
		}
	}
	sid := sum.SessionIDStart
	if sid == "" {
		sid = "twin-sid-" + sum.CycleID
	}
	newSID := "twin-post-" + sum.CycleID

	steps := synthesisTable[stratum]
	events := make([]keeper.Event, 0, len(steps))
	for _, st := range steps {
		at := base.Add(st.at)
		switch st.step {
		case stepGaugeTick:
			events = append(events, keeper.Event{
				Kind:    keeper.EvGaugeTick,
				At:      at,
				CF:      &keeper.CtxFile{Pct: stimulusPct, SessionID: sid},
				Gates:   keeper.GateSnapshot{Managed: true, CrispIdle: true},
				CycleID: sum.CycleID,
			})
		case stepNonceObserved:
			events = append(events, keeper.Event{
				Kind:    keeper.EvNonceObserved,
				At:      at,
				CycleID: sum.CycleID,
			})
		case stepModelDone:
			events = append(events, keeper.Event{
				Kind:      keeper.EvModelDone,
				At:        at,
				CycleID:   sum.CycleID,
				SessionID: sid,
				Source:    OldCorpusModelDoneSource,
			})
		case stepSessionChanged:
			events = append(events, keeper.Event{
				Kind:    keeper.EvSessionChanged,
				At:      at,
				CycleID: sum.CycleID,
				PrevSID: sid,
				NewSID:  newSID,
			})
		case stepTimerFired:
			events = append(events, keeper.Event{
				Kind:    keeper.EvTimerFired,
				At:      at,
				CycleID: sum.CycleID,
				Timer:   st.timer,
			})
		default:
			return nil, fmt.Errorf("keepertwin: unknown synthesis step %q", st.step)
		}
	}
	return events, nil
}

// EncodeStimulus serializes a synthesized schedule as NDJSON — the corpus
// format substrate.Twin replays and keeperCodec decodes (one keeper.Event per
// line, stable field order via encoding/json struct tags).
func EncodeStimulus(events []keeper.Event) ([]byte, error) {
	var buf bytes.Buffer
	for i, ev := range events {
		raw, err := json.Marshal(ev)
		if err != nil {
			return nil, fmt.Errorf("keepertwin: encode stimulus event %d: %w", i, err)
		}
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}
