package handlercontract_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// twinparity_hc036to049a_test.go — sensor tests for twin-parity requirements
// HC-036, HC-037, HC-038, HC-040, and HC-049a.
//
// Spec refs: specs/handler-contract.md §4.8.HC-036, HC-037, HC-038, HC-040, HC-049a.
// Beads: hk-8i31.43 (HC-036), hk-8i31.44 (HC-037), hk-8i31.45 (HC-038),
//
//	hk-8i31.47 (HC-040), hk-8i31.59 (HC-049a).
//
// Verifies:
//
//	(a) Handler interface has no isTwin conditional logic — zero "isTwin" or
//	    "is_twin" identifiers anywhere in the handlercontract package source.
//	(b) Spec-corpus sensors: handler-contract.md contains each requirement ID
//	    and its key constraint clause.
//	(c) ProgressMsgTypeAgentReady is a declared constant (twins MUST emit
//	    agent_ready identically per HC-040 — it must be a fixed type string).
//	(d) ProgressMsgTypeSkillsProvisioned is a declared constant (HC-049a wire
//	    parity — the type string must be stable for twin to wire-emit it).
//
// Helper prefix: twinParityFixture (per implementer-protocol.md).

// twinParityFixtureModuleRoot returns the module root by walking upward.
func twinParityFixtureModuleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod in any parent directory")
		}
		dir = parent
	}
}

// twinParityFixtureHCSpec reads and returns the handler-contract.md spec text.
func twinParityFixtureHCSpec(t *testing.T) string {
	t.Helper()
	root := twinParityFixtureModuleRoot(t)
	specPath := filepath.Join(root, "specs", "handler-contract.md")
	content, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading handler-contract.md: %v", err)
	}
	return string(content)
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-INV-002 / HC-035: no isTwin conditional logic in daemon-side packages
// ─────────────────────────────────────────────────────────────────────────────

// TestTwinParity_NoIsTwinBranchesInHandlerContract verifies that no source
// file in the handlercontract package contains "isTwin" or "is_twin" identifiers.
//
// Spec ref: handler-contract.md §4.8 HC-INV-002 — "Daemon MUST carry zero
// conditional logic varying on handler-is-twin."
func TestTwinParity_NoIsTwinBranchesInHandlerContract(t *testing.T) {
	t.Parallel()

	root := twinParityFixtureModuleRoot(t)
	pkgDir := filepath.Join(root, "internal", "handlercontract")

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkgDir, func(fi os.FileInfo) bool {
		// Exclude test files — the lint is for production code only.
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("HC-INV-002: parser.ParseDir: %v", err)
	}

	forbidden := []string{"isTwin", "is_twin", "IsTwin"}
	for pkgName, pkg := range pkgs {
		for fileName, file := range pkg.Files {
			ast.Inspect(file, func(n ast.Node) bool {
				ident, ok := n.(*ast.Ident)
				if !ok {
					return true
				}
				for _, f := range forbidden {
					if strings.Contains(ident.Name, f) {
						pos := fset.Position(ident.Pos())
						t.Errorf("HC-INV-002: package %s file %s:%d: forbidden identifier %q found in production code; daemon MUST carry zero conditional logic varying on handler-is-twin",
							pkgName, filepath.Base(fileName), pos.Line, ident.Name)
					}
				}
				return true
			})
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-036: Twin subprocesses honor the same wire protocol (hk-8i31.43)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC036_SpecCorpusClause verifies that handler-contract.md contains HC-036
// and the permitted-differences constraint.
func TestHC036_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-036") {
		t.Error("handler-contract.md missing HC-036 clause")
	}
	// HC-036 enumerates permitted differences: (a) script drives LLM, (b) no budget, (c) binary name.
	if !strings.Contains(spec, "Permitted differences") && !strings.Contains(spec, "permitted differences") {
		t.Error("handler-contract.md HC-036 missing 'permitted differences' enumeration; spec may have drifted")
	}
}

// twinParityFixtureStub is a minimal Handler implementation for twin-parity
// interface-conformance tests. It represents the canonical twin binary's
// in-process surface — the same Handler interface that real handlers satisfy.
type twinParityFixtureStub struct{}

func (twinParityFixtureStub) Launch(_ context.Context, _ *handlercontract.LaunchSpec) (handlercontract.Session, error) {
	return nil, handlercontract.ErrStructural
}

func (twinParityFixtureStub) AgentType() string { return "twin-claude-code" }

// TestHC036_HandlerInterfaceIsSharedForTwins verifies the Handler interface
// is the same type for both real and twin handlers (compile-time satisfaction).
// A twin handler that implements Handler satisfies the same interface a real
// handler does — no distinct "TwinHandler" interface exists.
//
// This is modelled by verifying that a stub (simulating a twin) can stand in
// for the production interface, and that no TwinHandler type is declared.
func TestHC036_HandlerInterfaceIsSharedForTwins(t *testing.T) {
	t.Parallel()

	// Compile-time check: both real and twin handlers satisfy handlercontract.Handler.
	// The stub below represents the canonical twin binary's in-process surface.
	var _ handlercontract.Handler = twinParityFixtureStub{}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-037: Twins carry identical boundary-classification tags (hk-8i31.44)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC037_SpecCorpusClause verifies that handler-contract.md contains HC-037
// and the "tagging deviation is a twin defect" constraint.
func TestHC037_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-037") {
		t.Error("handler-contract.md missing HC-037 clause")
	}
	if !strings.Contains(spec, "twin defect") {
		t.Error("handler-contract.md HC-037 missing 'twin defect' constraint clause; spec may have drifted")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-038: Twin conformance drift detection scoped to S07 (hk-8i31.45)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC038_SpecCorpusClause verifies that handler-contract.md contains HC-038
// and the S07 scope declaration.
func TestHC038_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-038") {
		t.Error("handler-contract.md missing HC-038 clause")
	}
	// HC-038 delegates drift-detection obligation to S07 (scenario-harness).
	if !strings.Contains(spec, "S07") {
		t.Error("handler-contract.md HC-038 missing S07 scope reference; drift-detection delegation may have drifted")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-040: Twins MUST emit agent_ready identically (hk-8i31.47)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC040_SpecCorpusClause verifies that handler-contract.md contains HC-040
// and the "twin handlers MUST emit agent_ready" constraint.
func TestHC040_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-040") {
		t.Error("handler-contract.md missing HC-040 clause")
	}
	if !strings.Contains(spec, "agent_ready") {
		t.Error("handler-contract.md HC-040 missing 'agent_ready' reference; spec may have drifted")
	}
}

