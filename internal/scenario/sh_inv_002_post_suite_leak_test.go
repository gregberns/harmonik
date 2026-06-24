package scenario

// sh_inv_002_post_suite_leak_test.go — contract tests for the SH-INV-002
// post-suite leak sensor (CheckPostSuiteLeaks).
//
// Per specs/scenario-harness.md §5 SH-INV-002 and §10.2: the sensor MUST be
// called after all scenario teardowns complete and BEFORE WriteSuiteResult.
// It inspects three resource categories:
//
//	(i)   process — no live HARMONIK_RUN_ID-marked descendant processes
//	(ii)  lease   — no held worktree lease.lock files under the fixture root
//	(iii) fd      — no open event-log file descriptors under the fixture root
//
// Test naming: shINV002PostSuiteLeak* (helper prefix per implementer-protocol).
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002, §10.2.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

func shINV002TempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "harmonik-shinv002-")
	if err != nil {
		t.Fatalf("shINV002TempDir: MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// shINV002WriteLease writes a minimal lease.lock file at
// <workspacePath>/.harmonik/lease.lock with the given run_id.
func shINV002WriteLease(t *testing.T, workspacePath string, runID core.RunID) string {
	t.Helper()
	lockDir := filepath.Join(workspacePath, ".harmonik")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		t.Fatalf("shINV002WriteLease: MkdirAll: %v", err)
	}
	lockPath := filepath.Join(lockDir, "lease.lock")
	content := map[string]interface{}{
		"run_id":     runID.String(),
		"pid":        os.Getpid(),
		"created_at": "2026-01-01T00:00:00Z",
		"ttl_sec":    3600,
	}
	data, _ := json.Marshal(content)
	data = append(data, '\n')
	//nolint:gosec // G306: 0644 is appropriate for test fixture files
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatalf("shINV002WriteLease: WriteFile: %v", err)
	}
	return lockPath
}

// shINV002MakeRunID returns a deterministic test RunID from a UUID string.
func shINV002MakeRunID(t *testing.T, uuidStr string) core.RunID {
	t.Helper()
	u, err := uuid.Parse(uuidStr)
	if err != nil {
		t.Fatalf("shINV002MakeRunID: parse %q: %v", uuidStr, err)
	}
	return core.RunID(u)
}

