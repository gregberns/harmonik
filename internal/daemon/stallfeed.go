package daemon

// stallfeed.go — the stall feeder: the production caller of the Layer A stall
// detector, and the producer of the two events that kill a frozen agent.
//
// # What was missing
//
// Two protections read a signal nothing wrote.
//
// The dashboard active-stall panel (readActiveStalls) reads stall_detected.
// sentinel.DetectLayerA could build that payload and had no production caller,
// so the panel was empty for every run, and a wedged run displayed as healthy.
//
// internal/runexec stepDispatchWorking turns EvNoChangeTimeout and
// EvHeartbeatStale into ActKillAgent. Neither event had a producer, so a frozen
// agent held its slot for ever.
//
// Both failed in the same direction — toward looking fine — which is why they
// are repaired together. The feeder below is the one producer for both.
//
// # Why the signal comes from memory and not from the run registry
//
// sentinel.ComputeSnapshot builds the same Snapshot from .harmonik/runs plus a
// replay of events.jsonl. The feeder does NOT use it, for two independent
// reasons.
//
// First, nothing in production writes .harmonik/runs today, so that snapshot
// reports zero active runs and a detector built on it would be silent — the
// exact failure being repaired. A separate repair is in flight for that writer;
// when it lands, this feeder still does not need it, because the in-memory run
// registry is the authority for what is running right now and the on-disk one
// is the authority for what survived a restart.
//
// Second, ComputeSnapshot refreshes last-event-age on ANY run-scoped event, and
// while an agent is running its launch beats every five minutes whether or not
// that agent is doing anything. A heartbeat-gap threshold under five minutes
// would alarm on every healthy run, and one above it can only fire once the beat
// itself has stopped. So heartbeat-gap here means "even the beat for this run
// stopped", which a working agent cannot produce. The watcher's own per-run
// bookkeeping already carries that time, and it carries it with the watcher's
// own alarms filtered out — see lastLivenessAt in stalewatch.go.
//
// # What each signature can and cannot catch
//
//   - heartbeat_gap — the run went quiet, its own beat included. That is a
//     wedged run, not a thinking agent: a thinking agent still beats.
//   - review_stall — the verdict that ENDS a run fired and the merge/close
//     spine never finished. That spine is seconds-to-minutes of work, so a wide
//     window is still tight. A verdict that sends the run back to the
//     implementer is a different thing wearing the same event name, and it is
//     not measured: the re-launch clears it (advanceStallPhase in
//     stalewatch.go). Without that, every reworking run reads as a verdict that
//     never finalized, ten minutes after the reviewer asked for changes.
//   - run_age — the backstop, and the one that catches a frozen agent. A run
//     past its ceiling with no terminal event is over budget by definition.
//     Beads that legitimately run longer carry a run_max_age label, which is
//     the same escape hatch the other three watchdogs in stalewatch.go use.
//
// Bead: hk-hsp9e.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// The three Layer A thresholds the daemon compiles in. Every StaleWatcherConfig
// field they back is settable, and the tests set them, so none of these needs to
// be a var.
const (
	// stallRunSilenceDefault is the quiet window that fires heartbeat_gap.
	//
	// While an agent is running, its launch beats every
	// handler.HeartbeatInterval (5 min), so this is four missed beats and a
	// working agent cannot reach it. The beat stops at agent exit, so a run
	// still in its post-agent phases — merge, gate, close — has no beat either,
	// and this window is what bounds how long one of those may take.
	stallRunSilenceDefault = 22 * time.Minute

	// stallReviewFinalizeDefault is the window allowed between the verdict that
	// ends a run and the run's own terminal event. The merge/close spine after a
	// verdict is minutes of git and ledger work, so this is generous by an order
	// of magnitude. A verdict that sends the run BACK to the implementer is not
	// measured at all: the re-launch clears it (advanceStallPhase).
	stallReviewFinalizeDefault = 10 * time.Minute

	// stallRunMaxAgeDefault is the absolute ceiling on a non-terminal run.
	//
	// Four hours is well past the longest legitimate run this daemon dispatches
	// (the reviewer's own hard ceiling is one hour) and well short of the
	// overnight window in which an unattended wedge does the most damage. A bead
	// that genuinely needs longer carries a run_max_age label.
	stallRunMaxAgeDefault = 4 * time.Hour
)

