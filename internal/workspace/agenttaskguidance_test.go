package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestSessionCompletionFollowsTheHarness verifies that the ## Session Completion
// section is written in the terms of the harness that will read it.
//
// The claude case is the older half and still load-bearing: claude is a REPL
// that outlives the work, its Stop hook fires on session exit, and without an
// explicit /quit the daemon's workloop waits at sess.Wait() forever (hk-cmybm).
// The one-shot case is the newer half: a pi agent that had already committed
// obeyed the claude instruction with `echo "/quit" | pbcopy`, outlived its
// budget, and was scored as a crash (hk-quit-instruction-not-portable-ms55w).
//
// The wiring that decides WHICH of the two a real pi or codex launch gets is
// proved separately, against the daemon's own launch-spec builder.
func TestSessionCompletionFollowsTheHarness(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		completion handlercontract.CompletionMode
		wantQuit   bool
	}{
		{"claude_repl", handlercontract.CompletionEventStreamThenQuit, true},
		{"one_shot", handlercontract.CompletionProcessExit, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workspacePath := t.TempDir()
			payload := AgentTaskPayload{
				BeadID:        "hk-abc11",
				Title:         "Task",
				Phase:         "implementer-initial",
				Iteration:     1,
				RunID:         "018e1234-0000-7000-8000-00000000000b",
				WorkspacePath: workspacePath,
				Body:          "Do the work.",
				Completion:    tc.completion,
			}
			if err := WriteAgentTask(workspacePath, payload); err != nil {
				t.Fatalf("WriteAgentTask: %v", err)
			}

			content := string(mustReadFile(t, AgentTaskPath(workspacePath)))
			if !strings.Contains(content, "## Session Completion") {
				t.Fatal("no ## Session Completion section")
			}
			if got := strings.Contains(content, "/quit"); got != tc.wantQuit {
				t.Errorf("/quit present = %v, want %v", got, tc.wantQuit)
			}
		})
	}
}

// TestAgentTaskGuidanceIsImplementerOnly verifies that the generated
// agent-task.md carries the Tests and Structure guidance for every implementer
// phase and for none of the reviewer's.
//
// The guidance is the instruction surface every dispatched implementer reads,
// so a regression here is silent: nothing fails to build, and the only symptom
// is agents behaving differently weeks later. The reviewer's quality sections
// live in review-target.md, so leaking implementer test guidance into a
// read-only reviewer is instruction noise, not a harmless extra.
//
// The assertions quote the load-bearing clauses rather than the headings: the
// prohibition on bead-named test files (whose absence produced 885 of them),
// the delete-a-bad-test-in-your-path default, and the commit-message contract
// the build gate enforces. The commit clauses belong to the implementer for the
// same reason the rest do — the reviewer writes no commit, so the whole shape
// of a commit message is noise in a read-only session.
func TestAgentTaskGuidanceIsImplementerOnly(t *testing.T) {
	t.Parallel()

	clauses := []string{
		"## Tests",
		"## Structure",
		"never after a bead or ticket ID",
		"the default is to delete it",
		"## Commit Message (a gate refuses any other shape)",
		"Do NOT use `git commit -m`",
		"NEVER write a verdict of APPROVE",
		"The commit-message gate never reads that line",
	}

	for _, tc := range []struct {
		phase string
		want  bool
	}{
		{"implementer-initial", true},
		{"implementer-resume", true},
		{"", true}, // single-mode dispatch
		{"reviewer", false},
	} {
		t.Run("phase="+tc.phase, func(t *testing.T) {
			t.Parallel()

			workspacePath := t.TempDir()
			payload := AgentTaskPayload{
				BeadID:        "hk-abc10",
				Title:         "Task",
				Phase:         tc.phase,
				Iteration:     2,
				RunID:         "018e1234-0000-7000-8000-00000000000a",
				WorkspacePath: workspacePath,
				Body:          "Do the work.",
			}

			if err := WriteAgentTask(workspacePath, payload); err != nil {
				t.Fatalf("WriteAgentTask phase=%q: %v", tc.phase, err)
			}

			content := string(mustReadFile(t, AgentTaskPath(workspacePath)))
			for _, clause := range clauses {
				if got := strings.Contains(content, clause); got != tc.want {
					t.Errorf("phase=%q: %q present = %v, want %v", tc.phase, clause, got, tc.want)
				}
			}
		})
	}
}

