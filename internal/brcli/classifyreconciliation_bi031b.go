package brcli

// classifyreconciliation_bi031b.go — BI-031b schema-mismatch divergence emission.
//
// Spec refs:
//   - specs/beads-integration.md §4.10 BI-031b (normative)
//   - specs/event-model.md §8.6.10 (divergence_inconclusive payload)
//   - specs/event-model.md §6.3 EV-023a (single-authority inconclusive semantics)
//
// BI-031b: a BrSchemaMismatch recovery path MUST emit divergence_inconclusive
// per event-model.md §8.6.10 with reason=authority_unavailable and refuse the
// reissue.  Recovery cannot proceed under schema drift; the pinned-version
// handshake of BI-024a is the mechanism that prevents schema drift from arising
// in non-pathological configurations.
//
// This file provides:
//   - SchemaMismatchEmitter — the narrow interface for divergence_inconclusive
//     emission.  eventbus.EventBus satisfies it via its Emit method.
//   - BrErrReconciliationCategoryWithEmit — augmented classifier that emits
//     divergence_inconclusive when the error resolves to BrSchemaMismatch.
//
// The existing BrErrReconciliationCategory pure function is unchanged and remains
// the canonical classification surface for callers that do not carry an event bus.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// SchemaMismatchEmitter is the narrow event-emission surface required by
// BrErrReconciliationCategoryWithEmit for BI-031b divergence_inconclusive
// emission.
//
// eventbus.EventBus satisfies this interface via its Emit method.  Callers MAY
// pass nil; in that case BrErrReconciliationCategoryWithEmit falls back to a
// structured-log record per operator-nfr.md §4.9 ON-035.
type SchemaMismatchEmitter interface {
	Emit(ctx context.Context, eventType core.EventType, payload []byte) error
}

// BrErrReconciliationCategoryWithEmit is an augmented variant of
// BrErrReconciliationCategory that, when err resolves to BrSchemaMismatch,
// emits a divergence_inconclusive event per BI-031b before returning RecCat0.
//
// For all other BrError values the function delegates directly to
// BrErrReconciliationCategory and performs no emission.
//
// evidenceRef is placed in the DivergenceInconclusivePayload.EvidenceRef field
// and MUST be non-empty; a caller-supplied string such as "br-schema-mismatch"
// or the br command name is appropriate.
//
// If bus is nil or emission fails, a structured-log record at level=error is
// emitted instead per operator-nfr.md §4.9 ON-035.  The classification result
// is always returned regardless of emission outcome.
//
// Spec ref: specs/beads-integration.md §4.10 BI-031b.
func BrErrReconciliationCategoryWithEmit(
	ctx context.Context,
	err error,
	evidenceRef string,
	bus SchemaMismatchEmitter,
) ReconciliationCategory {
	cat := BrErrReconciliationCategory(err)

	// Only emit on BrSchemaMismatch — errors.Is so wrapped errors resolve correctly.
	if errors.Is(err, BrSchemaMismatch) {
		emitSchemaMismatchInconclusive(ctx, evidenceRef, bus)
	}

	return cat
}

// emitSchemaMismatchInconclusive marshals and emits a divergence_inconclusive
// event with reason=authority_unavailable per BI-031b / event-model.md §8.6.10.
// Falls back to a structured-log record when bus is nil or emission fails.
func emitSchemaMismatchInconclusive(
	ctx context.Context,
	evidenceRef string,
	bus SchemaMismatchEmitter,
) {
	if evidenceRef == "" {
		evidenceRef = "br-schema-mismatch"
	}

	payload := core.DivergenceInconclusivePayload{
		EvidenceRef:     evidenceRef,
		PostCrashWindow: false,
		Reason:          core.DivergenceInconclusiveReasonAuthorityUnavailable,
	}

	raw, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		// Marshal of a known-shape struct should never fail; guard anyway.
		slog.ErrorContext(ctx, "brcli: divergence_inconclusive: payload marshal failed; falling back to structured-log",
			"subsystem", "beads-adapter",
			"evidence_ref", evidenceRef,
			"reason", string(core.DivergenceInconclusiveReasonAuthorityUnavailable),
			"error", marshalErr,
			"detected_at", time.Now().UTC().Format(time.RFC3339),
		)
		return
	}

	if bus != nil {
		if emitErr := bus.Emit(ctx, core.EventTypeDivergenceInconclusive, raw); emitErr != nil {
			// Bus emission failed — structured-log fallback per ON-035.
			slog.ErrorContext(ctx, "brcli: divergence_inconclusive: bus emission failed; structured-log fallback",
				"subsystem", "beads-adapter",
				"evidence_ref", evidenceRef,
				"reason", string(core.DivergenceInconclusiveReasonAuthorityUnavailable),
				"error", emitErr,
				"detected_at", time.Now().UTC().Format(time.RFC3339),
			)
		}
		return
	}

	// Bus is nil: structured-log fallback per ON-035.
	slog.ErrorContext(ctx, "brcli: divergence_inconclusive: bus unavailable; structured-log fallback",
		"subsystem", "beads-adapter",
		"evidence_ref", evidenceRef,
		"reason", string(core.DivergenceInconclusiveReasonAuthorityUnavailable),
		"detected_at", time.Now().UTC().Format(time.RFC3339),
	)
}