// beadRunMaxAge parses a "run_max_age=<seconds>" or "run_max_age:<seconds>"
// label and returns the corresponding duration. Returns defaultAge when no such
// label is present or the value is not a positive integer.
//
// This is the false-positive escape hatch for the run-age backstop, and it is
// the one signature that can fire on a run that is working. Put the label on a
// bead whose work legitimately outlives the ceiling.
func beadRunMaxAge(labels []string, defaultAge time.Duration) time.Duration {
	for _, l := range labels {
		var val string
		switch {
		case strings.HasPrefix(l, "run_max_age="):
			val = strings.TrimPrefix(l, "run_max_age=")
		case strings.HasPrefix(l, "run_max_age:"):
			val = strings.TrimPrefix(l, "run_max_age:")
		default:
			continue
		}
		secs, err := strconv.ParseInt(val, 10, 64)
		if err != nil || secs <= 0 {
			return defaultAge
		}
		return time.Duration(secs) * time.Second
	}
	return defaultAge
}

// stallSignatureEvent maps a detected signature onto the dispatch event that
// reaches the kill arm in stepDispatchWorking.
//
// heartbeat_gap is a liveness signal, so it maps to heartbeat_stale. The other
// two say the run stopped moving forward, which is what no_change_timeout
// means. Any future signature MUST map to one of these two: the reactor treats
// every other kind as an explicit no-op during Working, so an unmapped
// signature would detect a wedge and leave the agent running.
func stallSignatureEvent(sig core.StallSignature) (runexec.EventKind, bool) {
	switch sig {
	case core.StallSignatureHeartbeatGap:
		return runexec.EvHeartbeatStale, true
	case core.StallSignatureReviewStall, core.StallSignatureRunAge:
		return runexec.EvNoChangeTimeout, true
	default:
		return "", false
	}
}

// stallPass runs the Layer A detector over every registered run and acts on
// each new hit: it emits stall_detected so the run appears on the dashboard's
// active-stall panel, and it posts the matching event to the run's dispatch
// feed so the machine kills the agent.
//
// Detection runs per run rather than over one whole-fleet snapshot, because the
// run-age ceiling is per-bead: one bead may legitimately run longer than the
// fleet default, and a single shared config could not express that without
// raising the ceiling for every other run at the same time.
func (w *StaleWatcher) stallPass(ctx context.Context, now time.Time, handles map[core.RunID]*RunHandle) {
	for runID, handle := range handles {
		w.stallPassRun(ctx, runID, handle, now)
	}
}

// stallPassRun evaluates one run.
func (w *StaleWatcher) stallPassRun(ctx context.Context, runID core.RunID, handle *RunHandle, now time.Time) {
	snap, cfg, ok := w.stallInputs(runID, handle, now)
	if !ok {
		return
	}

	hits, err := sentinel.DetectLayerA(snap, cfg)
	if err != nil {
		// The config is built from compiled defaults, so this is unreachable
		// short of a code change. Say so rather than going quiet: a silent
		// detector is the defect this file exists to remove.
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: run %s: Layer A config rejected: %v\n", runID, err)
		return
	}

	for _, hit := range hits {
		if !w.claimStall(runID, hit.Signature) {
			continue
		}
		w.reportStall(ctx, runID, hit)
		w.killStalledRun(runID, hit, now)
	}
}

// stallInputs builds the one-run Snapshot and the per-bead thresholds the
// detector needs.
//
// Both come off the RunHandle the dispatcher registered, which sets StartedAt
// and copies the bead's labels — so the age ceiling and its per-bead override
// both reach a live run. ok is false for a handle with no start time, which is
// a handle no dispatch built: there is no instant to measure any of the three
// windows from, and guessing one would report an age that is not real.
func (w *StaleWatcher) stallInputs(
	runID core.RunID, handle *RunHandle, now time.Time,
) (sentinel.Snapshot, sentinel.LayerAConfig, bool) {
	if handle.StartedAt.IsZero() {
		return sentinel.Snapshot{}, sentinel.LayerAConfig{}, false
	}

	// The liveness clock defaults to the run's start: a run that has shown no
	// sign of life since it was registered has been silent for its whole life,
	// which is exactly what the heartbeat-gap window measures. It reads
	// lastLivenessAt and NOT lastEventAt, so the watcher's own alarms about this
	// run do not count as the run being alive.
	lastEventAt := handle.StartedAt
	phase := sentinel.RunPhaseStarted
	var verdictAt time.Time

	w.mu.Lock()
	if st, ok := w.states[runID]; ok {
		if st.lastLivenessAt.After(lastEventAt) {
			lastEventAt = st.lastLivenessAt
		}
		phase = st.phase
		verdictAt = st.verdictAt
	}
	w.mu.Unlock()

	snap := sentinel.Snapshot{
		Now: now,
		Runs: map[string]sentinel.RunSignal{
			runID.String(): {
				RunID:        runID.String(),
				BeadID:       string(handle.BeadID),
				LaneName:     handle.QueueName,
				StartedAt:    handle.StartedAt,
				LastEventAt:  lastEventAt,
				LastEventAge: now.Sub(lastEventAt),
				Phase:        phase,
				VerdictAt:    verdictAt,
			},
		},
	}
	cfg := sentinel.LayerAConfig{
		RunSilenceStall:     w.cfg.RunSilenceStall,
		ReviewFinalizeStall: w.cfg.ReviewFinalizeStall,
		RunMaxAge:           beadRunMaxAge(handle.Labels, w.cfg.RunMaxAge),
	}
	return snap, cfg, true
}

