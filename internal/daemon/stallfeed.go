package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runexec"
	"github.com/gregberns/harmonik/internal/sentinel"
)

const (
	stallRunSilenceDefault = 22 * time.Minute

	stallReviewFinalizeDefault = 10 * time.Minute

	stallRunMaxAgeDefault = 4 * time.Hour
)

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

func (w *StaleWatcher) stallPass(ctx context.Context, now time.Time, handles map[core.RunID]*runregistry.RunHandle) {
	for runID, handle := range handles {
		w.stallPassRun(ctx, runID, handle, now)
	}
}

func (w *StaleWatcher) stallPassRun(ctx context.Context, runID core.RunID, handle *runregistry.RunHandle, now time.Time) {
	snap, cfg, ok := w.stallInputs(runID, handle, now)
	if !ok {
		return
	}

	hits, err := sentinel.DetectLayerA(snap, cfg)
	if err != nil {
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

func (w *StaleWatcher) stallInputs(
	runID core.RunID, handle *runregistry.RunHandle, now time.Time,
) (sentinel.Snapshot, sentinel.LayerAConfig, bool) {
	if handle.StartedAt.IsZero() {
		return sentinel.Snapshot{}, sentinel.LayerAConfig{}, false
	}

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

func (w *StaleWatcher) reportStall(ctx context.Context, runID core.RunID, hit sentinel.StallHit) {
	pl := hit.StallDetectedPayload()
	pl.RunID = runID.String()
	if pl.BeadID == "" {
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
