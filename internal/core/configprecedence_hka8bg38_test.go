package core

import (
	"testing"
)

// ---------------------------------------------------------------------------
// CP-037: PolicyConfigLayers and ResolvePolicyConfig
// specs/control-points.md §4.7.CP-037
// ---------------------------------------------------------------------------

// TestResolvePolicyConfig_PrecedenceOrder verifies that ResolvePolicyConfig
// applies the four-layer precedence correctly (runtime > operator-policy >
// workflow-def > default) per §4.7.CP-037.
func TestResolvePolicyConfig_PrecedenceOrder(t *testing.T) {
	t.Parallel()

	layers := PolicyConfigLayers{
		DefaultConfig: PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{
			"timeout": "60",
			"source":  "default",
		}},
		WorkflowDef: PolicyConfig{ExtraFields: map[string]string{
			"timeout": "120",
			"source":  "workflow",
		}},
		OperatorPolicy: PolicyConfig{SchemaVersion: 2, ExtraFields: map[string]string{
			"source": "operator",
		}},
		RuntimeOverride: PolicyConfig{ExtraFields: map[string]string{
			"source": "runtime",
		}},
	}

	got := ResolvePolicyConfig(layers)

	// runtime wins on "source"
	if got.ExtraFields["source"] != "runtime" {
		t.Errorf("source = %q, want %q (runtime override wins)", got.ExtraFields["source"], "runtime")
	}

	// operator-policy wins on SchemaVersion (runtime did not set it)
	if got.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2 (operator-policy wins)", got.SchemaVersion)
	}

	// workflow-def wins on "timeout" (no higher layer set it)
	if got.ExtraFields["timeout"] != "120" {
		t.Errorf("timeout = %q, want %q (workflow-def wins over default)", got.ExtraFields["timeout"], "120")
	}
}

// TestResolvePolicyConfig_DefaultFallthrough verifies that fields absent from
// all higher-precedence layers fall through to the default per CP-037.
func TestResolvePolicyConfig_DefaultFallthrough(t *testing.T) {
	t.Parallel()

	layers := PolicyConfigLayers{
		DefaultConfig: PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{
			"only-in-default": "yes",
		}},
	}

	got := ResolvePolicyConfig(layers)

	if got.ExtraFields["only-in-default"] != "yes" {
		t.Errorf("only-in-default = %q, want %q (default fallthrough)", got.ExtraFields["only-in-default"], "yes")
	}
	if got.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1 (default fallthrough)", got.SchemaVersion)
	}
}

// TestResolvePolicyConfig_AllLayersEmpty verifies that resolving four empty
// layers produces an empty result without panicking.
func TestResolvePolicyConfig_AllLayersEmpty(t *testing.T) {
	t.Parallel()

	got := ResolvePolicyConfig(PolicyConfigLayers{})

	if got.SchemaVersion != 0 {
		t.Errorf("SchemaVersion = %d, want 0 for all-empty layers", got.SchemaVersion)
	}
	if len(got.ExtraFields) != 0 {
		t.Errorf("ExtraFields = %v, want empty for all-empty layers", got.ExtraFields)
	}
}

// TestResolvePolicyConfig_RuntimeAlwaysWins verifies that a non-zero
// RuntimeOverride always overrides every other layer per CP-037.
func TestResolvePolicyConfig_RuntimeAlwaysWins(t *testing.T) {
	t.Parallel()

	layers := PolicyConfigLayers{
		RuntimeOverride: PolicyConfig{SchemaVersion: 99, ExtraFields: map[string]string{"key": "runtime"}},
		OperatorPolicy:  PolicyConfig{SchemaVersion: 5, ExtraFields: map[string]string{"key": "operator"}},
		WorkflowDef:     PolicyConfig{SchemaVersion: 3, ExtraFields: map[string]string{"key": "workflow"}},
		DefaultConfig:   PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{"key": "default"}},
	}

	got := ResolvePolicyConfig(layers)

	if got.SchemaVersion != 99 {
		t.Errorf("SchemaVersion = %d, want 99 (runtime wins)", got.SchemaVersion)
	}
	if got.ExtraFields["key"] != "runtime" {
		t.Errorf("key = %q, want %q (runtime wins)", got.ExtraFields["key"], "runtime")
	}
}

// TestResolvePolicyConfig_Deterministic verifies that ResolvePolicyConfig
// produces an identical result on repeated calls with identical layers —
// a structural precondition for the no-mid-run-reload invariant of CP-037.
//
// If the same layers always produce the same resolved config, callers can
// safely call ResolvePolicyConfig once at daemon startup and treat the result
// as a frozen snapshot: any re-resolution would produce the same config until
// a layer changes (which can only happen at an operator pause boundary per
// [operator-nfr.md §4.3]).
func TestResolvePolicyConfig_Deterministic(t *testing.T) {
	t.Parallel()

	layers := PolicyConfigLayers{
		DefaultConfig: PolicyConfig{SchemaVersion: 1, ExtraFields: map[string]string{
			"timeout": "60",
		}},
		OperatorPolicy: PolicyConfig{SchemaVersion: 2},
		RuntimeOverride: PolicyConfig{ExtraFields: map[string]string{
			"key": "runtime-val",
		}},
	}

	first := ResolvePolicyConfig(layers)
	second := ResolvePolicyConfig(layers)

	if first.SchemaVersion != second.SchemaVersion {
		t.Errorf("SchemaVersion differs between calls: %d vs %d (must be deterministic)", first.SchemaVersion, second.SchemaVersion)
	}
	if first.ExtraFields["timeout"] != second.ExtraFields["timeout"] {
		t.Errorf("timeout differs between calls: %q vs %q (must be deterministic)", first.ExtraFields["timeout"], second.ExtraFields["timeout"])
	}
	if first.ExtraFields["key"] != second.ExtraFields["key"] {
		t.Errorf("key differs between calls: %q vs %q (must be deterministic)", first.ExtraFields["key"], second.ExtraFields["key"])
	}
}

// TestResolvePolicyConfig_MutatingLayerAfterResolveDoes NotAffectResult
// verifies that post-resolve mutation of a layer's ExtraFields map does NOT
// affect the resolved snapshot — the snapshot is independent of the layer maps
// used to construct it.
//
// This is a structural sensor for the no-mid-run-reload invariant of CP-037:
// if a higher-precedence layer is updated between operator pauses, the running
// snapshot is unaffected because it was built from the layer state at the
// moment ResolvePolicyConfig was called.
func TestResolvePolicyConfig_MutatingLayerAfterResolveDoesNotAffectResult(t *testing.T) {
	t.Parallel()

	operatorFields := map[string]string{"knob": "original"}
	layers := PolicyConfigLayers{
		DefaultConfig:  PolicyConfig{SchemaVersion: 1},
		OperatorPolicy: PolicyConfig{ExtraFields: operatorFields},
	}

	got := ResolvePolicyConfig(layers)
	if got.ExtraFields["knob"] != "original" {
		t.Fatalf("pre-mutation: knob = %q, want %q", got.ExtraFields["knob"], "original")
	}

	// Simulate an operator-policy file update between runs (only allowed at a
	// pause boundary per CP-037). The resolved snapshot must not change.
	operatorFields["knob"] = "mutated"

	// The resolved snapshot was built by copying values; mutation of the source
	// map must not reach the snapshot.
	if got.ExtraFields["knob"] == "mutated" {
		t.Errorf("post-mutation: knob = %q, want %q (snapshot must be independent of source maps)", got.ExtraFields["knob"], "original")
	}
}
