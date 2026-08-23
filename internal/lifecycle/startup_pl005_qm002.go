package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

// ErrQueueSchemaUnsupported is returned by LoadQueueAtStartup when the loaded
// queue.json carries an unrecognised schema_version (forward-incompatible per
// QM-002).
//
// The daemon MUST refuse startup with exit code 2 when this error is returned.
//
// Spec ref: specs/queue-model.md §3.2 QM-002 — "Any other value is
// forward-incompatible … refuses startup with exit code 2."
var ErrQueueSchemaUnsupported = fmt.Errorf("lifecycle: queue.json schema_version is not in the supported read-set; exit code 2 required per QM-002")

// BeadLedger is the minimal interface LoadQueueAtStartup needs to query the
// Beads ledger for item status during QM-002a/QM-002b startup cross-checks.
//
// The production implementation is *brcli.Adapter. Tests inject a deterministic
// fake.
//
// Spec ref: specs/queue-model.md §3.2a QM-002a — "call `br show <bead_id>`".
// Spec ref: specs/queue-model.md §3.2b QM-002b — full three-way reconciliation.
// Spec ref: specs/beads-integration.md §4.5 BI-015, §4.5 BI-016.
type BeadLedger interface {
	// ShowBead invokes `br show <id> --format json` and returns the parsed
	// BeadRecord for the given bead ID.
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)

	// ListInFlightBeads invokes `br list --status in_progress --json` and
	// returns a BeadRecord slice for all beads currently in_progress.
	// Used by QM-002b three-way reconciliation to detect beads that are
	// in_progress in the ledger but have no queue record.
	ListInFlightBeads(ctx context.Context) ([]core.BeadRecord, error)
}

// QueueEventEmitter is the minimal event-emission interface needed by
// LoadQueueAtStartup to emit queue_item_reconciled events per QM-002a.
//
// The production implementation is eventbus.EventBus. Tests inject a recording
// fake so test scenario (d) can assert the event fires BEFORE any
// dispatch-loop tick log line.
type QueueEventEmitter interface {
	// Emit emits an event on the bus.
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// QM002bReapConfig carries optional dependencies for the QM-002b Class B
// ("bead_inprogress_queue_absent") reap path. When nil (or when Resetter is
// nil), Class B beads are observed (mismatch event emitted) but not reaped —
// existing behaviour. When fully populated, Class B orphans are:
//  1. Reset to open (dispatchable) via the BI-010d reset write.
//  2. Their worktrees (discovered via cancelled/failed queue archives) are
//     removed via git worktree remove --force --force.
//
// This config is optional and backward-compatible: callers that omit it get
// the old observe-only behaviour.
//
// Spec ref: hk-5pg37 — reconciler must reap orphans from queue-cancel+restart.
type QM002bReapConfig struct {
	// Resetter is the BI adapter write surface for the BI-010d reset op
	// (in_progress → open). When nil, reaping is skipped.
	Resetter BeadResetter

	// IntentLogDir is the absolute path of .harmonik/beads-intents/.
	IntentLogDir string

	// ProjectHash is the per-project provenance marker per PL-006a.
	ProjectHash core.ProjectHash

	// DaemonStartNS is the daemon's startup wall-clock time in nanoseconds.
	// Used to derive the BI-010d idempotency key
	// `<project_hash>:<bead_id>:reset:<daemon_start_ns>`.
	DaemonStartNS int64

	// BrTimeoutCfg is the BI-025c timeout configuration forwarded to ResetBead.
	// Zero value is acceptable (defaults apply).
	BrTimeoutCfg brcli.TimeoutConfig
}

// LoadQueueAtStartup implements PL-005 step 8a for the queue subsystem.
//
// It first runs MigrateFromLegacy to promote any pre-NQ-A2 .harmonik/queue.json
// singleton to .harmonik/queues/main.json (one-shot migration; no-op thereafter).
// It then enumerates all per-queue files under .harmonik/queues/ and loads each
// one, running QM-002a + QM-002b reconciliation on every loaded queue.
//
// Per-queue outcomes per QM-002:
//
//   - File absent (empty queues/ dir): returns (nil, nil). The daemon starts
//     with no active queues.
//   - Corrupt / unparseable: that queue is skipped with a structured warning.
//     The daemon continues without it; the file is NOT deleted.
//   - schema_version unsupported (forward-incompatible): returns
//     (nil, ErrQueueSchemaUnsupported). The caller MUST refuse startup with
//     exit code 2.
//   - Clean parse: included in the returned slice after QM-002a + QM-002b.
//
// The emitter parameter MAY be nil; when nil, events are not emitted (useful
// for testing scenarios that don't care about event emission, but production
// callers MUST supply a non-nil emitter).
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
// Spec ref: specs/queue-model.md §3.2a QM-002a.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
// Bead ref: hk-tigaf.3.
func LoadQueueAtStartup(
	ctx context.Context,
	projectDir string,
	ledger BeadLedger,
	emitter QueueEventEmitter,
	logger *slog.Logger,
	reapCfg ...*QM002bReapConfig,
) ([]*queue.Queue, error) {
	if logger == nil {
		logger = slog.Default()
	}

	var classBReap *QM002bReapConfig
	if len(reapCfg) > 0 {
		classBReap = reapCfg[0]
	}

	if err := PrepareQueueNamespaceAtStartup(ctx, projectDir, logger); err != nil {
		return nil, err
	}

	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		logger.WarnContext(ctx, "queue: EnumerateQueueNames failed; starting with no active queues",
			"error", err,
		)
		return nil, nil
	}

	var loaded []*queue.Queue
	for _, name := range names {
		q, loadErr := loadOneQueueAtStartup(ctx, projectDir, name, ledger, emitter, logger, classBReap)
		if loadErr != nil {
			return nil, loadErr
		}
		if q != nil {
			loaded = append(loaded, q)
		}
	}
	return loaded, nil
}

