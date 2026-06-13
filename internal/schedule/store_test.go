package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func sampleJob(id string) ScheduledJob {
	return ScheduledJob{
		ID:       id,
		Schedule: Schedule{Kind: ScheduleKindDaily, At: "09:00", TZ: "UTC"},
		Action:   Action{Kind: ActionKindCommand, Argv: []string{"echo", "hi"}},
		Enabled:  true,
	}
}

func TestStore_AddGetListRemove(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load (empty): %v", err)
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List on empty store = %d jobs, want 0", len(got))
	}

	if err := s.Add(sampleJob("b")); err != nil {
		t.Fatalf("Add b: %v", err)
	}
	if err := s.Add(sampleJob("a")); err != nil {
		t.Fatalf("Add a: %v", err)
	}

	// Get
	got, ok := s.Get("a")
	if !ok {
		t.Fatalf("Get(a): not found")
	}
	// Defaults must be normalised on add.
	if got.OverlapPolicy != OverlapPolicySkip {
		t.Errorf("OverlapPolicy default = %q, want %q", got.OverlapPolicy, OverlapPolicySkip)
	}
	if got.Catchup != CatchupCoalesceWithinWindow {
		t.Errorf("Catchup default = %q, want %q", got.Catchup, CatchupCoalesceWithinWindow)
	}

	// List is sorted by id.
	list := s.List()
	if len(list) != 2 || list[0].ID != "a" || list[1].ID != "b" {
		t.Fatalf("List = %+v, want sorted [a b]", idsOf(list))
	}

	// Remove
	removed, err := s.Remove("a")
	if err != nil {
		t.Fatalf("Remove a: %v", err)
	}
	if !removed {
		t.Fatalf("Remove a returned false")
	}
	if _, ok := s.Get("a"); ok {
		t.Fatalf("Get(a) still found after remove")
	}

	// Remove of absent id is a no-op (false, nil).
	removed, err = s.Remove("zzz")
	if err != nil || removed {
		t.Fatalf("Remove(absent) = (%v,%v), want (false,nil)", removed, err)
	}
}

func TestStore_AddRequiresID(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add(ScheduledJob{}); err == nil {
		t.Fatalf("Add with empty id: expected error, got nil")
	}
}

func TestStore_PersistReloadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s1 := NewStore(dir)
	job := sampleJob("rt")
	job.OverlapPolicy = OverlapPolicyAllow
	job.Catchup = CatchupOff
	job.CatchupWindow = "12h"
	if err := s1.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A fresh store over the same dir must see the same job.
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := s2.Get("rt")
	if !ok {
		t.Fatalf("reloaded store missing job")
	}
	if got.OverlapPolicy != OverlapPolicyAllow || got.Catchup != CatchupOff || got.CatchupWindow != "12h" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if got.Action.Kind != ActionKindCommand || len(got.Action.Argv) != 2 || got.Action.Argv[0] != "echo" {
		t.Fatalf("round-trip action mismatch: %+v", got.Action)
	}
}

