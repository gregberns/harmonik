package daemon

// gocachepath_hk137y6_test.go — the three properties the fleet's Go build cache
// must have, and the launch wiring that delivers them (bead hk-137y6).
//
// WHAT WENT WRONG. hk-gjbpp's mitigation told every agent to run
// `GOCACHE=$(mktemp -d) go test ./...`. Each invocation made a NEW ~220 MiB
// cache and nothing deleted them: 66 caches / 7.3 GiB in one night, which put
// the box under the daemon's 10 GiB watermark, which silently pauses ALL
// dispatch and presents as a code defect. Three crews debugged their own diffs
// against that one cause, and the operator hand-reclaimed disk twice.
//
// The defect was that the cache location was a CONVENTION each agent had to
// re-obey per command. These tests pin the properties so the convention cannot
// come back:
//
//   (i)   BOUNDED — the same agent gets the SAME path every time (reused, not
//         one per invocation). This is the mktemp regression test.
//   (ii)  NON-PURGEABLE — never under ~/Library/Caches, which macOS reclaims
//         wholesale under disk pressure (hk-pgtbr).
//   (iii) NOT THE GO DEFAULT — so `go clean -cache` against the default cache
//         cannot wipe an agent's build mid-verification (hk-gjbpp).
//
// Plus wiring tests: the crew launch spec and the captain tmux argv must
// actually CARRY the pin. A correct helper nobody calls fixes nothing.
//
// Beads: hk-137y6 (canonical), hk-d6xqn (subsumed), hk-pgtbr, hk-cy4ej, hk-gjbpp.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGoCacheDirFor_IsStablePerAgent pins property (i): the SAME agent resolves
// to the SAME directory on every call. `mktemp -d` returns a fresh directory
// per invocation — that is precisely the leak, so a non-deterministic path here
// is the defect returning.
func TestGoCacheDirFor_IsStablePerAgent(t *testing.T) {
	t.Parallel()

	const project = "/tmp/example-project"

	first := GoCacheDirFor(project, "mike")
	for i := range 5 {
		if got := GoCacheDirFor(project, "mike"); got != first {
			t.Fatalf("GoCacheDirFor call %d = %q; want %q — the path MUST be stable per agent, "+
				"a fresh path per invocation is the hk-137y6 leak (a ~220 MiB cache abandoned per command)", i, got, first)
		}
	}
}

// TestGoCacheDirFor_IsPerAgent verifies two agents do not share one cache.
// Sharing would reintroduce the hk-gjbpp hazard from the other direction: one
// agent's reap or clean would disturb another agent's in-flight build.
func TestGoCacheDirFor_IsPerAgent(t *testing.T) {
	t.Parallel()

	const project = "/tmp/example-project"

	if GoCacheDirFor(project, "mike") == GoCacheDirFor(project, "india") {
		t.Error("two agents resolved to the SAME cache directory; want one per agent so neither can disturb the other's build")
	}
}

// TestGoCacheDirFor_NotPurgeableAndNotGoDefault pins properties (ii) and (iii).
func TestGoCacheDirFor_NotPurgeableAndNotGoDefault(t *testing.T) {
	t.Parallel()

	got := GoCacheDirFor("/tmp/example-project", "mike")

	// (ii) NON-PURGEABLE. macOS reclaims everything under ~/Library/Caches
	// wholesale under disk pressure. hk-pgtbr: a merge gate failed with errors
	// inside Go's OWN standard library because that cache vanished mid-build,
	// and the daemon recorded the infrastructure fault as a BEAD rejection.
	if strings.Contains(got, filepath.Join("Library", "Caches")) {
		t.Errorf("cache dir %q is under Library/Caches — macOS purges that path wholesale under disk pressure (hk-pgtbr)", got)
	}

	// (iii) NOT THE GO DEFAULT, which is what the daemon's reap targets.
	if !strings.Contains(got, filepath.Join(".harmonik", "go-cache")) {
		t.Errorf("cache dir %q is not under the project's .harmonik/go-cache root; it must be off Go's default cache "+
			"so `go clean -cache` cannot wipe an agent's build mid-verification (hk-gjbpp)", got)
	}
}

// TestGoCacheDirFor_BlankAgentStillBounded verifies a caller that cannot name
// itself still gets a real directory rather than a path with an empty segment.
// The bounded/non-purgeable properties matter more than the name.
func TestGoCacheDirFor_BlankAgentStillBounded(t *testing.T) {
	t.Parallel()

	got := GoCacheDirFor("/tmp/example-project", "")
	if got == "" || strings.HasSuffix(got, string(filepath.Separator)) {
		t.Errorf("GoCacheDirFor with a blank agent = %q; want a real bounded directory", got)
	}
}

