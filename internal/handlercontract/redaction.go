package handlercontract

import (
	"regexp"

	"github.com/gregberns/harmonik/internal/core"
)

// redactionCommonPrefixRe is the HC-031 common-prefix regex, kept here for
// internal use by schemachecker_hc033.go (structural payload scan).
//
// The canonical definition and authoritative implementation live in
// internal/core.  This copy MUST stay in sync with core — the evinv006 test
// (core/evinv006_redaction_sensor_hqwn52_test.go) validates alignment.
//
// Spec: specs/handler-contract.md §4.7.HC-031.
var redactionCommonPrefixRe = regexp.MustCompile(`(?i)(secret|token|password|api[_-]?key|auth)`)

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
