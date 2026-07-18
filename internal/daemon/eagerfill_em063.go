package daemon

// eagerfill_em063.go — daemon eager-refill path (EM-062 + EM-063) + flywheel
// staged-bead generator (flywheel-motion.md §5.4 B).
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
// stagedBeadGeneratorEval implements flywheel-motion.md §5.4 (B). It shares
// this file (both are triggered from workloop.go on bead completion) but uses
// a DIFFERENT code path: it calls `br create` directly, NOT queue.AppendItems.
// The two paths share the file, not the pipeline.
//
// Spec ref: specs/execution-model.md §4.13 EM-062, EM-063.
// Bead ref: hk-9321v (eagerRefill); hk-f722 (stagedBeadGenerator); hk-kgwv (reconcile).

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
	"github.com/gregberns/harmonik/internal/orchestrator"
	"github.com/gregberns/harmonik/internal/queue"
)

// labelNeedsGreenlight is the Beads label applied by stagedBeadGeneratorEval
// to staged deploy+verify follow-up beads. It gates dispatch until a captain
// explicitly clears it via `harmonik greenlight <bead-id>`.
// Flywheel-motion.md §5.3/§6.2 (AC2, hk-lacr). Mirrors the constant in
// internal/brcli/ready.go (two packages, one well-known string).
const labelNeedsGreenlight = "needs-greenlight"

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

	maxConcurrent := deps.maxConcurrent
	if deps.concurrencyCtrl != nil {
		maxConcurrent = deps.concurrencyCtrl.Get()
	}
	inFlight := deps.runRegistry.Len()

	// EM-062 deficit decision (M5 slice 3B): project the fleet under the lock,
	// then let the pure orchestrator.EagerFillTarget pick the first active stream
	// group short of pending work. snapshotFleet's globalCap/rrCursor/blockedQueues
	// are selector-only inputs eager-fill never reads — pass maxConcurrent/0/nil.
	lq := deps.queueStore.LockForMutation()
	target, ok := orchestrator.EagerFillTarget(
		snapshotFleet(lq, deps.runRegistry, maxConcurrent, 0, nil),
		maxConcurrent, inFlight,
	)
	lq.Done()

	if !ok {
		return
	}
	targetQueueName := target.QueueName
	targetQueueID := target.QueueID
	targetGroupPos := target.GroupPos
	deficit := target.Deficit

	limit := orchestrator.OverfetchLimit(deficit)
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
	survivors = orchestrator.ClampSurvivors(survivors, deficit)

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
	// Phase 1 (pure, M5 slice 3B): build the in-queue set under the lock (effect),
	// then let orchestrator.ScreenAlreadyQueued drop candidates already present.
	inQueue := buildInQueueSet(deps, targetQueueID)
	phase1Survivors := orchestrator.ScreenAlreadyQueued(candidates, inQueue)

	survivors := make([]core.BeadID, 0, len(phase1Survivors))
	for _, id := range phase1Survivors {
		// Phase 2 — already landed on origin/main.
		landed, commitSHA, gitErr := beadLandedOnOriginMain(ctx, deps.projectDir, deps.targetBranch, string(id))
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

// beadLandedOnOriginMain reports whether a commit carrying an exact
// "Refs: <beadID>" trailer line is reachable from origin/<targetBranch>, and
// returns that commit's SHA.
//
// It mirrors the sibling provenance guard beadOnOriginMain: the check targets
// the configured merge branch (NOT a hardcoded origin/main) and uses
// --fixed-strings plus an exact-line verification so a shorter bead id is not a
// false-positive substring of a longer one (e.g. "Refs: hk-12" must NOT match a
// commit trailing "Refs: hk-123"). Because a substring --grep can surface a
// superstring commit, --max-count is omitted so every candidate is inspected.
//
// Returns (false, "", nil) when targetBranch is empty or origin/<targetBranch>
// does not exist (git exits 128). Returns (false, "", err) on other git errors.
//
// Spec ref: specs/execution-model.md §4.13 EM-063 Phase 2.
func beadLandedOnOriginMain(ctx context.Context, projectDir, targetBranch, beadID string) (found bool, sha string, err error) {
	if targetBranch == "" {
		return false, "", nil
	}
	needle := "Refs: " + beadID
	ref := "origin/" + targetBranch
	// %x1f (unit sep) splits the SHA from the body; %x1e (record sep) splits
	// commits so each commit's body can be line-verified against needle.
	//nolint:gosec // G204: beadID/targetBranch are internal identifiers; projectDir is a controlled path.
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "log", ref,
		"--fixed-strings", "--grep", needle, "--format=%H%x1f%B%x1e")
	out, runErr := cmd.Output()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ExitCode() == 128 {
			// origin/<targetBranch> does not exist — treat as not landed.
			return false, "", nil
		}
		return false, "", fmt.Errorf("git log %s --grep %q: %w", ref, needle, runErr)
	}
	for _, rec := range strings.Split(string(out), "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		hashAndBody := strings.SplitN(rec, "\x1f", 2)
		if len(hashAndBody) != 2 {
			continue
		}
		hash := strings.TrimSpace(hashAndBody[0])
		for _, line := range strings.Split(hashAndBody[1], "\n") {
			if strings.TrimRight(line, "\r") == needle {
				return true, hash, nil
			}
		}
	}
	return false, "", nil
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

	// Provenance guard (§6.2 — hk-zlwq): only enqueue follow-ups of OWN
	// merged commits. When a merge target branch is configured, verify the
	// completed bead's Refs: trailer is present on origin/<targetBranch>.
	// Fail-closed: a run that succeeds but whose commit is absent from
	// origin/<targetBranch> spawns NO follow-up.
	// Skipped when targetBranch is empty (no remote merge target; not
	// reachable in production because merges require a non-empty targetBranch).
	if deps.targetBranch != "" && !beadOnOriginMain(ctx, deps.projectDir, completedBeadID, deps.targetBranch) {
		return
	}

	// Guardrail 4: at-most-once ledger (in-memory check; disk-backed by AC1).
	ledgerKey := string(completedBeadID) + ":" + matchedClass
	if deps.followUpLedgerMu != nil {
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
		// AC2 (hk-lacr): needs-greenlight blocks dispatch until captain clears it
		// via `harmonik greenlight <bead-id>` (flywheel-motion.md §5.3/§6.2).
		"--label", labelNeedsGreenlight,
	)
	cmd.Dir = deps.projectDir
	if out, runErr := cmd.Output(); runErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: br create bead=%s class=%s: %v\n%s",
			completedBeadID, matchedClass, runErr, out)
		return
	}

	// AC1 (hk-3ndb): persist the new key to disk after successful br create so
	// the at-most-once guarantee survives a daemon restart.
	if deps.followUpLedgerPath != "" {
		if persistErr := appendFollowUpLedger(deps.followUpLedgerPath, ledgerKey); persistErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: persist ledger key %s: %v\n", ledgerKey, persistErr)
		}
	}
}

// beadOnOriginMain returns true when beadID appears as a "Refs: <id>"
// trailer in any commit reachable from origin/<targetBranch>.
//
// Used by stagedBeadGeneratorEval as the §6.2 provenance guard: the
// work-generates-work loop SHALL only enqueue follow-ups of its OWN merged
// commits. A run that succeeds but whose Refs: SHA is absent from
// origin/<targetBranch> must not spawn a follow-up.
//
// Returns false on any git error (fail-closed) or when either argument is empty.
//
// Bead ref: hk-zlwq.
func beadOnOriginMain(ctx context.Context, projectDir string, beadID core.BeadID, targetBranch string) bool {
	if projectDir == "" || targetBranch == "" {
		return false
	}
	needle := "Refs: " + string(beadID)
	//nolint:gosec // G204: targetBranch is a daemon-internal config value, not user input
	cmd := exec.CommandContext(ctx, "git", "log", "origin/"+targetBranch, "--format=%B",
		"--fixed-strings", "--grep", needle)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimRight(line, "\r") == needle {
			return true
		}
	}
	return false
}
