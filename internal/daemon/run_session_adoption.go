// Package-level helpers for hk-o85ye: adopt bead-run sessions that survived a
// prior daemon SIGKILL. The daemon startup path (adoptDeadRunSessions) handles
// sessions that have already exited; the runWorkLoop path (adoptLiveRunSession,
// defined in workloop.go) handles sessions still running.
package daemon

import (
	"context"
	"fmt"
	"os"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

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
