package lifecycle

// startup_pl005_qm002.go — PL-005 step 8a queue.json load hook (QM-002 + QM-002a + QM-002b).
//
// Implements the daemon startup obligation to:
//   1. Read .harmonik/queue.json per QM-002 with three declared outcomes.
//   2. On a successful load, cross-check every dispatched item against the live
//      Beads ledger per QM-002a and revert claim-write-lost items to pending.
//   3. Run the full three-way reconciliation pass per QM-002b, including
//      Class B orphan reaping (hk-5pg37): beads in_progress with no queue
//      item are reset to open and their worktrees are removed.
//
// This check MUST complete before the daemon reaches ready state and before any
// dispatch-loop tick.
//
// Spec refs:
//   - specs/queue-model.md §3.2 QM-002 — startup read
//   - specs/queue-model.md §3.2a QM-002a — Beads cross-check
//   - specs/queue-model.md §3.2b QM-002b — three-way reconciliation (incl. Class B reap)
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a

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

	// Resolve the optional Class B reap config (first element if present; nil otherwise).
	var classBReap *QM002bReapConfig
	if len(reapCfg) > 0 {
		classBReap = reapCfg[0]
	}

	// NQ-A2: migrate legacy .harmonik/queue.json → .harmonik/queues/main.json
	// before enumeration so the per-queue scan picks it up.
	if err := queue.MigrateFromLegacy(ctx, projectDir); err != nil {
		// Non-fatal: log and proceed. A failed migration leaves the legacy file
		// in place; the next startup attempt retries the migration.
		logger.WarnContext(ctx, "queue: MigrateFromLegacy failed; legacy file may persist",
			"error", err,
		)
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
			// schema_version mismatch is fatal per QM-002.
			return nil, loadErr
		}
		if q != nil {
			loaded = append(loaded, q)
		}
	}
	return loaded, nil
}

// loadOneQueueAtStartup loads a single named queue file and runs QM-002a +
// QM-002b reconciliation. Returns (nil, nil) when the file is absent or
// corrupt. Returns (nil, ErrQueueSchemaUnsupported) on forward-incompatible
// schema_version.
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

	// QM-002a: Beads cross-check for dispatched items.
	if err := reconcileDispatchedItems(ctx, projectDir, q, ledger, emitter, logger); err != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: QM-002a reconcile: %w", name, err)
	}

	// QM-002b: Full three-way reconciliation (including Class B orphan reap).
	if err := reconcileThreeWay(ctx, projectDir, q, ledger, emitter, logger, classBReap); err != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: QM-002b three-way reconcile: %w", name, err)
	}

	// F5 (hk-qkahq): clear the stale active-marker left by a killed/wedged run.
	// After item-level reconciliation, check whether the queue's effective state
	// is fully terminal. If all groups are complete-success the file is unlinked;
	// if all groups are terminal with failures the status is demoted to
	// paused-by-failure (which QM-027 allows to be overwritten by a fresh submit).
	done, termErr := reconcileQueueTerminalState(ctx, projectDir, q, logger)
	if termErr != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup[%s]: terminal-state reconcile: %w", name, termErr)
	}
	if done {
		return nil, nil
	}

	return q, nil
}

// reconciledEvent carries the marshalled payload for a deferred queue_item_reconciled
// emission. Events are collected during the scan, then emitted after persist to
// honour the QM-063 persist-before-emit ordering rule.
type reconciledEvent struct {
	payload []byte
}