// TestCrewLaunchSpec_PinsGoCache is the WIRING test for crew sessions: the
// launch spec must actually carry GOCACHE, otherwise a crew inherits the
// default cache and the whole fix is theory.
func TestCrewLaunchSpec_PinsGoCache(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	spec, err := buildCrewLaunchSpec(crewLaunchCtx{
		name:       "mike",
		sessionID:  "11111111-2222-4333-8444-555555555555",
		projectDir: project,
	})
	if err != nil {
		t.Fatalf("buildCrewLaunchSpec: %v", err)
	}

	want := "GOCACHE=" + GoCacheDirFor(project, "mike")
	for _, e := range spec.Env {
		if e == want {
			return
		}
	}
	t.Errorf("crew launch spec Env = %v; want it to contain %q — without the pin a crew inherits Go's default "+
		"cache and either leaks a fresh one per command or gets wiped mid-build (hk-137y6)", spec.Env, want)
}

// ─────────────────────────────────────────────────────────────────────────────
// The reclaim must never delete a cache it cannot prove is stale (hk-137y6)
// ─────────────────────────────────────────────────────────────────────────────
//
// On 2026-07-22 something ran `go clean -cache` against a crew's private cache
// mid-experiment: 483M -> 8.0K, and the build then failed with "could not import
// os/exec" naming that crew's OWN path. The cache-miss errors nearly entered a
// flaky-test dataset as genuine failures. A contaminated measurement that looks
// like a result is worse than no measurement, so the disk-pressure reclaim gets
// a liveness guard and it FAILS CLOSED.

// TestGoCacheQuiescent_FreshCacheIsNotReapable is the regression test for that
// incident: a cache written to seconds ago must never be judged stale.
func TestGoCacheQuiescent_FreshCacheIsNotReapable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trim.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed cache file: %v", err)
	}

	if goCacheQuiescent(dir, time.Now()) {
		t.Error("a cache touched just now was judged quiescent; the reclaim would have deleted a live working set mid-build (hk-137y6)")
	}
}

// TestGoCacheQuiescent_OldCacheIsReapable verifies the guard still permits the
// reclaim to do its job — otherwise disk pressure has no relief valve at all and
// dispatch stays paused with nothing to free.
func TestGoCacheQuiescent_OldCacheIsReapable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "trim.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed cache file: %v", err)
	}

	// Ask about a moment well past the window rather than back-dating the files.
	future := time.Now().Add(goCacheQuiescenceWindow + time.Hour)
	if !goCacheQuiescent(dir, future) {
		t.Error("a cache untouched for longer than the quiescence window was judged in-use; the reclaim would never free anything and a disk-low daemon would stay paused forever")
	}
}

// TestGoCacheQuiescent_UninspectableFailsClosed pins the direction of failure:
// if the guard cannot inspect a directory it must claim the cache is IN USE.
// Reporting "safe to delete" about something it could not read is the one
// outcome that destroys other people's work.
func TestGoCacheQuiescent_UninspectableFailsClosed(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if goCacheQuiescent(missing, time.Now()) {
		t.Error("an uninspectable path was judged quiescent; the guard MUST fail closed (hk-137y6)")
	}
}

