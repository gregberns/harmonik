package codex

// billingguard_concurrency_test.go — concurrency-safety tests for the positive
// codex billing guard's config.toml rewrite (hk-codex-billing-guard-race-v6dl5).
//
// # Why this file exists separately
//
// $CODEX_HOME (default ~/.codex) is GLOBAL: one directory, one config.toml,
// shared by every codex process and every guard invocation on the host. It is
// not per-run, per-worktree, or per-project. At --max-concurrent N, N guards
// rewrite that one file at once, and N codex children read it.
//
// The guard used to rewrite it with os.WriteFile, which EMPTIES the target
// before it refills it, and took no lock. An independent review measured 8047 of
// 12000 concurrent reads landing inside that empty window. Both halves of that
// window are billing failures: a peer guard that reads no forced_login_method
// refuses a launch that was valid, and a codex child that reads none falls back
// to API-pool billing — which is the single outcome the guard exists to prevent.
// The guard could defeat itself.
//
// billingguard_test.go pins the single-caller content rules (fresh write,
// idempotence, wrong-value rewrite, unrelated content preserved). It says
// nothing about what a second caller sees, so the property that makes a shared
// $CODEX_HOME safe had no test. These are those tests.
//
// # Why none of these tests can hit the production lock bound
//
// An earlier draft of this file ran eight guards through the full locked path
// and DID hit the 10s bound at -cpu=1, which is both a flaky test and evidence
// that the refusal path was reachable under load. The fix was in the guard, not
// in the test: an already-pinned config now returns on a lock-free probe, so the
// common case never contends, and in-process writers queue on a mutex ahead of
// the unfair flock. The tests below are shaped to match — the writer-versus-
// reader test serializes its writers exactly as production does, so the only
// test that waits on a contended lock is the one asserting the timeout, and it
// passes its own short bound.
//
// Bead ref: hk-codex-billing-guard-race-v6dl5.

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// seedConfig writes content to codexHome/config.toml and returns its path.
func seedConfig(t *testing.T, codexHome, content string) string {
	t.Helper()
	cfgPath := filepath.Join(codexHome, codexConfigFileName)
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("seedConfig: write %s: %v", cfgPath, err)
	}
	return cfgPath
}

// pinnedLine is the exact line the guard materializes.
func pinnedLine() string {
	return forcedLoginMethodKey + " = \"" + forcedLoginMethodValue + "\""
}

// TestMaterializeForcedLoginMethod_HeldReaderSeesWholeOldFile pins the property
// that makes a concurrent read safe: the rewrite REPLACES config.toml by rename
// rather than emptying and refilling it in place.
//
// A reader that opened the file before the rewrite therefore still reads the
// whole old file. Under the old truncating write it read whatever the writer had
// managed to put down so far, which for a reader with bad timing is nothing.
//
// This is deterministic. The handle is opened before the call and read after it,
// so no scheduler outcome is involved.
func TestMaterializeForcedLoginMethod_HeldReaderSeesWholeOldFile(t *testing.T) {
	codexHome := t.TempDir()
	// Seeded with the WRONG value so the guard's already-pinned fast path does
	// not apply and a real rewrite happens.
	before := "model = \"gpt-5-codex\"\n" + forcedLoginMethodKey + " = \"apikey\"\nmodel_reasoning_effort = \"high\"\n"
	cfgPath := seedConfig(t, codexHome, before)

	// A peer codex process, or a peer guard, holding the config open across our
	// rewrite. It must not observe a partial file.
	held, err := os.Open(cfgPath) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open config to hold it: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := held.Close(); closeErr != nil {
			t.Errorf("close held handle: %v", closeErr)
		}
	})

	if err := materializeForcedLoginMethod(t.Context(), codexHome); err != nil {
		t.Fatalf("materializeForcedLoginMethod: %v", err)
	}

	seen, err := io.ReadAll(held)
	if err != nil {
		t.Fatalf("read through held handle: %v", err)
	}
	if string(seen) != before {
		t.Fatalf("a reader holding config.toml across the rewrite saw a changed or partial file.\n"+
			"This is the truncating-write window that billed the API pool.\ngot:  %q\nwant: %q",
			seen, before)
	}

	// The rewrite must still have landed — otherwise this test would pass on a
	// guard that does nothing at all.
	after, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("read config after rewrite: %v", err)
	}
	if !strings.Contains(string(after), pinnedLine()) {
		t.Fatalf("config.toml does not carry %q after materialize; got %q", pinnedLine(), after)
	}
	if !strings.Contains(string(after), "model_reasoning_effort") {
		t.Fatalf("the rewrite dropped unrelated config content; got %q", after)
	}
}

