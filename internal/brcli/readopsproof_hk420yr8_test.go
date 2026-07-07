package brcli_test

// readopsproof_hk420yr8_test.go — B3a subsystem-proofs: br-adapter read-ops
// acceptance suite.
//
// Proves the four read methods (Ready, ShowBead, ListInFlightBeads,
// ListBeadsByStatus) against a real `br` binary on a temp SQLite .beads/
// database. The database is seeded with `br init` into t.TempDir() and a
// NewForProject Adapter is used, matching the B3b write-ops fixture pattern.
//
// Acceptance criteria (task spec hk-420yr.8):
//
//  1. Ready: empty project returns empty non-nil slice (no beads → no error).
//  2. Ready: a freshly created open bead appears in the ready-work set.
//  3. ShowBead: returns the expected title, status, and type for a known bead.
//  4. ShowBead: a non-existent ID returns a non-nil error (ErrBrShowFailed with
//     real br, since the real binary emits error JSON to stderr per BI-025d;
//     ErrBeadNotFound is covered by show_test.go unit tests against a mock binary).
//  5. ListInFlightBeads: empty project returns empty non-nil slice.
//  6. ListInFlightBeads: a bead transitioned to in_progress appears in-flight.
//  7. ListBeadsByStatus("open"): a newly created bead appears in the open set.
//  8. ListBeadsByStatus("in_progress"): a claimed bead appears in the
//     in_progress set and NOT in the open set.
//
// Real-br tests (suffix _RealBr) skip automatically when `br` is not on PATH,
// so the suite stays green on environments without beads_rust installed.
//
// Spec ref: specs/beads-integration.md §4.5 BI-013, BI-015, BI-016.
// Bead ref: hk-420yr.8 (codename:subsystem-proofs).

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ── Fixture helpers (prefix b3aROP) ──────────────────────────────────────────

// b3aROPSkipIfNoBr skips the calling test when `br` is not on PATH and
// returns the resolved binary path when it is.
func b3aROPSkipIfNoBr(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("br")
	if err != nil {
		t.Skip("b3aROP: 'br' not on PATH — skipping (install beads_rust to run this suite)")
	}
	return path
}

// b3aROPTempProject creates a temp directory, runs `br init` inside it to
// initialize a real SQLite .beads/ database, and returns a NewForProject
// Adapter plus the project directory path.
func b3aROPTempProject(t *testing.T, brPath string) (adapter *brcli.Adapter, projectDir string) {
	t.Helper()
	projectDir = t.TempDir()

	//nolint:gosec // G204: brPath from exec.LookPath; args are static
	initCmd := exec.Command(brPath, "init")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("b3aROPTempProject: br init in %s: %v\n%s", projectDir, err, out)
	}

	a, err := brcli.NewForProject(brPath, projectDir)
	if err != nil {
		t.Fatalf("b3aROPTempProject: NewForProject: %v", err)
	}
	return a, projectDir
}

// b3aROPCreateBead runs `br create` in projectDir and returns the bead ID
// extracted from the success line "✓ Created <id>: <title>".
func b3aROPCreateBead(t *testing.T, brPath, projectDir, title string) core.BeadID {
	t.Helper()

	//nolint:gosec // G204: brPath from exec.LookPath; args are static
	cmd := exec.Command(brPath, "create", "--title", title, "--type", "task")
	cmd.Dir = projectDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("b3aROPCreateBead: br create: %v\n%s", err, out.String())
	}

	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.Contains(fields[1], "Created") {
			id := strings.TrimSuffix(fields[2], ":")
			if id != "" {
				return core.BeadID(id)
			}
		}
	}
	t.Fatalf("b3aROPCreateBead: cannot parse bead ID from br create output: %q", out.String())
	return ""
}

// b3aROPSetInProgress transitions a bead to in_progress via `br update
// --status in_progress` directly, without going through the adapter's write
// path. This lets B3a tests set up the necessary state while keeping the
// test focus on the read operations.
func b3aROPSetInProgress(t *testing.T, brPath, projectDir string, beadID core.BeadID) {
	t.Helper()

	//nolint:gosec // G204: brPath from exec.LookPath; args are static
	cmd := exec.Command(brPath, "update", string(beadID), "--status", "in_progress")
	cmd.Dir = projectDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("b3aROPSetInProgress: br update %s --status in_progress: %v\n%s", beadID, err, out.String())
	}
}

// b3aROPReadCtx returns a context with a generous read timeout for real-br
// tests (5 s read, consistent with BI-025c defaults).
func b3aROPReadCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// ── Ready acceptance tests ────────────────────────────────────────────────────

