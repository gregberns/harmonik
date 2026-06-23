package daemon

// crewstart.go — C2 daemon-side crew-start / crew-stop handler.
//
// Implements the CrewHandler interface: collision-check, registry write,
// queue-ensure, session launch, paste-seed, keeper-attach inputs, and teardown.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1–§3.5, §7.
// Acceptance criteria: C2 AC-1, AC-3.
// Bead ref: hk-5tg5o.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/queue"
)

// crewKeeperEventBus is the minimal event-emission seam used by the crew keeper
// post-spawn probe. Satisfied by eventbus.EventBus. May be nil in tests that do
// not assert on event emission.
type crewKeeperEventBus interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// crewKeeperCommsBus is the minimal comms-emission seam used by the crew keeper
// post-spawn probe. Satisfied by eventbus.CommsMessageEmitter (busImpl). May be
// nil in tests that do not assert on keeper-alert comms.
type crewKeeperCommsBus interface {
	EmitAgentMessage(ctx context.Context, payload core.AgentMessagePayload) (core.EventID, error)
}

// keeperProbePollInterval is the interval between LiveKeeperPresent polls during
// the post-spawn liveness probe. 1s is fine for a startup check: short enough to
// confirm a live watcher quickly, long enough not to busy-spin.
const keeperProbePollInterval = time.Second

// CrewHandler is the interface the daemon registers to process crew-start and
// crew-stop socket ops.
//
// Registered in daemon.go like CommsSendHandler; dispatched from socket.go's op
// switch for "crew-start" and "crew-stop" ops.
//
// Spec ref: c2-spec.md §3.1 (daemon RPC rationale).
// Bead ref: hk-5tg5o.
type CrewHandler interface {
	// HandleCrewStart processes one crew-start payload. Returns JSON-encoded
	// CrewStartResult on success.
	HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)

	// HandleCrewStop processes one crew-stop payload. Returns JSON-encoded stop
	// confirmation on success.
	HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error)
}

// CrewStartRequest is the wire payload for a "crew-start" socket op.
//
// Spec ref: c2-spec.md §3.1.
type CrewStartRequest struct {
	// Name is the crew member identifier (charset [a-z0-9-], 1–64 chars).
	Name string `json:"name"`
	// Queue is the named queue the crew member is bound to.
	Queue string `json:"queue"`
	// MissionPath is the path to the handoff file the crew seeds its boot loop from.
	MissionPath string `json:"mission_path"`
}

// CrewStopRequest is the wire payload for a "crew-stop" socket op.
//
// Spec ref: c2-spec.md §3.5.
type CrewStopRequest struct {
	// Name is the crew member to stop.
	Name string `json:"name"`
	// PauseQueue, when true, halts dispatch on the crew's named queue after teardown.
	PauseQueue bool `json:"pause_queue,omitempty"`
}

// CrewStartResult is the SocketResponse.Result payload for a successful crew-start.
//
// Spec ref: c2-spec.md AC-1 (prints the minted session_id).
type CrewStartResult struct {
	// SessionID is the minted (or resumed) session UUID.
	SessionID string `json:"session_id"`
	// Name echoes the crew member name.
	Name string `json:"name"`
}

// windowHandleExposer is an optional interface a SubstrateSession may implement
// to expose its underlying tmux window handle string for crew registry recording.
//
// *tmuxSubstrateSession implements this (WindowHandle method in tmuxsubstrate.go).
// Test doubles may implement it to control the recorded handle value.
type windowHandleExposer interface {
	WindowHandle() string
}

// crewPaneStopper is an optional interface a Substrate may implement to stop a
// persistent crew pane by its window handle string (crew-stop path).
//
// *tmuxSubstrate implements this (StopWindowByHandle method in tmuxsubstrate.go).
// Test doubles may implement it to record stop calls without real tmux.
type crewPaneStopper interface {
	// StopWindowByHandle sends /quit to the pane (best-effort), waits a grace
	// period, then kills the window identified by handle.
	StopWindowByHandle(ctx context.Context, handle string) error
}

