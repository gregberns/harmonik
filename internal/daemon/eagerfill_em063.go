package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/digest"
	"github.com/gregberns/harmonik/internal/orchestrator"
	"github.com/gregberns/harmonik/internal/queue"
)

const labelNeedsGreenlight = orchestrator.LabelNeedsGreenlight

type eagerRefillPort struct {
	kerfPath           string
	followUpLedger     map[string]struct{}
	followUpLedgerMu   *sync.Mutex
	followUpLedgerPath string
}

func newEagerRefillPort(cfg Config) eagerRefillPort {
	return eagerRefillPort{
		kerfPath:           cfg.KerfPath,
		followUpLedger:     make(map[string]struct{}),
		followUpLedgerMu:   &sync.Mutex{},
		followUpLedgerPath: filepath.Join(cfg.ProjectDir, ".harmonik", followUpLedgerFileName),
	}
}

func eagerRefillEval(ctx context.Context, port reapSeamPort) {
	if port.eagerRefill.kerfPath == "" {
		return
	}
	if port.queueStore == nil {
		return
	}

	maxConcurrent := port.maxConcurrent
	if port.concurrencyCtrl != nil {
		maxConcurrent = port.concurrencyCtrl.Get()
	}
	inFlight := port.runRegistry.Len()

	lq := port.queueStore.LockForMutation()
	target, ok := orchestrator.EagerFillTarget(
		snapshotFleet(lq, port.runRegistry, maxConcurrent, 0, nil, nil),
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
	rawCandidates, err := kerfNextBeads(ctx, port.eagerRefill.kerfPath, limit)
	if err != nil {
		return
	}
	if len(rawCandidates) == 0 {
		return
	}

	survivors := preScreenCandidates(ctx, port, rawCandidates)
	if len(survivors) == 0 {
		return
	}

	survivors = orchestrator.ClampSurvivors(survivors, deficit)

	lq = port.queueStore.LockForMutation()
	q := lq.LockedQueueByName(queue.NormaliseQueueName(targetQueueName))
	if q == nil || q.QueueID != targetQueueID {
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

	_, evts, appendErr := queue.AppendItems(ctx, q, targetGroupPos, beadStrs, port.queueLedger, time.Now())
	if appendErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: eagerRefillEval: AppendItems queueID=%s: %v\n",
			targetQueueID, appendErr)
		lq.Done()
		return
	}

	if err := queue.Persist(ctx, port.projectDir, q); err != nil {
		fmt.Fprintf(os.Stderr, "daemon: eagerRefillEval: Persist queueID=%s: %v\n",
			targetQueueID, err)
		lq.Done()
		return
	}
	lq.LockedSetQueueByName(queue.NormaliseQueueName(targetQueueName), q)
	lq.Done()

	port.queueStore.Wake()

	emitEagerRefillEvents(ctx, port, evts)
}

func emitEagerRefillEvents(ctx context.Context, port reapSeamPort, events []queue.EventIntent) {
	for _, evt := range events {
		if emitErr := port.bus.Emit(ctx, evt.Type, evt.Payload); emitErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: eagerRefillEval: emit %s: %v\n", evt.Type, emitErr)
		}
	}
}

func preScreenCandidates(ctx context.Context, port reapSeamPort, candidates []core.BeadID) []core.BeadID {
	inQueue := buildInQueueSet(port)
	phase1Survivors := orchestrator.ScreenAlreadyQueued(candidates, inQueue)

	survivors := make([]core.BeadID, 0, len(phase1Survivors))
	for _, id := range phase1Survivors {
		landed, commitSHA, gitErr := beadLandedOnOriginMain(ctx, port.projectDir, port.targetBranch, string(id))
		if gitErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: preScreenCandidates: git check bead=%s: %v\n", id, gitErr)
		}
		if landed {
			emitStaleOpenBeadDetected(ctx, port, id, commitSHA)
			continue
		}

		survivors = append(survivors, id)
	}
	return survivors
}