// TestMaterializeForcedLoginMethod_AlreadyPinned_TakesNoLock pins the fast path
// that keeps the guard off the lock in the case that actually happens.
//
// The operator's config carries the pin on every launch after the first, so if
// the guard locked on that path it would serialize every concurrent dispatch
// behind one file lock for no benefit — which is how the equivalent guard in
// internal/workspace/claudetrust_wm040b.go once produced a multi-minute spawn
// stall (hk-bfvby).
//
// This is deterministic: the lock is held by the test for the whole call, so a
// guard that tried to take it could not possibly succeed.
func TestMaterializeForcedLoginMethod_AlreadyPinned_TakesNoLock(t *testing.T) {
	codexHome := t.TempDir()
	seedConfig(t, codexHome, "model = \"gpt-5-codex\"\n"+pinnedLine()+"\n")

	lockPath := filepath.Join(codexHome, codexConfigLockName)
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open lockfile: %v", err)
	}
	if flockErr := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
		t.Fatalf("hold the config lock: %v", flockErr)
	}
	t.Cleanup(func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Errorf("close lockfile: %v", closeErr)
		}
	})

	// A short bound: if the guard reaches the lock at all this call cannot return
	// nil, and it would take the whole bound to fail.
	if err := materializeForcedLoginMethodWithin(t.Context(), codexHome, 100*time.Millisecond); err != nil {
		t.Fatalf("an already-pinned config made the guard wait on the config lock: %v", err)
	}

	// No staging file: the fast path must not have written anything.
	if _, statErr := os.Stat(filepath.Join(codexHome, codexConfigStagingName)); statErr == nil {
		t.Fatal("the already-pinned fast path staged a write; it must not write at all")
	}
}

// TestMaterializeForcedLoginMethod_ContendedLock_FailsClosed pins that a rewrite
// runs under an exclusive lock, and that a guard which cannot take that lock
// refuses rather than proceeding.
//
// Refusing is the correct posture: a guard that cannot serialise its own edit
// cannot promise the login pin is in place, and the whole mechanism is
// fail-closed by design. Without the lock the call below simply succeeds, which
// is how two guards used to lose each other's edit.
//
// The refusal must also be classified STRUCTURAL, so dispatch reads it as a
// contended host and not as a verdict on the bead.
//
// This is deterministic. The lock is held by the test process for the whole
// call, on a real lockfile, so there is no race to win or lose.
func TestMaterializeForcedLoginMethod_ContendedLock_FailsClosed(t *testing.T) {
	codexHome := t.TempDir()
	// Wrong value, so the fast path does not apply and a lock is genuinely needed.
	before := forcedLoginMethodKey + " = \"apikey\"\n"
	cfgPath := seedConfig(t, codexHome, before)

	lockPath := filepath.Join(codexHome, codexConfigLockName)
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open lockfile: %v", err)
	}
	if flockErr := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
		t.Fatalf("hold the config lock: %v", flockErr)
	}

	// A short bound so the test does not wait out the production timeout. The
	// bound decides how long a launch waits, not whether it is refused.
	err = materializeForcedLoginMethodWithin(t.Context(), codexHome, 100*time.Millisecond)
	if err == nil {
		t.Fatal("materialize succeeded while another holder had the config lock — " +
			"the read-modify-write is unserialised, so two guards can lose each other's edit")
	}
	if !errors.Is(err, errCodexConfigLockTimeout) {
		t.Fatalf("expected a config-lock timeout, got: %v", err)
	}
	if !errors.Is(err, handlercontract.ErrStructural) {
		t.Fatalf("a contended-host refusal must be classified structural so dispatch retries it; got: %v", err)
	}

	// Fail closed means it also left the file alone.
	untouched, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("read config after refusal: %v", err)
	}
	if string(untouched) != before {
		t.Fatalf("a refused materialize still edited config.toml; got %q want %q", untouched, before)
	}

	// Once the lock is free the same call must succeed, proving the refusal was
	// the lock and not some unrelated fault.
	if unlockErr := syscall.Flock(int(holder.Fd()), syscall.LOCK_UN); unlockErr != nil {
		t.Fatalf("release the config lock: %v", unlockErr)
	}
	if closeErr := holder.Close(); closeErr != nil {
		t.Fatalf("close lockfile: %v", closeErr)
	}
	if err := materializeForcedLoginMethodWithin(t.Context(), codexHome, 100*time.Millisecond); err != nil {
		t.Fatalf("materialize failed after the lock was released: %v", err)
	}
	ok, err := configDeclaresChatGPTLogin(codexHome)
	if err != nil {
		t.Fatalf("configDeclaresChatGPTLogin: %v", err)
	}
	if !ok {
		t.Fatal("config.toml does not declare the chatgpt login pin after an uncontended materialize")
	}
}

