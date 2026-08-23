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

	// RunID is the run-scoped identifier from the event envelope (EV-001;
	// EM-013), decoded so the post-suite leak sensor (SH-INV-002) can learn
	// which run_ids were actually executed. Nil for non-run-scoped events.
	RunID *core.RunID `json:"run_id,omitempty"`
}

// RunIDsFromEvents returns the distinct run_ids observed across events, in
// first-seen order. It feeds CheckPostSuiteLeaks the ExecutedRunIDs set
// (SH-INV-002) from a scenario's captured event log.
func RunIDsFromEvents(events []RawEvent) []core.RunID {
	seen := make(map[string]bool, len(events))
	out := make([]core.RunID, 0, len(events))
	for _, ev := range events {
		if ev.RunID == nil {
			continue
		}
		key := ev.RunID.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, *ev.RunID)
	}
	return out
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
//
// There is no detector here for events the bus dropped. SH-024 carried one
// until 2026-08-05 and it could not fire: it looked for bus_overflow, and
// event-model.md EV-011a's shed path — the only thing that would emit it —
// is not built. See the RATIONALE under SH-024. If EV-011a is built, a
// shed event will pass every check above, because the log stays well-formed
// and merely holds less than it should; the detector has to come back with it.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-020, SH-024.
func ReadEventLog(logPath string) ([]RawEvent, error) {
	//nolint:gosec // G304: logPath is the daemon-produced event log path, not user input
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(data, []byte("\n"))
	toProcess := lines[:len(lines)-1]

	events := make([]RawEvent, 0, len(toProcess))
	for i, line := range toProcess {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev RawEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return nil, fmt.Errorf("asserteval: JSON parse error at line %d (mid-file corruption per SH-024): %w", i+1, err)
		}
		events = append(events, ev)
	}
	return events, nil
}

