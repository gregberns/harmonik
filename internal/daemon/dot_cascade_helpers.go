package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/gitprobe"
	"github.com/gregberns/harmonik/internal/handler"
	"github.com/gregberns/harmonik/internal/handlercontract"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	"github.com/gregberns/harmonik/internal/runloop"
	"github.com/gregberns/harmonik/internal/substrate"
	"github.com/gregberns/harmonik/internal/workflow"
	"github.com/gregberns/harmonik/internal/workflow/dot"
	"github.com/gregberns/harmonik/internal/workspace"
)

func resolveDotWorktreeHEAD(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	if runner == nil {
		return gitprobe.ResolveWorktreeHEAD(ctx, wtPath)
	}
	return gitprobe.ResolveWorktreeHEADVia(ctx, runner, wtPath)
}

func dotNodeTerminalFailure(
	sessionID string,
	exit runloop.ExitInfo,
	socketOutcome *handler.ExportedOutcomeEmittedPayload,
	watcherErr error,
) (string, bool) {
	term := handler.MapWaitReturnToTerminalEvent(sessionID, exit.ExitCode, exit.WaitErr, socketOutcome)
	watcherFailed := watcherErr != nil && !isWatcherErrCanceled(watcherErr)

	if term.Type == handlercontract.ProgressMsgTypeAgentCompleted {
		return "", false
	}
	cleanExit := exit.ExitCode == exitCodeClean || exit.AgentAnnouncedEnd
	if !outcomeIsAnAgentReport(socketOutcome) && cleanExit && !watcherFailed {
		return "", false
	}

	switch {
	case watcherFailed:
		return fmt.Sprintf("watcher error: %v exit=%d", watcherErr, exit.ExitCode), true
	case term.SubReason != "":
		return fmt.Sprintf("agent_failed class=%s sub_reason=%s exit=%d", term.Class, term.SubReason, exit.ExitCode), true
	default:
		return fmt.Sprintf("exit=%d", exit.ExitCode), true
	}
}

func dotNoHeadAdvanceReason(exit runloop.ExitInfo, captureDir, preHeadSHA string) string {
	reason := fmt.Sprintf("exited without advancing HEAD past %s", preHeadSHA)
	if exit.AgentAnnouncedEnd {
		reason = fmt.Sprintf("announced the end of its turn and committed nothing; HEAD is still %s", preHeadSHA)
	}
	if captureDir == "" {
		return reason
	}
	return reason + fmt.Sprintf(" — the agent's captured output is under %s/", captureDir)
}

func outcomeIsAnAgentReport(outcome *handler.ExportedOutcomeEmittedPayload) bool {
	if outcome == nil {
		return false
	}
	switch outcome.Kind {
	case "WORK_COMPLETE", "REVIEWER_VERDICT", "FAILURE_SIGNAL":
		return true
	default:
		return false
	}
}

var errDotNoChangeSubsumed = errors.New("dot: noChange-subsumed: work already merged on the branch this run lands on")

var errDotReviewerNoVerdict = errors.New("dot: reviewer node produced no verdict")

const dotMaxNodeVisits = 64

type dotWorkflowResult struct {
	// success is true when the walk reached the success terminal node.
	success bool

	// terminalNodeID is the terminal node the walk reached (empty on a
	// non-terminal failure such as a cascade structural failure or a gate node).
	terminalNodeID string

	// needsAttention is true when the run terminated on a non-success path that
	// requires operator attention (BLOCK, cap-hit, no-progress, or a gate/
	// sub-workflow out-of-scope failure).
	needsAttention bool

	// summary is a short human-readable explanation for run_completed/run_failed.
	summary string

	// subsumed is true when the implementer exited without advancing HEAD and the
	// bead's work was already merged on the branch this run lands on
	// (noChange-subsumed). The caller closes the bead rather than reopening it.
	subsumed bool

	// advisoryRC is true when the cascade completed via the advisory-RC exemption
	// (hk-w2ow): REQUEST_CHANGES was advisory-only, committed work exists, gate
	// green, HEAD final. The caller uses this to reconcile-close on
	// rebase_dropped_commits instead of re-queuing — preventing the infinite
	// re-dispatch loop where work already merged in a prior run is re-identified
	// as advisory-RC and re-queued (hk-whru3).
	advisoryRC bool

	// approveVerdict carries the APPROVE verdict when the cascade reaches a
	// successful terminal or the approved-and-done salvage path. Nil when no
	// reviewer approved the work, the verdict cannot be read, or salvage did not
	// follow an approval.
	// The caller (workloop.go) uses this to stamp Reviewed-By / Review-Verdict
	// trailers on the HEAD commit before merging, mirroring the review-loop path
	// (hk-tnui).
	approveVerdict *workspace.ReviewVerdict
}