// crewHandlerImpl is the concrete implementation of CrewHandler.
type crewHandlerImpl struct {
	claudeBinary string
	projectDir   string
	rcPrefix     string                 // per-project --remote-control label prefix (hk-igpg); "" = bare label
	substrate    handler.Substrate      // spawns crew windows
	opPauseCtrl  OperatorControlHandler // for --pause-queue in crew-stop; may be nil

	// keeper probe fields (hk-qgfme): async post-spawn liveness check.
	// All fields are optional; nil = feature disabled (probe skipped).
	keeperCfg    KeeperConfig                        // FlockAcquireGrace drives the probe; zero = disabled
	eventBus     crewKeeperEventBus                  // for emitting session_keeper_watcher_dead; may be nil
	commsBus     crewKeeperCommsBus                  // for keeper-alert comms to operator; may be nil
	liveKeeperFn func(projectDir, agent string) bool // injectable for testing; nil = keeper.LiveKeeperPresent
}

// CrewHandlerOpt is a functional option for NewCrewHandler.
type CrewHandlerOpt func(*crewHandlerImpl)

// WithKeeperProbe configures the async post-spawn keeper liveness probe
// (hk-qgfme). The probe polls LiveKeeperPresent for up to
// keeperCfg.FlockAcquireGrace after SpawnCrewSession; if the keeper watcher
// flock is never held within that window, a session_keeper_watcher_dead event
// and a keeper-alert comms message are emitted to the operator. The crew agent
// is always kept live; this is warn-loud, never a hard-block.
//
// Either bus argument may be nil (disables that emission path). If
// keeperCfg.FlockAcquireGrace == 0 the probe is entirely disabled and no
// goroutine is launched.
func WithKeeperProbe(keeperCfg KeeperConfig, eventBus crewKeeperEventBus, commsBus crewKeeperCommsBus) CrewHandlerOpt {
	return func(h *crewHandlerImpl) {
		h.keeperCfg = keeperCfg
		h.eventBus = eventBus
		h.commsBus = commsBus
	}
}

