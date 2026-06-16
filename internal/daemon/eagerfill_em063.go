package daemon

// eagerfill_em063.go — daemon eager-refill path (EM-062 + EM-063).
//
// eagerRefillEval implements the EM-062 trigger/compute function: on every
// run_terminal event and on every poll tick it computes the available-slot
// deficit, fetches candidates from `kerf next`, filters them through the
// EM-063 two-phase pre-screen, and appends survivors to the active stream
// group via queue.AppendItems.
//
// preScreenCandidates implements EM-063:
//   - Phase 1: skip beads already present in the queue with a terminal or
//     in-flight status (already_in_queue — queue.json authority, fastest).
//   - Phase 2: skip beads that have a "Refs: <bead_id>" commit on origin/main
//     (already_landed — git authority); emits stale_open_bead_detected for
//     each hit.
//
// The provenance guard (EM-063 §"Provenance guard") is enforced structurally:
// newly-created beads land open (not yet ready); `kerf next --only=bead` returns
// only ready beads so the readiness gate is the normative enforcement here.
//
// When kerfPath is empty (kerf not installed), eagerRefillEval returns
// immediately — eager-refill is disabled for this daemon instance.
//
// Spec ref: specs/execution-model.md §4.13 EM-062, EM-063.
// Bead ref: hk-9321v.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/queue"
)

// eagerfillOverfetchFactor is the OVERFETCH_FACTOR from EM-062: kerf next is
// called with limit = deficit × factor so that pre-screen rejections do not
// leave an avoidable gap in the filled stream.
const eagerfillOverfetchFactor = 2

// eagerRefillEval implements the EM-062 eager-refill trigger and compute
// function.
//
// It is called:
//  1. After evaluateGroupAdvanceWithOutcome completes (run_terminal path).
//  2. On every dispatch-loop poll tick (runWorkLoop main loop).
//
// It is a best-effort, idempotent operation: if kerf next fails or the queue
// is absent/not a stream group, it returns without error. Errors that arise
// during the git Phase-2 check are logged to stderr but do not abort the call.
//
// Spec ref: specs/execution-model.md §4.13 EM-062.
func eagerRefillEval(ctx context.Context, deps workLoopDeps) {
	if deps.kerfPath == "" {
		return
	}
	if deps.queueStore == nil {
		return
	}

	lq := deps.queueStore.LockForMutation()

	// EM-062: only fire on the single active queue whose active group is a stream.
	// Named-queues extension: iterate all queues and refill the first stream group
	// that has a deficit. For v1 (spec scope) we take the first match found.
	var (
		targetQueueName string
		targetQueueID   string
		targetGroupPos  int = -1
		deficit         int
	)

	maxConcurrent := deps.maxConcurrent
	if deps.concurrencyCtrl != nil {
		maxConcurrent = deps.concurrencyCtrl.Get()
	}
	inFlight := deps.runRegistry.Len()
	available := maxConcurrent - inFlight
	if available <= 0 {
		lq.Done()
		return
	}

	for _, name := range lq.LockedAllQueueNames() {
		q := lq.LockedQueueByName(name)
		if q == nil || q.Status != queue.QueueStatusActive {
			continue
		}
		for gi := range q.Groups {
			g := &q.Groups[gi]
			if g.Status != queue.GroupStatusActive || g.Kind != queue.GroupKindStream {
				continue
			}
			// Count pending items already in the group (they will fill slots
			// without refill action).
			pendingCount := 0
			for ii := range g.Items {
				if g.Items[ii].Status == queue.ItemStatusPending {
					pendingCount++
				}
			}
			d := available - pendingCount
			if d <= 0 {
				continue
			}
			// Found a stream group with a deficit.
			targetQueueName = name
			targetQueueID = q.QueueID
			targetGroupPos = gi
			deficit = d
			break
		}
		if targetGroupPos >= 0 {
			break
		}
	}

	lq.Done()

	if targetGroupPos < 0 {
		return
	}

	limit := deficit * eagerfillOverfetchFactor
	rawCandidates, err := kerfNextBeads(ctx, deps.kerfPath, limit)
	if err != nil {
		// kerf not available or returned an error — eager-refill skips silently.
		return
	}
	if len(rawCandidates) == 0 {
		return
	}

	// EM-063: two-phase pre-screen.
	survivors := preScreenCandidates(ctx, deps, rawCandidates, targetQueueID)
	if len(survivors) == 0 {
		return
	}

	// Take up to deficit survivors (kerf returns in priority order; preserve it).
	if len(survivors) > deficit {
		survivors = survivors[:deficit]
	}

	// Append survivors to the active stream group (QM-040).
	lq = deps.queueStore.LockForMutation()
	q := lq.LockedQueueByName(queue.NormaliseQueueName(targetQueueName))
	if q == nil || q.QueueID != targetQueueID {
		// Queue was replaced or cleared between our check and now — skip.
		lq.Done()
		return
	}
	if targetGroupPos >= len(q.Groups) {
		lq.Done()
		return
	}

	beadStrs := make([]string, len(survivors))
	for i, id := range survivors {
		beadStrs[i] = string(id)
	}

	_, evts, appendErr := queue.AppendItems(ctx, q, targetGroupPos, beadStrs, deps.queueLedger)
	if appendErr != nil {
		// Validation error (e.g. wave group) or ledger error — log and continue.
		fmt.Fprintf(os.Stderr, "daemon: eagerRefillEval: AppendItems queueID=%s: %v\n",
			targetQueueID, appendErr)
		lq.Done()
		return
	}

	if err := queue.Persist(ctx, deps.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: eagerRefillEval: Persist queueID=%s: %v\n",
			targetQueueID, err)
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(targetQueueName), q)
	lq.Done()

	// Wake the dispatch loop so it picks up the newly-appended pending items.
	deps.queueStore.Wake()

	// Emit events after releasing the lock.
	for _, evt := range evts {
		raw, mErr := json.Marshal(evt.Payload)
		if mErr != nil {
			raw = evt.Payload
		}
		_ = deps.bus.Emit(ctx, core.EventType(evt.Type), raw)
	}
}

