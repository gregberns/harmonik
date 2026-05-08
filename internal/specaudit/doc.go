// Package specaudit contains cross-corpus spec conformance tests that walk
// specs/*.md and specs/**/spec.md and assert structural invariants.
//
// Each test is named after the architecture requirement it enforces (e.g.
// TestAR005TagsMutualExclusion for AR-005). Tests in this package are binding:
// a failure means the spec corpus is out of conformance with architecture.md.
//
// Spec ref: specs/architecture.md §4.2 AR-005.
package specaudit