// NewCrewHandler constructs a CrewHandler implementation.
//
// claudeBinary is the handler executable (empty resolves to "claude").
// projectDir is the harmonik project root directory.
// rcPrefix is the per-project Claude Code --remote-control label prefix
// (daemon.remote_control_prefix). Empty = bare label (today's behavior); a
// non-empty value yields "<prefix>-<name>" via JoinRemoteControlName. It is a
// COSMETIC label only — the crew's identity keys stay bare (hk-igpg).
// substrate is the tmux substrate for spawning crew sessions; may be nil in tests
// that don't exercise the actual spawn path.
// opPauseCtrl is the operator-pause controller used when --pause-queue is set;
// may be nil (crew-stop will skip the pause step).
// opts are optional CrewHandlerOpt functional options (e.g. WithKeeperProbe).
//
// Bead ref: hk-5tg5o, hk-igpg, hk-qgfme.
func NewCrewHandler(claudeBinary, projectDir, rcPrefix string, substrate handler.Substrate, opPauseCtrl OperatorControlHandler, opts ...CrewHandlerOpt) CrewHandler {
	h := &crewHandlerImpl{
		claudeBinary: claudeBinary,
		projectDir:   projectDir,
		rcPrefix:     rcPrefix,
		substrate:    substrate,
		opPauseCtrl:  opPauseCtrl,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// ─────────────────────────────────────────────────────────────────────────────
// HandleCrewStart
// ─────────────────────────────────────────────────────────────────────────────

// HandleCrewStart implements CrewHandler.HandleCrewStart.
//
// Ordering per c2-spec.md §7:
//  1. Check collision → mint session_id (or reuse for stale re-launch)
//  2. crew.Write registry record (before launch — id is minted a priori)
//  3. Ensure named queue (idempotent)
//  4. Build launch spec + spawn window via substrate
//  5. Paste mission kick-off line (best-effort)
//  6. Keeper-attach inputs: env vars set in LaunchSpec + .managed marker created
//  7. Update registry with window handle; return minted session_id
//
// On launch failure: crew.Remove rollback (queue created during step 3 is left
// as-is per spec §7 "empty queue is harmless and reused on retry").
func (h *crewHandlerImpl) HandleCrewStart(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req CrewStartRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Queue == "" {
		return nil, fmt.Errorf("queue is required")
	}

	// ── Step 1: collision check + resolve session_id ──
	sessionID, isResume, err := h.resolveSessionID(req.Name, req.Queue)
	if err != nil {
		return nil, err
	}

	// ── Step 2: write registry record before launch ──
	rec := crew.Record{
		Name:      req.Name,
		SessionID: sessionID,
		Queue:     req.Queue,
		StartedAt: time.Now().UTC(),
	}
	if writeErr := crew.Write(h.projectDir, rec); writeErr != nil {
		return nil, fmt.Errorf("write registry: %w", writeErr)
	}

	// ── Step 3: ensure named queue ──
	if qErr := h.ensureQueue(ctx, req.Queue); qErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback; primary error returned
		return nil, fmt.Errorf("ensure queue: %w", qErr)
	}

	// ── Step 4: build launch spec + spawn ──
	// Read the optional model: front-matter field from the mission handoff
	// (specs/crew-handoff-schema.md §3). Best-effort: a missing/unreadable mission
	// or absent field yields "" and the crew inherits the compiled default model.
	model := readMissionModel(req.MissionPath)
	lspec, buildErr := buildCrewLaunchSpec(crewLaunchCtx{
		claudeBinary: h.claudeBinary,
		name:         req.Name,
		rcPrefix:     h.rcPrefix,
		sessionID:    sessionID,
		projectDir:   h.projectDir,
		resume:       isResume,
		model:        model,
	})
	if buildErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
		return nil, fmt.Errorf("build launch spec: %w", buildErr)
	}

	var windowHandle string
	if h.substrate != nil {
		argv := append([]string{lspec.Binary}, lspec.Args...)
		// WindowName names the crew's claude pane window. The independent-session
		// path (SpawnCrewSession) hardcodes tmux.WindowAgent internally and also
		// adds a sibling tmux.WindowKeeper window; this value is consumed only by
		// the fallback SpawnWindow path, where the CONTRACT "agent" name keeps the
		// crew pane consistent across both paths (hk-rmy1, slice C).
		spawn := handler.SubstrateSpawn{
			WindowName: tmux.WindowAgent,
			Cwd:        lspec.WorkDir,
			Env:        lspec.Env,
			Argv:       argv,
		}

		if css, ok := h.substrate.(crewSessionSpawner); ok {
			// ── Independent-session path (hk-mmlqt) ──
			// Crew lives in its own tmux session so daemon SIGTERM / supervisor-revive
			// does not kill running crew windows. SpawnCrewSession creates the session
			// with TWO windows — "agent" (this crew claude) and "keeper" (the per-crew
			// session-keeper, "harmonik keeper --tmux <session>:agent"). Invariant I1:
			// a crew RESTART/re-task must respawn ONLY the "agent" window so the keeper
			// window survives — there is NO in-daemon crew-restart path here today (crew
			// restart is driven by the keeper itself / externally via crew stop+start),
			// so no agent-only respawn is implemented in this package; the keeper window
			// is the durable sibling that re-binds to a freshly respawned agent pane.
			var sess handler.SubstrateSession
			sess, err = css.SpawnCrewSession(ctx, req.Name, spawn)
			if err != nil {
				_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
				return nil, fmt.Errorf("spawn crew session: %w", err)
			}
			// ── Async keeper liveness probe (hk-qgfme) ──
			// Run off the synchronous RPC so the caller always gets a live agent
			// back immediately. If the keeper watcher fails to acquire its flock
			// within flock_acquire_grace, the goroutine emits an event + comms
			// alert. Probe is DISABLED when FlockAcquireGrace == 0 (not configured).
			if h.keeperCfg.FlockAcquireGrace > 0 {
				go h.probeKeeperLiveness(req.Name, h.keeperCfg.FlockAcquireGrace)
			}
			if wh, ok2 := sess.(windowHandleExposer); ok2 {
				windowHandle = wh.WindowHandle()
			}
			// ── Step 5: paste mission kick-off line (best-effort) ──
			if req.MissionPath != "" {
				h.pasteCrewMissionToSession(ctx, sess, sessionID, req.MissionPath)
			}
		} else {
			// ── Fallback: window inside the daemon's session ──
			// Used by test doubles that don't implement crewSessionSpawner.
			prs := newPerRunSubstrate(h.substrate, h.claudeBinary, nil)
			var sess handler.SubstrateSession
			if prs != nil {
				sess, err = prs.SpawnWindow(ctx, spawn)
			} else {
				sess, err = h.substrate.SpawnWindow(ctx, spawn)
			}
			if err != nil {
				_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
				return nil, fmt.Errorf("spawn window: %w", err)
			}
			if wh, ok2 := sess.(windowHandleExposer); ok2 {
				windowHandle = wh.WindowHandle()
			}
			// ── Step 5: paste mission kick-off line (best-effort) ──
			if prs != nil && req.MissionPath != "" {
				h.pasteCrewMission(ctx, prs, sessionID, req.MissionPath)
			}
		}
	}

	// ── Step 6a: .managed marker (keeper-attach input) ──
	//
	// Slice-C note (hk-rmy1): on the independent-session path the daemon now
	// launches the keeper IN-SESSION (the "keeper" window), so the .managed marker
	// is largely REDUNDANT for the in-session keeper — it no longer gates whether a
	// keeper attaches. It is RETAINED because external readers still depend on it:
	// keeper.IsManaged consults it (and the CLI crew keeper / crew-stop marker
	// cleanup, plus `keeper doctor`, key off it). Removing it is a separate cleanup,
	// not in scope for this topology change.
	if markerErr := createCrewManagedMarker(h.projectDir, req.Name); markerErr != nil {
		// Non-fatal: session is live; the in-session keeper window is already
		// launched independently of this marker.
		fmt.Fprintf(os.Stderr, "daemon: crew-start: create .managed marker for %q: %v\n", req.Name, markerErr)
	}

	// ── Step 7: update registry with handle ──
	if windowHandle != "" {
		rec.Handle = windowHandle
		if updateErr := crew.Write(h.projectDir, rec); updateErr != nil {
			// Non-fatal: session is running; handle is just missing from registry.
			fmt.Fprintf(os.Stderr, "daemon: crew-start: update registry handle for %q: %v\n", req.Name, updateErr)
		}
	}

	result := CrewStartResult{
		SessionID: sessionID,
		Name:      req.Name,
	}
	out, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("encode result: %w", marshalErr)
	}
	return out, nil
}

