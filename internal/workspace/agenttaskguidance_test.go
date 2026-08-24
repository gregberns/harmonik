package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/commitmsg"
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
// template back out of the rendered agent-task.md and runs the real gate rules
// over it, character for character.
//
// The ## Commit Message section carries a heading promising a gate refuses any
// other shape. Nothing made that promise true until this test existed, and the
// template it describes shipped in a shape that FAILED the validator nine ways:
// it was indented two spaces, with a sentence after it saying not to copy the
// indent, while `Reviewed-By:`, `Review-Verdict:` and `Trivial: true` are all
// anchored at column zero. A dispatched implementer that copies what it is
// given must land a passing commit, so the only honest check is to feed the
// given text to the gate that judges it.
//
// It calls internal/commitmsg directly. It used to shell out to
// scripts/validate-commit-msg.sh, which is deleted, and the change is a
// strengthening as well as a port: the shell version SKIPPED itself when the
// script was absent or not executable, so the one test proving the instructions
// we hand every agent pass the gate we run could go quiet and report a pass.
//
// KnownReviewers is pinned rather than read off the repository. The template
// records the honest no-reviewer form, so the reviewer set decides nothing here,
// and reading git would make this test's answer depend on the checkout.
//
// Cleanup is left at the zero value, which is what `git commit -F` gets — the
// spelling the template itself tells the implementer to use.
//
// The `Refs:` line inside the template is deliberately NOT what this test
// proves: the gate never reads it. Nor is it the daemon's done-detection signal
// — that is HEAD advance (internal/daemon/dot_cascade_core.go). It ties the
// commit to the bead for later reconciliation, and it rides along in the
// template because one message has to satisfy both the gate and that tie.
func TestCommitTemplatePassesTheCommitMessageGate(t *testing.T) {
	t.Parallel()

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

	problems := commitmsg.Validate(template, commitmsg.Options{
		KnownReviewers: []string{"agent-reviewer", "agent-config-reviewer"},
	})
	if len(problems) > 0 {
		t.Errorf("the commit-message gate refuses the template the task file hands every implementer:\n%s--- template ---\n%s",
			commitmsg.Render(problems), template)
	}
}

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

// TestCommitMessagePathIsWritableWhateverTmpdirTheRunHas defends the clause
// that tells an implementer WHERE to put its commit-message file.
//
// The section has to name a path, and every launch does not agree on what a
// temp directory is. Under the srt sandbox srt sets TMPDIR at the per-run
// scratch directory the profile grants. On three other live paths nothing sets
// it at all — a non-srt backend, a harness absent from sandbox.harnesses, and
// every remote run, where handler.go assigns cmd.Env = spec.Env outright and
// RemoteExecArgv rebuilds that same explicit slice over a non-login shell. On
// those, a bare "$TMPDIR/commit-msg.txt" expands to "/commit-msg.txt" and the
// agent is told to write at the filesystem root.
//
// So the claim is not "the section mentions TMPDIR". It is that the path the
// section prints, put through a real shell, lands somewhere writable whether or
// not the run has a TMPDIR. Both cases are executed here, because the failing
// one is the case a reader is least likely to picture.
//
// Bead: hk-sandbox-no-writable-tmpdir-7484h.
func TestCommitMessagePathIsWritableWhateverTmpdirTheRunHas(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	payload := AgentTaskPayload{
		BeadID:        "hk-abc12",
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
	pathExpr := extractCommitMessagePathExpr(t, string(mustReadFile(t, AgentTaskPath(workspacePath))))

	sandboxScratch := t.TempDir()
	for _, tc := range []struct {
		name   string
		tmpdir string
		setEnv bool
	}{
		{"srt_wrapped_run_has_a_tmpdir", sandboxScratch, true},
		// The gate declined to wrap, or the run is remote. Nothing in harmonik
		// sets TMPDIR, so the shell sees it unset. This is the case the first
		// version of this instruction got wrong.
		{"unwrapped_or_remote_run_has_no_tmpdir", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// #nosec G204 -- pathExpr is read out of this package's own rendered
			// task file, not from anything outside the test binary.
			cmd := exec.CommandContext(t.Context(), "sh", "-c", `printf %s "`+pathExpr+`"`)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			if tc.setEnv {
				cmd.Env = append(cmd.Env, "TMPDIR="+tc.tmpdir)
			}
			out, cmdErr := cmd.Output()
			if cmdErr != nil {
				t.Fatalf("expanding %q in a shell failed: %v", pathExpr, cmdErr)
			}
			got := string(out)

			if !filepath.IsAbs(got) {
				t.Fatalf("the task file tells the implementer to write %q, which expands to the relative path %q", pathExpr, got)
			}
			dir := filepath.Dir(got)
			if dir == string(filepath.Separator) {
				t.Fatalf("the task file tells the implementer to write %q, which expands to %q — a write at the FILESYSTEM ROOT. "+
					"Name a default (${TMPDIR:-/tmp}); a bare $TMPDIR is empty on every launch harmonik does not sandbox.", pathExpr, got)
			}

			probe := filepath.Join(dir, "harmonik-commit-msg-writable-probe-"+tc.name)
			if err := os.WriteFile(probe, []byte("probe"), 0o600); err != nil {
				t.Fatalf("the implementer is told to write its commit message to %q, and %q is not writable: %v", got, dir, err)
			}
			if err := os.Remove(probe); err != nil {
				t.Errorf("remove write probe %s: %v", probe, err)
			}
		})
	}
}

