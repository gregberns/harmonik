package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/crewrun"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/keeper"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/projectconfig"
	"github.com/gregberns/harmonik/internal/queue"
	"github.com/gregberns/harmonik/internal/substrate"
)

type crewKeeperEventBus interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

type crewKeeperCommsBus interface {
	EmitAgentMessage(ctx context.Context, payload core.AgentMessagePayload) (core.EventID, error)
}

const keeperProbePollInterval = time.Second

type windowHandleExposer interface {
	WindowHandle() string
}

type crewPaneStopper interface {
	// StopWindowByHandle sends /quit to the pane (best-effort), waits a grace
	// period, then kills the window identified by handle.
	StopWindowByHandle(ctx context.Context, handle string) error
}

type crewHandlerImpl struct {
	claudeBinary string
	projectDir   string
	rcPrefix     string                 // per-project --remote-control label prefix (hk-igpg); "" = bare label
	substrate    handler.Substrate      // spawns crew windows
	opPauseCtrl  OperatorControlHandler // for --pause-queue in crew-stop; may be nil

	// keeper probe fields (hk-qgfme): async post-spawn liveness check.
	// All fields are optional; nil = feature disabled (probe skipped).
	keeperCfg    projectconfig.KeeperConfig          // FlockAcquireGrace drives the probe; zero = disabled
	eventBus     crewKeeperEventBus                  // for emitting session_keeper_watcher_dead; may be nil
	commsBus     crewKeeperCommsBus                  // for keeper-alert comms to operator; may be nil
	liveKeeperFn func(projectDir, agent string) bool // injectable for testing; nil = keeper.LiveKeeperPresent

	// crews holds the per-crew-name config read from .harmonik/config.yaml's
	// crews: block (hk-l63b9). Nil/absent = no per-crew config; the harness
	// resolver's third tier falls through to the default "claude".
	crews map[string]projectconfig.CrewConfig
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
func WithKeeperProbe(keeperCfg projectconfig.KeeperConfig, eventBus crewKeeperEventBus, commsBus crewKeeperCommsBus) CrewHandlerOpt {
	return func(h *crewHandlerImpl) {
		h.keeperCfg = keeperCfg
		h.eventBus = eventBus
		h.commsBus = commsBus
	}
}

// WithCrewsConfig configures the per-crew-name config read from
// .harmonik/config.yaml's crews: block (hk-l63b9) — the third tier of the
// crew-scoped harness resolver (flag > mission front-matter > per-crew config >
// default "claude"). Omitting this option leaves the tier empty.
func WithCrewsConfig(crews map[string]projectconfig.CrewConfig) CrewHandlerOpt {
	return func(h *crewHandlerImpl) {
		h.crews = crews
	}
}

func (h *crewHandlerImpl) crewConfigHarness(name string) string {
	if h.crews == nil {
		return ""
	}
	return h.crews[name].Harness
}

// NewCrewHandler constructs a crewrun.CrewHandler implementation.
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
func NewCrewHandler(claudeBinary, projectDir, rcPrefix string, substrate handler.Substrate, opPauseCtrl OperatorControlHandler, opts ...CrewHandlerOpt) crewrun.CrewHandler {
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

// HandleCrewStart implements crewrun.CrewHandler.HandleCrewStart.
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
	var req crewrun.CrewStartRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if req.Queue == "" {
		return nil, fmt.Errorf("queue is required")
	}

	sessionID, isResume, err := h.resolveSessionID(req.Name, req.Queue)
	if err != nil {
		return nil, err
	}

	rec := crew.Record{
		Name:      req.Name,
		Type:      h.resolveCrewType(req),
		SessionID: sessionID,
		Queue:     req.Queue,
		StartedAt: time.Now().UTC(),
	}
	if writeErr := crew.Write(h.projectDir, rec); writeErr != nil {
		return nil, fmt.Errorf("write registry: %w", writeErr)
	}

	if qErr := h.ensureQueue(ctx, req.Queue); qErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback; primary error returned
		return nil, fmt.Errorf("ensure queue: %w", qErr)
	}

	if markerErr := createCrewManagedMarker(h.projectDir, req.Name); markerErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: crew-start: create .managed marker for %q: %v\n", req.Name, markerErr)
	}

	model := crewrun.ReadMissionModel(req.MissionPath)
	harness := crewrun.ResolveCrewHarness(req.Harness, crewrun.ReadMissionHarness(req.MissionPath), h.crewConfigHarness(req.Name))
	lspec, buildErr := crewrun.BuildCrewLaunchSpec(crewrun.CrewLaunchCtx{
		ClaudeBinary: h.claudeBinary,
		Name:         req.Name,
		RcPrefix:     h.rcPrefix,
		SessionID:    sessionID,
		ProjectDir:   h.projectDir,
		Resume:       isResume,
		Model:        model,
		Harness:      harness,
	})
	if buildErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
		return nil, fmt.Errorf("build launch spec: %w", buildErr)
	}

	var windowHandle string
	if h.substrate != nil {
		argv := append([]string{lspec.Binary}, lspec.Args...)
		spawn := handler.SubstrateSpawn{
			WindowName: tmux.WindowAgent,
			Cwd:        lspec.WorkDir,
			Env:        lspec.Env,
			Argv:       argv,
		}

		if css, ok := h.substrate.(crewSessionSpawner); ok {
			var sess handler.SubstrateSession
			sess, err = css.SpawnCrewSession(ctx, req.Name, spawn)
			if err != nil {
				_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
				return nil, fmt.Errorf("spawn crew session: %w", err)
			}
			if h.keeperCfg.FlockAcquireGrace > 0 {
				go h.probeKeeperLiveness(req.Name, h.keeperCfg.FlockAcquireGrace)
			}
			if wh, ok2 := sess.(windowHandleExposer); ok2 {
				windowHandle = wh.WindowHandle()
			}
			if req.MissionPath != "" {
				h.pasteCrewMissionToSession(ctx, sess, sessionID, req.MissionPath)
			}
		} else {
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
			if prs != nil && req.MissionPath != "" {
				h.pasteCrewMission(ctx, prs, sessionID, req.MissionPath)
			}
		}
	}

	if windowHandle != "" {
		rec.Handle = windowHandle
		if updateErr := crew.Write(h.projectDir, rec); updateErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: crew-start: update registry handle for %q: %v\n", req.Name, updateErr)
		}
	}

	result := crewrun.CrewStartResult{
		SessionID: sessionID,
		Name:      req.Name,
	}
	out, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		return nil, fmt.Errorf("encode result: %w", marshalErr)
	}
	return out, nil
}

