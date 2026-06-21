package agentlaunch

import (
	"context"
	"strings"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// WindowSpawner is the minimal tmux seam SpawnKeeperWindow needs: the ability to
// add a named window (running a command) to an EXISTING session. Both
// tmux.OSAdapter (the production CLI/daemon adapter) and the daemon's
// fake/test adapters satisfy it, and so do test doubles in this package — so the
// daemon's crew-spawn path and the CLI captain launcher share one keeper-window
// creator without either dragging in the other's deps.
type WindowSpawner interface {
	NewWindowIn(ctx context.Context, params ltmux.NewWindowIn) ltmux.Outcome
}

// SpawnKeeperWindow creates the sibling "keeper" window (ltmux.WindowKeeper) in
// an already-created agent session and launches the keeper in it with the argv
// built from opts.
//
// It is the single creator both the daemon crew-spawn path and the CLI captain
// launcher route through. Returns the NewWindowIn Outcome so callers can decide
// whether a failure is fatal (the captain wants the band armed) or best-effort
// (the daemon's crew start logs and continues — the agent window is already
// live).
//
// The keeper command is shell-quoted via ShellJoinArgv so a binary path or
// project dir containing spaces survives `tmux new-window`'s `sh -c`
// re-word-splitting.
func SpawnKeeperWindow(ctx context.Context, spawner WindowSpawner, opts KeeperWindowOpts) ltmux.Outcome {
	argv := KeeperWindowArgv(opts)
	params := ltmux.NewWindowIn{
		Session:    opts.Session,
		WindowName: ltmux.WindowKeeper,
		WorkDir:    opts.ProjectDir,
		Command:    ShellJoinArgv(argv),
	}
	return spawner.NewWindowIn(ctx, params)
}

// ShellJoinArgv single-quotes each argv element and joins with spaces so the
// command survives `tmux new-window`'s `sh -c` re-word-splitting. Mirrors the
// daemon's shellJoinArgv (which now delegates here): the keeper inject target
// "<session>:agent" contains no shell metacharacters, but the binary path,
// project dir, or respawn-cmd may contain spaces, so quote uniformly.
func ShellJoinArgv(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = shellQuoteArg(a)
	}
	return strings.Join(quoted, " ")
}

// shellQuoteArg single-quotes s so it survives `sh -c` word-splitting as a
// single token. Identical to the daemon's shellQuoteArg (hk-rpr6).
func shellQuoteArg(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
