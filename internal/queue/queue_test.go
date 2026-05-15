package queue_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

// Smoke test: confirms the package compiles and the Queue zero value is
// allocatable. Substantive tests land with their owning beads (hk-9s6yr).
func TestQueueCompiles(t *testing.T) {
	t.Parallel()
	var _ queue.Queue
}
