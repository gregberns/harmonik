// Command harmonik-twin-session is a faithful "session twin" for the
// session-keeper subsystem (codename:session-keeper, hk-ekap1; this binary is
// bead hk-sav, Part B). It mimics, in-process and deterministically, the parts
// of a real interactive Claude Code session that the keeper observes and drives:
//
//   - It EMITS statusLine JSON on the stdin of keeper-statusline.sh
//     (scripts/keeper-statusline.sh) on a fixed interval, using the EXACT field
//     paths that script reads: .context_window.used_percentage,
//     .context_window.total_input_tokens, .context_window_size (or the nested
//     .context_window.context_window_size), .session_id and .model. The script
//     atomically writes <project>/.harmonik/keeper/<agent>.ctx (gauge.go).
//
//   - After each emit it execs keeper-stop-hook.sh (scripts/keeper-stop-hook.sh)
//     which touches <project>/.harmonik/keeper/<agent>.idle, marking the crisp
//     await-input boundary the keeper's CrispIdle gate looks for.
//
//   - It grows token usage by --growth each emit so the gauge crosses the
//     keeper's warn / act / force thresholds (cycle.go).
//
//   - It runs a stdin REPL — one injected command per line, exactly as the
//     keeper's injector delivers them (paste-buffer -d then a trailing Enter,
//     internal/keeper/injector.go): a /session-handoff line carries the verbatim
//     nonce <!-- KEEPER:<cycleID> --> (cycle.go:374-376) which the twin writes
//     into the HANDOFF file the keeper polls (HANDOFF-<agent>.md at the project
//     root, cycle.go:299-301); /clear resets tokens and rotates the session_id
//     to a fresh UUIDv4 (NOT v7 — the keeper rejects v7, keeper.go:148-150);
//     /session-resume holds the post-clear low state.
//
// The REPL is idempotent: the injector's 750ms settle + bounded retry Enters
// (injector.go) can double-deliver the same line, so each command is keyed and
// processed at most once.
//
// Real injection, real tmux and real Claude are NOT simulated here — only the
// file/stdin contracts the keeper depends on. The hermetic unit test in
// main_test.go exercises handleLine and buildStatusJSON purely in-process;
// agents B and C build the tmux/integration tests on top of this binary.
package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// config holds the parsed command-line flags.
type config struct {
	project     string
	agent       string
	statusline  string
	idleHook    string
	emitEvery   time.Duration
	growth      int64
	startTokens int64
	window      int64
	model       string

	// emitNA makes every statusLine carry a non-numeric used_percentage ("NA")
	// instead of a derived number, modeling the transient post-/clear statusLine
	// real Claude Code emits. keeper-statusline.sh's numeric guard then SKIPS the
	// .ctx write (scripts/keeper-statusline.sh line ~69), so the gauge file never
	// advances. Test-only knob; NO production behavior change.
	emitNA bool

	// suppressAfter, when > 0, stops the twin emitting statusLine JSON once this
	// much wall-clock has elapsed since the emitter started. The idle hook and
	// token growth keep running, so the session stays alive while its gauge .ctx
	// goes STALE — the input the keeper's gauge-liveness / force-restart paths
	// need. 0 (the default) never suppresses. Test-only knob; NO production change.
	suppressAfter time.Duration

	// resumeStatuslineOnClear, when true, LIFTS an active --suppress-statusline-after
	// suppression the moment a /clear is processed, so the post-clear session
	// resumes emitting statusLine JSON. This models the operator's REAL recovery
	// path: the gauge froze on a live, high-context agent (stale gauge + live
	// pane), the keeper drove the reset cycle, and the fresh post-/clear session
	// is healthy again — its statusLine resumes and the gauge re-appears with the
	// rotated session_id. Without this knob a suppressed gauge would never show
	// the post-/clear session_id, so the keeper could not rebind .managed to it.
	// Test-only knob; NO production behavior change. Refs: hk-nlio (operator
	// real-env validation gate).
	resumeStatuslineOnClear bool
}

