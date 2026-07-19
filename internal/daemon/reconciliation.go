package daemon

// reconciliation.go — Cat-BL1, Cat-BL2, and Cat-BL3 detectors.
//
// Cat-BL1 (§8.BL1): child-bead orphan detector. At startup, enumerates beads
// with parent:hk-* labels, checks git for parent-run merge commits, closes
// orphans; escalates to operator if orphan is in_progress.
//
// Cat-BL2 (§8.BL2): bead-ledger import-failure reactive handler. Subscribes to
// bead_sync_failed events; retries `br sync --import-only` once; emits
// bead_ledger_recovered on success or bead_ledger_corrupt + Cat 6b escalation
// on persistent failure.
//
// Cat-BL3 (§8.BL3): merge-conflict-log audit. At startup, checks for a
// non-empty .beads/merge-conflicts.log, emits bead_ledger_conflict_audit, and
// truncates the log file.
//
// Every Cat-6 operator_escalation_required emission below is paired with an
// emitOperatorMailboxEscalation call (bead hk-u4dv4) so it also lands in the
// single operator-mailbox projection (`harmonik mailbox`), not just the raw
// event log.
//
// Spec ref: specs/reconciliation/spec.md §8.BL1, §8.BL2, §8.BL3.
// Bead ref: hk-27ghc, hk-k7va9, hk-u4dv4.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/eventbus"
)

// emitOperatorMailboxEscalation raises a decision_needed event tagged with the
// reserved operator-mailbox topic (core.DecisionTopicOperatorMailbox) so every
// Cat-6 operator_escalation_required this package emits ALSO lands in the
// single "harmonik mailbox" projection (`harmonik mailbox` / `decisions list
// --topic operator-mailbox`) instead of only the raw operator_escalation_required
// event, which no operator surface renders directly. Mirrors the CLI convention
// `harmonik decisions raise --topic operator-mailbox --from <source>` that
// stall-sentinel's Tier-3 (operator) escalation is designed to use once built
// (plans/2026-07-02-stall-sentinel/DESIGN.md §3, §7 item 5; bead hk-u4dv4).
//
// Non-fatal: a marshal or emit failure is logged and swallowed — the caller's
// operator_escalation_required emission (the durable audit record) has already
// happened or is unaffected either way.
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
		fmt.Fprintf(logW, "reconciliation: marshal decision_needed for operator-mailbox (%s): %v\n", source, marshalErr)
		return
	}
	if emitErr := emitter.Emit(ctx, core.EventTypeDecisionNeeded, b); emitErr != nil {
		fmt.Fprintf(logW, "reconciliation: emit decision_needed (operator-mailbox) for %s: %v\n", source, emitErr)
	}
}

// parentLabelPrefix is the label prefix used by child beads to record their
// parent bead lineage per specs/beads-integration.md §4.8b BI-010e (T0 rename:
// parent:hk-* not codename:hk-*).
const parentLabelPrefix = "parent:hk-"

// beadsMergeConflictsLog is the path of the merge-conflict log file relative
// to the project root .beads/ directory.
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

	// Collect open + in_progress beads — both are active and could be orphans.
	candidates, collectErr := collectParentLabeledBeads(scanCtx, adapter, logW)
	if collectErr != nil {
		return fmt.Errorf("reconciliation Cat-BL1: collect candidates: %w", collectErr)
	}
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
			fmt.Fprintf(logW, "reconciliation Cat-BL1: bead %s git scan for parent %s: %v (skipping)\n",
				rec.BeadID, parentID, gitErr)
			continue
		}
		if hasCommit {
			continue // parent ran and merged — not an orphan
		}

		// Orphan detected: emit orphaned_child_bead event.
		orphanPayload := core.OrphanedChildBeadPayload{
			BeadID:   string(rec.BeadID), //nolint:unconvert // BeadID is type alias of string; explicit for clarity
			ParentID: parentID,
		}
		if payloadBytes, marshalErr := json.Marshal(orphanPayload); marshalErr == nil {
			if emitErr := cfg.Emitter.Emit(scanCtx, core.EventTypeOrphanedChildBead, payloadBytes); emitErr != nil {
				fmt.Fprintf(logW, "reconciliation Cat-BL1: emit orphaned_child_bead for %s: %v\n",
					rec.BeadID, emitErr)
			}
		}

		if rec.Status == core.CoarseStatusInProgress {
			// Exception: in_progress orphan → escalate to operator, do not auto-close.
			fmt.Fprintf(logW, "reconciliation Cat-BL1: bead %s in_progress with orphaned parent %s — escalating\n",
				rec.BeadID, parentID)
			escalatePayload := core.OperatorEscalationRequiredPayload{
				Reason: core.OperatorEscalationReasonOtherVerdictDriven,
			}
			if escalateBytes, marshalErr := json.Marshal(escalatePayload); marshalErr == nil {
				if emitErr := cfg.Emitter.Emit(scanCtx, core.EventTypeOperatorEscalationRequired, escalateBytes); emitErr != nil {
					fmt.Fprintf(logW, "reconciliation Cat-BL1: emit operator_escalation_required for %s: %v\n",
						rec.BeadID, emitErr)
				}
			}
			emitOperatorMailboxEscalation(scanCtx, cfg.Emitter, logW, "reconciliation-cat-bl1",
				fmt.Sprintf("Bead %s is in_progress with orphaned parent %s (parent run never merged on %s) — verify and resolve manually.",
					rec.BeadID, parentID, targetBranch),
				string(rec.BeadID))
			continue
		}

		// Open orphan: auto-close via SweepCloseBead.
		closeErr := adapter.SweepCloseBead(scanCtx, timeoutCfg, rec.BeadID)
		if closeErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: close orphan bead %s (parent %s): %v\n",
				rec.BeadID, parentID, closeErr)
			continue
		}
		fmt.Fprintf(logW, "reconciliation Cat-BL1: closed orphan bead %s (parent %s run discarded)\n",
			rec.BeadID, parentID)
	}

	return nil
}

