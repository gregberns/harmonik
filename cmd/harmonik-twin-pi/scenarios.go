// Scenario implementations for harmonik-twin-pi (M6 WS3-pi).
//
// Each scenario simulates one pi `--mode json` single-turn outcome as a
// deterministic NDJSON stream. Determinism is load-bearing: the twin's output
// must be byte-reproducible so the committed reference capture and the
// twin-vs-real parity gate stay stable (M6-PLAN §WS3).
//
// # The two variants
//
//   - happy-path — session → message_start → message_end → agent_end, with
//     non-zero usage on every step. The canonical single-turn success stream;
//     drives the daemon parser to a captured session id, a fired agent_end
//     watcher, and accumulated token usage.
//
//   - empty-turn — session → agent_end with an empty messages array and no
//     message_start/message_end. Exercises the zero-usage edge (a turn pi
//     produced no assistant message for) — the parser must still capture the
//     session id and fire the terminal agent_end.
//
// # Deterministic session id
//
// Each scenario uses a fixed session id (not random) so the NDJSON stream is
// byte-reproducible. A --session override (resume argv) replaces it, mirroring
// real pi which reuses the caller-supplied session on resume.
package main

import (
	"fmt"
	"io"
)

// Scenario name constants — use these in tests and the --scenario flag.
const (
	ScenarioHappyPath = "happy-path"
	ScenarioEmptyTurn = "empty-turn"
)

// Deterministic per-turn token counts for the happy-path scenario. Fixed so the
// reference capture and the parser-driven usage assertion are byte-stable.
const (
	happyPathInputTokens  int64 = 42
	happyPathOutputTokens int64 = 17
)

// sessionIDForScenario returns a deterministic session id for the given scenario.
// The id is a fixed UUID per scenario so NDJSON streams are byte-reproducible.
func sessionIDForScenario(scenario string) string {
	switch scenario {
	case ScenarioHappyPath:
		return "00000000-0000-4000-8000-0000000000a1"
	case ScenarioEmptyTurn:
		return "00000000-0000-4000-8000-0000000000e0"
	default:
		return "00000000-0000-4000-8000-000000000000"
	}
}

// scenarioConfig carries the runtime parameters for a scenario execution.
type scenarioConfig struct {
	// sessionOverride, when non-empty, replaces the deterministic scenario
	// session id — set from the --session resume flag so the twin echoes the
	// caller-supplied session id exactly as real pi does on resume.
	sessionOverride string
}

// runScenario drives the named scenario, writing pi NDJSON to w.
//
// Returns an error if the scenario name is unrecognised or a write fails.
func runScenario(w io.Writer, name string, cfg scenarioConfig) error {
	e := newPiEmitter(w)
	sessionID := cfg.sessionOverride
	if sessionID == "" {
		sessionID = sessionIDForScenario(name)
	}

	switch name {
	case ScenarioHappyPath:
		return runHappyPath(e, sessionID)
	case ScenarioEmptyTurn:
		return runEmptyTurn(e, sessionID)
	default:
		return fmt.Errorf("unknown scenario %q: must be one of %s, %s",
			name, ScenarioHappyPath, ScenarioEmptyTurn)
	}
}

// runHappyPath implements the happy-path variant: a single successful turn with
// non-zero usage. session → message_start → message_end → agent_end.
func runHappyPath(e *piEmitter, sessionID string) error {
	if err := e.emitSession(sessionID, "/twin/pi/happy-path"); err != nil {
		return fmt.Errorf("happy-path: emit session: %w", err)
	}
	if err := e.emitMessageStart(happyPathInputTokens); err != nil {
		return fmt.Errorf("happy-path: emit message_start: %w", err)
	}
	if err := e.emitMessageEnd(happyPathOutputTokens); err != nil {
		return fmt.Errorf("happy-path: emit message_end: %w", err)
	}
	if err := e.emitAgentEnd(happyPathInputTokens, happyPathOutputTokens); err != nil {
		return fmt.Errorf("happy-path: emit agent_end: %w", err)
	}
	return nil
}

// runEmptyTurn implements the empty-turn variant: session → agent_end with no
// message events and zero usage.
func runEmptyTurn(e *piEmitter, sessionID string) error {
	if err := e.emitSession(sessionID, "/twin/pi/empty-turn"); err != nil {
		return fmt.Errorf("empty-turn: emit session: %w", err)
	}
	if err := e.emitAgentEnd(0, 0); err != nil {
		return fmt.Errorf("empty-turn: emit agent_end: %w", err)
	}
	return nil
}
