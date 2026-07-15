package main

import (
	"os"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/substrate"
)

// substrateSelectEnv is the composition-root substrate-selection axis
// (AIS-015): tmux hosting by default; the structured Codex app-server driver
// (internal/codexdriver) by explicit opt-in only. Selection is by which value
// is WIRED into daemon.Config.Substrate here at the root — never a runtime
// test-branch inside a driver (RS-017), and the driver itself is blind to this
// axis (twin-blindness: L2/L3 doubles substitute at the wire).
//
// Value "codexdriver" selects the structured driver. Anything else (including
// unset) keeps the tmux substrate — the safe pre-bake default.
const substrateSelectEnv = "HARMONIK_SUBSTRATE"

// selectSubstrate applies the AIS-015 selection axis: it returns tmuxSub
// unless HARMONIK_SUBSTRATE=codexdriver explicitly opts in to the structured
// Codex driver. codexBinary is the codex executable (--codex-binary flag /
// default) used when a LaunchSpec supplies no argv.
//
// The spawn seam stays remote-capable (AIS-016): the driver takes the same
// CommandRunner shape as the tmux path — tmux.LocalRunner here; an SSH runner
// substitutes when M4 rebuilds the remote transport.
func selectSubstrate(tmuxSub handler.Substrate, codexBinary string) handler.Substrate {
	if os.Getenv(substrateSelectEnv) != "codexdriver" {
		return tmuxSub
	}
	if codexBinary == "" {
		codexBinary = "codex"
	}
	return codexdriver.NewCodexSubstrate(codexdriver.Options{
		Binary: codexBinary,
		Runner: ltmux.LocalRunner{}, // AIS-016: same runner seam as the tmux/remote path
		Clock:  substrate.SystemClock{},
	})
}