// preScreenCandidates applies the EM-063 two-phase filter to candidates.
//
// Phase 1 (queue.json, in-memory): beads present in the named queue with a
// non-idle status are skipped — they are already dispatched or done.
//
// Phase 2 (git, origin/main): beads not eliminated by Phase 1 are checked
// against origin/main via `git log origin/main --grep "Refs: <id>"`. Any hit
// causes the bead to be skipped and a stale_open_bead_detected event to be
// emitted.
//
// Spec ref: specs/execution-model.md §4.13 EM-063.
func preScreenCandidates(ctx context.Context, deps workLoopDeps, candidates []core.BeadID, targetQueueID string) []core.BeadID {
	// Build a set of bead IDs already in the target queue for Phase 1.
	inQueue := buildInQueueSet(deps, targetQueueID)

	survivors := make([]core.BeadID, 0, len(candidates))
	for _, id := range candidates {
		// Phase 1 — already in queue.
		if _, alreadyIn := inQueue[id]; alreadyIn {
			continue
		}

		// Phase 2 — already landed on origin/main.
		landed, commitSHA, gitErr := beadLandedOnOriginMain(ctx, deps.projectDir, string(id))
		if gitErr != nil {
			// Non-fatal: log and treat as not-landed so we don't spuriously skip.
			fmt.Fprintf(os.Stderr, "daemon: preScreenCandidates: git check bead=%s: %v\n", id, gitErr)
		}
		if landed {
			emitStaleOpenBeadDetected(ctx, deps, id, commitSHA)
			continue
		}

		survivors = append(survivors, id)
	}
	return survivors
}