// TestCommitTemplatePassesTheCommitMessageGate lifts the commit-message
// template back out of the rendered agent-task.md and runs the repo's real
// scripts/validate-commit-msg.sh over it, character for character.
//
// The ## Commit Message section carries a comment promising its rules are the
// validator's rules. Nothing made that promise true until this test existed,
// and the template it describes shipped in a shape that FAILED the validator
// nine ways: it was indented two spaces, with a sentence after it saying not to
// copy the indent, while `^Reviewed-By:`, `^Review-Verdict:` and
// `^Trivial: true$` are all anchored at column zero in that script. A dispatched
// implementer that copies what it is given must land a passing commit, so the
// only honest check is to feed the given text to the gate that judges it.
//
// The `Refs:` line inside the template is deliberately NOT what this test
// proves: the validator never reads it. Nor is it the daemon's done-detection
// signal — that is HEAD advance (internal/daemon/dot_cascade_core.go). It ties
// the commit to the bead for later reconciliation, and it rides along in the
// template because one message has to satisfy both the gate and that tie.
//
// This test pins the TEMPLATE only, and the template is one sample: a 69-char
// `docs` subject with no trailing period. It therefore says nothing about
// whether the RULES the section states above the template are still the
// script's rules. TestRenderedCommitRulesMatchTheValidator covers that.
func TestCommitTemplatePassesTheCommitMessageGate(t *testing.T) {
	t.Parallel()

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	validateScript := filepath.Join(repoRoot, "scripts", "validate-commit-msg.sh")

	info, err := os.Stat(validateScript)
	if err != nil {
		t.Skipf("skipping: %s is absent (%v)", validateScript, err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Skipf("skipping: %s is not executable (mode %v)", validateScript, info.Mode().Perm())
	}

	workspacePath := t.TempDir()
	payload := AgentTaskPayload{
		BeadID:        "hk-abc10",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-00000000000c",
		WorkspacePath: workspacePath,
		Body:          "Do the work.",
	}
	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask: %v", err)
	}

	content := string(mustReadFile(t, AgentTaskPath(workspacePath)))
	template := extractCommitTemplate(t, content)

	msgPath := filepath.Join(t.TempDir(), "commit-msg")
	if err := os.WriteFile(msgPath, []byte(template), 0o600); err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	cmd := exec.CommandContext(t.Context(), "bash", validateScript, msgPath)
	cmd.Dir = repoRoot
	out, cmdErr := cmd.CombinedOutput()
	if cmdErr != nil {
		t.Errorf("validate-commit-msg.sh rejected the template the task file hands every implementer: %v\n--- script output ---\n%s--- template ---\n%s", cmdErr, out, template)
	}
}

