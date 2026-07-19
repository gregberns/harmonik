// Package hook holds the pure CHB-025 hook-relay state machine: the
// last-received-wins outcome dedup store and the agent_ready callback registry.
//
// It is defined normatively in [specs/claude-hook-bridge.md] §4.10 CHB-025 and
// §6.1/§6.2 (HookRelayMessage / HookRelayAck).
//
// # Purity
//
// This package performs NO I/O, reads NO clock (time.Now), and mints NO IDs
// (uuid). Every side effect the daemon needs — bus emission for the rate-limit
// path, clock reads, UUID parsing — is threaded IN by the daemon shell (see
// internal/daemon/hookrelay_chb025.go), which composes *SessionStore and adds
// those effects. The dependency edge is strictly daemon → hook, never reverse.
//
// The only non-test imports are the Go standard library; the depguard rule for
// this package (.golangci.yml) additionally permits internal/core and
// internal/eventbus, neither of which this state machine needs today.
package hook