// buildInQueueSet returns the set of bead IDs present in the queue identified
// by targetQueueID with a status that disqualifies them from re-dispatch.
//
// Statuses that cause exclusion (EM-063 Phase 1):
// pending, dispatched, completed, failed — all mean "already claimed or done."
//
// Spec ref: specs/execution-model.md §4.13 EM-063 Phase 1.
func buildInQueueSet(deps workLoopDeps, targetQueueID string) map[core.BeadID]struct{} {
	if deps.queueStore == nil {
		return nil
	}
	lq := deps.queueStore.LockForMutation()
	defer lq.Done()

	result := make(map[core.BeadID]struct{})
	for _, name := range lq.LockedAllQueueNames() {
		q := lq.LockedQueueByName(name)
		if q == nil {
			continue
		}
		// EM-063 inspects the "active queue.json envelope in-memory" — check ALL
		// queues, not just the target, so a bead in a different active queue is
		// also excluded. The spec says "the active queue.json"; with named queues
		// we conservatively check all queues.
		for gi := range q.Groups {
			for ii := range q.Groups[gi].Items {
				it := &q.Groups[gi].Items[ii]
				switch it.Status {
				case queue.ItemStatusPending, queue.ItemStatusDispatched,
					queue.ItemStatusCompleted, queue.ItemStatusFailed:
					result[it.BeadID] = struct{}{}
				}
			}
		}
	}
	return result
}

// beadLandedOnOriginMain executes `git log origin/main --grep "Refs: <id>"
// --max-count=1 --format=%H` in deps.projectDir and reports whether at least
// one commit carrying the Refs: trailer was found.
//
// Returns (false, "", nil) when origin/main does not exist (git exits 128).
// Returns (false, "", err) on other git errors.
//
// Spec ref: specs/execution-model.md §4.13 EM-063 Phase 2.
func beadLandedOnOriginMain(ctx context.Context, projectDir, beadID string) (found bool, sha string, err error) {
	grep := "Refs: " + beadID
	//nolint:gosec // G204: beadID is an internal identifier; projectDir is a controlled path.
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "log", "origin/main",
		"--grep", grep, "--max-count=1", "--format=%H")
	out, runErr := cmd.Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 128 {
			// origin/main does not exist — treat as not landed.
			return false, "", nil
		}
		return false, "", fmt.Errorf("git log origin/main --grep %q: %w", grep, runErr)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return false, "", nil
	}
	return true, trimmed, nil
}

// emitStaleOpenBeadDetected emits the stale_open_bead_detected informative
// event (EM-063 Phase 2 hit).
//
// Spec ref: specs/execution-model.md §4.13 EM-063.
func emitStaleOpenBeadDetected(ctx context.Context, deps workLoopDeps, beadID core.BeadID, commitSHA string) {
	payload := map[string]string{
		"bead_id":    string(beadID),
		"commit_sha": commitSHA,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = deps.bus.Emit(ctx, core.EventTypeStaleOpenBeadDetected, raw)
}

// kerfNextBeads runs `kerf next --format=json --only=bead --limit N` and
// returns the ordered list of bead IDs.
//
// The JSON output shape from `kerf next --format=json --only=bead` is an
// array of objects, each with at least a "bead_id" field. Unknown additional
// fields are silently ignored. When kerf returns a non-zero exit code or
// malformed JSON, an error is returned.
//
// Spec ref: specs/execution-model.md §4.13 EM-062 (kerf_next(limit = ...)).
func kerfNextBeads(ctx context.Context, kerfPath string, limit int) ([]core.BeadID, error) {
	//nolint:gosec // G204: kerfPath is resolved via exec.LookPath at startup; limit is an int.
	cmd := exec.CommandContext(ctx, kerfPath, "next",
		"--format=json", "--only=bead",
		fmt.Sprintf("--limit=%d", limit))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kerf next: %w", err)
	}
	if len(out) == 0 {
		return nil, nil
	}

	// kerf next --format=json --only=bead outputs a JSON array of objects.
	// Each object has at minimum a "bead_id" field.
	var items []struct {
		BeadID string `json:"bead_id"`
	}
	if jsonErr := json.Unmarshal(out, &items); jsonErr != nil {
		return nil, fmt.Errorf("kerf next: unmarshal: %w", jsonErr)
	}

	ids := make([]core.BeadID, 0, len(items))
	for _, item := range items {
		if item.BeadID != "" {
			ids = append(ids, core.BeadID(item.BeadID))
		}
	}
	return ids, nil
}