// TestB3a_Ready_EmptyProject_ReturnsEmptySlice_RealBr verifies that Ready
// returns a non-nil empty slice (not an error) when the project has no beads.
//
// Spec ref: specs/beads-integration.md §4.5 BI-013.
func TestB3a_Ready_EmptyProject_ReturnsEmptySlice_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, _ := b3aROPTempProject(t, brPath)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready on empty project: unexpected error: %v", err)
	}
	if records == nil {
		t.Error("Ready on empty project: returned nil slice, want non-nil empty slice")
	}
	if len(records) != 0 {
		t.Errorf("Ready on empty project: len(records) = %d, want 0", len(records))
	}
}

// TestB3a_Ready_OpenBeadAppearsInReadySet_RealBr verifies that a freshly
// created open bead with no dependencies appears in the ready-work set.
//
// This is the "Ready returns open bead" acceptance criterion for hk-420yr.8.
//
// Spec ref: specs/beads-integration.md §4.5 BI-013.
func TestB3a_Ready_OpenBeadAppearsInReadySet_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	beadID := b3aROPCreateBead(t, brPath, projectDir, "b3a-ready-test-bead")

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.Ready(ctx)
	if err != nil {
		t.Fatalf("Ready: unexpected error: %v", err)
	}

	found := false
	for _, r := range records {
		if r.BeadID == beadID {
			found = true
			if r.Status != core.CoarseStatusOpen {
				t.Errorf("ready record for %q: status = %q, want %q", beadID, r.Status, core.CoarseStatusOpen)
			}
			break
		}
	}
	if !found {
		t.Errorf("Ready: bead %q not found in ready set (got %d records)", beadID, len(records))
	}
}

// ── ShowBead acceptance tests ─────────────────────────────────────────────────

// TestB3a_ShowBead_ReturnsExpectedFields_RealBr verifies that ShowBead returns
// the correct title, status, and type for a bead that exists in the database.
//
// Spec ref: specs/beads-integration.md §4.5 BI-015.
func TestB3a_ShowBead_ReturnsExpectedFields_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	const title = "b3a-show-test-bead"
	beadID := b3aROPCreateBead(t, brPath, projectDir, title)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	record, err := adapter.ShowBead(ctx, beadID)
	if err != nil {
		t.Fatalf("ShowBead(%q): unexpected error: %v", beadID, err)
	}

	if record.BeadID != beadID {
		t.Errorf("ShowBead: BeadID = %q, want %q", record.BeadID, beadID)
	}
	if record.Title != title {
		t.Errorf("ShowBead: Title = %q, want %q", record.Title, title)
	}
	if record.Status != core.CoarseStatusOpen {
		t.Errorf("ShowBead: Status = %q, want %q", record.Status, core.CoarseStatusOpen)
	}
	if record.BeadType == "" {
		t.Errorf("ShowBead: BeadType is empty (want %q)", "task")
	}
}

// TestB3a_ShowBead_NonExistentID_ReturnsError_RealBr verifies that ShowBead
// returns a non-nil error when the requested bead ID does not exist in the
// real br database.
//
// Note: ErrBeadNotFound detection requires parsing the JSON error envelope
// from br's stdout, but the real br binary emits error JSON to stderr on
// non-zero exits. BI-025d prohibits parsing stderr for state. The unit tests
// in show_test.go cover the ErrBeadNotFound sentinel against a mock binary
// that puts the envelope on stdout. This real-br test verifies the weaker
// (but equally important) property: no panic, no nil error, no valid record.
//
// Spec ref: specs/beads-integration.md §4.5 BI-015; §4.8a BI-025d.
func TestB3a_ShowBead_NonExistentID_ReturnsError_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, _ := b3aROPTempProject(t, brPath)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	record, err := adapter.ShowBead(ctx, core.BeadID("hk-nonexistent-b3a"))
	if err == nil {
		t.Fatalf("ShowBead on non-existent ID: expected error, got nil (record=%+v)", record)
	}
	// The returned error should be ErrBrShowFailed (br exits 3, stdout is empty
	// because the real br puts error JSON on stderr). ErrBeadNotFound would
	// require stdout-envelope parsing which the real br doesn't support.
	if !errors.Is(err, brcli.ErrBrShowFailed) {
		t.Errorf("ShowBead on non-existent ID: expected ErrBrShowFailed, got: %v", err)
	}
}

// ── ListInFlightBeads acceptance tests ───────────────────────────────────────

