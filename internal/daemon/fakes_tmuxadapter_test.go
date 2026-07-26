package daemon

// fakes_tmuxadapter_test.go — the noopTmuxAdapter tmux.Adapter test fake.
//
// Split out of export_test.go (RT19.8, P2 E5 export_test.go split). The type,
// its 14 no-op methods, and the compile-time `var _ tmuxPkg.Adapter` assertion
// always change together, so they ship as one whole file. Same package (daemon),
// so the runWait ctx-cancel test seam and any other daemon_test caller resolve
// the fake byte-identically after the move.
//
// Bead: hk-ecrxy.

import (
	"context"

	tmuxPkg "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// noopTmuxAdapter is a minimal tmux.Adapter stub that satisfies the interface
// for the runWait test seam. Only WindowPanePID is reachable from runWait, and
// only in the pid==0 slow-path; ExportedRunWaitWithDeadFn always sets pid>0.
type noopTmuxAdapter struct{}

func (n *noopTmuxAdapter) ProbeTmux(_ context.Context) error                { return nil }
func (n *noopTmuxAdapter) ListSessions(_ context.Context) ([]string, error) { return nil, nil }
func (n *noopTmuxAdapter) ListWindows(_ context.Context, _ string) ([]string, error) {
	return nil, nil
}

func (n *noopTmuxAdapter) NewWindowIn(_ context.Context, _ tmuxPkg.NewWindowIn) tmuxPkg.Outcome {
	return tmuxPkg.Outcome{}
}
func (n *noopTmuxAdapter) KillWindow(_ context.Context, _ tmuxPkg.WindowHandle) error { return nil }
func (n *noopTmuxAdapter) WindowPanePID(_ context.Context, _ tmuxPkg.WindowHandle) (int, error) {
	return 0, nil
}

func (n *noopTmuxAdapter) WindowPaneID(_ context.Context, _ tmuxPkg.WindowHandle) (string, error) {
	return "", nil
}
func (n *noopTmuxAdapter) KillSession(_ context.Context, _ string) error              { return nil }
func (n *noopTmuxAdapter) LoadBuffer(_ context.Context, _ string, _ []byte) error     { return nil }
func (n *noopTmuxAdapter) PasteBuffer(_ context.Context, _, _ string) error           { return nil }
func (n *noopTmuxAdapter) SendKeysLiteral(_ context.Context, _, _ string) error       { return nil }
func (n *noopTmuxAdapter) SendKeysEnter(_ context.Context, _ string) error            { return nil }
func (n *noopTmuxAdapter) SendKeysQuit(_ context.Context, _ string) error             { return nil }
func (n *noopTmuxAdapter) WriteToPane(_ context.Context, _, _ string, _ []byte) error { return nil }

// Compile-time assertion: noopTmuxAdapter implements tmux.Adapter.
var _ tmuxPkg.Adapter = (*noopTmuxAdapter)(nil)