// collectParentLabeledBeads lists all open and in_progress beads and returns
// those that carry at least one parent:hk-* label. Non-fatal list errors for
// one status are logged; the other status is still queried.
func collectParentLabeledBeads(ctx context.Context, adapter *brcli.Adapter, logW io.Writer) ([]core.BeadRecord, error) {
	var candidates []core.BeadRecord

	for _, status := range []string{"open", "in_progress"} {
		beads, listErr := adapter.ListBeadsByStatus(ctx, status)
		if listErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL1: list %s beads: %v (skipping status)\n", status, listErr)
			continue
		}
		for _, rec := range beads {
			if hasParentLabel(rec.Labels) {
				candidates = append(candidates, rec)
			}
		}
	}
	return candidates, nil
}

// hasParentLabel reports whether any label in labels has the parent:hk-* prefix.
func hasParentLabel(labels []string) bool {
	for _, l := range labels {
		if strings.HasPrefix(l, parentLabelPrefix) {
			return true
		}
	}
	return false
}

// extractParentBeadID scans labels for a parent:hk-* label and returns the
// parent bead ID (e.g. "hk-e3fy") and true. Returns "", false if not found.
func extractParentBeadID(labels []string) (string, bool) {
	for _, l := range labels {
		if strings.HasPrefix(l, parentLabelPrefix) {
			// label = "parent:hk-e3fy" → parent bead ID = "hk-e3fy"
			return "hk-" + strings.TrimPrefix(l, "parent:hk-"), true
		}
	}
	return "", false
}

// hasParentMergeCommit checks whether a commit referencing parentBeadID via
// "Refs: <parentBeadID>" appears in the git log of targetBranch.
//
// Returns (true, nil) when a match is found, (false, nil) when no match, and
// (false, err) on git execution failure.
//
// The --all flag is intentionally omitted: the spec requires checking for a
// merge commit on main (the target branch), not on any branch. Using --all
// would produce false positives from in-flight worktree branches.
//
// Spec ref: reconciliation/spec.md §8.BL1 detection rule.
func hasParentMergeCommit(ctx context.Context, projectDir, targetBranch, parentBeadID string) (bool, error) {
	//nolint:gosec // G204: projectDir resolved from harmonik config; parentBeadID is a bead-ID suffix, not user input.
	cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "log", "-1",
		"--grep", "Refs: "+parentBeadID, "--format=%H", targetBranch)
	out, err := cmd.Output()
	if err != nil {
		// Return the error so the caller can skip this bead rather than treating
		// exec failure (missing repo, absent target branch) as a clean no-match.
		// A clean no-match exits 0 with empty output; a non-zero exit is a distinct
		// signal that the git state is unknown — skipping is safer than auto-closing.
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
	_ = f.Close()

	if len(conflicts) == 0 {
		// File was non-empty but no parseable lines — truncate anyway to avoid
		// re-processing on next startup.
		if err := os.Truncate(logPath, 0); err != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL3: truncate %s: %v\n", logPath, err)
		}
		return nil
	}

	// Emit bead_ledger_conflict_audit event.
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

	fmt.Fprintf(logW, "reconciliation Cat-BL3: emitted bead_ledger_conflict_audit (%d conflicts, %d beads)\n",
		len(conflicts), len(beadIDs))

	// Emit operator_escalation_required — audit notification; no data loss.
	escalatePayload := core.OperatorEscalationRequiredPayload{
		Reason: core.OperatorEscalationReasonMergeConflict,
	}
	if escalateBytes, marshalErr := json.Marshal(escalatePayload); marshalErr == nil {
		if emitErr := cfg.Emitter.Emit(ctx, core.EventTypeOperatorEscalationRequired, escalateBytes); emitErr != nil {
			fmt.Fprintf(logW, "reconciliation Cat-BL3: emit operator_escalation_required: %v\n", emitErr)
		}
	}
	emitOperatorMailboxEscalation(ctx, cfg.Emitter, logW, "reconciliation-cat-bl3",
		fmt.Sprintf("Merge-conflict-log audit found %d conflict(s) across %d bead(s) — see bead_ledger_conflict_audit in events.jsonl.",
			len(conflicts), len(beadIDs)),
		strings.Join(beadIDs, ","))

	// Truncate the log — conflicts are now durable in the event log.
	if err := os.Truncate(logPath, 0); err != nil {
		// Non-fatal: log the failure; the event has been emitted.
		fmt.Fprintf(logW, "reconciliation Cat-BL3: truncate %s: %v (event already emitted)\n", logPath, err)
	}

	return nil
}