func buildInQueueSet(port reapSeamPort) map[core.BeadID]struct{} {
	if port.queueStore == nil {
		return nil
	}
	lq := port.queueStore.LockForMutation()
	defer lq.Done()

	result := make(map[core.BeadID]struct{})
	for _, name := range lq.LockedAllQueueNames() {
		q := lq.LockedQueueByName(name)
		if q == nil {
			continue
		}
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

func emitStaleOpenBeadDetected(ctx context.Context, port reapSeamPort, beadID core.BeadID, commitSHA string) {
	payload := core.StaleOpenBeadDetectedPayload{BeadID: beadID, CommitSHA: commitSHA}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	if emitErr := port.bus.Emit(ctx, core.EventTypeStaleOpenBeadDetected, raw); emitErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: emit stale open bead detected: %v\n", emitErr)
	}
}

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

func stagedBeadGeneratorEval(ctx context.Context, brPath string, reapPort reapSeamPort, completedBeadID core.BeadID, completedBeadLabels []string) {
	stagedBeadGeneratorEvalWithPort(ctx, newRunCompletionPort(brPath, reapPort), completedBeadID, completedBeadLabels)
}

func stagedBeadGeneratorEvalWithPort(ctx context.Context, port runCompletionPort, completedBeadID core.BeadID, completedBeadLabels []string) {
	if port.brPath == "" || port.projectDir == "" {
		return
	}

	maxConcurrent := port.maxConcurrent
	if port.concurrencyCtrl != nil {
		maxConcurrent = port.concurrencyCtrl.Get()
	}
	if port.runRegistry != nil && port.runRegistry.Len() >= maxConcurrent {
		return
	}

	sentinelCfg, err := digest.LoadSentinelConfig(port.projectDir)
	if err != nil {
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

	if port.targetBranch != "" && !beadOnOriginMain(ctx, port.projectDir, completedBeadID, port.targetBranch) {
		return
	}

	ledgerKey := string(completedBeadID) + ":" + matchedClass
	if port.eagerRefill.followUpLedgerMu != nil {
		port.eagerRefill.followUpLedgerMu.Lock()
		_, exists := port.eagerRefill.followUpLedger[ledgerKey]
		if !exists {
			port.eagerRefill.followUpLedger[ledgerKey] = struct{}{}
		}
		port.eagerRefill.followUpLedgerMu.Unlock()
		if exists {
			return
		}
	}

	verifyCmd := sentinelCfg.DoneDefinitionFor(matchedClass)
	title := fmt.Sprintf("deploy+verify: %s (%s)", completedBeadID, matchedClass)
	description := fmt.Sprintf(
		"Phase-2 deploy+verify follow-up for bead %s (class: %s).\n"+
			"Verify command: %s\n\n"+
			"STAGED — captain must greenlit before dispatch (flywheel-motion.md §5.4 B).",
		completedBeadID, matchedClass, verifyCmd,
	)
	//nolint:gosec // G204: brPath resolved via exec.LookPath at startup; args are controlled
	cmd := exec.CommandContext(ctx, port.brPath,
		"create", title, "--type", "task", "--status", "open", // new bead, not a reset
		"--description", description,
		"--label", matchedClass,
		"--label", fmt.Sprintf("followup:%s:%s", completedBeadID, matchedClass),
		"--label", labelNeedsGreenlight,
	)
	cmd.Dir = port.projectDir
	if out, runErr := cmd.Output(); runErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: br create bead=%s class=%s: %v\n%s",
			completedBeadID, matchedClass, runErr, out)
		return
	}

	if port.eagerRefill.followUpLedgerPath != "" {
		if persistErr := appendFollowUpLedger(port.eagerRefill.followUpLedgerPath, ledgerKey); persistErr != nil {
			fmt.Fprintf(os.Stderr, "daemon: stagedBeadGeneratorEval: persist ledger key %s: %v\n", ledgerKey, persistErr)
		}
	}
}

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