var commitMessagePathRE = regexp.MustCompile("git commit -F \"([^\"]+)\"")

func extractCommitMessagePathExpr(t *testing.T, content string) string {
	t.Helper()
	m := commitMessagePathRE.FindStringSubmatch(content)
	if m == nil {
		t.Fatalf("the rendered task file carries no quoted `git commit -F \"<path>\"` instruction — "+
			"either the commit section is gone or it stopped naming where the message file belongs.\n%s", content)
	}
	return m[1]
}

// TestEveryTempDirectoryTheTaskFileNamesCarriesADefault pins the SHAPE of every
// temp-directory reference the implementer reads, not just the one inside the
// `git commit -F` argument.
//
// TestCommitMessagePathIsWritableWhateverTmpdirTheRunHas above expands one
// expression and proves it lands somewhere writable. That is the strongest
// assertion available for one path, and it reaches exactly one of the three
// mentions the section prints: the section also names `${TMPDIR:-/tmp}` twice in
// prose, and both of those could be regressed to a bare `$TMPDIR` with the
// package still green.
//
// The regression matters because a bare `$TMPDIR` is EMPTY on every launch
// harmonik does not sandbox — a non-srt backend, a harness absent from
// sandbox.harnesses, and every remote run — so the prose then tells the agent
// that the filesystem root is where it may write. A previous review already
// caught this exact regression once, which is the reason to hold it with a test
// rather than with care.
//
// Bead: hk-sandbox-no-writable-tmpdir-7484h.
func TestEveryTempDirectoryTheTaskFileNamesCarriesADefault(t *testing.T) {
	t.Parallel()

	workspacePath := t.TempDir()
	payload := AgentTaskPayload{
		BeadID:        "hk-abc13",
		Title:         "Task",
		Phase:         "implementer-initial",
		Iteration:     1,
		RunID:         "018e1234-0000-7000-8000-00000000000e",
		WorkspacePath: workspacePath,
		Body:          "Do the work.",
	}
	if err := WriteAgentTask(workspacePath, payload); err != nil {
		t.Fatalf("WriteAgentTask: %v", err)
	}
	content := string(mustReadFile(t, AgentTaskPath(workspacePath)))

	const wantForm = "${TMPDIR:-/tmp}"
	found := tmpDirReferenceRE.FindAllString(content, -1)

	const wantAtLeast = 3
	if len(found) < wantAtLeast {
		t.Fatalf("the rendered task file names a temp directory %d time(s), want at least %d "+
			"(one in the `git commit -F` argument and two in the prose around it). "+
			"The section has stopped telling the implementer where it may write.\n%s", len(found), wantAtLeast, content)
	}

	if !strings.Contains(content, "`sudo` does not lift that refusal") {
		t.Errorf("the rendered task file no longer says that `sudo` does not lift a sandbox write refusal.\n"+
			"An implementer that does not read it does what the reported failure did: retries the refused "+
			"write with sudo, is refused again, and abandons work it had already finished "+
			"(hk-sandbox-no-writable-tmpdir-7484h).\n%s", content)
	}

	for _, ref := range found {
		if ref != wantForm {
			t.Errorf("the task file names the temp directory as %q, want %q.\n"+
				"A bare $TMPDIR is EMPTY on every launch harmonik does not sandbox — a non-srt backend, "+
				"a harness not in sandbox.harnesses, and every remote run — so this line tells the "+
				"implementer to write at the filesystem root.", ref, wantForm)
		}
	}
}

var tmpDirReferenceRE = regexp.MustCompile(`\$\{TMPDIR[^}]*\}|\$TMPDIR`)
