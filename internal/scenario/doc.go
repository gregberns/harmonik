// Package scenario houses the scenario-harness data types declared in
// specs/scenario-harness.md §6 (Schemas and data shapes). These types model
// the RECORD and ENUM shapes that the harness reads from scenario YAML files,
// produces in result records, and passes to the orchestrator for test
// execution. All type definitions in this package are normatively governed by
// that spec; if a field description or constraint appears to conflict with the
// spec, the spec is authoritative.
// (soak-marker: codex harness live-test T2)
package scenario