// twinState is the mutable state shared between the emitter goroutine and the
// stdin REPL goroutine. All access MUST hold mu (emitter reads tokens/sessionID
// while the REPL mutates them on /clear).
type twinState struct {
	mu        sync.Mutex
	tokens    int64
	sessionID string

	// Immutable-after-construction config the JSON builder needs.
	window      int64
	model       string
	startTokens int64
	// emitNA forces a non-numeric used_percentage in every emit (see config).
	emitNA bool

	// handoffPath is the file the keeper polls for the nonce — the twin writes
	// the verbatim nonce line here on a /session-handoff. Project root,
	// HANDOFF-<agent>.md (cycle.go:299-301).
	handoffPath string

	// seen dedupes /session-handoff lines by their nonce so a redelivered
	// handoff does not rewrite the file. (/clear dedupe is token-level, not via
	// this map — see handleLine.) Guarded by mu.
	seen map[string]bool

	// handoffArmed is set once a /session-handoff trigger has been observed and
	// stays set until its nonce is found (or another recognized command closes
	// it). It models the production directive's MULTI-LINE / bracketed-paste
	// shape: cycle.go emits "/session-handoff <path>\n\n...verbatim: <nonce>",
	// so the nonce arrives on a LATER line. A real Claude REPL ingests the whole
	// bracketed paste as ONE prompt (paste-buffer's embedded '\n' bytes are not
	// dispatched as key events — see internal/daemon/pasteinject.go:112-114), so
	// the trigger and the nonce belong to the same logical prompt even though the
	// twin's line-by-line stdin scan delivers them as separate lines. While
	// armed, each subsequent line is scanned for the nonce marker. Guarded by mu.
	handoffArmed bool
}

// nonceRe matches the verbatim keeper nonce comment, e.g.
// "<!-- KEEPER:cyc-20260612T010203-000001 -->". Mirrors nonceMarker in
// internal/keeper/cycle.go (the literal it produces via fmt.Sprintf).
var nonceRe = regexp.MustCompile(`<!-- KEEPER:[^>]*-->`)

func run(args []string) int {
	cfg, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "harmonik-twin-session:", err)
		return 2
	}

	st := newState(cfg)

	// suppressDeadline is the wall-clock instant after which statusLine emits
	// stop (--suppress-statusline-after). Zero means never. Computed once and
	// shared by BOTH the emitter goroutine and the REPL re-emit so the gauge
	// .ctx goes stale consistently across both paths. The idle hook and token
	// growth keep running, so the session stays alive while its gauge ages out.
	var suppressDeadline time.Time
	if cfg.suppressAfter > 0 {
		suppressDeadline = time.Now().Add(cfg.suppressAfter)
	}
	// resumed is flipped to true by the REPL goroutine when a /clear is processed
	// under --resume-statusline-on-clear; it lifts the suppression deadline so the
	// post-clear session resumes emitting. An atomic.Bool keeps suppressDeadline
	// itself read-only (no data race with the emitter goroutine) while the pure
	// statuslineSuppressed helper stays untouched.
	var resumed atomic.Bool
	emit := func(j []byte) {
		if statuslineSuppressed(suppressDeadline, time.Now()) && !resumed.Load() {
			return
		}
		runStatusline(cfg, j)
	}

	// Emitter goroutine: pipe statusLine JSON to the script + fire the idle hook
	// on a fixed cadence, growing tokens so the gauge crosses keeper thresholds.
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(cfg.emitEvery)
		defer ticker.Stop()
		for range ticker.C {
			j := st.buildStatusJSON()
			emit(j)
			runIdleHook(cfg)
			st.grow(cfg.growth)
		}
	}()

	// Stdin REPL: one injected command per line, idempotent.
	sc := bufio.NewScanner(os.Stdin)
	// Allow long /session-handoff directives.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if changed := st.handleLine(line); changed {
			// changed==true only for a /clear (the sole state-mutating command:
			// it resets tokens and rotates the session_id). Under
			// --resume-statusline-on-clear, lift the suppression so the fresh
			// post-clear session resumes emitting — modeling a healthy agent
			// recovering after the keeper's reset cycle (the gauge re-appears with
			// the rotated session_id so the keeper can rebind .managed to it).
			if cfg.resumeStatuslineOnClear {
				resumed.Store(true)
			}
			// Re-emit immediately so a /clear's new session_id / reset tokens
			// reach the gauge without waiting a full tick, then mark idle. Honors
			// the suppression deadline (unless lifted above) so a suppressed gauge
			// otherwise stays stale even across a /clear.
			emit(st.buildStatusJSON())
		}
		// Every handled line is an await-input boundary.
		runIdleHook(cfg)
	}
	return 0
}

