package scenario

// single_active_per_name_test.go — QM-027 single-active rejection +
// per-name routing through the real HandleQueueSubmit / HandleQueueDryRun path.
//
// Bead refs: hk-stqn0, hk-40r9b, hk-1k5as
// Spec refs:
//   - specs/queue-model.md §6.3 QM-027 (single active queue per name, submit-only)
//   - specs/queue-model.md §2.1 QM-002 (empty name → "main")
//   - specs/named-queues.md NQ-A1 (per-name single-active guard)
//
// Test matrix (assertions a–d from the bead):
//
//	(a) submit to queue "a"                    → accepted (queue_id returned)
//	(b) second submit to "a" while "a" active  → -32010 queue_already_active
//	(c) submit to "b" while "a" active         → accepted; "b" appears in list as "b"
//	(d) bare submit (empty name)               → accepted; normalises to "main"
//
// Optionally: (b) and (c) through HandleQueueDryRun too (regression guard for
// the hk-40r9b name-forwarding bug: CLI dropped the queue name → all submits
// collapsed to "main" → per-name guard fired on "main" for wrong target).
//
// These tests call HandleQueueSubmit / HandleQueueDryRun directly — no live
// daemon, no event bus, no br DB. The only I/O is t.TempDir() for .harmonik/.
//
// Helper prefix: singleActiveName (implementer-protocol.md §Helper-prefix).
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// singleActiveNameProjectDir creates a temporary project root with .harmonik/.
func singleActiveNameProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	//nolint:gosec // G301: 0755 matches .harmonik dir conventions
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".harmonik"), 0o755))
	return dir
}

// singleActiveNameLedger is a minimal BeadLedger that reports every bead as
// open and records no blocking edges. Sufficient for QM-020/QM-021/QM-025
// checks that exercise the happy path.
type singleActiveNameLedger struct{}

