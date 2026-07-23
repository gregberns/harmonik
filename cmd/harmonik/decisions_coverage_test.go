package main

// decisions_coverage_test.go — pure-logic coverage for the `harmonik decisions`
// command cluster (decisions.go + decisions_k4.go). These tests exercise the
// paths that need NO live daemon: path resolution, terminal printing, row
// rendering, every usage/help block, and the flag-parsing / arg-validation /
// socket-absent (exit 17) branches of each verb.
//
// Deliberately NOT covered here (need a live daemon socket, a real presence
// projection over a running bus, or a blocking subscribe stream):
//   - decisionsBlockedWait / decisionsArmSubscribe (open a live subscribe stream)
//   - decisionsDialOp beyond its dial-failure branch (needs a responding daemon)
//   - runDecisionsListOrShowParsed past the dial (needs a daemon to return rows)
// Those are integration-level and out of scope for this pure-logic pass.
//
// Reuses captureStateStdout (state_cmd_coverage_test.go) and the dx9* helpers
// (decisions_hkxz9_test.go). New helpers use the "dcov" prefix.

import (
	"path/filepath"
	"strings"
	"testing"
)

// dcovRC runs fn (a subcommand invocation) while suppressing its stdout, and
// returns both the exit code and the captured stdout. Stderr is left alone
// (matches the existing raise/withdraw tests).
func dcovRC(t *testing.T, fn func() int) (int, string) {
	t.Helper()
	var rc int
	out := captureStateStdout(t, func() { rc = fn() })
	return rc, out
}

// ----------------------------------------------------------------------------
// decisionsResolvePaths — pure path resolution
// ----------------------------------------------------------------------------

func TestDecisionsResolvePaths(t *testing.T) {
	tests := []struct {
		name        string
		projectFlag string
		socketFlag  string
		wantAbs     string
		wantSock    string
	}{
		{
			name:        "explicit absolute project, default socket",
			projectFlag: "/abs/proj",
			socketFlag:  "",
			wantAbs:     "/abs/proj",
			wantSock:    filepath.Join("/abs/proj", ".harmonik", "daemon.sock"),
		},
		{
			name:        "explicit project and explicit socket override",
			projectFlag: "/abs/proj",
			socketFlag:  "/custom/daemon.sock",
			wantAbs:     "/abs/proj",
			wantSock:    "/custom/daemon.sock",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			abs, sock, rc := decisionsResolvePaths(tc.projectFlag, tc.socketFlag, "list")
			if rc != 0 {
				t.Fatalf("rc = %d, want 0", rc)
			}
			if abs != tc.wantAbs {
				t.Errorf("absProject = %q, want %q", abs, tc.wantAbs)
			}
			if sock != tc.wantSock {
				t.Errorf("sockPath = %q, want %q", sock, tc.wantSock)
			}
		})
	}
}

func TestDecisionsResolvePaths_DefaultsToCwd(t *testing.T) {
	// Empty project flag → cwd (absolute, non-empty), socket under it.
	abs, sock, rc := decisionsResolvePaths("", "", "list")
	if rc != 0 {
		t.Fatalf("rc = %d, want 0", rc)
	}
	if abs == "" || !filepath.IsAbs(abs) {
		t.Errorf("absProject = %q, want a non-empty absolute path", abs)
	}
	wantSock := filepath.Join(abs, ".harmonik", "daemon.sock")
	if sock != wantSock {
		t.Errorf("sockPath = %q, want %q", sock, wantSock)
	}
}

// ----------------------------------------------------------------------------
// decisionsPrintTerminal — pure output of a resolved/withdrawn terminal
// ----------------------------------------------------------------------------

