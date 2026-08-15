//go:build scenario

package daemon_test

// scenario_trust_clobber_hkqx065_test.go — concurrent dispatch against a shared
// ~/.claude.json that a NON-COOPERATING writer is rewriting underneath the fleet
// (bead hk-qx065).
//
// # What incident this guards
//
// At max-concurrent 3, two of three claude:local workers parked on Claude Code's
// folder-trust modal. That modal renders BEFORE SessionStart, so the hook bridge
// never fires, agent_ready is never synthesized, and the run dies at its ready
// deadline. On disk, the failed worktree's
// projects[<realpath>].hasTrustDialogAccepted was ABSENT from ~/.claude.json even
// though EnsureWorktreeTrust had run and returned success.
//
// The clobberer is Claude Code itself: every live claude process rewrites the
// shared config wholesale from its own in-memory snapshot and does not honor
// harmonik's advisory sidecar flock (independently recorded in a964cbcb). The fix
// (WM-040b as amended) is therefore verify-and-repair — re-read after every write,
// retry the whole read-modify-write on a lost key, fail structurally if it never
// sticks — NOT more locking, which cannot exclude a writer that never asks.
//
// # What this test proves, and what it deliberately does not
//
// PROVES, at real concurrency through the real provisioning path:
//
//   - The repair actually engages, and is load-bearing. Measured on this fixture
//     by mutating trustWriteMaxAttempts, unloaded, on one 10-core box:
//
//       budget 4, the shipping value ... 15 runs, 0 failures  [instrumented]
//       budget 2 ...................... 15 runs, 1 failure
//       budget 1, no repair ........... 35 runs, 19 failures
//
//     Every budget-4 run above is an instrumented overlay build — nothing in
//     that campaign ran the shipping value on this fixture without the extra
//     logging, so read that row as "the shipping value plus instrumentation".
//
//     A failing budget-1 run loses 1 or 2 of its 3 launches. So the retry is what
//     carries provisioning through. But read the budget-1 rate before you lean on
//     this test: a build with repair disabled still passes about half of single
//     unloaded runs, so ONE green run here is weak evidence that the repair loop
//     still works. Ask that question with repeated runs (hk-r8nx4).
//
//     What the shipping budget actually costs, from an instrumented build over
//     those same 15 budget-4 runs — 45 trust writes, every one a key the hostile
//     writer targeted: 40 settled on the first attempt, 4 on the second, 1 on the
//     third, and none needed the fourth. The typical write does not retry at all,
//     and the worst one observed still left an attempt spare. Logging may shift
//     the timing somewhat, which is the other reason to read that row with the
//     instrumentation in mind.
//
//     Two earlier claims are narrowed here, neither having reproduced as stated.
//     This bullet used to say the budget-1 mutation goes 0/3 completed. It does
//     not, on this fixture or on the intermediate draft the first campaign ran;
//     the fixture this one replaced was never run at budget 1 at all. And
//     6b9f3fb8e's commit message says the fixture now makes harmonik spend 3 of
//     its 4 attempts to settle a key rather than 2. As a general claim the counts
//     above refute it — 40 of 45 settle on the first attempt. Its predicted worst
//     case did happen, once.
//   - Liveness under a hostile writer: N=3 concurrent dispatches all reach
//     run_completed. A repair loop that gave up too eagerly would fail every run;
//     one that held the sidecar flock across its backoff, or took a fresh full
//     lock timeout per attempt, would blow the terminal budget instead.
//   - Preservation: harmonik's repair writes merge onto a FRESH read of whatever
//     the other writer left, not onto a snapshot of harmonik's own. The hostile
//     writer measures this continuously (see tclMaxTolerableBackslide) rather than
//     by an end-state comparison, which would be vacuous — it stamps a new
//     generation every 15ms and would paper over a revert within one tick.
//   - The config survives two concurrent atomic-rename writers as valid JSON.
//
// DOES NOT PROVE: that the trust modal itself is suppressed. Twins do not read
// ~/.claude.json, so no scenario-tier test can observe the modal; the end-to-end
// proof is the operator's live run — before the fix 1/3 runs completed with the
// modal captured on two panes, after it 3/3 reached agent_ready with zero modals
// across pane sweeps at T+30..150s.
//
// Run by hand (the daemon commit-gate SKIPS //go:build scenario tests):
//
//	go test -tags=scenario -run TestScenario_TrustClobber ./internal/daemon/ -count=1
//
// Bead: hk-qx065. Refs: hk-bfvby, hk-z16, hk-944c2, hk-ukhzu.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/daemon/scenariotest"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fixtures (tcl prefix — trust-clobber, bead hk-qx065)
// ─────────────────────────────────────────────────────────────────────────────

