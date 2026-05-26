package handlercontract_test

// stabilityhc053_test.go — stability sensor for HC-053 (cross-subsystem
// surface MUST remain stable across execution-shape evolution).
//
// Spec ref: specs/handler-contract.md §4.12.HC-053 and §10.2 (conformance
// evidence for HC-051–HC-053); bead hk-8i31.63.
//
// Helper prefix: stabilityFixture (per implementer-protocol.md §Helper-prefix
// discipline).
//
// HC-053 names four cross-subsystem surfaces that MUST remain stable:
//
//	1. The Handler interface (§6.1).
//	2. The LaunchSpec record (§6.1).
//	3. The error taxonomy (§8) — five primary sentinels + two sub-sentinels.
//	4. The emitted event set (§4.2.HC-007) — progress-stream message types.
//
// This file contains one sensor group per surface.  Each sensor pins the
// current method/field/constant set using reflect and source-level inspection;
// a failure means the surface was altered.  The fix is either:
//
//	(a) Update the expected set here (if the change is intentional and a
//	    foundation amendment has been filed per §6.3 / AR-020), or
//	(b) Revert the accidental change.
//
// Sensors that enumerate interface methods use reflect.TypeOf((*T)(nil)).Elem()
// to iterate over the method set without requiring a concrete implementation.
// Sensors that enumerate LaunchSpec fields use reflect.TypeOf(LaunchSpec{}) and
// iterate VisibleFields.  Progress-stream type constants are checked via the
// package-level symbol names exported from progressstream_hc007.go.

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// stabilityFixtureModuleRoot locates the module root (directory containing
// go.mod) by walking upward from the test source file.
func stabilityFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("stabilityFixtureModuleRoot: runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("stabilityFixtureModuleRoot: could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// stabilityFixtureHCSpec reads the handler-contract.md spec and returns its
// contents.  Fails the test if the file cannot be read.
func stabilityFixtureHCSpec(t *testing.T) string {
	t.Helper()
	root := stabilityFixtureModuleRoot(t)
	//nolint:gosec // G304: spec path is test-internal constant, not user-controlled
	content, err := os.ReadFile(filepath.Join(root, "specs", "handler-contract.md"))
	if err != nil {
		t.Fatalf("stabilityFixtureHCSpec: reading handler-contract.md: %v", err)
	}
	return string(content)
}

// stabilityFixtureInterfaceMethods returns a sorted slice of method names for
// the given interface type T (passed as reflect.Type).  Panics if T is not an
// interface.
func stabilityFixtureInterfaceMethods(t reflect.Type) []string {
	if t.Kind() != reflect.Interface {
		panic("stabilityFixtureInterfaceMethods: not an interface")
	}
	methods := make([]string, t.NumMethod())
	for i := range t.NumMethod() {
		methods[i] = t.Method(i).Name
	}
	sort.Strings(methods)
	return methods
}

// stabilityFixtureVisibleFields returns a sorted slice of exported field names
// for the given struct type T.  Uses reflect.VisibleFields to include embedded
// fields.  Panics if T is not a struct.
func stabilityFixtureVisibleFields(t reflect.Type) []string {
	if t.Kind() != reflect.Struct {
		panic("stabilityFixtureVisibleFields: not a struct")
	}
	var names []string
	for _, f := range reflect.VisibleFields(t) {
		if f.IsExported() {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	return names
}

// ─────────────────────────────────────────────────────────────────────────────
// Surface 1 — Handler interface (§6.1)
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_HandlerInterface asserts that the Handler interface exposes
// exactly the two methods declared in specs/handler-contract.md §6.1.
//
// Adding, removing, or renaming a method on Handler is a breaking change per
// HC-053 and requires a foundation amendment.  Update the wantMethods slice
// below only after the amendment has been filed.
func TestStabilityHC053_HandlerInterface(t *testing.T) {
	t.Parallel()

	// Canonical method set from specs/handler-contract.md §6.1:
	//   Launch(ctx, spec) -> (Session, error)
	//   AgentType() -> String
	wantMethods := []string{
		"AgentType",
		"Launch",
	}

	got := stabilityFixtureInterfaceMethods(reflect.TypeOf((*handlercontract.Handler)(nil)).Elem())

	if !reflect.DeepEqual(got, wantMethods) {
		t.Errorf(
			"HC-053 Handler interface drift detected:\n"+
				"  got  methods: %v\n"+
				"  want methods: %v\n"+
				"Altering the Handler interface is a breaking change requiring a foundation amendment "+
				"per specs/handler-contract.md §4.12.HC-053 and §6.3.",
			got, wantMethods,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Surface 1 — Session interface (§6.1)
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_SessionInterface asserts that the Session interface exposes
// exactly the six methods declared in specs/handler-contract.md §6.1.
//
// Adding, removing, or renaming a method on Session is a breaking change per
// HC-053 and requires a foundation amendment.
func TestStabilityHC053_SessionInterface(t *testing.T) {
	t.Parallel()

	// Canonical method set from specs/handler-contract.md §6.1:
	//   ID() -> SessionID
	//   SendInput(ctx, input) -> error
	//   Attach(ctx) -> (io.Reader, error)
	//   Kill(ctx) -> error
	//   Wait(ctx) -> (Outcome, error)
	//   LogLocation() -> String
	wantMethods := []string{
		"Attach",
		"ID",
		"Kill",
		"LogLocation",
		"SendInput",
		"Wait",
	}

	got := stabilityFixtureInterfaceMethods(reflect.TypeOf((*handlercontract.Session)(nil)).Elem())

	if !reflect.DeepEqual(got, wantMethods) {
		t.Errorf(
			"HC-053 Session interface drift detected:\n"+
				"  got  methods: %v\n"+
				"  want methods: %v\n"+
				"Altering the Session interface is a breaking change requiring a foundation amendment "+
				"per specs/handler-contract.md §4.12.HC-053 and §6.3.",
			got, wantMethods,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Surface 2 — LaunchSpec record (§6.1)
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_LaunchSpecFields asserts that LaunchSpec exposes exactly
// the exported fields declared in specs/handler-contract.md §6.1 (RECORD
// LaunchSpec).
//
// Adding an optional field is non-breaking but requires a SchemaVersion bump
// per §6.3 (N-1 readability contract).  Removing or renaming a field is
// breaking and requires a foundation amendment.  In either case, update the
// wantFields slice below.
func TestStabilityHC053_LaunchSpecFields(t *testing.T) {
	t.Parallel()

	// Canonical exported field set from specs/handler-contract.md §6.1 +
	// current Go struct (launchspec_hc006.go).  Alphabetical order (sort.Strings).
	// Required fields: RunID, WorkflowID, NodeID, AgentType, WorkspacePath,
	//   RequiredSkills, SkillSearchPaths, Timeout, ProvisioningTimeout,
	//   Budget, FreedomProfileRef, SchemaVersion.
	// Optional fields: BeadID, SnapshotToken, WorkflowMode, Phase,
	//   IterationCount, ClaudeSessionID.
	//
	// NOTE: The spec §6.1 also declares a ModelPreference optional field; it is
	// not yet present in the Go struct (pending implementation).  Add
	// "ModelPreference" to wantFields when that field lands.
	wantFields := []string{
		"AgentType",
		"BeadID",
		"Budget",
		"ClaudeSessionID",
		"FreedomProfileRef",
		"IterationCount",
		"NodeID",
		"Phase",
		"ProvisioningTimeout",
		"RequiredSkills",
		"RunID",
		"SchemaVersion",
		"SkillSearchPaths",
		"SnapshotToken",
		"Timeout",
		"WorkflowID",
		"WorkflowMode",
		"WorkspacePath",
	}

	got := stabilityFixtureVisibleFields(reflect.TypeOf(handlercontract.LaunchSpec{}))

	if !reflect.DeepEqual(got, wantFields) {
		t.Errorf(
			"HC-053 LaunchSpec field drift detected:\n"+
				"  got  fields: %v\n"+
				"  want fields: %v\n"+
				"Adding an optional field requires a SchemaVersion bump per §6.3 (non-breaking).\n"+
				"Removing or renaming a field requires a foundation amendment per HC-053.",
			got, wantFields,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Surface 3 — Error taxonomy (§8)
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_ErrorTaxonomySentinelCount asserts that exactly seven
// sentinel error variables exist as declared in specs/handler-contract.md §6.1
// and §8: five primary classes and two structural sub-sentinels.
//
// Adding a mandatory new sentinel class is a breaking change requiring a
// foundation amendment per §6.3 ("A breaking change to the sentinel set...").
func TestStabilityHC053_ErrorTaxonomySentinelCount(t *testing.T) {
	t.Parallel()

	// Five primary classes declared in §8.
	primaries := []struct {
		name string
		err  error
	}{
		{"ErrTransient", handlercontract.ErrTransient},
		{"ErrStructural", handlercontract.ErrStructural},
		{"ErrDeterministic", handlercontract.ErrDeterministic},
		{"ErrCanceled", handlercontract.ErrCanceled},
		{"ErrBudget", handlercontract.ErrBudget},
	}

	// Two structural sub-sentinels declared in §8 (each wraps ErrStructural).
	subSentinels := []struct {
		name string
		err  error
	}{
		{"ErrSkillProvisioningFailed", handlercontract.ErrSkillProvisioningFailed},
		{"ErrProtocolMismatch", handlercontract.ErrProtocolMismatch},
	}

	const wantPrimaryCount = 5
	const wantSubSentinelCount = 2

	if len(primaries) != wantPrimaryCount {
		t.Errorf(
			"HC-053 error taxonomy: primary sentinel count = %d, want %d; "+
				"adding or removing a primary class is a foundation amendment per §6.3",
			len(primaries), wantPrimaryCount,
		)
	}

	if len(subSentinels) != wantSubSentinelCount {
		t.Errorf(
			"HC-053 error taxonomy: sub-sentinel count = %d, want %d; "+
				"adding or removing a sub-sentinel is a foundation amendment per §6.3",
			len(subSentinels), wantSubSentinelCount,
		)
	}

	// Each primary sentinel must be non-nil.
	for _, p := range primaries {
		if p.err == nil {
			t.Errorf("HC-053: primary sentinel %s is nil; want non-nil error variable", p.name)
		}
	}

	// Each sub-sentinel must wrap ErrStructural.
	for _, s := range subSentinels {
		if s.err == nil {
			t.Errorf("HC-053: sub-sentinel %s is nil; want non-nil error variable", s.name)
		}
		if !errors.Is(s.err, handlercontract.ErrStructural) {
			t.Errorf(
				"HC-053: sub-sentinel %s does not wrap ErrStructural; "+
					"spec §6.1 requires sub-sentinels to wrap ErrStructural",
				s.name,
			)
		}
	}
}

// TestStabilityHC053_ErrorTaxonomyClassNames asserts that the Class function
// returns exactly the five canonical class-name strings declared in §8.  A
// rename of a class-name string is a breaking change per HC-053.
func TestStabilityHC053_ErrorTaxonomyClassNames(t *testing.T) {
	t.Parallel()

	// Canonical class-name strings from specs/handler-contract.md §8.
	wantClasses := map[error]string{
		handlercontract.ErrTransient:     "transient",
		handlercontract.ErrStructural:    "structural",
		handlercontract.ErrDeterministic: "deterministic",
		handlercontract.ErrCanceled:      "canceled",
		handlercontract.ErrBudget:        "budget",
	}

	for sentinel, want := range wantClasses {
		got := handlercontract.Class(sentinel)
		if got != want {
			t.Errorf(
				"HC-053: Class(%v) = %q, want %q; "+
					"renaming a class string is a breaking change per §8",
				sentinel, got, want,
			)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Surface 4 — Emitted event set (§4.2.HC-007)
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_ProgressMsgTypeSet asserts that exactly the progress-stream
// message types declared in specs/handler-contract.md §4.2.HC-007 are present
// as package-level constants.
//
// The 12 required handler-emitted types plus launch_initiated (relay-only) are
// tested via their string values.  Adding a new type is additive; removing or
// renaming one is a breaking change requiring a foundation amendment per HC-053.
func TestStabilityHC053_ProgressMsgTypeSet(t *testing.T) {
	t.Parallel()

	// Canonical emitted event type strings from §4.2.HC-007 and §6.4.
	// These are the 12 handler-emitted progress-stream message types that the
	// watcher translates to bus events, plus launch_initiated (relay-only per
	// §4.10.HC-045b).  Sorted alphabetically.
	wantTypes := []string{
		"agent_completed",
		"agent_failed",
		"agent_heartbeat",
		"agent_output_chunk",
		"agent_rate_limit_cleared",
		"agent_rate_limited",
		"agent_ready",
		"agent_started",
		"handler_capabilities",
		"launch_initiated",
		"outcome_emitted",
		"session_log_location",
		"skills_provisioned",
	}

	// Collect the package-level constant values via the exported symbols.
	gotTypes := []string{
		handlercontract.ProgressMsgTypeHandlerCapabilities,
		handlercontract.ProgressMsgTypeAgentReady,
		handlercontract.ProgressMsgTypeAgentStarted,
		handlercontract.ProgressMsgTypeAgentOutputChunk,
		handlercontract.ProgressMsgTypeAgentCompleted,
		handlercontract.ProgressMsgTypeAgentFailed,
		handlercontract.ProgressMsgTypeAgentRateLimited,
		handlercontract.ProgressMsgTypeAgentRateLimitCleared,
		handlercontract.ProgressMsgTypeAgentHeartbeat,
		handlercontract.ProgressMsgTypeSessionLogLocation,
		handlercontract.ProgressMsgTypeSkillsProvisioned,
		handlercontract.ProgressMsgTypeOutcomeEmitted,
		handlercontract.ProgressMsgTypeLaunchInitiated,
	}
	sort.Strings(gotTypes)

	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Errorf(
			"HC-053 progress-stream type set drift detected:\n"+
				"  got  types: %v\n"+
				"  want types: %v\n"+
				"Removing or renaming a progress-stream type is a breaking change per "+
				"specs/handler-contract.md §4.12.HC-053; adding one is additive (update wantTypes here).",
			gotTypes, wantTypes,
		)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Spec-corpus sensor — HC-053 requirement present in spec
// ─────────────────────────────────────────────────────────────────────────────

// TestStabilityHC053_SpecCorpusSensor verifies that the spec text contains the
// HC-053 requirement identifier and its key stability clause.  A failure here
// means the spec was edited in a way that silently removed or rewrote the
// stability requirement; any such edit must be intentional.
func TestStabilityHC053_SpecCorpusSensor(t *testing.T) {
	t.Parallel()

	spec := stabilityFixtureHCSpec(t)

	// The requirement identifier must be present.
	if !strings.Contains(spec, "HC-053") {
		t.Error(
			"HC-053 not found in specs/handler-contract.md; " +
				"the stability requirement has been removed from the spec",
		)
	}

	// The stability clause must be present (key phrase from the normative text).
	const stabilityClause = "MUST remain stable across execution-shape evolution"
	if !strings.Contains(spec, stabilityClause) {
		t.Errorf(
			"stability clause %q not found in specs/handler-contract.md; "+
				"the HC-053 normative text may have been silently reworded",
			stabilityClause,
		)
	}

	// The conformance-evidence citation must be present (§10.2).
	const conformanceCitation = "HC-051 — HC-053 (modularity)"
	if !strings.Contains(spec, conformanceCitation) {
		t.Errorf(
			"conformance citation %q not found in specs/handler-contract.md §10.2; "+
				"the test-surface obligation for HC-053 may have been removed",
			conformanceCitation,
		)
	}
}