// ─────────────────────────────────────────────────────────────────────────────
// LeakKind constants
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV002_LeakKindValues verifies the three LeakKind constants are
// declared with the expected string values used in Detail strings.
func TestSHINV002_LeakKindValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		kind LeakKind
		want string
	}{
		{LeakKindProcess, "process"},
		{LeakKindLease, "lease"},
		{LeakKindFD, "fd"},
	}
	for _, tc := range cases {
		if string(tc.kind) != tc.want {
			t.Errorf("LeakKind %q != %q", tc.kind, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PostSuiteLeakReport.HasLeaks
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV002_HasLeaks_FalseOnEmpty verifies HasLeaks returns false for an
// empty report (no leaks detected — the normal post-teardown case).
func TestSHINV002_HasLeaks_FalseOnEmpty(t *testing.T) {
	t.Parallel()

	report := &PostSuiteLeakReport{}
	if report.HasLeaks() {
		t.Error("HasLeaks() = true on empty Leaks slice; want false")
	}
}

// TestSHINV002_HasLeaks_TrueWhenLeaksPresent verifies HasLeaks returns true
// when at least one LeakDescriptor is recorded.
func TestSHINV002_HasLeaks_TrueWhenLeaksPresent(t *testing.T) {
	t.Parallel()

	report := &PostSuiteLeakReport{
		Leaks: []LeakDescriptor{{Kind: LeakKindLease, Detail: "test"}},
	}
	if !report.HasLeaks() {
		t.Error("HasLeaks() = false when Leaks is non-empty; want true")
	}
}

// TestSHINV002_HasLeaks_SafeOnNilReport verifies HasLeaks does not panic on a
// nil *PostSuiteLeakReport receiver.
func TestSHINV002_HasLeaks_SafeOnNilReport(t *testing.T) {
	t.Parallel()

	var report *PostSuiteLeakReport
	if report.HasLeaks() {
		t.Error("HasLeaks() on nil report = true; want false")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// checkLeakedLeases — SH-INV-002(ii)
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV002_LeaseCheck_NoLeaseLockFiles reports no leaks when the fixture
// root contains no lease.lock files.
func TestSHINV002_LeaseCheck_NoLeaseLockFiles(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	runID := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000001")

	leaks, err := checkLeakedLeases(fixtureRoot, []core.RunID{runID})
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 0 (no lease.lock files present)", len(leaks))
	}
}

// TestSHINV002_LeaseCheck_DetectsHeldLease verifies that a lease.lock file
// whose run_id matches an executed run_id is reported as LeakKindLease.
func TestSHINV002_LeaseCheck_DetectsHeldLease(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	runID := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000002")

	// Create a workspace directory under the fixture root and write a lease.
	workspacePath := filepath.Join(fixtureRoot, "my-scenario", "workspace")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lockPath := shINV002WriteLease(t, workspacePath, runID)

	leaks, err := checkLeakedLeases(fixtureRoot, []core.RunID{runID})
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error: %v", err)
	}
	if len(leaks) != 1 {
		t.Fatalf("checkLeakedLeases: got %d leaks; want 1", len(leaks))
	}
	if leaks[0].Kind != LeakKindLease {
		t.Errorf("leaks[0].Kind = %q; want %q", leaks[0].Kind, LeakKindLease)
	}
	// Detail must mention the run_id and the file path.
	if !containsSubstring(leaks[0].Detail, runID.String()) {
		t.Errorf("leaks[0].Detail = %q; want run_id %s", leaks[0].Detail, runID)
	}
	if !containsSubstring(leaks[0].Detail, lockPath) {
		t.Errorf("leaks[0].Detail = %q; want lock path %s", leaks[0].Detail, lockPath)
	}
}

// TestSHINV002_LeaseCheck_SkipsUnrelatedRunID verifies that a lease.lock file
// whose run_id does NOT match any executed run_id is not reported.
func TestSHINV002_LeaseCheck_SkipsUnrelatedRunID(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	// Write a lease held by an UNRELATED run_id.
	unrelatedRunID := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000010")
	executedRunID := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000011")

	workspacePath := filepath.Join(fixtureRoot, "other-scenario", "workspace")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	shINV002WriteLease(t, workspacePath, unrelatedRunID)

	// executedRunID does NOT match the on-disk lease.
	leaks, err := checkLeakedLeases(fixtureRoot, []core.RunID{executedRunID})
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 0 (run_id does not match)", len(leaks))
	}
}

// TestSHINV002_LeaseCheck_EmptyExecutedRunIDs returns no leaks when the
// executed run_id list is empty (vacuous: nothing to match against).
func TestSHINV002_LeaseCheck_EmptyExecutedRunIDs(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	// Even if a lease.lock exists, an empty executedRunIDs causes an early exit.
	workspacePath := filepath.Join(fixtureRoot, "s", "workspace")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	rid := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000020")
	shINV002WriteLease(t, workspacePath, rid)

	leaks, err := checkLeakedLeases(fixtureRoot, nil)
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 0 (empty executedRunIDs)", len(leaks))
	}
}

// TestSHINV002_LeaseCheck_EmptyFixtureRoot returns nil, nil immediately when
// fixtureRoot is empty.
func TestSHINV002_LeaseCheck_EmptyFixtureRoot(t *testing.T) {
	t.Parallel()

	rid := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000030")
	leaks, err := checkLeakedLeases("", []core.RunID{rid})
	if err != nil {
		t.Fatalf("checkLeakedLeases with empty fixtureRoot: error: %v; want nil", err)
	}
	if len(leaks) != 0 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 0 (empty fixtureRoot)", len(leaks))
	}
}

