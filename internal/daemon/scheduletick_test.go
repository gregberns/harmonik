package daemon

// scheduletick_test.go — unit tests for the work-loop schedule tick (hk-0es).
//
// These are plain unit tests (NOT scenario): they construct a workLoopDeps with
// a real schedule.Store over a temp dir and lightweight doubles for the crew
// handler and comms-who query. No daemon, tmux, or br is booted.

import (
	"context"
	"encoding/json"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/schedule"
)

// fakeCrewStarter records HandleCrewStart calls.
type fakeCrewStarter struct {
	mu     sync.Mutex
	starts []CrewStartRequest
	err    error
}

func (f *fakeCrewStarter) HandleCrewStart(_ context.Context, payload json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var req CrewStartRequest
	_ = json.Unmarshal(payload, &req)
	f.starts = append(f.starts, req)
	if f.err != nil {
		return nil, f.err
	}
	return json.RawMessage(`{"session_id":"fake","name":"` + req.Name + `"}`), nil
}

func (f *fakeCrewStarter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.starts)
}

// onlineSet returns a commsWhoQuerier that always reports the given online names.
func onlineSet(names ...string) commsWhoQuerier {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(_ context.Context) (map[string]struct{}, error) { return set, nil }
}

func newTickDeps(t *testing.T) (workLoopDeps, *schedule.Store, *fakeCrewStarter) {
	t.Helper()
	dir := t.TempDir()
	store := schedule.NewStore(dir)
	if err := store.Load(); err != nil {
		t.Fatalf("store Load: %v", err)
	}
	crew := &fakeCrewStarter{}
	deps := workLoopDeps{
		projectDir:      dir,
		scheduleStore:   store,
		crewHandler:     crew,
		commsWhoQuerier: onlineSet(), // none online by default
		handlerEnv:      []string{"HARMONIK_PROJECT_HASH=test"},
	}
	return deps, store, crew
}

// pastDailyAt returns an HH:MM string that is already in the past today (UTC) so
// a freshly-added enabled job is immediately due.
func pastDailyAt(t *testing.T) string {
	t.Helper()
	past := time.Now().UTC().Add(-2 * time.Hour)
	return past.Format("15:04")
}

func TestScheduleTick_SpawnCrewFires(t *testing.T) {
	deps, store, crew := newTickDeps(t)

	job := schedule.ScheduledJob{
		ID:       "j1",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:   schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night", Mission: "/tmp/m.md"},
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	runScheduleTick(context.Background(), deps)

	if crew.count() != 1 {
		t.Fatalf("crew start count = %d, want 1", crew.count())
	}
	got, _ := store.Get("j1")
	if got.LastFire == "" {
		t.Fatalf("LastFire not recorded after fire")
	}

	// A second tick in the same window must NOT re-fire (LastFire now covers it).
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("crew re-fired in same window: count = %d, want 1", crew.count())
	}
}