// reconcileDispatchedItems implements QM-002a: for every item with
// status=dispatched, queries the Beads ledger via ShowBead. If Beads reports
// the bead as open (claim-write-lost), the item is reverted to pending. After
// all reverts are collected, the queue is re-persisted via QM-001 (step 2),
// then queue_item_reconciled events are emitted (step 3).
//
// Ordering per QM-063 (persist BEFORE emit):
//  1. Scan all dispatched items; collect reverts + pending event payloads.
//  2. If any reverts: call queue.Persist (QM-001 atomic write).
//  3. Emit the collected queue_item_reconciled events.
//
// This function mutates q in-place on revert. The caller receives the corrected
// queue.
//
// Spec ref: specs/queue-model.md §3.2a QM-002a.
// Spec ref: specs/queue-model.md §9.3 QM-063 — persist BEFORE emit.
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
				// ShowBead failure: log warning and leave item as-is.
				// Only a confirmed Beads-open triggers the revert.
				logger.WarnContext(ctx, "QM-002a: ShowBead failed; skipping reconcile for item",
					"bead_id", string(item.BeadID),
					"error", showErr,
				)
				continue
			}

			if record.Status != core.CoarseStatusOpen {
				// Beads confirms the bead is NOT open — dispatch was recorded
				// correctly. No revert needed.
				continue
			}

			// Beads shows the bead as open but queue.json records it as dispatched.
			// This is the claim-write-lost case per QM-002a: revert to pending.
			reconciledAt := time.Now().UTC()

			logger.InfoContext(ctx, "QM-002a: reverting dispatched item to pending (claim_write_lost)",
				"bead_id", string(item.BeadID),
				"group_index", gi,
			)

			item.Status = queue.ItemStatusPending
			item.RunID = nil // clear run_id on revert

			// Build the event payload now; emit AFTER persist per QM-063.
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
					// Non-fatal: log and skip the event emit for this item.
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

	// QM-002a step 2: persist the corrected queue via QM-001 atomic write BEFORE
	// emitting events (QM-063 persist-before-emit ordering rule).
	if err := queue.Persist(ctx, projectDir, q); err != nil {
		return fmt.Errorf("QM-002a: persist corrected queue: %w", err)
	}

	// QM-002a step 3: emit queue_item_reconciled events after persist.
	for _, ev := range pending {
		if err := emitter.Emit(ctx, core.EventTypeQueueItemReconciled, ev.payload); err != nil {
			logger.WarnContext(ctx, "QM-002a: failed to emit queue_item_reconciled event",
				"error", err,
			)
		}
	}

	return nil
}

