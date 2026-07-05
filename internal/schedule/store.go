package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
)

// scheduleFileName is the durable store file under .harmonik/.
const scheduleFileName = "schedules.json"

// scheduleLockName is the sidecar advisory-lock file under .harmonik/. The lock
// is taken on this file (not on schedules.json itself) so the lock identity is
// stable across the atomic temp-then-rename writes of schedules.json (a rename
// changes the target's inode; flock on the renamed-away inode would protect
// nothing). Mirrors the sidecar-lockfile pattern in
// internal/workspace/claudetrust_wm040b.go (hk-bfvby).
const scheduleLockName = "schedules.json.lock"

// scheduleLockTimeout bounds how long a mutation waits to acquire the
// cross-process write lock before failing. A schedule mutation is a tiny
// read-modify-write over a small JSON file, so real contention is brief
// (sub-millisecond); the timeout exists only to convert a pathological stuck
// holder into a prompt error rather than an indefinite hang.
const scheduleLockTimeout = 10 * time.Second

// scheduleLockRetryInterval is the poll interval for the bounded
// LOCK_EX|LOCK_NB acquire loop.
const scheduleLockRetryInterval = 25 * time.Millisecond

// wakeBufSize is the buffer depth for the wake channel. Buffer of 1 ensures a
// non-blocking send never blocks and coalesces rapid bursts into a single
// wakeup (mirrors QueueStore.submitWakeCBufSize / hk-24xn1).
const wakeBufSize = 1

// fileDoc is the on-disk JSON envelope for the schedule store.
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

// signalWake performs a non-blocking coalescing send on the wake channel.
// Caller must NOT hold the lock (matches QueueStore.SetQueue ordering).
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

// loadFromDisk replaces the in-memory map with the file's contents and records
// the file modtime. Absent file → empty store (not an error).
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

// readFileLocked reads and parses schedules.json into a fresh id-keyed map plus
// the file's modtime. It performs no in-memory locking — callers hold the flock
// (during a mutation) or accept the read may race a concurrent writer's atomic
// rename (which only ever yields a whole old or whole new file, never a torn
// one). Absent file → empty map + zero modtime (not an error).
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

// mutate runs fn as a cross-process-safe read-modify-write:
//
//  1. acquire the advisory flock (LOCK_EX) on the sidecar lockfile (cross-process
//     serialisation),
//  2. reload the jobs map FROM DISK so fn sees current committed state (not a
//     stale in-memory snapshot) — this is what eliminates the lost-update window,
//  3. apply fn(jobs) (the single mutation),
//  4. atomically persist the result and record the new modtime,
//  5. swap the fresh map into memory under mu,
//  6. release the flock,
//  7. signal the wake channel (outside all locks).
//
// fn returns (changed, value, err): when changed is false no write happens (fn
// observed a no-op, e.g. id absent or flag already in the wanted state) and the
// returned value is propagated to the caller unchanged. The flock is held for the
// whole RMW; mu is taken only for the brief map-read in fn-support and the final
// swap, always AFTER the flock — so the acquisition order is flock→mu and there
// is no deadlock.
func (s *Store) mutate(fn func(jobs map[string]*ScheduledJob) (changed bool, value any, err error)) (any, error) {
	lockFd, release, err := s.acquireFileLock()
	if err != nil {
		return nil, err
	}
	defer release()
	_ = lockFd

	// Re-read current disk state under the flock so this mutation applies on top
	// of any write another process committed since our last load.
	jobs, _, err := s.readFileLocked()
	if err != nil {
		return nil, err
	}

	changed, value, err := fn(jobs)
	if err != nil {
		return value, err
	}
	if !changed {
		// No-op: still adopt the freshly-read disk state in memory so subsequent
		// reads reflect any out-of-process changes, but do not rewrite the file.
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
	return v.(bool), nil
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
	return v.(bool), nil
}

// MarkFired overwrites a job's LastFire (RFC3339 UTC string) and LastPID under
// the cross-process flock, persists, and signals the wake channel. Returns
// (false,nil) when the id is absent.
//
// Every MarkFired call OVERWRITES LastPID — there is no "leave unchanged"
// sentinel. A caller that wants to preserve the existing pid (e.g. the
// missed-fire skip path, which records the skipped instant in LastFire but did
// not spawn a process) MUST pass the job's current LastPID; the daemon tick does
// exactly that. A command-action fire passes the freshly spawned pid; a
// spawn-crew fire passes 0 (no command pid to track).
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
	return v.(bool), nil
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
	return v.(bool), nil
}

