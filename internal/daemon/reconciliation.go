package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

func emitOperatorMailboxEscalation(ctx context.Context, emitter interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}, logW io.Writer, source, question, contextLink string,
) {
	p := core.DecisionNeededPayload{
		Question:     question,
		Options:      []string{"acknowledged"},
		ContextLink:  contextLink,
		BlockedAgent: source,
		Topic:        core.DecisionTopicOperatorMailbox,
		Urgency:      core.DecisionUrgencyBlocker,
	}
	b, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		fmt.Fprintf(logW, "reconciliation: marshal decision_needed for operator-mailbox (%s): %v\n", source, marshalErr) //nolint:errcheck // best-effort stderr status log
		return
	}
	if emitErr := emitter.Emit(ctx, core.EventTypeDecisionNeeded, b); emitErr != nil {
		fmt.Fprintf(logW, "reconciliation: emit decision_needed (operator-mailbox) for %s: %v\n", source, emitErr) //nolint:errcheck // best-effort stderr status log
	}
}

const parentLabelPrefix = "parent:hk-"

const beadsMergeConflictsLog = ".beads/merge-conflicts.log"

// CatBL1StartupSweepConfig holds the construction-time parameters for the
// Cat-BL1 child-bead orphan startup sweep launched by RunCatBL1StartupSweep.
type CatBL1StartupSweepConfig struct {
	// ProjectDir is the harmonik project root. Must be non-empty.
	ProjectDir string

	// BrPath is the absolute path to the `br` binary. Must be non-empty for
	// bead-ledger queries and close operations.
	BrPath string

	// TargetBranch is the git branch the merge-commit scanner checks.
	// Defaults to "main" when empty.
	TargetBranch string

	// Emitter is used to emit orphaned_child_bead and operator_escalation_required
	// events. Required.
	Emitter interface {
		Emit(ctx context.Context, eventType core.EventType, payload []byte) error
	}

	// LogWriter receives non-fatal scan status messages. Nil → os.Stderr.
	LogWriter io.Writer
}

// RunCatBL1StartupSweep implements the Cat-BL1 child-bead orphan detector
// (reconciliation/spec.md §8.BL1). It:
//  1. Lists all open and in_progress beads.
//  2. Filters for beads carrying any parent:hk-* label.
//  3. For each, checks git for a "Refs: <parent-id>" commit on the target branch.
//  4. If no commit found and bead is open: emits orphaned_child_bead + closes via br close.
//  5. If no commit found and bead is in_progress: emits orphaned_child_bead +
//     emits operator_escalation_required (escalates rather than auto-closing).
//
// Non-fatal: individual bead errors are logged and skipped; the function
// continues over remaining candidates.
//
// Spec ref: specs/reconciliation/spec.md §8.BL1 — Cat-BL1 child-bead orphan.
//
//nolint:gocognit,cyclop // over the threshold; splitting the spec-mapped §8.BL1 detector loop mid-release is riskier than the marginal complexity
func RunCatBL1StartupSweep(ctx context.Context, cfg CatBL1StartupSweepConfig) error {
	if cfg.ProjectDir == "" || cfg.BrPath == "" {
		return fmt.Errorf("reconciliation Cat-BL1: ProjectDir and BrPath must be non-empty")
	}

	logW := cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}

	adapter, adapterErr := brcli.NewForProject(cfg.BrPath, cfg.ProjectDir)
	if adapterErr != nil {
		return fmt.Errorf("reconciliation Cat-BL1: br adapter: %w", adapterErr)
	}

	targetBranch := cfg.TargetBranch
	if targetBranch == "" {
		targetBranch = "main"
	}

	scanCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	candidates := collectParentLabeledBeads(scanCtx, adapter, logW)
	if len(candidates) == 0 {
		return nil
	}

	timeoutCfg := brcli.TimeoutConfig{}

	for _, rec := range candidates {
		parentID, ok := extractParentBeadID(rec.Labels)
		if !ok {
			continue // no parent:hk-* label after all
		}

		hasCommit, gitErr := hasParentMergeCommit(scanCtx, cfg.ProjectDir, targetBranch, parentID)
		if gitErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: bead %s git scan for parent %s: %v (skipping)\n", rec.BeadID, parentID, gitErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		if hasCommit {
			continue // parent ran and merged — not an orphan
		}

		orphanPayload := core.OrphanedChildBeadPayload{
			BeadID:   string(rec.BeadID),
			ParentID: parentID,
		}
		if payloadBytes, marshalErr := json.Marshal(orphanPayload); marshalErr == nil {
			if emitErr := cfg.Emitter.Emit(scanCtx, core.EventTypeOrphanedChildBead, payloadBytes); emitErr != nil {
				fmt.Fprintf(logW, "reconciliation Cat-BL1: emit orphaned_child_bead for %s: %v\n", rec.BeadID, emitErr) //nolint:errcheck // best-effort stderr status log
			}
		}

		if rec.Status == core.CoarseStatusInProgress {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: bead %s in_progress with orphaned parent %s — escalating\n", rec.BeadID, parentID) //nolint:errcheck // best-effort stderr status log
			escalatePayload := core.OperatorEscalationRequiredPayload{
				Reason: core.OperatorEscalationReasonOtherVerdictDriven,
			}
			if escalateBytes, marshalErr := json.Marshal(escalatePayload); marshalErr == nil {
				if emitErr := cfg.Emitter.Emit(scanCtx, core.EventTypeOperatorEscalationRequired, escalateBytes); emitErr != nil {
					fmt.Fprintf(logW, "reconciliation Cat-BL1: emit operator_escalation_required for %s: %v\n", rec.BeadID, emitErr) //nolint:errcheck // best-effort stderr status log
				}
			}
			emitOperatorMailboxEscalation(scanCtx, cfg.Emitter, logW, "reconciliation-cat-bl1",
				fmt.Sprintf("Bead %s is in_progress with orphaned parent %s (parent run never merged on %s) — verify and resolve manually.",
					rec.BeadID, parentID, targetBranch),
				string(rec.BeadID))
			continue
		}

		closeErr := adapter.SweepCloseBead(scanCtx, timeoutCfg, rec.BeadID)
		if closeErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: close orphan bead %s (parent %s): %v\n", rec.BeadID, parentID, closeErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		fmt.Fprintf(logW, "reconciliation Cat-BL1: closed orphan bead %s (parent %s run discarded)\n", rec.BeadID, parentID) //nolint:errcheck // best-effort stderr status log
	}

	return nil
}

