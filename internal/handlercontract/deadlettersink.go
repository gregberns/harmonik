package handlercontract

import "github.com/gregberns/harmonik/internal/core"

// DeadLetterSink is a type alias for core.DeadLetterSink, re-exported so that
// handler-side packages can name the type without importing internal/core
// directly (EV-002b boundary).
//
// Because this is a true Go alias (not a distinct named type), values of type
// handlercontract.DeadLetterSink and core.DeadLetterSink are interchangeable
// in all contexts — no conversion is required.
//
// Spec ref: MVH_ROADMAP.md row #9.
// Bead ref: hk-qyue9.
type DeadLetterSink = core.DeadLetterSink

// OpenDeadLetterSink is re-exported from core for handler-side packages that
// cannot import internal/core directly (EV-002b boundary).
//
// Bead ref: hk-qyue9.
func OpenDeadLetterSink(path string) (DeadLetterSink, error) {
	return core.OpenDeadLetterSink(path)
}
