package lifecycle

// startup_pl005_qm002.go — PL-005 step 8a queue.json load hook (QM-002 + QM-002a).
//
// Implements the daemon startup obligation to:
//   1. Read .harmonik/queue.json per QM-002 with three declared outcomes.
//   2. On a successful load, cross-check every dispatched item against the live
//      Beads ledger per QM-002a and revert claim-write-lost items to pending.
//
// This check MUST complete before the daemon reaches ready state and before any
// dispatch-loop tick.
//
// Spec refs:
//   - specs/queue-model.md §3.2 QM-002 — startup read
//   - specs/queue-model.md §3.2a QM-002a — Beads cross-check
//   - specs/process-lifecycle.md §4.2 PL-005 step 8a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
// Beads ledger for item status during QM-002a startup cross-check.
//
// The production implementation is *brcli.Adapter. Tests inject a deterministic
// fake.
//
// Spec ref: specs/queue-model.md §3.2a QM-002a — "call `br show <bead_id>`".
// Spec ref: specs/beads-integration.md §4.5 BI-015.
type BeadLedger interface {
	// ShowBead invokes `br show <id> --format json` and returns the parsed
	// BeadRecord for the given bead ID.
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
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

// LoadQueueAtStartup implements PL-005 step 8a for the queue subsystem.
//
// Three outcomes per QM-002:
//
//   - File absent: returns (nil, nil). The daemon starts with no active queue.
//   - Corrupt / unparseable: returns (nil, nil) after logging a structured
//     warning. The daemon continues without a queue; the file is NOT deleted.
//   - schema_version unsupported (forward-incompatible): returns
//     (nil, ErrQueueSchemaUnsupported). The caller MUST refuse startup with
//     exit code 2.
//   - Clean parse: returns (q, nil) after the QM-002a Beads cross-check and
//     any necessary in-memory + on-disk corrections.
//
// On a successful load, QM-002a Beads cross-check runs inline:
//   - For every item with status=dispatched, ShowBead is called.
//   - If Beads shows the bead as open, the item is reverted to pending, the
//     queue is re-persisted via QM-001, and a queue_item_reconciled event is
//     emitted with reason=claim_write_lost.
//   - ShowBead errors are logged as warnings; the item is not reverted (only a
//     confirmed Beads-open status triggers revert).
//
// The emitter parameter MAY be nil; when nil, events are not emitted (useful
// for testing scenarios that don't care about event emission, but production
// callers MUST supply a non-nil emitter).
//
// Spec ref: specs/queue-model.md §3.2 QM-002.
// Spec ref: specs/queue-model.md §3.2a QM-002a.
// Spec ref: specs/process-lifecycle.md §4.2 PL-005 step 8a.
func LoadQueueAtStartup(
	ctx context.Context,
	projectDir string,
	ledger BeadLedger,
	emitter QueueEventEmitter,
	logger *slog.Logger,
) (*queue.Queue, error) {
	if logger == nil {
		logger = slog.Default()
	}

	q, err := queue.Load(ctx, projectDir)
	if err != nil {
		// Distinguish schema_version mismatch (forward-incompatible → exit 2)
		// from general corruption (warn + proceed with nil queue).
		if errors.Is(err, queue.ErrSchemaVersion) {
			// Forward-incompatible schema_version: refuse startup with exit code 2
			// per QM-002.
			logger.ErrorContext(ctx, "queue.json schema_version is not in supported read-set; startup refused per QM-002",
				"error", err,
			)
			return nil, ErrQueueSchemaUnsupported
		}

		// ErrCorrupt or any other parse failure: log warning and treat as absent.
		// The file is NOT deleted; operator inspection comes first per QM-002.
		if errors.Is(err, queue.ErrCorrupt) {
			logger.WarnContext(ctx, "queue.json is present but unparseable; treating as absent per QM-002",
				"error", err,
			)
			return nil, nil
		}

		// Unexpected I/O error (permission denied, etc.) — also log and treat as absent
		// so startup is not blocked by transient FS issues, but surface as a warning.
		logger.WarnContext(ctx, "queue.json read failed unexpectedly; treating as absent",
			"error", err,
		)
		return nil, nil
	}

	// File absent: q == nil, err == nil.
	if q == nil {
		return nil, nil
	}

	// QM-002a: Beads cross-check for dispatched items.
	if err := reconcileDispatchedItems(ctx, projectDir, q, ledger, emitter, logger); err != nil {
		return nil, fmt.Errorf("lifecycle: LoadQueueAtStartup: QM-002a reconcile: %w", err)
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