// resolveSessionID determines the session_id to use for a crew-start call.
//
// Returns (newSessionID, false, nil) for a fresh crew session.
// Returns (existingID, true, nil) for a stale re-launch (record exists; resume it).
// Returns ("", false, err) when a collision blocks the start.
func (h *crewHandlerImpl) resolveSessionID(name, wantQueue string) (sessionID string, isResume bool, err error) {
	existing, loadErr := crew.Load(h.projectDir, name)
	if errors.Is(loadErr, crew.ErrNotFound) {
		// No existing record. Check for queue conflict then mint a fresh id.
		if conflictErr := h.checkQueueConflict(name, wantQueue); conflictErr != nil {
			return "", false, conflictErr
		}
		return uuid.New().String(), false, nil
	}
	if loadErr != nil {
		return "", false, fmt.Errorf("load existing record for %q: %w", name, loadErr)
	}
	// Record exists → treat as stale re-launch: reuse the recorded session_id
	// and launch with --resume so the crew continues the same conversation.
	// Per spec §7: "re-use name+queue and the recorded session_id, relaunching
	// interactive --resume <uuid>".
	return existing.SessionID, true, nil
}

// checkQueueConflict scans existing crew records for a LIVE binding to wantQueue
// under a different name. Returns an error if a conflict is found.
func (h *crewHandlerImpl) checkQueueConflict(name, wantQueue string) error {
	records, err := crew.List(h.projectDir)
	if err != nil {
		return fmt.Errorf("list crew for queue conflict: %w", err)
	}
	for _, r := range records {
		if r.Queue == wantQueue && r.Name != name {
			return fmt.Errorf("queue %q already bound to crew %q", wantQueue, r.Name)
		}
	}
	return nil
}

// ensureQueue ensures the named queue exists in .harmonik/queues/<name>.json.
// If absent, persists a minimal empty Queue{Name:q, Workers:1}. Idempotent.
func (h *crewHandlerImpl) ensureQueue(ctx context.Context, queueName string) error {
	q, err := queue.Load(ctx, h.projectDir, queueName)
	if err != nil {
		return fmt.Errorf("load queue %q: %w", queueName, err)
	}
	if q != nil {
		return nil // already exists
	}
	// Status is set to QueueStatusCompleted so the QM-027 single-active guard
	// permits the first queue-submit to this name. An empty/zero status is not
	// "completed", so the guard would incorrectly reject the submit with
	// queue_already_active (-32010) — hk-vrnh3.
	minimal := &queue.Queue{
		SchemaVersion: 1,
		Name:          queueName,
		Workers:       1,
		Status:        queue.QueueStatusCompleted,
	}
	if err := queue.Persist(ctx, h.projectDir, minimal); err != nil {
		return fmt.Errorf("persist queue %q: %w", queueName, err)
	}
	return nil
}

