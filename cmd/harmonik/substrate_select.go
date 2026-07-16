package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/gregberns/harmonik/internal/codexdriver"
	"github.com/gregberns/harmonik/internal/handler"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/sessioncapture"
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

// Live-capture selection (AIS-013/AIS-014, m2-4-capture-tee design §2). Capture
// is OPT-IN and OFF by default: it engages only when HARMONIK_CAPTURE_DIR names
// a workspace root under which the corpus lands at
// ${dir}/.harmonik/sessions/${session_id}/ (WM §4.7). It applies only to the
// structured Codex driver — the tmux/Claude path has no raw child stdio to tee
// (design §0, AIS-011). AIS-INV-002: capture is NEVER load-bearing, so an open
// failure degrades to uncaptured and never blocks substrate selection.
const (
	captureDirEnv  = "HARMONIK_CAPTURE_DIR"
	captureKeepEnv = "HARMONIK_CAPTURE_KEEP"    // retention keep-N (optional; int)
	captureAgeEnv  = "HARMONIK_CAPTURE_MAX_AGE" // age-prune bound (optional; Go duration)
)

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
	opts, _ := codexSubstrateOptions(codexBinary)
	return codexdriver.NewCodexSubstrate(opts)
}

// codexSubstrateOptions builds the structured-driver Options and, when live
// capture is opted in (HARMONIK_CAPTURE_DIR), wires the sessioncapture corpus
// writers into Options.InCapture/OutCapture — the M2-4 production tee (AIS-013).
// Without this wiring the tee is INERT (the writers stay nil and apptap tees to
// nothing). It returns the *sessioncapture.Session so a caller MAY Close it;
// nil session means capture is disabled or could not be established.
//
// AIS-INV-002 (capture never aborts the run): a capture-open failure is logged
// once and swallowed — the driver is returned uncaptured, never an error.
func codexSubstrateOptions(codexBinary string) (codexdriver.Options, *sessioncapture.Session) {
	if codexBinary == "" {
		codexBinary = "codex"
	}
	opts := codexdriver.Options{
		Binary: codexBinary,
		Runner: ltmux.LocalRunner{}, // AIS-016: same runner seam as the tmux/remote path
		Clock:  substrate.SystemClock{},
	}
	sess := openCaptureSession()
	if sess != nil {
		opts.InCapture = sess.Input()
		opts.OutCapture = sess.Output()
	}
	return opts, sess
}

// openCaptureSession opens a live-capture corpus session when opted in, else
// returns nil. Off by default; failures degrade to uncaptured (AIS-INV-002).
func openCaptureSession() *sessioncapture.Session {
	dir := os.Getenv(captureDirEnv)
	if dir == "" {
		return nil // opt-in; capture off by default (design §2, AIS-014)
	}
	cfg := sessioncapture.Config{
		WorkspacePath: dir,
		// One corpus dir per composition-root substrate; the session id is
		// monotone-by-open-time so retention (keep-N by mtime) prunes oldest.
		SessionID: "codexdriver-" + time.Now().UTC().Format("20060102T150405.000000000"),
	}
	if n, err := strconv.Atoi(os.Getenv(captureKeepEnv)); err == nil && n > 0 {
		cfg.KeepN = n
	}
	if d, err := time.ParseDuration(os.Getenv(captureAgeEnv)); err == nil && d > 0 {
		cfg.MaxAge = d
	}
	sess, err := sessioncapture.Open(context.Background(), cfg)
	if err != nil {
		// AIS-INV-002: never load-bearing — log once, proceed uncaptured.
		log.Printf("harmonik: live session capture disabled (open failed): %v", err)
		return nil
	}
	return sess
}
