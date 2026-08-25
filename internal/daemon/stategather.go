package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/runregistry"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/policy"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

const fallbackWindowSize int64 = 200_000

// LiveStateBuilder gathers a StateSnapshot from in-daemon memory + disk.
type LiveStateBuilder struct {
	runs        *runregistry.RunRegistry
	queues      *queuewiring.QueueStore
	drain       *DrainDetector
	conc        *ConcurrencyController
	globalCap   int // fallback when conc is nil
	projectDir  string
	projectHash core.ProjectHash
	// kconfig carries the project keeper config; zero value = not configured.
	// Used by buildCognition to populate TooBigSignal and ContextStaticSignal
	// thresholds (SS-012). Fields with zero value are treated as "not configured"
	// and their dependent signal fields are emitted as null (dark-when-unset).
	kconfig projectconfig.KeeperConfig
}

// NewLiveStateBuilder constructs a LiveStateBuilder. drain may be nil; when
// nil the work_axes field will be absent and read_quality.unsure = true.
// conc may be nil; when nil globalCap is used as the effective ceiling.
// kconfig carries the parsed keeper: block from .harmonik/config.yaml; the
// zero value (KeeperConfig{}) is safe and means all thresholds are unset.
func NewLiveStateBuilder(
	runs *runregistry.RunRegistry,
	queues *queuewiring.QueueStore,
	drain *DrainDetector,
	conc *ConcurrencyController,
	globalCap int,
	projectDir string,
	kconfig projectconfig.KeeperConfig,
) *LiveStateBuilder {
	return &LiveStateBuilder{
		runs:        runs,
		queues:      queues,
		drain:       drain,
		conc:        conc,
		globalCap:   globalCap,
		projectDir:  projectDir,
		projectHash: lifecycle.ComputeProjectHash(projectDir),
		kconfig:     kconfig,
	}
}

// Build assembles and returns a live StateSnapshot.
func (b *LiveStateBuilder) Build(ctx context.Context) StateSnapshot {
	now := time.Now().UTC()
	snap := StateSnapshot{
		SchemaVersion: 1,
		CapturedAt:    now.Format(time.RFC3339),
		Daemon: StateDaemon{
			Up:     true,
			Socket: lifecycle.SocketPath(b.projectDir),
		},
	}

	if pid, err := readPidFromFile(b.projectDir); err == nil {
		snap.Daemon.Pid = pid
	}

	snap.Runs = b.buildRuns()

	globalCap := b.globalCap
	if b.conc != nil {
		globalCap = b.conc.Get()
	}
	snap.Queues = b.buildQueues(globalCap)

	var sessErr error
	snap.Sessions, sessErr = b.buildSessions(ctx, now)
	if sessErr != nil {
		snap.ReadQuality.Unsure = true
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, "session gather error: "+sessErr.Error())
	}

	if b.drain != nil {
		facts, err := b.drain.GatherDrainFacts(ctx)
		if err != nil {
			snap.ReadQuality.Unsure = true
			snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, "GatherDrainFacts error: "+err.Error())
		}
		if facts != nil {
			snap.WorkAxes = facts
			if facts.Unsure {
				snap.ReadQuality.Unsure = true
				snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, facts.UnsureReasons...)
			}
		}
	} else {
		snap.ReadQuality.Unsure = true
		snap.ReadQuality.Reasons = append(snap.ReadQuality.Reasons, "drain detector not wired")
	}

	snap.ActivityLabel = RollUpLabel(snap.Runs, snap.Queues, snap.WorkAxes, snap.Sessions, b.projectDir)
	if snap.ReadQuality.Unsure && snap.ActivityLabel == ActivityInactive {
		snap.ActivityLabel = ActivityWaiting
	}

	snap.ReadQuality.Ok = !snap.ReadQuality.Unsure
	return snap
}

