package keeper

// injector_buffername_hky466l_test.go — hk-y466l site 1.
//
// The keeper injector shells out to tmux directly through the tmuxRunFn seam
// instead of going through tmux.OSAdapter, so the PL-021d buffer-name validator
// never runs on its name. That is exactly why the retired hardcoded literal
// "hk-keeper-inject" survived: it FAILS tmux.bufferNameRe (prefix "hk-", not
// "harmonik-"), and the moment this path is consolidated onto the adapter —
// the obvious future refactor — every keeper injection would start returning
// ErrStructural, silently dropping /session-handoff, /clear, and
// /session-resume.
//
// The name is now injectBufferName ("harmonik-keeper-inject"), which is the
// shape tmux.BufferName("keeper", "inject") produces.
//
// Package keeper is depguard-isolated and may NOT import
// internal/lifecycle/tmux (hk-ekap1 / hk-fzzc6), so the REAL bufferNameRe is
// unreachable from here. These tests deliberately do NOT restate it — a copied
// regex drifts from the original and then proves nothing, which is exactly what
// the bead's acceptance line warns against. The cross-check is instead split
// across the two packages:
//
//   - internal/lifecycle/tmux TestBufferName_KeeperInjectorDuplicate asserts the
//     shared helper produces the literal "harmonik-keeper-inject" and that the
//     REAL validator accepts it (and rejects the retired "hk-keeper-inject").
//   - the tests here assert the injector actually puts that literal on the tmux
//     argv.
//
// Change either side and the other fails.
//
// Package keeper (not keeper_test) so the tests can reach the tmuxRunFn seam via
// the shared installFakeTmux helper in injector_sequence_hkzole_test.go.

import (
	"context"
	"testing"
)

// TestInjectText_BufferNameIsValidShape drives a real InjectText against the
// fake tmux runner and checks the buffer name carried on both the load-buffer
// and paste-buffer argv. Validity of that name against the real bufferNameRe is
// asserted from the tmux package (TestBufferName_KeeperInjectorDuplicate); what
// this test owns is that the injector actually puts it on the argv.
func TestInjectText_BufferNameIsValidShape(t *testing.T) {
	f := installFakeTmux(t, nil)

	if err := InjectText(context.Background(), "sess:0.0", "hello\n"); err != nil {
		t.Fatalf("InjectText: unexpected error: %v", err)
	}

	seen := 0
	for _, c := range f.calls {
		if len(c.args) == 0 {
			continue
		}
		if c.args[0] != "load-buffer" && c.args[0] != "paste-buffer" {
			continue
		}
		name := bufferNameFromArgv(t, c.args)
		seen++
		if name != injectBufferName {
			t.Errorf("%s used buffer name %q; want injectBufferName %q — routing this "+
				"path through tmux.OSAdapter would return ErrStructural for anything "+
				"else (hk-y466l)", c.args[0], name, injectBufferName)
		}
	}
	if seen != 2 {
		t.Fatalf("inspected %d buffer-named calls; want 2 (load-buffer + paste-buffer). calls=%v",
			seen, f.subcommands())
	}
}

// TestInjectBufferName_NotTheRetiredLiteral pins the constant itself, so a
// revert to the "hk-" prefix is caught even if the argv plumbing changes.
func TestInjectBufferName_NotTheRetiredLiteral(t *testing.T) {
	t.Parallel()

	if injectBufferName == "hk-keeper-inject" {
		t.Fatal("injectBufferName is back to the retired hk- literal, which fails " +
			"tmux.bufferNameRe (hk-y466l)")
	}
	if want := "harmonik-keeper-inject"; injectBufferName != want {
		t.Errorf("injectBufferName = %q; want %q — must equal "+
			"tmux.BufferName(\"keeper\", \"inject\"), pinned from the other side by "+
			"TestBufferName_KeeperInjectorDuplicate", injectBufferName, want)
	}
}

// bufferNameFromArgv extracts the value following the -b flag in a tmux argv.
func bufferNameFromArgv(t *testing.T, args []string) string {
	t.Helper()
	for i, a := range args {
		if a == "-b" && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("no -b flag in tmux argv %v", args)
	return ""
}
