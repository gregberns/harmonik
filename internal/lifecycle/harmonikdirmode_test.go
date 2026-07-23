package lifecycle_test

// harmonikdirmode_test.go — cross-package proof that every .harmonik/ state
// directory is created with the SAME mode, core.HarmonikDirMode.
//
// Why this test exists: os.MkdirAll does not chmod a directory that already
// exists — it returns nil and leaves the mode alone (TestMkdirAllDoesNotChmod
// below pins that behaviour). The .harmonik/ tree is created lazily from many
// packages AND from cmd/harmonik, so if any two creators disagree on the mode
// the result depends on which one ran first. A per-package mode constant would
// re-introduce exactly that, silently. Two guards:
//
//  1. TestStateDirCreatorsUseHarmonikDirMode drives the real exported creators
//     against a fresh project dir and asserts the mode they produce.
//  2. TestNoLiteralDirModeInHarmonikPathFiles scans the source of every
//     non-excluded package that builds a ".harmonik" path and fails on any
//     os.MkdirAll with a hand-written mode literal. The allowlist below is the
//     explicit, documented inventory of what is still divergent and why.
//
// This file lives in internal/lifecycle because that package documents the
// per-project .harmonik/ file surface (daemonpaths.go, PL-004). It is an
// external test package (lifecycle_test) so it can import the other creators
// without an import cycle.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/dashboard"
	"github.com/gregberns/harmonik/internal/goalstate"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/release"
	"github.com/gregberns/harmonik/internal/sessiondata"
	"github.com/gregberns/harmonik/internal/structuredlog"
	"github.com/gregberns/harmonik/internal/watch"
)

// withFixedUmask pins the process umask to 022 for the duration of a test so
// the asserted mode is exact regardless of the developer's or CI runner's
// ambient umask. 0o750 has no group-write and no other-write bit, so umask 022
// masks nothing out of it — the created mode is the requested mode.
//
// MUST NOT be used from a t.Parallel() test: the umask is process-global.
func withFixedUmask(t *testing.T) {
	t.Helper()
	prev := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(prev) })
}

