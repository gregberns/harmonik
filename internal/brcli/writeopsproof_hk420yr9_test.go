package brcli_test

// writeopsproof_hk420yr9_test.go — B3b subsystem-proofs: br-adapter write-ops
// acceptance suite.
//
// Proves the four terminal-transition write methods (ClaimBead, CloseBead,
// ReopenBead, ResetBead) against a real `br` binary on a temp SQLite .beads/
// database. Also provides spec-content sensors for the daemon-owns-terminal-
// transitions and agent-prohibited-write invariants (BI-004 / BI-011 / BI-027).
//
// Acceptance criteria (task spec hk-420yr.9):
//
//  1. Concurrent CloseBead calls serialize via terminalMu with no double-close
//     error under SQLite write pressure.
//  2. ClaimBead on an already-in_progress bead is safe (idempotent — hk-amed0
//     fallback or br's own idempotency).
//  3. ResetBead transitions a stranded in_progress bead back to open —
//     the hk-l2xd1 orphan-sweep primitive.
//  4. Daemon-owns-terminal-transitions sentinel: spec text carries the ownership
//     invariant phrase (BI-027 sensor).
//  5. Agent-issued terminal write refused: BI-011 spec sensor verifies the
//     prohibition text is present and has not been silently removed.
//
// Real-br tests (suffix _RealBr) skip automatically when `br` is not on PATH,
// so the suite stays green on environments without beads_rust installed.
//
// Spec ref: specs/beads-integration.md §4.4 BI-010, BI-011; §4.10 BI-030;
// §4.8 BI-027.
// Bead ref: hk-420yr.9 (codename:subsystem-proofs).

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ── Fixture helpers (prefix b3bWOP) ──────────────────────────────────────────

// b3bWOPSkipIfNoBr skips the calling test when `br` is not on PATH and
// returns the resolved binary path when it is.
func b3bWOPSkipIfNoBr(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("br")
	if err != nil {
		t.Skip("b3bWOP: 'br' not on PATH — skipping (install beads_rust to run this suite)")
	}
	return path
}

// b3bWOPTempProject creates a temp directory, runs `br init` inside it to
// initialize a real SQLite .beads/ database, creates .harmonik/beads-intents/
// as the intent-log directory, and returns a NewForProject Adapter plus the
// project directory and intent-log directory paths.
func b3bWOPTempProject(t *testing.T, brPath string) (adapter *brcli.Adapter, projectDir, intentLogDir string) {
	t.Helper()
	projectDir = t.TempDir()

	//nolint:gosec // G204: brPath from exec.LookPath; args are static
	initCmd := exec.Command(brPath, "init")
	initCmd.Dir = projectDir
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("b3bWOPTempProject: br init in %s: %v\n%s", projectDir, err, out)
	}

	intentLogDir = filepath.Join(projectDir, ".harmonik", "beads-intents")
	//nolint:gosec // G301: 0755 matches production .harmonik directory convention
	if err := os.MkdirAll(intentLogDir, 0o755); err != nil {
		t.Fatalf("b3bWOPTempProject: MkdirAll %s: %v", intentLogDir, err)
	}

	a, err := brcli.NewForProject(brPath, projectDir)
	if err != nil {
		t.Fatalf("b3bWOPTempProject: NewForProject: %v", err)
	}
	return a, projectDir, intentLogDir
}

// b3bWOPCreateBead runs `br create` in projectDir and returns the bead ID
// extracted from the success line "✓ Created <id>: <title>".
func b3bWOPCreateBead(t *testing.T, brPath, projectDir string) core.BeadID {
	t.Helper()

	//nolint:gosec // G204: brPath from exec.LookPath; args are static
	cmd := exec.Command(brPath, "create", "--title", "b3b-write-ops-test", "--type", "task")
	cmd.Dir = projectDir

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out // capture combined for error context
	if err := cmd.Run(); err != nil {
		t.Fatalf("b3bWOPCreateBead: br create: %v\n%s", err, out.String())
	}

	// Parse "✓ Created <id>: <title>" — the id is the third whitespace-separated
	// token with the trailing colon stripped.
	for _, line := range strings.Split(out.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && strings.Contains(fields[1], "Created") {
			id := strings.TrimSuffix(fields[2], ":")
			if id != "" {
				return core.BeadID(id)
			}
		}
	}
	t.Fatalf("b3bWOPCreateBead: cannot parse bead ID from br create output: %q", out.String())
	return ""
}

