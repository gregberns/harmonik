// inject_directive_shape_hkpgtt6_test.go — the injected /session-handoff
// directive and the post-/clear brief command must survive newline collapsing
// and must be self-describing. Beads: hk-pgtt6, hk-4tjyj.

package keeper_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestInjectedHandoffDirective_SurvivesNewlineCollapse is the hk-pgtt6
// regression.
//
// The directive used to be built as
// "/session-handoff <path>\n\nIMPORTANT: include exactly this line verbatim…".
// Claude Code collapses a pasted multi-line block into ONE slash-command
// argument, so the "\n\n" vanished entirely and the crew saw
// `HANDOFF-kynes.mdIMPORTANT: include exactly this line verbatim…` — the path
// fused onto the instruction into a single unusable token.
//
// The fix is a VISIBLE separator, not whitespace: a trailing space is eaten with
// the newlines. This test collapses every newline out of the injected text (what
// the REPL does) and then asserts the path and the instruction are still
// separated by a visible, non-alphanumeric token. It fails on the old shape,
// where the collapsed text contains ".mdIMPORTANT".
func TestInjectedHandoffDirective_SurvivesNewlineCollapse(t *testing.T) {
	t.Parallel()

	const (
		agent   = "directive-shape-agent"
		cycleID = "cyc-directive-001"
	)
	s1, s2 := reactiveSIDs()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managed string
	rs := newReactiveSession(s1, s2, true, true)

	cycler := newReactiveCycler(
		agent, t.TempDir(), cycleID, rs, em, jc, &managed,
		500*time.Millisecond, 300*time.Millisecond,
	)
	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{
		Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1,
	}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	texts := rs.snapshotInjected()
	if len(texts) == 0 {
		t.Fatal("nothing was injected")
	}
	directive := texts[0]

	if !strings.Contains(directive, "/session-handoff ") {
		t.Fatalf("inject[0] must contain \"/session-handoff \" (trigger + space); got %q", directive)
	}

	// What the REPL actually delivers: newlines gone.
	collapsed := strings.ReplaceAll(directive, "\n", "")

	// The handoff path is the token right after "/session-handoff ".
	rest := collapsed[strings.Index(collapsed, "/session-handoff ")+len("/session-handoff "):]
	pathEnd := strings.IndexByte(rest, ' ')
	if pathEnd < 0 {
		t.Fatalf("after collapsing newlines the handoff path has no terminator — "+
			"the path fused onto the instruction (hk-pgtt6). collapsed = %q", collapsed)
	}
	handoffPath := rest[:pathEnd]
	if !strings.HasSuffix(handoffPath, ".md") {
		t.Fatalf("collapsed handoff path = %q; want a bare .md path — anything appended to it "+
			"means the separator was eaten (hk-pgtt6). collapsed = %q", handoffPath, collapsed)
	}

	// A VISIBLE (non-alphanumeric, non-space) separator must sit between the path
	// and the instruction. Whitespace alone is not enough: it is what got eaten.
	between := rest[pathEnd:]
	instr := strings.Index(between, "IMPORTANT")
	if instr < 0 {
		t.Fatalf("collapsed directive lost the IMPORTANT instruction: %q", collapsed)
	}
	sep := strings.TrimSpace(between[:instr])
	if sep == "" {
		t.Errorf("no VISIBLE separator between the handoff path and the instruction — "+
			"whitespace alone is collapsed away (hk-pgtt6). collapsed = %q", collapsed)
	}

	// And the nonce must still be carried verbatim.
	if !strings.Contains(directive, "<!-- KEEPER:"+cycleID+" -->") {
		t.Errorf("directive lost the verbatim nonce marker; got %q", directive)
	}

	// hk-4tjyj Fix 3: the crew is told to Read before Write, because the target
	// file already exists and Claude's Write tool refuses an unread file.
	if !strings.Contains(directive, "Read it first") {
		t.Errorf("directive does not tell the crew to Read the handoff before writing it; got %q", directive)
	}
}

// TestInjectedBriefCmd_IsSelfDescribing pins the hk-4tjyj Fix 4 contract: the
// post-/clear reboot command must carry --agent and --project.
//
// The bare `harmonik agent brief --wake keeper-restart` resolved the agent from
// $HARMONIK_AGENT and the project from the pane process's CWD. Both are ambient:
// a crew whose shell had cd'd elsewhere (or whose env var was lost) looked for
// HANDOFF-<agent>.md under the wrong root and printed "(no handoff on record)"
// with no error at all — silent, and indistinguishable from a genuinely absent
// handoff. Without the explicit flags this test fails.
func TestInjectedBriefCmd_IsSelfDescribing(t *testing.T) {
	t.Parallel()

	const (
		agent   = "self-describing-agent"
		cycleID = "cyc-selfdesc-001"
	)
	s1, s2 := reactiveSIDs()
	project := t.TempDir()

	em := &keeper.RecordingEmitter{}
	jc := &journalCapture{}
	var managed string
	rs := newReactiveSession(s1, s2, true, true)

	cycler := newReactiveCycler(
		agent, project, cycleID, rs, em, jc, &managed,
		500*time.Millisecond, 300*time.Millisecond,
	)
	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{
		Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: s1,
	}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	var brief string
	for _, txt := range rs.snapshotInjected() {
		if strings.Contains(txt, "agent brief") {
			brief = txt
		}
	}
	if brief == "" {
		t.Fatal("no `agent brief` command was injected")
	}

	for _, want := range []string{
		"harmonik agent brief",
		"--wake keeper-restart",
		"--agent " + agent,
		"--project " + project,
	} {
		if !strings.Contains(brief, want) {
			t.Errorf("brief command missing %q — the reboot is not self-describing (hk-4tjyj).\ngot: %q", want, brief)
		}
	}
}