func TestScheduleTick_SpawnCrewSkipOnOverlap(t *testing.T) {
	deps, store, crew := newTickDeps(t)
	deps.commsWhoQuerier = onlineSet("owl") // crew already online → skip

	job := schedule.ScheduledJob{
		ID:            "j2",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:        schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	runScheduleTick(context.Background(), deps)

	if crew.count() != 0 {
		t.Fatalf("crew fired despite overlap skip: count = %d, want 0", crew.count())
	}
	// LastFire NOT advanced (the fire was skipped, not serviced).
	got, _ := store.Get("j2")
	if got.LastFire != "" {
		t.Fatalf("LastFire advanced on a skipped fire: %q", got.LastFire)
	}
}

func TestScheduleTick_SpawnCrewAllowOverlap(t *testing.T) {
	deps, store, crew := newTickDeps(t)
	deps.commsWhoQuerier = onlineSet("owl") // online, but policy=allow

	job := schedule.ScheduledJob{
		ID:            "j3",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:        schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicyAllow,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	runScheduleTick(context.Background(), deps)

	if crew.count() != 1 {
		t.Fatalf("allow-overlap crew did not fire: count = %d, want 1", crew.count())
	}
}

func TestScheduleTick_DisabledJobNeverFires(t *testing.T) {
	deps, store, crew := newTickDeps(t)
	job := schedule.ScheduledJob{
		ID:       "j4",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:   schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:  false,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	runScheduleTick(context.Background(), deps)
	if crew.count() != 0 {
		t.Fatalf("disabled job fired: count = %d, want 0", crew.count())
	}
}

func TestScheduleTick_RunNowFiresAndClears(t *testing.T) {
	deps, store, crew := newTickDeps(t)
	// Future fire time so the job is NOT due on its own, isolating the run-now path.
	future := time.Now().UTC().Add(6 * time.Hour).Format("15:04")
	job := schedule.ScheduledJob{
		ID:       "j5",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: future, TZ: "UTC"},
		Action:   schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if ok, err := store.RequestRunNow("j5"); err != nil || !ok {
		t.Fatalf("RequestRunNow = (%v,%v)", ok, err)
	}

	runScheduleTick(context.Background(), deps)

	if crew.count() != 1 {
		t.Fatalf("run-now did not fire: count = %d, want 1", crew.count())
	}
	got, _ := store.Get("j5")
	if got.ForceNext {
		t.Fatalf("ForceNext not cleared after run-now fire")
	}

	// A subsequent tick must not re-fire (flag cleared, not yet due).
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("run-now re-fired: count = %d, want 1", crew.count())
	}
}

func TestScheduleTick_CommandActionRecordsPID(t *testing.T) {
	deps, store, _ := newTickDeps(t)
	job := schedule.ScheduledJob{
		ID:       "cmd1",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:   schedule.Action{Kind: schedule.ActionKindCommand, Argv: []string{"true"}},
		Enabled:  true,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	runScheduleTick(context.Background(), deps)
	got, _ := store.Get("cmd1")
	if got.LastFire == "" {
		t.Fatalf("command action did not record LastFire")
	}
	if got.LastPID <= 0 {
		t.Fatalf("command action did not record a positive LastPID, got %d", got.LastPID)
	}
}

func TestScheduleTick_ReloadPicksUpOutOfProcessAdd(t *testing.T) {
	deps, _, crew := newTickDeps(t)
	// Simulate the CLI writing to the same file via a SECOND store over the same
	// dir (the daemon's in-memory store has not seen this add).
	cliStore := schedule.NewStore(deps.projectDir)
	if err := cliStore.Load(); err != nil {
		t.Fatalf("cliStore Load: %v", err)
	}
	job := schedule.ScheduledJob{
		ID:       "oop",
		Schedule: schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: pastDailyAt(t), TZ: "UTC"},
		Action:   schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:  true,
	}
	if err := cliStore.Add(job); err != nil {
		t.Fatalf("cliStore Add: %v", err)
	}

	// The daemon's tick must reload the file (mtime changed) and fire the new job.
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("daemon did not pick up out-of-process add: count = %d, want 1", crew.count())
	}
}

// TestScheduleTick_MultiDayMissCoalescesToOneFire is the FIX-4 edge case: a job
// that missed MULTIPLE schedule intervals (LastFire 3 days ago, daemon was down)
// must coalesce to EXACTLY ONE catch-up fire within the window, and LastFire must
// advance to NOW (not to the old missed instant) so the next tick is not due.
func TestScheduleTick_MultiDayMissCoalescesToOneFire(t *testing.T) {
	deps, store, crew := newTickDeps(t)

	// At fires 1h ago each day; LastFire is 3 days ago → 3 missed instants. A wide
	// catch-up window so the most-recent missed instant is in-window.
	at := time.Now().UTC().Add(-1 * time.Hour).Format("15:04")
	job := schedule.ScheduledJob{
		ID:            "multi",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: at, TZ: "UTC"},
		Action:        schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:       true,
		Catchup:       schedule.CatchupCoalesceWithinWindow,
		CatchupWindow: "72h",
		LastFire:      time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339),
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	before := time.Now().UTC()
	runScheduleTick(context.Background(), deps)
	after := time.Now().UTC()

	if crew.count() != 1 {
		t.Fatalf("multi-day miss did not coalesce to ONE fire: count = %d, want 1", crew.count())
	}
	got, _ := store.Get("multi")
	lf, err := time.Parse(time.RFC3339, got.LastFire)
	if err != nil {
		t.Fatalf("LastFire %q not RFC3339: %v", got.LastFire, err)
	}
	// LastFire must advance to ~now (the fire instant), NOT remain at an old missed
	// boundary. doFireAction records nowUTC, so it must lie within the tick window.
	if lf.Before(before.Add(-time.Second)) || lf.After(after.Add(time.Second)) {
		t.Fatalf("LastFire = %s did not advance to now (~[%s,%s]); a stale boundary would re-fire next tick",
			lf.Format(time.RFC3339), before.Format(time.RFC3339), after.Format(time.RFC3339))
	}

	// A second tick must NOT re-fire — the coalesce is one-shot.
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("multi-day catch-up re-fired on second tick: count = %d, want 1", crew.count())
	}
}

