package daemon

// wiringlog_hk4mupj.go — daemon-startup composition-root wiring log (hk-4mupj).
//
// Operators enable the log by setting HARMONIK_DEBUG_WIRING=1 in the daemon's
// environment before launch.  Output goes to os.Stderr so it appears alongside
// other daemon diagnostic output and is always visible regardless of whether a
// structured log writer is attached.
//
// The output is intentionally stable: one line per wiring, sorted by call-site
// line number, tab-separated.  Operators can diff two daemon versions to catch
// silent drops.
//
// Bead ref: hk-4mupj.

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// wiringEntry describes a single composition-root wiring point.
type wiringEntry struct {
	// symbol is the constructed or injected Go symbol (e.g. "handlerPauseCtrl").
	symbol string
	// callSite is the file:line of the wiring call in daemon.go.
	callSite string
	// wires describes what the symbol connects (short plain-English description).
	wires string
}

// compositionRootWirings is the canonical list of the 31 wiring points in
// daemon.Start, ordered by call-site line number.  When a new wiring is added
// to Start, add a corresponding entry here so the log stays complete.
//
// Source: docs/audits/2026-05-20/composition-root-wiring-map.md
// Bead ref: hk-4mupj.
var compositionRootWirings = []wiringEntry{
	{
		symbol:   "registry",
		callSite: "daemon.go:444",
		wires:    "handlercontract.NewRedactionRegistry → bus",
	},
	{
		symbol:   "jsonlWriter",
		callSite: "daemon.go:451",
		wires:    "eventbus.OpenJSONLWriter → bus (JSONL durable log)",
	},
	{
		symbol:   "bus",
		callSite: "daemon.go:466",
		wires:    "eventbus.NewBusImplWithWriter(registry, jsonlWriter) → all subsystems",
	},
	{
		symbol:   "qs",
		callSite: "daemon.go:488",
		wires:    "newQueueStore (or cfg.QueueStore) → queueOpConsumer + work loop",
	},
	{
		symbol:   "handlerPauseCtrl",
		callSite: "daemon.go:492",
		wires:    "NewHandlerPauseController(bus, nil) → pause policy + work loop",
	},
	{
		symbol:   "sharedRunRegistry",
		callSite: "daemon.go:493",
		wires:    "NewRunRegistry → pause policy + work loop (shared snapshot)",
	},
	{
		symbol:   "pausePolicy",
		callSite: "daemon.go:505",
		wires:    "NewHandlerPausePolicyGoroutine(handlerPauseCtrl, sharedRunRegistry)",
	},
	{
		symbol:   "pausePolicy.Subscribe",
		callSite: "daemon.go:510",
		wires:    "agent_rate_limit_status + budget_exhausted → handlerPauseCtrl",
	},
	{
		symbol:   "queueOpConsumer",
		callSite: "daemon.go:524",
		wires:    "NewQueueOperatorEventConsumer(qs, projectDir, bus)",
	},
	{
		symbol:   "queueOpConsumer.Subscribe",
		callSite: "daemon.go:529",
		wires:    "operator_pause_status + operator_resuming → qs",
	},
	{
		symbol:   "subscribeHub",
		callSite: "daemon.go:565",
		wires:    "NewSubscribeHub → SubscribeHandler for 'subscribe' socket op (hk-6ynv4)",
	},
	{
		symbol:   "subscribeHub.Subscribe",
		callSite: "daemon.go:569",
		wires:    "wildcard observer → fans events to per-conn subscriptionStream",
	},
	{
		symbol:   "staleWatcher",
		callSite: "daemon.go:575",
		wires:    "NewStaleWatcher(bus, bus, sharedRunRegistry) → run_stale emitter",
	},
	{
		symbol:   "staleWatcher.Subscribe",
		callSite: "daemon.go:581",
		wires:    "wildcard observer → tracks last event time per run_id (hk-wkzlc)",
	},
	{
		symbol:   "bus.Seal",
		callSite: "daemon.go:541",
		wires:    "locks subscriber list; no further Subscribe calls allowed",
	},
	{
		symbol:   "brAdapter (sweep)",
		callSite: "daemon.go:601",
		wires:    "brcli.NewForProject → orphan-sweep bead ledger + resetter + cat3c closer",
	},
	{
		symbol:   "adapterReg",
		callSite: "daemon.go:694",
		wires:    "handlercontract.NewAdapterRegistry → handler.Register + work loop",
	},
	{
		symbol:   "handler.Register",
		callSite: "daemon.go:695",
		wires:    "ClaudeCodeAdapter → adapterReg for AgentTypeClaudeCode",
	},
	{
		symbol:   "adapterReg.ForAgent (seal)",
		callSite: "daemon.go:701",
		wires:    "seals adapter registry; ForAgent locks set",
	},
	{
		symbol:   "hookStore",
		callSite: "daemon.go:713",
		wires:    "newHookSessionStore → socket listener (HookRelayHandler) + work loop",
	},
	{
		symbol:   "brAdapterForQueue",
		callSite: "daemon.go:727",
		wires:    "brcli.NewForProject → lifecycle.LoadQueueAtStartup",
	},
	{
		symbol:   "qs.SetQueue",
		callSite: "daemon.go:749",
		wires:    "loadedQueue → qs singleton used by work loop",
	},
	{
		symbol:   "handlerPauseCtrl.SetPersistFn",
		callSite: "daemon.go:775",
		wires:    "MakeHandlerPausePersistFn(harmonikDir) → pause state persistence",
	},
	{
		symbol:   "LoadHandlerPauseState",
		callSite: "daemon.go:776",
		wires:    "handler-state.json → handlerPauseCtrl (seed on restart, HP-008)",
	},
	{
		symbol:   "queueHandler",
		callSite: "daemon.go:819",
		wires:    "queue.NewHandlerAdapter(brAdapter, projectDir, qs, bus) → socket listener",
	},
	{
		symbol:   "RunSocketListener",
		callSite: "daemon.go:830",
		wires:    "daemon.sock → hookStore + queueHandler (goroutine)",
	},
	{
		symbol:   "deps",
		callSite: "daemon.go:840",
		wires:    "newWorkLoopDeps(cfg, bus, workflowModeDefault, adapterReg, hookStore)",
	},
	{
		symbol:   "deps.queueStore",
		callSite: "daemon.go:848",
		wires:    "qs singleton → work loop queue-pull dispatch (QM-060)",
	},
	{
		symbol:   "deps.cancelOnQueueDrain",
		callSite: "daemon.go:858",
		wires:    "cfg.CancelOnQueueDrain → work loop exit-on-empty (hk-icecw)",
	},
	{
		symbol:   "deps.cancelOnQueueExit",
		callSite: "daemon.go:863",
		wires:    "cfg.CancelOnQueueExit → work loop exit-on-failure (hk-8jh26)",
	},
	{
		symbol:   "deps.stopDispatchCtx",
		callSite: "daemon.go:868",
		wires:    "cfg.StopDispatchCtx → work loop dispatch-halt ctx separate from in-flight ctx (hk-2o2i9)",
	},
	{
		symbol:   "deps.handlerPauseController",
		callSite: "daemon.go:867",
		wires:    "handlerPauseCtrl (SHARED) → work loop dispatch gate (hk-m0k0a; overrides cfg field)",
	},
	{
		symbol:   "deps.runRegistry",
		callSite: "daemon.go:876",
		wires:    "sharedRunRegistry → work loop + pause policy (same instance, hk-37zy8)",
	},
	{
		symbol:   "runWorkLoop",
		callSite: "daemon.go:883",
		wires:    "runWorkLoop(ctx, deps) → bead dispatch goroutine",
	},
}

// logCompositionRoot writes one line per composition-root wiring to w when
// HARMONIK_DEBUG_WIRING=1 is set in the process environment.  When w is nil
// it falls back to os.Stderr.
//
// Format: tab-separated columns — Symbol | CallSite | Wires.
// Output is deterministic (slice order = call-site order) so operators can
// diff across daemon versions.
//
// Bead ref: hk-4mupj.
func logCompositionRoot(w io.Writer) {
	if os.Getenv("HARMONIK_DEBUG_WIRING") != "1" {
		return
	}
	if w == nil {
		w = os.Stderr
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "daemon: composition-root wiring audit")
	fmt.Fprintln(tw, "  #\tSymbol\tCall site\tWires")
	fmt.Fprintln(tw, "  -\t------\t---------\t-----")
	for i, e := range compositionRootWirings {
		fmt.Fprintf(tw, "  %d\t%s\t%s\t%s\n", i+1, e.symbol, e.callSite, e.wires)
	}
	_ = tw.Flush()
}
