package queue_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// errorsAllReasons is the exhaustive list of all QueueValidationReason values
// defined in specs/queue-model.md §6.10 QM-029 and §6.11a QM-029b.
var errorsAllReasons = []queue.QueueValidationReason{
	queue.ReasonQueueAlreadyActive,
	queue.ReasonAppendTargetInvalid,
	queue.ReasonQueueNotAdvancing,
	queue.ReasonBeadNotFound,
	queue.ReasonBeadNotOpen,
	queue.ReasonBeadAlreadyDispatched,
	queue.ReasonDuplicateBeadID,
	queue.ReasonQueueTooLarge,
}

// errorsExpectedCodes is the normative code-to-reason table per QM-029b.
// Keys and values MUST match specs/queue-model.md §6.11a exactly.
var errorsExpectedCodes = map[queue.QueueValidationReason]int{
	queue.ReasonQueueAlreadyActive:    queue.ErrorCodeQueueAlreadyActive,
	queue.ReasonAppendTargetInvalid:   queue.ErrorCodeAppendTargetInvalid,
	queue.ReasonQueueNotAdvancing:     queue.ErrorCodeQueueNotAdvancing,
	queue.ReasonBeadNotFound:          queue.ErrorCodeBeadNotFound,
	queue.ReasonBeadNotOpen:           queue.ErrorCodeBeadNotOpen,
	queue.ReasonBeadAlreadyDispatched: queue.ErrorCodeBeadAlreadyDispatched,
	queue.ReasonDuplicateBeadID:       queue.ErrorCodeDuplicateBeadID,
	queue.ReasonQueueTooLarge:         queue.ErrorCodeQueueTooLarge,
}

// errorsExpectedMessages is the normative message-to-reason table per QM-029b.
// Message strings mirror the wire-level reason strings per QM-029.
var errorsExpectedMessages = map[queue.QueueValidationReason]string{
	queue.ReasonQueueAlreadyActive:    "queue_already_active",
	queue.ReasonAppendTargetInvalid:   "append_target_invalid",
	queue.ReasonQueueNotAdvancing:     "queue_not_advancing",
	queue.ReasonBeadNotFound:          "bead_not_found",
	queue.ReasonBeadNotOpen:           "bead_not_open",
	queue.ReasonBeadAlreadyDispatched: "bead_already_dispatched",
	queue.ReasonDuplicateBeadID:       "duplicate_bead_id",
	queue.ReasonQueueTooLarge:         "queue_too_large",
}

// TestErrorCodeConstantsNormativeMapping verifies that each constant value
// matches the normative QM-029b table exactly.
//
// Spec ref: queue-model.md §6.11a QM-029b.
func TestErrorCodeConstantsNormativeMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		got      int
		wantCode int
	}{
		{"ErrorCodeQueueAlreadyActive", queue.ErrorCodeQueueAlreadyActive, -32010},
		{"ErrorCodeAppendTargetInvalid", queue.ErrorCodeAppendTargetInvalid, -32011},
		{"ErrorCodeQueueNotAdvancing", queue.ErrorCodeQueueNotAdvancing, -32012},
		{"ErrorCodeBeadNotFound", queue.ErrorCodeBeadNotFound, -32013},
		{"ErrorCodeBeadNotOpen", queue.ErrorCodeBeadNotOpen, -32014},
		{"ErrorCodeBeadAlreadyDispatched", queue.ErrorCodeBeadAlreadyDispatched, -32015},
		{"ErrorCodeDuplicateBeadID", queue.ErrorCodeDuplicateBeadID, -32016},
		{"ErrorCodeQueueTooLarge", queue.ErrorCodeQueueTooLarge, -32017},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.got != tc.wantCode {
				t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.wantCode)
			}
		})
	}
}

// TestJSONRPCErrorExhaustive asserts that JSONRPCError maps every
// QueueValidationReason to a non-default code and the correct message.
//
// Spec ref: queue-model.md §6.11a QM-029b.
func TestJSONRPCErrorExhaustive(t *testing.T) {
	t.Parallel()

	for _, reason := range errorsAllReasons {
		reason := reason
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			code, message := queue.JSONRPCError(reason)

			wantCode, ok := errorsExpectedCodes[reason]
			if !ok {
				t.Fatalf("reason %q not in expected-codes table — update errorsExpectedCodes", reason)
			}
			if code != wantCode {
				t.Errorf("JSONRPCError(%q) code = %d, want %d", reason, code, wantCode)
			}

			wantMsg, ok := errorsExpectedMessages[reason]
			if !ok {
				t.Fatalf("reason %q not in expected-messages table — update errorsExpectedMessages", reason)
			}
			if message != wantMsg {
				t.Errorf("JSONRPCError(%q) message = %q, want %q", reason, message, wantMsg)
			}
		})
	}
}

// TestJSONRPCErrorStableRange verifies that all allocated codes fall within
// the -32010..-32017 range reserved for queue-model per PL-003a and that
// -32018 and -32019 are not used.
//
// Spec ref: queue-model.md §6.11a QM-029b; process-lifecycle.md §4.4 PL-003a.
func TestJSONRPCErrorStableRange(t *testing.T) {
	t.Parallel()

	reserved := map[int]bool{-32018: true, -32019: true}

	for _, reason := range errorsAllReasons {
		reason := reason
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			code, _ := queue.JSONRPCError(reason)

			if code < -32017 || code > -32010 {
				t.Errorf("JSONRPCError(%q) code %d is outside the reserved range [-32017, -32010]", reason, code)
			}
			if reserved[code] {
				t.Errorf("JSONRPCError(%q) code %d collides with a reserved slot", reason, code)
			}
		})
	}
}

// TestJSONRPCErrorCodesUnique verifies that no two QueueValidationReason
// values map to the same JSON-RPC error code (1:1 mapping per QM-029b).
//
// Spec ref: queue-model.md §6.11a QM-029b.
func TestJSONRPCErrorCodesUnique(t *testing.T) {
	t.Parallel()

	seen := make(map[int]queue.QueueValidationReason)
	for _, reason := range errorsAllReasons {
		code, _ := queue.JSONRPCError(reason)
		if prev, dup := seen[code]; dup {
			t.Errorf("code %d is shared by %q and %q — codes must be 1:1 per QM-029b", code, prev, reason)
		}
		seen[code] = reason
	}
}
