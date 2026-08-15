package workspace

// claudetrust_retry_test.go — the trust writer must confirm its own write landed
// on disk, and repair it when it did not.
//
// WHAT WENT WRONG, twice.
//
// First in production. At max-concurrent 3, two of three workers parked on Claude
// Code's folder-trust modal. That modal renders BEFORE SessionStart, so the hook
// never fires, agent_ready is never synthesized, and the run dies at its ready
// deadline with nothing saying why. On disk the failed worktree's
// projects[<realpath>].hasTrustDialogAccepted was ABSENT even though
// EnsureWorktreeTrust had run and reported success. The clobberer is Claude Code
// itself: a live claude process rewrites the shared config wholesale from its own
// in-memory snapshot and does not take our advisory lock. More locking cannot
// exclude a writer that never asks for the lock, so ensureWorktreeTrustAt writes,
// RE-READS to verify, retries the whole read-modify-write on a lost key, and
// fails structurally when the key never sticks.
//
// Then in the test suite. The unit tests for that loop were removed in a sweep
// that deleted 681 test files, and nothing replaced them. trustPostWriteHook —
// the seam the production file carries for exactly this purpose, invoked between
// the write and the verifying re-read — was left with zero users, so every
// behavior below was unmeasured. This file re-adopts the seam.
//
// The claims:
//
//  1. with nobody clobbering, the key persists and the config is written exactly
//     ONCE — verification must not cost extra writes in the normal case;
//  2. a clobber that erases our key once is detected and repaired, and the repair
//     lands on top of the other writer's file rather than reverting it;
//  3. a clobber that erases our key after EVERY write exhausts the bounded
//     attempts and returns a structural error, never a success the disk does not
//     support;
//  4. an already-trusted path short-circuits on the lock-free probe — it does not
//     write, it does not create the sidecar lockfile, and it does not queue
//     behind an in-process write that already holds trustWriteMu; and
//  5. a config that stays unparseable keeps the loop iterating for its whole
//     budget, then reports the decode error itself, and is left byte-for-byte as
//     it was found;
//  6. repeated clobbers each get their own fresh read, so the loop does not fall
//     back on a stale snapshot once it has retried a first time; and
//  7. a config that is unparseable in ONE snapshot because a foreign writer is
//     mid-rewrite is READ AGAIN and repaired once it settles.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// withTrustPostWriteHook installs fn as the post-write seam for the duration of
// the test. It takes trustWriteMu for the swap because the write path reads the
// var while holding that mutex — that is what makes the access race-free.
//
// The tests here are NOT parallel: the hook is one package-level var, so a
// sequential test owns it exclusively (Go pauses t.Parallel() tests for the
// duration of the sequential ones).
//
// fn runs on whatever goroutine reached the write, and the hooks built below
// report failures through t.Fatalf, which Go documents as callable only from the
// test goroutine. So a test that drives ensureWorktreeTrustAt from a goroutine it
// spawned must not install one of those hooks.
func withTrustPostWriteHook(t *testing.T, fn func(cfgPath string)) {
	t.Helper()
	trustWriteMu.Lock()
	prev := trustPostWriteHook
	trustPostWriteHook = fn
	trustWriteMu.Unlock()
	t.Cleanup(func() {
		trustWriteMu.Lock()
		trustPostWriteHook = prev
		trustWriteMu.Unlock()
	})
}

// clobberAddedProject is a project key the clobbering writer INVENTS, and
// clobberGenerationKey is a top-level key it bumps on every clobber. Both are
// deliberately content no snapshot the trust writer read before its own write
// can contain.
const (
	clobberAddedProject  = "/added/by/the/clobberer"
	clobberGenerationKey = "clobbererGeneration"
)

