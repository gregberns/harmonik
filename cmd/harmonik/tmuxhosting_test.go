package main

// tmuxhosting_test.go — unit tests for resolveTmuxHosting / reportNoTmuxHosting
// / tmuxSubstrateSelected.
//
// These are hermetic: the tmux binary is stubbed through tmux.RecordingRunner,
// so they assert the boot CONTRACT (which tmux verbs run, what the resolved
// hosting is, whether a missing tmux is fatal) rather than the behaviour of a
// real tmux server. The end-to-end "daemon actually boots without $TMUX" proof
// lives in main_test.go.
//
// Helper prefix: tmuxHostingFixture (per implementer-protocol.md §Helper-prefix
// discipline).

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// tmuxHostingFixtureRunner returns a RecordingRunner that stubs the tmux binary:
// `tmux -V` prints a supported version, and every other verb succeeds silently.
// When failVerb is non-empty, the tmux subcommand of that name exits non-zero.
func tmuxHostingFixtureRunner(failVerb string) *tmux.RecordingRunner {
	rr := &tmux.RecordingRunner{}
	rr.CmdFunc = func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		verb := ""
		if len(args) > 0 {
			verb = args[0]
		}
		if verb == failVerb {
			return exec.CommandContext(ctx, "false")
		}
		if verb == "-V" {
			return exec.CommandContext(ctx, "printf", "tmux 3.4\n")
		}
		return exec.CommandContext(ctx, "true")
	}
	return rr
}

// tmuxHostingFixtureCalledVerbs returns the tmux subcommand of every recorded
// call, in call order.
func tmuxHostingFixtureCalledVerbs(rr *tmux.RecordingRunner) []string {
	var verbs []string
	for _, c := range rr.Calls {
		if len(c.Args) > 0 {
			verbs = append(verbs, c.Args[0])
		}
	}
	return verbs
}

// TestResolveTmuxHosting_NoAmbientClient_CreatesDaemonOwnedSession is the core
// of the 2026-07-28 change: with $TMUX unset and a working tmux, hosting
// RESOLVES (it used to be a hard refusal to boot). The daemon takes the
// deterministic per-project session, creates it, and owns its keepalive.
//
// It also pins the reason display-message must NOT be consulted here: without
// an ambient client that verb answers with whatever session the tmux server
// most recently made current — possibly a crew or captain session — which would
// scatter implementer windows into sessions the daemon does not own.
//
// Not parallel: mutates $TMUX.
func TestResolveTmuxHosting_NoAmbientClient_CreatesDaemonOwnedSession(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)

	projectDir := t.TempDir()
	rr := tmuxHostingFixtureRunner("")
	var out bytes.Buffer

	got := resolveTmuxHosting(context.Background(), projectDir, tmux.OSAdapter{}.WithRunner(rr), &out)

	if !got.Available {
		t.Fatalf("resolveTmuxHosting with $TMUX unset: Available = false (Err = %v); want true — the daemon must boot without an ambient tmux client", got.Err)
	}
	if got.Ambient {
		t.Errorf("Ambient = true; want false ($TMUX was unset)")
	}
	if want := tmux.DefaultSessionName(projectDir); got.SessionName != want {
		t.Errorf("SessionName = %q; want the deterministic per-project session %q", got.SessionName, want)
	}
	if !got.NeedKeepalive {
		t.Error("NeedKeepalive = false; want true — the daemon created the session so it must keep it alive")
	}
	verbs := tmuxHostingFixtureCalledVerbs(rr)
	for _, v := range verbs {
		if v == "display-message" {
			t.Errorf("resolveTmuxHosting consulted `tmux display-message` with no ambient client (verbs: %v); without a client it reports the server's most-recently-current session, which may be a crew/captain session", verbs)
		}
	}
	if !strings.Contains(out.String(), "tmux attach -t "+got.SessionName) {
		t.Errorf("boot notice %q does not tell the operator how to inspect live agents (want a `tmux attach -t %s` directive)", out.String(), got.SessionName)
	}
}

// TestResolveTmuxHosting_TmuxUnusable_ReportsUnavailable verifies that a failing
// tmux probe resolves to "no hosting" rather than being swallowed — and that no
// session name leaks out, since NewTmuxSubstrate panics on an empty one and the
// caller must not construct it at all.
//
// Not parallel: mutates $TMUX.
func TestResolveTmuxHosting_TmuxUnusable_ReportsUnavailable(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)

	var out bytes.Buffer
	got := resolveTmuxHosting(context.Background(), t.TempDir(), tmux.OSAdapter{}.WithRunner(tmuxHostingFixtureRunner("-V")), &out)

	if got.Available {
		t.Fatal("Available = true after a failing `tmux -V` probe; want false")
	}
	if got.Err == nil {
		t.Error("Err = nil with Available = false; the reason must be reportable to the operator")
	}
	if got.SessionName != "" {
		t.Errorf("SessionName = %q with no tmux hosting; want empty", got.SessionName)
	}
}

