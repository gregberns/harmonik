package daemon

import (
	"context"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

type w4cFixtureAdapter struct {
	mu sync.Mutex

	// newSessionParams records every NewSessionIn call (sessionCreator).
	newSessionParams []tmux.NewWindowIn
	// newWindowParams records every NewWindowIn call.
	newWindowParams []tmux.NewWindowIn
	// killedHandles records every KillWindow call.
	killedHandles []tmux.WindowHandle
}

func (a *w4cFixtureAdapter) ProbeTmux(context.Context) error                { return nil }
func (a *w4cFixtureAdapter) ListSessions(context.Context) ([]string, error) { return nil, nil }
func (a *w4cFixtureAdapter) ListWindows(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *w4cFixtureAdapter) NewWindowIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.newWindowParams = append(a.newWindowParams, params)
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

// NewSessionIn satisfies the daemon-local sessionCreator interface.
func (a *w4cFixtureAdapter) NewSessionIn(_ context.Context, params tmux.NewWindowIn) tmux.Outcome {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.newSessionParams = append(a.newSessionParams, params)
	return tmux.Outcome{Handle: tmux.WindowHandle(params.Session + ":" + params.WindowName)}
}

func (a *w4cFixtureAdapter) KillWindow(_ context.Context, h tmux.WindowHandle) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.killedHandles = append(a.killedHandles, h)
	return nil
}

func (a *w4cFixtureAdapter) WindowPanePID(context.Context, tmux.WindowHandle) (int, error) {
	return 0, tmux.ErrNoSession
}

func (a *w4cFixtureAdapter) WindowPaneID(context.Context, tmux.WindowHandle) (string, error) {
	return "", nil
}
func (a *w4cFixtureAdapter) KillSession(context.Context, string) error             { return nil }
func (a *w4cFixtureAdapter) LoadBuffer(context.Context, string, []byte) error      { return nil }
func (a *w4cFixtureAdapter) PasteBuffer(context.Context, string, string) error     { return nil }
func (a *w4cFixtureAdapter) SendKeysEnter(context.Context, string) error           { return nil }
func (a *w4cFixtureAdapter) SendKeysQuit(context.Context, string) error            { return nil }
func (a *w4cFixtureAdapter) SendKeysLiteral(context.Context, string, string) error { return nil }
func (a *w4cFixtureAdapter) WriteToPane(context.Context, string, string, []byte) error {
	return nil
}

func (a *w4cFixtureAdapter) killedCopy() []tmux.WindowHandle {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]tmux.WindowHandle, len(a.killedHandles))
	copy(out, a.killedHandles)
	return out
}

func w4cFixtureSubstrate(t *testing.T, adapter tmux.Adapter) *tmuxSubstrate {
	t.Helper()
	sub, ok := NewTmuxSubstrate(adapter, "w4c-session",
		WithCrewProjectHash(core.ProjectHash("abcdef012345"))).(*tmuxSubstrate)
	if !ok {
		t.Fatal("NewTmuxSubstrate did not return *tmuxSubstrate")
	}
	return sub
}

var w4cFixtureArgv = []string{
	"/usr/local/bin/claude",
	"--append-system-prompt",
	"You are a crew agent. Work the queue.",
	"--flag-with-'quote'",
}

// TestSpawnCrewSession_ArgvWithSpacesQuoted verifies that SpawnCrewSession
// shell-quotes each argv element before joining (hk-rpr6 regression: a bare
// strings.Join let `sh -c` shatter multi-word elements).
func TestSpawnCrewSession_ArgvWithSpacesQuoted(t *testing.T) {
	t.Parallel()

	adapter := &w4cFixtureAdapter{}
	sub := w4cFixtureSubstrate(t, adapter)

	if _, err := sub.SpawnCrewSession(context.Background(), "alpha", handler.SubstrateSpawn{
		Argv: w4cFixtureArgv,
	}); err != nil {
		t.Fatalf("SpawnCrewSession: %v", err)
	}

	if len(adapter.newSessionParams) == 0 {
		t.Fatal("no NewSessionIn call recorded")
	}
	got := adapter.newSessionParams[0].Command
	want := shellJoinArgv(w4cFixtureArgv)
	if got != want {
		t.Errorf("agent-window Command = %q, want shell-quoted %q", got, want)
	}
	w4cFixtureAssertShellRetokenizes(t, got, w4cFixtureArgv)
}