func nodeModelForHarness(resolvedModel, nodeModelAttr string, effHarness core.AgentType) string {
	if nodeModelAttr != "" && effHarness == core.AgentTypeClaudeCode {
		return nodeModelAttr
	}
	return resolvedModel
}

func readDotReviewVerdictRetry(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.ReviewVerdict, error) {
	if gitprobe.RunnerIsLocalFS(runner) {
		return workspace.ReadReviewVerdictLocalRetry(ctx, wtPath)
	}
	return workspace.ReadReviewVerdictVia(ctx, runner, wtPath)
}

func readDotReviewerBudgetSentinel(ctx context.Context, runner tmux.CommandRunner, wtPath, nodeID string) (*reviewerBudgetSentinel, error) {
	sentinel, err := ReadReviewerBudgetSentinelVia(ctx, runner, wtPath)
	if err != nil {
		return nil, fmt.Errorf("read reviewer budget sentinel for node %q: %w", nodeID, err)
	}
	return sentinel, nil
}

func runAutoStatusInspection(ctx context.Context, runner tmux.CommandRunner, wtPath string) (core.Outcome, bool) {
	if !autoStatusHasGoMod(ctx, runner, wtPath) {
		return core.Outcome{}, true // no go.mod: pass through
	}
	fc := core.FailureClassDeterministic
	for _, buildArgs := range [][]string{
		{"build", "./..."},
		{"vet", "./..."},
	} {
		var cmd *exec.Cmd
		if runner == nil {
			cmd = exec.CommandContext(ctx, "go", buildArgs...)
			cmd.Dir = wtPath
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.Cancel = func() error {
				killProcessGroup(cmd.Process, "auto-status inspection")
				return nil
			}
			cmd.WaitDelay = 5 * time.Second
		} else {
			cmd = runner.Command(ctx, "/bin/sh", "-lc",
				fmt.Sprintf("cd %s && go %s", shellQuote(wtPath), strings.Join(buildArgs, " ")))
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return core.Outcome{
				Status:       core.OutcomeStatusFail,
				Kind:         core.OutcomeKindDefault,
				FailureClass: &fc,
				Notes:        fmt.Sprintf("auto_status inspection failed (go %s): %v\n%s", buildArgs[0], err, string(out)),
			}, false
		}
	}
	marker := readAutoStatusMarkerOrReport(ctx, runner, wtPath)
	if marker != nil {
		c2fc := core.FailureClassDeterministic // HC-059 daemon back-fill when hint absent.
		if marker.FailureClass != "" {
			c2fc = core.FailureClass(marker.FailureClass)
		}
		return core.Outcome{
			Status:       core.OutcomeStatusFail,
			Kind:         core.OutcomeKindDefault,
			FailureClass: &c2fc,
			Notes:        marker.Notes,
		}, false
	}
	return core.Outcome{}, true
}

func autoStatusHasGoMod(ctx context.Context, runner tmux.CommandRunner, wtPath string) bool {
	if runner == nil {
		_, err := os.Stat(filepath.Join(wtPath, "go.mod"))
		return err == nil
	}
	err := runner.Command(ctx, "test", "-f", filepath.Join(wtPath, "go.mod")).Run()
	return err == nil
}

