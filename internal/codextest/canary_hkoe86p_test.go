package codextest_test

// Pre-deploy corpus integrity drift canary (codex-app-server T5, hk-oe86p)
//
// TestCodexDriftCanary is the single pre-deploy drift gate. It verifies:
//
//  1. The captured corpus file (raw-session-01.jsonl) parses with ZERO
//     FrameKindRaw frames — if codexwire drifts away from the real protocol
//     (new method added, old method renamed) this test fires.
//
//  2. The corpus is non-empty — an accidentally truncated corpus fails fast
//     rather than silently passing all parse checks on zero frames.
//
//  3. Every line is valid JSON — a corrupted corpus line causes an
//     immediate failure.
//
// Run as part of `make test` (CODEX_LIVE=0). Also suitable as a pre-deploy
// gate step: `go test -count=1 -run TestCodexDriftCanary ./internal/codextest/...`
//
// Bead: hk-oe86p [codex-app-server T5]

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexwire"
)

// TestCodexDriftCanary is the pre-deploy wire-protocol drift canary.
// Fails if the parser cannot handle any frame in the captured corpus.
func TestCodexDriftCanary(t *testing.T) {
	f, err := os.Open(l1CorpusPath())
	if err != nil {
		t.Fatalf("drift canary: open corpus: %v", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	lineCount := 0
	unknownMethods := []string{}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		lineCount++

		// Gate 1: valid JSON
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			t.Errorf("drift canary: line %d: invalid JSON: %v", lineCount, err)
			continue
		}

		// Gate 2: parseable by codexwire
		frame, parseErr := codexwire.Parse([]byte(line))
		if parseErr != nil {
			t.Errorf("drift canary: line %d: Parse error: %v", lineCount, parseErr)
			continue
		}

		// Gate 3: no FrameKindRaw (no unknown methods)
		if frame.Kind == codexwire.FrameKindRaw {
			unknownMethods = append(unknownMethods, frame.Method)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("drift canary: scan: %v", err)
	}

	// Gate 4: corpus non-empty (must have >= 10 frames from a real session)
	if lineCount < 10 {
		t.Errorf("drift canary: corpus too small (%d frames) — possible truncation", lineCount)
	}

	// Gate 3 result: fail if any unknown methods
	if len(unknownMethods) > 0 {
		t.Errorf("drift canary: %d FrameKindRaw frames — methods unregistered in codexwire: %v",
			len(unknownMethods), unknownMethods)
		t.Log("FIX: add the unregistered method(s) to the methodRegistry in internal/codexwire/codexwire.go")
	}

	t.Logf("drift canary: PASS — %d corpus frames, 0 unknown methods", lineCount)
}