// clobberTrustEntry rewrites cfgPath the way a live claude process does: it drops
// worktreePath's trust entry (the key it never knew about) and, critically, ALSO
// COMMITS CONTENT OF ITS OWN — a new project entry plus a bumped generation
// counter.
//
// The added content is what makes the repair assertion mean something. If the
// trust writer retried from a snapshot it took before its first write instead of
// re-reading disk, a delete-only clobber would be undetectable: writing back the
// stale snapshot is byte-indistinguishable from re-applying onto a fresh read,
// because the snapshot already holds everything except our own key. Adding
// content breaks that symmetry — a stale-snapshot retry silently erases
// generation n, which is the lost-update behavior that would make harmonik the
// clobberer of a sibling worker's trust entry.
func clobberTrustEntry(t *testing.T, cfgPath, worktreePath string, generation int) {
	t.Helper()
	cfg := readConfigMap(t, cfgPath)
	projects, ok := jsonObject(cfg, "projects")
	if !ok {
		projects = map[string]any{}
		cfg["projects"] = projects
	}
	delete(projects, worktreePath)
	projects[clobberAddedProject] = map[string]any{
		"hasTrustDialogAccepted": true,
		"generation":             float64(generation),
	}
	cfg[clobberGenerationKey] = float64(generation)
	writeConfigMap(t, cfgPath, cfg)
}

// trustClobberHook builds a post-write hook that clobbers cfgPath on its first
// `times` invocations and leaves it alone afterwards.
//
// *writes counts every time the write path reached the post-write point. That is
// a count of WRITES THAT REACHED THE FILE, not of loop iterations: the seam fires
// after the atomic rename, and an attempt that finds the entry already trusted
// under the lock returns before it, having written nothing. Writes to any OTHER
// config path are ignored, so the shared hook cannot disturb an unrelated test.
//
// *clobbers reports the last generation the clobberer committed.
func trustClobberHook(t *testing.T, cfgPath, worktreePath string, times int, writes, clobbers *int) func(string) {
	t.Helper()
	return func(gotPath string) {
		if gotPath != cfgPath {
			return
		}
		*writes++
		if *writes <= times {
			*clobbers++
			clobberTrustEntry(t, cfgPath, worktreePath, *clobbers)
		}
	}
}

// assertClobberContentSurvived asserts that everything the clobbering writer
// committed at `generation` is still in the config. This is the
// merge-onto-fresh-read property: the repair write must land on top of the other
// writer's file, never revert it.
func assertClobberContentSurvived(t *testing.T, cfgPath string, generation int) {
	t.Helper()
	cfg := readConfigMap(t, cfgPath)
	if got, want := cfg[clobberGenerationKey], float64(generation); got != want {
		t.Errorf("%s = %v, want %v — the repair write reverted the other writer's file instead of "+
			"merging onto it, which is the lost update that makes harmonik the clobberer",
			clobberGenerationKey, got, want)
	}
	projects := mustJSONObject(t, cfg, "projects", "config after the repair")
	entry, ok := jsonObject(projects, clobberAddedProject)
	if !ok {
		t.Fatalf("the clobberer's own project entry %q was erased by the repair write — a sibling "+
			"worker's trust entry would be lost the same way", clobberAddedProject)
	}
	if got, want := entry["generation"], float64(generation); got != want {
		t.Errorf("the clobberer's project entry generation = %v, want %v; a stale snapshot was re-applied", got, want)
	}
}

// trustedInConfig reports whether cfgPath records worktreePath as trusted. It
// reads the file itself rather than calling alreadyTrustedAt, so the assertion
// does not depend on the function the test is checking.
func trustedInConfig(t *testing.T, cfgPath, worktreePath string) bool {
	t.Helper()
	cfg := readConfigMap(t, cfgPath)
	projects, _ := jsonObject(cfg, "projects")
	entry, _ := jsonObject(projects, worktreePath)
	return trustDialogAccepted(entry)
}

// readConfigMap parses cfgPath as a JSON object or fails the test.
func readConfigMap(t *testing.T, cfgPath string) map[string]any {
	t.Helper()
	data := mustReadFile(t, cfgPath)
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("parse %s: %v\nraw: %s", cfgPath, err, data)
	}
	return cfg
}

// writeConfigMap writes cfg to cfgPath in the same indented shape Claude Code and
// the trust writer both use.
func writeConfigMap(t *testing.T, cfgPath string, cfg map[string]any) {
	t.Helper()
	if err := os.WriteFile(cfgPath, marshalConfig(t, cfgPath, cfg), 0o600); err != nil {
		t.Fatalf("write %s: %v", cfgPath, err)
	}
}

// marshalConfig renders cfg as the bytes writeConfigMap would put on disk. The
// torn-read fixture needs the bytes themselves, so it can put half of them there.
func marshalConfig(t *testing.T, cfgPath string, cfg map[string]any) []byte {
	t.Helper()
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config for %s: %v", cfgPath, err)
	}
	return append(out, '\n')
}