func TestDecisionsPrintTerminal(t *testing.T) {
	tests := []struct {
		name string
		term decisionTerminal
		want string
	}{
		{"resolved prints chosen option", decisionTerminal{Resolved: true, ChosenOption: "ship"}, "ship\n"},
		{"withdrawn prints reason", decisionTerminal{Resolved: false, Reason: "self_obsoleted"}, "withdrawn: self_obsoleted\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var rc int
			out := captureStateStdout(t, func() { rc = decisionsPrintTerminal(tc.term) })
			if rc != 0 {
				t.Errorf("rc = %d, want 0", rc)
			}
			if out != tc.want {
				t.Errorf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// renderDecisionRows — the human "what-needs-me" queue renderer
// ----------------------------------------------------------------------------

func TestRenderDecisionRows_Empty(t *testing.T) {
	out := captureStateStdout(t, func() { renderDecisionRows(nil) })
	if out != "No open decisions.\n" {
		t.Errorf("empty render = %q, want %q", out, "No open decisions.\n")
	}
}

func TestRenderDecisionRows_Fields(t *testing.T) {
	rows := []decisionListRow{
		{
			decisionListItem: decisionListItem{
				DecisionID:   "dec-1",
				Question:     "Ship v2?",
				Options:      []string{"ship", "hold"},
				BlockedAgent: "alice",
				ContextLink:  "hk-aaa",
				Urgency:      "blocker",
			},
			OrphanedPending: true,
		},
		{
			// Empty blocked/context render as "-", no urgency, not orphaned.
			decisionListItem: decisionListItem{
				DecisionID: "dec-2",
				Question:   "Pick region",
				Options:    []string{"us", "eu"},
			},
			OrphanedPending: false,
		},
	}
	out := captureStateStdout(t, func() { renderDecisionRows(rows) })

	// Row 1: fully populated, orphaned + urgency decorations present.
	if !strings.Contains(out, "Ship v2? · ship|hold · alice · hk-aaa · dec-1  [blocker]  [orphaned-pending]") {
		t.Errorf("row 1 render wrong; full output:\n%s", out)
	}
	// Row 2: empty blocked_agent and context_link collapse to "-", no decorations.
	if !strings.Contains(out, "Pick region · us|eu · - · - · dec-2\n") {
		t.Errorf("row 2 render wrong (expected '-' placeholders, no decorations); full output:\n%s", out)
	}
	// Row 2 must NOT carry an orphaned/urgency tag.
	if strings.Contains(out, "dec-2  [") {
		t.Errorf("row 2 should have no urgency/orphaned decoration; full output:\n%s", out)
	}
}

// ----------------------------------------------------------------------------
// Usage / help blocks — every usage function prints non-empty guidance.
// ----------------------------------------------------------------------------

func TestDecisionsUsageFunctions(t *testing.T) {
	tests := []struct {
		name    string
		fn      func()
		mustHas string
	}{
		{"decisionsUsage", decisionsUsage, "agent→human decision surface"},
		{"decisionsRaiseUsage", decisionsRaiseUsage, "emit a decision_needed event"},
		{"decisionsWaitUsage", decisionsWaitUsage, "block until a decision's terminal"},
		{"decisionsWithdrawUsage", decisionsWithdrawUsage, "cancel your own open decision"},
		{"decisionsListUsage", decisionsListUsage, "what-needs-me"},
		{"decisionsShowUsage", decisionsShowUsage, "show one open decision by id"},
		{"decisionsAnswerUsage", decisionsAnswerUsage, "resolve an open decision"},
		{"mailboxUsage", mailboxUsage, "operator mailbox"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := captureStateStdout(t, tc.fn)
			if !strings.Contains(out, tc.mustHas) {
				t.Errorf("%s output missing %q; got:\n%s", tc.name, tc.mustHas, out)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// raise — flag-parsing branches not already covered (help, unknown flag,
// unexpected positional, invalid urgency). All return before any dial.
// ----------------------------------------------------------------------------

func TestDecisionsRaise_FlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
		{"unexpected positional", []string{"stray"}, 1},
		{"invalid urgency", []string{"--question", "Q?", "--option", "a", "--urgency", "bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsRaiseSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("raise %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// wait — help + unknown flag.
// ----------------------------------------------------------------------------

func TestDecisionsWait_FlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsWaitSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("wait %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// withdraw — help + unknown flag (id/reason validation already in hkxz9 test).
// ----------------------------------------------------------------------------

func TestDecisionsWithdraw_FlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsWithdrawSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("withdraw %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// list — help, unknown flag, unexpected positional (all pre-dial), plus the
// socket-absent exit 17 once parsing passes.
// ----------------------------------------------------------------------------

func TestDecisionsList_FlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
		{"unexpected positional", []string{"stray"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsListSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("list %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

func TestDecisionsList_MissingDaemonExit17(t *testing.T) {
	dir := t.TempDir()
	rc, _ := dcovRC(t, func() int { return runDecisionsListSubcommand([]string{"--project", dir}) })
	if rc != 17 {
		t.Errorf("list with no daemon: rc = %d, want 17", rc)
	}
}

// ----------------------------------------------------------------------------
// show — help, no id, two ids, unknown flag, and exit 17 with a valid single id.
// ----------------------------------------------------------------------------

func TestDecisionsShow_ArgValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"no id", []string{}, 1},
		{"two ids", []string{"id1", "id2"}, 1},
		{"unknown flag", []string{"--bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsShowSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("show %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

func TestDecisionsShow_MissingDaemonExit17(t *testing.T) {
	dir := t.TempDir()
	rc, _ := dcovRC(t, func() int {
		return runDecisionsShowSubcommand([]string{dx9D1, "--project", dir})
	})
	if rc != 17 {
		t.Errorf("show with no daemon: rc = %d, want 17", rc)
	}
}

// ----------------------------------------------------------------------------
// answer — help, wrong arg count, unknown flag, and exit 17 with valid args.
// ----------------------------------------------------------------------------

func TestDecisionsAnswer_ArgValidation(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"no args", []string{}, 1},
		{"one arg (needs two)", []string{"id-only"}, 1},
		{"three args", []string{"id", "opt", "extra"}, 1},
		{"unknown flag", []string{"--bogus"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runDecisionsAnswerSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("answer %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

func TestDecisionsAnswer_MissingDaemonExit17(t *testing.T) {
	dir := t.TempDir()
	// Valid two positionals + a resolver default → parsing passes, dial fails → 17.
	rc, _ := dcovRC(t, func() int {
		return runDecisionsAnswerSubcommand([]string{dx9D1, "ship", "--project", dir})
	})
	if rc != 17 {
		t.Errorf("answer with no daemon: rc = %d, want 17", rc)
	}
}

// ----------------------------------------------------------------------------
// mailbox — help, unknown flag, unexpected positional, and exit 17.
// ----------------------------------------------------------------------------

func TestMailbox_FlagParsing(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, 0},
		{"unknown flag", []string{"--bogus"}, 1},
		{"unexpected positional", []string{"stray"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if rc, _ := dcovRC(t, func() int { return runMailboxSubcommand(tc.args) }); rc != tc.want {
				t.Errorf("mailbox %v: rc = %d, want %d", tc.args, rc, tc.want)
			}
		})
	}
}

func TestMailbox_MissingDaemonExit17(t *testing.T) {
	dir := t.TempDir()
	rc, _ := dcovRC(t, func() int { return runMailboxSubcommand([]string{"--project", dir}) })
	if rc != 17 {
		t.Errorf("mailbox with no daemon: rc = %d, want 17", rc)
	}
}
