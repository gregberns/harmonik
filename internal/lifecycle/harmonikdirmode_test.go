package lifecycle_test

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
	"github.com/gregberns/harmonik/internal/daemon"
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
	"github.com/gregberns/harmonik/internal/workspace"
)

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
		{
			name:   "daemon.CursorStore.Advance",
			relDir: ".harmonik/comms/cursors",
			create: func(t *testing.T, pd string) {
				t.Helper()
				dir := filepath.Join(pd, ".harmonik", "comms", "cursors")
				if err := daemon.NewCursorStore(dir).Advance("captain", uuid.Must(uuid.NewV7()).String()); err != nil {
					t.Fatalf("daemon.NewCursorStore().Advance: %v", err)
				}
				lockInfo, statErr := os.Stat(dir + ".locks")
				if statErr != nil {
					t.Fatalf("stat cursor lock dir: %v", statErr)
				}
				if got := lockInfo.Mode().Perm(); got != core.HarmonikDirMode {
					t.Errorf(".harmonik/comms/cursors.locks: mode = %v, want core.HarmonikDirMode (%v)", got, core.HarmonikDirMode)
				}
			},
		},
		{
			name:   "workspace.CreateSessionLogDir",
			relDir: ".harmonik/sessions/sid-1",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := workspace.CreateSessionLogDir(pd, "sid-1"); err != nil {
					t.Fatalf("workspace.CreateSessionLogDir: %v", err)
				}
			},
		},
		{
			name:   "workspace.WriteReviewVerdictAtomic",
			relDir: ".harmonik",
			create: func(t *testing.T, pd string) {
				t.Helper()
				if err := workspace.WriteReviewVerdictAtomic(pd, &workspace.ReviewVerdict{
					SchemaVersion: workspace.ReviewVerdictSchemaVersion,
					Verdict:       "APPROVE",
					Notes:         "driven dir-mode probe",
				}); err != nil {
					t.Fatalf("workspace.WriteReviewVerdictAtomic: %v", err)
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

const siteAllowMarker = "//dirmode:allow"

var dirModeAllowlist = map[string]string{
	"internal/run/registry.go":              "tighter on purpose: .harmonik/runs/ is 0o700 (run handles)",
	"internal/sentinel/trip_ev043b.go":      "tighter on purpose: .harmonik/decision_acks/ is 0o700 (ack tokens)",
	"internal/harness/codex/walguard.go":    "tighter on purpose: CODEX_HOME backup is 0o700 (agent credentials)",
	"internal/harness/pi/launchspec.go":     "tighter on purpose: pi agent dir is 0o700 (agent credentials)",
	"internal/scenario/fixtureroot.go":      "not a .harmonik state dir: per-suite fixture root under TMPDIR",
	"internal/scenario/synthprojectroot.go": "not a .harmonik state dir: synthesized scenario project root",
	"internal/scenario/resultemit.go":       "not a .harmonik state dir: scenario-result JSON output dir",
	"cmd/harmonik-twin-session/main.go":     "not a .harmonik state dir: operator-supplied HANDOFF path (single site)",

	"internal/daemon/pasteinject.go": "STILL DIVERGENT (hk-b5ljs follow-up): reviewer-budget sentinel dir at 0o755; file held by a concurrent lane",
}

var excludedDirs = []string{
	"internal/testhelpers/",
}

var mkdirAllCall = regexp.MustCompile(`os\.MkdirAll\(`)

func mkdirAllModeArg(text string, openIdx int) (string, bool) {
	depth := 1
	lastComma := -1
	for i := openIdx; i < len(text); i++ {
		switch c := text[i]; c {
		case '"', '\'', '`':
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

func repoRootFromTestFile(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
