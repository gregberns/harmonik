// Package policy holds the pure handler-pause decision predicates: the
// rate-limit hysteresis reducer and the auto-resume backoff computation.
//
// It is the S02 Policy Engine leaf, defined normatively in
// [specs/handler-pause.md] §5 (trigger taxonomy) and §1.2 (auto-resume).
//
// # Purity
//
// This package performs NO I/O, reads NO clock (time.Now), mints NO IDs
// (uuid), emits NO bus events, and mutates NO external state. Every decision
// is value-in / value-out (the internal/mergeq "critical func" discipline).
// The daemon shell threads IN every effect: it projects
// core.AgentRateLimitStatusPayload → RateLimitEvent at the call site (keeping
// uuid/payload out of policy), stamps the clock (TrippedAt), builds the
// in-flight freeze-list from RunRegistry, and calls Controller.Pause /
// Schedule / Resume. See internal/daemon/handlerpause_policy_37zy8.go and
// internal/daemon/handlerpause_autoresume_0otqs.go.
//
// The dependency edge is strictly daemon → policy, never reverse. The only
// non-test imports are the Go standard library and internal/core; the depguard
// rule for this package (.golangci.yml) permits exactly $gostd + internal/core.
package policy
