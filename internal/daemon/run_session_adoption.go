package daemon

// run_session_adoption.go — the run-session registry, both halves.
//
// setUpRunSession is the WRITE half: a run takes a tmux session of its own and
// records the session's name before it launches anything into it.
// adoptDeadRunSessions is one READ half, at daemon startup, for the sessions
// that have already exited; adoptLiveRunSession (scheduler.go) is the other, for
// the ones still running.
//
// The two halves live in one file on purpose. The write used to live six hundred
// lines away in the run path and was deleted with the code around it; the readers
// kept compiling, kept returning the empty set, and nothing failed for three days.
// A reader whose writer is on the next screen is a reader whose writer is hard to
// delete by accident.

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/runlease"
	"github.com/gregberns/harmonik/internal/runloop"
)

// setUpRunSession gives a local run a tmux session of its own and writes the
// record that names it, then reports whether the run got one.
//
// Two things have to be true for a run to survive the daemon that started it.
// The agent must be in a session the daemon does not own, or it dies with the
// daemon. And .harmonik/runs/ must hold a record naming that session, or the
// next boot has a live agent it cannot find, cannot adopt and cannot attribute
// to a bead — the bead is reset and re-dispatched under the agent still working
// it.
//
// The record is written BEFORE the caller launches anything. A record written
// after the spawn is not written at all for a daemon killed in between, and that
// is the exact crash this registry exists for.
//
// On any refusal the run keeps the daemon's shared session and this returns
// false: no record, no survival, and the ordinary teardown. That is the safe
// direction. A run in its own session with no record on disk is worse than a run
// that dies with the daemon, because nothing on the next boot knows it is there.
func setUpRunSession(
	env *runloop.RunEnv,
	rp runloop.RunPorts,
	handles runloop.SharedHandles,
	runScope *runlease.Scope,
	remote bool,
	runID core.RunID,
	beadID core.BeadID,
) bool {
	// A remote run's agent lives on the worker, in the worker's session. Box A's
	// tmux server has nothing to keep alive for it.
	if remote || env.ProjectDir == "" {
		return false
	}
	ts, isTmux := handles.Substrate.(*tmuxSubstrate)
	if !isTmux {
		return false
	}
	// An adapter that cannot create a session cannot give this run one. Test
	// stubs and the $TMUX-reuse mode land here and keep the shared-session path.
	if _, canCreateSession := ts.adapter.(sessionCreator); !canCreateSession {
		return false
	}
	sessName, nameErr := ts.runSessionName(runID.String())
	if nameErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: setUpRunSession: name the tmux session for run %s: %v (keeping the daemon's session)\n",
			runID.String(), nameErr)
		return false
	}

	queueIDStr := ""
	if env.QueueID != nil {
		queueIDStr = *env.QueueID
	}
	queueGroupIdx := -1
	if env.QueueGroupIndex != nil {
		queueGroupIdx = *env.QueueGroupIndex
	}
	if writeErr := runpkg.Write(env.ProjectDir, runpkg.Record{
		SchemaVersion: 1,
		RunID:         runID.String(),
		BeadID:        string(beadID),
		QueueName:     env.QueueName,
		QueueID:       queueIDStr,
		GroupIndex:    queueGroupIdx,
		ItemIndex:     env.QueueItemIndex,
		SessionName:   sessName,
		StartedAt:     rp.Clock.Now(),
	}); writeErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: setUpRunSession: write the run record for %s: %v (keeping the daemon's session)\n",
			runID.String(), writeErr)
		return false
	}

	// The record exists, so the run holds it. A surviving run leaves it standing —
	// it is how the next boot finds this agent's session by name — and every other
	// ending gives it back. The hold is here rather than at the top of the run
	// because a record that was never written is not a resource, and this is the
	// only place that knows whether it was.
	runScope.Hold(runlease.RunRecord, func() error {
		// An absent record is the already-cleaned case, not a failure. Any other
		// failure is retried by the next boot's adoption sweep, so it is reported
		// and not acted on — the same handling adoptDeadRunSessions gives this call.
		if remErr := runpkg.Remove(env.ProjectDir, runID.String()); remErr != nil &&
			!errors.Is(remErr, runpkg.ErrNotFound) {
			return remErr
		}
		return nil
	})

	// Last, because every launch this run makes reads it: the run is now on the
	// independent-session path, and the record on disk names the session it will
	// spawn into.
	env.RunSessionID = runID.String()
	return true
}

// adoptDeadRunSessions is called during daemon.Start, after the orphan session
// sweep and before LoadQueueAtStartup (QM-002a). It:
//
//  1. Lists all records in .harmonik/runs/.
//  2. For each record whose tmux session no longer exists (session dead = Claude exited):
//     a. Calls beadResetter.ResetBead to transition the bead from in_progress → open.
//     b. Removes the registry entry.
//  3. Skips records with live sessions — those are handled by adoptLiveRunSession
//     goroutines started from runWorkLoop.
//
// QM-002a (LoadQueueAtStartup) then sees the bead as open and reverts the queue
// item from dispatched → pending, making it eligible for re-dispatch.
//
// All errors are non-fatal: a failed reset is logged and the registry entry is
// left in place so the next restart retries.
func adoptDeadRunSessions(
	ctx context.Context,
	projectDir string,
	projectHash core.ProjectHash,
	daemonStartNS int64,
	intentLogDir string,
	adapter ltmux.Adapter,
	resetter runBeadResetter,
) {
	if projectDir == "" {
		return
	}
	recs, err := runpkg.List(projectDir)
	if err != nil || len(recs) == 0 {
		return
	}

	// Build live-session set (best-effort; nil adapter = no sessions known).
	liveSessions := make(map[string]struct{})
	if adapter != nil {
		if sessions, listErr := adapter.ListSessions(ctx); listErr == nil {
			for _, s := range sessions {
				liveSessions[s] = struct{}{}
			}
		}
	}

	for _, rec := range recs {
		if rec.SessionName == "" {
			// No session name recorded — treat as dead (can't verify liveness).
		} else if _, alive := liveSessions[rec.SessionName]; alive {
			// Session still live; runWorkLoop's adoptLiveRunSession handles it.
			continue
		}

		// Session is gone. Reset the bead so QM-002a can revert the queue item.
		if resetter != nil && rec.BeadID != "" {
			if resetErr := resetter.ResetBead(
				ctx,
				intentLogDir,
				brcli.TimeoutConfig{},
				core.BeadID(rec.BeadID),
				projectHash,
				daemonStartNS,
			); resetErr != nil {
				fmt.Fprintf(os.Stderr,
					"daemon: adoptDeadRunSessions: ResetBead %s (run %s): %v — bead may stay stuck; will retry on next boot\n",
					rec.BeadID, rec.RunID, resetErr)
				continue
			}
		}

		// Remove the registry entry; if this fails it's non-fatal (next boot retries).
		if removeErr := runpkg.Remove(projectDir, rec.RunID); removeErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: adoptDeadRunSessions: Remove registry entry %s: %v\n",
				rec.RunID, removeErr)
		}
	}
}

// runBeadResetter is the subset of lifecycle.BeadResetter used by the
// run-session adoption path; mirrors the interface in orphansweepbeads.go.
type runBeadResetter interface {
	ResetBead(
		ctx context.Context,
		intentLogDir string,
		cfg brcli.TimeoutConfig,
		beadID core.BeadID,
		projectHash core.ProjectHash,
		daemonStartNS int64,
	) error
}