func parseFlags(args []string) (config, error) {
	fs := flag.NewFlagSet("harmonik-twin-session", flag.ContinueOnError)
	var cfg config
	fs.StringVar(&cfg.project, "project", "", "absolute project root (HARMONIK_PROJECT for the scripts)")
	fs.StringVar(&cfg.agent, "agent", "default", "agent name (HARMONIK_AGENT / namespaces the .ctx + .idle files)")
	fs.StringVar(&cfg.statusline, "statusline", "", "path to keeper-statusline.sh")
	fs.StringVar(&cfg.idleHook, "idle-hook", "", "path to keeper-stop-hook.sh")
	fs.DurationVar(&cfg.emitEvery, "emit-interval", 200*time.Millisecond, "interval between statusLine emits")
	fs.Int64Var(&cfg.growth, "growth", 20000, "tokens added per emit (crosses warn/act/force)")
	fs.Int64Var(&cfg.startTokens, "start-tokens", 10000, "initial token count (and post-/clear reset value)")
	fs.Int64Var(&cfg.window, "window", 0, "context window size; 0 omits context_window_size from the JSON (the [1m] quirk)")
	fs.StringVar(&cfg.model, "model", "claude-opus-4-8 [1m]", "model id reported in the statusLine JSON")
	fs.BoolVar(&cfg.emitNA, "emit-na", false, "emit a non-numeric used_percentage (\"NA\") so the statusLine script skips the .ctx write (models the post-/clear NA statusLine)")
	fs.DurationVar(&cfg.suppressAfter, "suppress-statusline-after", 0, "stop emitting statusLine JSON after this much elapsed time so the gauge .ctx goes stale; 0 never suppresses (idle hook + growth keep running)")
	fs.BoolVar(&cfg.resumeStatuslineOnClear, "resume-statusline-on-clear", false, "lift --suppress-statusline-after on the first /clear so the post-clear session resumes emitting (models the operator's stale-gauge-then-recover path)")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if cfg.project == "" {
		return cfg, fmt.Errorf("--project is required")
	}
	if cfg.statusline == "" {
		return cfg, fmt.Errorf("--statusline is required")
	}
	if cfg.idleHook == "" {
		return cfg, fmt.Errorf("--idle-hook is required")
	}
	return cfg, nil
}

// newState builds the initial twin state, minting a starting UUIDv4 session_id.
func newState(cfg config) *twinState {
	return &twinState{
		tokens:      cfg.startTokens,
		sessionID:   newUUIDv4(),
		window:      cfg.window,
		model:       cfg.model,
		startTokens: cfg.startTokens,
		emitNA:      cfg.emitNA,
		handoffPath: handoffFilePath(cfg.project, cfg.agent),
		seen:        make(map[string]bool),
	}
}

// handoffFilePath mirrors defaultHandoffFilePath in internal/keeper/cycle.go:
// <projectDir>/HANDOFF-<agentName>.md at the project root.
func handoffFilePath(projectDir, agent string) string {
	return filepath.Join(projectDir, fmt.Sprintf("HANDOFF-%s.md", agent))
}

// grow adds delta tokens under the lock.
func (s *twinState) grow(delta int64) {
	s.mu.Lock()
	s.tokens += delta
	s.mu.Unlock()
}

// statusSnapshot is the minimal state buildStatusJSON needs, taken under lock.
type statusSnapshot struct {
	tokens    int64
	sessionID string
	window    int64
	model     string
	emitNA    bool
}

// statusJSON is the shape marshaled to the statusLine script's stdin. The field
// paths MUST match what scripts/keeper-statusline.sh reads:
//
//	.context_window.used_percentage   (gate input; numeric)
//	.context_window.total_input_tokens
//	.context_window_size              (top-level; omitted when window==0)
//	.context_window.context_window_size (nested fallback; omitted when window==0)
//	.session_id
//	.model
type statusJSON struct {
	ContextWindow contextWindow `json:"context_window"`
	// Pointer so it can be omitted entirely (not emitted as null/0) when
	// window==0, reproducing the [1m] models that omit context_window_size.
	ContextWindowSize *int64 `json:"context_window_size,omitempty"`
	SessionID         string `json:"session_id"`
	Model             string `json:"model"`
}

type contextWindow struct {
	// Raw so the builder can emit EITHER a derived number (the normal path) or
	// the non-numeric literal "NA" (--emit-na) under the same struct/field path.
	UsedPercentage   json.RawMessage `json:"used_percentage"`
	TotalInputTokens int64           `json:"total_input_tokens"`
	// Nested fallback path the script also checks
	// (.context_window.context_window_size). Omitted when window==0.
	ContextWindowSize *int64 `json:"context_window_size,omitempty"`
}

