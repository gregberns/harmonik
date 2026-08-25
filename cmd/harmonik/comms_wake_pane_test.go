package main

// Candidate ordering for the directed-comms wake (hk-vigk8).
//
// Pure projection: commsWakePaneCandidates derives strings and touches no tmux,
// so these tests fork nothing. The tmux fact they encode was measured on a real
// server, not recalled: `tmux paste-buffer -t 'session:window'` succeeds and
// lands on the window's ACTIVE pane, while `-t 'session:window.0'` exits 1 with
// "can't find pane: 0" on a server that sets pane-base-index 1. A crew registry
// handle is "session:window" with no pane component, so appending ".0" turns a
// correct target into one that can never resolve.

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/lifecycle"
)

func wakeCandidateIndex(candidates []string, want string) int {
	for i, got := range candidates {
		if got == want {
			return i
		}
	}
	return -1
}

func TestCommsWakePaneCandidates_HandleTargetsWindowBeforeAnyPaneGuess(t *testing.T) {
	t.Parallel()

	const handle = "hk-alpha:1"
	dir := t.TempDir()
	if err := crew.Write(dir, crew.Record{
		Name:      "alpha",
		SessionID: "sess-alpha",
		Queue:     "q-alpha",
		Handle:    handle,
	}); err != nil {
		t.Fatalf("crew.Write: %v", err)
	}

	got := commsWakePaneCandidates(dir, "alpha")

	// The bare handle must come first: tmux resolves "session:window" to that
	// window's active pane, which is right at any pane-base-index.
	if len(got) == 0 {
		t.Fatalf("no candidates derived for a registered agent; want the bare registry handle %q first", handle)
	}
	if got[0] != handle {
		t.Fatalf("first candidate = %q, want the bare registry handle %q; full list: %v", got[0], handle, got)
	}

	// No candidate before the bare handle may name a pane index. A pane guess
	// ahead of the handle is the defect: on this server pane 0 does not exist,
	// so the wake fails for every registered agent.
	for i, candidate := range got {
		if candidate == handle {
			break
		}
		if strings.HasPrefix(candidate, handle+".") {
			t.Errorf("candidate %d %q names an explicit pane index and precedes the bare handle %q; full list: %v",
				i, candidate, handle, got)
		}
	}

	// NO candidate names a pane index, anywhere in the list. A ".0" target is
	// reachable only after the bare handle has already failed, and a narrower
	// target cannot resolve where the wider one did not, so keeping one as a
	// late fallback would pin unreachable code (hk-vigk8).
	for i, candidate := range got {
		if strings.Contains(candidate, ".") && strings.HasPrefix(candidate, handle+".") {
			t.Errorf("candidate %d %q names an explicit pane index; no candidate should: %v", i, candidate, got)
		}
	}
	handleIdx := wakeCandidateIndex(got, handle)

	// The naming-convention candidates still follow, so an agent whose handle
	// is stale keeps its conventional routes.
	hash := lifecycle.ComputeProjectHash(resolveProjectPath(dir))
	wantCrew := lifecycle.TmuxSessionName(hash, "crew-alpha")
	wantBare := lifecycle.TmuxSessionName(hash, "alpha")
	crewIdx := wakeCandidateIndex(got, wantCrew)
	bareIdx := wakeCandidateIndex(got, wantBare)
	if crewIdx < 0 {
		t.Errorf("crew-convention candidate %q absent; got %v", wantCrew, got)
	}
	if bareIdx < 0 {
		t.Errorf("bare-convention candidate %q absent; got %v", wantBare, got)
	}
	if crewIdx >= 0 && crewIdx < handleIdx {
		t.Errorf("crew-convention candidate (idx=%d) must follow the registry handle (idx=%d); full list: %v", crewIdx, handleIdx, got)
	}
	if crewIdx >= 0 && bareIdx >= 0 && crewIdx >= bareIdx {
		t.Errorf("crew-convention candidate (idx=%d) must precede the bare-convention candidate (idx=%d); full list: %v", crewIdx, bareIdx, got)
	}
}

func TestCommsWakePaneCandidates_NoRegistryRecordKeepsBothConventions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() // no crew record for "paul"
	hash := lifecycle.ComputeProjectHash(resolveProjectPath(dir))

	got := commsWakePaneCandidates(dir, "paul")

	want := []string{
		lifecycle.TmuxSessionName(hash, "crew-paul"),
		lifecycle.TmuxSessionName(hash, "paul"),
	}
	if len(got) != len(want) {
		t.Fatalf("candidates = %v, want exactly the two conventions %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("candidate %d = %q, want %q; full list: %v", i, got[i], want[i], got)
		}
	}
	for _, candidate := range got {
		if strings.Contains(candidate, ".") {
			t.Errorf("candidate %q names an explicit pane index; a convention candidate must target the session so tmux picks the active pane", candidate)
		}
	}
}