func tokenizePayloadPath(path string) []string {
	var result []string
	for _, part := range strings.Split(path, ".") {
		if part == "" {
			continue
		}
		rest := part
		for rest != "" {
			if strings.HasPrefix(rest, "[") {
				end := strings.Index(rest, "]")
				if end < 0 {
					result = append(result, rest)
					rest = ""
					break
				}
				result = append(result, rest[:end+1])
				rest = rest[end+1:]
				rest = strings.TrimPrefix(rest, ".")
			} else {
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

func jsonAsFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint8:
		return float64(n), true
	case uint16:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

func jsonValuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if af, aIsNum := jsonAsFloat(a); aIsNum {
		if bf, bIsNum := jsonAsFloat(b); bIsNum {
			return af == bf
		}
		return false
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

func eventMatchesExpectation(ev RawEvent, exp EventExpectation) bool {
	if ev.Type != string(exp.Type) {
		return false
	}
	return exp.PayloadMatch == nil || payloadMatchHolds(ev.Payload, exp.PayloadMatch)
}

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

func isFilePredicateKind(k WorkspacePredicateKind) bool {
	switch k {
	case WorkspacePredicateKindFileExists,
		WorkspacePredicateKindFileContentsEqual,
		WorkspacePredicateKindFileContentsMatch:
		return true
	case WorkspacePredicateKindGitRefAt,
		WorkspacePredicateKindCommitTrailerPresent:
		return false
	}
	return false // invalid values are rejected while loading the scenario.
}

func checkSymlinkSafety(targetPath, workspaceDir string) error {
	absWS, err := filepath.Abs(workspaceDir)
	if err != nil {
		return fmt.Errorf("workspace abs path: %w", err)
	}
	rootReal, err := filepath.EvalSymlinks(absWS)
	if err != nil {
		return fmt.Errorf("workspace resolution failed: %w", err)
	}
	rootReal = filepath.Clean(rootReal)

	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("target abs path: %w", err)
	}

	existing := absTarget
	var trailing []string
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return nil
		}
		trailing = append([]string{filepath.Base(existing)}, trailing...)
		existing = parent
	}

	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("symlink resolution failed: %w", err)
	}
	full := filepath.Clean(filepath.Join(append([]string{resolved}, trailing...)...))

	if full != rootReal && !strings.HasPrefix(full, rootReal+string(filepath.Separator)) {
		return fmt.Errorf("symlink traversal: %q resolves to %q outside workspace", targetPath, full)
	}
	return nil
}

func evalWorkspacePredicate(pred WorkspacePredicate, workspaceDir string) AssertionResult {
	ar := AssertionResult{
		AssertionKind: AssertionResultKindWorkspaceState,
		Description:   pred.Description,
		ExpectedValue: map[string]any{"kind": string(pred.Kind), "path": pred.Path, "expected": pred.Expected},
	}

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

		case WorkspacePredicateKindGitRefAt,
			WorkspacePredicateKindCommitTrailerPresent:
		}
		return ar
	}

	switch pred.Kind {
	case WorkspacePredicateKindGitRefAt:
		if !validScenarioGitRef(pred.Path) {
			ar.Passed = false
			ar.ActualValue = fmt.Sprintf("invalid git ref %q", pred.Path)
			return ar
		}
		// Resolve the ref at pred.Path to a SHA.
		//nolint:gosec // G204: ref is validated by validScenarioGitRef and workspaceDir passed symlink/root containment checks above.
		out, gitErr := exec.CommandContext(context.Background(), "git", "-C", workspaceDir, "rev-parse", "--verify", pred.Path).Output()
		if gitErr != nil {
			ar.Passed = false
			ar.ActualValue = fmt.Sprintf("git rev-parse %q: %v", pred.Path, gitErr)
			return ar
		}
		actualSHA := strings.TrimSpace(string(out))

		expected := *pred.Expected
		if !sha1Re.MatchString(expected) {
			if !validScenarioGitRef(expected) {
				ar.Passed = false
				ar.ActualValue = fmt.Sprintf("invalid expected git ref %q", expected)
				return ar
			}
			//nolint:gosec // G204: expected is validated by validScenarioGitRef and workspaceDir passed symlink/root containment checks above.
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
		if !validScenarioGitRef(pred.Path) {
			ar.Passed = false
			ar.ActualValue = fmt.Sprintf("invalid git ref %q", pred.Path)
			return ar
		}
		// Read the commit message at pred.Path (ref name).
		//nolint:gosec // G204: ref is validated by validScenarioGitRef and workspaceDir passed symlink/root containment checks above.
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

	case WorkspacePredicateKindFileExists,
		WorkspacePredicateKindFileContentsEqual,
		WorkspacePredicateKindFileContentsMatch:
	}
	return ar
}

func validScenarioGitRef(ref string) bool {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return false
	}
	for _, r := range ref {
		if !scenarioGitRefRuneValid(r) {
			return false
		}
	}
	return true
}

func scenarioGitRefRuneValid(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	default:
		return strings.ContainsRune("._/-", r)
	}
}

func commitMessageHasTrailer(message, key string) bool {
	for _, line := range strings.Split(message, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+":") || strings.HasPrefix(trimmed, key+" :") {
			return true
		}
	}
	return false
}

func evalOutcomeExpectation(exp OutcomeExpectation, events []RawEvent) AssertionResult {
	ar := AssertionResult{
		AssertionKind: AssertionResultKindExitCode,
		Description:   exp.Description,
		ExpectedValue: string(exp.OutcomeStatus),
	}

	var actual core.OutcomeStatus
	found := false

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
	for _, exp := range sf.ExpectedEvents {
		results = append(results, evalEventExpectation(exp, events))
	}

	for _, pred := range sf.ExpectedWorkspace {
		results = append(results, evalWorkspacePredicate(pred, workspaceDir))
	}

	if sf.ExpectedOutcome != nil {
		results = append(results, evalOutcomeExpectation(*sf.ExpectedOutcome, events))
	}

	for _, r := range results {
		if !r.Passed {
			return results, ScenarioVerdictFail, FailureClassAssertionFailed
		}
	}
	return results, ScenarioVerdictPass, ""
}
