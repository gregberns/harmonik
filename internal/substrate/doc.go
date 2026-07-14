// Package substrate is the generic, vertical-agnostic record→replay seam
// (spec: replay-substrate.md, codename session-restart-substrate).
//
// It holds four generic primitives, none of which knows any vertical's concrete
// event or action type:
//
//   - The seam: EventSource[E], Effector[A], and the free-function Run driver
//     loop — the composition boundary at which any source composes with any
//     effector without either knowing the other's concrete nature (RS-001,
//     RS-002).
//   - The two test doubles: FakeEffector[A] (a recorder) and SyntheticSource[E]
//     (a fixed-slice source) — the "swap any layer for a deterministic fake"
//     primitives (RS-006, RS-007).
//   - The replay engine: Twin[E] + ReplayCodec[E] + FaultConfig — presents a
//     captured corpus as an EventSource[E] and injects vertical-neutral
//     transport faults (RS-008 … RS-012).
//   - The determinism port: ClockPort + Ticker with a real (SystemClock) and a
//     fake (FakeClock) implementation, so a vertical can replay timeouts and
//     poll races in virtual time (RS-015).
//
// The package is a stdlib-only leaf: it imports only the Go standard library
// and MUST NOT import any vertical (codex, keeper) or the daemon. Verticals
// instantiate the substrate; the substrate never depends on a vertical (RS-005).
//
// # Not to be confused with (RS-023)
//
// This "substrate" is the record→replay seam ONLY. It is unrelated to three
// other normative "substrate" senses in the spec tree:
//
//   - the process-spawn seam (internal/handler.Substrate, the PL-021b Substrate
//     seam, PI-012a, CI-004);
//   - the cognition session substrate (CL-015 / CL-024 "substrate teardown",
//     the flywheel fresh-start session recycle);
//   - the transport/production substrate (credential-isolation.md §2.2 "LLM
//     transport substrate", pi-harness PI-069 "production substrate = paid").
//
// The replay-substrate spec name and the RS requirement prefix exist to keep
// this seam distinct from those three; the Go package remains internal/substrate.
package substrate