// TestStateDirCreatorsUseHarmonikDirMode drives each exported .harmonik/
// state-directory creator and asserts the directory it creates has exactly
// core.HarmonikDirMode.
func TestStateDirCreatorsUseHarmonikDirMode(t *testing.T) {
	withFixedUmask(t)

	cases := []struct {
		name   string
		relDir string // path of the created dir, relative to the project dir
		create func(t *testing.T, projectDir string)
	}{
		{
			name:   "crew.Write",
			relDir: ".harmonik/crew",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := crew.Write(pd, crew.Record{Name: "alpha", SessionID: "s1"}); err != nil {
					t.Fatalf("crew.Write: %v", err)
				}
			},
		},
		{
			name:   "dashboard.Write",
			relDir: ".harmonik/context",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := dashboard.Write(pd, &dashboard.DashboardState{SchemaVersion: 1}); err != nil {
					t.Fatalf("dashboard.Write: %v", err)
				}
			},
		},
		{
			name:   "dashboard.WriteUnlock",
			relDir: ".harmonik/context",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := dashboard.WriteUnlock(pd, time.Now().Add(time.Hour), "test"); err != nil {
					t.Fatalf("dashboard.WriteUnlock: %v", err)
				}
			},
		},
		{
			name:   "goalstate.Write",
			relDir: ".harmonik/intent",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := goalstate.Write(pd, &goalstate.GoalState{SchemaVersion: 1}); err != nil {
					t.Fatalf("goalstate.Write: %v", err)
				}
			},
		},
		{
			name:   "keeper.SetDispatching",
			relDir: ".harmonik/keeper",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := keeper.SetDispatching(pd, "captain"); err != nil {
					t.Fatalf("keeper.SetDispatching: %v", err)
				}
			},
		},
		{
			name:   "keeper.WriteManagedSessionID",
			relDir: ".harmonik/keeper",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := keeper.WriteManagedSessionID(pd, "captain", "sid-1"); err != nil {
					t.Fatalf("keeper.WriteManagedSessionID: %v", err)
				}
			},
		},
		{
			name:   "lifecycle.WritePersistedTip",
			relDir: ".harmonik/run-tips",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := lifecycle.WritePersistedTip(pd, core.RunID(uuid.Must(uuid.NewV7())), "deadbeef"); err != nil {
					t.Fatalf("lifecycle.WritePersistedTip: %v", err)
				}
			},
		},
		{
			name:   "lifecycle.AcquireReconciliationLock",
			relDir: ".harmonik/reconciliation-locks",
			create: func(t *testing.T, pd string) {
				t.Helper()
				lk, err := lifecycle.AcquireReconciliationLock(pd, "run-1")
				if err != nil {
					t.Fatalf("lifecycle.AcquireReconciliationLock: %v", err)
				}
				t.Cleanup(func() {
					if relErr := lk.Release(); relErr != nil {
						t.Errorf("release reconciliation lock: %v", relErr)
					}
				})
			},
		},
		{
			name:   "lifecycle.WriteVerdictAttemptAtomic",
			relDir: ".harmonik/reconciliation-attempts",
			create: func(t *testing.T, pd string) {
				t.Helper()
				rec := &core.VerdictExecutionAttemptRecord{
					TargetRunID:   "run-1",
					Attempt:       1,
					LastAttemptAt: time.Now().UTC().Format(time.RFC3339),
				}
				if err := lifecycle.WriteVerdictAttemptAtomic(pd, rec); err != nil {
					t.Fatalf("lifecycle.WriteVerdictAttemptAtomic: %v", err)
				}
			},
		},
		{
			name:   "queue.Persist",
			relDir: ".harmonik/queues",
			create: func(t *testing.T, pd string) {
				t.Helper()
				q := &queue.Queue{SchemaVersion: 1, Name: queue.QueueNameMain}
				if err := queue.Persist(context.Background(), pd, q); err != nil {
					t.Fatalf("queue.Persist: %v", err)
				}
			},
		},
		{
			name:   "release.WriteLastGoodBinary",
			relDir: ".harmonik/state",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := release.WriteLastGoodBinary(release.LastGoodStatePath(pd), "/usr/bin/true"); err != nil {
					t.Fatalf("release.WriteLastGoodBinary: %v", err)
				}
			},
		},
		{
			name:   "sessiondata.Append",
			relDir: ".harmonik",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := sessiondata.Append(pd, sessiondata.Record{SchemaVersion: 1, RunID: "run-1"}); err != nil {
					t.Fatalf("sessiondata.Append: %v", err)
				}
			},
		},
		{
			name:   "structuredlog.NewHandler",
			relDir: ".harmonik/logs",
			create: func(t *testing.T, pd string) {
				t.Helper()
				h, err := structuredlog.NewHandler(structuredlog.Config{Subsystem: "test", ProjectDir: pd})
				if err != nil {
					t.Fatalf("structuredlog.NewHandler: %v", err)
				}
				t.Cleanup(func() {
					if closeErr := h.Close(); closeErr != nil {
						t.Errorf("close structuredlog handler: %v", closeErr)
					}
				})
			},
		},
		{
			name:   "watch.NewLedger",
			relDir: ".harmonik/watch",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if _, err := watch.NewLedger(filepath.Join(pd, ".harmonik")); err != nil {
					t.Fatalf("watch.NewLedger: %v", err)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			tc.create(t, projectDir)

			dir := filepath.Join(projectDir, filepath.FromSlash(tc.relDir))
			info, err := os.Stat(dir)
			if err != nil {
				t.Fatalf("stat %s: %v", dir, err)
			}
			if !info.IsDir() {
				t.Fatalf("%s: not a directory", dir)
			}
			if got := info.Mode().Perm(); got != core.HarmonikDirMode {
				t.Errorf("%s: mode = %v, want core.HarmonikDirMode (%v)", tc.relDir, got, core.HarmonikDirMode)
			}
		})
	}
}

// TestMkdirAllDoesNotChmodExisting pins the os.MkdirAll behaviour that makes a
// single shared mode constant necessary in the first place, and that justifies
// NOT chmod-ing existing installs: MkdirAll on an existing directory succeeds
// and leaves the mode untouched. If the stdlib ever changed this, the migration
// reasoning in core.HarmonikDirMode's doc comment would need revisiting.
func TestMkdirAllDoesNotChmodExisting(t *testing.T) {
	withFixedUmask(t)

	// legacyMode is what an install created before core.HarmonikDirMode existed
	// already has on disk: the current mode plus the world r-x bits that the old
	// 0o755 carried. Derived rather than written as a literal so this test does
	// not need a gosec G301 suppression to state its own premise.
	legacyMode := core.HarmonikDirMode | 0o005

	dir := filepath.Join(t.TempDir(), "a", "b")
	if err := os.MkdirAll(dir, legacyMode); err != nil {
		t.Fatalf("first MkdirAll: %v", err)
	}
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		t.Fatalf("second MkdirAll: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != legacyMode {
		t.Fatalf("MkdirAll re-applied a mode to an existing dir: got %v, want %v unchanged", got, legacyMode)
	}
}

