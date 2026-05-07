//go:build scenario

// Package scenario is the placeholder package for harmonik's scenario test
// suite. Full scenario execution is defined in specs/scenario-harness.md and
// will be implemented by hk-i0tw.* beads. This file exists so that
// `go test -tags=scenario ./test/scenario/` compiles and produces zero
// failures, satisfying the `make check-full` Tier-3 gate before the
// scenario-harness implementation lands.
package scenario