// TestTrustRetry_UnclobberedWriteIsNotRepeated is claim 1, and the baseline the
// other tests are read against: with nobody rewriting the config, the key
// persists, the verification read passes the first time, and the file is written
// once. The write count is the assertion that matters — a verify-and-repair loop
// that costs extra writes when nothing is wrong would put the daemon back on the
// write-contention path the lock-free probe took it off.
//
// The count is of writes, not of loop iterations, because the seam fires after
// the rename. An extra iteration that finds the entry already trusted under the
// lock writes nothing and is invisible here, which is the right reading: an extra
// iteration costs the backoff, an extra WRITE costs the contention.
func TestTrustRetry_UnclobberedWriteIsNotRepeated(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-settles")

	writes, clobbers := 0, 0
	withTrustPostWriteHook(t, trustClobberHook(t, cfgPath, worktreePath, 0, &writes, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("an unclobbered trust write failed: %v", err)
	}
	if writes != 1 {
		t.Errorf("the writer rewrote the config %d times, want exactly 1; extra writes when nothing "+
			"is wrong are what made the shared config a contention point", writes)
	}
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("the call reported success but projects[%q].hasTrustDialogAccepted is not on disk", worktreePath)
	}
}

// TestTrustRetry_TransientClobberIsRepaired is claim 2. An external writer erases
// our key once, in the window between our write and our verification read. The
// re-read must catch it and the retry must re-apply the key ON TOP of the
// clobberer's file, so the call succeeds, the key is on disk, and nothing the
// other writer committed is lost.
func TestTrustRetry_TransientClobberIsRepaired(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-transient")

	// Content that predates our first read. Preserving it only proves the write
	// merges; the generation assertion below is the one that proves the RETRY
	// re-reads.
	writeConfigMap(t, cfgPath, map[string]any{
		"theme": "dark",
		"projects": map[string]any{
			"/some/other/project": map[string]any{"hasTrustDialogAccepted": true},
		},
	})

	writes, clobbers := 0, 0
	withTrustPostWriteHook(t, trustClobberHook(t, cfgPath, worktreePath, 1, &writes, &clobbers))

	start := time.Now()
	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("a single clobber must be repaired, not reported as a failure: %v", err)
	}
	if writes != 2 {
		t.Errorf("the writer rewrote the config %d times after one clobber, want 2 "+
			"(the initial write and the repair)", writes)
	}
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("the call reported success but projects[%q].hasTrustDialogAccepted is not on disk "+
			"after the repair", worktreePath)
	}
	if elapsed < trustWriteRetryBackoff {
		t.Errorf("the repair took %v, less than one backoff (%v) — the retry did not wait for the "+
			"competing writer's rename to land", elapsed, trustWriteRetryBackoff)
	}

	cfg := readConfigMap(t, cfgPath)
	if cfg["theme"] != "dark" {
		t.Errorf("the repair dropped an unrelated top-level key; theme = %v", cfg["theme"])
	}
	projects := mustJSONObject(t, cfg, "projects", "config after the repair")
	if _, ok := projects["/some/other/project"]; !ok {
		t.Error("the repair dropped an unrelated project entry")
	}

	// The discriminating assertion: content the clobberer committed AFTER our
	// first read must survive too. Only a retry that re-reads disk can preserve
	// it — a retry that re-applied its own earlier snapshot would silently revert
	// it, and the two are indistinguishable without this check.
	if clobbers != 1 {
		t.Fatalf("the clobberer fired %d times, want 1; the fixture is wrong and the assertions below "+
			"do not mean what they say", clobbers)
	}
	assertClobberContentSurvived(t, cfgPath, clobbers)
}