func collectParentLabeledBeads(ctx context.Context, adapter *brcli.Adapter, logW io.Writer) []core.BeadRecord {
	var candidates []core.BeadRecord

	for _, status := range []string{"open", "in_progress"} {
		beads, listErr := adapter.ListBeadsByStatus(ctx, status)
		if listErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: list %s beads: %v (skipping status)\n", status, listErr) //nolint:errcheck // best-effort stderr status log
			continue
		}
		for _, rec := range beads {
			if hasParentLabel(rec.Labels) {
				candidates = append(candidates, rec)
			}
		}
	}
	return candidates
}

func hasParentLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, parentLabelPrefix) {
			return true
		}
	}
	return false
}

func extractParentBeadID(labels []string) (string, bool) {
	for _, l := range labels {
		if strings.HasPrefix(l, parentLabelPrefix) {
			return "hk-" + strings.TrimPrefix(l, "parent:hk-"), true
		}
	}
	return "", false
}

func hasParentMergeCommit(ctx context.Context, projectDir, targetBranch, parentBeadID string) (bool, error) {
	//nolint:gosec // G204: projectDir resolved from harmonik config; parentBeadID is a bead-ID suffix, not user input.
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "log", "-1",
		"--grep", "Refs: "+parentBeadID, "--format=%H", targetBranch)
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// CatBL3StartupSweepConfig holds the construction-time parameters for the
// Cat-BL3 merge-conflict-log audit sweep.
type CatBL3StartupSweepConfig struct {
	// ProjectDir is the harmonik project root. Must be non-empty.
	ProjectDir string

	// RunID is the reconciliation run ID used in the emitted event payload.
	// When empty, the event's run_id field is left blank (non-fatal).
	RunID string

	// Emitter is used to emit bead_ledger_conflict_audit events. Required.
	Emitter interface {
		Emit(ctx context.Context, eventType core.EventType, payload []byte) error
	}

	// LogWriter receives non-fatal scan status messages. Nil → os.Stderr.
	LogWriter io.Writer
}

