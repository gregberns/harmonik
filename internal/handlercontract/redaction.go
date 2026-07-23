package handlercontract

import (
	"github.com/gregberns/harmonik/internal/core"
)

// The HC-031 common-prefix regex used to live here as a duplicate of the
// canonical core.redactionCommonPrefixRe, for use by schemachecker_hc033.go.
// That file was removed by the A9 mega-review (see doc.go); the copy has had no
// readers since and was deleted. The live definition is internal/core
// redaction.go, exercised through [RedactByFieldName] below.
//
// Spec: specs/handler-contract.md §4.7.HC-031.

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
