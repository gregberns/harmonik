package daemon

// statedisk.go — disk-only StateSnapshot builder for daemon-down fallback.
//
// BuildDiskSnapshot assembles a best-effort StateSnapshot from disk when the
// daemon is not running.  Per SS-001a / SS-006 a daemon-down snapshot MUST
// set read_quality.unsure = true and MUST NOT emit activity_label INACTIVE.
//
// Spec ref: specs/system-state.md §4.1 SS-001a, §4.6 SS-006.
// Bead ref: hk-gv04 (P2-a: harmonik state aggregator command).

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
)

// BuildDiskSnapshot assembles a StateSnapshot from disk sources only.
// It is used when the daemon socket is absent or refused.
// The returned snapshot always has ReadQuality.Unsure=true (SS-001a).
func BuildDiskSnapshot(ctx context.Context, projectDir string) StateSnapshot {
	now := time.Now().UTC()
	snap := StateSnapshot{
		SchemaVersion: 1,
		CapturedAt:    now.Format(time.RFC3339),
		Daemon: StateDaemon{
			Up:     false,
			Socket: lifecycle.SocketPath(projectDir),
		},
		ReadQuality: ReadQuality{
			Ok:      false,
			Unsure:  true,
			Reasons: []string{"daemon is not running; disk-only read (SS-001a)"},
		},
	}

	// Queues from disk.
	snap.Queues = diskQueues(ctx, projectDir)

	// Sessions from crew registry + keeper gauges.
	var sessErr error
	snap.Sessions, sessErr = diskSessions(ctx, projectDir, now)
	if sessErr != nil {
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, "session gather error: "+sessErr.Error())
	}

	// Work axes from the drain detector (disk-only path: no br-ready, best-effort).
	// We can call GatherDrainFacts with a nil-wired DrainDetector since we have
	// no RunRegistry / QueueStore in daemon-down mode.  Build a minimal facts
	// bundle from the disk-queue data and the worktrees scan.
	snap.WorkAxes = diskWorkAxes(projectDir)
	if snap.WorkAxes != nil && snap.WorkAxes.Unsure {
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, snap.WorkAxes.UnsureReasons...)
	}

	// Runs from live worktrees directory (disk only — no RunRegistry).
	snap.Runs = diskRuns(projectDir)

	// Activity label fold (disk-based).
	snap.ActivityLabel = RollUpLabel(snap.Runs, snap.Queues, snap.WorkAxes, snap.Sessions, projectDir)
	// Per SS-001a / SS-006: daemon-down MUST NOT emit INACTIVE.
	if snap.ActivityLabel == ActivityInactive {
		snap.ActivityLabel = ActivityWaiting
	}

	return snap
}