// PrepareQueueNamespaceAtStartup resolves queue namespace transactions before
// any caller reads queue facts for dispatch replay or normal reconciliation.
func PrepareQueueNamespaceAtStartup(ctx context.Context, projectDir string, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	return prepareQueueNamespaceAtStartup(ctx, projectDir, logger, func() time.Time { return time.Now().UTC() })
}

func prepareQueueNamespaceAtStartup(
	ctx context.Context,
	projectDir string,
	logger *slog.Logger,
	releaseTime func() time.Time,
) error {
	if err := queue.MigrateFromLegacy(ctx, projectDir); err != nil {
		logger.ErrorContext(ctx, "queue: MigrateFromLegacy failed; startup refuses to choose a queue", "error", err)
		return fmt.Errorf("lifecycle: queue migration failed: %w", err)
	}

	if err := recoverReplaceIntents(ctx, projectDir, logger); err != nil {
		return err
	}
	return recoverCompletionReleaseMarkers(ctx, projectDir, logger, releaseTime)
}

func recoverCompletionReleaseMarkers(
	ctx context.Context,
	projectDir string,
	logger *slog.Logger,
	releaseTime func() time.Time,
) error {
	recoveries, err := queue.RecoverCompletionReleaseMarkers(projectDir, releaseTime)
	if err != nil {
		logger.ErrorContext(ctx, "queue: completion marker recovery could not read the receipt root", "error", err)
		return fmt.Errorf("lifecycle: recover completion release markers: %w", err)
	}
	for _, recovery := range recoveries {
		if recovery.Err != nil {
			logger.WarnContext(ctx, "queue: completion release marker remains pending",
				"queue_id", recovery.QueueID,
				"receipt_id", recovery.ReceiptID,
				"error", recovery.Err,
			)
			continue
		}
		if recovery.Marker != nil {
			logger.InfoContext(ctx, "queue: installed a completion release marker after restart",
				"queue_id", recovery.QueueID,
				"receipt_id", recovery.ReceiptID,
			)
		}
	}
	return nil
}