// tclGenerationKey is the top-level key the hostile writer bumps on every
// rewrite. It is the merge-not-revert probe: harmonik writing a snapshot of its
// own back over the file would reset it to an older value.
const tclGenerationKey = "hostileWriterGeneration"

// tclOwnProject is a project entry the hostile writer invents. Nothing harmonik
// read before its own first write can contain it, so its survival proves the
// repair write merged onto a FRESH read of the other writer's file.
const tclOwnProject = "/invented/by/the/hostile/writer"

// tclHostileWriter is a non-cooperating rewriter of the shared claude config, in
// the shape of the real thing: it takes NO flock, reads the whole config, drops
// trust entries it does not recognise, stamps content of its own, and commits by
// atomic rename (Claude Code uses os.replace, so torn reads are not the modelled
// failure here).
//
// Erasures are BUDGETED per key (tclErasesPerKey) rather than unbounded: a live
// claude's first few rewrites come from a snapshot taken BEFORE harmonik wrote the
// entry, so they drop it; once that process has re-read the config its snapshot
// carries the entry and it writes it back. So after tclErasesPerKey rounds the
// writer LEARNS a key and preserves it from then on.
//
// BE PRECISE ABOUT WHAT THIS MODELS. The SHAPE is faithful and is evidenced: a real
// claude rewrites the whole file from a stale in-memory snapshot and takes no lock,
// so entries written after its read are erased (87b0e3ca4 ruled out the competing
// "claude prunes stale entries" reading by measurement — the config retained 156
// entries for long-deleted worktrees, so nothing prunes; a964cbcb watched a live
// claude reset a top-level key to null). The RATE is not faithful and is not
// evidenced — see tclRewriteInterval. An earlier version of this comment claimed
// the budget "models the real thing precisely". It does not, and reading it that
// way invites treating this scenario as evidence about production timing, which it
// cannot supply.
//
// The learning step is load-bearing, not decoration, and the budget must count
// IMPLICIT drops (writing back a snapshot that no longer contains a key harmonik
// re-applied after we read) as well as explicit ones. Without either half the
// writer never stops losing the entry — a relentless clobberer, which correctly
// drives harmonik into the structural "did not persist" failure but does so
// non-deterministically, making the liveness assertion flaky rather than wrong.
type tclHostileWriter struct {
	cfgPath string

	mu      sync.Mutex
	seen    map[string]bool // every harmonik-written trust key we have ever observed
	dropped map[string]int  // trust key -> times our write has cost harmonik the entry
	learned map[string]bool // trust keys whose budget is spent; preserved from now on
	gen     int
	rewrite int

	// maxBackslide is the largest number of generations the writer has ever seen
	// LOST — i.e. it wrote generation N and later read generation N-k. See
	// tclMaxTolerableBackslide for why a small k is inherent and a large one is
	// the lost-update bug.
	maxBackslide int

	stop chan struct{}
	done chan struct{}

	// observed counts distinct harmonik-written trust keys the writer ever saw.
	observed atomic.Int64

	// beforeFreshnessCheck runs inside tryRewriteLocked, after the replacement is
	// staged and immediately before the freshness re-read. It is the only way to
	// land a competing write INSIDE the cycle's window deterministically, which is
	// what TestTclHostileWriter_RefusesLostUpdate needs in order to fail against
	// the defect this fixture used to carry. nil in the scenario itself.
	beforeFreshnessCheck func()
}

