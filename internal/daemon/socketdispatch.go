package daemon

// socketdispatch.go — the daemon-side adapter holder for the pure socketrouter
// dispatch table (giant-retirement, plans/2026-07-16-giant-retirement).
//
// socketDispatch bundles the injected handler interfaces and exposes one small
// named method per op, each returning a socketrouter.Result (the neutral,
// wire-vocabulary-free outcome). buildSocketRouter registers all 27 routable ops
// (every op except subscribe, which stays a daemon pre-branch alongside
// hook-relay). resultToResponse maps a socketrouter.Result back to the wire
// SocketResponse — the single real byte-drift surface, pinned by T5.
//
// The adapters lift each op's switch-case body verbatim and re-decode
// SocketRequest from raw locally when they need scalar fields (byte-identical to
// the pre-carve reEncoded bytes). Effectful concerns (net.Conn writers, uuid
// validation, the hook-relay/subscribe pre-branches) stay in socket.go.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
	"github.com/gregberns/harmonik/internal/queue"
)

// socketDispatch holds the injected op handlers. SubscribeHandler and
// HookRelayHandler are deliberately absent — both are daemon pre-branches handled
// before Dispatch (scope-F4/Q2).
type socketDispatch struct {
	h          RequestHandler
	qh         QueueHandler
	oh         OperatorControlHandler
	ch         CommsSendHandler // comma-ok asserted for presence/recv/decisions
	crewh      CrewHandler
	sleepWakeh QuiesceOverrideHandler
	stateh     StateHandler
	dashh      DashboardHandler
}

// decodeReq re-decodes SocketRequest from the re-encoded raw bytes. Byte-identical
// to the pre-carve path (same reEncoded bytes, same json.RawMessage extraction,
// same nil on an absent key). The unmarshal error is intentionally ignored: the
// pre-switch decodeSocketRequest already validated these bytes decode into a
// SocketRequest, so a second decode of the same bytes cannot newly fail.
func decodeReq(raw json.RawMessage) SocketRequest {
	var req SocketRequest
	_ = json.Unmarshal(raw, &req) //nolint:errcheck // bytes already validated pre-Dispatch
	return req
}

// resultToResponse maps a socketrouter.Result to the wire SocketResponse.
// It is the inverse of resultFromResponse and the one new byte-drift surface.
func resultToResponse(res socketrouter.Result, op string) SocketResponse {
	if res.Unknown {
		return SocketResponse{Ok: false, Error: fmt.Sprintf("daemon: unknown op %q", op)}
	}
	return SocketResponse{
		Ok:        res.OK,
		Result:    res.Payload,
		Error:     res.Err,
		ErrorCode: res.ErrorCode,
	}
}

// resultFromResponse lifts a daemon-built SocketResponse (from handleQueueOp) into
// a neutral socketrouter.Result. Round-trips byte-identically via resultToResponse.
func resultFromResponse(resp SocketResponse) socketrouter.Result {
	return socketrouter.Result{
		OK:        resp.Ok,
		Payload:   resp.Result,
		Err:       resp.Error,
		ErrorCode: resp.ErrorCode,
	}
}

// --- RequestHandler ops ------------------------------------------------------

func (d *socketDispatch) emitOutcome(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	req := decodeReq(raw)
	result, err := d.h.EmitOutcome(ctx, OutcomeRequest{
		RunID:   req.RunID,
		BeadID:  req.BeadID,
		Outcome: req.Outcome,
	})
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) claimNext(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	req := decodeReq(raw)
	result, err := d.h.ClaimNext(ctx, req.Role)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

// --- QueueHandler ops --------------------------------------------------------

func (d *socketDispatch) queueSubmit(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueSubmit(ctx, raw)
	}))
}

func (d *socketDispatch) queueAppend(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueAppend(ctx, raw)
	}))
}

func (d *socketDispatch) queueStatus(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueStatus(ctx, raw)
	}))
}

func (d *socketDispatch) queueDryRun(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueDryRun(ctx, raw)
	}))
}

func (d *socketDispatch) queueList(ctx context.Context, _ json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueList(ctx)
	}))
}

func (d *socketDispatch) queueSetConcurrency(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueSetConcurrency(ctx, raw)
	}))
}

func (d *socketDispatch) queueCancel(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleQueueCancel(ctx, raw)
	}))
}