func recoverReplaceIntents(ctx context.Context, projectDir string, logger *slog.Logger) error {
	recoveries, err := queue.RecoverReplaceIntents(projectDir)
	if err != nil {
		logger.WarnContext(ctx, "queue: replace-intent recovery could not read the queues directory",
			"error", err,
		)
		return fmt.Errorf("lifecycle: read replace intents: %w", err)
	}
	unresolved := make([]error, 0, len(recoveries))
	for _, r := range recoveries {
		if r.Resolved() {
			logger.InfoContext(ctx, "queue: resolved a replace intent left by an earlier crash",
				"queue_name", r.NormalizedName,
				"action", string(r.Action),
			)
			continue
		}
		logger.ErrorContext(ctx, "queue: replace intent could not be resolved; this queue refuses further mutation until the intent file is dealt with",
			"queue_name", r.NormalizedName,
			"intent_path", filepath.Join(projectDir, ".harmonik", "queues", r.NormalizedName+".replace-intent"),
			"error", r.Err,
		)
		unresolved = append(unresolved, fmt.Errorf("queue %q replace intent: %w", r.NormalizedName, r.Err))
	}
	if len(unresolved) > 0 {
		return fmt.Errorf("lifecycle: unresolved replace intents: %w", errors.Join(unresolved...))
	}
	return nil
}

func loadOneQueueAtStartup(
	ctx context.Context,
	projectDir string,
	name string,
	ledger BeadLedger,
	emitter QueueEventEmitter,
	logger *slog.Logger,
	classBReap *QM002bReapConfig,
) (*queue.Queue, error) {
	q, err := queue.Load(ctx, projectDir, name)
	if err != nil {
		if errors.Is(err, queue.ErrSchemaVersion) {
			logger.ErrorContext(ctx, "queue file schema_version not in supported read-set; startup refused per QM-002",
				"queue_name", name,
				"error", err,
			)
			return nil, ErrQueueSchemaUnsupported
		}
		if errors.Is(err, queue.ErrCorrupt) {
			logger.WarnContext(ctx, "queue file is present but unparseable; treating as absent per QM-002",
				"queue_name", name,
				"error", err,
			)
			return nil, nil
		}
		logger.WarnContext(ctx, "queue file read failed unexpectedly; treating as absent",
			"queue_name", name,
			"error", err,
		)
		return nil, nil
	}

	if q == nil {
		return nil, nil
	}

	if err := reconcileDispatchedItems(ctx, projectDir, q, ledger, emitter, logger); err != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: QM-002a reconcile: %w", name, err)
	}

	if q.Status == queue.QueueStatusPausedByDrain && q.ResumeOnStart {
		if resumeErr := queue.ResumeQueueFromDrain(q); resumeErr != nil {
			return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: resume shutdown drain: %w", name, resumeErr)
		}
		if persistErr := queue.Persist(ctx, projectDir, q); persistErr != nil {
			return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: persist resumed shutdown drain: %w", name, persistErr)
		}
		logger.InfoContext(ctx, "queue: resumed clean-shutdown drain",
			"queue_name", name,
			"queue_id", q.QueueID,
		)
	}

	if err := reconcileThreeWay(ctx, projectDir, q, ledger, emitter, logger, classBReap); err != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: QM-002b three-way reconcile: %w", name, err)
	}

	done, termErr := reconcileQueueTerminalState(ctx, projectDir, q, logger)
	if termErr != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: terminal-state reconcile: %w", name, termErr)
	}
	if done {
		return nil, nil
	}

	return q, nil
}

type reconciledEvent struct {
	payload []byte
}

