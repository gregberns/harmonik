package handlercontract_test

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

func stabilityFixtureInterfaceMethods(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ.Kind() != reflect.Interface {
		t.Fatalf("stabilityFixtureInterfaceMethods: %v is a %s, not an interface", typ, typ.Kind())
	}
	methods := make([]string, typ.NumMethod())
	for i := range typ.NumMethod() {
		methods[i] = typ.Method(i).Name
	}
	sort.Strings(methods)
	return methods
}

func stabilityFixtureVisibleFields(t *testing.T, typ reflect.Type) []string {
	t.Helper()
	if typ.Kind() != reflect.Struct {
		t.Fatalf("stabilityFixtureVisibleFields: %v is a %s, not a struct", typ, typ.Kind())
	}
	var names []string
	for _, f := range reflect.VisibleFields(typ) {
		if f.IsExported() {
			names = append(names, f.Name)
		}
	}
	sort.Strings(names)
	return names
}

// TestStabilityHC053_HandlerInterface asserts that the Handler interface exposes
// exactly the two methods declared in specs/handler-contract.md §6.1.
//
// Adding, removing, or renaming a method on Handler is a breaking change per
// HC-053 and requires a foundation amendment.  Update the wantMethods slice
// below only after the amendment has been filed.
func TestStabilityHC053_HandlerInterface(t *testing.T) {
	t.Parallel()

	wantMethods := []string{
		"AgentType",
		"Launch",
	}

	got := stabilityFixtureInterfaceMethods(t, reflect.TypeOf((*handlercontract.Handler)(nil)).Elem())

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

// TestStabilityHC053_SessionInterface asserts that the Session interface exposes
// exactly the six methods declared in specs/handler-contract.md §6.1.
//
// Adding, removing, or renaming a method on Session is a breaking change per
// HC-053 and requires a foundation amendment.
func TestStabilityHC053_SessionInterface(t *testing.T) {
	t.Parallel()

	wantMethods := []string{
		"Attach",
		"ID",
		"Kill",
		"LogLocation",
		"SendInput",
		"Wait",
	}

	got := stabilityFixtureInterfaceMethods(t, reflect.TypeOf((*handlercontract.Session)(nil)).Elem())

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

	got := stabilityFixtureVisibleFields(t, reflect.TypeOf(handlercontract.LaunchSpec{}))

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

// TestStabilityHC053_ErrorTaxonomySentinelCount asserts that exactly seven
// sentinel error variables exist as declared in specs/handler-contract.md §6.1
// and §8: five primary classes and two structural sub-sentinels.
//
// Adding a mandatory new sentinel class is a breaking change requiring a
// foundation amendment per §6.3 ("A breaking change to the sentinel set...").
func TestStabilityHC053_ErrorTaxonomySentinelCount(t *testing.T) {
	t.Parallel()

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

	for _, p := range primaries {
		if p.err == nil {
			t.Errorf("HC-053: primary sentinel %s is nil; want non-nil error variable", p.name)
		}
	}

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

// TestStabilityHC053_ProgressMsgTypeSet asserts that exactly the progress-stream
// message types declared in specs/handler-contract.md §4.2.HC-007 are present
// as package-level constants.
//
// The 12 required handler-emitted types plus launch_initiated (relay-only) are
// tested via their string values.  Adding a new type is additive; removing or
// renaming one is a breaking change requiring a foundation amendment per HC-053.
func TestStabilityHC053_ProgressMsgTypeSet(t *testing.T) {
	t.Parallel()

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

// TestStabilityHC053_SpecCorpusSensor verifies that the spec text contains the
// HC-053 requirement identifier and its key stability clause.  A failure here
// means the spec was edited in a way that silently removed or rewrote the
// stability requirement; any such edit must be intentional.
func TestStabilityHC053_SpecCorpusSensor(t *testing.T) {
	t.Parallel()

	spec := stabilityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-053") {
		t.Error(
			"HC-053 not found in specs/handler-contract.md; " +
				"the stability requirement has been removed from the spec",
		)
	}

	const stabilityClause = "MUST remain stable across execution-shape evolution"
	if !strings.Contains(spec, stabilityClause) {
		t.Errorf(
			"stability clause %q not found in specs/handler-contract.md; "+
				"the HC-053 normative text may have been silently reworded",
			stabilityClause,
		)
	}

	const conformanceCitation = "HC-051 — HC-053 (modularity)"
	if !strings.Contains(spec, conformanceCitation) {
		t.Errorf(
			"conformance citation %q not found in specs/handler-contract.md §10.2; "+
				"the test-surface obligation for HC-053 may have been removed",
			conformanceCitation,
		)
	}
}
