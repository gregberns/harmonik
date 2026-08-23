package brcli

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

	if errors.Is(err, BrSchemaMismatch) {
		emitSchemaMismatchInconclusive(ctx, evidenceRef, bus)
	}

	return cat
}

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

	slog.ErrorContext(ctx, "brcli: divergence_inconclusive: bus unavailable; structured-log fallback",
		"subsystem", "beads-adapter",
		"evidence_ref", evidenceRef,
		"reason", string(core.DivergenceInconclusiveReasonAuthorityUnavailable),
		"detected_at", time.Now().UTC().Format(time.RFC3339),
	)
}
