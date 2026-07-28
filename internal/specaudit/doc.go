// Package specaudit contains binding tests that tie the spec corpus to the code
// that implements it.
//
// The 129 prose-only sensors that formerly lived here — tests that walked
// specs/*.md asserting headings and phrases were present, executing zero product
// code — were removed on 2026-07-27. They could not fail when the code broke and
// could not pass when it was fixed. See plans/2026-07-27-delete-and-rewrite/.
//
// What remains are the three tests that exercise real product behaviour against
// the spec text: the agent-type regex (AR-025), the event-bus interface
// (HQWN-57), and declarative scenario loadability (SH-INV-005). Each imports the
// package it constrains, so a failure here means code and spec have genuinely
// diverged.
package specaudit
