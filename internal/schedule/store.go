package schedule

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

const scheduleFileName = "schedules.json"

const scheduleLockName = "schedules.json.lock"

const scheduleLockTimeout = 10 * time.Second

const scheduleLockRetryInterval = 25 * time.Millisecond

const wakeBufSize = 1

type fileDoc struct {
	SchemaVersion int            `json:"schema_version"`
	Jobs          []ScheduledJob `json:"jobs"`
}

const fileSchemaVersion = 1

// Store is the id-keyed registry of ScheduledJobs backed by
// .harmonik/schedules.json.
//
// # Concurrency model (two locks, fixed acquisition order)
//
// The daemon and the CLI are SEPARATE processes that both mutate schedules.json
// (the CLI runs `schedule add/remove/enable/...` whether or not the daemon is
// up; the daemon runs MarkFired/ClearForceNext on each tick). To prevent a
// cross-process lost update — e.g. a CLI Load that precedes a daemon MarkFired
// but whose write follows it, clobbering LastFire back to a stale value and
// double-firing the catch-up — every mutation is a read-modify-write performed
// UNDER an advisory file lock:
//
//  1. flock (cross-process): an advisory LOCK_EX on the sidecar
//     .harmonik/schedules.json.lock, held across the whole RMW. This serialises
//     writers in DIFFERENT processes.
//  2. mu (in-process): a sync.RWMutex guarding the in-memory jobs map so
//     in-process List/Get readers always see a consistent snapshot.
//
// ACQUISITION ORDER is always flock-first, then mu — see mutate(). The flock is
// taken once for the whole RMW; mu is taken only for the brief moments the
// in-memory map is read or swapped. Because the two locks are always acquired in
// this same order (and mu is never held while blocking on the flock), there is
// no lock-ordering deadlock.
//
// All reads (Get/List) go through mu (read lock) only — they never take the
// flock, so they may observe state up to one tick stale relative to another
// process's in-flight write. That staleness is acceptable: the daemon's
// schedule.Decide is idempotent against LastFire, and ReloadIfChanged re-reads
// before each tick.
//
// The zero value is NOT valid — use NewStore.
type Store struct {
	mu         sync.RWMutex
	jobs       map[string]*ScheduledJob
	projectDir string
	wakeC      chan struct{}
	// loadedMod is the modtime of the file at the last Load/ReloadIfChanged, used
	// to cheaply detect out-of-process mutations (the CLI writes the file directly
	// whether or not the daemon is up).
	loadedMod time.Time
}

// NewStore returns a ready-to-use Store bound to projectDir with no jobs loaded.
// Call Load to hydrate from disk.
func NewStore(projectDir string) *Store {
	return &Store{
		jobs:       make(map[string]*ScheduledJob),
		projectDir: projectDir,
		wakeC:      make(chan struct{}, wakeBufSize),
	}
}

// WakeCh returns the channel that receives a signal after every mutation. The
// work loop selects on this alongside its poll timer so a CLI mutation (when the
// daemon shares the in-memory store) wakes the loop immediately.
func (s *Store) WakeCh() <-chan struct{} { return s.wakeC }

func (s *Store) signalWake() {
	select {
	case s.wakeC <- struct{}{}:
	default:
	}
}

// Load reads .harmonik/schedules.json into the in-memory map. An absent file is
// not an error (empty store). A present-but-unparseable file returns an error so
// the operator can inspect it (the file is never auto-deleted).
func (s *Store) Load() error {
	return s.loadFromDisk()
}

// ReloadIfChanged re-reads the file ONLY when its modtime differs from the last
// load (cheap stat). The CLI mutates the file directly whether or not the daemon
// is running, so the work-loop calls this each tick to pick up out-of-process
// add/remove/enable/disable/run-now mutations without a socket op. Returns
// (true,nil) when a reload happened.
//
// Contract (NOT "no lost updates" — that was a false earlier claim): this
// overwrites in-memory state with the file's contents. Lost updates are
// prevented NOT by this reload but by every MUTATION being a flock-serialised
// read-modify-write (see mutate): each writer re-reads current disk state under
// LOCK_EX before applying its single change, so a stale in-memory snapshot can
// never clobber another process's committed write. ReloadIfChanged is a pure
// read-side convenience that lets a long-lived daemon pick up out-of-process
// config edits between ticks; the values it loads may be up to one tick stale
// relative to an in-flight write in another process.
func (s *Store) ReloadIfChanged() (bool, error) {
	info, err := os.Stat(s.filePath())
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("schedule: ReloadIfChanged: stat: %w", err)
	}
	s.mu.RLock()
	unchanged := info.ModTime().Equal(s.loadedMod)
	s.mu.RUnlock()
	if unchanged {
		return false, nil
	}
	if err := s.loadFromDisk(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) loadFromDisk() error {
	jobs, mod, err := s.readFileLocked()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.jobs = jobs
	s.loadedMod = mod
	s.mu.Unlock()
	return nil
}