// extractCommitTemplate returns the fenced block of the ## Commit Message
// section that holds the trailers — the one an implementer is told to copy.
//
// It copies nothing and strips nothing: the bytes handed to the validator are
// the bytes between the fence lines. That is the whole point of the test, so a
// helper that trimmed indentation here would prove the opposite of what is
// wanted. It fails the test when the section, the fences or the trailers are
// missing, so a rename cannot quietly turn this into a check of an empty string.
func extractCommitTemplate(t *testing.T, content string) string {
	t.Helper()

	const heading = "## Commit Message"
	idx := strings.Index(content, heading)
	if idx < 0 {
		t.Fatalf("no %q section in the rendered agent-task.md", heading)
	}

	var blocks []string
	var current []string
	inBlock := false
	for _, line := range strings.Split(content[idx:], "\n") {
		if line == "```" {
			if inBlock {
				blocks = append(blocks, strings.Join(current, "\n")+"\n")
				current = nil
			}
			inBlock = !inBlock
			continue
		}
		if inBlock {
			current = append(current, line)
		}
	}
	if inBlock {
		t.Fatal("unclosed ``` fence in the ## Commit Message section")
	}

	var found []string
	for _, block := range blocks {
		if strings.Contains(block, "Reviewed-By:") {
			found = append(found, block)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want exactly 1 fenced block carrying a Reviewed-By: trailer, got %d of %d fenced blocks", len(found), len(blocks))
	}
	return found[0]
}

// TestRenderedCommitRulesMatchTheValidator reads the authoritative commit
// rules back out of scripts/validate-commit-msg.sh and checks that the PROSE in
// the rendered agent-task.md states those same rules.
//
// TestCommitTemplatePassesTheCommitMessageGate proves only that ONE sample
// message — a 69-character `docs` subject with no trailing period — survives
// the gate. Add a tenth type to CC_PATTERN, drop `spec` from it, or raise the
// 72-character ceiling, and that test stays green while the sentences the
// implementer actually reads become false. The task file is the only statement
// of these rules a dispatched agent ever sees, so a stale sentence there costs
// a rebuild-and-retry loop against a rule the agent was never given.
//
// Every extraction below fails with t.Fatalf when its pattern no longer
// matches. A test that quietly finds nothing and passes would be worse than no
// test at all: it would carry the promise without keeping it, which is the
// exact failure this test exists to end.
func TestRenderedCommitRulesMatchTheValidator(t *testing.T) {
	t.Parallel()

	_, thisFile, _, _ := runtime.Caller(0)
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	validateScript := filepath.Join(repoRoot, "scripts", "validate-commit-msg.sh")

	scriptBytes, err := os.ReadFile(validateScript) //nolint:gosec // G304: path is derived from runtime.Caller, not from input.
	if err != nil {
		t.Skipf("skipping: %s is absent (%v)", validateScript, err)
	}
	script := string(scriptBytes)

	wantTypes := validatorTypeSet(t, script)
	wantCeiling := validatorSubjectCeiling(t, script)
	requireValidatorRefusesTrailingPeriod(t, script)

	workspacePath := t.TempDir()
	payload := AgentTaskPayload{
		BeadID:        "hk-abc10",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-00000000000d",
		WorkspacePath: workspacePath,
		Body:          "Do the work.",
	}
	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask: %v", err)
	}
	content := string(mustReadFile(t, AgentTaskPath(workspacePath)))

	// The type set: exactly the script's, no more and no fewer.
	gotCount, gotTypes := renderedTypeSet(t, content)
	if !equalStringSlices(gotTypes, wantTypes) {
		t.Errorf("rendered task file lists types %v; scripts/validate-commit-msg.sh CC_PATTERN allows %v", gotTypes, wantTypes)
	}
	if wantCount := countWord(t, len(wantTypes)); gotCount != wantCount {
		t.Errorf("rendered task file says %q words; the script allows %d types (%q)", gotCount, len(wantTypes), wantCount)
	}

	// The subject-length ceiling, and the one-over example that follows it.
	for _, want := range []string{
		fmt.Sprintf("The subject MUST be %d characters or fewer.", wantCeiling),
		fmt.Sprintf("A subject of %d characters is refused.", wantCeiling+1),
	} {
		if !strings.Contains(content, want) {
			t.Errorf("rendered task file does not state %q; the script's ceiling is %d", want, wantCeiling)
		}
	}

	// The trailing-period refusal. Only the script's HAVING the rule is pinned
	// (above) — the rule carries no number to drift, so the prose is checked
	// against a fixed sentence.
	if want := "The subject MUST NOT end with a period."; !strings.Contains(content, want) {
		t.Errorf("rendered task file does not state %q, but the script refuses a trailing period", want)
	}
}

// ccPatternRE lifts the alternation group out of the validator's CC_PATTERN
// assignment — the closed set of Conventional Commits types.
var ccPatternRE = regexp.MustCompile(`(?m)^CC_PATTERN='\^\(([a-z|]+)\)`)