// TestTrustRetry_MultiRoundClobberMergesEachTime is claim 6, and it is the one
// the deleted MultiRoundClobber test covered. Claim 2 proves the merge-onto-a-
// fresh-read property for ONE repair. This proves the loop does not degrade
// across repairs.
//
// The failure it exists to catch: a loop that re-reads on its first retry and
// then reuses THAT snapshot for the rest of the budget. Claim 2 cannot see it,
// because with one clobber there is no second retry to get wrong. Here the
// clobberer commits generation 1, then generation 2, and only a repair that
// re-read before its THIRD write can carry generation 2 through. A loop holding
// the generation-1 snapshot would write it back and silently revert the newer
// one, which is the lost update that makes harmonik the clobberer of a sibling
// worker's trust entry.
func TestTrustRetry_MultiRoundClobberMergesEachTime(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-multi-round")

	writeConfigMap(t, cfgPath, map[string]any{
		"projects": map[string]any{
			"/some/other/project": map[string]any{"hasTrustDialogAccepted": true},
		},
	})

	// Two clobbers, which the 4-attempt budget absorbs: write, clobber, write,
	// clobber, write, and the third one sticks.
	writes, clobbers := 0, 0
	withTrustPostWriteHook(t, trustClobberHook(t, cfgPath, worktreePath, 2, &writes, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("two clobbers are inside the attempt budget and must be repaired, not reported as a "+
			"failure: %v", err)
	}
	if writes != 3 {
		t.Errorf("the writer rewrote the config %d times after two clobbers, want 3 "+
			"(the initial write and two repairs)", writes)
	}
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("the call reported success but projects[%q].hasTrustDialogAccepted is not on disk "+
			"after the second repair", worktreePath)
	}
	// The fixture guard: the assertion below names a generation, so the clobberer
	// must actually have reached it.
	if clobbers != 2 {
		t.Fatalf("the clobberer fired %d times, want 2; the fixture is wrong and the assertion below "+
			"does not mean what it says", clobbers)
	}
	// Generation 2, not 1. This is the whole point of the test.
	assertClobberContentSurvived(t, cfgPath, clobbers)
}

// TestTrustRetry_PersistentClobberFailsStructurally is claim 3. The external
// writer erases our key after every write, so the key can never stick. The
// bounded attempts must run out and the call must FAIL — reporting success here
// is the original defect: the launch proceeds, claude parks on the trust modal,
// and the run dies at the agent_ready deadline with nothing explaining why.
//
// This test costs about 600ms, which is three backoffs. That is the price of
// measuring the real loop rather than a stubbed one.
func TestTrustRetry_PersistentClobberFailsStructurally(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-persistent")

	writes, clobbers := 0, 0
	// Clobber every attempt: the count is larger than any attempt budget.
	withTrustPostWriteHook(t, trustClobberHook(t, cfgPath, worktreePath, 1<<30, &writes, &clobbers))

	err := ensureWorktreeTrustAt(worktreePath, cfgPath)

	if err == nil {
		t.Fatal("a write that was erased every single time reported success; the caller then execs " +
			"claude into an untrusted folder and the run dies at its ready deadline")
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Errorf("the error does not classify as structural, so the dispatch path will not stop the "+
			"launch: %v", err)
	}
	if writes != trustWriteMaxAttempts {
		t.Errorf("the writer rewrote the config %d times, want one per attempt in the budget "+
			"(trustWriteMaxAttempts = %d); a loop that gives up early leaves a repairable launch broken",
			writes, trustWriteMaxAttempts)
	}
	// The message is the only artifact whoever hits this failure will read. It has
	// to name what did not persist and what most likely did it.
	if !strings.Contains(err.Error(), "did not persist") || !strings.Contains(err.Error(), "concurrent writer") {
		t.Errorf("the error does not explain the failure to whoever finds it: %v", err)
	}
	// And the disk really is untrusted, so this is a true failure and not a broken
	// verification read reporting one.
	if trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("the call returned an error although projects[%q].hasTrustDialogAccepted IS on disk", worktreePath)
	}
}