// reconcileThreeWay implements QM-002b: the full three-way reconciliation pass
// that runs after QM-002a to catch mismatch classes not covered by the
// dispatched-items-only scan.
//
// Three mismatch classes are handled:
//
//  1. Class A — "bead_closed_queue_pending":
//     A queue item has status=pending (or deferred-for-ledger-dep) but the Beads
//     ledger reports the bead as closed/tombstone. The item is advanced to
//     completed so the queue does not wait for a bead that already finished.
//     Correction: mutate item status in memory, persist via QM-001, emit
//     reconciliation_mismatch_observed.
//
//  2. Class B — "bead_inprogress_queue_absent":
//     The Beads ledger reports a bead as in_progress but no queue item
//     references that bead at all. This orphan is left for the orphan-sweep
//     (hk-2ty0g's sweep handles the queue-owned case; this covers the
//     no-record-at-all case). No queue mutation; emit
//     reconciliation_mismatch_observed + log for operator visibility.
//
//  3. Class C — "bead_closed_queue_inprogress":
//     A queue item has status=completed or failed but the Beads ledger still
//     shows in_progress. No queue mutation (the queue-side terminal is already
//     set); emit reconciliation_mismatch_observed + log for operator visibility.
//
// Ordering per QM-063 (persist BEFORE emit):
//  1. Scan all queue items; collect Class A mutations + pending event payloads.
//  2. If any Class A mutations: persist via QM-001.
//  3. Enumerate in-progress ledger beads; collect Class B payloads.
//  4. Emit all collected events.
//
// Spec ref: specs/queue-model.md §3.2b QM-002b (added by hk-nvfvj).
// Spec ref: specs/queue-model.md §9.3 QM-063 — persist BEFORE emit.
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

	// beadsInQueue maps bead IDs referenced by any queue item to their item status.
	// Used in the Class B pass to identify in-progress ledger beads not in the queue.
	beadsInQueue := make(map[core.BeadID]queue.ItemStatus)

	// pendingEvents collects all events to emit after any persist step.
	type pendingEvent struct {
		eventType core.EventType
		payload   []byte
	}
	var pendingEvents []pendingEvent
	var classACount int

	// --- Class A and Class C scan: iterate queue items ---
	for gi := range q.Groups {
		for ii := range q.Groups[gi].Items {
			item := &q.Groups[gi].Items[ii]

			beadsInQueue[item.BeadID] = item.Status

			// Class A: queue item is pending or deferred but bead is already closed.
			// Class A': queue item is dispatched but bead is already closed
			//   (daemon restart abandoned the goroutine; bead was closed via another
			//   path — e.g. a sibling queue or a direct br close).  Without this
			//   branch the item is stuck at dispatched forever and blocks
			//   QM-027's single-active check.  Mirrors Class A persist-before-emit.
			isPendingLike := item.Status == queue.ItemStatusPending ||
				item.Status == queue.ItemStatusDeferredForLedgerDep
			if !isPendingLike {
				// Class C check below.
				isQueueTerminal := item.Status == queue.ItemStatusCompleted ||
					item.Status == queue.ItemStatusFailed
				if !isQueueTerminal {
					// Class A': dispatched + bead closed → advance to completed.
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
					isDispLedgerClosed := dispRecord.Status == core.CoarseStatusClosed ||
						dispRecord.Status == core.CoarseStatusTombstone
					if !isDispLedgerClosed {
						continue
					}
					logger.InfoContext(ctx, "QM-002b Class A': advancing dispatched item to completed (bead_closed_queue_dispatched)",
						"bead_id", string(item.BeadID),
						"group_index", gi,
						"ledger_status", string(dispRecord.Status),
					)
					item.Status = queue.ItemStatusCompleted
					classACount++
					if emitter != nil {
						p := core.ReconciliationMismatchObservedPayload{
							QueueID:       q.QueueID,
							GroupIndex:    gi,
							BeadID:        string(item.BeadID),
							MismatchClass: "bead_closed_queue_dispatched",
							LedgerStatus:  string(dispRecord.Status),
							QueueStatus:   "dispatched",
							ObservedAt:    observedAt,
						}
						payloadBytes, marshalErr := json.Marshal(p)
						if marshalErr != nil {
							logger.WarnContext(ctx, "QM-002b: failed to marshal mismatch payload",
								"bead_id", string(item.BeadID),
								"error", marshalErr,
							)
						} else {
							pendingEvents = append(pendingEvents, pendingEvent{
								eventType: core.EventTypeReconciliationMismatchObserved,
								payload:   payloadBytes,
							})
						}
					}
					continue
				}
				// Class C: queue says terminal; check ledger.
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
				// Mismatch: queue terminal but ledger in_progress.
				logger.WarnContext(ctx, "QM-002b Class C: queue item terminal but ledger in_progress (bead_closed_queue_inprogress)",
					"bead_id", string(item.BeadID),
					"queue_status", string(item.Status),
					"group_index", gi,
				)
				if emitter != nil {
					p := core.ReconciliationMismatchObservedPayload{
						QueueID:       q.QueueID,
						GroupIndex:    gi,
						BeadID:        string(item.BeadID),
						MismatchClass: "bead_closed_queue_inprogress",
						LedgerStatus:  string(record.Status),
						QueueStatus:   string(item.Status),
						ObservedAt:    observedAt,
					}
					payloadBytes, marshalErr := json.Marshal(p)
					if marshalErr != nil {
						logger.WarnContext(ctx, "QM-002b: failed to marshal mismatch payload",
							"bead_id", string(item.BeadID),
							"error", marshalErr,
						)
					} else {
						pendingEvents = append(pendingEvents, pendingEvent{
							eventType: core.EventTypeReconciliationMismatchObserved,
							payload:   payloadBytes,
						})
					}
				}
				continue
			}

			// isPendingLike — check ledger for Class A.
			record, showErr := ledger.ShowBead(ctx, item.BeadID)
			if showErr != nil {
				logger.WarnContext(ctx, "QM-002b Class A: ShowBead failed; skipping",
					"bead_id", string(item.BeadID),
					"error", showErr,
				)
				continue
			}
			isLedgerClosed := record.Status == core.CoarseStatusClosed ||
				record.Status == core.CoarseStatusTombstone
			if !isLedgerClosed {
				continue
			}

			// Mismatch: queue pending but ledger closed.
			logger.InfoContext(ctx, "QM-002b Class A: advancing pending item to completed (bead_closed_queue_pending)",
				"bead_id", string(item.BeadID),
				"group_index", gi,
				"ledger_status", string(record.Status),
			)

			item.Status = queue.ItemStatusCompleted
			classACount++

			if emitter != nil {
				p := core.ReconciliationMismatchObservedPayload{
					QueueID:       q.QueueID,
					GroupIndex:    gi,
					BeadID:        string(item.BeadID),
					MismatchClass: "bead_closed_queue_pending",
					LedgerStatus:  string(record.Status),
					QueueStatus:   "pending",
					ObservedAt:    observedAt,
				}
				payloadBytes, marshalErr := json.Marshal(p)
				if marshalErr != nil {
					logger.WarnContext(ctx, "QM-002b: failed to marshal mismatch payload",
						"bead_id", string(item.BeadID),
						"error", marshalErr,
					)
				} else {
					pendingEvents = append(pendingEvents, pendingEvent{
						eventType: core.EventTypeReconciliationMismatchObserved,
						payload:   payloadBytes,
					})
				}
			}
		}
	}

	// QM-063 step 2: persist queue if Class A mutations were applied.
	if classACount > 0 {
		if err := queue.Persist(ctx, projectDir, q); err != nil {
			return fmt.Errorf("QM-002b: persist Class A corrections: %w", err)
		}
	}

	// --- Class B scan: enumerate in-progress ledger beads ---
	// Run if the emitter is non-nil (observability event) OR if classBReap is
	// configured (reap action). Class B produces no queue mutation.
	reapEnabled := classBReap != nil && classBReap.Resetter != nil
	if emitter != nil || reapEnabled {
		inFlight, listErr := ledger.ListInFlightBeads(ctx)
		if listErr != nil {
			// Non-fatal: log and skip Class B entirely.
			// ListInFlightBeads failure must not block startup.
			logger.WarnContext(ctx, "QM-002b Class B: ListInFlightBeads failed; skipping orphan check",
				"error", listErr,
			)
		} else {
			var classBOrphans []core.BeadID
			for _, rec := range inFlight {
				if _, inQueue := beadsInQueue[rec.BeadID]; inQueue {
					continue // bead has a queue item — not a Class B orphan
				}
				// F21: demoted Warn→Info — fires x83/session on normal restarts where
				// in_progress beads predate the current queue (QM-002b reap handles it).
				logger.InfoContext(ctx, "QM-002b Class B: ledger in_progress bead has no queue item (bead_inprogress_queue_absent)",
					"bead_id", string(rec.BeadID),
				)
				if emitter != nil {
					p := core.ReconciliationMismatchObservedPayload{
						QueueID:       "",
						GroupIndex:    -1,
						BeadID:        string(rec.BeadID),
						MismatchClass: "bead_inprogress_queue_absent",
						LedgerStatus:  string(rec.Status),
						QueueStatus:   "",
						ObservedAt:    observedAt,
					}
					payloadBytes, marshalErr := json.Marshal(p)
					if marshalErr != nil {
						logger.WarnContext(ctx, "QM-002b: failed to marshal Class B mismatch payload",
							"bead_id", string(rec.BeadID),
							"error", marshalErr,
						)
					} else {
						pendingEvents = append(pendingEvents, pendingEvent{
							eventType: core.EventTypeReconciliationMismatchObserved,
							payload:   payloadBytes,
						})
					}
				}
				if reapEnabled {
					classBOrphans = append(classBOrphans, rec.BeadID)
				}
			}
			// Reap Class B orphans: reset each bead to open and remove its
			// worktree. Non-fatal: a reap failure for one bead does not block
			// the others or the startup sequence.
			//
			// Spec ref: hk-5pg37 — reconciler reaps queue-cancel orphans.
			for _, beadID := range classBOrphans {
				reapClassBOrphan(ctx, projectDir, beadID, classBReap, logger)
			}
		}
	}

	// QM-063 step 4: emit all collected events.
	for _, ev := range pendingEvents {
		if err := emitter.Emit(ctx, ev.eventType, ev.payload); err != nil {
			logger.WarnContext(ctx, "QM-002b: failed to emit reconciliation_mismatch_observed event",
				"error", err,
			)
		}
	}

	return nil
}