// RunCatBL3StartupSweep implements the Cat-BL3 merge-conflict-log audit
// (reconciliation/spec.md §8.BL3). It:
//  1. Reads .beads/merge-conflicts.log; skips if absent or empty.
//  2. Parses conflict lines and collects BeadLedgerConflict records.
//  3. Emits bead_ledger_conflict_audit{run_id, bead_ids, conflicts, timestamp}.
//  4. Emits operator_escalation_required with reason=merge_conflict (audit notification; no data loss).
//  5. Truncates the log file (it is ephemeral; conflicts are now in the event log).
//
// Spec ref: specs/reconciliation/spec.md §8.BL3 — Cat-BL3 merge-conflict-log audit.
func RunCatBL3StartupSweep(ctx context.Context, cfg CatBL3StartupSweepConfig) error {
	if cfg.ProjectDir == "" {
		return fmt.Errorf("reconciliation Cat-BL3: ProjectDir must be non-empty")
	}

	logW := cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}

	logPath := filepath.Join(cfg.ProjectDir, beadsMergeConflictsLog)

	info, statErr := os.Stat(logPath)
	if os.IsNotExist(statErr) {
		return nil // nothing to audit
	}
	if statErr != nil {
		return fmt.Errorf("reconciliation Cat-BL3: stat %s: %w", logPath, statErr)
	}
	if info.Size() == 0 {
		return nil // empty — no conflicts
	}

	//nolint:gosec // G304: logPath derived from harmonik project root config
	f, openErr := os.Open(logPath)
	if openErr != nil {
		return fmt.Errorf("reconciliation Cat-BL3: open %s: %w", logPath, openErr)
	}
	conflicts, beadIDs := parseConflictLog(f)
	if closeErr := f.Close(); closeErr != nil {
		slog.WarnContext(ctx, "reconciliation Cat-BL3: close conflict log", "err", closeErr, "path", logPath)
	}

	if len(conflicts) == 0 {
		if err := os.Truncate(logPath, 0); err != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL3: truncate %s: %v\n", logPath, err) //nolint:errcheck // best-effort stderr status log
		}
		return nil
	}

	payload := core.BeadLedgerConflictAuditPayload{
		RunID:     cfg.RunID,
		BeadIDs:   beadIDs,
		Conflicts: conflicts,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return fmt.Errorf("reconciliation Cat-BL3: marshal bead_ledger_conflict_audit: %w", marshalErr)
	}
	if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeBeadLedgerConflictAudit, payloadBytes); emitErr != nil {
		return fmt.Errorf("reconciliation Cat-BL3: emit bead_ledger_conflict_audit: %w", emitErr)
	}

	fmt.Fprintf(logW, "reconciliation Cat-BL3: emitted bead_ledger_conflict_audit (%d conflicts, %d beads)\n", len(conflicts), len(beadIDs)) //nolint:errcheck // best-effort stderr status log

	escalatePayload := core.OperatorEscalationRequiredPayload{
		Reason: core.OperatorEscalationReasonMergeConflict,
	}
	if escalateBytes, marshalErr := json.Marshal(escalatePayload); marshalErr == nil {
		if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeOperatorEscalationRequired, escalateBytes); emitErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL3: emit operator_escalation_required: %v\n", emitErr) //nolint:errcheck // best-effort stderr status log
		}
	}
	emitOperatorMailboxEscalation(ctx, cfg.Emitter, logW, "reconciliation-cat-bl3",
		fmt.Sprintf("Merge-conflict-log audit found %d conflict(s) across %d bead(s) — see bead_ledger_conflict_audit in events.jsonl.",
			len(conflicts), len(beadIDs)),
		strings.Join(beadIDs, ","))

	if err := os.Truncate(logPath, 0); err != nil {
		fmt.Fprintf(logW, "reconciliation Cat-BL3: truncate %s: %v (event already emitted)\n", logPath, err) //nolint:errcheck // best-effort stderr status log
	}

	return nil
}

func parseConflictLog(r io.Reader) (conflicts []core.BeadLedgerConflict, beadIDs []string) {
	seen := make(map[string]struct{})

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		c, ok := parseConflictLine(line)
		if !ok {
			continue
		}
		conflicts = append(conflicts, c)
		if _, already := seen[c.BeadID]; !already {
			seen[c.BeadID] = struct{}{}
			beadIDs = append(beadIDs, c.BeadID)
		}
	}
	return conflicts, beadIDs
}

func parseConflictLine(line string) (core.BeadLedgerConflict, bool) {
	parts := strings.Fields(line)
	if len(parts) < 7 || parts[1] != "CONFLICT" {
		return core.BeadLedgerConflict{}, false
	}
	var c core.BeadLedgerConflict
	for _, part := range parts[2:] {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch k {
		case "bead":
			c.BeadID = v
		case "field":
			c.Field = v
		case "a":
			c.AValue = v
		case "b":
			c.BValue = v
		case "resolution":
			c.Resolution = v
		}
	}
	if c.BeadID == "" {
		return core.BeadLedgerConflict{}, false
	}
	return c, true
}

// CatBL2HandlerConfig holds the construction-time parameters for the
// Cat-BL2 reactive bead-ledger import-failure handler.
type CatBL2HandlerConfig struct {
	// ProjectDir is the harmonik project root. Must be non-empty.
	ProjectDir string

	// BrPath is the absolute path to the `br` binary. Must be non-empty.
	BrPath string

	// Emitter is used to emit bead_ledger_recovered, bead_ledger_corrupt, and
	// operator_escalation_required events. Required.
	Emitter interface {
		Emit(ctx context.Context, eventType core.EventType, payload []byte) error
	}

	// LogWriter receives non-fatal handler status messages. Nil → os.Stderr.
	LogWriter io.Writer
}