func (h *crewHandlerImpl) resolveSessionID(name, wantQueue string) (sessionID string, isResume bool, err error) {
	existing, loadErr := crew.Load(h.projectDir, name)
	if errors.Is(loadErr, crew.ErrNotFound) {
		if conflictErr := h.checkQueueConflict(name, wantQueue); conflictErr != nil {
			return "", false, conflictErr
		}
		return uuid.New().String(), false, nil
	}
	if loadErr != nil {
		return "", false, fmt.Errorf("load existing record for %q: %w", name, loadErr)
	}
	return existing.SessionID, true, nil
}

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

func (h *crewHandlerImpl) resolveCrewType(req crewrun.CrewStartRequest) string {
	if req.Type != "" {
		return req.Type
	}
	agentsDir := filepath.Join(h.projectDir, ".harmonik", "agents")
	if st, err := os.Stat(filepath.Join(agentsDir, req.Name)); err == nil && st.IsDir() {
		return req.Name
	}
	return ""
}

func (h *crewHandlerImpl) ensureQueue(ctx context.Context, queueName string) error {
	q, err := queue.Load(ctx, h.projectDir, queueName)
	if err != nil {
		return fmt.Errorf("load queue %q: %w", queueName, err)
	}
	if q != nil {
		return nil // already exists
	}
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

func (h *crewHandlerImpl) pasteCrewMission(ctx context.Context, inj pasteInjecter, sessionID, handoffPath string) {
	clk := substrate.ClockPort(substrate.SystemClock{})

	if es, ok := inj.(enterSender); ok {
		if err := es.SendEnterToLastPane(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "daemon: crew-start: splash dismiss SendEnterToLastPane: %v\n", err)
		}
		splashDismissWait(ctx, clk)
	}

	bufName := bufferName(sessionID, "crew-init")
	msg := fmt.Sprintf("Please read %s and run /session-resume on it, then begin your operating loop.\n", handoffPath)

	if reason := injectAndVerifySeed(ctx, clk, inj, bufName, []byte(msg), "/session-resume", "crew-init"); reason != "" {
		fmt.Fprintf(os.Stderr, "daemon: crew-start: paste mission unverified: %s\n", reason)
		return
	}
	splashDismissWait(ctx, clk)
	if es, ok := inj.(enterSender); ok {
		sendSubmitEnterWithRetry(ctx, clk, es, "crew-init")
	}
}

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