// TestTrustRetry_AlreadyTrustedPathTakesNoWrite is claim 4. The lock-free probe
// exists so a repeat launch never joins the write contention on the shared
// config. A verify-and-repair loop that ran anyway would undo that.
//
// Three witnesses, because the obvious two are not enough. The hook firing means
// a write happened and the mtime would show a rewrite with identical content —
// but BOTH go quiet if the two trusted-state checks are removed, since
// trustUpsertOnce re-checks under the flock and returns before the write. Only
// the sidecar lockfile sees that far: trustUpsertOnce opens it with O_CREATE and
// nothing ever unlinks it, so the file on disk is a durable record that the write
// path was entered at all.
//
// What no witness here can see is the LOCK-FREE half of the claim. Delete only
// the first probe and the second check — the one under trustWriteMu — still
// returns before any lockfile or write, so every assertion below stays green.
// TestTrustRetry_AlreadyTrustedPathDoesNotWaitOnAWriteInFlight is the test that
// measures that, and it is the one to read next.
func TestTrustRetry_AlreadyTrustedPathTakesNoWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-trusted")

	writeConfigMap(t, cfgPath, map[string]any{
		"projects": map[string]any{
			worktreePath: map[string]any{"hasTrustDialogAccepted": true},
		},
	})
	// Backdate the file so a rewrite is visible even at a coarse mtime resolution.
	old := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(cfgPath, old, old); err != nil {
		t.Fatalf("backdate %s: %v", cfgPath, err)
	}
	before := configMtime(t, cfgPath)

	writes, clobbers := 0, 0
	withTrustPostWriteHook(t, trustClobberHook(t, cfgPath, worktreePath, 0, &writes, &clobbers))

	if err := ensureWorktreeTrustAt(worktreePath, cfgPath); err != nil {
		t.Fatalf("an already-trusted path failed: %v", err)
	}
	if writes != 0 {
		t.Errorf("an already-trusted path rewrote the config %d times, want 0; the lock-free probe "+
			"must short-circuit before the write path", writes)
	}
	if after := configMtime(t, cfgPath); !after.Equal(before) {
		t.Errorf("an already-trusted path rewrote the config (mtime %v -> %v)", before, after)
	}
	// trustUpsertOnce is the only thing that creates this file on the path under
	// test, so its absence proves the probe returned before the write path.
	// (pruneWorktreeTrustAt opens the same lockfile, and nothing here calls it.)
	if _, statErr := os.Stat(cfgPath + ".lock"); !os.IsNotExist(statErr) {
		t.Errorf("an already-trusted path created the sidecar lockfile %s (stat err %v), so it took the "+
			"advisory flock and joined the write contention the lock-free probe exists to avoid",
			cfgPath+".lock", statErr)
	}
	// Positive evidence that the fixture really was trusted, so "no write" is the
	// short-circuit and not a call that failed to reach anything.
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("projects[%q].hasTrustDialogAccepted is not on disk, so this test never exercised "+
			"the already-trusted path", worktreePath)
	}
}

// trustFastPathWait bounds how long an already-trusted call may take while
// another trust write holds trustWriteMu. The fast path answers from one
// read-only stat-and-parse, so it is orders of magnitude under this. The margin
// is for a loaded CI box, not for the behavior under test.
const trustFastPathWait = 2 * time.Second

// TestTrustRetry_AlreadyTrustedPathDoesNotWaitOnAWriteInFlight is the other half
// of claim 4, and the only test that can see it.
//
// The point of the lock-free probe is not merely that a repeat launch skips the
// write. It is that a repeat launch does not QUEUE behind one. trustWriteMu is
// held across the whole write-verify-repair loop, backoffs included — up to about
// 15.6 seconds — so a launch that took the mutex before checking whether it had
// anything to do would stall for that long behind an unrelated first-time
// worktree, on the path every repeat launch takes.
//
// Holding trustWriteMu here is a stand-in for that in-flight write. With the
// probe in place the call answers from its own read and returns at once. Without
// it, the call blocks until this test releases the mutex, which is the failure
// this test exists to name — and no assertion on writes, mtime, or the lockfile
// can distinguish the two, because the second check returns before all three.
func TestTrustRetry_AlreadyTrustedPathDoesNotWaitOnAWriteInFlight(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-repeat-launch")

	writeConfigMap(t, cfgPath, map[string]any{
		"projects": map[string]any{
			worktreePath: map[string]any{"hasTrustDialogAccepted": true},
		},
	})

	// No post-write hook here, deliberately. This is the one test that drives the
	// write path from a SPAWNED goroutine, and the hook's helpers report through
	// t.Fatalf, which Go documents as callable only from the test goroutine. The
	// "no write happens" half of claim 4 is already measured by
	// TestTrustRetry_AlreadyTrustedPathTakesNoWrite, on the test goroutine.
	trustWriteMu.Lock()

	done := make(chan error, 1)
	go func() { done <- ensureWorktreeTrustAt(worktreePath, cfgPath) }()

	// t.Errorf, never t.Fatalf, until the mutex is released below. A Fatalf here
	// ends the goroutine with trustWriteMu still held, and every later test that
	// needs a trust write then blocks forever.
	var blocked bool
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("an already-trusted path failed: %v", err)
		}
	case <-time.After(trustFastPathWait):
		blocked = true
	}

	trustWriteMu.Unlock()

	if blocked {
		// The call was only waiting, so it completes now. Drain it rather than
		// leaving the goroutine to write into a TempDir this test is about to
		// remove. The wait is bounded: so is the write loop.
		<-done
		t.Fatalf("an already-trusted path did not return within %v while another trust write held "+
			"trustWriteMu; it is queueing behind in-process writes instead of answering from the "+
			"lock-free probe, so every repeat launch can stall for the write loop's full budget",
			trustFastPathWait)
	}
	// Positive evidence the fixture really was trusted, so a prompt return is the
	// fast path and not a call that failed to reach anything.
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("projects[%q].hasTrustDialogAccepted is not on disk, so this test never exercised "+
			"the already-trusted path", worktreePath)
	}
}