// parseConflictLog reads lines from r and returns (conflicts, beadIDs).
// Each line follows the format written by harmonik beads-merge:
//
//	<iso8601-timestamp> CONFLICT bead=<id> field=<field> a=<a_val> b=<b_val> resolution=<res>
//
// Malformed lines are silently skipped. beadIDs is deduplicated.
func parseConflictLog(r io.Reader) ([]core.BeadLedgerConflict, []string) {
	var conflicts []core.BeadLedgerConflict
	seen := make(map[string]struct{})
	var beadIDs []string

	scanner := bufio.NewScanner(r)
	// Conflict lines embed full field values (a=..., b=...), which can exceed
	// the default 64KB token limit; raise it so the scan does not abort.
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

// parseConflictLine parses one merge-conflicts.log line into a BeadLedgerConflict.
// Format: "<timestamp> CONFLICT bead=<id> field=<f> a=<a> b=<b> resolution=<r>".
// Returns (zero, false) on malformed input.
func parseConflictLine(line string) (core.BeadLedgerConflict, bool) {
	parts := strings.Fields(line)
	// Minimum viable line: <ts> CONFLICT bead=<id> field=<f> a=<a> b=<b> resolution=<r> = 7 parts.
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

// ---------------------------------------------------------------------------
// Cat-BL2: bead-ledger import-failure reactive handler (§8.BL2)
// ---------------------------------------------------------------------------

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

// handleBeadSyncFailed is the Cat-BL2 event handler. It retries `br sync
// --import-only` once and emits the appropriate outcome event.
func (h *CatBL2Handler) handleBeadSyncFailed(ctx context.Context, evt core.Event) error {
	var pl core.BeadSyncFailedPayload
	if err := json.Unmarshal(evt.Payload, &pl); err != nil {
		fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: unmarshal bead_sync_failed: %v (skipping)\n", err)
		return nil
	}

	now := time.Now().UTC().Format(time.RFC3339)

	//nolint:gosec // G204: BrPath resolved from harmonik config, not user input
	retryCmd := exec.CommandContext(ctx, h.cfg.BrPath, "sync", "--import-only")
	retryCmd.Dir = h.cfg.ProjectDir
	retryOut, retryErr := retryCmd.CombinedOutput()

	if retryErr == nil {
		// Retry succeeded: emit bead_ledger_recovered.
		recovered := core.BeadLedgerRecoveredPayload{
			RunID:     pl.RunID,
			Timestamp: now,
		}
		if b, marshalErr := json.Marshal(recovered); marshalErr == nil {
			if emitErr := h.cfg.Emitter.Emit(ctx, core.EventTypeBeadLedgerRecovered, b); emitErr != nil {
				fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit bead_ledger_recovered: %v\n", emitErr)
			}
		}
		fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: ledger recovered for run %s\n", pl.RunID)
		return nil
	}

	// Retry failed: emit bead_ledger_corrupt + Cat 6b escalation.
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
			fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit bead_ledger_corrupt: %v\n", emitErr)
		}
	}

	escalate := core.OperatorEscalationRequiredPayload{
		Reason: core.OperatorEscalationReasonCat6bAutoEscalated,
	}
	if b, marshalErr := json.Marshal(escalate); marshalErr == nil {
		if emitErr := h.cfg.Emitter.Emit(ctx, core.EventTypeOperatorEscalationRequired, b); emitErr != nil {
			fmt.Fprintf(h.logWriter, "reconciliation Cat-BL2: emit operator_escalation_required: %v\n", emitErr)
		}
	}
	emitOperatorMailboxEscalation(ctx, h.cfg.Emitter, h.logWriter, "reconciliation-cat-bl2",
		fmt.Sprintf("Bead-ledger import failed after retry for run %s: %s", pl.RunID, errMsg),
		pl.RunID)

	fmt.Fprintf(h.logWriter,
		"reconciliation Cat-BL2: ledger corrupt for run %s — escalated to operator (Cat 6b): %s\n",
		pl.RunID, retryErr)
	return nil
}