// tclErasesPerKey is how many times the hostile writer drops any one trust key
// before leaving it alone. Kept below the trust writer's attempt budget so a key
// the writer targets while harmonik is still repairing can still settle.
//
// That calibration has a consequence worth stating out loud, because it bounds what
// this whole scenario can be cited for: the fixture is tuned AGAINST the very
// constant under test. With 2 erasures against a budget of 4, the liveness
// assertion below is by construction incapable of failing because "trustWriteMaxAttempts
// is too small". A green run here is NOT evidence that the production attempt
// budget is adequate against a real writer. Nothing in this repo measures that; see
// the bead this file's header names.
const tclErasesPerKey = 2

// tclRewriteInterval paces the hostile writer. Fast enough to interleave heavily
// with three concurrent provisioning paths, slow enough not to saturate a core.
//
// This is an ERGONOMIC choice and bears no measured relation to a real fleet's
// cadence. It sustains roughly 66 rewrites/sec, which nothing suggests a live fleet
// approaches. Contention here is therefore far above life, on purpose — good for
// exercising the repair loop, useless as a source of production probabilities.
const tclRewriteInterval = 15 * time.Millisecond

func tclStartHostileWriter(t *testing.T, cfgPath string) *tclHostileWriter {
	t.Helper()
	w := &tclHostileWriter{
		cfgPath: cfgPath,
		seen:    map[string]bool{},
		dropped: map[string]int{},
		learned: map[string]bool{},
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go w.run()
	t.Cleanup(w.Stop)
	return w
}

func (w *tclHostileWriter) Stop() {
	select {
	case <-w.stop:
		return // already stopped
	default:
	}
	close(w.stop)
	<-w.done
}

func (w *tclHostileWriter) run() {
	defer close(w.done)
	ticker := time.NewTicker(tclRewriteInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			w.rewriteOnce()
		}
	}
}

// tclCommitAttempts bounds the redo loop in rewriteOnce. A redo happens only when
// the file changed under the writer mid-cycle, which is self-limiting, but an
// unbounded loop here would spin a core against a busy fleet. Exhausting it makes
// the writer skip a beat rather than commit a lost update — still hostile, just
// quiet for one tick.
const tclCommitAttempts = 8

// rewriteOnce performs one wholesale read-modify-write with NO lock — the whole
// point — and refuses to commit a LOST UPDATE. Errors are swallowed: a hostile
// writer racing an atomic rename can read a file that is momentarily absent, and
// that is not a test failure.
func (w *tclHostileWriter) rewriteOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for attempt := 0; attempt < tclCommitAttempts; attempt++ {
		if w.tryRewriteLocked() {
			return
		}
	}
}

