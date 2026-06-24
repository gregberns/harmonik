package scenario

// sh_inv_004_rerun_diff.go — SH-INV-004 nightly rerun-diff sensor.
//
// Implements the determinism-contract field-set diff check declared in
// specs/scenario-harness.md §5 SH-INV-004 (Identical inputs produce identical
// verdicts on rerun) and §4.8 (Repeatability and determinism — SH-027).
//
// The sensor compares N≥1 ScenarioResult snapshots of the same scenario across
// runs and reports any divergences in the contract field set:
//
//   - verdict
//   - failure_class
//   - ordered list of (AssertionResult.passed, AssertionResult.assertion_kind) tuples
//   - multiset of distinct event types observed in the captured JSONL
//
// Byte-identity of the captured JSONL is NOT required per §4.8 (UUIDv7 event
// IDs, wall-clock timestamps, PIDs, and daemon_instance_id drift on every run).
// Semantic identity of the contract field set is required.
//
// Scope carve-out (§4.8): scenarios tagged cadence_tag=nightly and exercising
// wall-clock heartbeat mode (HC-026a) are exempt. The caller is responsible for
// excluding those scenarios before calling DiffRerunSnapshots.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-004, §4.8 SH-027.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// AssertionTuple is the (passed, assertion_kind) projection of AssertionResult
// used in the SH-INV-004 contract field set comparison.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-027.
type AssertionTuple struct {
	Passed        bool
	AssertionKind AssertionResultKind
}

// RerunDiffSnapshot holds the determinism contract field set extracted from a
// single ScenarioResult execution. EventTypeMultiset is populated separately
// from the captured JSONL via ReadEventTypeMultiset (nil is valid when the
// event log is unavailable or not yet read).
//
// Spec ref: specs/scenario-harness.md §4.8 SH-027.
type RerunDiffSnapshot struct {
	// ScenarioName identifies the scenario this snapshot belongs to.
	ScenarioName string

	// Verdict is the top-level verdict from ScenarioResult.
	Verdict ScenarioVerdict

	// FailureClass is the harness failure class from ScenarioResult.
	// Empty string represents None (verdict=pass).
	FailureClass FailureClass

	// AssertionTuples is the ordered list of (passed, assertion_kind) projections
	// from ScenarioResult.AssertionResults, preserving source order.
	AssertionTuples []AssertionTuple

	// EventTypeMultiset is the multiset of event types observed in the captured
	// JSONL event log (map from EventType to occurrence count). nil and an empty
	// map are treated as identical by DiffRerunSnapshots.
	EventTypeMultiset map[core.EventType]int
}

// RerunDivergence describes a single contract-field-set divergence between the
// reference run (index 0) and another run.
type RerunDivergence struct {
	// FieldName is the contract field where divergence was detected:
	// "verdict", "failure_class", "assertion_tuples", or "event_type_multiset".
	FieldName string

	// RunIndex is the index of the diverged run in the input slice (always ≥ 1;
	// run 0 is the reference).
	RunIndex int

	// BaseValue is the canonical string representation of the reference (run 0) value.
	BaseValue string

	// DivergedValue is the canonical string representation of the diverged run's value.
	DivergedValue string
}

// RerunDiffReport is the output of DiffRerunSnapshots.
type RerunDiffReport struct {
	// Uniform reports whether all N runs produced the same contract field set.
	// True iff Divergences is empty.
	Uniform bool

	// Divergences lists each contract-field-set divergence found. Empty when
	// all runs are uniform. Within each run index, fields appear in declaration
	// order: verdict, failure_class, assertion_tuples, event_type_multiset.
	Divergences []RerunDivergence
}

// SnapshotFromResult extracts the non-JSONL portion of the contract field set
// from a ScenarioResult. The EventTypeMultiset field is NOT populated (nil);
// callers must supply it from ReadEventTypeMultiset when the event log is available.
//
// Spec ref: specs/scenario-harness.md §4.8 SH-027.
func SnapshotFromResult(r ScenarioResult) RerunDiffSnapshot {
	tuples := make([]AssertionTuple, len(r.AssertionResults))
	for i, ar := range r.AssertionResults {
		tuples[i] = AssertionTuple{
			Passed:        ar.Passed,
			AssertionKind: ar.AssertionKind,
		}
	}
	return RerunDiffSnapshot{
		ScenarioName:    r.ScenarioName,
		Verdict:         r.Verdict,
		FailureClass:    r.FailureClass,
		AssertionTuples: tuples,
	}
}