// CatBL2Handler is the reactive Cat-BL2 detector: it subscribes to
// bead_sync_failed events and, for each one:
//  1. Retries `br sync --import-only` once.
//  2. On success: emits bead_ledger_recovered{run_id, timestamp}.
//  3. On persistent failure: emits bead_ledger_corrupt{run_id, error, timestamp}
//     and operator_escalation_required{reason=cat_6b_auto_escalated}.
//
// Spec ref: specs/reconciliation/spec.md §8.BL2 — Cat-BL2 bead-ledger import failure.
// Bead ref: hk-k7va9.
type CatBL2Handler struct {
	cfg       CatBL2HandlerConfig
	logWriter io.Writer
}

// NewCatBL2Handler constructs a CatBL2Handler from cfg.
func NewCatBL2Handler(cfg CatBL2HandlerConfig) *CatBL2Handler {
	logW := cfg.LogWriter
	if logW == nil {
		logW = os.Stderr
	}
	return &CatBL2Handler{cfg: cfg, logWriter: logW}
}

// Subscribe registers the Cat-BL2 asynchronous bead_sync_failed consumer with
// the bus. Must be called before bus.Seal per EV-009.
//
// DeclaredEmitTypes: bead_ledger_recovered, bead_ledger_corrupt,
// operator_escalation_required (emitted back to the bus).
func (h *CatBL2Handler) Subscribe(bus eventbus.EventBus) error {
	sub := core.Subscription{
		ConsumerID:    "cat-bl2-ledger-import-failure",
		ConsumerClass: core.ConsumerClassAsynchronous,
		EventPattern: core.EventPattern{
			Types: map[core.EventType]struct{}{
				core.EventTypeBeadSyncFailed: {},
			},
		},
		OnPanic: core.OnPanicRecoverAndLog,
		Handler: h.handleBeadSyncFailed,
	}
	if _, err := bus.Subscribe(sub); err != nil {
		return fmt.Errorf("CatBL2Handler.Subscribe: %w", err)
	}
	return nil
}

func (h *CatBL2Handler) handleBeadSyncFailed(ctx context.Context, evt core.Event) error {
	var pl core.BeadSyncFailedPayload
	if err := json.Unmarshal(evt.Payload, &pl); err != nil {
		fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: unmarshal bead_sync_failed: %v (skipping)\n", err) //nolint:errcheck // best-effort stderr status log
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	//nolint:gosec // G204: BrPath resolved from harmonik config, not user input
	retryCmd := exec.CommandContext(ctx, h.cfg.BrPath, "sync", "--import-only")
	retryCmd.Dir = h.cfg.ProjectDir
	retryOut, retryErr := retryCmd.CombinedOutput()

	if retryErr == nil {
		recovered := core.BeadLedgerRecoveredPayload{
			RunID:     pl.RunID,
			Timestamp: now,
		}
		if b, marshalErr := json.Marshal(recovered); marshalErr == nil {
			if emitErr := h.cfg.Emitter.Emit(ctx, core.EventTypeBeadLedgerRecovered, b); emitErr != nil {
				fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit bead_ledger_recovered: %v\n", emitErr) //nolint:errcheck // best-effort stderr status log
			}
		}
		fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: ledger recovered for run %s\n", pl.RunID) //nolint:errcheck // best-effort stderr status log
		return nil
	}

	errMsg := retryErr.Error()
	if len(retryOut) > 0 {
		errMsg = fmt.Sprintf("%s\n%s", errMsg, strings.TrimRight(string(retryOut), "\n"))
	}
	corrupt := core.BeadLedgerCorruptPayload{
		RunID:     pl.RunID,
		Error:     errMsg,
		Timestamp: now,
	}
	if b, marshalErr := json.Marshal(corrupt); marshalErr == nil {
		if emitErr := h.cfg.Emitter.Emit(ctx, core.EventTypeBeadLedgerCorrupt, b); emitErr != nil {
			fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit bead_ledger_corrupt: %v\n", emitErr) //nolint:errcheck // best-effort stderr status log
		}
	}

	escalate := core.OperatorEscalationRequiredPayload{
		Reason: core.OperatorEscalationReasonCat6bAutoEscalated,
	}
	if b, marshalErr := json.Marshal(escalate); marshalErr == nil {
		if emitErr := h.cfg.Emitter.Emit(ctx, core.EventTypeOperatorEscalationRequired, b); emitErr != nil {
			fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit operator_escalation_required: %v\n", emitErr) //nolint:errcheck // best-effort stderr status log
		}
	}
	emitOperatorMailboxEscalation(ctx, h.cfg.Emitter, h.logWriter, "reconciliation-cat-bl2",
		fmt.Sprintf("Bead-ledger import failed after retry for run %s: %s", pl.RunID, errMsg),
		pl.RunID)

	fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: ledger corrupt for run %s — escalated to operator (Cat 6b): %s\n", pl.RunID, retryErr) //nolint:errcheck // best-effort stderr status log
	return nil
}