// tryRewriteLocked runs one read-modify-write cycle and reports whether it is done
// with this tick. It returns false ONLY when it read the config, built a rewrite,
// and then found the file had changed underneath it; the caller redoes the cycle
// against the fresh bytes.
//
// Why refusing that commit is load-bearing, and is NOT the writer going soft: the
// per-key erase budget above can only charge a key the writer has SEEN. A key
// harmonik lands entirely inside this cycle's read→rename window is in neither the
// snapshot nor w.seen, so committing the snapshot erases it while spending NONE of
// the budget. The writer never learns that key, drops it again on the next tick,
// and on the next — the relentless clobberer this file's header says the budget
// exists to prevent, arriving by the one path the budget cannot see. Harmonik then
// spends every one of its attempts on a key that is erased faster than it can be
// repaired, and the launch fails structurally. That reads as a product defect and
// is not one: it is the fixture exceeding its own stated model. Refusing the commit
// puts the key back where the budget can charge it — the redo reads it explicitly,
// deletes it explicitly, and the erase counts.
//
// The writer does not go soft. It still takes no flock, still deletes every
// harmonik key it recognises, and still races every launch. What it loses is only
// the ability to commit an UNCOUNTED erasure.
//
// TWO CONSEQUENCES THAT MUST NOT BE LEFT UNSAID.
//
// First, deferring the bookkeeping also moved the `learned` write-back's view of
// the budget. Before this change, the explicit-drop loop set learned[key] inline
// the instant the budget saturated, and the write-back loop three statements later
// put the key straight back — so the SECOND budgeted erase was counted but never
// actually committed. Now learned[key] is set after the rename, so the write-back
// does not yet know the key and the deletion really lands. Each key therefore costs
// two real erasures where it used to cost one, and harmonik needs 3 of its 4
// attempts to settle a key rather than 2. That is more faithful to what
// tclErasesPerKey says on the tin, and it makes the fixture more hostile, not less
// — but it halves the spare margin, so a run now sits ONE residual-window lost
// update away from red instead of two. That margin is why the floor assertions in
// the scenario body exist. Measured across the A/B that landed this change:
// 0 failures in 200 runs with it, 12 in 104 without.
//
// Second, the refusal narrows the lost-update class; it does not close it. A rename
// landing inside the one-syscall window between the freshness check and our own
// rename still commits an erasure nobody counted. Arm A's 0-of-200 bounds that
// residual at roughly 1.5% per run at 95% confidence — low, NOT zero. Do not read
// "stops erasing keys it never saw" as an absolute; it is a class narrowed by
// orders of magnitude, and the fixture keeps a small by-construction flake floor.
//
// The caller holds w.mu. Bookkeeping is applied only after the rename lands, so a
// discarded cycle charges nothing.
func (w *tclHostileWriter) tryRewriteLocked() bool {
	data, err := os.ReadFile(w.cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		return true
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return true
	}

	// Lost-update probe. We stamped generation w.gen on our last rewrite. Reading
	// back something OLDER means another writer replaced the file with a snapshot
	// taken before that stamp — it merged onto stale bytes instead of a fresh
	// read. See tclMaxTolerableBackslide for the size that separates the inherent
	// race from the bug. This is an observation of what we really read, so it
	// stands whether or not this cycle goes on to commit.
	if seen, ok := cfg[tclGenerationKey].(float64); ok {
		if backslide := w.gen - int(seen); backslide > w.maxBackslide {
			w.maxBackslide = backslide
		}
	}

	projects, ok := cfg["projects"].(map[string]interface{})
	if !ok {
		projects = map[string]interface{}{}
		cfg["projects"] = projects
	}

	// Bookkeeping this cycle WOULD apply, held aside until the rename lands.
	var newlySeen []string
	drops := map[string]int{}

	// IMPLICIT drops, counted first (before we mutate projects). A key we have seen
	// before but that is absent from this snapshot gets silently dropped the moment
	// we write the snapshot back — harmonik may have re-applied it after we read.
	// That costs harmonik a repair round exactly like an explicit delete, so it
	// spends the same budget. Not counting it is what made this writer relentless:
	// the budget never advanced while the key was missing, so the entry was dropped
	// on every rewrite forever.
	for key := range w.seen {
		if w.learned[key] {
			continue
		}
		if _, present := projects[key]; !present {
			drops[key]++
		}
	}

	// EXPLICIT drops: keys present in our snapshot that we do not yet know.
	for key := range projects {
		if key == tclOwnProject || w.learned[key] {
			continue
		}
		if !w.seen[key] {
			newlySeen = append(newlySeen, key)
		}
		delete(projects, key)
		drops[key]++
	}

	// Keys we have learned are part of OUR snapshot now, so we write them back —
	// including on rewrites where harmonik has not yet re-applied them.
	for key := range w.learned {
		if _, present := projects[key]; !present {
			projects[key] = map[string]interface{}{"hasTrustDialogAccepted": true}
		}
	}

	// Commit content of our own that no earlier harmonik snapshot has seen.
	nextGen := w.gen + 1
	projects[tclOwnProject] = map[string]interface{}{
		"hasTrustDialogAccepted": true,
		"generation":             float64(nextGen),
	}
	cfg[tclGenerationKey] = float64(nextGen)

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return true
	}
	// Stage the replacement BEFORE the freshness check below, so the window
	// between that check and the rename is one syscall wide.
	tmp := fmt.Sprintf("%s.hostile-tmp", w.cfgPath)
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return true
	}

	if w.beforeFreshnessCheck != nil {
		w.beforeFreshnessCheck()
	}

	// The freshness check. Anything harmonik landed since our read makes this
	// snapshot a lost update, so discard it and redo against the new bytes.
	fresh, err := os.ReadFile(w.cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		return true
	}
	if !bytes.Equal(fresh, data) {
		return false
	}

	// Atomic rename, exactly like the real writer: readers see a whole old or
	// whole new file. A same-filesystem rename effectively never fails, but the
	// bookkeeping below claims the rewrite is ON DISK, so check rather than assert
	// it: charging drops and advancing w.gen against a file that never changed
	// would inflate maxBackslide on the next read and fail the preservation
	// assertion for a reason that has nothing to do with harmonik.
	if err := os.Rename(tmp, w.cfgPath); err != nil {
		return true
	}

	// The rewrite is on disk, so the bookkeeping it earned is now due.
	for _, key := range newlySeen {
		w.seen[key] = true
		w.observed.Add(1)
	}
	for key, n := range drops {
		w.dropped[key] += n
		if w.dropped[key] >= tclErasesPerKey {
			w.learned[key] = true
		}
	}
	w.gen = nextGen
	w.rewrite++
	return true
}

