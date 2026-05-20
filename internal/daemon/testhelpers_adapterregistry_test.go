package daemon_test

// testhelpers_adapterregistry_test.go — shared test helper for AdapterRegistry
// construction in daemon tests.
//
// Background (hk-d8u1y): workloop.go and reviewloop.go no longer nil-guard
// deps.adapterRegistry. Every test that builds WorkLoopDepsParams must wire a
// non-nil sealed registry so the production code path is exercised.
//
// Use NewSealedAdapterRegistryForTest in every WorkLoopDepsParams that previously
// omitted AdapterRegistry2 (or passed nil).
//
// Bead ref: hk-d8u1y.

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// NewSealedAdapterRegistryForTest constructs and seals an AdapterRegistry with
// the real ClaudeCode adapter registered. It is the minimum-viable wiring
// required after the nil-guard deletion in hk-d8u1y.
//
// The registry is sealed by calling ForAgent (discarding the result) so that
// subsequent Register calls fail rather than silently succeeding.
//
// Tests that need a custom adapter (e.g. a timeout-injecting stub) should
// construct their own registry rather than using this helper.
func NewSealedAdapterRegistryForTest(t *testing.T) *handlercontract.AdapterRegistry {
	t.Helper()
	reg := handlercontract.NewAdapterRegistry()
	if err := handler.Register(reg); err != nil {
		t.Fatalf("NewSealedAdapterRegistryForTest: Register: %v", err)
	}
	_, _ = reg.ForAgent(core.AgentTypeClaudeCode) // seal
	return reg
}