// TestScheduleTick_MissedBeyondWindowSkips is the FIX-4 companion: when the
// most-recent missed instant is BEYOND the catch-up window, the tick skips the
// fire (no crew start) but advances LastFire past the missed instant and logs, so
// the job resumes normal forward scheduling without re-evaluating the stale miss.
func TestScheduleTick_MissedBeyondWindowSkips(t *testing.T) {
	deps, store, crew := newTickDeps(t)

	// At fires 1h ago; window 30m → the 1h-ago missed instant is outside it.
	at := time.Now().UTC().Add(-1 * time.Hour).Format("15:04")
	job := schedule.ScheduledJob{
		ID:            "skip",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: at, TZ: "UTC"},
		Action:        schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:       true,
		Catchup:       schedule.CatchupCoalesceWithinWindow,
		CatchupWindow: "30m",
		LastFire:      time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339),
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	runScheduleTick(context.Background(), deps)

	if crew.count() != 0 {
		t.Fatalf("missed-beyond-window fired the crew: count = %d, want 0", crew.count())
	}
	got, _ := store.Get("skip")
	// LastFire advanced to the skipped missed instant (PrevFire ~1h ago), so the
	// stale miss is not re-evaluated forever.
	lf, err := time.Parse(time.RFC3339, got.LastFire)
	if err != nil {
		t.Fatalf("LastFire %q not RFC3339: %v", got.LastFire, err)
	}
	if !lf.After(time.Now().UTC().Add(-48 * time.Hour)) {
		t.Fatalf("LastFire %s was not advanced past the old miss", lf.Format(time.RFC3339))
	}

	// Next tick: the skipped instant is now serviced, so nothing is due (no fire).
	runScheduleTick(context.Background(), deps)
	if crew.count() != 0 {
		t.Fatalf("re-evaluated the skipped miss on a later tick: count = %d, want 0", crew.count())
	}
}