// claimStall records that (runID, sig) has been reported and returns true only
// for the first claim SINCE THE RUN LAST LAUNCHED. The detector re-derives every standing hit on every
// scan, so without this one wedged run posts a stall every scan interval for as
// long as it stays wedged.
//
// The set is cleared when the run launches again (advanceStallPhase in
// stalewatch.go), because a graph run reaches the launch once per node and a
// set carried across nodes would mean that once one node was stall-killed, no
// later node of that run could ever be.
func (w *StaleWatcher) claimStall(runID core.RunID, sig core.StallSignature) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	st, ok := w.states[runID]
	if !ok {
		st = &runStaleState{nextEmitAfter: w.cfg.StaleAfter}
		w.states[runID] = st
	}
	if st.stallEmitted[sig] {
		return false
	}
	if st.stallEmitted == nil {
		st.stallEmitted = make(map[core.StallSignature]bool, 3)
	}
	st.stallEmitted[sig] = true
	return true
}

// reportStall emits stall_detected for one hit. The event is run-stamped
// because readActiveStalls joins it to the live-run set; an unstamped emit
// writes a record the panel silently drops.
func (w *StaleWatcher) reportStall(ctx context.Context, runID core.RunID, hit sentinel.StallHit) {
	pl := hit.StallDetectedPayload()
	pl.RunID = runID.String()
	if pl.BeadID == "" {
		// The panel drops any payload whose Valid() is false, and it does it
		// without a word. A run with no bead is not a thing the daemon can
		// dispatch, so say what happened rather than emitting a dropped record.
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: run %s stalled (%s) but names no bead; not reported\n",
			runID, hit.Signature)
		return
	}
	b, err := json.Marshal(pl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: marshal stall_detected for run %s: %v\n", runID, err)
		return
	}
	if emitErr := w.cfg.Emitter.EmitWithRunID(ctx, runID, core.EventTypeStallDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: emit stall_detected for run %s: %v\n", runID, emitErr)
		return
	}
	fmt.Fprintf(os.Stderr, "daemon: stallfeed: bead %s run %s stalled (%s, %s)\n",
		pl.BeadID, runID, hit.Signature, time.Duration(pl.ElapsedMs)*time.Millisecond)
}

// killStalledRun posts the stall to the run's dispatch machine, which turns it
// into the agent kill.
//
// A run whose feed will not take it is reported and otherwise left alone. There
// is deliberately no second recovery path here: the run_stale kill-consumer
// backstop and the force-reap watchdog in stalewatch.go already cancel a wedged
// run, and a third canceller racing those two would be a new way to abort a run
// rather than a new way to kill a frozen agent.
func (w *StaleWatcher) killStalledRun(runID core.RunID, hit sentinel.StallHit, now time.Time) {
	if w.cfg.StallFeed == nil {
		return
	}
	kind, ok := stallSignatureEvent(hit.Signature)
	if !ok {
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: run %s: signature %q has no dispatch event; "+
			"the stall is reported but the agent is NOT killed\n", runID, hit.Signature)
		return
	}
	if !w.cfg.StallFeed.Post(runID.String(), runexec.Event{Kind: kind, At: now}) {
		fmt.Fprintf(os.Stderr, "daemon: stallfeed: run %s: no dispatch feed took the stall; "+
			"it is reported but the agent is NOT killed here\n", runID)
	}
}