// buildStatusJSON marshals the current state into the statusLine JSON the script
// consumes. When window==0 BOTH context_window_size paths are omitted, so the
// script falls back to its [1m]-model / HARMONIK_KEEPER_WINDOW_SIZE inference.
func (s *twinState) buildStatusJSON() []byte {
	s.mu.Lock()
	snap := statusSnapshot{
		tokens:    s.tokens,
		sessionID: s.sessionID,
		window:    s.window,
		model:     s.model,
		emitNA:    s.emitNA,
	}
	s.mu.Unlock()
	return marshalStatusJSON(snap)
}

// marshalStatusJSON is the pure builder (no shared state) so it is trivially
// testable. used_percentage is derived from tokens/window when window>0 (mirrors
// how real Claude reports it); when window==0 the script ignores the window and
// gates on pct alone, so we report 0 (the keeper's pct fallback uses the absolute
// pct field, which the script copies through verbatim).
func marshalStatusJSON(snap statusSnapshot) []byte {
	// used_percentage is either the non-numeric literal "NA" (--emit-na, models
	// the post-/clear statusLine the script's numeric guard rejects) or a derived
	// number. Both travel through the SAME field path so downstream beads need no
	// second emit shape.
	var pctRaw json.RawMessage
	if snap.emitNA {
		pctRaw = json.RawMessage(`"NA"`)
	} else {
		pct := 0.0
		if snap.window > 0 {
			pct = 100.0 * float64(snap.tokens) / float64(snap.window)
		}
		// json.Marshal of a finite float never fails.
		pctRaw, _ = json.Marshal(pct) //nolint:errcheck
	}
	js := statusJSON{
		ContextWindow: contextWindow{
			UsedPercentage:   pctRaw,
			TotalInputTokens: snap.tokens,
		},
		SessionID: snap.sessionID,
		Model:     snap.model,
	}
	if snap.window > 0 {
		w := snap.window
		js.ContextWindowSize = &w
		js.ContextWindow.ContextWindowSize = &w
	}
	// json.Marshal never fails for this concrete, finite-field-only struct.
	out, _ := json.Marshal(js) //nolint:errcheck
	return out
}

// handleLine processes one injected REPL command. It returns true when the line
// mutated state (so the caller re-emits immediately). It is idempotent: a
// redelivered identical command (the injector's settle+retry can double-deliver)
// is a no-op. Blank lines are ignored.
//
// Multi-line /session-handoff: the production directive (cycle.go:553-556) is
// MULTI-LINE — "/session-handoff <path>\n\n...verbatim: <nonce>" — so the nonce
// lands on a LATER line. Real keeper.InjectText delivers it via tmux
// paste-buffer (bracketed paste), and a real Claude REPL ingests the whole paste
// as ONE prompt; but the twin's bufio.Scanner splits stdin on '\n', so the
// trigger and the nonce arrive as separate handleLine calls. To stay faithful,
// the twin arms on the trigger and scans subsequent lines for the nonce (the
// rest of the same paste). A nonce on the SAME line as the trigger still works.
func (s *twinState) handleLine(line string) bool {
	if isBlank(line) {
		// A blank line is part of a pasted handoff directive's body (cycle.go
		// emits a "\n\n" between the trigger and the IMPORTANT/nonce line), so it
		// must NOT disarm a pending handoff. It is otherwise ignored.
		return false
	}

	switch {
	case containsCmd(line, "/session-handoff"):
		// Arm for a possibly-multi-line directive, then try this same line for an
		// inline nonce (the single-line case main_test.go already covers).
		s.mu.Lock()
		s.handoffArmed = true
		s.mu.Unlock()
		return s.tryWriteNonce(line)

	case containsCmd(line, "/clear"):
		s.mu.Lock()
		// A /clear ends any pending handoff scan (a real REPL would have ingested
		// the handoff prompt by now; the keeper only injects /clear AFTER the
		// nonce confirms, so an armed-but-unconfirmed handoff is stale).
		s.handoffArmed = false
		// Idempotent: a /clear only fires on a session that has grown above
		// startTokens. The injector's settle+retry Enters can double-deliver the
		// same /clear; the second lands on the already-cleared (start-tokens)
		// session and is a no-op — no second rotation. This is faithful: the
		// keeper only /clears a high-context session, and a redelivered /clear
		// hits the freshly-low one.
		if s.tokens <= s.startTokens {
			s.mu.Unlock()
			return false
		}
		s.tokens = s.startTokens
		s.sessionID = newUUIDv4() // fresh, distinct, valid UUIDv4 (never v7).
		s.mu.Unlock()
		return true

	case containsCmd(line, "/session-resume"):
		// Resume ends any pending handoff scan and holds the current (post-clear,
		// low) state; nothing to mutate.
		s.mu.Lock()
		s.handoffArmed = false
		s.mu.Unlock()
		return false

	default:
		// A non-command line while a handoff is armed is a continuation of the
		// pasted directive (e.g. the "IMPORTANT: ...verbatim: <nonce>" line) —
		// scan it for the nonce. Otherwise it is unrelated prose; ignore.
		s.mu.Lock()
		armed := s.handoffArmed
		s.mu.Unlock()
		if armed {
			return s.tryWriteNonce(line)
		}
		return false
	}
}

