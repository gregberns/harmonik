package handlercontract_test

// cp051_skill_mechanism_tagged_test.go — CP-051 sensor tests.
//
// CP-051 (specs/control-points.md §4.11.CP-051) states:
//
//	"Skill declaration, resolution, and provisioning MUST be mechanism-tagged:
//	 no cognition participates in determining the effective skill set."
//
// This file verifies that the key implementation types in the
// declaration-to-provisioning pipeline carry "Tags: mechanism" in their
// godoc comments, encoding CP-051's prohibition on cognition participation
// as a source-verifiable invariant.
//
// Checked types:
//   - ResolvedSkill (skillresolution_hc047_hc048.go) — resolution phase
//   - SkillsProvisionedMsg (skillsprovisioned_hc049.go) — provisioning phase
//
// The tests use go/parser with ParseComments mode so that a future contributor
// removing or altering the tag causes an immediate failure.
//
// Helper prefix: cp051Fixture (per implementer-protocol.md §Helper-prefix
// discipline; bead hk-a8bg.53).

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// cp051FixtureDirPath returns the absolute path to the handlercontract package
// directory by resolving relative to this test file's source location.
func cp051FixtureDirPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cp051FixtureDirPath: runtime.Caller(0) failed")
	}
	return filepath.Dir(thisFile)
}

// cp051FixtureTagsLine matches a "Tags: mechanism" line in a doc comment.
var cp051FixtureTagsLine = regexp.MustCompile(`\bTags:.*\bmechanism\b`)

// cp051FixtureFindTypeDoc parses the named Go source file and returns the
// godoc comment text for the named type declaration, or ("", false) if the
// type is not found.
func cp051FixtureFindTypeDoc(t *testing.T, srcPath, typeName string) (string, bool) {
	t.Helper()

	fset := token.NewFileSet()
	//nolint:gosec // G304: path derived from runtime.Caller + same-package dir; not user input.
	f, err := parser.ParseFile(fset, srcPath, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("cp051FixtureFindTypeDoc: parser.ParseFile(%q): %v", srcPath, err)
	}

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		if genDecl.Doc == nil {
			continue
		}
		for _, spec := range genDecl.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			if ts.Name.Name == typeName {
				return genDecl.Doc.Text(), true
			}
		}
	}
	return "", false
}

// cp051FixtureAssertTypeIsMechanismTagged is the shared assertion used by each
// CP-051 sensor sub-test. It parses srcFile, locates typeName, and fails the
// test if the type is missing or its godoc lacks "Tags: mechanism".
func cp051FixtureAssertTypeIsMechanismTagged(t *testing.T, srcFile, typeName string) {
	t.Helper()

	docText, found := cp051FixtureFindTypeDoc(t, srcFile, typeName)
	if !found {
		t.Fatalf(
			"CP-051 sensor: type %s not found in %s; "+
				"the type must exist and carry a godoc comment with 'Tags: mechanism'",
			typeName, srcFile,
		)
	}

	for _, line := range strings.Split(docText, "\n") {
		if cp051FixtureTagsLine.MatchString(line) {
			t.Logf(
				"CP-051 sensor PASS: %s.%s godoc carries %q "+
					"(specs/control-points.md §4.11.CP-051)",
				filepath.Base(srcFile), typeName, strings.TrimSpace(line),
			)
			return
		}
	}

	t.Errorf(
		"CP-051 sensor FAIL: %s type %s godoc does not carry a 'Tags: mechanism' line;\n"+
			"CP-051 requires the skill declaration-to-provisioning pipeline to be\n"+
			"mechanism-tagged (specs/control-points.md §4.11.CP-051).\n"+
			"Godoc text:\n%s",
		srcFile, typeName, docText,
	)
}

// TestCP051_ResolvedSkillIsMechanismTagged verifies that the ResolvedSkill type
// in skillresolution_hc047_hc048.go carries "Tags: mechanism" in its godoc.
//
// ResolvedSkill represents the resolution phase of the skill pipeline.
// CP-051 requires the resolution step to be mechanism-tagged: a deterministic
// filesystem lookup with no cognition participation.
//
// Spec ref: specs/control-points.md §4.11.CP-051.
func TestCP051_ResolvedSkillIsMechanismTagged(t *testing.T) {
	t.Parallel()

	srcFile := filepath.Join(cp051FixtureDirPath(t), "skillresolution_hc047_hc048.go")
	cp051FixtureAssertTypeIsMechanismTagged(t, srcFile, "ResolvedSkill")
}

// TestCP051_SkillsProvisionedMsgIsMechanismTagged verifies that the
// SkillsProvisionedMsg type in skillsprovisioned_hc049.go carries
// "Tags: mechanism" in its godoc.
//
// SkillsProvisionedMsg represents the provisioning phase of the skill pipeline.
// CP-051 requires the provisioning step to be mechanism-tagged: the effective
// skill set is determined entirely by declared LaunchSpec fields, not by any
// model call.
//
// Spec ref: specs/control-points.md §4.11.CP-051.
func TestCP051_SkillsProvisionedMsgIsMechanismTagged(t *testing.T) {
	t.Parallel()

	srcFile := filepath.Join(cp051FixtureDirPath(t), "skillsprovisioned_hc049.go")
	cp051FixtureAssertTypeIsMechanismTagged(t, srcFile, "SkillsProvisionedMsg")
}