// stagedBeadGeneratorEval implements flywheel-motion.md §5.4 (B) STAGED-BEAD
// GENERATOR. On a Phase-1 completion (successful merge to origin/main) of a
// deploy-relevant bead, it emits a staged deploy+verify follow-up bead via `br
// create` with all four guardrails:
//
//  1. Rule-only   — fires only when the completed bead carries a label matching
//     a Phase-2 class declared in sentinel.done_definition; never LLM-invented.
//  2. Land-open   — created bead lands with status=open; never auto-dispatched
//     the same tick.
//  3. WIP ceiling — skipped when the current in-flight count equals maxConcurrent.
//  4. At-most-once — idempotency guard keyed on (completedBeadID, class) so
//     re-entrant calls from retry paths do not duplicate the follow-up.
//
// The created bead is STAGED (captain must greenlit before dispatch). It is
// NEVER auto-deployed by this function.
//
// Spec ref: flywheel-motion.md §5.4 (B). Bead ref: hk-f722.
func stagedBeadGeneratorEval(ctx context.Context, deps workLoopDeps, completedBeadID core.BeadID, completedBeadLabels []string) {
	// Require brPath and projectDir — without them we cannot shell out to br.
	if deps.brPath == "" || deps.projectDir == "" {
		return
	}

	// Guardrail 3: skip when WIP == max_concurrent.
	maxConcurrent := deps.maxConcurrent
	if deps.concurrencyCtrl != nil {
		maxConcurrent = deps.concurrencyCtrl.Get()
	}
	if deps.runRegistry != nil && deps.runRegistry.Len() >= maxConcurrent {
		return
	}

	// Guardrail 1: rule-only — load sentinel config to determine Phase-2 classes.
	sentinelCfg, err := digest.LoadSentinelConfig(deps.projectDir)
	if err != nil {
		// Config parse failure is non-fatal; log and skip.
		fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: LoadSentinelConfig: %v\n", err)
		return
	}
	phase2Classes := sentinelCfg.Phase2Classes()
	if len(phase2Classes) == 0 {
		return
	}
	phase2Set := make(map[string]struct{}, len(phase2Classes))
	for _, c := range phase2Classes {
		phase2Set[c] = struct{}{}
	}
	var matchedClass string
	for _, label := range completedBeadLabels {
		if _, ok := phase2Set[label]; ok {
			matchedClass = label
			break
		}
	}
	if matchedClass == "" {
		return
	}

	// Guardrail 4: at-most-once ledger.
	if deps.followUpLedgerMu != nil {
		ledgerKey := string(completedBeadID) + ":" + matchedClass
		deps.followUpLedgerMu.Lock()
		_, exists := deps.followUpLedger[ledgerKey]
		if !exists {
			deps.followUpLedger[ledgerKey] = struct{}{}
		}
		deps.followUpLedgerMu.Unlock()
		if exists {
			return
		}
	}

	// Guardrail 2: land-open — br create with --status open so the bead is
	// never auto-dispatched the same tick. Captain must greenlit before dispatch.
	verifyCmd := sentinelCfg.DoneDefinitionFor(matchedClass)
	title := fmt.Sprintf("deploy+verify: %s (%s)", completedBeadID, matchedClass)
	description := fmt.Sprintf(
		"Phase-2 deploy+verify follow-up for bead %s (class: %s).\n"+
			"Verify command: %s\n\n"+
			"STAGED — captain must greenlit before dispatch (flywheel-motion.md §5.4 B).",
		completedBeadID, matchedClass, verifyCmd,
	)
	//nolint:gosec // G204: brPath resolved via exec.LookPath at startup; args are controlled
	cmd := exec.CommandContext(ctx, deps.brPath,
		"create", title, "--type", "task", "--status", "open", // new bead, not a reset
		"--description", description,
		"--label", matchedClass,
		"--label", fmt.Sprintf("followup:%s:%s", completedBeadID, matchedClass),
	)
	cmd.Dir = deps.projectDir
	if out, runErr := cmd.Output(); runErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: br create bead=%s class=%s: %v\n%s",
			completedBeadID, matchedClass, runErr, out)
	}
}
