package goalstate

// Distill appends messages to gs.OperatorDirectives verbatim, prunes to
// MaxDirectives (keeping the most recent), and advances LastEventID to
// lastEventID. Returns gs (mutated in-place). No-op when messages is empty.
//
// This is the testability seam extracted from goalkeeper_cmd.go so that the
// distil logic can be exercised without shelling out to harmonik comms.
//
// Spec ref: docs/flywheel-self-reinforcing-design.md §6 (goal-keeper distil contract).
// Bead: hk-owz1 (goal-keeper), hk-fvzt (BT2 unit tests).
func Distill(gs *GoalState, messages []string, lastEventID string) *GoalState {
	if len(messages) == 0 {
		return gs
	}
	gs.OperatorDirectives = append(gs.OperatorDirectives, messages...)
	if len(gs.OperatorDirectives) > MaxDirectives {
		gs.OperatorDirectives = gs.OperatorDirectives[len(gs.OperatorDirectives)-MaxDirectives:]
	}
	if lastEventID != "" {
		gs.LastEventID = lastEventID
	}
	return gs
}