func reconcileDispatchedItems(
	ctx context.Context,
	projectDir string,
	q *queue.Queue,
	ledger BeadLedger,
	emitter QueueEventEmitter,
	logger *slog.Logger,
) error {
	var pending []reconciledEvent

	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := &q.Groups[gi].Items[ii]
			if item.Status != queue.ItemStatusDispatched {
				continue
			}

			record, showErr := ledger.ShowBead(ctx, item.BeadID)
			if showErr != nil {
				logger.WarnContext(ctx, "QM-002a: ShowBead failed; skipping reconcile for item",
					"bead_id", string(item.BeadID),
					"error", showErr,
				)
				continue
			}

			if record.Status != core.CoarseStatusOpen {
				continue
			}

			reconciledAt := time.Now().UTC()

			logger.InfoContext(ctx, "QM-002a: reverting dispatched item to pending (claim_write_lost)",
				"bead_id", string(item.BeadID),
				"group_index", gi,
			)

			if transitionErr := queue.RecoverDispatchedItemToPending(item); transitionErr != nil {
				return fmt.Errorf("QM-002a: recover dispatched item: %w", transitionErr)
			}

			if emitter != nil {
				evPayload := core.QueueItemReconciledPayload{
					QueueID:      q.QueueID,
					GroupIndex:   gi,
					BeadID:       string(item.BeadID),
					Reason:       "claim_write_lost",
					ReconciledAt: reconciledAt.Format(time.RFC3339Nano),
				}
				payloadBytes, marshalErr := json.Marshal(evPayload)
				if marshalErr != nil {
					logger.WarnContext(ctx, "QM-002a: failed to marshal queue_item_reconciled payload",
						"bead_id", string(item.BeadID),
						"error", marshalErr,
					)
				} else {
					pending = append(pending, reconciledEvent{payload: payloadBytes})
				}
			}
		}
	}

	if len(pending) == 0 {
		return nil
	}

	if err := queue.Persist(ctx, projectDir, q); err != nil {
		return fmt.Errorf("QM-002a: persist corrected queue: %w", err)
	}

	for _, ev := range pending {
		if err := emitter.Emit(ctx, core.EventTypeQueueItemReconciled, ev.payload); err != nil {
			logger.WarnContext(ctx, "QM-002a: failed to emit queue_item_reconciled event",
				"error", err,
			)
		}
	}

	return nil
}

type qm002bPendingEvent struct {
	eventType core.EventType
	payload   []byte
}

func appendMismatchObserved(
	ctx context.Context,
	logger *slog.Logger,
	events []qm002bPendingEvent,
	p core.ReconciliationMismatchObservedPayload,
) []qm002bPendingEvent {
	payloadBytes, marshalErr := json.Marshal(p)
	if marshalErr != nil {
		logger.WarnContext(ctx, "QM-002b: failed to marshal mismatch payload",
			"bead_id", p.BeadID,
			"mismatch_class", p.MismatchClass,
			"error", marshalErr,
		)
		return events
	}
	return append(events, qm002bPendingEvent{
		eventType: core.EventTypeReconciliationMismatchObserved,
		payload:   payloadBytes,
	})
}