func readAutoStatusMarkerVia(ctx context.Context, runner tmux.CommandRunner, wtPath string) (*workspace.AutoStatusMarker, error) {
	return workspace.ReadAutoStatusMarkerVia(ctx, runner, wtPath)
}

func dispatchDotToolNode(ctx context.Context, bus handlercontract.EventEmitter, runID core.RunID, runner tmux.CommandRunner, projectDir, wtPath string, node *dot.Node, env []string) (core.Outcome, error) {
	timeoutSecs := 300
	if node.Timeout != "" {
		if n, err := strconv.Atoi(node.Timeout); err == nil && n > 0 {
			timeoutSecs = n
		}
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSecs)*time.Second)
	defer cancel()

	if bus != nil {
		hbDone := make(chan struct{})
		defer close(hbDone)
		gateSessionID := string(handlercontract.NewSessionID())
		go handler.RunHeartbeatLoop(execCtx, gateSessionID,
			dotGateHeartbeatInterval, hbDone,
			newDaemonHeartbeatEmitter(bus, runID))
	}

	var cmd *exec.Cmd
	if runner == nil {
		cmd = exec.CommandContext(execCtx, "/bin/sh", "-c", node.ToolCommand)
		cmd.Dir = wtPath
		cmd.Env = append(os.Environ(), env...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			killProcessGroup(cmd.Process, "commit gate")
			return nil
		}
		cmd.WaitDelay = 5 * time.Second
	} else {
		var sb strings.Builder
		for _, kv := range env {
			if idx := strings.IndexByte(kv, '='); idx >= 0 {
				sb.WriteString("export ")
				sb.WriteString(kv[:idx])
				sb.WriteByte('=')
				sb.WriteString(shellQuote(kv[idx+1:]))
				sb.WriteString("; ")
			}
		}
		sb.WriteString("cd ")
		sb.WriteString(shellQuote(wtPath))
		sb.WriteString(" && ")
		sb.WriteString(node.ToolCommand)
		cmd = runner.Command(execCtx, "/bin/sh", "-lc", sb.String())
	}

	combined, err := cmd.CombinedOutput()
	if err == nil {
		return core.Outcome{Status: core.OutcomeStatusSuccess}, nil
	}

	gateLogPath := filepath.Join(wtPath, ".harmonik", "commit-gate.log")
	if runner == nil {
		writeGateLog(gateLogPath, combined)
	}

	if archived := appendGateLogArchive(projectDir, runID, node.ID, combined, err); archived != "" {
		gateLogPath = archived
	}

	outputTail := tailString(string(combined), dotGateOutputTailBytes)

	fc, logDesc := classifyDotToolNodeFailure(err, execCtx.Err(), ctx.Err(), combined, node.ID, timeoutSecs, runner != nil)
	if logDesc != "" {
		fmt.Fprintf(os.Stderr, "daemon: %s; gate log: %s\n", logDesc, gateLogPath)
	}
	return core.Outcome{Status: core.OutcomeStatusFail, FailureClass: &fc, Notes: outputTail}, nil
}