// configMtime returns cfgPath's modification time.
func configMtime(t *testing.T, cfgPath string) time.Time {
	t.Helper()
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat %s: %v", cfgPath, err)
	}
	return info.ModTime()
}

// tornSettleDelay is how long the foreign writer's rewrite stays half-landed. It
// must fall between the verifying re-read that follows the tear (the very next
// thing the loop does, microseconds later) and the next attempt's read, one full
// backoff away. A quarter of the backoff sits far from both ends.
const tornSettleDelay = trustWriteRetryBackoff / 4

// TestTrustRetry_TornReadRecovers is claim 7, and it is the reason the decode
// carve-out exists at all. Claim 5 covers the giving-up half: a file that never
// parses is reported. This covers the half that pays for it — a file that is
// unparseable in ONE snapshot because a foreign writer is mid-rewrite, and
// parses on the next read.
//
// Without this test the carve-out can be made STICKY — keep the loop iterating
// and sleeping, but reuse the first decode error instead of reading again — and
// nothing goes red, claim 5 included. Claim 5 cannot see it, because a file that
// never parses gives the same answer either way.
//
// The tear is anchored to the WRITE, not to the call: the hook fires immediately
// after the atomic rename, so tearing the file there guarantees the verifying
// re-read that follows sees the torn bytes. The settle then lands a quarter of a
// backoff later, long before the next attempt reads.
func TestTrustRetry_TornReadRecovers(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-torn-read")

	// What the foreign writer's COMPLETED rewrite looks like: valid, carrying its
	// own content, and with no trust entry of ours — so the loop still has work to
	// do once it can read the file again.
	settled := marshalConfig(t, cfgPath, map[string]any{
		"theme": "dark",
		"projects": map[string]any{
			clobberAddedProject: map[string]any{"hasTrustDialogAccepted": true},
		},
	})
	// And what a reader sees in the middle of it, since that writer does not
	// rename atomically.
	torn := settled[:len(settled)/2]
	if json.Valid(torn) {
		t.Fatalf("the torn fixture still parses, so this test cannot produce a decode error: %s", torn)
	}

	writeConfigMap(t, cfgPath, map[string]any{"projects": map[string]any{}})

	// The settler runs off the test goroutine, so it reports through a channel
	// rather than t.Fatalf.
	settleErr := make(chan error, 1)
	writes := 0
	tornOnce := false
	withTrustPostWriteHook(t, func(gotPath string) {
		if gotPath != cfgPath {
			return
		}
		writes++
		if tornOnce {
			return
		}
		tornOnce = true
		if err := os.WriteFile(cfgPath, torn, 0o600); err != nil {
			t.Fatalf("tear the config: %v", err)
		}
		go func() {
			time.Sleep(tornSettleDelay)
			settleErr <- os.WriteFile(cfgPath, settled, 0o600)
		}()
	})

	start := time.Now()
	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	elapsed := time.Since(start)

	// Guarded, because the settler exists only if the hook fired. An unconditional
	// receive here blocks forever whenever the write path never reaches the seam —
	// which is what a regression looks like — and a hung test panics the whole
	// package binary at the go-test timeout, destroying every other result. Reading
	// tornOnce is race-free: the hook sets it on this goroutine, inside the call.
	if tornOnce {
		if settleWriteErr := <-settleErr; settleWriteErr != nil {
			t.Fatalf("the settling writer failed, so this test never exercised a torn read: %v", settleWriteErr)
		}
	}
	if err != nil {
		t.Fatalf("a torn read that settled must be repaired, not reported as a failure. This is the "+
			"whole reason a decode error is retried rather than returned: %v", err)
	}
	if !trustedInConfig(t, cfgPath, worktreePath) {
		t.Errorf("the call reported success but projects[%q].hasTrustDialogAccepted is not on disk",
			worktreePath)
	}
	if elapsed < trustWriteRetryBackoff {
		t.Errorf("the repair took %v, less than one backoff (%v), so it never re-read after the torn "+
			"snapshot", elapsed, trustWriteRetryBackoff)
	}
	// The recovery must MERGE onto what the settling writer left, exactly as a
	// clobber repair does. A loop that rewrote its pre-tear snapshot would drop
	// this and become the lost-update clobberer itself.
	cfg := readConfigMap(t, cfgPath)
	if cfg["theme"] != "dark" {
		t.Errorf("the repair dropped the settling writer's top-level key; theme = %v", cfg["theme"])
	}
	projects := mustJSONObject(t, cfg, "projects", "config after the torn-read repair")
	if _, ok := projects[clobberAddedProject]; !ok {
		t.Errorf("the repair dropped the settling writer's project entry %q", clobberAddedProject)
	}
	if writes < 2 {
		t.Errorf("the writer reached the file %d times, want at least 2 (the write that was torn and "+
			"the repair after the settle)", writes)
	}
}