func (b *LiveStateBuilder) buildRuns() []StateRun {
	if b.runs == nil {
		return nil
	}
	keyed := b.runs.SnapshotWithKeys()
	runs := make([]StateRun, 0, len(keyed))
	for runID, h := range keyed {
		sr := StateRun{
			RunID:          runID.String(),
			BeadID:         string(h.BeadID),
			QueueName:      h.QueueName,
			WorktreePath:   h.WorktreePath,
			StartedAt:      formatRFC3339(h.StartedAt),
			OwningEpicID:   h.OwningEpicID,
			OwningAssignee: h.OwningEpicAssignee,
			Source:         "live",
		}
		if m := h.GetMachine(); m != nil {
			sr.LifecycleState = m.Current().String()
		}
		runs = append(runs, sr)
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].RunID < runs[j].RunID })
	return runs
}

func (b *LiveStateBuilder) buildQueues(globalCap int) []StateQueue {
	if b.queues == nil {
		return nil
	}
	allQ := b.queues.AllQueues()
	result := make([]StateQueue, 0, len(allQ))
	for name, q := range allQ {
		if q == nil {
			continue
		}
		activeCount := 0
		if b.runs != nil {
			activeCount = b.runs.LenForQueue(name)
		}
		effectiveCap := queue.DefaultWorkers(q.Workers, globalCap)
		totalItems := 0
		eligibleCount := 0
		for gi := range q.Groups {
			g := &q.Groups[gi]
			totalItems += len(g.Items)
			if g.Status == queue.GroupStatusActive {
				eligibleCount += len(queue.EligibleItems(g))
			}
		}
		eligible := q.Status == queue.QueueStatusActive &&
			eligibleCount > 0 &&
			activeCount < effectiveCap

		sq := StateQueue{
			Name:               name,
			Status:             string(q.Status),
			Source:             "live",
			ItemCount:          totalItems,
			ActiveCount:        activeCount,
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

func (b *LiveStateBuilder) buildSessions(ctx context.Context, now time.Time) ([]StateSession, error) {
	if b.projectDir == "" {
		return nil, nil
	}
	crewRecords, err := crew.List(b.projectDir)
	if err != nil {
		return nil, fmt.Errorf("crew.List: %w", err)
	}
	sleepSIDs := scanSleepMarkerSIDs(b.projectDir)
	sessions := make([]StateSession, 0, len(crewRecords))
	for _, cr := range crewRecords {
		sess := b.buildOneSession(ctx, cr.Name, "crew", cr.SessionID, sleepSIDs, now)
		sessions = append(sessions, sess)
	}
	if !hasCaptainRecord(crewRecords) {
		if _, _, err := keeper.ReadCtxFile(b.projectDir, captainAgentName); err == nil {
			sess := b.buildOneSession(ctx, captainAgentName, "captain", "", sleepSIDs, now)
			sessions = append(sessions, sess)
		}
	}
	for i, s := range sessions {
		if s.Agent == captainAgentName {
			sessions[i].SessionType = "captain"
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Agent < sessions[j].Agent })
	return sessions, nil
}

func hasCaptainRecord(records []crew.Record) bool {
	for _, r := range records {
		if r.Name == captainAgentName {
			return true
		}
	}
	return false
}

func (b *LiveStateBuilder) buildOneSession(ctx context.Context, agent, sessionType, declaredSID string, sleepSIDs map[string]bool, now time.Time) StateSession {
	tmuxTarget := lifecycle.TmuxSessionName(b.projectHash, agent)
	alive := tmuxHasSession(ctx, tmuxTarget)

	presenceSrc := "registry"
	if alive && declaredSID == "" {
		presenceSrc = "tmux"
	} else if alive {
		presenceSrc = "both"
	}

	liveSID := liveSessionID(b.projectDir, agent)

	sleepMarker := liveSID != "" && sleepSIDs[strings.ToLower(liveSID)]
	if !sleepMarker && declaredSID != "" && sleepSIDs[strings.ToLower(declaredSID)] {
		sleepMarker = true
	}

	sess := StateSession{
		Agent:          agent,
		SessionType:    sessionType,
		Alive:          alive,
		SleepMarker:    sleepMarker,
		AtRest:         alive && sleepMarker,
		PresenceSource: presenceSrc,
	}
	if alive {
		sess.Cognition = b.buildCognition(agent, liveSID, declaredSID, now)
	}
	return sess
}

func (b *LiveStateBuilder) buildCognition(agent, liveSID, declaredSID string, now time.Time) *SessionCognition {
	cog := &SessionCognition{
		Agent:             agent,
		SessionID:         liveSID,
		SessionIDDeclared: declaredSID,
		SIDDesync:         liveSID != "" && declaredSID != "" && !strings.EqualFold(liveSID, declaredSID),
		Subagents:         nil,
	}

	cf, _, err := keeper.ReadCtxFile(b.projectDir, agent)
	if err != nil {
		cog.Context = SessionContext{Source: "absent", ReadTS: formatRFC3339(now)}
		cog.Signals = absentCognitionSignals()
		return cog
	}

	windowSize := cf.WindowSize
	effectiveWindow := windowSize
	if effectiveWindow == 0 {
		effectiveWindow = fallbackWindowSize
	}
	var fillFrac float64
	if effectiveWindow > 0 && cf.Tokens > 0 {
		fillFrac = math.Round(float64(cf.Tokens)/float64(effectiveWindow)*1000) / 1000
	}

	var gaugeTS string
	var ageSeconds int
	if cf.Ts != "" {
		if ts, parseErr := time.Parse(time.RFC3339, cf.Ts); parseErr == nil {
			gaugeTS = formatRFC3339(ts)
			if d := int(now.Sub(ts).Seconds()); d >= 0 {
				ageSeconds = d
			}
		}
	}

	cog.Context = SessionContext{
		Tokens:     cf.Tokens,
		WindowSize: windowSize,
		FillFrac:   fillFrac,
		Source:     "gauge",
		GaugeTS:    gaugeTS,
		ReadTS:     formatRFC3339(now),
		AgeSeconds: ageSeconds,
	}
	cog.Signals = b.buildCognitionSignals(cf.Tokens, ageSeconds)
	return cog
}

func (b *LiveStateBuilder) buildCognitionSignals(tokens int64, gaugeAgeSeconds int) CognitionSignals {
	return CognitionSignals{
		TooBig:        b.buildTooBigSignal(tokens),
		ContextStatic: b.buildContextStaticSignal(gaugeAgeSeconds),
		LoopDetected:  nil, // DEFERRED (SS-013)
	}
}

func (b *LiveStateBuilder) buildTooBigSignal(tokens int64) TooBigSignal {
	warnAbs := b.kconfig.WarnAbsTokens // 0 = not configured
	actAbs := b.kconfig.ActAbsTokens
	forceAbs := b.kconfig.ForceActAbsTokens

	sig := TooBigSignal{
		ThresholdRef: "keeper.context_thresholds.warn_abs_tokens",
		Value:        tokens,
	}
	if warnAbs <= 0 {
		return sig
	}

	sig.Threshold = &warnAbs
	if tokens >= warnAbs {
		sig.Tripped = true
		sig.Band = "warn"
		if actAbs > 0 && tokens >= actAbs {
			sig.Band = "act"
		}
		if forceAbs > 0 && tokens >= forceAbs {
			sig.Band = "force_act"
		}
	} else {
		sig.Band = "warn" // reference level; value is below the warn band
	}
	return sig
}

func (b *LiveStateBuilder) buildContextStaticSignal(gaugeAgeSeconds int) ContextStaticSignal {
	sig := ContextStaticSignal{
		GaugeAgeSeconds:          gaugeAgeSeconds,
		StalenessRef:             "keeper.staleness",
		TokensUnchangedIntervals: 0, // no multi-sample history in a single .ctx read
		StuckMinIntervalsRef:     "keeper.stuck_min_intervals",
		StuckMinIntervals:        nil, // knob not yet in KeeperConfig (SS-012 deferred)
		Flat:                     nil, // null when StuckMinIntervals unset
	}
	if b.kconfig.Staleness > 0 {
		s := int(b.kconfig.Staleness.Seconds())
		sig.StalenessS = &s
	}
	return sig
}

func absentCognitionSignals() CognitionSignals {
	return CognitionSignals{
		TooBig:        TooBigSignal{ThresholdRef: "keeper.context_thresholds.warn_abs_tokens"},
		ContextStatic: ContextStaticSignal{StalenessRef: "keeper.staleness", StuckMinIntervalsRef: "keeper.stuck_min_intervals"},
		LoopDetected:  nil,
	}
}

func scanSleepMarkerSIDs(projectDir string) map[string]bool {
	dir := filepath.Join(projectDir, sleepingMarkerDir)
	pattern := filepath.Join(dir, onDiskSleepMarkerPrefix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return nil
	}
	sids := make(map[string]bool, len(matches))
	for _, path := range matches {
		base := filepath.Base(path)
		sid := strings.TrimPrefix(base, onDiskSleepMarkerPrefix)
		if sid != "" {
			sids[strings.ToLower(sid)] = true
		}
		data, readErr := os.ReadFile(path) //nolint:gosec // G304: glob within projectDir/.harmonik
		if readErr != nil {
			continue
		}
		var m struct {
			SessionID string `json:"session_id"`
		}
		if jsonErr := json.Unmarshal(data, &m); jsonErr == nil && m.SessionID != "" {
			sids[strings.ToLower(m.SessionID)] = true
		}
	}
	return sids
}

// RollUpLabel computes the fleet activity label.
// Priority: PROCESSING > DRAINING > WAITING > INACTIVE. Spec: SS-003..006.
func RollUpLabel(runs []StateRun, queues []StateQueue, facts *FleetFacts, sessions []StateSession, projectDir string) ActivityLabel {
	if runsInflight(runs, facts) || queueDispatchable(queues) {
		return ActivityProcessing
	}
	if queueDraining(queues) || anySleeping(projectDir) {
		return ActivityDraining
	}
	if sessionsAlive(sessions) && hasLatentWork(facts) {
		return ActivityWaiting
	}
	return ActivityInactive
}

func runsInflight(runs []StateRun, facts *FleetFacts) bool {
	if len(runs) > 0 {
		return true
	}
	return facts != nil && facts.Runs.LiveWorktrees > 0
}

func queueDispatchable(queues []StateQueue) bool {
	for _, q := range queues {
		if q.EligibleNow {
			return true
		}
	}
	return false
}

func queueDraining(queues []StateQueue) bool {
	for _, q := range queues {
		if q.Status == string(queue.QueueStatusPausedByDrain) {
			return true
		}
	}
	return false
}

func anySleeping(projectDir string) bool {
	if projectDir == "" {
		return false
	}
	dir := filepath.Join(projectDir, sleepingMarkerDir)
	pattern := filepath.Join(dir, onDiskSleepMarkerPrefix+"*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return false // bad pattern: no marker can match either
	}
	return len(matches) > 0
}

func sessionsAlive(sessions []StateSession) bool {
	for _, s := range sessions {
		if s.Alive {
			return true
		}
	}
	return false
}

func hasLatentWork(facts *FleetFacts) bool {
	if facts == nil {
		return false
	}
	return policy.HasLatentWork(drainSnapshot(facts))
}

func readPidFromFile(projectDir string) (int, error) {
	pidFile := filepath.Join(projectDir, ".harmonik", "supervisor.pid")
	data, err := os.ReadFile(pidFile) //nolint:gosec // G304: operator-controlled projectDir
	if err != nil {
		return 0, err
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err != nil {
		return 0, err
	}
	return pid, nil
}

func readDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