// pasteCrewMission delivers the mission kick-off line to the crew pane via the
// bracketed-paste mechanism (mirrors pasteInjectImplementerInitial).
//
// Message: "Please read <handoffPath> and run /session-resume on it, then begin
// your operating loop."
//
// Best-effort: errors are logged to stderr but do not fail the crew-start op.
func (h *crewHandlerImpl) pasteCrewMission(ctx context.Context, inj pasteInjecter, sessionID, handoffPath string) {
	// Dismiss the welcome splash with an Enter keypress before the paste.
	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: crew-start: splash dismiss SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx)
	}

	bufName := bufferName(sessionID, "crew-init")
	msg := fmt.Sprintf("Please read %s and run /session-resume on it, then begin your operating loop.\n", handoffPath)

	if err := inj.WriteLastPane(ctx, bufName, []byte(msg)); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: crew-start: paste mission WriteLastPane: %v\n", err)
		return
	}
	// Settle after the paste before submitting (hk-jzpqo).
	//
	// Root cause of the not-submitted seed: the post-paste submit Enter was sent
	// IMMEDIATELY after WriteLastPane (the bracketed paste), with no settle in
	// between.  A freshly-spawned crew pane — like a freshly-`--resume`'d
	// implementer (hk-ip33d) — has a REPL input handler that is intermittently
	// not yet ready to accept the keypress at that instant: the paste content is
	// still being absorbed by the TUI, so the single Enter races it and is
	// swallowed.  The seed then sits in the input bar unsubmitted and the crew
	// never begins its loop until someone manually presses Enter.
	//
	// Fix: mirror the working implementer paths — wait splashDismissWait after the
	// paste so the bracketed-paste content lands and the REPL returns to an
	// input-ready prompt, THEN submit via sendResumeSubmitEnter, the same bounded
	// submit-Enter retry the implementer-resume path uses (hk-ip33d).  A redundant
	// Enter at an already-submitted REPL is a harmless empty line, so the retry
	// only ever helps: at least one keypress lands after the input handler is ready.
	splashDismissWait(ctx)
	if es, ok := inj.(enterSender); ok {
		sendResumeSubmitEnter(ctx, es)
	}
}

// crewPasteInjector implements pasteInjecter and enterSender for the crew
// independent-session spawn path (hk-mmlqt). It delivers paste operations
// directly to a specific pane target using the tmux adapter, bypassing the
// perRunSubstrate (which routes via shared spawn state in the daemon session).
type crewPasteInjector struct {
	adapter interface {
		WriteToPane(ctx context.Context, bufferName, paneTarget string, payload []byte) error
		SendKeysEnter(ctx context.Context, paneTarget string) error
	}
	paneTarget string
}

func (c *crewPasteInjector) WriteLastPane(ctx context.Context, bufferName string, payload []byte) error {
	return c.adapter.WriteToPane(ctx, bufferName, c.paneTarget, payload)
}

func (c *crewPasteInjector) SendEnterToLastPane(ctx context.Context) error {
	return c.adapter.SendKeysEnter(ctx, c.paneTarget)
}

// pasteCrewMissionToSession delivers the mission kick-off line to the crew pane
// using the pane target captured from sess (independent-session path, hk-mmlqt).
//
// It builds a crewPasteInjector from the substrate's tmux adapter and the
// session's pane target, then delegates to pasteCrewMission. Best-effort: if
// the adapter or pane target is unavailable, the paste is silently skipped.
func (h *crewHandlerImpl) pasteCrewMissionToSession(ctx context.Context, sess handler.SubstrateSession, sessionID, handoffPath string) {
	pt, ok := sess.(paneTargeter)
	if !ok {
		return
	}
	target := pt.PaneTarget()
	if target == "" {
		return
	}
	sa, ok := h.substrate.(substrateWithAdapter)
	if !ok {
		return
	}
	inj := &crewPasteInjector{adapter: sa.tmuxAdapter(), paneTarget: target}
	h.pasteCrewMission(ctx, inj, sessionID, handoffPath)
}

