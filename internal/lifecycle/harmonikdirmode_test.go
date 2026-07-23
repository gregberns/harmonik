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
//     os.MkdirAll with a hand-written mode literal. Exemption is PER SITE — a
//     //dirmode:allow <reason> comment on the call's own line — so a file is
//     never wholly excused by one legitimate literal. excludedDirs and
//     dirModeAllowlist below are the inventory of what is still divergent, each
//     entry saying whether it is blocked or merely deferred, and to which bead.
//
// This file lives in internal/lifecycle because that package documents the
// per-project .harmonik/ file surface (daemonpaths.go, PL-004). It is an
// external test package (lifecycle_test) so it can import the other creators
// without an import cycle.

import (
	"context"
	"fmt"
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
	"github.com/gregberns/harmonik/internal/schedule"
	"github.com/gregberns/harmonik/internal/sessioncapture"
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
		{
			// Unblocked by the hk-8dtiv depguard change: internal/schedule may
			// now import internal/core, so its lazy .harmonik/ creation uses the
			// shared constant instead of a private 0o755.
			name:   "schedule.Store.Add",
			relDir: ".harmonik",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := schedule.NewStore(pd).Add(schedule.ScheduledJob{
					ID:       "job-1",
					Schedule: schedule.Schedule{Kind: "every", Interval: "1h"},
					Action:   schedule.Action{Kind: "command", Argv: []string{"true"}},
					Enabled:  true,
				}); err != nil {
					t.Fatalf("schedule.NewStore().Add: %v", err)
				}
			},
		},
		{
			// Same unblocking for internal/sessioncapture, which creates
			// .harmonik/sessions/<id>/ on Open.
			name:   "sessioncapture.Open",
			relDir: ".harmonik/sessions/sid-1",
			create: func(t *testing.T, pd string) {
				t.Helper()
				sess, err := sessioncapture.Open(context.Background(), sessioncapture.Config{
					WorkspacePath: pd,
					SessionID:     "sid-1",
				})
				if err != nil {
					t.Fatalf("sessioncapture.Open: %v", err)
				}
				if closeErr := sess.Close(); closeErr != nil {
					t.Fatalf("sessioncapture.Session.Close: %v", closeErr)
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

// siteAllowMarker is the PER-SITE exemption: an os.MkdirAll line that
// legitimately creates something other than a .harmonik state directory carries
//
//	//dirmode:allow <reason>
//
// on the same line. Site-granular is the point. The first cut of this guard was
// FILE-granular, and that silently voided it: cmd/harmonik/sync_assets_cmd.go
// was filed as "not a .harmonik state dir" while its writeFileEnsureDir was in
// fact creating .harmonik/context/ at 0o755 — the exact ordering bug
// core.HarmonikDirMode exists to remove, live inside a single `harmonik
// sync-assets --apply` run (internal/dashboard and the assets.lock write both
// create that tree at the constant). File granularity also left init_cmd.go,
// keeper_enable_doctor_cmd.go and harness.go WHOLLY exempt even though each has
// converted sites, so a regression in any of them was invisible.
const siteAllowMarker = "//dirmode:allow"

// dirModeAllowlist is the FILE-granular legacy escape hatch, kept only for trees
// outside cmd/harmonik/ that the site-marker conversion has not reached. Each
// entry exempts the WHOLE file, so it is strictly weaker than a site marker —
// prefer the marker; TestSiteMarkersPreferredOverFileAllowlist forbids new
// file-granular entries under cmd/harmonik/. Two kinds appear here:
//
//   - "tighter on purpose": the directory holds credentials or capability
//     tokens and is created 0o700. Never widen one of these to the constant.
//   - "not a .harmonik state dir": the file mentions ".harmonik" somewhere but
//     the call creates something else (a scenario fixture root, an
//     operator-supplied path).
//
// Files listed here have NO converted sites, so whole-file exemption costs no
// coverage today. Packages excluded wholesale (see excludedDirs) are NOT listed.
var dirModeAllowlist = map[string]string{
	"internal/run/registry.go":              "tighter on purpose: .harmonik/runs/ is 0o700 (run handles)",
	"internal/sentinel/trip_ev043b.go":      "tighter on purpose: .harmonik/decision_acks/ is 0o700 (ack tokens)",
	"internal/harness/codex/walguard.go":    "tighter on purpose: CODEX_HOME backup is 0o700 (agent credentials)",
	"internal/harness/pi/launchspec.go":     "tighter on purpose: pi agent dir is 0o700 (agent credentials)",
	"internal/scenario/fixtureroot.go":      "not a .harmonik state dir: per-suite fixture root under TMPDIR",
	"internal/scenario/synthprojectroot.go": "not a .harmonik state dir: synthesized scenario project root",
	"internal/scenario/resultemit.go":       "not a .harmonik state dir: scenario-result JSON output dir",
	"cmd/harmonik-twin-session/main.go":     "not a .harmonik state dir: operator-supplied HANDOFF path (single site)",

	// hk-8dtiv used to park internal/schedule/store.go and
	// internal/sessioncapture/sessioncapture.go here as STILL DIVERGENT: the
	// depguard component matrix fenced both packages off from internal/core, so
	// neither could name the constant. The matrix now allows the core edge and
	// both packages use core.HarmonikDirMode, so the entries are gone and both
	// files are covered by the scan below AND by a driven case above.
}

// excludedDirs are source trees this scan does not walk.
//
// Both production entries are STILL DIVERGENT for SCHEDULING reasons — the code
// is reachable, the fix is not blocked, it is simply owned by another in-flight
// change. Tracked as hk-b5ljs; closing it means deleting these entries.
var excludedDirs = []string{
	// STILL DIVERGENT (hk-b5ljs): 11 sites at 0o755, slice rewrite in flight.
	// The load-bearing one is daemon.go, which creates the .harmonik/ ROOT — so
	// until this lands the root's mode still depends on whether the daemon or
	// the CLI got there first.
	"internal/daemon/",
	// STILL DIVERGENT (hk-b5ljs): ~10 sites at 0o755, held by another agent.
	"internal/workspace/",
	// Test infrastructure, not production state.
	"internal/testhelpers/",
}

// mkdirAllCall locates the start of an os.MkdirAll call.
var mkdirAllCall = regexp.MustCompile(`os\.MkdirAll\(`)

// mkdirAllModeArg returns the MODE argument of the os.MkdirAll call that starts
// at the "os.MkdirAll(" match ending at openIdx (the index just past the "(").
// It walks to the matching close paren tracking nesting and string/rune
// literals, then takes the text after the last TOP-LEVEL comma — so a nested
// filepath.Join(a, b) in the path argument does not confuse it. Returns
// ("", false) when the call does not close on the text given.
func mkdirAllModeArg(text string, openIdx int) (string, bool) {
	depth := 1
	lastComma := -1
	for i := openIdx; i < len(text); i++ {
		switch c := text[i]; c {
		case '"', '\'', '`':
			// Skip the literal wholesale; its contents are not syntax.
			j := i + 1
			for j < len(text) {
				if text[j] == '\\' && c != '`' {
					j += 2
					continue
				}
				if text[j] == c {
					break
				}
				j++
			}
			if j >= len(text) {
				return "", false
			}
			i = j
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				if lastComma < 0 {
					return "", false
				}
				return strings.TrimSpace(text[lastComma+1 : i]), true
			}
		case ',':
			if depth == 1 {
				lastComma = i
			}
		case '\n':
			// Every os.MkdirAll call in this repo is single-line, and the
			// site marker is a same-line comment. A wrapped call would make
			// the marker ambiguous, so refuse rather than guess.
			return "", false
		}
	}
	return "", false
}

// TestNoLiteralDirModeInHarmonikPathFiles fails when a production file that
// builds a ".harmonik" path creates a directory with a hand-written mode
// instead of core.HarmonikDirMode. This is the guard that stops the two sides
// of the CLI/library boundary from silently re-diverging: a new creator added
// with a literal 0o755 lands as a test failure, not as an ordering-dependent
// permission bug.
//
// Exemption is PER SITE: put //dirmode:allow <reason> on the os.MkdirAll line.
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
			for _, loc := range mkdirAllCall.FindAllStringIndex(text, -1) {
				mode, ok := mkdirAllModeArg(text, loc[1])
				if !ok {
					findings = append(findings, rel+": os.MkdirAll call this guard could not parse (wrap it onto one line)")
					continue
				}
				if mode == "core.HarmonikDirMode" {
					continue
				}
				// Per-site exemption: the marker must be on the same line.
				lineEnd := strings.IndexByte(text[loc[0]:], '\n')
				if lineEnd < 0 {
					lineEnd = len(text) - loc[0]
				}
				line := text[loc[0] : loc[0]+lineEnd]
				marker := strings.Index(line, siteAllowMarker)
				if marker >= 0 && strings.TrimSpace(line[marker+len(siteAllowMarker):]) != "" {
					continue
				}
				lineNo := 1 + strings.Count(text[:loc[0]], "\n")
				findings = append(findings,
					fmt.Sprintf("%s:%d: os.MkdirAll mode %s", rel, lineNo, mode))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(findings) > 0 {
		t.Errorf("state-dir creation with a literal mode instead of core.HarmonikDirMode:\n  %s\n\n"+
			"Use core.HarmonikDirMode, or mark the specific line %s <reason>.",
			strings.Join(findings, "\n  "), siteAllowMarker)
	}
}

// TestSiteMarkersPreferredOverFileAllowlist keeps the file-granular hatch from
// creeping back into cmd/harmonik/, where whole-file exemption is what let the
// sync-assets divergence hide behind a mislabelled entry. Every exemption in
// that tree must be a per-site marker.
func TestSiteMarkersPreferredOverFileAllowlist(t *testing.T) {
	for rel := range dirModeAllowlist {
		if strings.HasPrefix(rel, "cmd/harmonik/") {
			t.Errorf("%s: cmd/harmonik/ files must use a per-site %s marker, not a whole-file allowlist entry",
				rel, siteAllowMarker)
		}
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