func reconcileThreeWay(
	ctx context.Context,
	projectDir string,
	q *queue.Queue,
	ledger BeadLedger,
	emitter QueueEventEmitter,
	logger *slog.Logger,
	classBReap *QM002bReapConfig,
) error {
	observedAt := time.Now().UTC().Format(time.RFC3339Nano)

	beadsInQueue := make(map[core.BeadID]queue.ItemStatus)

	var pendingEvents []qm002bPendingEvent
	var classACount int

	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := &q.Groups[gi].Items[ii]

			beadsInQueue[item.BeadID] = item.Status

			isPendingLike := item.Status == queue.ItemStatusPending ||
				item.Status == queue.ItemStatusDeferredForLedgerDep
			if !isPendingLike {
				isQueueTerminal := item.Status == queue.ItemStatusCompleted ||
					item.Status == queue.ItemStatusFailed
				if !isQueueTerminal {
					if item.Status != queue.ItemStatusDispatched {
						continue
					}
					dispRecord, dispShowErr := ledger.ShowBead(ctx, item.BeadID)
					if dispShowErr != nil {
						logger.WarnContext(ctx, "QM-002b Class A': ShowBead failed; skipping",
							"bead_id", string(item.BeadID),
							"error", dispShowErr,
						)
						continue
					}
					isDispLedgerTerminal := dispRecord.Status.IsTerminal()
					if !isDispLedgerTerminal {
						continue
					}
					logger.InfoContext(ctx, "QM-002b Class A': advancing dispatched item to completed (bead_closed_queue_dispatched)",
						"bead_id", string(item.BeadID),
						"group_index", gi,
						"ledger_status", string(dispRecord.Status),
					)
					if transitionErr := queue.ReconcileItemToCompleted(item); transitionErr != nil {
						return fmt.Errorf("QM-002b Class A': reconcile dispatched item: %w", transitionErr)
					}
					classACount++
					if emitter != nil {
						pendingEvents = appendMismatchObserved(ctx, logger, pendingEvents, core.ReconciliationMismatchObservedPayload{
							QueueID:       q.QueueID,
							GroupIndex:    gi,
							BeadID:        string(item.BeadID),
							MismatchClass: "bead_closed_queue_dispatched",
							LedgerStatus:  string(dispRecord.Status),
							QueueStatus:   "dispatched",
							ObservedAt:    observedAt,
						})
					}
					continue
				}
				record, showErr := ledger.ShowBead(ctx, item.BeadID)
				if showErr != nil {
					logger.WarnContext(ctx, "QM-002b Class C: ShowBead failed; skipping",
						"bead_id", string(item.BeadID),
						"error", showErr,
					)
					continue
				}
				if record.Status != core.CoarseStatusInProgress {
					continue
				}
				logger.WarnContext(ctx, "QM-002b Class C: queue item terminal but ledger in_progress (bead_closed_queue_inprogress)",
					"bead_id", string(item.BeadID),
					"queue_status", string(item.Status),
					"group_index", gi,
				)
				if emitter != nil {
					pendingEvents = appendMismatchObserved(ctx, logger, pendingEvents, core.ReconciliationMismatchObservedPayload{
						QueueID:       q.QueueID,
						GroupIndex:    gi,
						BeadID:        string(item.BeadID),
						MismatchClass: "bead_closed_queue_inprogress",
						LedgerStatus:  string(record.Status),
						QueueStatus:   string(item.Status),
						ObservedAt:    observedAt,
					})
				}
				continue
			}

			record, showErr := ledger.ShowBead(ctx, item.BeadID)
			if showErr != nil {
				logger.WarnContext(ctx, "QM-002b Class A: ShowBead failed; skipping",
					"bead_id", string(item.BeadID),
					"error", showErr,
				)
				continue
			}
			isLedgerTerminal := record.Status.IsTerminal()
			if !isLedgerTerminal {
				continue
			}

			logger.InfoContext(ctx, "QM-002b Class A: advancing pending item to completed (bead_closed_queue_pending)",
				"bead_id", string(item.BeadID),
				"group_index", gi,
				"ledger_status", string(record.Status),
			)

			if transitionErr := queue.ReconcileItemToCompleted(item); transitionErr != nil {
				return fmt.Errorf("QM-002b Class A: reconcile pending item: %w", transitionErr)
			}
			classACount++

			if emitter != nil {
				pendingEvents = appendMismatchObserved(ctx, logger, pendingEvents, core.ReconciliationMismatchObservedPayload{
					QueueID:       q.QueueID,
					GroupIndex:    gi,
					BeadID:        string(item.BeadID),
					MismatchClass: "bead_closed_queue_pending",
					LedgerStatus:  string(record.Status),
					QueueStatus:   "pending",
					ObservedAt:    observedAt,
				})
			}
		}
	}

	if classACount > 0 {
		if err := queue.Persist(ctx, projectDir, q); err != nil {
			return fmt.Errorf("QM-002b: persist Class A corrections: %w", err)
		}
	}

	reapEnabled := classBReap != nil && classBReap.Resetter != nil
	if emitter != nil || reapEnabled {
		inFlight, listErr := ledger.ListInFlightBeads(ctx)
		if listErr != nil {
			logger.WarnContext(ctx, "QM-002b Class B: ListInFlightBeads failed; skipping orphan check",
				"error", listErr,
			)
		} else {
			var classBOrphans []core.BeadID
			for _, rec := range inFlight {
				if _, inQueue := beadsInQueue[rec.BeadID]; inQueue {
					continue // bead has a queue item — not a Class B orphan
				}
				logger.InfoContext(ctx, "QM-002b Class B: ledger in_progress bead has no queue item (bead_inprogress_queue_absent)",
					"bead_id", string(rec.BeadID),
				)
				if emitter != nil {
					pendingEvents = appendMismatchObserved(ctx, logger, pendingEvents, core.ReconciliationMismatchObservedPayload{
						QueueID:       "",
						GroupIndex:    -1,
						BeadID:        string(rec.BeadID),
						MismatchClass: "bead_inprogress_queue_absent",
						LedgerStatus:  string(rec.Status),
						QueueStatus:   "",
						ObservedAt:    observedAt,
					})
				}
				if reapEnabled {
					classBOrphans = append(classBOrphans, rec.BeadID)
				}
			}
			for _, beadID := range classBOrphans {
				reapClassBOrphan(ctx, projectDir, beadID, classBReap, logger)
			}
		}
	}

	for _, ev := range pendingEvents {
		if err := emitter.Emit(ctx, ev.eventType, ev.payload); err != nil {
			logger.WarnContext(ctx, "QM-002b: failed to emit reconciliation_mismatch_observed event",
				"error", err,
			)
		}
	}

	return nil
}

