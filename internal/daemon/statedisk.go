package daemon

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

	snap.Queues = diskQueues(ctx, projectDir)

	var sessErr error
	snap.Sessions, sessErr = diskSessions(ctx, projectDir, now)
	if sessErr != nil {
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, "session gather error: "+sessErr.Error())
	}

	snap.WorkAxes = diskWorkAxes(projectDir)
	if snap.WorkAxes != nil && snap.WorkAxes.Unsure {
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, snap.WorkAxes.UnsureReasons...)
	}

	snap.Runs = diskRuns(projectDir)

	snap.ActivityLabel = RollUpLabel(snap.Runs, snap.Queues, snap.WorkAxes, snap.Sessions, projectDir)
	if snap.ActivityLabel == ActivityInactive {
		snap.ActivityLabel = ActivityWaiting
	}

	return snap
}

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

func diskSessions(ctx context.Context, projectDir string, now time.Time) ([]StateSession, error) {
	crewRecords, err := crew.List(projectDir)
	if err != nil {
		return nil, fmt.Errorf("crew.List: %w", err)
	}

	ph := lifecycle.ComputeProjectHash(projectDir)
	lb := &LiveStateBuilder{projectDir: projectDir, projectHash: ph}

	sleepSIDs := scanSleepMarkerSIDs(projectDir)
	sessions := make([]StateSession, 0, len(crewRecords))

	for _, cr := range crewRecords {
		alive := tmuxHasSession(ctx, lifecycle.TmuxSessionName(ph, cr.Name))
		liveSID := liveSessionID(projectDir, cr.Name)

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

	if !hasCaptainRecord(crewRecords) {
		if _, _, err := keeper.ReadCtxFile(projectDir, captainAgentName); err == nil {
			alive := tmuxHasSession(ctx, lifecycle.TmuxSessionName(ph, captainAgentName))
			liveSID := liveSessionID(projectDir, captainAgentName)
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

func diskWorkAxes(projectDir string) *FleetFacts {
	facts := &FleetFacts{GatheredAt: time.Now()}
	facts.markUnsure("disk-only: br ready axis unavailable without daemon")

	archives, err := diskFailedArchives(projectDir)
	if err != nil {
		facts.markUnsure("failed-archive scan error: " + err.Error())
	} else {
		facts.Queued.FailedArchives = archives
	}

	wtDir := filepath.Join(projectDir, ".harmonik", "worktrees")
	paths := listWorktreePaths(wtDir)
	facts.Runs.LiveWorktrees = len(paths)
	facts.Runs.WorktreePaths = paths

	return facts
}

func diskFailedArchives(projectDir string) ([]string, error) {
	matches, err := queue.ListFailedArchives(projectDir)
	if err != nil {
		return nil, fmt.Errorf("disk failed-archive scan: %w", err)
	}
	return matches, nil
}

func liveSessionID(projectDir, agent string) string {
	sid, _, err := keeper.ReadSessionIDFile(projectDir, agent)
	if err != nil {
		return ""
	}
	return sid
}