func TestStore_AtomicWrite_NoTempLeftBehind(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Add(sampleJob("x")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	hk := filepath.Join(dir, ".harmonik")
	entries, err := os.ReadDir(hk)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" {
			continue
		}
		if len(e.Name()) >= 4 && e.Name()[len(e.Name())-4:] == ".tmp" {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
		// also catch the .tmp-<pid> form
		if containsTmp(e.Name()) {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
	// The canonical file exists and parses as a fileDoc.
	data, err := os.ReadFile(filepath.Join(hk, scheduleFileName))
	if err != nil {
		t.Fatalf("read schedules.json: %v", err)
	}
	var doc fileDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("schedules.json not valid JSON: %v", err)
	}
	if doc.SchemaVersion != fileSchemaVersion {
		t.Fatalf("schema_version = %d, want %d", doc.SchemaVersion, fileSchemaVersion)
	}
}

func TestStore_SetEnabled(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add(sampleJob("e")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ok, err := s.SetEnabled("e", false)
	if err != nil || !ok {
		t.Fatalf("SetEnabled = (%v,%v)", ok, err)
	}
	got, _ := s.Get("e")
	if got.Enabled {
		t.Fatalf("job still enabled after disable")
	}
	ok, err = s.SetEnabled("absent", true)
	if err != nil || ok {
		t.Fatalf("SetEnabled(absent) = (%v,%v), want (false,nil)", ok, err)
	}
}

func TestStore_MarkFired(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add(sampleJob("m")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ts := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	ok, err := s.MarkFired("m", ts, 4242)
	if err != nil || !ok {
		t.Fatalf("MarkFired = (%v,%v)", ok, err)
	}
	got, _ := s.Get("m")
	if got.LastFire != ts || got.LastPID != 4242 {
		t.Fatalf("MarkFired result: LastFire=%q LastPID=%d", got.LastFire, got.LastPID)
	}
}

func TestStore_RunNowFlag(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Add(sampleJob("r")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ok, err := s.RequestRunNow("r")
	if err != nil || !ok {
		t.Fatalf("RequestRunNow = (%v,%v)", ok, err)
	}
	got, _ := s.Get("r")
	if !got.ForceNext {
		t.Fatalf("ForceNext not set after RequestRunNow")
	}
	ok, err = s.ClearForceNext("r")
	if err != nil || !ok {
		t.Fatalf("ClearForceNext = (%v,%v)", ok, err)
	}
	got, _ = s.Get("r")
	if got.ForceNext {
		t.Fatalf("ForceNext still set after ClearForceNext")
	}
}

func TestStore_ReloadIfChanged(t *testing.T) {
	dir := t.TempDir()
	writer := NewStore(dir)
	reader := NewStore(dir)
	if err := reader.Load(); err != nil {
		t.Fatalf("reader Load: %v", err)
	}

	// Out-of-process add (simulating the CLI writing the file).
	if err := writer.Add(sampleJob("rc")); err != nil {
		t.Fatalf("writer Add: %v", err)
	}
	// Force a modtime difference robustly: rewrite the file once more so its mtime
	// is strictly newer than the reader's zero-value loadedMod.
	if err := writer.Add(sampleJob("rc2")); err != nil {
		t.Fatalf("writer Add 2: %v", err)
	}

	changed, err := reader.ReloadIfChanged()
	if err != nil {
		t.Fatalf("ReloadIfChanged: %v", err)
	}
	if !changed {
		t.Fatalf("ReloadIfChanged reported no change after out-of-process write")
	}
	if _, ok := reader.Get("rc"); !ok {
		t.Fatalf("reader did not pick up out-of-process job after reload")
	}

	// A second reload with no file change reports false.
	changed, err = reader.ReloadIfChanged()
	if err != nil {
		t.Fatalf("ReloadIfChanged (2): %v", err)
	}
	if changed {
		t.Fatalf("ReloadIfChanged reported change when file was unmodified")
	}
}

func TestStore_LoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	hk := filepath.Join(dir, ".harmonik")
	if err := os.MkdirAll(hk, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hk, scheduleFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	s := NewStore(dir)
	if err := s.Load(); err == nil {
		t.Fatalf("Load on corrupt file: expected error, got nil")
	}
}

func idsOf(jobs []ScheduledJob) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}

func containsTmp(name string) bool {
	const marker = ".tmp-"
	for i := 0; i+len(marker) <= len(name); i++ {
		if name[i:i+len(marker)] == marker {
			return true
		}
	}
	return false
}

// TestStore_CrossProcessNoLostUpdate is the FIX-1 regression: two SEPARATE Store
// instances over the same dir model the daemon and the CLI as distinct
// processes. The daemon records a fire (MarkFired) using its in-memory snapshot;
// the CLI then disables the SAME job from ITS (now stale) in-memory snapshot.
// Before the flock+reload-from-disk fix, the CLI's whole-file write clobbered the
// daemon's LastFire back to empty, so the next tick would double-fire the
// catch-up. With the fix, every mutation re-reads current disk state under the
// flock, so the disable preserves the daemon's LastFire.
func TestStore_CrossProcessNoLostUpdate(t *testing.T) {
	dir := t.TempDir()

	daemon := NewStore(dir)
	cli := NewStore(dir)

	// Both processes start from the same on-disk state (job present, never fired).
	if err := daemon.Add(sampleJob("job1")); err != nil {
		t.Fatalf("daemon Add: %v", err)
	}
	if err := cli.Load(); err != nil {
		t.Fatalf("cli Load: %v", err)
	}

	// Daemon records a fire. Its in-memory + on-disk LastFire now advances.
	fireTS := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if ok, err := daemon.MarkFired("job1", fireTS, 4242); err != nil || !ok {
		t.Fatalf("daemon MarkFired = (%v,%v)", ok, err)
	}

	// CLI now disables the job from its STALE snapshot (it never saw the fire).
	// The fix makes this a read-modify-write under the flock: it reloads the
	// daemon's committed LastFire before applying the disable, so LastFire is NOT
	// clobbered back to empty.
	if ok, err := cli.SetEnabled("job1", false); err != nil || !ok {
		t.Fatalf("cli SetEnabled = (%v,%v)", ok, err)
	}

	// Re-read from disk via a fresh store: both mutations must be present.
	verify := NewStore(dir)
	if err := verify.Load(); err != nil {
		t.Fatalf("verify Load: %v", err)
	}
	got, ok := verify.Get("job1")
	if !ok {
		t.Fatalf("job1 missing after both mutations")
	}
	if got.LastFire != fireTS {
		t.Fatalf("LOST UPDATE: LastFire = %q after CLI disable, want %q (daemon's fire was clobbered)", got.LastFire, fireTS)
	}
	if got.LastPID != 4242 {
		t.Fatalf("LOST UPDATE: LastPID = %d after CLI disable, want 4242", got.LastPID)
	}
	if got.Enabled {
		t.Fatalf("CLI disable did not take effect: job still enabled")
	}
}

// TestStore_ConcurrentMutationsRaceClean hammers a single store dir with
// concurrent mutations from many goroutines across two Store instances (daemon +
// CLI). Each goroutine adds its own job then marks it fired; at the end every
// job's LastFire must be set — i.e. no mutation was lost to a clobber and the
// flock+RMW path is race-clean under -race. Run with `go test -race`.
func TestStore_ConcurrentMutationsRaceClean(t *testing.T) {
	dir := t.TempDir()
	storeA := NewStore(dir)
	storeB := NewStore(dir)
	if err := storeA.Load(); err != nil {
		t.Fatalf("storeA Load: %v", err)
	}
	if err := storeB.Load(); err != nil {
		t.Fatalf("storeB Load: %v", err)
	}

	const n = 12
	ts := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			st := storeA
			if i%2 == 1 {
				st = storeB // alternate the two "processes"
			}
			id := fmt.Sprintf("j%02d", i)
			job := sampleJob(id)
			if err := st.Add(job); err != nil {
				t.Errorf("Add %s: %v", id, err)
				return
			}
			if ok, err := st.MarkFired(id, ts, 1000+i); err != nil || !ok {
				t.Errorf("MarkFired %s = (%v,%v)", id, ok, err)
			}
		}(i)
	}
	wg.Wait()

	verify := NewStore(dir)
	if err := verify.Load(); err != nil {
		t.Fatalf("verify Load: %v", err)
	}
	jobs := verify.List()
	if len(jobs) != n {
		t.Fatalf("expected %d jobs after concurrent adds, got %d (a job was lost)", n, len(jobs))
	}
	for _, j := range jobs {
		if j.LastFire != ts {
			t.Fatalf("job %s LastFire = %q, want %q (MarkFired lost)", j.ID, j.LastFire, ts)
		}
	}
}
