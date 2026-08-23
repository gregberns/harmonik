package queue

import (
	"log"

	"github.com/gregberns/harmonik/internal/core"
)

const subsystemID = "github.com/gregberns/harmonik/internal/queue"

func init() {
	if err := core.RegisterSourceSubsystem(subsystemID); err != nil {
		log.Fatalf("queue: RegisterSourceSubsystem: %v", err)
	}
}
