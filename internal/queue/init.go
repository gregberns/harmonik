package queue

import (
	"log"

	"github.com/gregberns/harmonik/internal/core"
)

// subsystemID is the canonical source_subsystem identifier for this package.
//
// Spec ref: event-model.md §4.9 EV-034a — each subsystem MUST register its
// identifier exactly once at daemon init.
const subsystemID = "github.com/gregberns/harmonik/internal/queue"

func init() {
	if err := core.RegisterSourceSubsystem(subsystemID); err != nil {
		// Duplicate registration means two packages claimed the same identifier,
		// which is a programming error caught at startup per EV-034a.
		log.Fatalf("queue: RegisterSourceSubsystem: %v", err)
	}
}