func (s *Store) readFileLocked() (map[string]*ScheduledJob, time.Time, error) {
	path := s.filePath()
	//nolint:gosec // G304: path derived from projectDir/.harmonik/schedules.json
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*ScheduledJob), time.Time{}, nil
		}
		return nil, time.Time{}, fmt.Errorf("schedule: Load: read %q: %w", path, err)
	}
	var doc fileDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, time.Time{}, fmt.Errorf("schedule: Load: parse %q: %w", path, err)
	}
	var mod time.Time
	if info, statErr := os.Stat(path); statErr == nil {
		mod = info.ModTime()
	}
	jobs := make(map[string]*ScheduledJob, len(doc.Jobs))
	for i := range doc.Jobs {
		j := doc.Jobs[i]
		j.NormaliseDefaults()
		jobs[j.ID] = &j
	}
	return jobs, mod, nil
}

// Get returns a copy of the job with the given id, or (zero,false) if absent.
func (s *Store) Get(id string) (ScheduledJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return ScheduledJob{}, false
	}
	return *j, true
}

// List returns a snapshot of all jobs sorted by id (deterministic output).
func (s *Store) List() []ScheduledJob {
	s.mu.RLock()
	out := make([]ScheduledJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, *j)
	}
	s.mu.RUnlock()
	sort.Slice(out, func(i, k int) bool { return out[i].ID < out[k].ID })
	return out
}

func (s *Store) mutate(fn func(jobs map[string]*ScheduledJob) (changed bool, value any, err error)) (any, error) {
	lockFd, release, err := s.acquireFileLock()
	if err != nil {
		return nil, err
	}
	defer release()
	_ = lockFd

	jobs, _, err := s.readFileLocked()
	if err != nil {
		return nil, err
	}

	changed, value, err := fn(jobs)
	if err != nil {
		return value, err
	}
	if !changed {
		mod := s.statModtime()
		s.mu.Lock()
		s.jobs = jobs
		s.loadedMod = mod
		s.mu.Unlock()
		return value, nil
	}

	mod, err := s.persistJobs(jobs)
	if err != nil {
		return value, err
	}

	s.mu.Lock()
	s.jobs = jobs
	s.loadedMod = mod
	s.mu.Unlock()

	s.signalWake()
	return value, nil
}

// Add inserts or replaces a job by its ID under the cross-process flock, persists,
// and signals the wake channel. Defaults are normalised before storage. Returns
// an error if ID is empty or persistence fails.
func (s *Store) Add(j ScheduledJob) error {
	if j.ID == "" {
		return fmt.Errorf("schedule: Add: job id is required")
	}
	j.NormaliseDefaults()
	_, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		stored := j
		jobs[j.ID] = &stored
		return true, nil, nil
	})
	return err
}

// Remove deletes the job with the given id under the cross-process flock, persists,
// and signals the wake channel. Returns (false,nil) when the id is absent (no-op).
func (s *Store) Remove(id string) (bool, error) {
	v, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		if _, ok := jobs[id]; !ok {
			return false, false, nil
		}
		delete(jobs, id)
		return true, true, nil
	})
	if err != nil {
		return false, err
	}
	return mutationBool(v)
}

// SetEnabled flips a job's Enabled flag under the cross-process flock, persists,
// and signals the wake channel. Returns (false,nil) when the id is absent.
func (s *Store) SetEnabled(id string, enabled bool) (bool, error) {
	v, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		j, ok := jobs[id]
		if !ok {
			return false, false, nil
		}
		j.Enabled = enabled
		return true, true, nil
	})
	if err != nil {
		return false, err
	}
	return mutationBool(v)
}

// MarkFired overwrites a job's LastFire (RFC3339 UTC string) and LastPID under
// the cross-process flock, persists, and signals the wake channel. Returns
// (false,nil) when the id is absent.
//
// Every MarkFired call OVERWRITES LastPID — there is no "leave unchanged"
// sentinel. A caller that wants to preserve the existing pid (e.g. the
// missed-fire skip path, which records the skipped instant in LastFire but did
// not spawn a process) MUST pass the job's current LastPID; the daemon tick does
// exactly that. A command-action fire that SUCCEEDED passes the freshly spawned
// pid; a spawn-crew fire passes 0 (no command pid to track).
//
// A command-action fire that FAILED TO START also passes 0, deliberately, and
// that is the one caller which neither spawns a process nor preserves the prior
// pid (hk-pbdti). Zero is the honest record: no process exists, and overlapBlocks
// treats only LastPID > 0 as a prior run that may still be alive, so a start that
// produced nothing blocks nothing. Under the SKIP policy this cannot lose a live
// pid, because a live prior pid blocks before the fire is attempted at all. Under
// the ALLOW policy it can overwrite a live pid with 0 — harmless today, since
// allow returns before reading the field and nothing in the tree kills by
// LastPID, but an operator who flips allow to skip while that process is still
// running gets one skip evaluation that does not block.
//
// This is called by the work loop after a fire (or a missed-skip); it is the
// only mutation the daemon tick performs that records fire state.
func (s *Store) MarkFired(id, lastFireUTC string, pid int) (bool, error) {
	v, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		j, ok := jobs[id]
		if !ok {
			return false, false, nil
		}
		j.LastFire = lastFireUTC
		j.LastPID = pid
		return true, true, nil
	})
	if err != nil {
		return false, err
	}
	return mutationBool(v)
}