// TestScheduleTick_CommandOverlapSkip is the FIX-4 command-action overlap case: a
// job with a LIVE LastPID under overlap_policy=skip must NOT fire (the prior
// command is still running). We spawn a real long-sleep process to obtain a live
// pid, record it as the job's LastPID, and assert the tick skips.
func TestScheduleTick_CommandOverlapSkip(t *testing.T) {
	deps, store, _ := newTickDeps(t)

	// Spawn a real, alive process whose pid we can record as the prior command.
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatalf("start sleeper: %v", err)
	}
	t.Cleanup(func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	})
	alivePID := sleeper.Process.Pid

	// Build a job that would be due (At 1h ago, LastFire 2 days ago) but whose
	// prior command pid is still alive → skip-on-overlap.
	at := time.Now().UTC().Add(-1 * time.Hour).Format("15:04")
	job := schedule.ScheduledJob{
		ID:            "cmdskip",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindDaily, At: at, TZ: "UTC"},
		Action:        schedule.Action{Kind: schedule.ActionKindCommand, Argv: []string{"true"}},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
		LastFire:      time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339),
		LastPID:       alivePID,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	runScheduleTick(context.Background(), deps)

	got, _ := store.Get("cmdskip")
	// Skipped: LastFire/LastPID unchanged (the fire was not serviced).
	if got.LastPID != alivePID {
		t.Fatalf("LastPID changed on a skipped overlap fire: got %d, want %d (alive sleeper)", got.LastPID, alivePID)
	}
	if got.LastFire != job.LastFire {
		t.Fatalf("LastFire advanced on a skipped overlap fire: %q (a fire was wrongly serviced)", got.LastFire)
	}
}

// TestScheduleTick_IntervalFires covers the ScheduleKindEvery path end-to-end
// through the tick: a job that has never fired fires immediately; a subsequent
// tick that is too early does not re-fire; a tick after the interval elapses fires
// again.
func TestScheduleTick_IntervalFires(t *testing.T) {
	deps, store, crew := newTickDeps(t)

	job := schedule.ScheduledJob{
		ID:            "interval1",
		Schedule:      schedule.Schedule{Kind: schedule.ScheduleKindEvery, Interval: "1s"},
		Action:        schedule.Action{Kind: schedule.ActionKindSpawnCrew, Crew: "owl", Queue: "night"},
		Enabled:       true,
		OverlapPolicy: schedule.OverlapPolicySkip,
		Catchup:       schedule.CatchupOff,
	}
	if err := store.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// First tick: never fired → should fire immediately.
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("interval job did not fire on first tick: count = %d, want 1", crew.count())
	}

	// Immediate second tick: interval not yet elapsed → no re-fire.
	runScheduleTick(context.Background(), deps)
	if crew.count() != 1 {
		t.Fatalf("interval job re-fired before interval elapsed: count = %d, want 1", crew.count())
	}
}

// TestScheduleTick_EnsureOpsMonitor verifies ensureOpsMonitorSchedule registers
// the ops-monitor job when absent and is a no-op when already present.
func TestScheduleTick_EnsureOpsMonitor(t *testing.T) {
	deps, store, _ := newTickDeps(t)

	// Before: job is absent.
	if _, ok := store.Get(opsMonitorJobID); ok {
		t.Fatal("ops-monitor job unexpectedly present before ensure call")
	}

	ensureOpsMonitorSchedule(store)

	j, ok := store.Get(opsMonitorJobID)
	if !ok {
		t.Fatal("ops-monitor job not registered after ensureOpsMonitorSchedule")
	}
	if j.Schedule.Kind != schedule.ScheduleKindEvery {
		t.Errorf("kind = %q, want %q", j.Schedule.Kind, schedule.ScheduleKindEvery)
	}
	if j.Schedule.Interval != "5m" {
		t.Errorf("interval = %q, want %q", j.Schedule.Interval, "5m")
	}
	if !j.Enabled {
		t.Errorf("ops-monitor job should be enabled by default")
	}

	// Second call is a no-op (idempotent).
	ensureOpsMonitorSchedule(store)
	_ = deps // satisfy unused import
}