func reapClassBOrphan(
	ctx context.Context,
	projectDir string,
	beadID core.BeadID,
	cfg *QM002bReapConfig,
	logger *slog.Logger,
) {
	logger.InfoContext(ctx, "QM-002b Class B: reaping orphaned bead (queue_cancel_reap)",
		"bead_id", string(beadID),
	)
	if resetErr := cfg.Resetter.ResetBead(
		ctx,
		cfg.IntentLogDir,
		cfg.BrTimeoutCfg,
		beadID,
		cfg.ProjectHash,
		cfg.DaemonStartNS,
	); resetErr != nil {
		logger.WarnContext(ctx, "QM-002b Class B: ResetBead failed; bead remains in_progress",
			"bead_id", string(beadID),
			"error", resetErr,
		)
		return
	}
	logger.InfoContext(ctx, "QM-002b Class B: bead reset to open",
		"bead_id", string(beadID),
	)
	reapOrphanWorktreesFromArchives(ctx, projectDir, beadID, logger)
}

func reapOrphanWorktreesFromArchives(
	ctx context.Context,
	projectDir string,
	beadID core.BeadID,
	logger *slog.Logger,
) {
	queuesDir := filepath.Join(projectDir, ".harmonik", "queues")
	entries, err := os.ReadDir(queuesDir)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.WarnContext(ctx, "QM-002b reap: ReadDir queues failed",
				"bead_id", string(beadID),
				"error", err,
			)
		}
		return
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.Contains(name, ".cancelled-") && !strings.Contains(name, ".failed-") {
			continue
		}
		archivePath := filepath.Join(queuesDir, name)
		//nolint:gosec // G304: path is constructed from projectDir + .harmonik/queues/ + entry name
		data, readErr := os.ReadFile(archivePath)
		if readErr != nil {
			logger.WarnContext(ctx, "QM-002b reap: read archive failed",
				"bead_id", string(beadID),
				"archive", name,
				"error", readErr,
			)
			continue
		}
		var q queue.Queue
		if unmarshalErr := json.Unmarshal(data, &q); unmarshalErr != nil {
			continue // skip corrupt archives silently
		}
		for _, g := range q.Groups {
			for _, item := range g.Items {
				if item.BeadID != beadID {
					continue
				}
				if item.RunID == nil || *item.RunID == "" {
					continue
				}
				wtPath := filepath.Join(projectDir, ".harmonik", "worktrees", *item.RunID)
				if _, statErr := os.Stat(wtPath); os.IsNotExist(statErr) {
					continue // already gone
				}
				logger.InfoContext(ctx, "QM-002b reap: removing orphaned worktree",
					"bead_id", string(beadID),
					"run_id", *item.RunID,
					"path", wtPath,
				)
				//nolint:gosec // G204: projectDir is operator-controlled; runID is a uuid from queue.json
				cmd := exec.CommandContext(ctx, "git", "-C", projectDir, "worktree", "remove", "--force", "--force", wtPath)
				if out, cmdErr := cmd.CombinedOutput(); cmdErr != nil {
					logger.WarnContext(ctx, "QM-002b reap: git worktree remove failed",
						"bead_id", string(beadID),
						"run_id", *item.RunID,
						"error", cmdErr,
						"output", strings.TrimSpace(string(out)),
					)
				}
			}
		}
	}
}

