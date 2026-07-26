// Package codex is the OpenAI codex implementation of the
// handlercontract.Harness seam: the launch-spec builder, the JSONL
// stream parser and thread-id interceptor, the per-launch stale-WAL guard, the
// pre-flight ChatGPT billing guard, the post-exit Refs:<bead> trailer fallback,
// and the no-work detector.
//
// The package sits BELOW the daemon and never imports it. That direction is the
// whole point: a P3 container must be able to link this harness without
// dragging the daemon monolith in with it. The daemon remains the composition
// root — newHarnessRegistry (internal/daemon/harnessregistry.go) constructs a
// codex.Harness and registers it under core.AgentTypeCodex, and the routed
// launch path and DOT cascade call into this package. Nothing here calls back.
// depguard enforces the edge (.golangci.yml, rule "harness-codex"); the
// harnesscodex-freeze-gate.sh ratchet enforces that the concern does not
// reappear in internal/daemon under a new filename.
//
// Cross-harness helpers that codex shares with the pi harness — the resume seed
// prompt, the Refs-trailer commit/amend primitives, the env-key splitter — live
// in internal/harness/shared, a leaf below every harness implementation. They
// are deliberately NOT here: a pi -> codex import would be a daemon back-edge in
// disguise.
//
// Origin: internal/daemon/codexharness.go, codexlaunchspec.go, codexcommit.go,
// codexjsonlparser.go, codexwalguard.go, codexbillingguard.go and
// codexnowork_hk368i4.go, relocated wholesale by P2 unit E1a-1
// (plans/2026-07-21-p2-extraction/E1a-codex-harness.md §1.2). The move was pure:
// error strings, log output and behaviour are byte-identical to the daemon-side
// originals, including the ones that still say "daemon:". Rewording observable
// output is a follow-up, not part of a relocation.
//
// Spec: specs/harness-contract.md §2 (N1 credential strip, N2
// CompletionProcessExit, N3 SessionIDCaptured).
package codex