// tryWriteNonce scans line for the keeper nonce marker and, if present and not
// already seen, writes it to the HANDOFF file the keeper polls and disarms the
// pending-handoff scan. It is the shared body for the inline (same-line) and the
// continuation-line (multi-line directive) paths. Returns false: writing the
// handoff nonce never changes tokens/session_id, so the caller need not re-emit.
func (s *twinState) tryWriteNonce(line string) bool {
	m := nonceRe.FindString(line)
	if m == "" {
		// No nonce on this line yet — stay armed for a later line of the paste.
		return false
	}
	s.mu.Lock()
	// The nonce arrived; the directive is complete — disarm.
	s.handoffArmed = false
	if s.seen[m] {
		s.mu.Unlock()
		return false // already wrote this nonce — idempotent.
	}
	s.seen[m] = true
	path := s.handoffPath
	s.mu.Unlock()
	// Write the verbatim nonce line into the HANDOFF file the keeper polls. A
	// real handoff appends a body too; only the nonce line is load-bearing for
	// the keeper's pollForNonce (strings.Contains).
	_ = writeHandoffNonce(path, m) //nolint:errcheck // best-effort; keeper poll surfaces failures
	return false                   // handoff does not change tokens/session_id.
}

// writeHandoffNonce writes the verbatim nonce line to the HANDOFF file. It
// overwrites (the keeper truncates the file before injecting, cycle.go step 2),
// so a single nonce line is the faithful minimum.
func writeHandoffNonce(path, nonce string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: matches .harmonik conventions
		return err
	}
	//nolint:gosec // G306: 0600 — keeper-owned handoff file
	return os.WriteFile(path, []byte(nonce+"\n"), 0o600)
}

// runStatusline pipes the statusLine JSON to keeper-statusline.sh with the env
// the scripts read (HARMONIK_PROJECT, HARMONIK_AGENT) plus the inherited
// environment so HARMONIK_KEEPER_WINDOW_SIZE passes through. Best-effort.
func runStatusline(cfg config, jsonLine []byte) {
	cmd := exec.Command(cfg.statusline) //nolint:gosec // G204: operator-supplied script path
	cmd.Stdin = bytes.NewReader(jsonLine)
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+cfg.project,
		"HARMONIK_AGENT="+cfg.agent,
	)
	_ = cmd.Run() //nolint:errcheck // best-effort emitter
}

// runIdleHook execs keeper-stop-hook.sh to touch the .idle marker. The stop hook
// now reads HARMONIK_AGENT first (same var as statusline), falling back to
// HARMONIK_KEEPER_AGENT for backward compat (hk-p9kw). Pass the agent positionally
// as belt-and-suspenders.
func runIdleHook(cfg config) {
	cmd := exec.Command(cfg.idleHook, cfg.agent) //nolint:gosec // G204: operator-supplied script path
	cmd.Env = append(os.Environ(),
		"HARMONIK_PROJECT="+cfg.project,
		"HARMONIK_AGENT="+cfg.agent,
	)
	_ = cmd.Run() //nolint:errcheck // best-effort idle marker
}

// newUUIDv4 mints a random RFC-4122 version-4 UUID. The version nibble (index 14
// of the canonical string) is forced to '4'.
func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])      //nolint:errcheck // crypto/rand on these platforms does not fail
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC-4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// statuslineSuppressed reports whether statusLine emits should be suppressed at
// now: true once now is at/after the deadline. A zero deadline (the default,
// --suppress-statusline-after unset/0) never suppresses. Pure so the gating
// logic is unit-testable without wall-clock or tmux.
func statuslineSuppressed(deadline, now time.Time) bool {
	return !deadline.IsZero() && !now.Before(deadline)
}

// isBlank reports whether the line is empty or whitespace-only.
func isBlank(line string) bool {
	return strings.TrimSpace(line) == ""
}

// containsCmd reports whether the injected line contains the given slash
// command. The injector delivers the command as raw pasted text, so a substring
// match mirrors how the real REPL would see it.
func containsCmd(line, cmd string) bool {
	return strings.Contains(line, cmd)
}
