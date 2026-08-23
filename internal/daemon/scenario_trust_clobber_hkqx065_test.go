//go:build scenario

package daemon_test

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

const tclGenerationKey = "hostileWriterGeneration"

const tclOwnProject = "/invented/by/the/hostile/writer"

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

const tclErasesPerKey = 2

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

const tclCommitAttempts = 8

func (w *tclHostileWriter) rewriteOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for attempt := 0; attempt < tclCommitAttempts; attempt++ {
		if w.tryRewriteLocked() {
			return
		}
	}
}

func (w *tclHostileWriter) tryRewriteLocked() bool {
	data, err := os.ReadFile(w.cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		return true
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return true
	}

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

	var newlySeen []string
	drops := map[string]int{}

	for key := range w.seen {
		if w.learned[key] {
			continue
		}
		if _, present := projects[key]; !present {
			drops[key]++
		}
	}

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

	for key := range w.learned {
		if _, present := projects[key]; !present {
			projects[key] = map[string]interface{}{"hasTrustDialogAccepted": true}
		}
	}

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
	tmp := fmt.Sprintf("%s.hostile-tmp", w.cfgPath)
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return true
	}

	if w.beforeFreshnessCheck != nil {
		w.beforeFreshnessCheck()
	}

	fresh, err := os.ReadFile(w.cfgPath) //nolint:gosec // G304: test-controlled temp path
	if err != nil {
		return true
	}
	if !bytes.Equal(fresh, data) {
		return false
	}

	if err := os.Rename(tmp, w.cfgPath); err != nil {
		return true
	}

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

const tclMaxTolerableBackslide = 5

const tclMinRewrites = 200

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

var tclBootForTesting = vn4BootForTesting

// TestScenario_TrustClobber_ConcurrentDispatch_SurvivesHostileRewriter dispatches
// N=3 beads concurrently while a non-cooperating writer rewrites the shared
// claude config throughout, and asserts the liveness + preservation properties
// described in this file's header.
func TestScenario_TrustClobber_ConcurrentDispatch_SurvivesHostileRewriter(t *testing.T) {
	skipRealDaemonE2EInShort(t)

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

	hostile.Stop()
	generation, rewrites, keysErased, keysLearned, maxBackslide := hostile.snapshot()

	if res.Completed < len(res.BeadIDs) {
		t.Errorf("hk-qx065 scenario: only %d/%d runs completed under a hostile config rewriter "+
			"(the verify-and-repair loop must not hard-fail or stall a launch it can repair)",
			res.Completed, len(res.BeadIDs))
	}

	if rewrites == 0 {
		t.Fatalf("hk-qx065 scenario: the hostile writer never rewrote the config; fixture is inert")
	}
	if observed := hostile.observed.Load(); observed == 0 {
		t.Fatalf("hk-qx065 scenario: the hostile writer never saw a harmonik-written trust key "+
			"(rewrites=%d) — the trust seed is not on this dispatch path, so this scenario proves nothing",
			rewrites)
	}

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

	if maxBackslide > tclMaxTolerableBackslide {
		t.Errorf("hk-qx065 scenario: harmonik's write lost up to %d of the other writer's generations "+
			"(tolerable: %d). That is a stale-snapshot merge, not a fresh-read merge — at this size it "+
			"erases entries committed while harmonik was retrying, including a sibling worker's trust key",
			maxBackslide, tclMaxTolerableBackslide)
	}

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

	verdict := "PASS"
	if t.Failed() {
		verdict = "FAILED"
	}
	t.Logf("hk-qx065 scenario %s: N=%d completed=%d closed=%d | hostile rewrites=%d generation=%d "+
		"trustKeysTargeted=%d trustKeysLearned=%d maxBackslide=%d (bound %d)",
		verdict, len(res.BeadIDs), res.Completed, res.ClosedBeads, rewrites, generation, keysErased,
		keysLearned, maxBackslide, tclMaxTolerableBackslide)
}
