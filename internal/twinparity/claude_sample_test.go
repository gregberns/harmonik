package twinparity_test

import (
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/twinparity"
)

// TestClaudeHappyPathSampleSelfEquivalent proves the hand-authored WS3-Claude-A
// sample capture (testdata/twin-parity/claude/happy-path-sample/events.jsonl)
// loads via twinparity.LoadStream and matches F1's durable terminal spine
// (TerminalKinds: outcome_emitted → bead_closed → run_completed) when asserted
// against itself. This pins the capture FORMAT to the equivalence engine so
// Claude-B's replay round-trip has a valid, spine-complete corpus.
//
// It is NOT a real capture (see meta.yaml hand_authored: true); the real
// reference captures are produced by `make capture-claude-fixtures` on an
// auth'd box.
func TestClaudeHappyPathSampleSelfEquivalent(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "twin-parity", "claude", "happy-path-sample", "events.jsonl")

	sample, err := twinparity.LoadStream(path)
	if err != nil {
		t.Fatalf("LoadStream(%s): %v", path, err)
	}
	if len(sample.Events) == 0 {
		t.Fatalf("sample stream is empty; expected the durable terminal triad")
	}

	// Self-equivalence with default options (spine = TerminalKinds). A failure
	// here means the sample is missing a durable-spine kind or drifted from F1.
	twinparity.AssertStreamEquivalent(t, sample, sample, twinparity.EquivOptions{})
}