// RequestRunNow sets the ForceNext flag on a job (the `schedule run-now`
// mechanism) under the cross-process flock, persists, and signals the wake
// channel. Returns (false,nil) when the id is absent. The running daemon consumes
// and clears the flag on its next tick (honouring the overlap policy); when no
// daemon is up the flag fires on next boot.
func (s *Store) RequestRunNow(id string) (bool, error) {
	v, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		j, ok := jobs[id]
		if !ok {
			return false, false, nil
		}
		j.ForceNext = true
		return true, true, nil
	})
	if err != nil {
		return false, err
	}
	return mutationBool(v)
}

const sleepSuspendedFileName = "sleep-suspended-jobs.json"

// SuspendAllForSleep atomically disables all currently-enabled jobs under the
// cross-process flock, records their IDs in .harmonik/sleep-suspended-jobs.json,
// and returns the list of IDs that were disabled. Jobs already disabled before
// the sleep call are NOT recorded so that RestoreFromSleep does not inadvertently
// re-enable them. Called by the daemon's QuiesceArbiter on `harmonik sleep`.
func (s *Store) SuspendAllForSleep() ([]string, error) {
	var suspended []string
	_, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		var changed bool
		for _, j := range jobs {
			if j.Enabled {
				j.Enabled = false
				suspended = append(suspended, j.ID)
				changed = true
			}
		}
		return changed, nil, nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(suspended) // deterministic order
	if writeErr := s.writeSuspendedSet(suspended); writeErr != nil {
		return suspended, fmt.Errorf("schedule: SuspendAllForSleep: persist suspended set: %w", writeErr)
	}
	return suspended, nil
}

// RestoreFromSleep reads .harmonik/sleep-suspended-jobs.json and re-enables
// exactly the jobs recorded there. Jobs not in the suspended set (were already
// disabled before sleep) are left disabled. The suspended-set file is removed
// after a successful restore. Returns the list of IDs that were re-enabled.
// Called by the daemon's QuiesceArbiter on `harmonik wake --all`.
func (s *Store) RestoreFromSleep() ([]string, error) {
	suspended, err := s.readSuspendedSet()
	if err != nil {
		return nil, fmt.Errorf("schedule: RestoreFromSleep: read suspended set: %w", err)
	}
	if len(suspended) == 0 {
		return nil, nil
	}
	toEnable := make(map[string]struct{}, len(suspended))
	for _, id := range suspended {
		toEnable[id] = struct{}{}
	}
	var restored []string
	_, mutErr := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		var changed bool
		for _, j := range jobs {
			if _, ok := toEnable[j.ID]; ok && !j.Enabled {
				j.Enabled = true
				restored = append(restored, j.ID)
				changed = true
			}
		}
		return changed, nil, nil
	})
	if mutErr != nil {
		return nil, mutErr
	}
	sort.Strings(restored)
	if rmErr := os.Remove(s.suspendedSetPath()); rmErr != nil && !os.IsNotExist(rmErr) {
		return restored, fmt.Errorf("schedule: RestoreFromSleep: remove suspended set: %w", rmErr)
	}
	return restored, nil
}

func (s *Store) suspendedSetPath() string {
	return filepath.Join(s.projectDir, ".harmonik", sleepSuspendedFileName)
}

func (s *Store) writeSuspendedSet(ids []string) error {
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	dir := filepath.Join(s.projectDir, ".harmonik")
	if mkErr := os.MkdirAll(dir, core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("mkdir %q: %w", dir, mkErr)
	}
	path := s.suspendedSetPath()
	//nolint:gosec // G306: 0644 intentional — marker is world-readable within the project
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		return fmt.Errorf("write %q: %w", path, writeErr)
	}
	return nil
}

func (s *Store) readSuspendedSet() ([]string, error) {
	path := s.suspendedSetPath()
	//nolint:gosec // G304: path derived from projectDir/.harmonik
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	var ids []string
	if jErr := json.Unmarshal(data, &ids); jErr != nil {
		return nil, fmt.Errorf("parse %q: %w", path, jErr)
	}
	return ids, nil
}