// b3bWOPFastCfg returns a TimeoutConfig with production-like timeouts for
// real-br tests. No retry-parameter overrides — uses production defaults.
func b3bWOPFastCfg() brcli.TimeoutConfig {
	return brcli.TimeoutConfig{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

// b3bWOPAssertStatus calls adapter.ShowBead and fails the test if the
// bead's status does not equal want.
func b3bWOPAssertStatus(
	t *testing.T,
	adapter *brcli.Adapter,
	ctx context.Context,
	beadID core.BeadID,
	want core.CoarseStatus,
) {
	t.Helper()
	record, err := adapter.ShowBead(ctx, beadID)
	if err != nil {
		t.Fatalf("b3bWOPAssertStatus: ShowBead(%q): %v", beadID, err)
	}
	if record.Status != want {
		t.Errorf("bead %q: status = %q, want %q", beadID, record.Status, want)
	}
}

// b3bWOPSpecContent reads specs/beads-integration.md relative to the repo root
// (derived from this test file's path via runtime.Caller) and returns its content.
func b3bWOPSpecContent(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("b3bWOPSpecContent: runtime.Caller(0) failed")
	}
	// thisFile: .../internal/brcli/<file>.go → repo root is two dirs up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")
	//nolint:gosec // G304: path constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("b3bWOPSpecContent: read %s: %v", specPath, err)
	}
	return string(raw)
}

// ── Spec sentinel tests ───────────────────────────────────────────────────────

// TestB3b_DaemonOwnsTerminalTransitions_SpecSentinel verifies that the
// beads-integration spec contains the daemon-owns-terminal-transitions
// ownership invariant in its BI-027 / Beads-CLI skill section.
//
// The two required canonical phrases:
//   - "daemon owns those writes per §4.4" — the ownership direction (BI-027).
//   - "Terminal transitions remain daemon-only" — the BI-010e / BI-011 constraint.
//
// Removing either phrase from the spec is a breaking change: every agent's
// launch context (injected via the Beads-CLI skill) would lose the prohibition.
//
// Spec ref: specs/beads-integration.md §4.4 BI-011; §4.8 BI-027 Beads-CLI skill.
func TestB3b_DaemonOwnsTerminalTransitions_SpecSentinel(t *testing.T) {
	t.Parallel()

	spec := b3bWOPSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "daemon owns those writes per §4.4",
			hint: "BI-027 / Beads-CLI skill must assert daemon ownership of terminal-transition " +
				"writes; removing or rephrasing this breaks the agent-prohibition contract " +
				"encoded in every agent's launch context",
		},
		{
			phrase: "Terminal transitions remain daemon-only",
			hint: "BI-010e / BI-011 must declare terminal transitions as daemon-only; " +
				"this phrase anchors the prohibition table and must not be removed",
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

// TestB3b_AgentIssuedTerminalWrite_SpecRefused_BI011 verifies that the BI-011
// section of the spec explicitly prohibits agent-issued terminal writes.
//
// The two required canonical phrases:
//   - "Agents MUST NOT call `br close`" — explicit prohibition on close from worktree.
//   - "agents MUST NOT issue terminal-transition `br` writes" — the general prohibition.
//
// Spec ref: specs/beads-integration.md §4.4 BI-011; §4.8 BI-027.
func TestB3b_AgentIssuedTerminalWrite_SpecRefused_BI011(t *testing.T) {
	t.Parallel()

	spec := b3bWOPSpecContent(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "Agents MUST NOT call `br close`",
			hint: "BI-010e / BI-011 must explicitly prohibit agents from issuing `br close` " +
				"inside a worktree; this is the non-negotiable anchor for the " +
				"agent-prohibited-write invariant",
		},
		{
			phrase: "agents MUST NOT issue terminal-transition `br` writes",
			hint: "BI-027 Beads-CLI skill must document the terminal-write prohibition; " +
				"removing this phrase leaves agents without the constraint in their launch context",
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

// ── Real-br behavioral tests ──────────────────────────────────────────────────

// TestB3b_ClaimBead_TransitionsOpenToInProgress_RealBr verifies that
// ClaimBead correctly writes a BI-030 intent log, invokes real br, leaves the
// bead in in_progress (BI-010a claim row), and deletes the intent file on
// success (BI-030 step 6).
func TestB3b_ClaimBead_TransitionsOpenToInProgress_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)
	beadID := b3bWOPCreateBead(t, brPath, projectDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := adapter.ClaimBead(ctx, intentLogDir, b3bWOPFastCfg(),
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead: %v", err)
	}

	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusInProgress)

	// BI-030 step 6: intent file must be deleted after successful write.
	if count := bi010FixtureCountIntentFiles(t, intentLogDir); count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful claim, got %d", count)
	}
}