func classifyDotToolNodeFailure(err, execCtxErr, runCtxErr error, combined []byte, nodeID string, timeoutSecs int, remote bool) (class core.FailureClass, logDesc string) {
	if errors.Is(execCtxErr, context.DeadlineExceeded) {
		return core.FailureClassTransient, fmt.Sprintf("dot tool node %q timed out after %ds", nodeID, timeoutSecs)
	}

	// Parent context cancelled (operator stop / SIGKILL / ctx-cancel).
	//
	// hk-vjsv Fix (B) — gate-shell teardown on run re-dispatch / teardown:
	//   LOCAL (runner == nil): the gate runs under exec.CommandContext(execCtx, …);
	//   when the per-run context is cancelled (the run is torn down or re-dispatched
	//   after run_stale), Go kills the local shell process automatically, so no
	//   local gate shell leaks. Confirmed by the canceled-path test
	//   (dot_cascade_tool_hkcucz6_test.go).
	//
	//   REMOTE (runner != nil, SSHRunner): exec.CommandContext kills only the LOCAL
	//   `ssh` client on ctx-cancel; the remote `/bin/sh -lc '… go build/test …'`
	//   process tree on the WORKER is orphaned (classic SSH no-PTY orphan), which is
	//   the pile-up of concurrent full build+scenario runs the bug observed. Fix (A)
	//   removes the ROOT trigger (run_stale no longer false-fires for a healthy
	//   gate, so the run is not re-dispatched out from under a live gate), so this
	//   residual only bites on a GENUINE teardown of a still-running remote gate.
	//   TODO(hk-vjsv): make the remote gate killable — allocate a PTY (`ssh -tt`) so
	//   the worker shell dies with the client, or run the gate inside a wrapper that
	//   records its remote PID/PGID and have SSHRunner-aware teardown `ssh <host>
	//   kill -TERM -<pgid>` on ctx-cancel. Threading this needs the same
	//   runner/worker-session plumbing dispatchDotGateNode's cognition path is
	//   already missing (see TODO(hk-538l) in dot_gate.go); do it once for both.
	if runCtxErr != nil {
		return core.FailureClassCanceled, ""
	}

	if sigDesc, killed := gateKilledBySignal(err, combined); killed {
		return core.FailureClassCanceled, fmt.Sprintf("dot tool node %q was KILLED mid-flight (%s) — it reached no verdict, so this is NOT a test failure; canceled, routed to close-needs-attention for triage", nodeID, sigDesc)
	}

	if remote && tmux.IsSSHConnectionFailure(err) {
		return core.FailureClassCanceled, fmt.Sprintf("dot tool node %q ran on a worker and ssh exited 255 — the transport failed or the remote gate died from a signal; it reached no verdict, so this is NOT a test failure; canceled, routed to close-needs-attention for triage", nodeID)
	}

	if isGateBuildCacheInfraError(combined) {
		return core.FailureClassTransient, fmt.Sprintf("dot tool node %q failed (%v) with build-cache infra signature (transient)", nodeID, err)
	}

	if isGateCannotRunError(combined) {
		return core.FailureClassStructural, fmt.Sprintf("dot tool node %q could not RUN — a command it names does not exist (%v); structural, NOT routed back to the implementer", nodeID, err)
	}

	return core.FailureClassDeterministic, fmt.Sprintf("dot tool node %q failed (%v)", nodeID, err)
}

const dotGateOutputTailBytes = 4096

var dotGateHeartbeatInterval = handler.HeartbeatInterval

func shellQuote(s string) string {
	return workflow.ShellQuote(s)
}

func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[len(s)-n:]
	for i := 0; i < len(cut) && i < 4; i++ {
		if utf8.RuneStart(cut[i]) {
			cut = cut[i:]
			break
		}
	}
	return "…(truncated)…\n" + cut
}

type gateOutputSignature struct {
	// text is what the detector matches, anywhere in the gate's output.
	text string
	// quoted is what gateEvidenceQuote writes in text's place. It MUST NOT
	// contain text — otherwise the rewrite buys nothing — and it MUST still
	// read as the same fact to a human, because these strings are the whole
	// content of a diagnostic somebody has to act on.
	quoted string
}

var gateBuildCacheInfraSignatures = []gateOutputSignature{
	{text: "is not in std", quoted: "is not part of std"},
}

var gateCannotRunSignatures = []gateOutputSignature{
	{text: "] Error 127", quoted: "] Error code 127"},
	{text: ": command not found", quoted: ": command was not found"},
}

var gateUnscopedSignatures = slices.Concat(gateBuildCacheInfraSignatures, gateCannotRunSignatures)

func gateOutputHasAnySignature(output string, sigs []gateOutputSignature) bool {
	for _, sig := range sigs {
		if strings.Contains(output, sig.text) {
			return true
		}
	}
	return false
}

func isGateBuildCacheInfraError(output []byte) bool {
	return gateOutputHasAnySignature(string(output), gateBuildCacheInfraSignatures)
}

func isGateCannotRunError(output []byte) bool {
	return gateOutputHasAnySignature(string(output), gateCannotRunSignatures)
}