// TestGoCacheQuiescent_ShardChurnReadsAsInUse pins the reason the guard uses
// mtime rather than size — a reason that was TRUE BUT UNVERIFIED when the guard
// was written, and is verified here so a later change to the signal cannot
// silently remove the property.
//
// A size-based detector fires FALSELY on a live cache: Go tears down and
// rebuilds its 00–ff shard directories mid-run, so `du -sh` can read near-empty
// on a cache that is being actively written (juliet, 2026-07-22: 39M -> 8.0K in
// one minute on a healthy 245M cache — india's incident signature exactly, and
// nothing was wrong).
//
// Removing or creating a shard directory UPDATES THE PARENT'S mtime. So the
// exact event that fools a size check makes this guard read MORE recently-used,
// not less: it fails toward "in use" by mechanism, not by luck. That is the
// property worth pinning — a detector built from one incident inherits that
// incident's coincidences unless you copy the mechanism instead of the symptom.
func TestGoCacheQuiescent_ShardChurnReadsAsInUse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	shard := filepath.Join(dir, "ab")
	if err := os.Mkdir(shard, 0o755); err != nil {
		t.Fatalf("seed shard dir: %v", err)
	}

	// Back-date the cache so it would otherwise be judged reapable.
	old := time.Now().Add(-90 * 24 * time.Hour)
	for _, p := range []string{shard, dir} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("back-date %s: %v", p, err)
		}
	}
	if !goCacheQuiescent(dir, time.Now()) {
		t.Fatalf("precondition failed: a 90-day-old cache should be quiescent before any churn")
	}

	// Now simulate Go rebuilding its shards, which is what a live build does.
	if err := os.Remove(shard); err != nil {
		t.Fatalf("remove shard dir: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "cd"), 0o755); err != nil {
		t.Fatalf("recreate shard dir: %v", err)
	}

	if goCacheQuiescent(dir, time.Now()) {
		t.Error("shard churn left the cache judged QUIESCENT — the guard would delete a cache mid-build, " +
			"which is the failure a size-based detector has and mtime is supposed to avoid (hk-137y6)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// An explicitly-targeted cache must not be overridable by an inherited GOCACHE
// ─────────────────────────────────────────────────────────────────────────────
//
// The relocation call sites (per-agent cache reclaim, merge-gate build env)
// strip GOCACHE and then append their own, so the cache a command acts on is a
// property of the COMMAND rather than of whoever launched the daemon. These
// tests pin the strip itself, because everything above rests on it.
//
// Scope note: this helper is NOT wired into runGoCleanCache here — that is
// hk-agl8b and it is HELD (see the comment on goCleanOwnCacheEnv). These tests
// therefore guard the relocation sites, not the reap.

// TestGoCleanOwnCacheEnv_StripsInheritedGOCACHE is the regression test: an
// inherited GOCACHE must not survive to override the explicit target.
func TestGoCleanOwnCacheEnv_StripsInheritedGOCACHE(t *testing.T) {
	t.Parallel()

	parent := []string{"PATH=/usr/bin", "GOCACHE=/tmp/gocache-somebody-else", "HOME=/home/x"}

	for _, kv := range goCleanOwnCacheEnv(parent) {
		if strings.HasPrefix(kv, "GOCACHE=") {
			t.Fatalf("goCleanOwnCacheEnv leaked %q — an inherited cache could override the explicit target", kv)
		}
	}
}

// TestGoCleanOwnCacheEnv_PreservesEverythingElse guards against over-stripping:
// the command still needs PATH to find the go toolchain at all.
func TestGoCleanOwnCacheEnv_PreservesEverythingElse(t *testing.T) {
	t.Parallel()

	parent := []string{"PATH=/usr/bin", "GOCACHE=/tmp/x", "HOME=/home/x"}
	got := goCleanOwnCacheEnv(parent)

	if len(got) != 2 {
		t.Fatalf("goCleanOwnCacheEnv(%v) = %v; want the two non-GOCACHE entries preserved", parent, got)
	}
	for _, want := range []string{"PATH=/usr/bin", "HOME=/home/x"} {
		if !slices.Contains(got, want) {
			t.Errorf("goCleanOwnCacheEnv dropped %q; only GOCACHE may be removed", want)
		}
	}
}

// TestReapAgentGoCaches_HonoursQuiescence is the direct test for the reclaim
// loop itself (reviewer follow-up on hk-137y6): the quiescence guard is only
// worth having if the loop actually consults it.
//
// Two agent caches, one fresh and one long-idle. The fresh one must survive —
// it is a live working set — and the idle one may be reclaimed.
func TestReapAgentGoCaches_HonoursQuiescence(t *testing.T) {
	t.Parallel()

	project := t.TempDir()
	root := goCacheRootDir(project)

	fresh := GoCacheDirFor(project, "busy-agent")
	idle := GoCacheDirFor(project, "idle-agent")
	for _, d := range []string{fresh, idle} {
		if err := os.MkdirAll(filepath.Join(d, "ab"), 0o755); err != nil {
			t.Fatalf("seed %s: %v", d, err)
		}
	}
	// Age the idle one past the window; leave the fresh one at "now".
	old := time.Now().Add(-goCacheQuiescenceWindow - time.Hour)
	for _, p := range []string{filepath.Join(idle, "ab"), idle} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatalf("back-date %s: %v", p, err)
		}
	}

	if !goCacheQuiescent(idle, time.Now()) {
		t.Fatal("precondition: the back-dated cache should read as quiescent")
	}
	if goCacheQuiescent(fresh, time.Now()) {
		t.Fatal("precondition: the just-created cache should read as in-use")
	}

	reapAgentGoCaches(t.Context(), &workLoopDeps{projectDir: project})

	// The live cache must still be there. `go clean -cache` removes the hex
	// shard directories, so the shard surviving is the signal that the reclaim
	// left this agent alone.
	if _, err := os.Stat(filepath.Join(fresh, "ab")); err != nil {
		t.Errorf("the FRESH agent's cache shard was removed (%v) — the reclaim deleted a live working set, "+
			"which is india's incident reproduced inside our own fix (hk-137y6)", err)
	}
	// Sanity: the root still exists and we did not wander outside it.
	if _, err := os.Stat(root); err != nil {
		t.Errorf("cache root missing after reclaim: %v", err)
	}
}