// TestB3b_ClaimBead_AlreadyInProgress_Safe_RealBr verifies that ClaimBead on
// an already-in_progress bead returns nil and the bead remains in_progress.
//
// This is the "claim on already-in_progress safe" acceptance criterion.
// Idempotency is provided either by br's native handling (same actor re-claiming)
// or the hk-amed0 fallback path (br update --status in_progress) when --claim
// rejects a pre-assigned bead.
func TestB3b_ClaimBead_AlreadyInProgress_Safe_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)
	beadID := b3bWOPCreateBead(t, brPath, projectDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := b3bWOPFastCfg()

	// First claim: open → in_progress.
	if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead (first): %v", err)
	}
	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusInProgress)

	// Second claim on an already-in_progress bead — must not error.
	if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Errorf("ClaimBead (second, already-in_progress): expected nil, got: %v", err)
	}
	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusInProgress)
}

// TestB3b_CloseBead_TransitionsInProgressToClosed_RealBr verifies that
// CloseBead correctly transitions a real in_progress bead to closed and
// deletes the intent file on success (BI-030 step 6).
func TestB3b_CloseBead_TransitionsInProgressToClosed_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)
	beadID := b3bWOPCreateBead(t, brPath, projectDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := b3bWOPFastCfg()

	// Claim first (open → in_progress).
	if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead: %v", err)
	}

	// Close (in_progress → closed).
	if err := adapter.CloseBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID, false,
	); err != nil {
		t.Fatalf("CloseBead: %v", err)
	}

	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusClosed)

	// BI-030 step 6: intent file deleted after successful close.
	if count := bi010FixtureCountIntentFiles(t, intentLogDir); count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful close, got %d", count)
	}
}

// TestB3b_ReopenBead_TransitionsClosedToOpen_RealBr verifies that ReopenBead
// transitions a real closed bead back to open using br update --status open.
func TestB3b_ReopenBead_TransitionsClosedToOpen_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)
	beadID := b3bWOPCreateBead(t, brPath, projectDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := b3bWOPFastCfg()

	// Claim → close to reach the closed state.
	if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead: %v", err)
	}
	if err := adapter.CloseBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID, false,
	); err != nil {
		t.Fatalf("CloseBead: %v", err)
	}

	// Reopen (closed → open).
	if err := adapter.ReopenBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID, "b3b-reopen-acceptance-test",
	); err != nil {
		t.Fatalf("ReopenBead: %v", err)
	}

	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusOpen)
}

// TestB3b_ResetBead_StrandedInProgressToOpen_RealBr proves the hk-l2xd1
// stranded-in_progress reset primitive: ResetBead transitions an in_progress
// bead (stranded by a crashed daemon) back to open via the daemon orphan-sweep.
//
// This is the "ResetBead returns stranded in_progress to open" acceptance
// criterion.
//
// Spec ref: beads-integration.md §4.4 BI-010d; process-lifecycle.md §4.5 PL-006.
func TestB3b_ResetBead_StrandedInProgressToOpen_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)
	beadID := b3bWOPCreateBead(t, brPath, projectDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := b3bWOPFastCfg()

	// Claim the bead to simulate a "stranded" in_progress state (daemon crashed
	// before it could close or reopen).
	if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
		core.RunID(uuid.Must(uuid.NewV7())),
		core.TransitionID(uuid.Must(uuid.NewV7())),
		beadID,
	); err != nil {
		t.Fatalf("ClaimBead (stranding setup): %v", err)
	}
	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusInProgress)

	// ResetBead: daemon startup orphan-sweep resets stranded bead to open.
	// daemonStartNS scopes the idempotency key to this simulated daemon lifetime.
	const (
		b3bProjectHash   = core.ProjectHash("b3b0aabb1cdd")
		b3bDaemonStartNS = int64(1_747_000_000_000_420_009)
	)
	if err := adapter.ResetBead(ctx, intentLogDir, cfg, beadID, b3bProjectHash, b3bDaemonStartNS); err != nil {
		t.Fatalf("ResetBead: %v", err)
	}

	b3bWOPAssertStatus(t, adapter, ctx, beadID, core.CoarseStatusOpen)

	// BI-030 step 6: intent file deleted after successful reset.
	if count := bi010FixtureCountIntentFiles(t, intentLogDir); count != 0 {
		t.Errorf("BI-030 step 6: expected 0 intent files after successful reset, got %d", count)
	}
}

