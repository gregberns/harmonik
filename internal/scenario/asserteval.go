package scenario

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// RawEvent is the minimal envelope decoded from a JSONL event line for
// assertion evaluation. Only the fields needed for SH-021 evaluation
// (type + payload) are decoded; envelope metadata fields are ignored.
type RawEvent struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ReadEventLog reads the JSONL event log at logPath per SH-020 / SH-024.
//
// Torn-tail handling: a partial trailing record after the last fsync is
// silently skipped per [event-model.md §6.2] — it is normal post-fsync
// behavior, not corruption. A file that ends with a newline is handled
// cleanly (the empty string after the final \n is the "torn tail" and
// is discarded).
//
// The caller MUST classify a non-nil error return as verdict=error with
// failure_class=harness-internal-error per SH-024. Errors include:
//
//   - file/dir does not exist (SH-024 i)
//   - permissions error (SH-024 ii)
//   - JSON parse error at a non-tail position (SH-024 iii)
//   - I/O error during read (SH-024 iv)
//   - bus_overflow event observed (SH-024 v)
//
// Spec ref: specs/scenario-harness.md §4.6 SH-020, SH-024.
func ReadEventLog(logPath string) ([]RawEvent, error) {
	//nolint:gosec // G304: logPath is the daemon-produced event log path, not user input
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}

	// Split on newlines. The last element after splitting is either:
	//   - empty string  — file ends with \n (clean file); discard it
	//   - partial record — torn tail (post-fsync partial write); skip silently
	// Either way, lines[:len(lines)-1] contains only complete lines.
	lines := bytes.Split(data, []byte("\n"))
	toProcess := lines[:len(lines)-1]

	var events []RawEvent
	for i, line := range toProcess {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("asserteval: JSON parse error at line %d (mid-file corruption per SH-024): %w", i+1, err)
		}
		if ev.Type == string(core.EventTypeBusOverflow) {
			return nil, fmt.Errorf("asserteval: bus_overflow event at line %d: assertion completeness is defeated (SH-024 v)", i+1)
		}
		events = append(events, ev)
	}
	return events, nil
}

// tokenizePayloadPath splits a dotted-path key into lookup segments per SH-021.
//
// Grammar: dots separate object keys; bracket form addresses array indices.
// "a.b[0].c"  → ["a", "b", "[0]", "c"]
// "items[0].id" → ["items", "[0]", "id"]
// "a[0][1]"    → ["a", "[0]", "[1]"]
func tokenizePayloadPath(path string) []string {
	var result []string
	// Process each dot-separated component.
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		rest := part
		for rest != "" {
			if strings.HasPrefix(rest, "[") {
				// Bracket-form array index.
				end := strings.Index(rest, "]")
				if end < 0 {
					// Malformed; treat remainder as a key.
					result = append(result, rest)
					rest = ""
					break
				}
				result = append(result, rest[:end+1])
				rest = rest[end+1:]
				// Strip leading "." separator if present (e.g., "[0].foo").
				rest = strings.TrimPrefix(rest, ".")
			} else {
				// Object key; stop before the first "[".
				bracketIdx := strings.Index(rest, "[")
				if bracketIdx < 0 {
					result = append(result, rest)
					rest = ""
				} else {
					result = append(result, rest[:bracketIdx])
					rest = rest[bracketIdx:]
				}
			}
		}
	}
	return result
}

// walkPayloadPath resolves a dotted-path key within a JSON-decoded payload
// per SH-021. Returns (value, true) on success; (nil, false) on missing path
// or type mismatch.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-021.
func walkPayloadPath(payload any, path string) (any, bool) {
	if path == "" {
		return payload, true
	}
	segments := tokenizePayloadPath(path)
	cur := payload
	for _, seg := range segments {
		if cur == nil {
			return nil, false
		}
		if strings.HasPrefix(seg, "[") && strings.HasSuffix(seg, "]") {
			// Array index.
			idx, err := strconv.Atoi(seg[1 : len(seg)-1])
			if err != nil {
				return nil, false
			}
			arr, ok := cur.([]any)
			if !ok || idx < 0 || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
		} else {
			// Object key.
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			val, exists := obj[seg]
			if !exists {
				return nil, false
			}
			cur = val
		}
	}
	return cur, true
}