// TestResolveTmuxHosting_EnsureSessionFails_ReportsUnavailable verifies that a
// usable tmux binary which nonetheless cannot create the daemon's session is
// still "no hosting" — the probe passing is not by itself proof of a spawn target.
//
// Not parallel: mutates $TMUX.
func TestResolveTmuxHosting_EnsureSessionFails_ReportsUnavailable(t *testing.T) {
	mainFixtureSaveRestoreEnv(t, "TMUX", "", true /* unset */)

	var out bytes.Buffer
	got := resolveTmuxHosting(context.Background(), t.TempDir(), tmux.OSAdapter{}.WithRunner(tmuxHostingFixtureRunner("new-session")), &out)

	if got.Available {
		t.Fatal("Available = true when the daemon's session could not be created; want false")
	}
	if got.Err == nil || !strings.Contains(got.Err.Error(), "cannot ensure daemon tmux session") {
		t.Errorf("Err = %v; want it to name the session-creation failure", got.Err)
	}
}

// TestReportNoTmuxHosting_TmuxSubstrateIsFatal verifies the honest-degradation
// boundary: with the tmux substrate wired, every agent spawn routes through
// `tmux new-window`, so booting without tmux would be a lie. The daemon must
// refuse AND name both escape hatches.
func TestReportNoTmuxHosting_TmuxSubstrateIsFatal(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	code := reportNoTmuxHosting(tmuxHosting{Err: tmux.ErrTmuxMissing}, true /* tmuxSubstrate */, &out)

	if code == 0 {
		t.Error("reportNoTmuxHosting with the tmux substrate selected returned 0; want non-zero — the daemon would fail on the first dispatch")
	}
	for _, want := range []string{"FATAL", "install tmux", substrateSelectEnv + "=codexdriver"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("fatal notice does not mention %q; got:\n%s", want, out.String())
		}
	}
}

// TestReportNoTmuxHosting_CodexDriverDegradesLoudly verifies the other side: the
// structured Codex driver owns child stdio and never shells out to tmux, so
// dispatch genuinely works. The daemon continues — but must say at boot exactly
// which capabilities it has lost, rather than discovering them later.
func TestReportNoTmuxHosting_CodexDriverDegradesLoudly(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	code := reportNoTmuxHosting(tmuxHosting{Err: tmux.ErrTmuxMissing}, false /* tmuxSubstrate */, &out)

	if code != 0 {
		t.Errorf("reportNoTmuxHosting on the codexdriver path returned %d; want 0 — dispatch works without tmux there", code)
	}
	// "NO SUPERVISOR" is the highest-value line in the banner: without tmux there
	// is no flywheel session, so `harmonik supervise start` cannot run and the
	// daemon has no auto-revive. Pin it explicitly so it cannot silently regress.
	for _, want := range []string{
		"WARNING",
		"DEGRADED",
		"NO SUPERVISOR",
		"no auto-revive",
		"no attachable agent panes",
		"harmonik start crew",
		"it is off by default", // the capture-tee is opt-in, not a standing fallback
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("degradation notice does not mention %q; got:\n%s", want, out.String())
		}
	}
}

// TestTmuxSubstrateSelected verifies the single predicate for the AIS-015 axis
// that both selectSubstrate and the boot-time fatality decision now share.
//
// Not parallel: mutates HARMONIK_SUBSTRATE.
func TestTmuxSubstrateSelected(t *testing.T) {
	tests := []struct {
		name  string
		unset bool
		val   string
		want  bool
	}{
		{name: "unset defaults to tmux", unset: true, want: true},
		{name: "empty defaults to tmux", val: "", want: true},
		{name: "unrecognised value defaults to tmux", val: "banana", want: true},
		{name: "explicit codexdriver opts out", val: "codexdriver", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mainFixtureSaveRestoreEnv(t, substrateSelectEnv, tc.val, tc.unset)
			if got := tmuxSubstrateSelected(); got != tc.want {
				t.Errorf("tmuxSubstrateSelected() = %v; want %v", got, tc.want)
			}
		})
	}
}
