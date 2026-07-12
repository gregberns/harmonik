package daemon

// pi_profile_resolve.go — claim-time per-bead Pi provider-profile resolver
// (pi-provider-switch, hk-m6uu2 C3).
//
// A bead may carry a `profile:<name>` label selecting one of the named
// profiles under harnesses.pi.profiles (C2, projectconfig.go PiProfileConfig).
// resolvePiProfile mirrors resolveModelField's collect→count pattern
// (modelpreference.go:203-256): exactly one label resolves against the
// C2-validated map (existence-only check; the value is never re-validated
// here — opacity, matches modelpreference.go:220-224); more than one is a
// conflict (emitBeadLabelConflict, treated as absent); zero is absent.
//
// hk-pkugu discipline (load-bearing): the resolver MUST be called with the
// resolvedAgentType produced by resolveHarnessAgentTypeQuiet (workloop.go,
// after the model-preference walk) so a claude/codex-resolved bead never
// receives a pi tuple — there is no "family" of pi types, only the single
// core.AgentTypePi constant.
//
// Spec: ~/.kerf/projects/gregberns-harmonik/pi-provider-switch/05-specs/C3-spec.md.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// labelPrefixProfile is the label prefix for per-bead Pi provider-profile
// selection (pi-provider-switch). E.g. `profile:ornith-dgx`.
const labelPrefixProfile = "profile:"

// PiProfileUnknownError is returned by resolvePiProfile when a bead names a
// profile that is absent from the C2-validated harnesses.pi.profiles map.
// Fail-loud: the caller MUST reopen the bead rather than launch on an
// empty/wrong tuple (C3-spec.md §"Claim-time wiring").
type PiProfileUnknownError struct {
	BeadID  string
	Profile string
}

func (e *PiProfileUnknownError) Error() string {
	return fmt.Sprintf("daemon: bead %s: unknown Pi profile %q (not present in harnesses.pi.profiles)", e.BeadID, e.Profile)
}

// resolvePiProfile resolves the per-bead Pi provider profile from a
// `profile:<name>` label. Returns the zero PiProfileConfig (all-empty) when:
// agentType is not core.AgentTypePi; no profile: label is present; or
// (conflict) more than one is present. Existence is checked against the
// C2-validated profiles map; an unknown reference is fail-loud via
// *PiProfileUnknownError. Profile NAME is never value-validated (opacity).
//
// hk-pkugu discipline: callers MUST pass resolvedAgentType from
// resolveHarnessAgentTypeQuiet so a claude/codex-resolved bead yields the
// zero tuple.
func resolvePiProfile(
	ctx context.Context,
	beadLabels []string,
	agentType core.AgentType,
	piCfg PiHarnessConfig,
	bus handlercontract.EventEmitter,
	beadID string,
) (PiProfileConfig, error) {
	// Harness gate (hk-pkugu): only the pi harness resolves a profile tuple.
	// No lookup, no error, no event — quiet, matching the quiet handling of
	// tier-1 mismatches elsewhere.
	if agentType != core.AgentTypePi {
		return PiProfileConfig{}, nil
	}

	// Collect all labels with the profile: prefix.
	var profileLabels []string
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixProfile) {
			profileLabels = append(profileLabels, lbl)
		}
	}

	switch len(profileLabels) {
	case 0:
		// Absent: no event, zero tuple (⇒ C4 h.* fallback).
		return PiProfileConfig{}, nil
	case 1:
		name := strings.TrimPrefix(profileLabels[0], labelPrefixProfile)
		profile, ok := piCfg.Profiles[name]
		if !ok {
			// Existence check (fail-loud): the C2→C3 contract. Name value
			// itself is never re-validated (opacity) — only existence.
			return PiProfileConfig{}, &PiProfileUnknownError{BeadID: beadID, Profile: name}
		}
		return profile, nil
	default:
		// Conflict: multiple profile: labels → treat as absent, emit event.
		emitBeadLabelConflict(ctx, bus,
			core.BeadRecord{BeadID: core.BeadID(beadID), Labels: beadLabels},
			profileLabels,
			"tier-1 profile absent: multiple profile:<name> labels; zero tuple (C4 h.* fallback)")
		return PiProfileConfig{}, nil
	}
}

// emitProviderSelected emits a provider_selected event (hk-8ziid.2,
// docs/design/pi-multi-provider-slot-accounting.md) recording the resolved Pi
// provider identity keyed on run_id. Called by beadRunOne (workloop.go)
// immediately after resolvePiProfile returns, for Pi runs only, alongside the
// RunHandle.SetResolvedProvider call the same value is stored under.
// Best-effort: emit errors are silently discarded (the resolution result is
// already determined before this call).
func emitProviderSelected(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	provider string,
) {
	pl := core.ProviderSelectedPayload{
		RunID:    runID.String(),
		Provider: provider,
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	_ = bus.Emit(ctx, core.EventTypeProviderSelected, b)
}

// hasSingleModelLabel reports whether beadLabels carries EXACTLY ONE
// model:<alias> label (mirrors resolveModelField's exactly-one test,
// modelpreference.go:212-224). False for both zero and more-than-one
// model: labels.
//
// Used at claim time (workloop.go) to decide the locked model/profile
// precedence: when a profile is present and hasSingleModelLabel is false,
// resolvedModel coalesces to profile.Model; when true, ResolveModelPreference's
// tier-1 result (already reflecting the model: label) wins and is left as-is.
func hasSingleModelLabel(beadLabels []string) bool {
	count := 0
	for _, lbl := range beadLabels {
		if strings.HasPrefix(lbl, labelPrefixModel) {
			count++
		}
	}
	return count == 1
}