// TestB3a_ListInFlightBeads_EmptyProject_ReturnsEmptySlice_RealBr verifies
// that ListInFlightBeads returns a non-nil empty slice when no beads are
// in_progress.
//
// Spec ref: specs/beads-integration.md §4.5 BI-016.
func TestB3a_ListInFlightBeads_EmptyProject_ReturnsEmptySlice_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, _ := b3aROPTempProject(t, brPath)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.ListInFlightBeads(ctx)
	if err != nil {
		t.Fatalf("ListInFlightBeads on empty project: unexpected error: %v", err)
	}
	if records == nil {
		t.Error("ListInFlightBeads on empty project: returned nil slice, want non-nil empty slice")
	}
	if len(records) != 0 {
		t.Errorf("ListInFlightBeads on empty project: len(records) = %d, want 0", len(records))
	}
}

// TestB3a_ListInFlightBeads_InProgressBeadAppearsInFlight_RealBr verifies
// that a bead transitioned to in_progress appears in the ListInFlightBeads
// result and carries the expected BeadID and Status.
//
// This is the "ListInFlightBeads returns in_progress bead" acceptance criterion
// for hk-420yr.8.
//
// Spec ref: specs/beads-integration.md §4.5 BI-016.
func TestB3a_ListInFlightBeads_InProgressBeadAppearsInFlight_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	beadID := b3aROPCreateBead(t, brPath, projectDir, "b3a-inflight-test-bead")
	b3aROPSetInProgress(t, brPath, projectDir, beadID)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.ListInFlightBeads(ctx)
	if err != nil {
		t.Fatalf("ListInFlightBeads: unexpected error: %v", err)
	}

	found := false
	for _, r := range records {
		if r.BeadID == beadID {
			found = true
			if r.Status != core.CoarseStatusInProgress {
				t.Errorf("in-flight record for %q: status = %q, want %q",
					beadID, r.Status, core.CoarseStatusInProgress)
			}
			break
		}
	}
	if !found {
		t.Errorf("ListInFlightBeads: bead %q not found in in-flight set (got %d records)",
			beadID, len(records))
	}
}

// ── ListBeadsByStatus acceptance tests ───────────────────────────────────────

// TestB3a_ListBeadsByStatus_Open_ReturnsCreatedBead_RealBr verifies that
// ListBeadsByStatus("open") returns a newly created bead (which defaults
// to open status).
//
// Spec ref: specs/beads-integration.md §4.5 BI-016; execution-model.md §4.7 EM-031a.
func TestB3a_ListBeadsByStatus_Open_ReturnsCreatedBead_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	beadID := b3aROPCreateBead(t, brPath, projectDir, "b3a-listbystatus-open-bead")

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.ListBeadsByStatus(ctx, "open")
	if err != nil {
		t.Fatalf("ListBeadsByStatus(open): unexpected error: %v", err)
	}

	found := false
	for _, r := range records {
		if r.BeadID == beadID {
			found = true
			if r.Status != core.CoarseStatusOpen {
				t.Errorf("open-status record for %q: status = %q, want %q",
					beadID, r.Status, core.CoarseStatusOpen)
			}
			break
		}
	}
	if !found {
		t.Errorf("ListBeadsByStatus(open): bead %q not found in open set (got %d records)",
			beadID, len(records))
	}
}

// TestB3a_ListBeadsByStatus_InProgress_AfterTransition_RealBr verifies that
// ListBeadsByStatus("in_progress") returns a bead after it has been
// transitioned to in_progress, and that the same bead no longer appears in
// the open set.
//
// This covers the EM-031a generalisation: active-run discovery queries all
// non-terminal statuses, not only in_progress via ListInFlightBeads.
//
// Spec ref: execution-model.md §4.7 EM-031a; beads-integration.md §4.5 BI-016.
func TestB3a_ListBeadsByStatus_InProgress_AfterTransition_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	beadID := b3aROPCreateBead(t, brPath, projectDir, "b3a-listbystatus-inprogress-bead")
	b3aROPSetInProgress(t, brPath, projectDir, beadID)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	// Must appear in in_progress set.
	inProgress, err := adapter.ListBeadsByStatus(ctx, "in_progress")
	if err != nil {
		t.Fatalf("ListBeadsByStatus(in_progress): unexpected error: %v", err)
	}
	foundInProgress := false
	for _, r := range inProgress {
		if r.BeadID == beadID {
			foundInProgress = true
			if r.Status != core.CoarseStatusInProgress {
				t.Errorf("in_progress record for %q: status = %q, want %q",
					beadID, r.Status, core.CoarseStatusInProgress)
			}
			break
		}
	}
	if !foundInProgress {
		t.Errorf("ListBeadsByStatus(in_progress): bead %q not found (got %d records)",
			beadID, len(inProgress))
	}

	// Must NOT appear in open set after transitioning to in_progress.
	open, err := adapter.ListBeadsByStatus(ctx, "open")
	if err != nil {
		t.Fatalf("ListBeadsByStatus(open): unexpected error: %v", err)
	}
	for _, r := range open {
		if r.BeadID == beadID {
			t.Errorf("ListBeadsByStatus(open): bead %q appears in open set after in_progress transition",
				beadID)
			break
		}
	}
}