// TestMaterializeForcedLoginMethod_CancelledContext_DoesNotWaitOutTheBound pins
// that a cancelled run stops waiting for the lock promptly.
//
// The retry loop used to sleep between attempts without watching the context, so
// a run cancelled one millisecond in still burned the whole bound before it
// noticed. The bound is 10s in production; that is 10s of a dead run holding a
// dispatch slot.
func TestMaterializeForcedLoginMethod_CancelledContext_DoesNotWaitOutTheBound(t *testing.T) {
	codexHome := t.TempDir()
	seedConfig(t, codexHome, forcedLoginMethodKey+" = \"apikey\"\n")

	lockPath := filepath.Join(codexHome, codexConfigLockName)
	holder, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // G304: test-local temp path.
	if err != nil {
		t.Fatalf("open lockfile: %v", err)
	}
	if flockErr := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); flockErr != nil {
		t.Fatalf("hold the config lock: %v", flockErr)
	}
	t.Cleanup(func() {
		if closeErr := holder.Close(); closeErr != nil {
			t.Errorf("close lockfile: %v", closeErr)
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // already cancelled before the call

	// A bound far larger than any acceptable return time, so only cancellation
	// can end this call quickly.
	const bound = 30 * time.Second
	start := time.Now()
	err = materializeForcedLoginMethodWithin(ctx, codexHome, bound)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("materialize succeeded despite a held lock and a cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to surface, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("a cancelled run waited %s for the lock; it must stop at cancellation, not at the %s bound",
			elapsed, bound)
	}
}

// TestReplaceCodexConfig_ConcurrentReadersNeverSeeAnUnpinnedConfig reproduces
// the measured shape: the one shared config.toml rewritten over and over while
// readers ask the question the guard's own pre-flight asks.
//
// The invariant is absolute rather than statistical, because a single reader
// that sees no pin is a single launch billed to the API pool: NO reader may ever
// observe a config.toml without forced_login_method. Against the truncating
// write this failed at roughly two reads in three.
//
// The writers hold the real config lock, exactly as production writers do, so
// this test measures reader-versus-writer atomicity and nothing else. Serialised
// writers cannot starve each other, so no lock bound is in play here.
func TestReplaceCodexConfig_ConcurrentReadersNeverSeeAnUnpinnedConfig(t *testing.T) {
	codexHome := t.TempDir()
	// Two pinned variants of different lengths, so each rewrite genuinely changes
	// the file and a truncation window would be wide enough to observe.
	variants := []string{
		pinnedLine() + "\n",
		"model = \"gpt-5-codex\"\n" + pinnedLine() + "\nmodel_reasoning_effort = \"high\"\n",
	}
	cfgPath := seedConfig(t, codexHome, variants[0])

	const (
		writers = 4
		readers = 4
		rounds  = 50
	)

	ctx := t.Context()
	var writerWG, readerWG sync.WaitGroup
	writeErrs := make([]error, writers)
	done := make(chan struct{})
	start := make(chan struct{})

	for i := 0; i < writers; i++ {
		writerWG.Add(1)
		go func(idx int) {
			defer writerWG.Done()
			<-start
			for r := 0; r < rounds; r++ {
				// The production write path: in-process queue, then the flock,
				// then the atomic replace.
				codexConfigWriteMu.Lock()
				release, err := acquireCodexConfigLock(ctx, codexHome, codexConfigLockTimeout)
				if err != nil {
					codexConfigWriteMu.Unlock()
					writeErrs[idx] = err
					return
				}
				werr := replaceCodexConfig(codexHome, cfgPath, []byte(variants[r%len(variants)]))
				release()
				codexConfigWriteMu.Unlock()
				if werr != nil {
					writeErrs[idx] = werr
					return
				}
			}
		}(i)
	}

	unpinned := make([]int, readers)
	readErrs := make([]error, readers)
	for i := 0; i < readers; i++ {
		readerWG.Add(1)
		go func(idx int) {
			defer readerWG.Done()
			<-start
			for {
				select {
				case <-done:
					return
				default:
				}
				ok, err := configDeclaresChatGPTLogin(codexHome)
				if err != nil {
					readErrs[idx] = err
					return
				}
				if !ok {
					unpinned[idx]++
				}
				// Yield: a reader that never yields can starve the writers on a
				// single-CPU box, which turns a correctness test into a timing one.
				runtime.Gosched()
			}
		}(i)
	}

	close(start)
	writerWG.Wait()
	close(done)
	readerWG.Wait()

	for i, err := range writeErrs {
		if err != nil {
			t.Fatalf("writer %d errored under concurrency: %v", i, err)
		}
	}
	for i, err := range readErrs {
		if err != nil {
			t.Fatalf("reader %d could not read config.toml under concurrency: %v", i, err)
		}
	}
	total := 0
	for _, n := range unpinned {
		total += n
	}
	if total != 0 {
		t.Fatalf("%d concurrent reads saw a config.toml with no %s. "+
			"Each one is a codex launch that either falls back to API-pool billing or is refused for a reason "+
			"that has nothing to do with the work.", total, forcedLoginMethodKey)
	}
}

// TestMaterializeForcedLoginMethod_ConcurrentGuards_NoneAreRefused pins the
// dispatch-shaped case end to end: N guards fire at once against one unpinned
// $CODEX_HOME, which is what --max-concurrent N produces on a fresh box.
//
// Every one of them must succeed. The first writes; the rest find the pin
// already there and take the lock-free path. A refusal here would be the guard
// defeating itself in a new way — turning a lock the guard needs for correctness
// into a reason to deny a valid launch.
func TestMaterializeForcedLoginMethod_ConcurrentGuards_NoneAreRefused(t *testing.T) {
	codexHome := t.TempDir()
	seedConfig(t, codexHome, "model = \"gpt-5-codex\"\n"+forcedLoginMethodKey+" = \"apikey\"\n")

	const guards = 8 // a plausible --max-concurrent
	ctx := t.Context()
	var wg sync.WaitGroup
	errs := make([]error, guards)
	start := make(chan struct{})

	for i := 0; i < guards; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			// The production bound, deliberately: this test asserts the bound is
			// NOT reachable on the dispatch-shaped path.
			errs[idx] = materializeForcedLoginMethod(ctx, codexHome)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("guard %d was refused under concurrent dispatch: %v", i, err)
		}
	}
	ok, err := configDeclaresChatGPTLogin(codexHome)
	if err != nil {
		t.Fatalf("configDeclaresChatGPTLogin: %v", err)
	}
	if !ok {
		t.Fatal("config.toml is not pinned after eight concurrent guards claimed success")
	}
}