// TestSpawnRunSession_ArgvWithSpacesQuoted is the SpawnRunSession twin of the
// crew test above.
func TestSpawnRunSession_ArgvWithSpacesQuoted(t *testing.T) {
	t.Parallel()

	adapter := &w4cFixtureAdapter{}
	sub := w4cFixtureSubstrate(t, adapter)

	if _, err := sub.SpawnRunSession(context.Background(),
		"0f0e0d0c-0b0a-0908-0706-050403020100", handler.SubstrateSpawn{
			Argv: w4cFixtureArgv,
		}); err != nil {
		t.Fatalf("SpawnRunSession: %v", err)
	}

	if len(adapter.newSessionParams) == 0 {
		t.Fatal("no NewSessionIn call recorded")
	}
	got := adapter.newSessionParams[0].Command
	want := shellJoinArgv(w4cFixtureArgv)
	if got != want {
		t.Errorf("run-window Command = %q, want shell-quoted %q", got, want)
	}
	w4cFixtureAssertShellRetokenizes(t, got, w4cFixtureArgv)
}

func w4cFixtureAssertShellRetokenizes(t *testing.T, quotedCommand string, argv []string) {
	t.Helper()

	quotedBin := "'" + strings.ReplaceAll(argv[0], "'", `'\''`) + "'"
	if !strings.HasPrefix(quotedCommand, quotedBin) {
		t.Fatalf("quoted command %q does not start with quoted binary %q", quotedCommand, quotedBin)
	}
	script := `printf '%s\n' ` + quotedBin + strings.TrimPrefix(quotedCommand, quotedBin)

	out, err := exec.CommandContext(t.Context(), "/bin/sh", "-c", script).Output() //nolint:gosec // G204: script built from test-controlled quoted command, not user input
	if err != nil {
		t.Fatalf("sh -c re-tokenization: %v", err)
	}
	gotTokens := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(gotTokens) != len(argv) {
		t.Fatalf("sh -c re-tokenized to %d tokens %q, want %d (argv shattering!)",
			len(gotTokens), gotTokens, len(argv))
	}
	for i, tok := range gotTokens {
		if tok != argv[i] {
			t.Errorf("token[%d] = %q, want %q", i, tok, argv[i])
		}
	}
}

// TestKillAllWindows_RemoteWindowKilledViaRemoteAdapter verifies that a window
// spawned through a remote (worker-hosted) adapter is killed through that SAME
// adapter by KillAllWindows — not through the local adapter, which would leak
// the window on the worker's tmux server.
func TestKillAllWindows_RemoteWindowKilledViaRemoteAdapter(t *testing.T) {
	t.Parallel()

	localAdapter := &w4cFixtureAdapter{}
	remoteAdapter := &w4cFixtureAdapter{}
	sub := w4cFixtureSubstrate(t, localAdapter)

	ctx := context.Background()

	if _, err := sub.SpawnWindow(ctx, handler.SubstrateSpawn{
		WindowName: "local-win",
		Argv:       []string{"/bin/sh", "-c", "exit 0"},
	}); err != nil {
		t.Fatalf("SpawnWindow(local): %v", err)
	}

	if _, err := sub.spawnWindowVia(ctx, handler.SubstrateSpawn{
		WindowName: "remote-win",
		Argv:       []string{"/bin/sh", "-c", "exit 0"},
	}, remoteAdapter, "worker-session", true, nil); err != nil {
		t.Fatalf("spawnWindowVia(remote): %v", err)
	}

	if err := sub.KillAllWindows(ctx); err != nil {
		t.Fatalf("KillAllWindows: %v", err)
	}

	localKilled := localAdapter.killedCopy()
	remoteKilled := remoteAdapter.killedCopy()

	if len(localKilled) != 1 || !strings.Contains(string(localKilled[0]), "local-win") {
		t.Errorf("local adapter killed = %v, want exactly the local window", localKilled)
	}
	if len(remoteKilled) != 1 || !strings.Contains(string(remoteKilled[0]), "remote-win") {
		t.Errorf("remote adapter killed = %v, want exactly the remote window (remote windows leak when killed via the local adapter)", remoteKilled)
	}
}