// jsonValuesEqual reports whether two JSON-decoded values are equal per SH-021:
//   - numbers compare by numeric value (1 == 1.0)
//   - strings compare byte-equal (NFC normalization; ASCII content is NFC by definition)
//   - booleans by identity
//   - null by identity (both nil)
//   - arrays element-wise; objects key-set and value-wise
func jsonValuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	// encoding/json decodes all numbers as float64.
	af, aIsFloat := a.(float64)
	bf, bIsFloat := b.(float64)
	if aIsFloat && bIsFloat {
		return af == bf
	}
	as, aIsStr := a.(string)
	bs, bIsStr := b.(string)
	if aIsStr && bIsStr {
		return as == bs
	}
	ab, aIsBool := a.(bool)
	bb, bIsBool := b.(bool)
	if aIsBool && bIsBool {
		return ab == bb
	}
	aArr, aIsArr := a.([]any)
	bArr, bIsArr := b.([]any)
	if aIsArr && bIsArr {
		if len(aArr) != len(bArr) {
			return false
		}
		for i := range aArr {
			if !jsonValuesEqual(aArr[i], bArr[i]) {
				return false
			}
		}
		return true
	}
	aObj, aIsObj := a.(map[string]any)
	bObj, bIsObj := b.(map[string]any)
	if aIsObj && bIsObj {
		if len(aObj) != len(bObj) {
			return false
		}
		for k, av := range aObj {
			bv, ok := bObj[k]
			if !ok || !jsonValuesEqual(av, bv) {
				return false
			}
		}
		return true
	}
	return false
}

// payloadMatchHolds reports whether the actual raw-JSON event payload satisfies
// the declared payload_match predicates per SH-021 shallow-merge semantics:
// declared keys MUST appear in the actual payload with equal values; the actual
// payload MAY contain additional unmatched keys.
func payloadMatchHolds(rawPayload json.RawMessage, match map[string]any) bool {
	if len(match) == 0 {
		return true
	}
	var payload any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return false
	}
	for path, expected := range match {
		actual, ok := walkPayloadPath(payload, path)
		if !ok || !jsonValuesEqual(actual, expected) {
			return false
		}
	}
	return true
}

// eventMatchesExpectation reports whether ev matches exp's type and optional
// payload_match predicates.
func eventMatchesExpectation(ev RawEvent, exp EventExpectation) bool {
	if ev.Type != string(exp.Type) {
		return false
	}
	return exp.PayloadMatch == nil || payloadMatchHolds(ev.Payload, exp.PayloadMatch)
}

// evalEventExpectation evaluates a single EventExpectation against the captured
// event log per SH-021.
func evalEventExpectation(exp EventExpectation, events []RawEvent) AssertionResult {
	ar := AssertionResult{
		Description:   exp.Description,
		ExpectedValue: map[string]any{"type": string(exp.Type), "payload_match": exp.PayloadMatch},
	}

	switch exp.Kind {
	case EventExpectationKindPresent:
		ar.AssertionKind = AssertionResultKindEventPresent
		for _, ev := range events {
			if eventMatchesExpectation(ev, exp) {
				ar.Passed = true
				ar.ActualValue = map[string]any{"type": ev.Type}
				return ar
			}
		}
		ar.Passed = false
		ar.ActualValue = "event not found"

	case EventExpectationKindAbsent:
		ar.AssertionKind = AssertionResultKindEventAbsent
		for _, ev := range events {
			if eventMatchesExpectation(ev, exp) {
				ar.Passed = false
				ar.ActualValue = map[string]any{"type": ev.Type, "note": "event was present"}
				return ar
			}
		}
		ar.Passed = true
		ar.ActualValue = "event correctly absent"
	}
	return ar
}

// isFilePredicateKind reports whether kind involves filesystem path resolution.
func isFilePredicateKind(k WorkspacePredicateKind) bool {
	switch k {
	case WorkspacePredicateKindFileExists,
		WorkspacePredicateKindFileContentsEqual,
		WorkspacePredicateKindFileContentsMatch:
		return true
	}
	return false
}