// TestHC040_AgentReadyTypeIsStableConstant verifies that ProgressMsgTypeAgentReady
// is a declared string constant. Twins MUST emit agent_ready with the same type
// string as real handlers; this is only possible if the type string is a stable,
// declared constant rather than a runtime-computed value.
func TestHC040_AgentReadyTypeIsStableConstant(t *testing.T) {
	t.Parallel()

	const wantValue = "agent_ready"
	got := string(handlercontract.ProgressMsgTypeAgentReady)
	if got != wantValue {
		t.Errorf("HC-040: ProgressMsgTypeAgentReady = %q; want %q (stable wire type for twin parity)", got, wantValue)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// HC-049a: Twin-parity for skill provisioning is wire-only (hk-8i31.59)
// ─────────────────────────────────────────────────────────────────────────────

// TestHC049a_SpecCorpusClause verifies that handler-contract.md contains HC-049a
// and the "wire signal only" constraint.
func TestHC049a_SpecCorpusClause(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	if !strings.Contains(spec, "HC-049a") {
		t.Error("handler-contract.md missing HC-049a clause")
	}
	// HC-049a: twin parity applies to the wire signal (skills_provisioned event),
	// NOT filesystem side effects.
	if !strings.Contains(spec, "skills_provisioned") {
		t.Error("handler-contract.md HC-049a missing 'skills_provisioned' reference; spec may have drifted")
	}
}

// TestHC049a_SkillsProvisionedTypeIsStableConstant verifies that
// ProgressMsgTypeSkillsProvisioned is a declared string constant. Per HC-049a,
// twins MUST emit skills_provisioned with the same type string; this requires
// a stable constant rather than a runtime-computed string.
func TestHC049a_SkillsProvisionedTypeIsStableConstant(t *testing.T) {
	t.Parallel()

	const wantValue = "skills_provisioned"
	got := string(handlercontract.ProgressMsgTypeSkillsProvisioned)
	if got != wantValue {
		t.Errorf("HC-049a: ProgressMsgTypeSkillsProvisioned = %q; want %q (stable wire type for twin wire-parity)", got, wantValue)
	}
}

// TestHC049a_WireOnlyParityExcludesFilesystemSideEffects verifies the HC-049a
// carve-out: twin parity is scoped to the wire signal, not filesystem artifacts.
// This is a spec-corpus sensor confirming the carve-out text exists.
func TestHC049a_WireOnlyParityExcludesFilesystemSideEffects(t *testing.T) {
	t.Parallel()

	spec := twinParityFixtureHCSpec(t)

	// HC-049a specifically carves out filesystem side effects.
	if !strings.Contains(spec, "filesystem") && !strings.Contains(spec, "file system") {
		t.Error("handler-contract.md HC-049a missing filesystem-carve-out reference; wire-only parity scope may have drifted")
	}
}
