package daemon

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

func setUpRunSession(
	env *runloop.RunEnv,
	rp runloop.RunPorts,
	handles runloop.SharedHandles,
	runScope *runlease.Scope,
	hasRunner bool,
	runID core.RunID,
	beadID core.BeadID,
) bool {
	if hasRunner || env.ProjectDir == "" {
		return false
	}
	ts, isTmux := handles.Substrate.(*tmuxSubstrate)
	if !isTmux {
		return false
	}
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

	runScope.Hold(runlease.RunRecord, func() error {
		if remErr := runpkg.Remove(env.ProjectDir, runID.String()); remErr != nil &&
			!errors.Is(remErr, runpkg.ErrNotFound) {
			return remErr
		}
		return nil
	})

	env.RunSessionID = runID.String()
	return true
}

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
	snapshot, err := runpkg.ScanRegistry(projectDir)
	if err != nil || len(snapshot.Legacy) == 0 {
		return
	}

	liveSessions := make(map[string]struct{})
	if adapter != nil {
		if sessions, listErr := adapter.ListSessions(ctx); listErr == nil {
			for _, s := range sessions {
				liveSessions[s] = struct{}{}
			}
		}
	}

	for _, rec := range snapshot.Legacy {
		if rec.SessionName == "" {
		} else if _, alive := liveSessions[rec.SessionName]; alive {
			continue
		}

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

		if removeErr := runpkg.Remove(projectDir, rec.RunID); removeErr != nil {
			fmt.Fprintf(os.Stderr,
				"daemon: adoptDeadRunSessions: Remove registry entry %s: %v\n",
				rec.RunID, removeErr)
		}
	}
}

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