// checkSymlinkSafety returns an error if targetPath is a symlink that resolves
// outside workspaceDir. Returns nil if path doesn't exist, is not a symlink, or
// resolves safely within the workspace.
func checkSymlinkSafety(targetPath, workspaceDir string) error {
	fi, err := os.Lstat(targetPath)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return fmt.Errorf("symlink resolution failed: %w", err)
	}
	absWS, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("workspace abs path: %w", err)
	}
	absResolved := filepath.Clean(resolved)
	absWS = filepath.Clean(absWS)
	// Accept if resolved path IS the workspace dir or is under it.
	if absResolved != absWS && !strings.HasPrefix(absResolved, absWS+string(filepath.Separator)) {
		return fmt.Errorf("symlink traversal: %q resolves to %q outside workspace", targetPath, resolved)
	}
	return nil
}

// evalWorkspacePredicate evaluates a single WorkspacePredicate against the
// per-scenario worktree at workspaceDir per SH-022.
//
// File predicates inspect working files directly. Git predicates (git_ref_at,
// commit_trailer_present) use git plumbing commands inside workspaceDir.
// Symlinks that escape the workspace are rejected per SH-022.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-022.
func evalWorkspacePredicate(pred WorkspacePredicate, workspaceDir string) AssertionResult {
	ar := AssertionResult{
		AssertionKind: AssertionResultKindWorkspaceState,
		Description:   pred.Description,
		ExpectedValue: map[string]any{"kind": string(pred.Kind), "path": pred.Path, "expected": pred.Expected},
	}

	// Symlink traversal check for file predicates (SH-022).
	if isFilePredicateKind(pred.Kind) {
		targetPath := filepath.Join(workspaceDir, filepath.FromSlash(pred.Path))
		if err := checkSymlinkSafety(targetPath, workspaceDir); err != nil {
			ar.Passed = false
			ar.ActualValue = err.Error()
			return ar
		}

		switch pred.Kind {
		case WorkspacePredicateKindFileExists:
			_, statErr := os.Stat(targetPath)
			ar.Passed = statErr == nil
			if statErr != nil {
				ar.ActualValue = statErr.Error()
			} else {
				ar.ActualValue = "file exists"
			}

		case WorkspacePredicateKindFileContentsEqual:
			//nolint:gosec // G304: targetPath is validated against workspaceDir by checkSymlinkSafety above
			contents, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				ar.Passed = false
				ar.ActualValue = fmt.Sprintf("read error: %v", readErr)
				return ar
			}
			actual := string(contents)
			ar.Passed = actual == *pred.Expected
			ar.ActualValue = actual

		case WorkspacePredicateKindFileContentsMatch:
			//nolint:gosec // G304: targetPath is validated against workspaceDir by checkSymlinkSafety above
			contents, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				ar.Passed = false
				ar.ActualValue = fmt.Sprintf("read error: %v", readErr)
				return ar
			}
			re, compErr := regexp.Compile(*pred.Expected)
			if compErr != nil {
				ar.Passed = false
				ar.ActualValue = fmt.Sprintf("pattern compile error: %v", compErr)
				return ar
			}
			ar.Passed = re.Match(contents)
			ar.ActualValue = string(contents)
		}
		return ar
	}

	// Git predicates: pred.Path is interpreted as a git ref name.
	switch pred.Kind {
	case WorkspacePredicateKindGitRefAt:
		// Resolve the ref at pred.Path to a SHA.
		out, gitErr := exec.CommandContext(context.Background(), "git", "-C", workspaceDir, "rev-parse", "--verify", pred.Path).Output()
		if gitErr != nil {
			ar.Passed = false
			ar.ActualValue = fmt.Sprintf("git rev-parse %q: %v", pred.Path, gitErr)
			return ar
		}
		actualSHA := strings.TrimSpace(string(out))

		expected := *pred.Expected
		// If expected is a ref name (not a full 40-char SHA), resolve it.
		if !sha1Re.MatchString(expected) {
			expOut, expErr := exec.CommandContext(context.Background(), "git", "-C", workspaceDir, "rev-parse", "--verify", expected).Output()
			if expErr != nil {
				ar.Passed = false
				ar.ActualValue = fmt.Sprintf("git rev-parse expected ref %q: %v", expected, expErr)
				return ar
			}
			expected = strings.TrimSpace(string(expOut))
		}
		ar.Passed = actualSHA == expected
		ar.ActualValue = actualSHA

	case WorkspacePredicateKindCommitTrailerPresent:
		// Read the commit message at pred.Path (ref name).
		out, gitErr := exec.CommandContext(context.Background(), "git", "-C", workspaceDir, "log", "-1", "--format=%B", pred.Path).Output()
		if gitErr != nil {
			ar.Passed = false
			ar.ActualValue = fmt.Sprintf("git log %q: %v", pred.Path, gitErr)
			return ar
		}
		message := string(out)
		trailerKey := *pred.Expected
		ar.Passed = commitMessageHasTrailer(message, trailerKey)
		ar.ActualValue = strings.TrimSpace(message)
	}
	return ar
}

