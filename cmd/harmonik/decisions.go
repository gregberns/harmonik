package main

// decisions.go — `harmonik decisions` CLI subcommand block (hitl-decisions
// SPEC §2, component K2, bead hk-xz9).
//
// K2 implements the AGENT-side verbs only:
//   - raise    → decisions-raise daemon op (emit decision_needed, print the
//                minted decision_id). With --wait, also runs the §4 blocked-wait.
//   - wait      → NO daemon op; a pure client-side subscribe stream with the
//                §4 / N8 arm-then-check ordering. Prints chosen_option (resolved)
//                or the withdrawal reason.
//   - withdraw → decisions-withdraw daemon op (emit decision_withdrawn,
//                reason=self_obsoleted by default).
//
// The OPERATOR-side verbs (list / show / answer — component K4, bead hk-kba)
// live in decisions_k4.go and are routed from runDecisionsSubcommand below. The
// orphan reaper (K5) is keeper-resident and does not live here.
//
// §4 blocked-wait contract (NORMATIVE — N8 arm-then-check, the #1 footgun):
// a subscribe stream only delivers events that arrive AFTER it is armed, so the
// wait MUST, in this exact order:
//  1. ARM a `subscribe --types decision_resolved,decision_withdrawn` stream FIRST;
//  2. THEN re-project §3 (a client-side fold over events.jsonl) for this decision_id;
//  3. if already terminal in the log, return IMMEDIATELY with the logged result
//     (do NOT block) — this catches an answer that landed before the arm;
//  4. else BLOCK on the armed stream, apply the FIRST matching terminal (N3
//     first-writer-wins), dedupe on event_id (N2), print the result, exit.
//
// Getting arm-BEFORE-reproject wrong is the answer-lands-between-read-and-arm
// race → the agent waits forever. That ordering is the whole point of N8.
//
// Client-side re-project: cmd/harmonik CANNOT import internal/daemon (K3's
// decisionsProjection lives there, and the package boundary is load-bearing).
// We therefore re-implement the SAME open-set fold here over eventbus.ScanAfter
// (decisionsClientProjection below), matching K3's semantics exactly: ADD on
// decision_needed keyed by the event's own event_id; REMOVE on
// decision_resolved/decision_withdrawn keyed by payload.decision_id; dedupe on
// event_id. For `wait` we only need this decision's terminal state, so the fold
// is specialised to one decision_id (decisionTerminalInLog).
//
// Spec ref: ~/.kerf/projects/gregberns-harmonik/hitl-decisions/SPEC.md §2, §3, §4, §6.
// Bead ref: hk-xz9 (component K2).

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// runDecisionsSubcommand routes `harmonik decisions <verb> [args]`.
// subArgs is os.Args[2:].
func runDecisionsSubcommand(subArgs []string) int {
	verb := ""
	if len(subArgs) > 0 {
		verb = subArgs[0]
	}

	switch verb {
	case "", "--help", "-h":
		decisionsUsage()
		return 0
	case "raise":
		return runDecisionsRaiseSubcommand(subArgs[1:])
	case "wait":
		return runDecisionsWaitSubcommand(subArgs[1:])
	case "withdraw":
		return runDecisionsWithdrawSubcommand(subArgs[1:])
	// list / show / answer are the OPERATOR side (component K4, bead hk-kba).
	case "list":
		return runDecisionsListSubcommand(subArgs[1:])
	case "show":
		return runDecisionsShowSubcommand(subArgs[1:])
	case "answer":
		return runDecisionsAnswerSubcommand(subArgs[1:])
	default:
		fmt.Fprintf(os.Stderr, "harmonik decisions: unrecognised verb %q; verbs are: raise, wait, withdraw, list, show, answer\n", verb)
		return 2
	}
}

// -----------------------------------------------------------------------------
// raise
// -----------------------------------------------------------------------------