// TestB3a_ListBeadsByStatus_EmptyStatus_ReturnsError verifies that
// ListBeadsByStatus rejects an empty status string without invoking br.
//
// This is a defensive check on the adapter's input validation (not real-br).
func TestB3a_ListBeadsByStatus_EmptyStatus_ReturnsError(t *testing.T) {
	t.Parallel()

	// Use a non-existent binary path — the error must be triggered by the
	// adapter's own validation before any exec, so the binary path is irrelevant.
	adapter, err := brcli.New("/nonexistent/path/to/br")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, err = adapter.ListBeadsByStatus(context.Background(), "")
	if err == nil {
		t.Fatal("ListBeadsByStatus(empty): expected error for empty status, got nil")
	}
}

// TestB3a_ReadyAll_EmptyProject_ReturnsEmptySlice_RealBr verifies that
// ReadyAll (the un-paginated variant, hk-95uf defense #1) returns an empty
// non-nil slice when the project has no beads.
//
// Spec ref: specs/beads-integration.md §4.5 BI-013.
func TestB3a_ReadyAll_EmptyProject_ReturnsEmptySlice_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, _ := b3aROPTempProject(t, brPath)

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.ReadyAll(ctx)
	if err != nil {
		t.Fatalf("ReadyAll on empty project: unexpected error: %v", err)
	}
	if records == nil {
		t.Error("ReadyAll on empty project: returned nil slice, want non-nil empty slice")
	}
	if len(records) != 0 {
		t.Errorf("ReadyAll on empty project: len(records) = %d, want 0", len(records))
	}
}

// TestB3a_ReadyAll_OpenBeadAppearsInFullSet_RealBr verifies that ReadyAll
// surfaces a freshly created open bead, proving the --limit 0 path returns
// the full un-paginated set (hk-95uf genuine-drain oracle, defense #1).
//
// Spec ref: specs/beads-integration.md §4.5 BI-013.
func TestB3a_ReadyAll_OpenBeadAppearsInFullSet_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3aROPSkipIfNoBr(t)

	adapter, projectDir := b3aROPTempProject(t, brPath)
	beadID := b3aROPCreateBead(t, brPath, projectDir, "b3a-readyall-test-bead")

	ctx, cancel := b3aROPReadCtx(t)
	defer cancel()

	records, err := adapter.ReadyAll(ctx)
	if err != nil {
		t.Fatalf("ReadyAll: unexpected error: %v", err)
	}

	found := false
	for _, r := range records {
		if r.BeadID == beadID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ReadyAll: bead %q not found in full ready set (got %d records)",
			beadID, len(records))
	}
}

// ── Spec sentinel tests ───────────────────────────────────────────────────────

// b3aROPSpecContent reads specs/beads-integration.md relative to the repo
// root (derived from this test file's path via runtime.Caller) and returns
// its content.
func b3aROPSpecContent(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("b3aROPSpecContent: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/brcli/<file>.go → repo root is two dirs up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")
	//nolint:gosec // G304: path constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("b3aROPSpecContent: read %s: %v", specPath, err)
	}
	return string(raw)
}

// TestB3a_ReadSurface_SpecSentinel verifies that the beads-integration spec
// contains the key read-surface contract phrases that the adapter's read
// methods (BI-013, BI-015, BI-016) depend on.
//
// Removing either phrase from the spec is a breaking change: agents and the
// adapter lose the normative anchor for their read-surface behaviour.
//
// Spec ref: specs/beads-integration.md §4.5 BI-013, BI-015, BI-016.
func TestB3a_ReadSurface_SpecSentinel(t *testing.T) {
	t.Parallel()

	spec := b3aROPSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "BI-013 — Ready-work query (orchestrator-facing)",
			hint: "BI-013 section header must exist in beads-integration spec; " +
				"removing it loses the normative anchor for the ready-work read surface " +
				"and the adapter's br ready invocation contract",
		},
		{
			phrase: "BI-015 — Bead-detail query",
			hint: "BI-015 section header must exist in beads-integration spec; " +
				"removing it loses the normative anchor for ShowBead / br show",
		},
		{
			phrase: "BI-016 — Reconciliation queries",
			hint: "BI-016 section header must exist in beads-integration spec; " +
				"removing it loses the normative anchor for ListInFlightBeads " +
				"and ListBeadsByStatus (EM-031a generalisation)",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(spec, tc.phrase) {
			t.Errorf(
				"beads-integration spec does not contain %q\n  hint: %s",
				tc.phrase, tc.hint,
			)
		}
	}
}
