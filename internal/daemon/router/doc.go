// Package socketrouter holds the pure protocol-dispatch table carved out of the
// daemon's handleSocketConn (giant-retirement, plans/2026-07-16-giant-retirement).
//
// It is a routing leaf, placed as a daemon sub-package at internal/daemon/router
// (OG-1 resolved: daemon sub-package, not a top-level subsystem). Its content is
// envelope classification (Classify), an op→handler lookup table (Router), and a
// neutral per-op outcome (Result). The 26 per-op handler bodies stay in daemon as
// adapter methods threaded IN as closures (the mergeq/runexec pattern).
//
// # Purity
//
// This package performs NO I/O, touches NO net.Conn, reads NO clock, mints NO
// IDs, and emits NO bus events. Every operation is value-in / value-out over
// json.RawMessage. The only non-test imports are the Go standard library
// (context, encoding/json, fmt) — no net, no internal/core. The op is a plain
// string; payloads are json.RawMessage.
//
// The dependency edge is strictly daemon → socketrouter, never the reverse. The
// daemon threads effectful handler closures in; the router MUST NOT import daemon
// back (import-cycle caveat). The depguard rule for this package (.golangci.yml,
// key "socketrouter") permits exactly $gostd + self-import.
//
// All wire vocabulary (the "daemon:" error prefixes, the SocketResponse envelope,
// the two writers, the hook-relay and subscribe pre-branches) lives daemon-side.
// An unknown op yields a neutral Result{Unknown: true}; the daemon builds the
// exact "daemon: unknown op %q" wire string from it.
package socketrouter