func (d *socketDispatch) workerSetEnabled(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return resultFromResponse(handleQueueOp(ctx, d.qh, func(h QueueHandler) (json.RawMessage, *queue.RPCError) {
		return h.HandleWorkerSetEnabled(ctx, raw)
	}))
}

// --- CommsSendHandler + type-asserted comms/decisions ops --------------------

func (d *socketDispatch) commsSend(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.ch == nil {
		return socketrouter.Result{OK: false, Err: "daemon: CommsSendHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := d.ch.HandleCommsSend(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) commsPresence(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	cp, ok := d.ch.(CommsPresenceHandler)
	if !ok || cp == nil {
		return socketrouter.Result{OK: false, Err: "daemon: CommsPresenceHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := cp.HandleCommsPresence(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) commsRecv(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	cr, ok := d.ch.(CommsRecvHandler)
	if !ok || cr == nil {
		return socketrouter.Result{OK: false, Err: "daemon: CommsRecvHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := cr.HandleCommsRecv(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) decisionsRaise(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	dh, ok := d.ch.(DecisionsHandler)
	if !ok || dh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: DecisionsHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := dh.HandleDecisionsRaise(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) decisionsWithdraw(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	dh, ok := d.ch.(DecisionsHandler)
	if !ok || dh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: DecisionsHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := dh.HandleDecisionsWithdraw(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) decisionsList(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	dh, ok := d.ch.(DecisionsHandler)
	if !ok || dh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: DecisionsHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := dh.HandleDecisionsList(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) decisionsAnswer(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	dh, ok := d.ch.(DecisionsHandler)
	if !ok || dh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: DecisionsHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := dh.HandleDecisionsAnswer(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

// --- OperatorControlHandler ops (no-result success) --------------------------

func (d *socketDispatch) operatorPause(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.oh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: OperatorControlHandler not registered"}
	}
	req := decodeReq(raw)
	if err := d.oh.HandleOperatorPause(ctx, req.Queue); err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: operator-pause: %v", err)}
	}
	return socketrouter.Result{OK: true}
}

func (d *socketDispatch) operatorResume(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.oh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: OperatorControlHandler not registered"}
	}
	req := decodeReq(raw)
	if err := d.oh.HandleOperatorResume(ctx, req.Queue); err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: operator-resume: %v", err)}
	}
	return socketrouter.Result{OK: true}
}

// --- Operator verdict-override ops (RC-027) ----------------------------------
//
// confirm_verdict / veto_verdict ride the OperatorControlHandler value (d.oh)
// via interface type-assertion — the same late-addition idiom as decisions-* on
// d.ch — so no new socketDispatch field is needed. A reconciliation run whose
// YAML policy sets confirm_required: true parks in the VerdictConfirmationRegistry;
// these ops deliver the operator's confirm/veto decision to that parked run.
// error_code 16 (operator-control-invalid-state) is returned when no run is
// parked for the given run_id — the CLI exits 16.
//
// Spec ref: specs/reconciliation/spec.md §4.5 RC-027; specs/operator-nfr.md §4.3 ON-014.

func (d *socketDispatch) confirmVerdict(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return d.verdictOverride(ctx, raw, core.VerdictOverrideDecisionConfirm, "confirm_verdict")
}

func (d *socketDispatch) vetoVerdict(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	return d.verdictOverride(ctx, raw, core.VerdictOverrideDecisionVeto, "veto_verdict")
}

// verdictOverride is the shared body for confirmVerdict / vetoVerdict: decode
// the raw request, build a core.OperatorVerdictOverrideRequest, validate it, and
// route to the OperatorControlHandler's HandleVerdictOverride (type-asserted to
// VerdictOverrideHandler). promote_to is only meaningful for veto.
func (d *socketDispatch) verdictOverride(ctx context.Context, raw json.RawMessage, decision core.VerdictOverrideDecision, op string) socketrouter.Result {
	voh, ok := d.oh.(VerdictOverrideHandler)
	if !ok || voh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: VerdictOverrideHandler not registered"}
	}
	req := decodeReq(raw)
	overrideReq := core.OperatorVerdictOverrideRequest{
		TargetRunID:   req.RunID,
		Decision:      decision,
		VetoPromotion: core.VetoPromotion(req.PromoteTo),
	}
	if !overrideReq.Valid() {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: %s: invalid override request for run %q", op, req.RunID)}
	}
	code, err := voh.HandleVerdictOverride(ctx, overrideReq)
	if err != nil {
		return socketrouter.Result{OK: false, Err: err.Error(), ErrorCode: code}
	}
	return socketrouter.Result{OK: true}
}

// --- CrewHandler ops ---------------------------------------------------------

func (d *socketDispatch) crewStart(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.crewh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: CrewHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := d.crewh.HandleCrewStart(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: crew-start: %v", err)}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) crewStop(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.crewh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: CrewHandler not registered"}
	}
	req := decodeReq(raw)
	result, err := d.crewh.HandleCrewStop(ctx, req.Payload)
	if err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: crew-stop: %v", err)}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

// --- QuiesceOverrideHandler ops (no-result success) --------------------------

func (d *socketDispatch) daemonSleep(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.sleepWakeh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: QuiesceOverrideHandler not registered"}
	}
	req := decodeReq(raw)
	var sleepReq struct {
		Force bool `json:"force"`
	}
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &sleepReq); err != nil {
			return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: daemon-sleep: decode payload: %v", err)}
		}
	}
	if err := d.sleepWakeh.HandleDaemonSleep(ctx, sleepReq.Force); err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true}
}

func (d *socketDispatch) daemonWake(ctx context.Context, raw json.RawMessage) socketrouter.Result {
	if d.sleepWakeh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: QuiesceOverrideHandler not registered"}
	}
	req := decodeReq(raw)
	var wakeReq struct {
		Agent string `json:"agent"`
		All   bool   `json:"all"`
	}
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &wakeReq); err != nil {
			return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: daemon-wake: decode payload: %v", err)}
		}
	}
	if err := d.sleepWakeh.HandleDaemonWake(ctx, wakeReq.Agent, wakeReq.All); err != nil {
		return socketrouter.Result{OK: false, Err: err.Error()}
	}
	return socketrouter.Result{OK: true}
}