// diskQueues loads queue state from disk (.harmonik/queues/*.json).
func diskQueues(ctx context.Context, projectDir string) []StateQueue {
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return nil
	}
	result := make([]StateQueue, 0, len(names))
	for _, name := range names {
		q, loadErr := queue.Load(ctx, projectDir, name)
		if loadErr != nil {
			continue
		}
		totalItems := 0
		eligibleCount := 0
		for gi := range q.Groups {
			g := &q.Groups[gi]
			for range g.Items {
				totalItems++
			}
			if g.Status == queue.GroupStatusActive {
				eligibleCount += len(queue.EligibleItems(g))
			}
		}
		// In disk mode we don't have RunRegistry, so ActiveCount is 0.
		// EffectiveWorkerCap defaults to 1 (global cap unknown).
		effectiveCap := queue.DefaultWorkers(q.Workers, 1)
		eligible := q.Status == queue.QueueStatusActive && eligibleCount > 0

		sq := StateQueue{
			Name:               name,
			Status:             string(q.Status),
			Source:             "disk",
			ItemCount:          totalItems,
			ActiveCount:        0,
			EffectiveWorkerCap: effectiveCap,
			EligibleNow:        eligible,
		}
		switch q.Status {
		case queue.QueueStatusPausedByFailure:
			sq.PauseReason = "paused-by-failure"
		case queue.QueueStatusPausedByDrain:
			sq.PauseReason = "paused-by-drain"
		case queue.QueueStatusPausedByBudget:
			sq.PauseReason = "paused-by-budget"
		}
		result = append(result, sq)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

// diskSessions reads crew registry entries and keeper gauge files.
func diskSessions(_ context.Context, projectDir string, now time.Time) ([]StateSession, error) {
	crewRecords, err := crew.List(projectDir)
	if err != nil {
		return nil, fmt.Errorf("crew.List: %w", err)
	}

	ph := lifecycle.ComputeProjectHash(projectDir)
	// Reuse LiveStateBuilder for cognition building (disk path: no runs/queues).
	lb := &LiveStateBuilder{projectDir: projectDir, projectHash: ph}

	sleepSIDs := scanSleepMarkerSIDs(projectDir)
	sessions := make([]StateSession, 0, len(crewRecords))

	for _, cr := range crewRecords {
		alive := tmuxHasSession(lifecycle.TmuxSessionName(ph, cr.Name))
		liveSID, _, _ := keeper.ReadSessionIDFile(projectDir, cr.Name)

		sleepMarker := sleepSIDs[strings.ToLower(liveSID)] ||
			(cr.SessionID != "" && sleepSIDs[strings.ToLower(cr.SessionID)])

		presenceSrc := "registry"
		if alive {
			presenceSrc = "both"
		}

		sessionType := "crew"
		if cr.Name == captainAgentName {
			sessionType = "captain"
		}

		sess := StateSession{
			Agent:          cr.Name,
			SessionType:    sessionType,
			Alive:          alive,
			SleepMarker:    sleepMarker,
			AtRest:         alive && sleepMarker,
			PresenceSource: presenceSrc,
		}
		if alive {
			sess.Cognition = lb.buildCognition(cr.Name, liveSID, cr.SessionID, now)
		}
		sessions = append(sessions, sess)
	}

	// Captain (if not in crew registry).
	if !hasCaptainRecord(crewRecords) {
		if _, _, err := keeper.ReadCtxFile(projectDir, captainAgentName); err == nil {
			alive := tmuxHasSession(lifecycle.TmuxSessionName(ph, captainAgentName))
			liveSID, _, _ := keeper.ReadSessionIDFile(projectDir, captainAgentName)
			sleepMarker := sleepSIDs[strings.ToLower(liveSID)]
			sess := StateSession{
				Agent:          captainAgentName,
				SessionType:    "captain",
				Alive:          alive,
				SleepMarker:    sleepMarker,
				AtRest:         alive && sleepMarker,
				PresenceSource: "tmux",
			}
			if alive {
				sess.Cognition = lb.buildCognition(captainAgentName, liveSID, "", now)
			}
			sessions = append(sessions, sess)
		}
	}

	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Agent < sessions[j].Agent })
	return sessions, nil
}

// diskRuns derives in-flight runs from the live-worktrees directory (disk only).
func diskRuns(projectDir string) []StateRun {
	wtDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	paths := listWorktreePaths(wtDir)
	runs := make([]StateRun, 0, len(paths))
	for _, p := range paths {
		runs = append(runs, StateRun{
			RunID:        filepath.Base(p),
			WorktreePath: p,
			Source:       "disk",
		})
	}
	return runs
}

// listWorktreePaths returns a sorted slice of paths under .harmonik/worktrees.
func listWorktreePaths(dir string) []string {
	names := readDirNames(dir)
	if len(names) == 0 {
		return nil
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	sort.Strings(paths)
	return paths
}

// diskWorkAxes builds a minimal FleetFacts from disk (no br-ready — that
// requires a running br process).  Only the queue-based and worktree-based
// axes are populated; br-ready requires the br binary.
func diskWorkAxes(projectDir string) *FleetFacts {
	facts := &FleetFacts{GatheredAt: time.Now()}
	facts.markUnsure("disk-only: br ready axis unavailable without daemon")

	// Failed archives scan (defense #3).
	archives, err := diskFailedArchives(projectDir)
	if err != nil {
		facts.markUnsure("failed-archive scan error: " + err.Error())
	} else {
		facts.Queued.FailedArchives = archives
	}

	// Live worktrees (defense #4).
	wtDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	paths := listWorktreePaths(wtDir)
	facts.Runs.LiveWorktrees = len(paths)
	facts.Runs.WorktreePaths = paths

	return facts
}

// diskFailedArchives globs .harmonik/queues/*.json.failed-* directly.
func diskFailedArchives(projectDir string) ([]string, error) {
	pattern := filepath.Join(projectDir, ".harmonik", "queues", "*.json.failed-*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return matches, nil
}
