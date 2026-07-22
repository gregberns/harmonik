// Package pi is the Pi implementation of the handlercontract.Harness seam: the
// launch-spec builder (argv, PI-021 allowlist-strip env, the base_url
// models.json passthrough), the NDJSON stream parser and session-id/agent_end
// interceptor, the fail-closed pre-flight billing guard, and the post-exit
// Refs:<bead> trailer fallback.
//
// The package sits BELOW the daemon and never imports it. That direction is the
// whole point: a P3 container must be able to link this harness without
// dragging the daemon monolith in with it. The daemon remains the composition
// root — newHarnessRegistry (internal/daemon/harnessregistry.go) constructs a
// pi.Harness and registers it under core.AgentTypePi, and the routed launch
// path and DOT cascade call into this package. Nothing here calls back.
// depguard enforces the edge (.golangci.yml, rule "harness-pi"); the
// harnesspi-freeze-gate.sh ratchet enforces that the concern does not reappear
// in internal/daemon under a new filename.
//
// Cross-harness helpers that pi shares with the codex harness — the resume seed
// prompt, the Refs-trailer commit/amend primitives, the env-key splitter — live
// in internal/harness/shared, a leaf below every harness implementation. They
// are deliberately NOT here, and a pi -> codex import would be a daemon
// back-edge in disguise.
//
// What deliberately did NOT move: pi_profile_resolve.go stays in the daemon. It
// is claim-time wiring bound to the daemon's projectconfig types
// (PiHarnessConfig / PiProfileConfig) with three call sites in workloop.go — it
// resolves WHICH pi profile a bead gets, which is a daemon decision, not harness
// implementation.
//
// Origin: internal/daemon/piharness.go, pilaunchspec.go, picommit.go,
// pijsonlparser.go and pibillingguard.go, relocated wholesale by P2 unit E1c
// (plans/2026-07-21-p2-extraction/E1c-pi.md §1.1). The move was pure: argv, env,
// event payloads and behaviour are byte-identical to the daemon-side originals.
//
// Spec: specs/pi-harness.md (PI-010…PI-050); specs/harness-contract.md §2
// (N1 credential strip, N2 CompletionProcessExit, N3 SessionIDCaptured).
package pi