// tclMaxTolerableBackslide bounds how many of the hostile writer's generations
// harmonik may lose in one write.
//
// Some loss is INHERENT and is not a bug: two writers that do not share a lock
// will always have a read-modify-write window in which the other's commits are
// invisible. For a correct implementation that window is ONE cycle — read, apply,
// write — a couple of milliseconds, so at tclRewriteInterval pacing it costs at
// most a generation or two.
//
// A stale-snapshot retry is a different animal: its window stretches from the
// FIRST attempt's read to the LAST attempt's write, spanning the whole backoff
// budget, so it discards tens of generations at this cadence. The gap between
// those two magnitudes is what this bound tests. 5 sits well above the inherent
// noise and far below the mutation's ~40.
//
// READ THE DIRECTION OF THIS PROBE BEFORE CITING IT. It measures harmonik
// clobbering the WRITER, and only that. Nothing here has ever measured the writer
// clobbering HARMONIK, which is a separate failure with a separate cause. A green
// maxBackslide therefore rules out NOTHING about lost updates in the other
// direction — the two coexist happily, and were measured coexisting. The inference
// "both preservation probes are green, so this is not a lost-update" is false and
// has already propagated into a commit message, a bead and a fleet initiative doc.
// It cost this investigation real time. Do not repeat it.
const tclMaxTolerableBackslide = 5

// tclMinRewrites is the floor on rewrites the hostile writer must actually commit
// for the scenario's liveness result to mean anything. At tclRewriteInterval over a
// run of this length the real number lands in the high hundreds (87b0e3ca4 recorded
// 804), so this sits far below normal and far above inert.
const tclMinRewrites = 200

// snapshot returns the writer's own bookkeeping under its lock.
func (w *tclHostileWriter) snapshot() (generation, rewrites, keysErased, keysLearned, maxBackslide int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gen, w.rewrite, len(w.dropped), len(w.learned), w.maxBackslide
}