func (singleActiveNameLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}
func (singleActiveNameLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// singleActiveNameStreamGroup returns a one-item stream Group at groupIndex 0
// containing beadID.
func singleActiveNameStreamGroup(beadID core.BeadID) queue.Group {
	return queue.Group{
		GroupIndex: 0,
		Kind:       queue.GroupKindStream,
		Status:     queue.GroupStatusPending,
		Items: []queue.Item{
			{BeadID: beadID, Status: queue.ItemStatusPending},
		},
	}
}

// singleActiveNameSubmitReq builds a QueueSubmitRequest for the named queue
// with a single stream group containing beadID.
func singleActiveNameSubmitReq(name string, beadID core.BeadID) queue.QueueSubmitRequest {
	return queue.QueueSubmitRequest{
		SchemaVersion: 1,
		Name:          name,
		Groups:        []queue.Group{singleActiveNameStreamGroup(beadID)},
	}
}

// singleActiveNameDryRunReq builds a QueueDryRunRequest for the named queue
// with a single stream group containing beadID.
func singleActiveNameDryRunReq(name string, beadID core.BeadID) queue.QueueDryRunRequest {
	return queue.QueueDryRunRequest{
		SchemaVersion: 1,
		Name:          name,
		Groups:        []queue.Group{singleActiveNameStreamGroup(beadID)},
	}
}

// ---------------------------------------------------------------------------
// (a) submit to queue "a" → accepted
// ---------------------------------------------------------------------------

// TestScenario_SingleActivePerName_SubmitToNamedQueueAccepted verifies that
// the first HandleQueueSubmit for a named queue "a" succeeds and returns a
// non-empty queue_id with status=active.
//
// Spec ref: specs/queue-model.md §8.1 QM-050; §2.1 QM-002.
func TestScenario_SingleActivePerName_SubmitToNamedQueueAccepted(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	req := singleActiveNameSubmitReq("a", "hk-qm027-a1")
	resp, _, _, rpcErr := queue.HandleQueueSubmit(ctx, req, ledger, projectDir, 1)

	require.Nil(t, rpcErr, "first submit to queue 'a' must be accepted; got RPC error: %v", rpcErr)
	require.NotEmpty(t, resp.QueueID, "queue_id must be non-empty on accept")
	require.Equal(t, queue.QueueStatusActive, resp.Status, "status must be active on first submit")
	require.Equal(t, 1, resp.GroupCount, "group_count must be 1")
}

// ---------------------------------------------------------------------------
// (b) second submit to "a" while "a" active → -32010 queue_already_active
// ---------------------------------------------------------------------------

// TestScenario_SingleActivePerName_SecondSubmitSameNameRejected verifies that
// a second HandleQueueSubmit targeting queue "a" while "a" is active returns
// RPC error -32010 (queue_already_active), fulfilling QM-027 per-name
// single-active enforcement (NQ-A1).
//
// This is the regression test for hk-40r9b: the CLI was dropping the queue
// name so all submits collapsed to "main", which meant the guard would fire
// on "main" for an unrelated target — or fail to fire at all on the real target.
//
// Spec ref: specs/queue-model.md §6.3 QM-027; named-queues.md NQ-A1.
func TestScenario_SingleActivePerName_SecondSubmitSameNameRejected(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// First submit — must succeed to establish the "a"-active state.
	req1 := singleActiveNameSubmitReq("a", "hk-qm027-a2")
	_, _, _, rpcErr1 := queue.HandleQueueSubmit(ctx, req1, ledger, projectDir, 1)
	require.Nil(t, rpcErr1, "first submit to queue 'a' must succeed to set up the test; got: %v", rpcErr1)

	// Second submit to the same name — must be rejected.
	req2 := singleActiveNameSubmitReq("a", "hk-qm027-a3")
	_, _, _, rpcErr2 := queue.HandleQueueSubmit(ctx, req2, ledger, projectDir, 1)

	require.NotNil(t, rpcErr2, "second submit to active queue 'a' must return an RPC error (QM-027)")
	require.Equal(t, queue.ErrorCodeQueueAlreadyActive, rpcErr2.Code,
		"expected -32010 queue_already_active; got code=%d msg=%s", rpcErr2.Code, rpcErr2.Message)
	require.Equal(t, "queue_already_active", rpcErr2.Message,
		"expected message 'queue_already_active'")
}

// TestScenario_SingleActivePerName_DryRunSecondSubmitSameNameRejected verifies
// that HandleQueueDryRun for queue "a" while "a" is active also returns -32010.
// This exercises the dry-run name-forwarding path to guard the hk-40r9b
// regression directly: if the CLI drops the name, dry-run would evaluate the
// wrong per-name slot.
//
// Spec ref: specs/queue-model.md §6 QM-028, §6.3 QM-027.
func TestScenario_SingleActivePerName_DryRunSecondSubmitSameNameRejected(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// Submit to "a" to make it active on disk.
	req := singleActiveNameSubmitReq("a", "hk-qm027-a4")
	_, _, _, rpcErr := queue.HandleQueueSubmit(ctx, req, ledger, projectDir, 1)
	require.Nil(t, rpcErr, "setup submit to 'a' must succeed; got: %v", rpcErr)

	// Dry-run a second submit to "a" — must be rejected with the same error code.
	dryReq := singleActiveNameDryRunReq("a", "hk-qm027-a5")
	_, dryRPCErr := queue.HandleQueueDryRun(ctx, dryReq, ledger, projectDir)

	require.NotNil(t, dryRPCErr, "dry-run against active queue 'a' must return an RPC error (QM-027)")
	require.Equal(t, queue.ErrorCodeQueueAlreadyActive, dryRPCErr.Code,
		"dry-run: expected -32010 queue_already_active; got code=%d msg=%s", dryRPCErr.Code, dryRPCErr.Message)
}

// ---------------------------------------------------------------------------
// (c) submit to "b" while "a" active → accepted; "b" appears in list as "b"
// ---------------------------------------------------------------------------

// TestScenario_SingleActivePerName_DifferentNameAcceptedWhileAActive verifies
// that submitting to queue "b" while queue "a" is already active is accepted.
// QM-027 is per-name: the "b"-slot is empty so no single-active violation.
//
// Spec ref: specs/queue-model.md §6.3 QM-027 (per-name guard); NQ-A1.
func TestScenario_SingleActivePerName_DifferentNameAcceptedWhileAActive(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// Submit "a" first.
	reqA := singleActiveNameSubmitReq("a", "hk-qm027-b1")
	_, _, _, rpcErrA := queue.HandleQueueSubmit(ctx, reqA, ledger, projectDir, 1)
	require.Nil(t, rpcErrA, "submit to 'a' must succeed; got: %v", rpcErrA)

	// Submit "b" — must be accepted despite "a" being active.
	reqB := singleActiveNameSubmitReq("b", "hk-qm027-b2")
	respB, _, _, rpcErrB := queue.HandleQueueSubmit(ctx, reqB, ledger, projectDir, 1)

	require.Nil(t, rpcErrB, "submit to 'b' while 'a' is active must be accepted (per-name guard, NQ-A1); got: %v", rpcErrB)
	require.NotEmpty(t, respB.QueueID, "queue_id for 'b' must be non-empty")
	require.Equal(t, queue.QueueStatusActive, respB.Status, "'b' status must be active")
}

// TestScenario_SingleActivePerName_QueueBAppearsInListAsB verifies that after
// submitting to both "a" and "b", HandleQueueList returns a summary for "b"
// with Name="b" (NOT "main"). This is the per-name routing assertion: before
// hk-40r9b was fixed, the CLI dropped the name so both submits landed in "main"
// and "b" never appeared as its own slot.
//
// Spec ref: specs/queue-model.md §8.9 QM-058; NQ-A1.
func TestScenario_SingleActivePerName_QueueBAppearsInListAsB(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// Submit "a".
	reqA := singleActiveNameSubmitReq("a", "hk-qm027-c1")
	_, _, _, rpcErrA := queue.HandleQueueSubmit(ctx, reqA, ledger, projectDir, 1)
	require.Nil(t, rpcErrA, "submit to 'a' must succeed; got: %v", rpcErrA)

	// Submit "b".
	reqB := singleActiveNameSubmitReq("b", "hk-qm027-c2")
	_, _, _, rpcErrB := queue.HandleQueueSubmit(ctx, reqB, ledger, projectDir, 1)
	require.Nil(t, rpcErrB, "submit to 'b' must succeed; got: %v", rpcErrB)

	// queue list must contain both "a" and "b".
	listResp, listRPCErr := queue.HandleQueueList(ctx, projectDir)
	require.Nil(t, listRPCErr, "HandleQueueList must succeed; got: %v", listRPCErr)
	require.Len(t, listResp.Queues, 2, "expected 2 queues (a + b); got names: %v", queueSummaryNames(listResp.Queues))

	byName := make(map[string]queue.QueueSummary, 2)
	for _, s := range listResp.Queues {
		byName[s.Name] = s
	}

	aSum, hasA := byName["a"]
	require.True(t, hasA, "queue list must contain 'a'; got names: %v", queueSummaryNames(listResp.Queues))
	require.Equal(t, queue.QueueStatusActive, aSum.Status, "'a' status must be active in list")

	bSum, hasB := byName["b"]
	require.True(t, hasB,
		"queue list must contain 'b' (NOT 'main') — regression guard for hk-40r9b name-forwarding bug; got names: %v",
		queueSummaryNames(listResp.Queues))
	require.Equal(t, "b", bSum.Name, "queue 'b' must be listed under name 'b', not 'main'")
	require.Equal(t, queue.QueueStatusActive, bSum.Status, "'b' status must be active in list")
}

// TestScenario_SingleActivePerName_DryRunDifferentNameNotRejected verifies
// that HandleQueueDryRun for queue "b" while "a" is active returns no error.
// Proves the dry-run path correctly scopes the per-name guard to the requested
// name (regression guard for hk-40r9b in the dry-run code path).
//
// Spec ref: specs/queue-model.md §6 QM-028, §6.3 QM-027 (per-name guard).
func TestScenario_SingleActivePerName_DryRunDifferentNameNotRejected(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// Submit "a" to disk.
	reqA := singleActiveNameSubmitReq("a", "hk-qm027-d1")
	_, _, _, rpcErrA := queue.HandleQueueSubmit(ctx, reqA, ledger, projectDir, 1)
	require.Nil(t, rpcErrA, "submit to 'a' must succeed; got: %v", rpcErrA)

	// Dry-run for "b" while "a" is active — must pass.
	dryReq := singleActiveNameDryRunReq("b", "hk-qm027-d2")
	_, dryRPCErr := queue.HandleQueueDryRun(ctx, dryReq, ledger, projectDir)

	require.Nil(t, dryRPCErr,
		"dry-run for 'b' while 'a' is active must be accepted (per-name guard scoped to requested name); got: %v",
		dryRPCErr)
}

// ---------------------------------------------------------------------------
// (d) bare submit (empty name) normalises to "main"
// ---------------------------------------------------------------------------

// TestScenario_SingleActivePerName_BareSubmitNormalisesToMain verifies that
// a HandleQueueSubmit with an empty Name field succeeds and the resulting queue
// is persisted and listed as "main" (QM-002 / NormaliseQueueName).
//
// Spec ref: specs/queue-model.md §2.1 QM-002 (empty name → "main").
func TestScenario_SingleActivePerName_BareSubmitNormalisesToMain(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// Submit with empty Name — the bare-submit path.
	req := singleActiveNameSubmitReq("", "hk-qm027-e1")
	resp, _, _, rpcErr := queue.HandleQueueSubmit(ctx, req, ledger, projectDir, 1)

	require.Nil(t, rpcErr, "bare submit (empty name) must be accepted; got: %v", rpcErr)
	require.NotEmpty(t, resp.QueueID, "queue_id must be non-empty for bare submit")
	require.Equal(t, queue.QueueStatusActive, resp.Status)

	// The queue must be reachable by name "main" and listed as "main".
	listResp, listRPCErr := queue.HandleQueueList(ctx, projectDir)
	require.Nil(t, listRPCErr, "HandleQueueList must succeed; got: %v", listRPCErr)
	require.Len(t, listResp.Queues, 1, "bare submit must produce exactly one queue; got: %v", queueSummaryNames(listResp.Queues))
	require.Equal(t, "main", listResp.Queues[0].Name,
		"bare submit must route to 'main', not %q", listResp.Queues[0].Name)
}

// TestScenario_SingleActivePerName_BareSubmitSingleActiveGuardFiresOnMain verifies
// that after a bare submit establishes an active "main" queue, a second bare
// submit is rejected with -32010 queue_already_active. This confirms that the
// per-name guard correctly fires on the "main" slot, not a different per-name
// slot, when the name is omitted.
//
// Spec ref: specs/queue-model.md §6.3 QM-027; §2.1 QM-002.
func TestScenario_SingleActivePerName_BareSubmitSingleActiveGuardFiresOnMain(t *testing.T) {
	t.Parallel()

	projectDir := singleActiveNameProjectDir(t)
	ctx := context.Background()
	ledger := singleActiveNameLedger{}

	// First bare submit — must succeed.
	req1 := singleActiveNameSubmitReq("", "hk-qm027-f1")
	_, _, _, rpcErr1 := queue.HandleQueueSubmit(ctx, req1, ledger, projectDir, 1)
	require.Nil(t, rpcErr1, "first bare submit must succeed; got: %v", rpcErr1)

	// Second bare submit — must be rejected (single-active on "main").
	req2 := singleActiveNameSubmitReq("", "hk-qm027-f2")
	_, _, _, rpcErr2 := queue.HandleQueueSubmit(ctx, req2, ledger, projectDir, 1)

	require.NotNil(t, rpcErr2, "second bare submit while 'main' is active must be rejected (QM-027)")
	require.Equal(t, queue.ErrorCodeQueueAlreadyActive, rpcErr2.Code,
		"expected -32010 queue_already_active; got code=%d msg=%s", rpcErr2.Code, rpcErr2.Message)
}
