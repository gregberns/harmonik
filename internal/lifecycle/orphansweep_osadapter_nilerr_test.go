package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestOSTmuxSessionLister_NoServerIsEmpty(t *testing.T) {
	installListerTmuxFixture(t, "no server running on /tmp/tmux-test/default", 1)

	got, err := (OSTmuxSessionLister{}).ListTmuxSessions(context.Background())
	if err != nil || got != nil {
		t.Fatalf("no server: got sessions=%v err=%v, want nil,nil", got, err)
	}
}

func TestOSTmuxSessionLister_UnexpectedFailureIsReturned(t *testing.T) {
	installListerTmuxFixture(t, "permission denied", 1)

	got, err := (OSTmuxSessionLister{}).ListTmuxSessions(context.Background())
	if err == nil {
		t.Fatalf("unexpected failure: got sessions=%v err=nil, want error", got)
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("unexpected failure: error %q omits command output", err)
	}
}

func installListerTmuxFixture(t *testing.T, output string, exitCode int) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' '" + output + "'\nexit " + strconv.Itoa(exitCode) + "\n"
	path := filepath.Join(dir, "tmux")
	//nolint:gosec // G306: executable test script, 0755 is intentional
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write tmux fixture: %v", err)
	}
	// Prepend rather than replace, matching the sibling fixture in
	// internal/lifecycle/tmux: a bare replacement leaves the script's own
	// `/bin/sh` lookup and any helper the test shells out to unresolvable.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