// TestTclHostileWriter_RefusesLostUpdate pins the fixture's own invariant at the
// lowest layer that can fail: one rewrite cycle, with a competing write landing
// inside the cycle's read→rename window.
//
// This is the reproducing test for hk-c7eox. The defect was in this fixture, and
// the evidence that it was fixed is a 300-run A/B nothing in the tree re-runs. A
// scenario test cannot pin it — the failure was ~11% per run and needed CPU load to
// surface. This one is deterministic, costs milliseconds, and fails the moment the
// writer goes back to committing erasures it never saw.
//
// What the defect was: the writer read the config, and if harmonik landed a trust
// key before the writer's rename, the writer's snapshot — which predates that key —
// erased it. The key was in neither the snapshot nor w.seen, so it charged NOTHING
// against the per-key erase budget, and the writer clobbered it again on the next
// tick, and the next, until harmonik's repair budget ran out and the launch failed.
func TestTclHostileWriter_RefusesLostUpdate(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), ".claude.json")
	writeCfg := func(projects map[string]interface{}) {
		t.Helper()
		raw, err := json.MarshalIndent(map[string]interface{}{
			"theme":    "dark",
			"projects": projects,
		}, "", "  ")
		if err != nil {
			t.Fatalf("marshal config: %v", err)
		}
		if err := os.WriteFile(cfgPath, append(raw, '\n'), 0o600); err != nil {
			t.Fatalf("write config: %v", err)
		}
	}
	readProjects := func() map[string]interface{} {
		t.Helper()
		data, err := os.ReadFile(cfgPath) //nolint:gosec // G304: test-controlled temp path
		if err != nil {
			t.Fatalf("read config: %v", err)
		}
		var cfg map[string]interface{}
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("config is not valid JSON: %v\n%s", err, data)
		}
		projects, ok := cfg["projects"].(map[string]interface{})
		if !ok {
			t.Fatalf("projects map missing from config: %v", cfg)
		}
		return projects
	}

	const trustKey = "/hk/worktree/landed-mid-cycle"
	writeCfg(map[string]interface{}{})

	w := &tclHostileWriter{
		cfgPath: cfgPath,
		seen:    map[string]bool{},
		dropped: map[string]int{},
		learned: map[string]bool{},
	}
	// Stand in for harmonik: land a trust key after the writer has read the config
	// and staged its replacement, but before it commits.
	w.beforeFreshnessCheck = func() {
		writeCfg(map[string]interface{}{
			trustKey: map[string]interface{}{"hasTrustDialogAccepted": true},
		})
	}

	w.mu.Lock()
	committed := w.tryRewriteLocked()
	w.mu.Unlock()

	if committed {
		t.Errorf("the writer committed a cycle whose snapshot predates a key that landed mid-cycle. " +
			"That erases the key without ever seeing it, so it charges nothing against the per-key " +
			"erase budget and the writer clobbers it without bound — the hk-c7eox failure exactly")
	}
	if _, present := readProjects()[trustKey]; !present {
		t.Errorf("the trust key that landed mid-cycle was erased from disk; it must survive a cycle " +
			"the writer had no way to see it in")
	}

	// A discarded cycle must charge NOTHING, or the budget drifts against erasures
	// that never happened and the writer learns keys it never erased.
	if w.gen != 0 || w.rewrite != 0 {
		t.Errorf("a discarded cycle advanced the writer's own counters: gen=%d rewrite=%d, want 0 and 0",
			w.gen, w.rewrite)
	}
	if len(w.seen) != 0 || len(w.dropped) != 0 || len(w.learned) != 0 {
		t.Errorf("a discarded cycle charged bookkeeping: seen=%v dropped=%v learned=%v, want all empty",
			w.seen, w.dropped, w.learned)
	}
	if observed := w.observed.Load(); observed != 0 {
		t.Errorf("a discarded cycle counted %d observed keys, want 0", observed)
	}

	// And the writer is still hostile: with nothing landing mid-cycle it reads the
	// key, deletes it explicitly, commits, and CHARGES the erase. That is the whole
	// point — the fix removes uncounted erasures, not erasures.
	w.beforeFreshnessCheck = nil
	w.mu.Lock()
	committed = w.tryRewriteLocked()
	w.mu.Unlock()

	if !committed {
		t.Fatalf("the writer failed to commit an uncontended cycle; the fixture is now inert")
	}
	if _, present := readProjects()[trustKey]; present {
		t.Errorf("the writer left a trust key it could see in place; it is supposed to erase every key " +
			"it does not recognise, and a fixture that stops erasing tests nothing")
	}
	if w.dropped[trustKey] != 1 || !w.seen[trustKey] {
		t.Errorf("the erase went uncharged: dropped=%d seen=%v, want 1 and true",
			w.dropped[trustKey], w.seen[trustKey])
	}
}

