package brcli_test

// reissueintent_bi031_test.go — integration tests for Adapter.ReissueTerminalTransition.
//
// These tests drive the full method (step 5 br invocation + step 6 intent-file
// delete) using mock br binaries, complementing the lower-level BrError
// classification tests in idempstep4_bi031_test.go.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031 step 4 (4a–4f).
// Bead ref: hk-aev8t (G3 — step-4 re-drive).

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// reissueFixtureEntry returns a valid claim IntentLogEntry for
// ReissueTerminalTransition tests. The key matches the format written by
// idempStep4FixtureIntentFile so the two suites share fixture semantics.
func reissueFixtureEntry(t *testing.T, beadID core.BeadID) core.IntentLogEntry {
	t.Helper()
	return core.IntentLogEntry{
		IdempotencyKey:    "reissue:run-abc:trans-def:claim",
		RunID:             core.RunID(uuid.Must(uuid.NewV7())),
		TransitionID:      core.TransitionID(uuid.Must(uuid.NewV7())),
		Op:                core.TerminalOpClaim,
		BeadID:            beadID,
		IntendedPostState: core.CoarseStatusInProgress,
		RequestedAt:       time.Now().UTC().Truncate(time.Second),
		SchemaVersion:     1,
	}
}

// reissueFixtureIntentFile writes entry as an intent-log JSON file to
// intentLogDir/<encoded_key>.json, where encoded_key is IdempotencyKey with
// colons replaced by underscores (same encoding used by DeleteIntentLogAndSyncParent).
func reissueFixtureIntentFile(t *testing.T, intentLogDir string, entry core.IntentLogEntry) string {
	t.Helper()
	if err := os.MkdirAll(intentLogDir, 0o755); err != nil { //nolint:gosec
		t.Fatalf("reissueFixtureIntentFile: MkdirAll: %v", err)
	}
	jsonStr := `{"idempotency_key":"` + entry.IdempotencyKey +
		`","run_id":"` + entry.RunID.String() +
		`","transition_id":"` + entry.TransitionID.String() +
		`","op":"` + string(entry.Op) +
		`","bead_id":"` + string(entry.BeadID) +
		`","intended_post_state":"` + string(entry.IntendedPostState) +
		`","requested_at":"` + entry.RequestedAt.Format(time.RFC3339) +
		`","schema_version":1}`
	encodedKey := strings.ReplaceAll(entry.IdempotencyKey, ":", "_")
	path := intentLogDir + "/" + encodedKey + ".json"
	if err := os.WriteFile(path, []byte(jsonStr), 0o600); err != nil {
		t.Fatalf("reissueFixtureIntentFile: WriteFile: %v", err)
	}
	return path
}

// TestReissueTerminalTransition_4a_BrOK verifies that when `br` exits 0 (BrOK),
// ReissueTerminalTransition returns nil and deletes the intent file (BI-031
// step 6 — success path).
func TestReissueTerminalTransition_4a_BrOK(t *testing.T) {
	t.Parallel()

	intentLogDir := t.TempDir()
	entry := reissueFixtureEntry(t, "hk-reissue-4a")
	intentPath := reissueFixtureIntentFile(t, intentLogDir, entry)

	// br exits 0 — successful transition.
	brPath := brcliFixtureMockBinary(t, `{}`, "", 0)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	reissueErr := adapter.ReissueTerminalTransition(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{WriteTimeout: 2 * time.Second},
		entry,
	)
	if reissueErr != nil {
		t.Fatalf("4a BrOK: expected nil error, got: %v", reissueErr)
	}

	// Step 6: intent file must be deleted on success.
	if _, statErr := os.Stat(intentPath); !os.IsNotExist(statErr) {
		t.Errorf("4a BrOK: intent file still present at %q — step 6 delete did not happen", intentPath)
	}
}

// TestReissueTerminalTransition_4d_BrUnavailable verifies that when `br` is
// unavailable (persistent BrDbLocked exhausting the retry budget),
// ReissueTerminalTransition returns a BrUnavailable-wrapping error and the
// intent file is retained for Cat 3a routing.
//
// We trigger BrUnavailable via DbLocked retry exhaustion (exit 3) rather than
// a wall-clock timeout to avoid pipe-inheritance hangs in test (shell child
// processes keep stdout open past SIGKILL on macOS). The post-condition —
// BrUnavailable error, intent retained — is identical to the timeout path.
func TestReissueTerminalTransition_4d_BrUnavailable(t *testing.T) {
	t.Parallel()

	intentLogDir := t.TempDir()
	entry := reissueFixtureEntry(t, "hk-reissue-4d")
	intentPath := reissueFixtureIntentFile(t, intentLogDir, entry)

	// br always exits 3 (BrDbLocked). After TerminalWriteMaxRetries=1 exhaustion
	// RunWithDBLockedRetry escalates to BrUnavailable.
	brPath := brcliFixtureMockBinary(t, ``, "", 3)
	adapter, err := brcli.New(brPath)
	if err != nil {
		t.Fatalf("brcli.New: %v", err)
	}

	reissueErr := adapter.ReissueTerminalTransition(
		context.Background(),
		intentLogDir,
		brcli.TimeoutConfig{
			WriteTimeout:           2 * time.Second,
			TerminalWriteMaxRetries: 1,
			TerminalWriteRetryBase:  1 * time.Millisecond,
			TerminalWriteRetryCap:   5 * time.Millisecond,
		},
		entry,
	)
	if reissueErr == nil {
		t.Fatal("4d BrUnavailable: expected error, got nil")
	}
	if !errors.Is(reissueErr, brcli.BrUnavailable) {
		t.Errorf("4d BrUnavailable: errors.Is(err, BrUnavailable) = false; got %v", reissueErr)
	}

	// Intent file must be retained for Cat 3a.
	if _, statErr := os.Stat(intentPath); os.IsNotExist(statErr) {
		t.Errorf("4d BrUnavailable: intent file was unexpectedly deleted — must be retained for Cat 3a")
	}
}