// createCrewManagedMarker creates .harmonik/keeper/<name>.managed so the keeper
// recognises this crew member as managed (keeper.IsManaged returns true).
// Idempotent: succeeds when the file already exists.
func createCrewManagedMarker(projectDir, name string) error {
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		return fmt.Errorf("mkdir keeper: %w", err)
	}
	markerPath := filepath.Join(keeperDir, name+".managed")
	//nolint:gosec // G304: markerPath derived from projectDir/.harmonik/keeper/<name>.managed
	f, err := os.OpenFile(markerPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create .managed: %w", err)
	}
	return f.Close()
}

// missionFrontMatter is the subset of the mission-handoff YAML front-matter the
// daemon reads at launch time. Only model: matters here; all other fields are
// the crew's concern (it re-derives them on /session-resume). yaml.v3 silently
// ignores the unmodelled keys (schema_version, crew_name, queue, …).
//
// Spec ref: specs/crew-handoff-schema.md §3 (model: optional, opus|sonnet|haiku).
type missionFrontMatter struct {
	Model string `yaml:"model"`
}

// readMissionModel reads the optional model: field from a mission handoff's YAML
// front-matter (the leading `---`-delimited block per crew-handoff-schema.md §3).
//
// Best-effort by design: an empty path, a missing/unreadable file, a mission
// without a front-matter block, or an absent model: field all return "". The
// caller passes that to buildCrewLaunchSpec, which then injects no --model flag
// and the crew inherits the compiled default model. A malformed front-matter
// block likewise degrades to "" rather than failing the crew-start op — the
// model: field is an optimisation, not a correctness contract.
func readMissionModel(missionPath string) string {
	if missionPath == "" {
		return ""
	}
	//nolint:gosec // G304: missionPath is an operator/captain-supplied handoff path
	data, err := os.ReadFile(missionPath)
	if err != nil {
		return ""
	}

	block := frontMatterBlock(string(data))
	if block == "" {
		return ""
	}

	var fm missionFrontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return ""
	}
	return fm.Model
}