// TestTrustRetry_PermanentlyCorruptConfigReportsTheDecodeError is claim 5, and it
// is the other side of the decode-retry line. A decode failure is retried because
// a torn read of a foreign non-atomic rewrite looks exactly like a corrupt file in
// one snapshot. A file that is STILL unparseable after every attempt is not torn,
// so the call must surface the decode error itself rather than the generic "did
// not persist", and must never replace the file with a freshly marshalled one —
// that would discard every other project entry and top-level key the operator has.
//
// The elapsed-time assertion is what measures the LOOP, and it is the only thing
// here that does. The other three assertions all still hold if the decode
// carve-out is deleted and the first attempt returns its error straight to the
// caller: the file is intact, the error is non-nil, and it is still a decode
// error. Only the clock separates "kept trying" from "gave up at once", because
// a config that never parses never reaches the post-write seam and so cannot be
// counted there. The bound is a lower one against real sleeps, not a timing
// window — a sleep does not return early.
//
// What it does NOT measure is whether each iteration READ AGAIN. A loop that
// iterated and slept but reused its first decode error would satisfy every
// assertion here, and a file that never parses cannot tell the difference.
// TestTrustRetry_TornReadRecovers is what pins that, by settling the file and
// requiring the next read to notice.
func TestTrustRetry_PermanentlyCorruptConfigReportsTheDecodeError(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".claude.json")
	worktreePath := filepath.Join(dir, "worktrees", "run-corrupt")

	// A truncated object carrying a second project entry: what a reader sees
	// mid-rewrite, and it makes the damage visible if the file is replaced.
	corrupt := []byte(`{"projects": {"/some/other/project": {"hasTrustDi`)
	if err := os.WriteFile(cfgPath, corrupt, 0o600); err != nil {
		t.Fatalf("seed the corrupt config: %v", err)
	}

	start := time.Now()
	err := ensureWorktreeTrustAt(worktreePath, cfgPath)
	elapsed := time.Since(start)

	// Checked before the error, because "the file is intact" is the claim that
	// matters most and it holds whatever the call returned.
	if got := mustReadFile(t, cfgPath); !bytes.Equal(got, corrupt) {
		t.Errorf("the corrupt config was overwritten, which discards every other project entry and "+
			"top-level key in it.\nbefore: %s\nafter:  %s", corrupt, got)
	}
	if err == nil {
		t.Fatal("a config that never became parseable reported success; the trust key was never written")
	}
	if !trustConfigDecodeErr(err) {
		t.Errorf("the error is not the decode failure itself, so whoever reads it cannot tell a corrupt "+
			"config from a clobbering writer: %v", err)
	}
	// trustWriteMaxAttempts attempts carry that many minus one backoffs between
	// them. Anything faster did not re-read, and a torn read of a foreign writer
	// would then be reported as a corrupt config off its first snapshot.
	if wantAtLeast := time.Duration(trustWriteMaxAttempts-1) * trustWriteRetryBackoff; elapsed < wantAtLeast {
		t.Errorf("the decode failure was reported after %v, sooner than the %v of backoff that %d attempts "+
			"cost; the config was read once and never re-read, so a torn read gets no second chance",
			elapsed, wantAtLeast, trustWriteMaxAttempts)
	}
}