// minimalEventEnvelope is the minimal JSON structure for type-only parsing of
// JSONL event log records per specs/event-model.md §6.1.
type minimalEventEnvelope struct {
	Type string `json:"type"`
}

// ReadEventTypeMultiset reads the JSONL event log at jsonlPath and returns a
// multiset of event types (map from EventType to occurrence count). Lines that
// are empty, whitespace-only, or fail JSON decode are silently skipped (torn-tail
// tolerance per SH-020). Returns a non-nil empty map when the file contains no
// decodable events.
//
// Spec ref: specs/scenario-harness.md §4.6 SH-020, §4.8 SH-027.
func ReadEventTypeMultiset(jsonlPath string) (map[core.EventType]int, error) {
	//nolint:gosec // G304: jsonlPath is a harness-internal fixture path
	f, err := os.Open(jsonlPath)
	if err != nil {
		return nil, fmt.Errorf("ReadEventTypeMultiset: open %q: %w", jsonlPath, err)
	}
	defer f.Close()

	multiset := make(map[core.EventType]int)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var env minimalEventEnvelope
		if err := json.Unmarshal([]byte(line), &env); err != nil || env.Type == "" {
			continue // torn tail or non-standard line: skip per SH-020
		}
		multiset[core.EventType(env.Type)]++
	}
	if err := scanner.Err(); err != nil {
		return multiset, fmt.Errorf("ReadEventTypeMultiset: scan %q: %w", jsonlPath, err)
	}
	return multiset, nil
}

// DiffRerunSnapshots compares N snapshots of the same scenario and returns a
// RerunDiffReport. Run 0 is the reference; all subsequent runs are compared
// against it. Returns Uniform=true when len(snapshots) ≤ 1 (vacuous: nothing
// to diff).
//
// The spec mandates N≥10 for the nightly cadence run. This function imposes no
// minimum; the caller is responsible for meeting the count requirement.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-004, §4.8 SH-027.
func DiffRerunSnapshots(snapshots []RerunDiffSnapshot) *RerunDiffReport {
	report := &RerunDiffReport{Uniform: true}
	if len(snapshots) <= 1 {
		return report
	}

	base := snapshots[0]
	baseATuples := rerunFormatAssertionTuples(base.AssertionTuples)
	baseMultiset := rerunFormatEventTypeMultiset(base.EventTypeMultiset)

	for i, s := range snapshots[1:] {
		runIdx := i + 1

		if s.Verdict != base.Verdict {
			report.Divergences = append(report.Divergences, RerunDivergence{
				FieldName:     "verdict",
				RunIndex:      runIdx,
				BaseValue:     string(base.Verdict),
				DivergedValue: string(s.Verdict),
			})
		}

		if s.FailureClass != base.FailureClass {
			report.Divergences = append(report.Divergences, RerunDivergence{
				FieldName:     "failure_class",
				RunIndex:      runIdx,
				BaseValue:     string(base.FailureClass),
				DivergedValue: string(s.FailureClass),
			})
		}

		if cur := rerunFormatAssertionTuples(s.AssertionTuples); cur != baseATuples {
			report.Divergences = append(report.Divergences, RerunDivergence{
				FieldName:     "assertion_tuples",
				RunIndex:      runIdx,
				BaseValue:     baseATuples,
				DivergedValue: cur,
			})
		}

		if cur := rerunFormatEventTypeMultiset(s.EventTypeMultiset); cur != baseMultiset {
			report.Divergences = append(report.Divergences, RerunDivergence{
				FieldName:     "event_type_multiset",
				RunIndex:      runIdx,
				BaseValue:     baseMultiset,
				DivergedValue: cur,
			})
		}
	}

	report.Uniform = len(report.Divergences) == 0
	return report
}

// rerunFormatAssertionTuples serialises the ordered assertion-tuple list to a
// canonical string for comparison. Format: "true:event_present|false:exit_code|…".
// Returns "" for a nil or empty slice.
func rerunFormatAssertionTuples(tuples []AssertionTuple) string {
	if len(tuples) == 0 {
		return ""
	}
	parts := make([]string, len(tuples))
	for i, t := range tuples {
		parts[i] = fmt.Sprintf("%v:%s", t.Passed, t.AssertionKind)
	}
	return strings.Join(parts, "|")
}

// rerunFormatEventTypeMultiset serialises the event-type multiset to a
// canonical sorted string for comparison. Format: "agent_ready=2,outcome=1".
// Returns "" for a nil or empty map.
func rerunFormatEventTypeMultiset(m map[core.EventType]int) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s=%d", k, m[core.EventType(k)])
	}
	return strings.Join(parts, ",")
}