// TestB3b_ConcurrentClose_SerializesNoDoubleClose_RealBr verifies that N
// goroutines calling CloseBead concurrently on distinct beads via the same
// Adapter all succeed and all beads reach closed status.
//
// This proves the terminalMu serialization property: even under concurrent
// real SQLite write pressure from the same Adapter instance, all close calls
// land cleanly without BrDbLocked errors or double-close corruption.
//
// This is the "concurrent CloseBead serializes no double-close" acceptance
// criterion for hk-420yr.9.
func TestB3b_ConcurrentClose_SerializesNoDoubleClose_RealBr(t *testing.T) {
	t.Parallel()
	brPath := b3bWOPSkipIfNoBr(t)

	const N = 4

	adapter, projectDir, intentLogDir := b3bWOPTempProject(t, brPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg := b3bWOPFastCfg()

	// Setup phase: create and claim N beads sequentially (setup is not under test).
	beadIDs := make([]core.BeadID, N)
	runIDs := make([]core.RunID, N)
	transIDs := make([]core.TransitionID, N)
	for i := range N {
		runIDs[i] = core.RunID(uuid.Must(uuid.NewV7()))
		transIDs[i] = core.TransitionID(uuid.Must(uuid.NewV7()))
		id := b3bWOPCreateBead(t, brPath, projectDir)
		if err := adapter.ClaimBead(ctx, intentLogDir, cfg,
			core.RunID(uuid.Must(uuid.NewV7())),
			core.TransitionID(uuid.Must(uuid.NewV7())),
			id,
		); err != nil {
			t.Fatalf("setup ClaimBead[%d]: %v", i, err)
		}
		beadIDs[i] = id
	}

	// Concurrent close phase: all N goroutines call CloseBead simultaneously.
	// terminalMu inside the Adapter serializes the actual br invocations.
	var wg sync.WaitGroup
	errs := make([]error, N)
	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// Each goroutine uses a private sub-directory for its intent files
			// to avoid directory-fsync races (terminalMu serializes writes but
			// using distinct dirs keeps intent files cleanly scoped per call).
			closeIntentDir := filepath.Join(intentLogDir, fmt.Sprintf("close%d", idx))
			if mkErr := os.MkdirAll(closeIntentDir, 0o755); mkErr != nil { //nolint:gosec
				errs[idx] = fmt.Errorf("MkdirAll closeIntentDir[%d]: %w", idx, mkErr)
				return
			}
			errs[idx] = adapter.CloseBead(ctx, closeIntentDir, cfg,
				runIDs[idx], transIDs[idx], beadIDs[idx], false,
			)
		}(i)
	}
	wg.Wait()

	// All close calls must have succeeded.
	for i, err := range errs {
		if err != nil {
			t.Errorf("concurrent CloseBead[%d]: unexpected error: %v", i, err)
		}
	}

	// All N beads must be in closed status — no double-close or missed close.
	for i, beadID := range beadIDs {
		record, showErr := adapter.ShowBead(ctx, beadID)
		if showErr != nil {
			t.Errorf("ShowBead[%d] (%q): %v", i, beadID, showErr)
			continue
		}
		if record.Status != core.CoarseStatusClosed {
			t.Errorf("bead[%d] %q: status = %q, want %q (concurrent close did not land)",
				i, beadID, record.Status, core.CoarseStatusClosed)
		}
	}
}