// --- StateHandler / DashboardHandler ops -------------------------------------

func (d *socketDispatch) state(ctx context.Context, _ json.RawMessage) socketrouter.Result {
	if d.stateh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: StateHandler not registered"}
	}
	result, err := d.stateh.HandleState(ctx)
	if err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: state: %v", err)}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

func (d *socketDispatch) dashboard(ctx context.Context, _ json.RawMessage) socketrouter.Result {
	if d.dashh == nil {
		return socketrouter.Result{OK: false, Err: "daemon: DashboardHandler not registered"}
	}
	result, err := d.dashh.HandleDashboard(ctx)
	if err != nil {
		return socketrouter.Result{OK: false, Err: fmt.Sprintf("daemon: dashboard: %v", err)}
	}
	return socketrouter.Result{OK: true, Payload: result}
}

// buildSocketRouter registers all 27 routable ops (every op except subscribe,
// which is a daemon pre-branch alongside hook-relay). cyclop(buildSocketRouter)=1.
func buildSocketRouter(d *socketDispatch) *socketrouter.Router {
	r := socketrouter.New()
	r.Register("emit-outcome", d.emitOutcome)
	r.Register("claim-next", d.claimNext)
	r.Register("queue-submit", d.queueSubmit)
	r.Register("queue-append", d.queueAppend)
	r.Register("queue-status", d.queueStatus)
	r.Register("queue-dry-run", d.queueDryRun)
	r.Register("queue-list", d.queueList)
	r.Register("queue-set-concurrency", d.queueSetConcurrency)
	r.Register("queue-cancel", d.queueCancel)
	r.Register("worker-set-enabled", d.workerSetEnabled)
	r.Register("comms-send", d.commsSend)
	r.Register("comms-presence", d.commsPresence)
	r.Register("comms-recv", d.commsRecv)
	r.Register("decisions-raise", d.decisionsRaise)
	r.Register("decisions-withdraw", d.decisionsWithdraw)
	r.Register("decisions-list", d.decisionsList)
	r.Register("decisions-answer", d.decisionsAnswer)
	r.Register("operator-pause", d.operatorPause)
	r.Register("operator-resume", d.operatorResume)
	r.Register("confirm_verdict", d.confirmVerdict)
	r.Register("veto_verdict", d.vetoVerdict)
	r.Register("crew-start", d.crewStart)
	r.Register("crew-stop", d.crewStop)
	r.Register("daemon-sleep", d.daemonSleep)
	r.Register("daemon-wake", d.daemonWake)
	r.Register("state", d.state)
	r.Register("dashboard", d.dashboard)
	return r
}
