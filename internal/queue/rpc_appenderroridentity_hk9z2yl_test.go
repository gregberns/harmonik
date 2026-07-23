package queue_test

// rpc_appenderroridentity_hk9z2yl_test.go — the errors.As contract in
// HandleQueueAppendOnQueue (de9aac48, follow-up bead hk-9z2yl).
//
// de9aac48 replaced this pair:
//
//	if IsValidationError(appendErr) {          // unwraps via errors.As
//	    ve := appendErr.(*ValidationError)     // does NOT unwrap
//
// with a single errors.As. Establishing that contract was the point of the
// commit, and nothing would have caught its removal: every pre-existing append
// test drives AppendItems down a path that returns a BARE *ValidationError, and
// a bare one is extracted correctly by all three spellings (the old guard, the
// old assertion, and errors.As).
//
// The distinguishing input is a WRAPPED *ValidationError, which reaches
// HandleQueueAppendOnQueue when the BeadLedger seam returns one: Validate wraps
// every ledger error with %w ("QM-020 ledger lookup %q: %w") and AppendItems
// wraps that again ("queue: AppendItems: validation: %w"). Against that input
// the three spellings diverge:
//
//	errors.As                      → mapped validation code   (current, asserted here)
//	bare appendErr.(*ValidationError) with a comma-ok → falls through to -32099
//	IsValidationError + bare assertion → PANICS
//
// So deleting the errors.As handling fails this test either way.
//
// On the commit message's claim that the OLD code "PANICS on the assertion":
// that panic was LATENT, not shipped. AppendItems only ever returns a
// *ValidationError bare, so no production caller could reach the wrapped input
// — see TestHandleQueueAppendOnQueue_BareValidationErrorStillMaps for the
// reachable shape. The errors.As fix is still strictly better, and this test
// pins the contract it establishes rather than the panic that was claimed.
//
// Bead ref: hk-9z2yl.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// appendIdentityLedger is a BeadLedger whose LookupStatus returns a caller-
// supplied error. It is the only seam through which a non-bare error can reach
// HandleQueueAppendOnQueue's error-identity branch.
type appendIdentityLedger struct{ lookupErr error }

func (l *appendIdentityLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	if l.lookupErr != nil {
		return "", l.lookupErr
	}
	return queue.BeadStatusOpen, nil
}

func (l *appendIdentityLedger) BlocksEdge(_ context.Context, _, _ core.BeadID) (bool, error) {
	return false, nil
}

// appendIdentityQueue returns an active queue with one active stream group,
// ready to be appended to.
func appendIdentityQueue() *queue.Queue {
	return &queue.Queue{
		SchemaVersion: 1,
		QueueID:       "0192a7c0-0000-7000-8000-00000000ae01",
		Name:          "main",
		Status:        queue.QueueStatusActive,
		Groups: []queue.Group{
			{
				GroupIndex: 0,
				Kind:       queue.GroupKindStream,
				Status:     queue.GroupStatusActive,
				Items: []queue.Item{
					{BeadID: "hk-exist01", Status: queue.ItemStatusCompleted},
				},
			},
		},
	}
}

// appendIdentityProjectDir returns a project dir with an empty .harmonik/queues,
// so loadOtherQueues finds nothing and the append reaches AppendItems.
func appendIdentityProjectDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "queues"), 0o700); err != nil {
		t.Fatalf("appendIdentityProjectDir: MkdirAll: %v", err)
	}
	return dir
}