// tclBootForTesting mirrors vn4BootForTesting: it binds daemon.StartForTesting
// with the determinism options the RunConcurrentMerge fixture needs but cannot
// reference itself (they live in package daemon's test files).
var tclBootForTesting = vn4BootForTesting

// ─────────────────────────────────────────────────────────────────────────────
// The scenario
// ─────────────────────────────────────────────────────────────────────────────

// TestScenario_TrustClobber_ConcurrentDispatch_SurvivesHostileRewriter dispatches
// N=3 beads concurrently while a non-cooperating writer rewrites the shared
// claude config throughout, and asserts the liveness + preservation properties
// described in this file's header.
func TestScenario_TrustClobber_ConcurrentDispatch_SurvivesHostileRewriter(t *testing.T) {
	skipRealDaemonE2EInShort(t)

	// The shared config the whole fleet (and the hostile writer) contends on.
	claudeCfg := filepath.Join(t.TempDir(), ".claude.json")
	seed := map[string]interface{}{
		"theme":    "dark",
		"projects": map[string]interface{}{},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("hk-qx065 scenario: marshal seed config: %v", err)
	}
	if err := os.WriteFile(claudeCfg, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("hk-qx065 scenario: write seed config: %v", err)
	}

	hostile := tclStartHostileWriter(t, claudeCfg)

	res := scenariotest.RunConcurrentMerge(t, scenariotest.ConcurrentMergeConfig{
		N: 3,
		// commit-on-cue-startup-delay, not single-happy-path: dot checks HEAD
		// advance per node, and single-happy-path leans on the fixture's
		// pre-committed empty commit rather than landing one of its own.
		TwinScenario:      "commit-on-cue-startup-delay",
		Boot:              tclBootForTesting(t),
		ExpectAllComplete: true,
		AgentReadyTimeout: 5 * time.Second,
		BeadPrefix:        "tcl",
		ClaudeConfigPath:  claudeCfg,
	})

	// Stop the writer BEFORE reading the file, so the assertions see a settled
	// state rather than racing one more rewrite.
	hostile.Stop()
	generation, rewrites, keysErased, keysLearned, maxBackslide := hostile.snapshot()

	// ── Liveness ────────────────────────────────────────────────────────────
	// Every run provisioned and completed. A trust seed that hard-failed on the
	// first lost key would reopen its bead instead; one that serialized the fleet
	// behind a held flock, or took a fresh full lock timeout per attempt, would
	// blow the terminal budget.
	if res.Completed < len(res.BeadIDs) {
		t.Errorf("hk-qx065 scenario: only %d/%d runs completed under a hostile config rewriter "+
			"(the verify-and-repair loop must not hard-fail or stall a launch it can repair)",
			res.Completed, len(res.BeadIDs))
	}

	// ── The writer actually raced us ────────────────────────────────────────
	// Without this the assertions above are vacuous: the test would pass on a
	// build where the trust seed never ran at all.
	if rewrites == 0 {
		t.Fatalf("hk-qx065 scenario: the hostile writer never rewrote the config; fixture is inert")
	}
	if observed := hostile.observed.Load(); observed == 0 {
		t.Fatalf("hk-qx065 scenario: the hostile writer never saw a harmonik-written trust key "+
			"(rewrites=%d) — the trust seed is not on this dispatch path, so this scenario proves nothing",
			rewrites)
	}

	// FLOORS, not sanity checks. rewrites>0 and observed>0 only prove the fixture is
	// not inert; neither notices a writer that has gone quiet, and a quiet writer
	// makes the liveness assertion above pass for the wrong reason. tryRewriteLocked
	// can now DISCARD a cycle, so "commits fewer rewrites than it used to" is a live
	// failure mode this fixture did not previously have — these two floors are what
	// catch it. 87b0e3ca4 recorded 804 rewrites and runs since land in the high
	// hundreds; the floor sits far below that and far above a writer that has
	// stopped mattering.
	if rewrites < tclMinRewrites {
		t.Errorf("hk-qx065 scenario: the hostile writer committed only %d rewrites (floor %d). It has "+
			"gone quiet, so the liveness result above is much weaker than it reads — a run can pass "+
			"simply because nothing contended with it",
			rewrites, tclMinRewrites)
	}
	if keysLearned == 0 {
		t.Errorf("hk-qx065 scenario: no trust key ever saturated the writer's erase budget "+
			"(rewrites=%d keysTargeted=%d). The repair loop was therefore never forced to engage, so a "+
			"green run here says nothing about whether repair works",
			rewrites, keysErased)
	}

	// ── Preservation (merge onto a FRESH read, never a stale snapshot) ──────
	// Measured by the writer itself, continuously, because an end-state check is
	// vacuous here: the writer stamps a new generation every 15ms, so it would
	// paper over a revert within one tick. The live probe cannot be papered over —
	// it records the worst backslide it ever observed.
	if maxBackslide > tclMaxTolerableBackslide {
		t.Errorf("hk-qx065 scenario: harmonik's write lost up to %d of the other writer's generations "+
			"(tolerable: %d). That is a stale-snapshot merge, not a fresh-read merge — at this size it "+
			"erases entries committed while harmonik was retrying, including a sibling worker's trust key",
			maxBackslide, tclMaxTolerableBackslide)
	}

	// The config must still parse after two concurrent atomic-rename writers, and
	// keep the content neither of them targeted.
	data, err := os.ReadFile(claudeCfg) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		t.Fatalf("hk-qx065 scenario: read final config: %v", err)
	}
	var final map[string]interface{}
	if err := json.Unmarshal(data, &final); err != nil {
		t.Fatalf("hk-qx065 scenario: final config is not valid JSON after concurrent writers: %v\n%s", err, data)
	}
	if _, ok := final["projects"].(map[string]interface{}); !ok {
		t.Errorf("hk-qx065 scenario: projects map missing from final config: %v", final)
	}
	if final["theme"] != "dark" {
		t.Errorf("hk-qx065 scenario: top-level key lost under concurrent writers; theme=%v", final["theme"])
	}

	// t.Errorf does not stop the test, so this line prints on failing runs too — and
	// it carries the exact numbers a reader mines when triaging one. Saying "PASS" on
	// a run that failed is the same overclaim this file's comments were corrected for.
	verdict := "PASS"
	if t.Failed() {
		verdict = "FAILED"
	}
	t.Logf("hk-qx065 scenario %s: N=%d completed=%d closed=%d | hostile rewrites=%d generation=%d "+
		"trustKeysTargeted=%d trustKeysLearned=%d maxBackslide=%d (bound %d)",
		verdict, len(res.BeadIDs), res.Completed, res.ClosedBeads, rewrites, generation, keysErased,
		keysLearned, maxBackslide, tclMaxTolerableBackslide)
}