func gateBackEdgeMessage(class core.FailureClass, notes string) string {
	quoted := gateEvidenceQuote(notes)
	if class == core.FailureClassDeterministic {
		return "The commit gate failed — your commit was recorded but the build/test gate did not pass. " +
			"Fix the failure and re-commit:\n\n" + quoted
	}
	return "The commit gate did not finish — your commit was recorded, but the gate stopped before it " +
		"reached a verdict, so NOTHING is known to be wrong with your change. Do not invent a fix and do " +
		"not rewrite working code. Re-read .harmonik/agent-task.md, confirm your work is complete and " +
		"committed, and change something only if it is genuinely missing. The last output the gate " +
		"produced follows, for context only — it is a partial log, not a failure report:\n\n" + quoted
}

func gateKilledBySignal(err error, output []byte) (string, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ProcessState != nil {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return "killed by signal " + ws.Signal().String(), true
		}
	}
	if line, ok := gateSignalKillOutputLine(output); ok {
		if gateJoinBuildsASignature(gateKillOutputPrefix, line) {
			line = "…" + line
		}
		return gateKillOutputPrefix + line, true
	}
	return "", false
}

const gateKillOutputPrefix = "gate output reports a signal kill: "

func gateSignalKillOutputLine(output []byte) (string, bool) {
	for _, line := range gateTerminalRecipeFailures(output) {
		if gateRecipeLineNamesAKill(line) {
			return gateEvidenceQuote(line), true
		}
	}
	return "", false
}

func gateLineIsIndented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

const gateRecipeFailureAnchor = "*** ["

const gateCascadeGapLines = 4

func gateTerminalRecipeFailures(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	var reversed []string
	gap := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], gateRecipeFailureAnchor) && !gateLineIsIndented(lines[i]) {
			reversed = append(reversed, strings.TrimSpace(lines[i]))
			gap = 0
			continue
		}
		gap++
		if gap > gateCascadeGapLines {
			break
		}
	}
	cascade := make([]string, 0, len(reversed))
	for i := len(reversed) - 1; i >= 0; i-- {
		cascade = append(cascade, reversed[i])
	}
	return cascade
}

var gateCascadeKillWords = []string{"Terminated", "Killed", "Interrupt", "Hangup"}

func gateRecipeLineNamesAKill(line string) bool {
	for _, word := range gateCascadeKillWords {
		if strings.Contains(line, word) {
			return true
		}
	}
	return gateRecipeExitCodeIsSignalDeath(line)
}

func gateRecipeExitCodeIsSignalDeath(line string) bool {
	const marker = "] Error "
	i := strings.LastIndex(line, marker)
	if i < 0 {
		return false
	}
	rest := line[i+len(marker):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return false
	}
	code, err := strconv.Atoi(rest[:end])
	if err != nil {
		return false
	}
	return code >= 129 && code <= 255
}

const gateRecipeFailureAnchorQuoted = "recipe ["

func gateEvidenceQuote(text string) string {
	out := strings.ReplaceAll(text, gateRecipeFailureAnchor, gateRecipeFailureAnchorQuoted)
	for _, sig := range gateUnscopedSignatures {
		out = strings.ReplaceAll(out, sig.text, sig.quoted)
	}
	return out
}

func gateJoinBuildsASignature(left, right string) bool {
	for _, sig := range gateUnscopedSignatures {
		for i := 1; i < len(sig.text); i++ {
			if strings.HasSuffix(left, sig.text[:i]) && strings.HasPrefix(right, sig.text[i:]) {
				return true
			}
		}
	}
	return false
}

func nodeIsReviewer(node *dot.Node) bool {
	if node.AgentType == "reviewer" {
		return true
	}
	return node.HandlerRef == "claude-reviewer"
}

func graphHasReviewerNode(nodesByID map[string]*dot.Node) bool {
	for _, n := range nodesByID {
		if nodeIsReviewer(n) {
			return true
		}
	}
	return false
}