// subjectCeilingRE lifts the number out of the validator's subject-length
// comparison, `(( SUBJECT_LEN > 72 ))`.
var subjectCeilingRE = regexp.MustCompile(`\(\(\s*SUBJECT_LEN\s*>\s*([0-9]+)\s*\)\)`)

// trailingPeriodRE matches the validator's trailing-period test,
// `grep -qE '\.$' <<<"$SUBJECT"`.
var trailingPeriodRE = regexp.MustCompile(`grep\s+-qE\s+'\\\.\$'\s*<<<\s*"\$SUBJECT"`)

// renderedTypesRE lifts the count word and the backticked type list out of the
// rendered task file's type sentence.
var renderedTypesRE = regexp.MustCompile("(?m)^The `<type>` MUST be one of these ([a-z]+) words: ([^\n]*?) —")

// backtickedWordRE matches one `word` token.
var backtickedWordRE = regexp.MustCompile("`([a-z]+)`")

// validatorTypeSet returns the sorted Conventional Commits types the validator
// accepts, read from its CC_PATTERN assignment.
func validatorTypeSet(t *testing.T, script string) []string {
	t.Helper()

	m := ccPatternRE.FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("cannot find the CC_PATTERN type alternation in scripts/validate-commit-msg.sh with %v — the script changed shape and this test can no longer read the authoritative type set; fix the pattern, do not delete the check", ccPatternRE)
	}
	types := strings.Split(m[1], "|")
	if len(types) < 2 {
		t.Fatalf("CC_PATTERN yielded %d type(s) (%q); that is not a type set — the pattern is matching the wrong thing", len(types), m[1])
	}
	sort.Strings(types)
	return types
}

// validatorSubjectCeiling returns the maximum subject length the validator
// accepts, read from its `(( SUBJECT_LEN > N ))` comparison.
func validatorSubjectCeiling(t *testing.T, script string) int {
	t.Helper()

	m := subjectCeilingRE.FindStringSubmatch(script)
	if m == nil {
		t.Fatalf("cannot find the SUBJECT_LEN ceiling comparison in scripts/validate-commit-msg.sh with %v — the script changed shape and this test can no longer read the authoritative ceiling; fix the pattern, do not delete the check", subjectCeilingRE)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		t.Fatalf("SUBJECT_LEN ceiling %q does not parse as a positive number: %v", m[1], err)
	}
	return n
}

// requireValidatorRefusesTrailingPeriod fails when the validator no longer
// carries a trailing-period refusal, so the prose sentence asserted against it
// cannot outlive the rule.
func requireValidatorRefusesTrailingPeriod(t *testing.T, script string) {
	t.Helper()

	if !trailingPeriodRE.MatchString(script) {
		t.Fatalf("cannot find the trailing-period refusal in scripts/validate-commit-msg.sh with %v — either the rule is gone (delete the sentence from the task file) or the script changed shape (fix the pattern)", trailingPeriodRE)
	}
}

// renderedTypeSet returns the count word and the sorted list of types the
// rendered task file tells an implementer it may use.
func renderedTypeSet(t *testing.T, content string) (string, []string) {
	t.Helper()

	m := renderedTypesRE.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("cannot find the type sentence in the rendered agent-task.md with %v — the sentence was reworded; update the pattern so this check keeps reading the real prose", renderedTypesRE)
	}
	var types []string
	for _, tok := range backtickedWordRE.FindAllStringSubmatch(m[2], -1) {
		types = append(types, tok[1])
	}
	if len(types) == 0 {
		t.Fatalf("the type sentence %q lists no backticked types", m[0])
	}
	sort.Strings(types)
	return m[1], types
}

// countWord spells n for comparison against the rendered prose, which writes
// the count as a word ("nine") rather than a digit.
func countWord(t *testing.T, n int) string {
	t.Helper()

	words := []string{
		"zero", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve",
	}
	if n < 0 || n >= len(words) {
		t.Fatalf("the validator allows %d types and this test has no word for that count; extend countWord", n)
	}
	return words[n]
}

// equalStringSlices reports whether two sorted slices hold the same elements.
func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
