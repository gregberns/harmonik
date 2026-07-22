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
//   - The repair actually engages, and is load-bearing. Measured, not assumed:
//     with the attempt budget cut to 1 (verify, no repair) this scenario goes
//     0/3 completed — the very first launch's key is erased between the write and
//     the verifying re-read. At a 15ms rewrite cadence the foreign writer lands
//     inside that window essentially every launch, so the retry is what carries
//     provisioning through.
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
// Erasures are BUDGETED per key (tclErasesPerKey) rather than unbounded, and the
// budget models the real thing precisely: a live claude's first few rewrites come
// from a snapshot taken BEFORE harmonik wrote the entry, so they drop it; once
// that process has re-read the config its snapshot carries the entry and it
// writes it back. So after tclErasesPerKey rounds the writer LEARNS a key and
// preserves it from then on.
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
}

// tclErasesPerKey is how many times the hostile writer drops any one trust key
// before leaving it alone. Kept below the trust writer's attempt budget so a key
// the writer targets while harmonik is still repairing can still settle.
const tclErasesPerKey = 2

// tclRewriteInterval paces the hostile writer. Fast enough to interleave heavily
// with three concurrent provisioning paths, slow enough not to saturate a core.
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

// rewriteOnce performs one wholesale read-modify-write with NO lock — the whole
// point. Errors are swallowed: a hostile writer racing an atomic rename can read
// a file that is momentarily absent, and that is not a test failure.
func (w *tclHostileWriter) rewriteOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := os.ReadFile(w.cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		return
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	// Lost-update probe. We stamped generation w.gen on our last rewrite. Reading
	// back something OLDER means another writer replaced the file with a snapshot
	// taken before that stamp — it merged onto stale bytes instead of a fresh
	// read. See tclMaxTolerableBackslide for the size that separates the inherent
	// race from the bug.
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
			w.dropped[key]++
			if w.dropped[key] >= tclErasesPerKey {
				w.learned[key] = true
			}
		}
	}

	// EXPLICIT drops: keys present in our snapshot that we do not yet know.
	for key := range projects {
		if key == tclOwnProject || w.learned[key] {
			continue
		}
		if !w.seen[key] {
			w.seen[key] = true
			w.observed.Add(1)
		}
		delete(projects, key)
		w.dropped[key]++
		if w.dropped[key] >= tclErasesPerKey {
			w.learned[key] = true
		}
	}

	// Keys we have learned are part of OUR snapshot now, so we write them back —
	// including on rewrites where harmonik has not yet re-applied them.
	for key := range w.learned {
		if _, present := projects[key]; !present {
			projects[key] = map[string]interface{}{"hasTrustDialogAccepted": true}
		}
	}

	// Commit content of our own that no earlier harmonik snapshot has seen.
	w.gen++
	projects[tclOwnProject] = map[string]interface{}{
		"hasTrustDialogAccepted": true,
		"generation":             float64(w.gen),
	}
	cfg[tclGenerationKey] = float64(w.gen)

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	// Atomic rename, exactly like the real writer: readers see a whole old or
	// whole new file.
	tmp := fmt.Sprintf("%s.hostile-tmp", w.cfgPath)
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, w.cfgPath)
	w.rewrite++
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
const tclMaxTolerableBackslide = 5

// snapshot returns the writer's own bookkeeping under its lock.
func (w *tclHostileWriter) snapshot() (generation, rewrites, keysErased, maxBackslide int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gen, w.rewrite, len(w.dropped), w.maxBackslide
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
		N:                 3,
		TwinScenario:      "single-happy-path",
		Boot:              tclBootForTesting(t),
		ExpectAllComplete: true,
		AgentReadyTimeout: 5 * time.Second,
		BeadPrefix:        "tcl",
		ClaudeConfigPath:  claudeCfg,
	})

	// Stop the writer BEFORE reading the file, so the assertions see a settled
	// state rather than racing one more rewrite.
	hostile.Stop()
	generation, rewrites, keysErased, maxBackslide := hostile.snapshot()

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

	t.Logf("hk-qx065 scenario PASS: N=%d completed=%d closed=%d | hostile rewrites=%d generation=%d "+
		"trustKeysTargeted=%d maxBackslide=%d (bound %d)",
		len(res.BeadIDs), res.Completed, res.ClosedBeads, rewrites, generation, keysErased,
		maxBackslide, tclMaxTolerableBackslide)
}