// TestHandleQueueAppendOnQueue_WrappedValidationErrorIsUnwrapped is the
// regression test for the errors.As contract itself: a *ValidationError buried
// two %w layers deep must still be mapped to ITS JSON-RPC code and detail, not
// flattened into the generic -32099 internal_error.
func TestHandleQueueAppendOnQueue_WrappedValidationErrorIsUnwrapped(t *testing.T) {
	t.Parallel()

	buried := fmt.Errorf("beads adapter: %w", &queue.ValidationError{
		Reason: queue.ReasonBeadNotOpen,
		Detail: map[string]any{"bead_id": "hk-wrapped1", "actual_status": "closed"},
	})

	_, _, _, rpcErr := queue.HandleQueueAppendOnQueue(
		context.Background(),
		queue.QueueAppendRequest{GroupIndex: 0, BeadIDs: []core.BeadID{"hk-wrapped1"}},
		&appendIdentityLedger{lookupErr: buried},
		appendIdentityProjectDir(t),
		appendIdentityQueue(),
	)

	if rpcErr == nil {
		t.Fatal("append with a failing ledger returned no RPCError")
	}
	if rpcErr.Code != queue.ErrorCodeBeadNotOpen {
		t.Fatalf("wrapped *ValidationError mapped to code %d (%q), want %d (bead_not_open): "+
			"the error-identity check no longer unwraps",
			rpcErr.Code, rpcErr.Message, queue.ErrorCodeBeadNotOpen)
	}
	if rpcErr.Message != "bead_not_open" {
		t.Errorf("message = %q, want %q", rpcErr.Message, "bead_not_open")
	}
	// The detail must come from the buried ValidationError, not from a
	// stringified wrapper — that is what the caller reads off the wire.
	if got := rpcErr.Detail["bead_id"]; got != "hk-wrapped1" {
		t.Errorf("detail[bead_id] = %v, want %q", got, "hk-wrapped1")
	}
	if got := rpcErr.Detail["actual_status"]; got != "closed" {
		t.Errorf("detail[actual_status] = %v, want %q", got, "closed")
	}
}

// TestHandleQueueAppendOnQueue_BareValidationErrorStillMaps holds the other
// half of the contract: the reachable, bare shape (AppendItems returns
// &verrs[0] and &ValidationError{…} directly) must keep mapping to its own
// code. This is the case that was ALREADY correct before de9aac48; it is
// asserted here so a future "simplification" of the errors.As back to a bare
// assertion cannot be justified by "the wrapped test is artificial".
func TestHandleQueueAppendOnQueue_BareValidationErrorStillMaps(t *testing.T) {
	t.Parallel()

	// GroupIndex 7 is out of range, so AppendItems returns a bare
	// *ValidationError with ReasonAppendTargetInvalid before touching the ledger.
	_, _, _, rpcErr := queue.HandleQueueAppendOnQueue(
		context.Background(),
		queue.QueueAppendRequest{GroupIndex: 7, BeadIDs: []core.BeadID{"hk-bare0001"}},
		&appendIdentityLedger{},
		appendIdentityProjectDir(t),
		appendIdentityQueue(),
	)

	if rpcErr == nil {
		t.Fatal("append to an out-of-range group returned no RPCError")
	}
	if rpcErr.Code != queue.ErrorCodeAppendTargetInvalid {
		t.Fatalf("bare *ValidationError mapped to code %d (%q), want %d (append_target_invalid)",
			rpcErr.Code, rpcErr.Message, queue.ErrorCodeAppendTargetInvalid)
	}
}

// TestHandleQueueAppendOnQueue_NonValidationErrorStaysInternal is the negative
// control: a ledger failure that is NOT a ValidationError must still surface as
// -32099 internal_error. Without it, "map everything to a validation code"
// would pass the two tests above.
func TestHandleQueueAppendOnQueue_NonValidationErrorStaysInternal(t *testing.T) {
	t.Parallel()

	_, _, _, rpcErr := queue.HandleQueueAppendOnQueue(
		context.Background(),
		queue.QueueAppendRequest{GroupIndex: 0, BeadIDs: []core.BeadID{"hk-plainerr"}},
		&appendIdentityLedger{lookupErr: fmt.Errorf("beads adapter: %w", os.ErrDeadlineExceeded)},
		appendIdentityProjectDir(t),
		appendIdentityQueue(),
	)

	if rpcErr == nil {
		t.Fatal("append with a failing ledger returned no RPCError")
	}
	if rpcErr.Code != -32099 {
		t.Fatalf("plain ledger error mapped to code %d (%q), want -32099 (internal_error)",
			rpcErr.Code, rpcErr.Message)
	}
}