// reapClassBOrphan resets a Class B orphan bead (in_progress → open) and
// attempts to remove its associated worktree. Both operations are best-effort:
// a failure in either step is logged but does not block startup or prevent the
// other bead reaps from proceeding.
//
// Spec ref: hk-5pg37 — reconciler must reap queue-cancel+restart orphans.
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
		// Do not attempt worktree removal when the bead reset failed:
		// leaving the worktree intact gives the operator a chance to inspect
		// the state. The bead will be retried on the next daemon restart.
		return
	}
	logger.InfoContext(ctx, "QM-002b Class B: bead reset to open",
		"bead_id", string(beadID),
	)
	// Best-effort: find and remove the orphaned worktree from cancelled/failed
	// queue archives. A failure here is non-fatal — the bead is already reset
	// and can be redispatched to a new worktree.
	reapOrphanWorktreesFromArchives(ctx, projectDir, beadID, logger)
}

// reapOrphanWorktreesFromArchives scans .harmonik/queues/ for archived
// (cancelled or failed) queue files, finds items whose bead_id matches
// beadID, and removes the associated worktrees via
// git worktree remove --force --force. Non-fatal: each step that fails is
// logged and the scan continues.
//
// Spec ref: hk-5pg37.
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
		// Only scan cancelled and failed archive files.
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