func reconcileQueueTerminalState(
	ctx context.Context,
	projectDir string,
	q *queue.Queue,
	logger *slog.Logger,
) (done bool, err error) {
	if q.Status != queue.QueueStatusActive {
		return false, nil
	}

	for gi := range q.Groups {
		g := &q.Groups[gi]
		if g.Status != queue.GroupStatusActive {
			continue
		}
		allTerminal := true
		for _, item := range g.Items {
			switch item.Status {
			case queue.ItemStatusCompleted:
			case queue.ItemStatusFailed:
			default:
				allTerminal = false
			}
		}
		if !allTerminal {
			continue
		}
		before := g.Status
		if transitionErr := queue.CompleteActiveGroup(g, time.Now()); transitionErr != nil {
			return false, fmt.Errorf("reconcile F5: complete group: %w", transitionErr)
		}
		logger.InfoContext(ctx, "reconcile F5: advanced all-terminal active group (stale active-marker)",
			"queue_id", q.QueueID,
			"group_index", gi,
			"from", string(before),
			"to", string(g.Status),
		)
	}

	allSuccess := len(q.Groups) > 0
	allTerminal := len(q.Groups) > 0
	for _, g := range q.Groups {
		switch g.Status {
		case queue.GroupStatusCompleteSuccess:
		case queue.GroupStatusCompleteWithFailures:
			allSuccess = false
		default:
			allSuccess = false
			allTerminal = false
		}
	}

	if allSuccess {
		logger.InfoContext(ctx, "reconcile F5: all groups complete-success; unlinking queue (stale active-marker cleared)",
			"queue_id", q.QueueID,
		)
		if unlinkErr := queue.CompleteAndUnlink(ctx, projectDir, q); unlinkErr != nil {
			logger.WarnContext(ctx, "reconcile F5: CompleteAndUnlink failed; file may remain but queue will not be loaded",
				"queue_id", q.QueueID,
				"error", unlinkErr,
			)
		}
		return true, nil
	}

	if allTerminal {
		logger.InfoContext(ctx, "reconcile F5: all groups terminal with failures; demoting queue to paused-by-failure",
			"queue_id", q.QueueID,
		)
		if transitionErr := queue.PauseQueueForFailure(q); transitionErr != nil {
			return false, fmt.Errorf("reconcile F5: pause queue: %w", transitionErr)
		}
		if persistErr := queue.Persist(ctx, projectDir, q); persistErr != nil {
			logger.WarnContext(ctx, "reconcile F5: Persist paused-by-failure failed; queue stays active in file",
				"queue_id", q.QueueID,
				"error", persistErr,
			)
		}
		return false, nil
	}

	return false, nil
}