func verdictSeverity(verdict string) int {
	switch verdict {
	case workspace.ReviewVerdictApprove:
		return 0
	case workspace.ReviewVerdictRequestChanges:
		return 1
	case workspace.ReviewVerdictBlock:
		return 2
	default:
		return 1
	}
}

func verdictSeverityMax(verdicts []string) string {
	best := ""
	bestRank := -1
	for _, v := range verdicts {
		if r := verdictSeverity(v); r > bestRank {
			bestRank = r
			best = v
		}
	}
	return best
}

func upstreamReviewerNodeIDs(graph *dot.Graph, nodesByID map[string]*dot.Node, targetID string) map[string]bool {
	backward := make(map[string][]string, len(graph.Nodes))
	for _, e := range graph.Edges {
		backward[e.ToNodeID] = append(backward[e.ToNodeID], e.FromNodeID)
	}
	reviewers := make(map[string]bool)
	seen := map[string]bool{targetID: true}
	queue := append([]string{}, backward[targetID]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if n := nodesByID[id]; n != nil && nodeIsReviewer(n) {
			reviewers[id] = true
		}
		queue = append(queue, backward[id]...)
	}
	return reviewers
}

func nodeRoutesOnPreferredLabel(graph *dot.Graph, nodeID string) bool {
	for _, e := range graph.Edges {
		if e.FromNodeID != nodeID || e.Condition == nil {
			continue
		}
		for _, c := range e.Condition.Clauses {
			if c.LHS == "outcome.preferred_label" {
				return true
			}
		}
	}
	return false
}

func isConsolidateJoinNode(graph *dot.Graph, nodesByID map[string]*dot.Node, nodeID string) (upstream map[string]bool, ok bool) {
	if !nodeRoutesOnPreferredLabel(graph, nodeID) {
		return nil, false
	}
	upstream = upstreamReviewerNodeIDs(graph, nodesByID, nodeID)
	return upstream, len(upstream) >= 2
}

const (
	dotTerminalDispositionAttr           = "terminal_disposition"
	dotTerminalDispositionSuccess        = "success"
	dotTerminalDispositionNeedsAttention = "needs_attention"
)

func dotTerminalNodeIsSuccess(graph *dot.Graph, terminalID string) (success bool, why string) {
	switch terminalID {
	case "close":
		return true, ""
	case "close-needs-attention":
		return false, ""
	}

	var node *dot.Node
	for _, n := range graph.Nodes {
		if n.ID == terminalID {
			node = n
			break
		}
	}
	if node == nil {
		return false, fmt.Sprintf("terminal node %q is not declared in the graph", terminalID)
	}

	switch raw := node.UnknownAttrs[dotTerminalDispositionAttr]; raw {
	case dotTerminalDispositionSuccess:
		return true, ""
	case dotTerminalDispositionNeedsAttention:
		return false, ""
	case "":
		return false, fmt.Sprintf(
			"terminal node %q is not a reserved terminal (WG-022) and declares no %s=%q|%q; "+
				"the graph does not say whether reaching it is a success, so the run is not merged",
			terminalID, dotTerminalDispositionAttr,
			dotTerminalDispositionSuccess, dotTerminalDispositionNeedsAttention)
	default:
		return false, fmt.Sprintf(
			"terminal node %q declares %s=%q, which is not one of %q|%q",
			terminalID, dotTerminalDispositionAttr, raw,
			dotTerminalDispositionSuccess, dotTerminalDispositionNeedsAttention)
	}
}

func incrementCapIfBounded(graph *dot.Graph, cycles *core.CycleCounter, runID core.RunID, fromID, toID string) {
	for _, e := range graph.Edges {
		if e.FromNodeID != fromID || e.ToNodeID != toID {
			continue
		}
		if cap := dotEdgeTraversalCap(e); cap != nil && *cap > 0 {
			if _, incErr := cycles.Increment(runID, core.NodeID(fromID), core.NodeID(toID), cap); incErr != nil {
				fmt.Fprintf(os.Stderr, "daemon: dot cascade: increment traversal cap for edge %s→%s: %v (cap not enforced)\n", fromID, toID, incErr)
			}
		}
		return
	}
}

