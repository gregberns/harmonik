package handlercontract

import "github.com/gregberns/harmonik/internal/core"

// RedactionRegistry is a type alias for core.RedactionRegistry, re-exported so
// that handler-side packages can name the type without importing internal/core
// directly (EV-002b boundary).
//
// Because this is a true Go alias (not a distinct named type), values of type
// handlercontract.RedactionRegistry and core.RedactionRegistry are
// interchangeable in all contexts — no conversion is required.
//
// Spec refs: specs/handler-contract.md §4.7.HC-030, §4.7.HC-032.
type RedactionRegistry = core.RedactionRegistry

// NewRedactionRegistry is re-exported from core for handler-side packages that
// cannot import internal/core directly (EV-002b boundary).
//
// Spec ref: specs/handler-contract.md §4.7.HC-032.
func NewRedactionRegistry() *RedactionRegistry {
	return core.NewRedactionRegistry()
}