// TestSHINV002_LeaseCheck_MalformedLeaseLockSkipped verifies that a malformed
// lease.lock file (invalid JSON) is silently skipped and not reported as a leak.
func TestSHINV002_LeaseCheck_MalformedLeaseLockSkipped(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	workspacePath := filepath.Join(fixtureRoot, "malformed-scenario", "workspace")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(filepath.Join(workspacePath, ".harmonik"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	lockPath := filepath.Join(workspacePath, ".harmonik", "lease.lock")
	//nolint:gosec // G306: test fixture
	if err := os.WriteFile(lockPath, []byte("not-json\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	rid := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000040")
	leaks, err := checkLeakedLeases(fixtureRoot, []core.RunID{rid})
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error on malformed file: %v", err)
	}
	if len(leaks) != 0 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 0 (malformed lease.lock skipped)", len(leaks))
	}
}

// TestSHINV002_LeaseCheck_MultipleMatchedLeases verifies that multiple held
// leases are all reported when all match executed run_ids.
func TestSHINV002_LeaseCheck_MultipleMatchedLeases(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	rid1 := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000051")
	rid2 := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000052")

	for i, rid := range []core.RunID{rid1, rid2} {
		ws := filepath.Join(fixtureRoot, fmt.Sprintf("scenario-%d", i), "workspace")
		//nolint:gosec // G301: test fixture directory
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		shINV002WriteLease(t, ws, rid)
	}

	leaks, err := checkLeakedLeases(fixtureRoot, []core.RunID{rid1, rid2})
	if err != nil {
		t.Fatalf("checkLeakedLeases: unexpected error: %v", err)
	}
	if len(leaks) != 2 {
		t.Errorf("checkLeakedLeases: got %d leaks; want 2", len(leaks))
	}
	for _, l := range leaks {
		if l.Kind != LeakKindLease {
			t.Errorf("leak Kind = %q; want %q", l.Kind, LeakKindLease)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CheckPostSuiteLeaks — integration
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV002_CheckPostSuiteLeaks_EmptyParamsNoError verifies that calling
// CheckPostSuiteLeaks with empty params returns a non-nil report with no leaks
// and no error. This is the vacuous clean case.
func TestSHINV002_CheckPostSuiteLeaks_EmptyParamsNoError(t *testing.T) {
	t.Parallel()

	report, err := CheckPostSuiteLeaks(t.Context(), PostSuiteLeakParams{})
	if err != nil {
		t.Fatalf("CheckPostSuiteLeaks with empty params: error: %v; want nil", err)
	}
	if report == nil {
		t.Fatal("CheckPostSuiteLeaks: report is nil; want non-nil")
	}
	if report.HasLeaks() {
		t.Errorf("CheckPostSuiteLeaks with empty params: HasLeaks() = true; want false (no leaks on empty fixture root)")
	}
}

// TestSHINV002_CheckPostSuiteLeaks_CleanFixtureNoLeaks verifies that a fixture
// root with no lease.lock files and empty executedRunIDs produces no leaks.
func TestSHINV002_CheckPostSuiteLeaks_CleanFixtureNoLeaks(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	// Create a workspace subdirectory (no lease.lock).
	ws := filepath.Join(fixtureRoot, "clean-scenario", "workspace", ".harmonik")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	params := PostSuiteLeakParams{
		FixtureRoot:    fixtureRoot,
		ExecutedRunIDs: nil,
	}
	report, err := CheckPostSuiteLeaks(t.Context(), params)
	if err != nil {
		t.Fatalf("CheckPostSuiteLeaks: error: %v; want nil", err)
	}
	if report == nil {
		t.Fatal("report is nil; want non-nil")
	}
	if report.HasLeaks() {
		t.Errorf("HasLeaks() = true on clean fixture; want false\nleaks: %v", report.Leaks)
	}
}

// TestSHINV002_CheckPostSuiteLeaks_DetectsHeldLeaseViaFullCheck verifies that
// CheckPostSuiteLeaks detects a held lease.lock file and includes it in
// PostSuiteLeakReport.Leaks.
func TestSHINV002_CheckPostSuiteLeaks_DetectsHeldLeaseViaFullCheck(t *testing.T) {
	t.Parallel()

	fixtureRoot := shINV002TempDir(t)
	rid := shINV002MakeRunID(t, "00000000-0000-0000-0000-000000000060")

	workspacePath := filepath.Join(fixtureRoot, "leaked-scenario", "workspace")
	//nolint:gosec // G301: test fixture directory
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	shINV002WriteLease(t, workspacePath, rid)

	params := PostSuiteLeakParams{
		FixtureRoot:    fixtureRoot,
		ExecutedRunIDs: []core.RunID{rid},
	}
	report, err := CheckPostSuiteLeaks(t.Context(), params)
	if err != nil {
		t.Fatalf("CheckPostSuiteLeaks: error: %v; want nil", err)
	}
	if report == nil {
		t.Fatal("report is nil; want non-nil")
	}
	if !report.HasLeaks() {
		t.Error("HasLeaks() = false; want true (held lease.lock must be detected)")
	}

	// At minimum one LeakKindLease must appear in the report.
	foundLease := false
	for _, l := range report.Leaks {
		if l.Kind == LeakKindLease {
			foundLease = true
			break
		}
	}
	if !foundLease {
		t.Errorf("no LeakKindLease in report.Leaks; got %v", report.Leaks)
	}
}

// TestSHINV002_CheckPostSuiteLeaks_ReturnTypeIsNonNilReport verifies the
// return type contract: CheckPostSuiteLeaks always returns a non-nil
// *PostSuiteLeakReport on success (nil only on error).
func TestSHINV002_CheckPostSuiteLeaks_ReturnTypeIsNonNilReport(t *testing.T) {
	t.Parallel()

	report, err := CheckPostSuiteLeaks(t.Context(), PostSuiteLeakParams{
		FixtureRoot: shINV002TempDir(t),
	})
	if err != nil {
		t.Fatalf("CheckPostSuiteLeaks: error: %v", err)
	}
	if report == nil {
		t.Fatal("CheckPostSuiteLeaks: nil report on success; want non-nil *PostSuiteLeakReport")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Spec-corpus sensor: verify SH-INV-002 is declared in the spec
// ─────────────────────────────────────────────────────────────────────────────

// TestSHINV002_SpecCorpus_SensorDeclaredInSpec verifies that the
// scenario-harness spec (v0.2.2) declares SH-INV-002 and the three required
// check categories. This guards against spec-drift silently removing the
// sensor obligation.
func TestSHINV002_SpecCorpus_SensorDeclaredInSpec(t *testing.T) {
	t.Parallel()

	root := conformanceCorpusFixtureRepoRoot(t)
	specPath := filepath.Join(root, "specs", "scenario-harness.md")
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec %q: %v", specPath, err)
	}
	spec := string(data)

	required := []string{
		"SH-INV-002",
		"HARMONIK_RUN_ID",
		"lease.lock",
		"lsof",
	}
	for _, token := range required {
		if !containsSubstring(spec, token) {
			t.Errorf("spec %q missing required token %q; spec may have drifted", specPath, token)
		}
	}
}