// reconcileQueueTerminalState detects and clears the stale active-marker left
// when a daemon is killed or wedged after all queue items reached terminal
// states but before evaluateGroupAdvanceWithOutcome could advance the group
// status and call CompleteAndUnlink.
//
// The reconciliation passes (QM-002a, QM-002b) fix individual item statuses
// but do not re-evaluate group or queue status. This function fills that gap:
//
//  1. Pass 1 — advance any active group where all items are terminal.
//     Items that are completed or failed are considered terminal; any other
//     status (pending, dispatched, deferred-for-ledger-dep) blocks the
//     transition (mirrors the QM-030 all-terminal gate in state.go).
//
//  2. Pass 2 — evaluate the overall queue terminal state:
//     - All groups complete-success → CompleteAndUnlink; return done=true.
//       The caller should NOT add this queue to QueueStore.
//     - All groups terminal (some complete-with-failures) → transition
//       q.Status to paused-by-failure and persist. QM-027 allows a fresh
//       submit to overwrite a paused-by-failure queue, so the queue name is
//       unblocked without discarding the failure record. Return done=false.
//     - Any group still pending or active → return done=false; the daemon
//       resumes dispatching normally.
//
// Returns (true, nil)  when the queue was fully cleaned up.
// Returns (false, nil) when the queue has pending/active work or was
//
//	transitioned to paused-by-failure.
//
// Returns (false, err) only on an unexpected persistence failure.
//
// Bead ref: hk-qkahq (logmine F5 — stale active-marker on kill/wedge).
func reconcileQueueTerminalState(
	ctx context.Context,
	projectDir string,
	q *queue.Queue,
	logger *slog.Logger,
) (done bool, err error) {
	// Only active queues can carry a stale active-marker.
	// paused-by-failure already unblocks QM-027; other statuses are not targets.
	if q.Status != queue.QueueStatusActive {
		return false, nil
	}

	// Pass 1: advance each active group whose items are all terminal.
	for gi := range q.Groups {
		g := &q.Groups[gi]
		if g.Status != queue.GroupStatusActive {
			continue
		}
		allTerminal := true
		hasFailed := false
		for _, item := range g.Items {
			switch item.Status {
			case queue.ItemStatusCompleted:
				// terminal, success
			case queue.ItemStatusFailed:
				hasFailed = true
			default:
				// pending, dispatched, deferred-for-ledger-dep — not terminal
				allTerminal = false
			}
		}
		if !allTerminal {
			continue
		}
		before := g.Status
		if hasFailed {
			g.Status = queue.GroupStatusCompleteWithFailures
		} else {
			g.Status = queue.GroupStatusCompleteSuccess
		}
		logger.InfoContext(ctx, "reconcile F5: advanced all-terminal active group (stale active-marker)",
			"queue_id", q.QueueID,
			"group_index", gi,
			"from", string(before),
			"to", string(g.Status),
		)
	}

	// Pass 2: derive overall terminal state from group statuses.
	// Require at least one group (an empty queue cannot be "all complete").
	allSuccess := len(q.Groups) > 0
	allTerminal := len(q.Groups) > 0
	for _, g := range q.Groups {
		switch g.Status {
		case queue.GroupStatusCompleteSuccess:
			// contributes to both allSuccess and allTerminal
		case queue.GroupStatusCompleteWithFailures:
			allSuccess = false
		default:
			// pending or active — not terminal
			allSuccess = false
			allTerminal = false
		}
	}

	if allSuccess {
		// Mirror the happy-path completion in evaluateGroupAdvanceWithOutcome:
		// CompleteAndUnlink sets status=completed and removes the queue file.
		logger.InfoContext(ctx, "reconcile F5: all groups complete-success; unlinking queue (stale active-marker cleared)",
			"queue_id", q.QueueID,
		)
		if unlinkErr := queue.CompleteAndUnlink(ctx, projectDir, q); unlinkErr != nil {
			// Non-fatal: log and still return done=true. The queue must not be
			// loaded into QueueStore (it would permanently block new submits).
			// The stale file will be retried on the next daemon restart.
			logger.WarnContext(ctx, "reconcile F5: CompleteAndUnlink failed; file may remain but queue will not be loaded",
				"queue_id", q.QueueID,
				"error", unlinkErr,
			)
		}
		return true, nil
	}

	if allTerminal {
		// All groups are terminal but some have failures. Demote to
		// paused-by-failure so QM-027 lets operators submit new work to the
		// same queue name without a manual file removal step.
		logger.InfoContext(ctx, "reconcile F5: all groups terminal with failures; demoting queue to paused-by-failure",
			"queue_id", q.QueueID,
		)
		q.Status = queue.QueueStatusPausedByFailure
		if persistErr := queue.Persist(ctx, projectDir, q); persistErr != nil {
			logger.WarnContext(ctx, "reconcile F5: Persist paused-by-failure failed; queue stays active in file",
				"queue_id", q.QueueID,
				"error", persistErr,
			)
		}
		return false, nil
	}

	// Queue has pending or active groups with non-terminal items — normal dispatch.
	return false, nil
}