// commitMessageHasTrailer reports whether the commit message contains a trailer
// with the given key. Trailer lines have the form "Key: value" or "Key : value".
// Key-only matching per §6.3 (values are NOT matched at v0.1).
func commitMessageHasTrailer(message, key string) bool {
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+":") || strings.HasPrefix(trimmed, key+" :") {
			return true
		}
	}
	return false
}

// evalOutcomeExpectation evaluates the OutcomeExpectation (exit_code assertion)
// against the captured event log per SH-021.
//
// Actual outcome derivation:
//  1. Last outcome_emitted event's outcome_status (§8.1.8 — carries the explicit value).
//  2. Fallback: run_completed → SUCCESS; run_failed → FAIL.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-021.
func evalOutcomeExpectation(exp OutcomeExpectation, events []RawEvent) AssertionResult {
	ar := AssertionResult{
		AssertionKind: AssertionResultKindExitCode,
		Description:   exp.Description,
		ExpectedValue: string(exp.OutcomeStatus),
	}

	var actual core.OutcomeStatus
	found := false

	// Priority 1: last outcome_emitted event carries explicit outcome_status.
	for _, ev := range events {
		if ev.Type == string(core.EventTypeOutcomeEmitted) {
			var p struct {
				OutcomeStatus core.OutcomeStatus `json:"outcome_status"`
			}
			if err := json.Unmarshal(ev.Payload, &p); err == nil && p.OutcomeStatus.Valid() {
				actual = p.OutcomeStatus
				found = true
			}
		}
	}

	// Priority 2: fall back to terminal event type.
	if !found {
		for _, ev := range events {
			switch ev.Type {
			case string(core.EventTypeRunCompleted):
				actual = core.OutcomeStatusSuccess
				found = true
			case string(core.EventTypeRunFailed):
				actual = core.OutcomeStatusFail
				found = true
			}
		}
	}

	if !found {
		ar.Passed = false
		ar.ActualValue = "no terminal event (run_completed / run_failed / outcome_emitted) found in event log"
		return ar
	}

	ar.ActualValue = string(actual)
	ar.Passed = actual == exp.OutcomeStatus
	return ar
}

// EvaluateAssertions evaluates all declared assertions in sf against the
// captured event log and workspace state per SH-021 through SH-023.
//
// Evaluation order: expected_events (declaration order), then
// expected_workspace (declaration order), then expected_outcome (single entry
// if declared). Per SH-023, the harness MUST NOT short-circuit; every
// assertion is evaluated even after an earlier failure.
//
// Returns:
//   - results: one AssertionResult per declared assertion, in evaluation order.
//   - verdict: ScenarioVerdictPass if all pass; ScenarioVerdictFail otherwise.
//   - fc: empty (None) on pass; FailureClassAssertionFailed on any failure.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-021, SH-022, SH-023.
func EvaluateAssertions(sf ScenarioFile, events []RawEvent, workspaceDir string) (results []AssertionResult, verdict ScenarioVerdict, fc FailureClass) {
	// 1. expected_events (declaration order).
	for _, exp := range sf.ExpectedEvents {
		results = append(results, evalEventExpectation(exp, events))
	}

	// 2. expected_workspace (declaration order).
	for _, pred := range sf.ExpectedWorkspace {
		results = append(results, evalWorkspacePredicate(pred, workspaceDir))
	}

	// 3. expected_outcome (single entry if declared).
	if sf.ExpectedOutcome != nil {
		results = append(results, evalOutcomeExpectation(*sf.ExpectedOutcome, events))
	}

	// Determine verdict (SH-023: no short-circuit).
	for _, r := range results {
		if !r.Passed {
			return results, ScenarioVerdictFail, FailureClassAssertionFailed
		}
	}
	return results, ScenarioVerdictPass, ""
}