func dotEdgeTraversalCap(e *dot.Edge) *int {
	raw, ok := e.UnknownAttrs["traversal_cap"]
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return nil
	}
	return &n
}

func emitDotNoProgressDetected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	iterationCount int,
	diffHashCurrent string,
	diffHashPrior string,
) {
	pl := core.NoProgressDetectedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		IterationCount:  iterationCount,
		DiffHashCurrent: diffHashCurrent,
		DiffHashPrior:   diffHashPrior,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeNoProgressDetected, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit no_progress_detected: %v\n", emitErr)
	}
}

func strandedCommitNote(
	runID core.RunID,
	committed bool,
	gatePassed bool,
	gateNodeID string,
	prevNodeID string,
	headSHA string,
	gateNotes string,
) string {
	if !committed || gatePassed || gateNodeID == "" || prevNodeID != gateNodeID {
		return ""
	}
	return fmt.Sprintf(
		" — the %s gate stayed red and the implementer added nothing; commit %s is preserved on %s and was NOT merged%s",
		gateNodeID, headSHA, workspace.TaskBranchPrefix+runID.String(), gateFailureTail(gateNotes))
}

const gateFailureTailMaxBytes = 200

const gateFailureTailPrefix = "; gate output: "

func gateFailureTail(notes string) string {
	trimmed := strings.TrimSpace(notes)
	if trimmed == "" {
		return ""
	}
	oneLine := gateEvidenceQuote(strings.ReplaceAll(trimmed, "\n", " "))
	if len(oneLine) > gateFailureTailMaxBytes {
		b := []byte(oneLine[len(oneLine)-gateFailureTailMaxBytes:])
		for len(b) > 0 && !utf8.Valid(b) {
			b = b[1:]
		}
		oneLine = "…" + string(b)
	}
	if gateJoinBuildsASignature(gateFailureTailPrefix, oneLine) {
		oneLine = "…" + oneLine
	}
	return gateFailureTailPrefix + oneLine
}

func graphVersionOr(graph *dot.Graph) string {
	if graph.Version != "" {
		return graph.Version
	}
	return "0"
}

func emitNodeDispatchRequested(ctx context.Context, bus handlercontract.EventEmitter, clk substrate.ClockPort, runID core.RunID, nodeID core.NodeID) {
	pl := core.NodeDispatchRequestedPayload{
		RunID:       runID,
		NodeID:      nodeID,
		RequestedAt: clk.Now().UTC().Format(time.RFC3339),
		Origin:      core.NodeDispatchOriginWorkflow,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeNodeDispatchRequested, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit node_dispatch_requested: %v\n", emitErr)
	}
}

func emitNodeDispatchDecided(ctx context.Context, bus handlercontract.EventEmitter, payload *core.NodeDispatchDecidedPayload) {
	if payload == nil {
		return
	}
	b, err := json.Marshal(*payload)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, payload.RunID, core.EventTypeNodeDispatchDecided, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit node_dispatch_decided: %v\n", emitErr)
	}
}

func emitDotReviewerLaunched(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	sessionID core.SessionID,
	claudeSessionID string,
	iterationCount int,
) {
	pl := core.ReviewerLaunchedPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		SessionID:       sessionID,
		ClaudeSessionID: claudeSessionID,
		IterationCount:  iterationCount,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerLaunched, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit reviewer_launched: %v\n", emitErr)
	}
}

func emitDotReviewerVerdict(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	sessionID core.SessionID,
	claudeSessionID string,
	iterationCount int,
	verdict *workspace.ReviewVerdict,
) {
	flags := verdict.Flags
	if flags == nil {
		flags = []string{}
	}
	pl := core.ReviewerVerdictPayload{
		RunID:           runID,
		WorkflowMode:    core.WorkflowModeDot,
		SessionID:       sessionID,
		ClaudeSessionID: claudeSessionID,
		IterationCount:  iterationCount,
		SchemaVersion:   verdict.SchemaVersion,
		Verdict:         core.ReviewerVerdict(verdict.Verdict),
		Flags:           flags,
		Notes:           verdict.Notes,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeReviewerVerdict, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit reviewer_verdict: %v\n", emitErr)
	}
}

