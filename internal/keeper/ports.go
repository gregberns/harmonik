package keeper

import (
	"context"
	"time"
)

// RespawnPort is the kill+respawn escalation fired after MaxHandoffTimeouts
// consecutive handoff timeouts above the force threshold (hk-qoz). It is a
// process-lifecycle effect, not a pane write, so it is its own one-method port
// rather than part of PaneWriter (D10).
type RespawnPort interface {
	ForceRestart(ctx context.Context, agent string) error
}

// GateSnapshot is the per-entry read burst of gate inputs. The shell composes
// it from narrow source probes. The reactor only receives this value.
//
// Zero-value semantics: LastUserTurnAt / LastAssistantTurnAt are zero when no
// qualifying transcript turn exists OR the corresponding feature is disabled
// (OperatorTurnLookback / PostAnswerGrace == 0 — the adapter skips the heavier
// transcript tail-scan entirely, matching today's lazy gate reads).
type GateSnapshot struct {
	Managed             bool
	CrispIdle           bool
	HoldingDispatch     bool
	Sleeping            bool
	Held                bool
	OperatorAttached    bool
	LastUserTurnAt      time.Time // Gate 5d input
	LastAssistantTurnAt time.Time // Gate 5e input
}
