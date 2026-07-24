package daemon

// export_extpkg_queuewiring_test.go — thin re-exports of the already-extracted
// internal/queuewiring package (RT19.15b split of export_test.go). RETAINED shims
// for STAYING daemon_test files that name the queuewiring types. package daemon
// test file; see export_test.go header for the seam rationale. Bead: hk-ecrxy.

import (
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/queuewiring"
)

// ExportedQueueStoreSetQueue installs q into the QueueStore for tests (NQ-X1).
//
// Bead ref: hk-tigaf.11.
func ExportedQueueStoreSetQueue(s *queuewiring.QueueStore, q *queue.Queue) {
	s.SetQueue(q)
}

// ExportedNewQueueStore returns a queuewiring.QueueStore for tests in package
// daemon_test. The store itself moved to internal/queuewiring (P2 E3a); this
// shim is kept because 41 daemon_test files call it and none names the type, so
// keeping it is the difference between a bounded diff and a 60-file one.
//
// Bead ref: hk-j808w.
func ExportedNewQueueStore() *queuewiring.QueueStore {
	return queuewiring.NewQueueStore()
}

// ExportedQueueOperatorEventConsumerConfig is a type alias for
// queuewiring.QueueOperatorEventConsumerConfig for tests in package daemon_test.
// Kept after the P2 E3a move because operatornfr_pause_inflight_hk95a2r_test.go
// (which stays in daemon) names it.
//
// Bead ref: hk-7urls.
type ExportedQueueOperatorEventConsumerConfig = queuewiring.QueueOperatorEventConsumerConfig

// ExportedNewQueueOperatorEventConsumer exposes
// queuewiring.NewQueueOperatorEventConsumer for tests in package daemon_test.
//
// Bead ref: hk-7urls.
var ExportedNewQueueOperatorEventConsumer = queuewiring.NewQueueOperatorEventConsumer