// CaptureLastPane implements paneCapturer so injectAndVerifySeed can confirm the
// crew seed rendered before submitting (hk-dvcc7). It type-asserts the adapter
// to the pane-capture capability (tmux.OSAdapter satisfies it) and reads the
// crew pane's rendered text. Returns errPaneCaptureUnsupported (wrapped) when
// the adapter cannot capture — e.g. a minimal test double — so the verify loop
// falls back to trusting the write, unchanged from the prior blind behaviour.
func (c *crewPasteInjector) CaptureLastPane(ctx context.Context, scrollback int) (string, error) {
	pc, ok := c.adapter.(paneCaptureAdapter)
	if !ok {
		return "", fmt.Errorf("daemon: crewPasteInjector.CaptureLastPane: adapter %T lacks CapturePane: %w", c.adapter, errPaneCaptureUnsupported)
	}
	return pc.CapturePane(ctx, c.paneTarget, scrollback)
}

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

func createCrewManagedMarker(projectDir, name string) error {
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, core.HarmonikDirMode); err != nil {
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

func (h *crewHandlerImpl) reportKeeperWatcherDead(crewName string, grace time.Duration) {
	ctx := context.Background()
	fmt.Fprintf(os.Stderr,
		"daemon: crew %q keeper watcher NOT live after %.0fs grace — crew is monitor-less; "+
			"check keeper config and run 'harmonik keeper --agent %s'\n",
		crewName, grace.Seconds(), crewName)

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

// HandleCrewStop implements crewrun.CrewHandler.HandleCrewStop.
//
// Stop flow per c2-spec.md §3.5:
//  1. crew.Load → error if absent
//  2. quit→grace→kill the pane via handle (crewPaneStopper, best-effort)
//  3. Remove .managed marker
//  4. crew.Remove registry record
//  5. Optional --pause-queue via OperatorControlHandler
func (h *crewHandlerImpl) HandleCrewStop(ctx context.Context, payload json.RawMessage) (json.RawMessage, error) {
	var req crewrun.CrewStopRequest
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

	markerPath := filepath.Join(h.projectDir, ".harmonik", "keeper", req.Name+".managed")
	if removeErr := os.Remove(markerPath); removeErr != nil && !os.IsNotExist(removeErr) {
		fmt.Fprintf(os.Stderr, "daemon: crew-stop: remove .managed for %q: %v\n", req.Name, removeErr)
	}

	if removeErr := crew.Remove(h.projectDir, req.Name); removeErr != nil && !errors.Is(removeErr, crew.ErrNotFound) {
		return nil, fmt.Errorf("remove registry for %q: %w", req.Name, removeErr)
	}

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
