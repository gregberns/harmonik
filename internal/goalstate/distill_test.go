package goalstate

import (
	"strings"
	"testing"
)

// TestDistill_NoMessages_NoChange verifies that Distill with an empty messages
// slice leaves the GoalState unchanged (including LastEventID and OperatorDirectives).
func TestDistill_NoMessages_NoChange(t *testing.T) {
	t.Parallel()
	gs := &GoalState{
		OperatorDirectives: []string{"prior directive"},
		LastEventID:        "old-event-id",
	}
	result := Distill(gs, nil, "new-event-id")

	if len(result.OperatorDirectives) != 1 || result.OperatorDirectives[0] != "prior directive" {
		t.Errorf("NoMessages: OperatorDirectives changed; got %v", result.OperatorDirectives)
	}
	if result.LastEventID != "old-event-id" {
		t.Errorf("NoMessages: LastEventID changed to %q; want %q", result.LastEventID, "old-event-id")
	}
}

// TestDistill_CursorAdvance verifies that when messages are present, LastEventID
// is advanced to lastEventID.
func TestDistill_CursorAdvance(t *testing.T) {
	t.Parallel()
	gs := &GoalState{LastEventID: "cursor-before"}
	Distill(gs, []string{"msg1"}, "cursor-after")

	if gs.LastEventID != "cursor-after" {
		t.Errorf("CursorAdvance: LastEventID = %q, want %q", gs.LastEventID, "cursor-after")
	}
}

// TestDistill_VerbatimDirectives verifies that messages are stored verbatim —
// no transformation, no trimming, no re-encoding.
func TestDistill_VerbatimDirectives(t *testing.T) {
	t.Parallel()
	msgs := []string{
		"  leading spaces  ",
		"line with\nnewline",
		"unicode: 日本語",
	}
	gs := &GoalState{}
	Distill(gs, msgs, "ev-1")

	if len(gs.OperatorDirectives) != len(msgs) {
		t.Fatalf("VerbatimDirectives: got %d directives, want %d", len(gs.OperatorDirectives), len(msgs))
	}
	for i, want := range msgs {
		if gs.OperatorDirectives[i] != want {
			t.Errorf("VerbatimDirectives[%d]: got %q, want %q", i, gs.OperatorDirectives[i], want)
		}
	}
}

// TestDistill_MaxDirectivesPrune verifies that exceeding MaxDirectives causes
// only the most recent MaxDirectives entries to be kept (oldest are dropped).
func TestDistill_MaxDirectivesPrune(t *testing.T) {
	t.Parallel()
	// Fill with MaxDirectives existing directives, then add 5 more.
	existing := make([]string, MaxDirectives)
	for i := range existing {
		existing[i] = strings.Repeat("old", 1)
	}
	gs := &GoalState{OperatorDirectives: existing}

	newMsgs := []string{"new-0", "new-1", "new-2", "new-3", "new-4"}
	Distill(gs, newMsgs, "ev-pruned")

	if len(gs.OperatorDirectives) != MaxDirectives {
		t.Fatalf("MaxDirectivesPrune: got %d directives, want %d", len(gs.OperatorDirectives), MaxDirectives)
	}
	// The last 5 entries must be the new messages.
	for i, want := range newMsgs {
		got := gs.OperatorDirectives[MaxDirectives-len(newMsgs)+i]
		if got != want {
			t.Errorf("MaxDirectivesPrune: tail[%d] = %q, want %q", i, got, want)
		}
	}
}

// TestDistill_ExactlyAtMax_NoPrune verifies that when OperatorDirectives reaches
// exactly MaxDirectives after the append, no pruning occurs.
func TestDistill_ExactlyAtMax_NoPrune(t *testing.T) {
	t.Parallel()
	// Start with MaxDirectives-1 existing and add exactly 1 more.
	existing := make([]string, MaxDirectives-1)
	for i := range existing {
		existing[i] = "prior"
	}
	gs := &GoalState{OperatorDirectives: existing}
	Distill(gs, []string{"exactly-max"}, "ev-exact")

	if len(gs.OperatorDirectives) != MaxDirectives {
		t.Fatalf("ExactlyAtMax: got %d directives, want %d", len(gs.OperatorDirectives), MaxDirectives)
	}
	// Last entry must be the new message.
	if last := gs.OperatorDirectives[MaxDirectives-1]; last != "exactly-max" {
		t.Errorf("ExactlyAtMax: last entry = %q, want %q", last, "exactly-max")
	}
}

// TestDistill_AppendsPriorDirectives verifies that existing directives are
// preserved (appended to, not replaced) when new messages arrive and the total
// does not exceed MaxDirectives.
func TestDistill_AppendsPriorDirectives(t *testing.T) {
	t.Parallel()
	gs := &GoalState{
		OperatorDirectives: []string{"prior-a", "prior-b"},
	}
	Distill(gs, []string{"new-c", "new-d"}, "ev-append")

	want := []string{"prior-a", "prior-b", "new-c", "new-d"}
	if len(gs.OperatorDirectives) != len(want) {
		t.Fatalf("AppendsPrior: got %d entries, want %d", len(gs.OperatorDirectives), len(want))
	}
	for i, w := range want {
		if gs.OperatorDirectives[i] != w {
			t.Errorf("AppendsPrior[%d]: got %q, want %q", i, gs.OperatorDirectives[i], w)
		}
	}
}