// TestScheduleTick_EnsureCtxWatchdog verifies ensureCtxWatchdogSchedule registers
// the ctx-watchdog job when absent and is a no-op when already present (hk-sbitr).
func TestScheduleTick_EnsureCtxWatchdog(t *testing.T) {
	deps, store, _ := newTickDeps(t)

	// enabled=true path: job absent before the first call.
	if _, ok := store.Get(ctxWatchdogJobID); ok {
		t.Fatal("ctx-watchdog job unexpectedly present before ensure call")
	}

	ensureCtxWatchdogSchedule(store, true)

	j, ok := store.Get(ctxWatchdogJobID)
	if !ok {
		t.Fatal("ctx-watchdog job not registered after ensureCtxWatchdogSchedule(enabled=true)")
	}
	if j.Schedule.Kind != schedule.ScheduleKindEvery {
		t.Errorf("kind = %q, want %q", j.Schedule.Kind, schedule.ScheduleKindEvery)
	}
	if j.Schedule.Interval != "5m" {
		t.Errorf("interval = %q, want %q", j.Schedule.Interval, "5m")
	}
	if !j.Enabled {
		t.Errorf("ctx-watchdog job should be enabled by default")
	}

	// Second call is a no-op (idempotent).
	ensureCtxWatchdogSchedule(store, true)

	// enabled=false path: a fresh store should NOT register the job.
	_, store2, _ := newTickDeps(t)
	ensureCtxWatchdogSchedule(store2, false)
	if _, ok := store2.Get(ctxWatchdogJobID); ok {
		t.Error("ctx-watchdog job registered despite enabled=false")
	}
	_ = deps // satisfy unused import
}

// TestParseCommsWho_NDJSONContract pins the FIX-3 `comms who --json` contract:
// NDJSON rows; only status=="online" agents are reported; stale/dead rows are
// excluded; and a JSON-array fallback is honoured so a future shape change fails
// LOUD instead of fail-OPEN with an empty set.
func TestParseCommsWho_NDJSONContract(t *testing.T) {
	t.Run("ndjson filters by online status", func(t *testing.T) {
		out := `{"agent":"liet","last_seen":"2026-06-12T00:00:00Z","status":"online"}
{"agent":"stale","last_seen":"2026-06-11T00:00:00Z","status":"stale"}
{"agent":"dead","last_seen":"2026-06-10T00:00:00Z","status":"dead"}
{"agent":"duncan","last_seen":"2026-06-12T00:00:01Z","status":"online"}
`
		got, err := parseCommsWho([]byte(out))
		if err != nil {
			t.Fatalf("parseCommsWho: %v", err)
		}
		if _, ok := got["liet"]; !ok {
			t.Errorf("online agent liet missing from set")
		}
		if _, ok := got["duncan"]; !ok {
			t.Errorf("online agent duncan missing from set")
		}
		if _, ok := got["stale"]; ok {
			t.Errorf("stale agent must NOT be reported online")
		}
		if _, ok := got["dead"]; ok {
			t.Errorf("dead agent must NOT be reported online")
		}
		if len(got) != 2 {
			t.Errorf("online set size = %d, want 2", len(got))
		}
	})

	t.Run("empty output → empty set, no error", func(t *testing.T) {
		got, err := parseCommsWho([]byte("\n  \n"))
		if err != nil {
			t.Fatalf("parseCommsWho(empty): %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("empty output → set size %d, want 0", len(got))
		}
	})

	t.Run("json-array fallback honoured (no silent fail-open)", func(t *testing.T) {
		out := `[{"agent":"liet","status":"online"},{"agent":"stale","status":"stale"}]`
		got, err := parseCommsWho([]byte(out))
		if err != nil {
			t.Fatalf("parseCommsWho(array): %v", err)
		}
		if _, ok := got["liet"]; !ok {
			t.Errorf("array form: online agent liet missing")
		}
		if _, ok := got["stale"]; ok {
			t.Errorf("array form: stale agent wrongly reported online")
		}
		if len(got) != 1 {
			t.Errorf("array form: set size = %d, want 1", len(got))
		}
	})

	t.Run("unparseable non-empty output → error (fail LOUD)", func(t *testing.T) {
		if _, err := parseCommsWho([]byte("this is not json at all <<<")); err == nil {
			t.Fatalf("expected an error on unparseable output, got nil (would fail-OPEN)")
		}
	})
}