// dirModeAllowlist enumerates the os.MkdirAll call sites that legitimately use
// a mode literal instead of core.HarmonikDirMode, keyed by repo-relative path.
// Every entry needs a reason. Two kinds appear here:
//
//   - "tighter on purpose": the directory holds credentials or capability
//     tokens and is created 0o700. Never widen one of these to the constant.
//   - "not a .harmonik state dir": the file mentions ".harmonik" somewhere but
//     the call creates something else (a .claude tree, a scenario fixture root,
//     an operator-supplied path).
//
// Packages excluded wholesale (see excludedDirs) are NOT listed here.
var dirModeAllowlist = map[string]string{
	"internal/run/registry.go":                 "tighter on purpose: .harmonik/runs/ is 0o700 (run handles)",
	"internal/sentinel/trip_ev043b.go":         "tighter on purpose: .harmonik/decision_acks/ is 0o700 (ack tokens)",
	"internal/harness/codex/walguard.go":       "tighter on purpose: CODEX_HOME backup is 0o700 (agent credentials)",
	"internal/harness/pi/launchspec.go":        "tighter on purpose: pi agent dir is 0o700 (agent credentials)",
	"internal/scenario/fixtureroot.go":         "not a .harmonik state dir: per-suite fixture root under TMPDIR",
	"internal/scenario/synthprojectroot.go":    "not a .harmonik state dir: synthesized scenario project root",
	"internal/scenario/resultemit.go":          "not a .harmonik state dir: scenario-result JSON output dir",
	"cmd/harmonik/init_cmd.go":                 "not a .harmonik state dir: .claude/skills/ scaffold",
	"cmd/harmonik/keeper_enable_doctor_cmd.go": "not a .harmonik state dir: .claude/settings.json parent",
	"cmd/harmonik/harness.go":                  "not a .harmonik state dir: scenario fixture root + seeded fixture files",
	"cmd/harmonik/subscribe.go":                "not a .harmonik state dir: operator-supplied --heartbeat-file path",
	"cmd/harmonik/sync_assets_cmd.go":          "not a .harmonik state dir: generic asset writer (.claude and .harmonik)",
	"cmd/harmonik-twin-session/main.go":        "not a .harmonik state dir: operator-supplied HANDOFF path",

	// Still divergent — tracked follow-up, NOT deliberate. Both packages are
	// fenced away from internal/core by the depguard component matrix
	// (.golangci.yml rules "schedule" and "sessioncapture" allow stdlib + self
	// only), so adopting the constant needs an enforced-config change that is
	// out of scope for the commit that introduced it.
	"internal/schedule/store.go":                "STILL DIVERGENT: creates .harmonik/ at 0o755; depguard fences schedule off from core",
	"internal/sessioncapture/sessioncapture.go": "STILL DIVERGENT: creates .harmonik/sessions/ at 0o755; depguard fences sessioncapture off from core",
}

// excludedDirs are source trees this scan does not walk.
var excludedDirs = []string{
	// Slice rewrite in flight; its .harmonik creators are a tracked follow-up.
	"internal/daemon/",
	// Held by another agent at the time this guard landed; also a tracked follow-up.
	"internal/workspace/",
	// Test infrastructure, not production state.
	"internal/testhelpers/",
}

var mkdirAllModeRE = regexp.MustCompile(`os\.MkdirAll\([^,]*,\s*([A-Za-z0-9_.]+)\s*\)`)

// TestNoLiteralDirModeInHarmonikPathFiles fails when a production file that
// builds a ".harmonik" path creates a directory with a hand-written mode
// instead of core.HarmonikDirMode. This is the guard that stops the two sides
// of the CLI/library boundary from silently re-diverging: a new creator added
// with a literal 0o755 lands as a test failure, not as an ordering-dependent
// permission bug.
func TestNoLiteralDirModeInHarmonikPathFiles(t *testing.T) {
	repoRoot := repoRootFromTestFile(t)

	var findings []string
	for _, top := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, top)
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)
			for _, ex := range excludedDirs {
				if strings.HasPrefix(rel, ex) {
					return nil
				}
			}
			if _, ok := dirModeAllowlist[rel]; ok {
				return nil
			}
			//nolint:gosec // G304: the path is a runtime value by construction — it
			// comes from WalkDir over the repo's own internal/ and cmd/ trees, so no
			// amount of validation removes the finding. What is given up is gosec's
			// tainted-path check on a test-only reader that opens nothing outside the
			// checkout it is compiled from.
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			text := string(src)
			if !strings.Contains(text, ".harmonik") {
				return nil
			}
			for _, m := range mkdirAllModeRE.FindAllStringSubmatch(text, -1) {
				if m[1] != "core.HarmonikDirMode" {
					findings = append(findings, rel+": os.MkdirAll mode "+m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(findings) > 0 {
		t.Errorf("state-dir creation with a literal mode instead of core.HarmonikDirMode:\n  %s\n\n"+
			"Use core.HarmonikDirMode, or add the file to dirModeAllowlist with a reason.",
			strings.Join(findings, "\n  "))
	}
}

// repoRootFromTestFile resolves the repository root from this package's
// location on disk (internal/lifecycle -> ../..).
func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