// ClearForceNext clears a job's ForceNext flag under the cross-process flock,
// persists, and signals the wake channel. Called by the daemon after consuming a
// run-now request. Returns (false,nil) when the id is absent; (true,nil) with no
// write when the flag was already clear.
func (s *Store) ClearForceNext(id string) (bool, error) {
	v, err := s.mutate(func(jobs map[string]*ScheduledJob) (bool, any, error) {
		j, ok := jobs[id]
		if !ok {
			return false, false, nil
		}
		if !j.ForceNext {
			return false, true, nil // already clear; no write needed
		}
		j.ForceNext = false
		return true, true, nil
	})
	if err != nil {
		return false, err
	}
	return mutationBool(v)
}

func mutationBool(v any) (bool, error) {
	changed, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("schedule: internal mutation result has type %T, want bool", v)
	}
	return changed, nil
}

func (s *Store) filePath() string {
	return filepath.Join(s.projectDir, ".harmonik", scheduleFileName)
}

func (s *Store) lockPath() string {
	return filepath.Join(s.projectDir, ".harmonik", scheduleLockName)
}

func (s *Store) statModtime() time.Time {
	if info, err := os.Stat(s.filePath()); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func (s *Store) acquireFileLock() (*os.File, func(), error) {
	dir := filepath.Join(s.projectDir, ".harmonik")
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		return nil, nil, fmt.Errorf("schedule: lock: mkdir %q: %w", dir, err)
	}
	lockPath := s.lockPath()
	//nolint:gosec // G304: sidecar lockfile path is derived from projectDir/.harmonik
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("schedule: lock: open %q: %w", lockPath, err)
	}
	if err := acquireExclusiveBounded(int(fd.Fd()), scheduleLockTimeout); err != nil {
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "schedule: acquireFileLock: close lockfile fd after acquire failure", "err", closeErr, "path", lockPath)
		}
		return nil, nil, err
	}
	release := func() {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN) //nolint:errcheck // unlock error non-actionable; close also drops the advisory lock
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(context.Background(), "schedule: acquireFileLock: close lockfile fd on release", "err", closeErr, "path", lockPath)
		}
	}
	return fd, release, nil
}

func acquireExclusiveBounded(fd int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("schedule: flock LOCK_EX: %w", err)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("schedule: flock LOCK_EX: acquire timed out after %s (contended %s)", timeout, scheduleLockName)
		}
		time.Sleep(scheduleLockRetryInterval)
	}
}

func (s *Store) persistJobs(jobsMap map[string]*ScheduledJob) (time.Time, error) {
	jobs := make([]ScheduledJob, 0, len(jobsMap))
	for _, j := range jobsMap {
		jobs = append(jobs, *j)
	}
	sort.Slice(jobs, func(i, k int) bool { return jobs[i].ID < jobs[k].ID })

	data, err := json.MarshalIndent(fileDoc{SchemaVersion: fileSchemaVersion, Jobs: jobs}, "", "  ")
	if err != nil {
		return time.Time{}, fmt.Errorf("schedule: persist: marshal: %w", err)
	}

	dir := filepath.Join(s.projectDir, ".harmonik")
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		return time.Time{}, fmt.Errorf("schedule: persist: mkdir %q: %w", dir, err)
	}

	target := s.filePath()
	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: tmpPath derived from projectDir/.harmonik + Getpid
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o600)
	if err != nil {
		return time.Time{}, fmt.Errorf("schedule: persist: create temp %q: %w", tmpPath, err)
	}
	if _, err := f.Write(data); err != nil {
		err = errors.Join(err, f.Close())
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on write failure
		return time.Time{}, fmt.Errorf("schedule: persist: write temp %q: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		err = errors.Join(err, f.Close())
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on sync failure
		return time.Time{}, fmt.Errorf("schedule: persist: fsync temp %q: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on close failure
		return time.Time{}, fmt.Errorf("schedule: persist: close temp %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on rename failure
		return time.Time{}, fmt.Errorf("schedule: persist: rename %q → %q: %w", tmpPath, target, err)
	}

	mod := s.statModtime()

	// fsync parent dir so the rename is durable.
	//nolint:gosec // G304: dir is the daemon-internal .harmonik directory
	d, err := os.Open(dir)
	if err != nil {
		return mod, fmt.Errorf("schedule: persist: open parent dir %q: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		err = errors.Join(err, d.Close())
		return mod, fmt.Errorf("schedule: persist: fsync parent dir %q: %w", dir, err)
	}
	if err := d.Close(); err != nil {
		return mod, fmt.Errorf("schedule: persist: close parent dir %q: %w", dir, err)
	}
	return mod, nil
}