func emitDotImplementerResumed(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	claudeSessionID string,
	iterationCount int,
	priorVerdictSummary string,
) {
	pl := core.ImplementerResumedPayload{
		RunID:               runID,
		WorkflowMode:        core.WorkflowModeDot,
		SessionID:           handlercontract.NewSessionID(),
		ClaudeSessionID:     claudeSessionID,
		IterationCount:      iterationCount,
		PriorVerdictSummary: priorVerdictSummary,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	if emitErr := bus.EmitWithRunID(ctx, runID, core.EventTypeImplementerResumed, b); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: emit implementer_resumed: %v\n", emitErr)
	}
}

func newCapturedSpawnProof(ctx context.Context, tap *runloop.PerRunEventTap, runID core.RunID) func() {
	emitCtx := context.WithoutCancel(ctx)
	var once sync.Once
	return func() {
		once.Do(func() {
			//nolint:errcheck // best-effort emit; the reaper disarm is a guard, not a correctness gate (pre-RT8 idiom)
			_ = tap.EmitWithRunID(emitCtx, runID, core.EventTypeAgentReady, nil)
		})
	}
}

func killProcessGroup(proc *os.Process, label string) {
	if proc == nil {
		return
	}
	if killErr := syscall.Kill(-proc.Pid, syscall.SIGKILL); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: kill %s process group %d: %v\n", label, proc.Pid, killErr)
	}
}

func writeGateLog(path string, combined []byte) {
	if writeErr := os.WriteFile(path, combined, 0o600); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate log %q: %v\n", path, writeErr)
	}
}

const gateLogArchiveDir = "gate-logs"

func appendGateLogArchive(projectDir string, runID core.RunID, nodeID string, combined []byte, gateErr error) string {
	if projectDir == "" {
		return ""
	}
	dir := filepath.Join(projectDir, ".harmonik", gateLogArchiveDir, runID.String())
	if mkErr := os.MkdirAll(dir, core.HarmonikDirMode); mkErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: mkdir gate-log archive %q: %v\n", dir, mkErr)
		return ""
	}
	path := filepath.Join(dir, sanitizeGateLogName(nodeID)+".log")

	//nolint:gosec // G304: path is projectDir (harmonik config) + runID (a UUID) + a sanitized node ID.
	f, openErr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if openErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: open gate-log archive %q: %v\n", path, openErr)
		return ""
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: dot cascade: close gate-log archive %q: %v\n", path, closeErr)
		}
	}()

	header := fmt.Sprintf("\n===== gate attempt: node=%s run=%s at=%s exit=%v =====\n",
		nodeID, runID.String(), time.Now().UTC().Format(time.RFC3339), gateErr)
	if _, writeErr := f.WriteString(header); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate-log archive %q: %v\n", path, writeErr)
		return ""
	}
	if _, writeErr := f.Write(combined); writeErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: dot cascade: write gate-log archive %q: %v\n", path, writeErr)
		return ""
	}
	return path
}

func sanitizeGateLogName(nodeID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, nodeID)
	if safe == "" || strings.Trim(safe, ".") == "" {
		return "node"
	}
	return safe
}

func dotResolveResumeSessionID(capturedID, mintedID string, sessionIDCaptured bool) string {
	if capturedID != "" {
		return capturedID
	}
	if sessionIDCaptured {
		return ""
	}
	return mintedID
}

func readAutoStatusMarkerOrReport(ctx context.Context, runner tmux.CommandRunner, wtPath string) *workspace.AutoStatusMarker {
	marker, markerErr := readAutoStatusMarkerVia(ctx, runner, wtPath)
	if markerErr != nil {
		fmt.Fprintf(os.Stderr,
			"daemon: dot cascade: read auto-status marker in %q: %v (C2 deny-side check treated as absent)\n",
			wtPath, markerErr)
	}
	return marker
}