// sleepSuspendedFileName is the sidecar file under .harmonik/ that records
// which job IDs were disabled by SuspendAllForSleep so that RestoreFromSleep
// can re-enable exactly those jobs — no more, no less.
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

// suspendedSetPath returns the path of the sleep-suspended-jobs sidecar file.
func (s *Store) suspendedSetPath() string {
	return filepath.Join(s.projectDir, ".harmonik", sleepSuspendedFileName)
}

// writeSuspendedSet persists the given job ID slice to the suspended-set file.
func (s *Store) writeSuspendedSet(ids []string) error {
	data, err := json.Marshal(ids)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	dir := filepath.Join(s.projectDir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return fmt.Errorf("mkdir %q: %w", dir, mkErr)
	}
	path := s.suspendedSetPath()
	//nolint:gosec // G306: 0644 intentional — marker is world-readable within the project
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		return fmt.Errorf("write %q: %w", path, writeErr)
	}
	return nil
}

// readSuspendedSet reads and parses the suspended-set file. Returns nil (not
// an error) when the file is absent (no prior sleep or already restored).
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
	return v.(bool), nil
}

// filePath returns the absolute path of the durable store file.
func (s *Store) filePath() string {
	return filepath.Join(s.projectDir, ".harmonik", scheduleFileName)
}

// lockPath returns the absolute path of the sidecar advisory-lock file.
func (s *Store) lockPath() string {
	return filepath.Join(s.projectDir, ".harmonik", scheduleLockName)
}

// statModtime returns the current modtime of schedules.json, or the zero time if
// it cannot be stat'd (absent file).
func (s *Store) statModtime() time.Time {
	if info, err := os.Stat(s.filePath()); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

// acquireFileLock opens (creating if needed) the sidecar lockfile and takes a
// bounded advisory exclusive flock on it, returning the open fd and a release
// closure that unlocks and closes it. The sidecar lockfile lives in .harmonik/,
// which acquireFileLock ensures exists. Mirrors the bounded LOCK_EX|LOCK_NB
// idiom in internal/workspace/claudetrust_wm040b.go (hk-bfvby) so a stuck holder
// surfaces as a prompt error rather than an indefinite hang.
func (s *Store) acquireFileLock() (*os.File, func(), error) {
	dir := filepath.Join(s.projectDir, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("schedule: lock: mkdir %q: %w", dir, err)
	}
	lockPath := s.lockPath()
	//nolint:gosec // G304: sidecar lockfile path is derived from projectDir/.harmonik
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("schedule: lock: open %q: %w", lockPath, err)
	}
	if err := acquireExclusiveBounded(int(fd.Fd()), scheduleLockTimeout); err != nil {
		_ = fd.Close() //nolint:errcheck // closing on acquire failure; error non-actionable
		return nil, nil, err
	}
	release := func() {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN) //nolint:errcheck // unlock error non-actionable; close also drops the advisory lock
		_ = fd.Close()                                   //nolint:errcheck // closing a lock fd; error non-actionable
	}
	return fd, release, nil
}

// acquireExclusiveBounded acquires an advisory exclusive flock on fd, retrying
// the non-blocking LOCK_EX|LOCK_NB attempt every scheduleLockRetryInterval until
// it succeeds or timeout elapses. On timeout it returns an error so the caller
// fails fast rather than blocking indefinitely behind an unfair flock waiter.
// Mirrors internal/workspace/claudetrust_wm040b.go.acquireExclusiveBounded.
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

// persistJobs writes the given jobs map to .harmonik/schedules.json atomically
// (write-temp → fsync → rename → fsync parent dir), mirroring queue.Persist, and
// returns the just-written modtime. The caller MUST hold the advisory flock (see
// mutate) so no other process writes the file concurrently.
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
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
		_ = f.Close()
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup on write failure
		return time.Time{}, fmt.Errorf("schedule: persist: write temp %q: %w", tmpPath, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
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

	// Record the just-written modtime so ReloadIfChanged does not re-read our own
	// write (the daemon's MarkFired/ClearForceNext go through this path).
	mod := s.statModtime()

	// fsync parent dir so the rename is durable.
	//nolint:gosec // G304: dir is the daemon-internal .harmonik directory
	d, err := os.Open(dir)
	if err != nil {
		return mod, fmt.Errorf("schedule: persist: open parent dir %q: %w", dir, err)
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return mod, fmt.Errorf("schedule: persist: fsync parent dir %q: %w", dir, err)
	}
	if err := d.Close(); err != nil {
		return mod, fmt.Errorf("schedule: persist: close parent dir %q: %w", dir, err)
	}
	return mod, nil
}
