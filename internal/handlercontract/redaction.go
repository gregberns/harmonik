package handlercontract

import (
	"github.com/gregberns/harmonik/internal/core"
)

// RedactedSentinel is re-exported from core so that handler-side packages that
// cannot import internal/core directly (EV-002b boundary) can reference the
// sentinel value.
const RedactedSentinel = core.RedactedSentinel

// RedactByFieldName is re-exported from core for handler-side packages that
// cannot import internal/core directly (EV-002b boundary).
//
// Spec: specs/handler-contract.md §4.7.HC-031.
func RedactByFieldName(payload map[string]any) map[string]any {
	return core.RedactByFieldName(payload)
}
