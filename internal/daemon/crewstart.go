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
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/crew"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/queue"
)

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
	substrate    handler.Substrate       // spawns crew windows
	opPauseCtrl  OperatorControlHandler  // for --pause-queue in crew-stop; may be nil
}

// NewCrewHandler constructs a CrewHandler implementation.
//
// claudeBinary is the handler executable (empty resolves to "claude").
// projectDir is the harmonik project root directory.
// substrate is the tmux substrate for spawning crew sessions; may be nil in tests
// that don't exercise the actual spawn path.
// opPauseCtrl is the operator-pause controller used when --pause-queue is set;
// may be nil (crew-stop will skip the pause step).
//
// Bead ref: hk-5tg5o.
func NewCrewHandler(claudeBinary, projectDir string, substrate handler.Substrate, opPauseCtrl OperatorControlHandler) CrewHandler {
	return &crewHandlerImpl{
		claudeBinary: claudeBinary,
		projectDir:   projectDir,
		substrate:    substrate,
		opPauseCtrl:  opPauseCtrl,
	}
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
		return nil, fmt.Errorf("crew-start: decode request: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("crew-start: name is required")
	}
	if req.Queue == "" {
		return nil, fmt.Errorf("crew-start: queue is required")
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
		return nil, fmt.Errorf("crew-start: write registry: %w", writeErr)
	}

	// ── Step 3: ensure named queue ──
	if qErr := h.ensureQueue(ctx, req.Queue); qErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback; primary error returned
		return nil, fmt.Errorf("crew-start: ensure queue: %w", qErr)
	}

	// ── Step 4: build launch spec + spawn ──
	lspec, buildErr := buildCrewLaunchSpec(crewLaunchCtx{
		claudeBinary: h.claudeBinary,
		name:         req.Name,
		sessionID:    sessionID,
		projectDir:   h.projectDir,
		resume:       isResume,
	})
	if buildErr != nil {
		_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
		return nil, fmt.Errorf("crew-start: build launch spec: %w", buildErr)
	}

	var windowHandle string
	if h.substrate != nil {
		argv := append([]string{lspec.Binary}, lspec.Args...)
		spawn := handler.SubstrateSpawn{
			WindowName: "hk-crew-" + req.Name,
			Cwd:        lspec.WorkDir,
			Env:        lspec.Env,
			Argv:       argv,
		}

		if css, ok := h.substrate.(crewSessionSpawner); ok {
			// ── Independent-session path (hk-mmlqt) ──
			// Crew lives in its own tmux session so daemon SIGTERM / supervisor-revive
			// does not kill running crew windows.
			var sess handler.SubstrateSession
			sess, err = css.SpawnCrewSession(ctx, req.Name, spawn)
			if err != nil {
				_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
				return nil, fmt.Errorf("crew-start: spawn crew session: %w", err)
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
			prs := newPerRunSubstrate(h.substrate, h.claudeBinary)
			var sess handler.SubstrateSession
			if prs != nil {
				sess, err = prs.SpawnWindow(ctx, spawn)
			} else {
				sess, err = h.substrate.SpawnWindow(ctx, spawn)
			}
			if err != nil {
				_ = crew.Remove(h.projectDir, req.Name) //nolint:errcheck // rollback
				return nil, fmt.Errorf("crew-start: spawn window: %w", err)
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
	if markerErr := createCrewManagedMarker(h.projectDir, req.Name); markerErr != nil {
		// Non-fatal: session is live; keeper just won't attach until the marker is
		// created externally.
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
		return nil, fmt.Errorf("crew-start: encode result: %w", marshalErr)
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
		return "", false, fmt.Errorf("crew-start: load existing record for %q: %w", name, loadErr)
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
		return fmt.Errorf("crew-start: list crew for queue conflict: %w", err)
	}
	for _, r := range records {
		if r.Queue == wantQueue && r.Name != name {
			return fmt.Errorf("crew-start: queue %q already bound to crew %q", wantQueue, r.Name)
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
	adapter    interface {
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
		return nil, fmt.Errorf("crew-stop: decode request: %w", err)
	}
	if req.Name == "" {
		return nil, fmt.Errorf("crew-stop: name is required")
	}

	rec, err := crew.Load(h.projectDir, req.Name)
	if errors.Is(err, crew.ErrNotFound) {
		return nil, fmt.Errorf("crew-stop: crew %q not found", req.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("crew-stop: load record for %q: %w", req.Name, err)
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
		return nil, fmt.Errorf("crew-stop: remove registry for %q: %w", req.Name, removeErr)
	}

	// ── Optional --pause-queue ──
	if req.PauseQueue && h.opPauseCtrl != nil && rec.Queue != "" {
		if pauseErr := h.opPauseCtrl.HandleOperatorPause(ctx, rec.Queue); pauseErr != nil {
			return nil, fmt.Errorf("crew-stop: pause queue %q: %w", rec.Queue, pauseErr)
		}
	}

	out, marshalErr := json.Marshal(struct {
		Name string `json:"name"`
	}{Name: req.Name})
	if marshalErr != nil {
		return nil, fmt.Errorf("crew-stop: encode result: %w", marshalErr)
	}
	return out, nil
}
