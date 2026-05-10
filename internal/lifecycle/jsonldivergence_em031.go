package lifecycle

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// JSONLReadResult is returned by ReadJSONLForDivergenceEvidence for each line
// successfully decoded from the JSONL stream. Only valid lines are included;
// the torn-tail line (if present) is silently discarded per EM-031.
type JSONLReadResult struct {
	// LineNumber is the 1-based line index in the JSONL stream.
	LineNumber int
	// Raw is the raw JSON bytes for this line (without the trailing newline).
	Raw json.RawMessage
}

// ErrJSONLMidFileCorruption is returned by ReadJSONLForDivergenceEvidence when
// an unparseable (bad JSON) line is found anywhere OTHER than the final line, or
// when the final line IS terminated by a newline but still fails to parse.
//
// Per EM-031: an unparseable mid-file line, or an unparseable tail line
// terminated by a newline, IS a Cat 6b signal
// (reconciliation/spec.md §8.11a).
//
// Callers that receive this error MUST route the event-log file to Cat 6b
// reconciliation rather than treating the file as usable divergence evidence.
var ErrJSONLMidFileCorruption = errors.New("lifecycle: JSONL mid-file or terminated-tail corruption (Cat 6b signal)")

// ReadJSONLForDivergenceEvidence reads a JSONL byte slice as divergence
// evidence per EM-031. It applies the torn-tail discard rule: if the final
// line of the input is unparseable AND is NOT terminated by a newline, the
// final line is silently discarded and the preceding valid lines are returned.
// All other corruption cases (mid-file, terminated-tail) return
// ErrJSONLMidFileCorruption, which maps to a Cat 6b reconciliation signal.
//
// Usage: callers pass the raw bytes from a JSONL event-log file opened for
// divergence-evidence reading. The returned slice contains one entry per valid
// line decoded, in file order. An empty slice with a nil error means the file
// has no valid lines (e.g., empty or single-line torn tail).
//
// Constraint: callers MUST NOT use the JSONL content to reconstruct run state.
// Per EM-031, state reconstruction MUST use git + Beads only. JSONL reads are
// permitted only to detect divergence (e.g., Cat 3 corroboration) and MUST be
// discarded after the comparison step.
//
// Spec ref: execution-model.md §4.7 EM-031 — "A consumer reading JSONL for
// divergence-evidence purposes per this requirement MUST tolerate a torn last
// line: if the final line of a JSONL file is unparseable AND is not terminated
// by a newline, the consumer MUST discard that line and treat the remainder of
// the file as valid rather than raising a Cat 6b integrity signal."
func ReadJSONLForDivergenceEvidence(data []byte) ([]JSONLReadResult, error) {
	if len(data) == 0 {
		return nil, nil
	}

	// Split data into lines while preserving whether the final line ends in '\n'.
	lines, finalHasNewline := splitJSONLLines(data)
	if len(lines) == 0 {
		return nil, nil
	}

	var results []JSONLReadResult

	for i, line := range lines {
		lineNum := i + 1
		isLast := i == len(lines)-1

		// line has no trailing newline (bufio.Scanner strips it).
		if len(line) == 0 {
			// Skip blank lines.
			continue
		}

		// Attempt to parse as JSON.
		if !json.Valid(line) {
			if isLast && !finalHasNewline {
				// Torn tail: unparseable final line without a terminating newline.
				// Per EM-031: silently discard, NOT a Cat 6b signal.
				break
			}
			// Mid-file corruption, or a terminated (newline-closed) bad tail line.
			// Per EM-031: IS a Cat 6b signal.
			return nil, fmt.Errorf("%w: line %d", ErrJSONLMidFileCorruption, lineNum)
		}

		raw := make(json.RawMessage, len(line))
		copy(raw, line)
		results = append(results, JSONLReadResult{
			LineNumber: lineNum,
			Raw:        raw,
		})
	}

	return results, nil
}

// splitJSONLLines splits JSONL data into individual lines (without trailing
// newlines, as bufio.Scanner strips them). The second return value reports
// whether the last byte of data is '\n' (i.e., the final line is properly
// terminated). Each returned line is a copy of the scanner token.
func splitJSONLLines(data []byte) (lines [][]byte, finalHasNewline bool) {
	finalHasNewline = len(data) > 0 && data[len(data)-1] == '\n'

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		tok := scanner.Bytes()
		cp := make([]byte, len(tok))
		copy(cp, tok)
		lines = append(lines, cp)
	}
	// scanner.Err() is always nil for in-memory byte-slice readers; checked
	// for errcheck compliance.
	if err := scanner.Err(); err != nil {
		// Unreachable for bytes.NewReader; treat as a programming error.
		lines = nil
	}
	return lines, finalHasNewline
}