// frontMatterBlock extracts the YAML body between the leading `---` fence and the
// closing `---` fence of a Markdown handoff. Returns "" when no front-matter
// block is present (the file does not open with a `---` line).
func frontMatterBlock(content string) string {
	const fence = "---"
	rest, ok := strings.CutPrefix(content, fence+"\n")
	if !ok {
		return ""
	}
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// ─────────────────────────────────────────────────────────────────────────────
// Keeper post-spawn liveness probe (hk-qgfme)
// ─────────────────────────────────────────────────────────────────────────────

// probeKeeperLiveness polls LiveKeeperPresent for up to grace after a crew
// session is spawned. Called as a goroutine (async, off the HandleCrewStart RPC)
// so the caller always receives a live agent immediately.
//
// If the keeper watcher flock is not held by the end of the grace window,
// reportKeeperWatcherDead emits a session_keeper_watcher_dead event and a
// keeper-alert comms message. The crew agent is ALWAYS kept live; this path
// is warn-loud, never a hard-block.
//
// The probe is disabled (not called) when keeperCfg.FlockAcquireGrace == 0.
func (h *crewHandlerImpl) probeKeeperLiveness(crewName string, grace time.Duration) {
	fn := h.liveKeeperFn
	if fn == nil {
		fn = keeper.LiveKeeperPresent
	}

	deadline := time.Now().Add(grace)
	for {
		if fn(h.projectDir, crewName) {
			return // keeper watcher flock confirmed live
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			h.reportKeeperWatcherDead(crewName, grace)
			return
		}
		sleep := keeperProbePollInterval
		if sleep > remaining {
			sleep = remaining
		}
		time.Sleep(sleep)
	}
}

// reportKeeperWatcherDead fires when the post-spawn probe finds the keeper
// watcher flock unheld after the grace window. It logs to stderr (always),
// emits a session_keeper_watcher_dead event (when eventBus != nil), and sends
// a keeper-alert comms message to the operator (when commsBus != nil).
//
// The crew agent remains live; the captain/operator is responsible for
// remediation (e.g. running `harmonik keeper --agent <crew>`).
func (h *crewHandlerImpl) reportKeeperWatcherDead(crewName string, grace time.Duration) {
	ctx := context.Background()
	fmt.Fprintf(os.Stderr,
		"daemon: crew %q keeper watcher NOT live after %.0fs grace — crew is monitor-less; "+
			"check keeper config and run 'harmonik keeper --agent %s'\n",
		crewName, grace.Seconds(), crewName)

	// Emit durable session_keeper_watcher_dead event.
	if h.eventBus != nil {
		payload := core.SessionKeeperWatcherDeadPayload{
			AgentName:          crewName,
			GracePeriodSeconds: grace.Seconds(),
			Reason:             "flock not acquired within flock_acquire_grace",
		}
		if b, marshalErr := json.Marshal(payload); marshalErr == nil {
			//nolint:errcheck // best-effort diagnostic event; failure logged by bus
			_ = h.eventBus.Emit(ctx, core.EventTypeSessionKeeperWatcherDead, b)
		}
	}

	// Send keeper-alert comms to operator.
	if h.commsBus != nil {
		msg := core.AgentMessagePayload{
			From:  "daemon",
			To:    "operator",
			Topic: "keeper-alert",
			Body: fmt.Sprintf(
				"crew %q keeper window spawned but watcher NOT live (flock unheld after %.0fs) — "+
					"crew is monitor-less; run 'harmonik keeper --agent %s' to attach a watcher",
				crewName, grace.Seconds(), crewName),
		}
		//nolint:errcheck // best-effort alert; non-fatal
		_, _ = h.commsBus.EmitAgentMessage(ctx, msg)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HandleCrewStop
// ─────────────────────────────────────────────────────────────────────────────

// HandleCrewStop implements CrewHandler.HandleCrewStop.
//
// Stop flow per c2-spec.md §3.5:
//  1. crew.Load → error if absent
//  2. quit→grace→kill the pane via handle (crewPaneStopper, best-effort)
//  3. Remove .managed marker
//  4. crew.Remove registry record
//  5. Optional --pause-queue via OperatorControlHandler
func (h *crewHandlerImpl) HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req CrewStopRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	rec, err := crew.Load(h.projectDir, req.Name)
	if errors.Is(err, crew.ErrNotFound) {
		return nil, fmt.Errorf("crew %q not found", req.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("load record for %q: %w", req.Name, err)
	}

	// ── Quit→grace→kill the pane / session (hk-mmlqt) ──
	// Use crewSessionStopper (kills the whole independent session) when available.
	// Fall back to crewPaneStopper (kills the window inside the daemon session)
	// for substrates that don't implement the independent-session path.
	if h.substrate != nil {
		if css, ok := h.substrate.(crewSessionStopper); ok {
			if stopErr := css.StopCrewSession(ctx, req.Name, rec.Handle); stopErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: crew-stop: stop crew session for %q: %v\n", req.Name, stopErr)
			}
		} else if rec.Handle != "" {
			if stopper, ok2 := h.substrate.(crewPaneStopper); ok2 {
				if stopErr := stopper.StopWindowByHandle(ctx, rec.Handle); stopErr != nil {
					fmt.Fprintf(os.Stderr, "daemon: crew-stop: stop window %q for %q: %v\n", rec.Handle, req.Name, stopErr)
				}
			}
		}
	}

	// ── Remove .managed marker ──
	markerPath := filepath.Join(h.projectDir, ".harmonik", "keeper", req.Name+".managed")
	if removeErr := os.Remove(markerPath); removeErr != nil && !os.IsNotExist(removeErr) {
		fmt.Fprintf(os.Stderr, "daemon: crew-stop: remove .managed for %q: %v\n", req.Name, removeErr)
	}

	// ── Remove registry record ──
	if removeErr := crew.Remove(h.projectDir, req.Name); removeErr != nil && !errors.Is(removeErr, crew.ErrNotFound) {
		return nil, fmt.Errorf("remove registry for %q: %w", req.Name, removeErr)
	}

	// ── Optional --pause-queue ──
	if req.PauseQueue && h.opPauseCtrl != nil && rec.Queue != "" {
		if pauseErr := h.opPauseCtrl.HandleOperatorPause(ctx, rec.Queue); pauseErr != nil {
			return nil, fmt.Errorf("pause queue %q: %w", rec.Queue, pauseErr)
		}
	}

	out, marshalErr := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: req.Name})
	if marshalErr != nil {
		return nil, fmt.Errorf("encode result: %w", marshalErr)
	}
	return out, nil
}