// runDecisionsRaiseSubcommand implements `harmonik decisions raise`.
// subArgs is os.Args[3:].
func runDecisionsRaiseSubcommand(subArgs []string) int {
	questionFlag := ""
	var optionFlags []string
	contextFlag := ""
	fromFlag := ""
	waitFlag := false
	socketFlag := ""
	projectFlag := ""
	topicFlag := ""
	urgencyFlag := ""

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsRaiseUsage()
			return 0
		case arg == "--question" && i+1 < len(subArgs):
			i++
			questionFlag = subArgs[i]
		case strings.HasPrefix(arg, "--question="):
			questionFlag = strings.TrimPrefix(arg, "--question=")
		case arg == "--option" && i+1 < len(subArgs):
			i++
			optionFlags = append(optionFlags, subArgs[i])
		case strings.HasPrefix(arg, "--option="):
			optionFlags = append(optionFlags, strings.TrimPrefix(arg, "--option="))
		case arg == "--context" && i+1 < len(subArgs):
			i++
			contextFlag = subArgs[i]
		case strings.HasPrefix(arg, "--context="):
			contextFlag = strings.TrimPrefix(arg, "--context=")
		case arg == "--from" && i+1 < len(subArgs):
			i++
			fromFlag = subArgs[i]
		case strings.HasPrefix(arg, "--from="):
			fromFlag = strings.TrimPrefix(arg, "--from=")
		case arg == "--wait":
			waitFlag = true
		case arg == "--topic" && i+1 < len(subArgs):
			i++
			topicFlag = subArgs[i]
		case strings.HasPrefix(arg, "--topic="):
			topicFlag = strings.TrimPrefix(arg, "--topic=")
		case arg == "--urgency" && i+1 < len(subArgs):
			i++
			urgencyFlag = subArgs[i]
		case strings.HasPrefix(arg, "--urgency="):
			urgencyFlag = strings.TrimPrefix(arg, "--urgency=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions raise: unknown flag %q\n", arg)
			return 1
		default:
			fmt.Fprintf(os.Stderr, "harmonik decisions raise: unexpected argument %q\n", arg)
			return 1
		}
	}

	// Validate: --question required, ≥1 --option required (N7-checkability).
	if questionFlag == "" {
		fmt.Fprintf(os.Stderr, "harmonik decisions raise: --question is required\n")
		return 1
	}
	if len(optionFlags) < 1 {
		fmt.Fprintf(os.Stderr, "harmonik decisions raise: at least one --option is required\n")
		return 1
	}
	if !core.DecisionUrgency(urgencyFlag).Valid() {
		fmt.Fprintf(os.Stderr, "harmonik decisions raise: --urgency %q is invalid (must be blocker, question, or fyi)\n", urgencyFlag)
		return 1
	}

	// Resolve emitting agent: --from > $HARMONIK_AGENT (may be empty — blocked_agent
	// is optional per SPEC §1.1, but a wait without a known agent still works).
	from := fromFlag
	if from == "" {
		from = os.Getenv("HARMONIK_AGENT")
	}

	absProject, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, "raise")
	if rc != 0 {
		return rc
	}

	// Build the decisions-raise request payload.
	raisePayload := map[string]any{
		"question": questionFlag,
		"options":  optionFlags,
	}
	if contextFlag != "" {
		raisePayload["context_link"] = contextFlag
	}
	if from != "" {
		raisePayload["blocked_agent"] = from
	}
	if topicFlag != "" {
		raisePayload["topic"] = topicFlag
	}
	if urgencyFlag != "" {
		raisePayload["urgency"] = urgencyFlag
	}

	resultBytes, rc := decisionsDialOp(sockPath, "decisions-raise", raisePayload, "raise")
	if rc != 0 {
		return rc
	}

	var result struct {
		DecisionID string `json:"decision_id"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions raise: decode result: %v\n", err)
		return 1
	}
	if result.DecisionID == "" {
		fmt.Fprintf(os.Stderr, "harmonik decisions raise: daemon returned empty decision_id\n")
		return 1
	}

	// Print the minted decision_id (the value the agent waits on).
	fmt.Println(result.DecisionID)

	// --wait: hold the §4 blocked-wait and print the chosen_option (or withdrawal).
	if waitFlag {
		return decisionsBlockedWait(absProject, sockPath, result.DecisionID)
	}
	return 0
}

// -----------------------------------------------------------------------------
// wait
// -----------------------------------------------------------------------------

// runDecisionsWaitSubcommand implements `harmonik decisions wait <decision_id>`.
// subArgs is os.Args[3:].
func runDecisionsWaitSubcommand(subArgs []string) int {
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsWaitUsage()
			return 0
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions wait: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik decisions wait: exactly one <decision_id> argument is required\n")
		return 1
	}
	decisionID := positional[0]

	absProject, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, "wait")
	if rc != 0 {
		return rc
	}

	return decisionsBlockedWait(absProject, sockPath, decisionID)
}

// -----------------------------------------------------------------------------
// withdraw
// -----------------------------------------------------------------------------

// runDecisionsWithdrawSubcommand implements `harmonik decisions withdraw <id>`.
// subArgs is os.Args[3:].
func runDecisionsWithdrawSubcommand(subArgs []string) int {
	reasonFlag := string(core.DecisionWithdrawnReasonSelfObsoleted) // default
	fromFlag := ""
	socketFlag := ""
	projectFlag := ""
	var positional []string

	for i := 0; i < len(subArgs); i++ {
		arg := subArgs[i]
		switch {
		case arg == "--help" || arg == "-h":
			decisionsWithdrawUsage()
			return 0
		case arg == "--reason" && i+1 < len(subArgs):
			i++
			reasonFlag = subArgs[i]
		case strings.HasPrefix(arg, "--reason="):
			reasonFlag = strings.TrimPrefix(arg, "--reason=")
		case arg == "--from" && i+1 < len(subArgs):
			i++
			fromFlag = subArgs[i]
		case strings.HasPrefix(arg, "--from="):
			fromFlag = strings.TrimPrefix(arg, "--from=")
		case arg == "--socket" && i+1 < len(subArgs):
			i++
			socketFlag = subArgs[i]
		case strings.HasPrefix(arg, "--socket="):
			socketFlag = strings.TrimPrefix(arg, "--socket=")
		case arg == "--project" && i+1 < len(subArgs):
			i++
			projectFlag = subArgs[i]
		case strings.HasPrefix(arg, "--project="):
			projectFlag = strings.TrimPrefix(arg, "--project=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "harmonik decisions withdraw: unknown flag %q\n", arg)
			return 1
		default:
			positional = append(positional, arg)
		}
	}

	if len(positional) != 1 {
		fmt.Fprintf(os.Stderr, "harmonik decisions withdraw: exactly one <decision_id> argument is required\n")
		return 1
	}
	decisionID := positional[0]

	// Validate the reason against the K1 enum (mirrors DecisionWithdrawnReason.Valid).
	// The agent-side default and intended use is self_obsoleted; the keeper-only
	// "orphaned" reason (N9) is not the agent's to emit, but we accept any valid
	// enum value and let the daemon's Valid() be the single gate.
	if !core.DecisionWithdrawnReason(reasonFlag).Valid() {
		fmt.Fprintf(os.Stderr, "harmonik decisions withdraw: --reason %q is invalid (must be self_obsoleted or orphaned)\n", reasonFlag)
		return 1
	}

	from := fromFlag
	if from == "" {
		from = os.Getenv("HARMONIK_AGENT")
	}

	_, sockPath, rc := decisionsResolvePaths(projectFlag, socketFlag, "withdraw")
	if rc != 0 {
		return rc
	}

	withdrawPayload := map[string]any{
		"decision_id": decisionID,
		"reason":      reasonFlag,
	}
	if from != "" {
		withdrawPayload["by"] = from
	}

	resultBytes, rc := decisionsDialOp(sockPath, "decisions-withdraw", withdrawPayload, "withdraw")
	if rc != 0 {
		return rc
	}

	var result struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions withdraw: decode result: %v\n", err)
		return 1
	}
	fmt.Println(result.EventID)
	return 0
}

// -----------------------------------------------------------------------------
// §4 blocked-wait — N8 arm-then-check
// -----------------------------------------------------------------------------

// decisionsBlockedWait runs the §4 blocked-wait for decisionID with the N8
// arm-then-check ordering:
//
//  1. ARM a subscribe stream (types decision_resolved,decision_withdrawn) FIRST.
//  2. THEN re-project the durable log for this decision_id.
//  3. If already terminal, print the logged result and return immediately.
//  4. Else block on the armed stream, apply the first matching terminal (N3),
//     dedupe on event_id (N2), print, and return.
//
// Prints `chosen_option` on resolve, or `withdrawn: <reason>` on withdrawal.
// Returns exit 0 on a terminal, 17 if the daemon socket is absent, 1 on error.
func decisionsBlockedWait(absProject, sockPath, decisionID string) int {
	eventsPath := filepath.Join(absProject, ".harmonik", "events", "events.jsonl")

	// Step 1: ARM the subscribe stream FIRST (live-only — no since_event_id, so
	// it delivers only events that arrive AFTER this arm). Connecting here, before
	// the re-project below, is the load-bearing N8 ordering.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	conn, rc := decisionsArmSubscribe(ctx, sockPath)
	if rc != 0 {
		return rc
	}
	defer func() { _ = conn.Close() }()

	// Step 2 + 3: re-project the durable log for this decision_id. If a terminal
	// is already logged (the answer landed before/at our arm), return immediately.
	if term, ok := decisionTerminalInLog(eventsPath, decisionID); ok {
		return decisionsPrintTerminal(term)
	}

	// Step 4: block on the armed stream until the matching terminal arrives.
	// Dedupe on event_id (N2); apply the first matching terminal (N3).
	seen := make(map[string]struct{})
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		// Heartbeat lines carry "type":"heartbeat" and no decision payload; the
		// core.Event decode below yields a non-terminal type and is skipped.
		var evt core.Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		evID := evt.EventID.String()
		if _, dup := seen[evID]; dup {
			continue // N2 dedupe
		}
		seen[evID] = struct{}{}

		term, matched := decisionTerminalFromEvent(evt, decisionID)
		if !matched {
			continue
		}
		return decisionsPrintTerminal(term)
	}
	if err := sc.Err(); err != nil {
		// A closed connection (signal / daemon exit) is a clean stop; otherwise error.
		if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed") {
			return 0
		}
		fmt.Fprintf(os.Stderr, "harmonik decisions wait: stream read: %v\n", err)
		return 1
	}
	// Stream closed (EOF) without a terminal — daemon went away or signalled.
	return 0
}

// decisionTerminal is the resolved/withdrawn outcome of one decision, as read
// either from the durable log (re-project) or from a live stream event.
type decisionTerminal struct {
	// Resolved is true for a decision_resolved terminal, false for withdrawn.
	Resolved bool
	// ChosenOption is set when Resolved.
	ChosenOption string
	// Reason is set when withdrawn.
	Reason string
}

// decisionsPrintTerminal prints a terminal and returns exit 0.
func decisionsPrintTerminal(t decisionTerminal) int {
	if t.Resolved {
		fmt.Println(t.ChosenOption)
	} else {
		fmt.Printf("withdrawn: %s\n", t.Reason)
	}
	return 0
}

// decisionTerminalFromEvent reports whether evt is a terminal (decision_resolved
// or decision_withdrawn) for decisionID, and if so returns its outcome.
func decisionTerminalFromEvent(evt core.Event, decisionID string) (decisionTerminal, bool) {
	switch evt.Type {
	case string(core.EventTypeDecisionResolved):
		var p core.DecisionResolvedPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return decisionTerminal{}, false
		}
		if p.DecisionID != decisionID {
			return decisionTerminal{}, false
		}
		return decisionTerminal{Resolved: true, ChosenOption: p.ChosenOption}, true
	case string(core.EventTypeDecisionWithdrawn):
		var p core.DecisionWithdrawnPayload
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			return decisionTerminal{}, false
		}
		if p.DecisionID != decisionID {
			return decisionTerminal{}, false
		}
		return decisionTerminal{Resolved: false, Reason: string(p.Reason)}, true
	}
	return decisionTerminal{}, false
}

// decisionTerminalInLog folds events.jsonl (a single forward ScanAfter scan) to
// find whether decisionID has already reached a terminal, returning the FIRST
// terminal for it (N3 first-writer-wins) if so.
//
// This is the client-side re-project for the §4 step-2 check. It is a
// specialised form of K3's decisionsProjection (internal/daemon —
// un-importable here): it tracks only the one decision_id rather than the full
// open set, but applies the SAME semantics — terminals key on payload.decision_id,
// dedupe on event_id (N2), first-writer-wins (N3). It returns as soon as the
// first matching terminal is seen.
//
// The decision_needed event for decisionID need not be scanned for the wait
// check: we only care whether a terminal exists. (decisionsClientProjection
// below carries the full open-set fold for any future K2-side use; the wait
// path uses this lighter scan.)
func decisionTerminalInLog(eventsPath, decisionID string) (decisionTerminal, bool) {
	var zeroID core.EventID
	seen := make(map[string]struct{})
	for evt := range eventbus.ScanAfter(eventsPath, zeroID) {
		evID := evt.EventID.String()
		if _, dup := seen[evID]; dup {
			continue // N2 dedupe
		}
		seen[evID] = struct{}{}
		if term, ok := decisionTerminalFromEvent(evt, decisionID); ok {
			return term, true // N3: first terminal wins
		}
	}
	return decisionTerminal{}, false
}

// -----------------------------------------------------------------------------
// socket helpers
// -----------------------------------------------------------------------------

// decisionsResolvePaths resolves the project dir (absolute) and the socket path
// from the --project / --socket flags, defaulting to cwd and
// <project>/.harmonik/daemon.sock. verb is used only for error messages.
func decisionsResolvePaths(projectFlag, socketFlag, verb string) (absProject, sockPath string, rc int) {
	projectDir := projectFlag
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "harmonik decisions %s: cannot determine cwd: %v\n", verb, err)
			return "", "", 1
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: cannot resolve project path: %v\n", verb, err)
		return "", "", 1
	}
	sock := socketFlag
	if sock == "" {
		sock = filepath.Join(abs, ".harmonik", "daemon.sock")
	}
	return abs, sock, 0
}

// decisionsDialOp dials the daemon socket, sends a {op,payload} request, reads
// the SocketResponse, and returns the raw Result on success. Returns exit 17 if
// the socket is absent/refused (mirrors comms.go), 1 on any other error.
func decisionsDialOp(sockPath, op string, payload map[string]any, verb string) (json.RawMessage, int) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: marshal payload: %v\n", verb, err)
		return nil, 1
	}
	reqBytes, err := json.Marshal(map[string]any{
		"op":      op,
		"payload": json.RawMessage(payloadBytes),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: marshal request: %v\n", verb, err)
		return nil, 1
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik decisions %s: daemon not running (socket %s missing or refused)\n", verb, sockPath)
			return nil, 17
		}
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: dial %s: %v\n", verb, sockPath, dialErr)
		return nil, 1
	}
	defer func() { _ = conn.Close() }()

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: write request: %v\n", verb, writeErr)
		return nil, 1
	}
	if uw, ok := conn.(*net.UnixConn); ok {
		_ = uw.CloseWrite()
	}

	var resp struct {
		Ok     bool            `json:"ok"`
		Result json.RawMessage `json:"result,omitempty"`
		Error  string          `json:"error,omitempty"`
	}
	if decErr := json.NewDecoder(conn).Decode(&resp); decErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: decode response: %v\n", verb, decErr)
		return nil, 1
	}
	if !resp.Ok {
		fmt.Fprintf(os.Stderr, "harmonik decisions %s: %s\n", verb, resp.Error)
		return nil, 1
	}
	return resp.Result, 0
}

// decisionsArmSubscribe dials the daemon and sends a live-only subscribe request
// for the two decision terminals. It returns the open connection (the caller
// reads NDJSON event lines from it) on success. No since_event_id is set — the
// stream is deliberately live-only so the N8 re-project below catches anything
// already logged.
//
// The server-side heartbeat (default 60s) keeps the stream — and thus the
// agent's keeper gauge — fresh while blocked (SPEC §4 keeper-alive).
func decisionsArmSubscribe(ctx context.Context, sockPath string) (net.Conn, int) {
	reqBytes, err := json.Marshal(map[string]any{
		"op":                "subscribe",
		"types":             []string{"decision_resolved", "decision_withdrawn"},
		"heartbeat_seconds": 60,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik decisions wait: marshal subscribe request: %v\n", err)
		return nil, 1
	}

	dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
	conn, dialErr := (&net.Dialer{}).DialContext(dialCtx, "unix", sockPath)
	cancelDial()
	if dialErr != nil {
		if commsIsSocketAbsent(dialErr) || commsIsConnRefused(dialErr) {
			fmt.Fprintf(os.Stderr, "harmonik decisions wait: daemon not running (socket %s missing or refused)\n", sockPath)
			return nil, 17
		}
		fmt.Fprintf(os.Stderr, "harmonik decisions wait: dial %s: %v\n", sockPath, dialErr)
		return nil, 1
	}

	if _, writeErr := conn.Write(reqBytes); writeErr != nil {
		_ = conn.Close()
		fmt.Fprintf(os.Stderr, "harmonik decisions wait: write subscribe request: %v\n", writeErr)
		return nil, 1
	}

	// Close conn on signal so the blocking scan unblocks and we exit cleanly.
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	return conn, 0
}

// -----------------------------------------------------------------------------
// usage
// -----------------------------------------------------------------------------

func decisionsUsage() {
	fmt.Print(`harmonik decisions — agent→human decision surface

USAGE
  harmonik decisions <verb> [flags]

VERBS (agent side)
  raise     Emit a decision_needed event; print the minted decision_id.
            With --wait, block until answered and print the chosen option.
  wait      Block until a decision's terminal arrives; print the chosen option
            (resolved) or the withdrawal reason. Holds an open subscribe stream.
  withdraw  Emit a decision_withdrawn(self_obsoleted) — the agent cancels its
            own open decision.

VERBS (operator side)
  list      Show every open decision across all agents (the what-needs-me queue):
            question · options · blocked_agent · context_link · decision_id.
            An open decision whose blocked_agent is Offline is flagged
            "orphaned-pending" (display only). --json for machine-readable output.
  show      Show one decision by id (list filtered to a single decision_id).
  answer    Resolve an open decision: emit decision_resolved with the chosen
            option (must be one of the decision's options). A no-op on an
            unknown or already-answered decision_id.

EXIT CODES
  0   Success
  1   Argument error or op rejected
  2   Unrecognised verb
  17  Daemon not running (socket missing or ECONNREFUSED)

EXAMPLES
  harmonik decisions raise --question "Ship v2?" --option ship --option hold
  harmonik decisions raise --question "Ship v2?" --option ship --option hold --wait
  harmonik decisions wait 0192f5a1-...
  harmonik decisions withdraw 0192f5a1-... --reason self_obsoleted
  harmonik decisions list
  harmonik decisions list --json
  harmonik decisions show 0192f5a1-...
  harmonik decisions answer 0192f5a1-... ship --resolver operator
`)
}

func decisionsRaiseUsage() {
	fmt.Print(`harmonik decisions raise — emit a decision_needed event

USAGE
  harmonik decisions raise --question "..." --option A --option B [--option ...]
                           [--context <link>] [--from <agent>] [--wait]
                           [--topic <topic>] [--urgency blocker|question|fyi] [flags]

FLAGS
  --question TEXT   The decision the human must make. Required.
  --option VALUE    An enumerated choice. Repeatable; at least one required.
  --context LINK    Free-form context pointer (bead id / codename / run_id).
  --from NAME       Emitting (blocked) agent name (default: $HARMONIK_AGENT).
  --wait            After raising, block until the decision is answered (§4 N8
                    arm-then-check) and print the chosen option (or withdrawal).
  --topic TOPIC     Optional routing tag. Use "operator-mailbox" to route this
                    decision into the operator mailbox (harmonik mailbox /
                    decisions list --topic operator-mailbox).
  --urgency LEVEL   Optional operator-mailbox-flavor hint: blocker, question,
                    or fyi. Rejected if set to anything else.
  --socket PATH     Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR     Project directory (default: cwd).

EXIT CODES
  0   Success (decision_id printed; with --wait, then the chosen option)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}

func decisionsWaitUsage() {
	fmt.Print(`harmonik decisions wait — block until a decision's terminal arrives

USAGE
  harmonik decisions wait <decision_id> [--socket PATH] [--project DIR]

Holds an open subscribe stream (types decision_resolved,decision_withdrawn) with
the §4 N8 arm-then-check ordering: arm the stream FIRST, then re-project the log;
if already terminal, return immediately; else block. Prints the chosen option on
resolve, or "withdrawn: <reason>" on withdrawal. Dedupes on event_id (N2);
applies the first terminal (N3 first-writer-wins).

EXIT CODES
  0   A terminal arrived (or the stream closed cleanly)
  1   Argument error or read failure
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}

func decisionsWithdrawUsage() {
	fmt.Print(`harmonik decisions withdraw — cancel your own open decision

USAGE
  harmonik decisions withdraw <decision_id> [--reason self_obsoleted] [--from NAME] [flags]

FLAGS
  --reason REASON   Withdrawal reason (default: self_obsoleted). Must be
                    self_obsoleted or orphaned. The keeper is the sole emitter of
                    "orphaned" withdrawals (N9) — agents use self_obsoleted.
  --from NAME       Agent name recorded as the withdrawer (default: $HARMONIK_AGENT).
  --socket PATH     Override socket path (default: <project>/.harmonik/daemon.sock).
  --project DIR     Project directory (default: cwd).

EXIT CODES
  0   Success (event_id printed)
  1   Argument error or daemon rejected the op
  17  Daemon not running (socket missing or ECONNREFUSED)
`)
}
