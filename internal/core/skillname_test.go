package core_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// TestSkillName_Valid_NonEmpty verifies that a non-empty SkillName reports
// Valid() = true.
//
// Spec ref: control-points.md §6.2 — SkillName must be non-empty.
func TestSkillName_Valid_NonEmpty(t *testing.T) {
	t.Parallel()

	s := core.SkillName("beads-cli")
	if !s.Valid() {
		t.Errorf("SkillName(%q).Valid() = false, want true", s)
	}
}

// TestSkillName_Valid_Zero verifies that the zero SkillName reports Valid() = false.
//
// Spec ref: control-points.md §6.2.
func TestSkillName_Valid_Zero(t *testing.T) {
	t.Parallel()

	var s core.SkillName
	if s.Valid() {
		t.Error("SkillName zero value: Valid() = true, want false")
	}
}

// TestSkillName_ValidShape_LowercaseHyphenated verifies that standard
// lowercase-hyphenated skill names match the CP-049 shape.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestSkillName_ValidShape_LowercaseHyphenated(t *testing.T) {
	t.Parallel()

	valid := []string{
		"beads-cli",
		"a",
		"my-skill",
		"some-longer-skill-name",
		"skill123",
		"skill-v2",
	}
	for _, name := range valid {
		s := core.SkillName(name)
		if !s.ValidShape() {
			t.Errorf("CP-049: SkillName(%q).ValidShape() = false, want true", name)
		}
	}
}

// TestSkillName_ValidShape_WithVersionSuffix verifies that skill names with
// @<version> suffix match the CP-049 shape.
//
// Spec ref: control-points.md §4.11.CP-049 — "optionally suffixed with @<version>."
func TestSkillName_ValidShape_WithVersionSuffix(t *testing.T) {
	t.Parallel()

	valid := []string{
		"beads-cli@1.0.0",
		"my-skill@2.3.4-rc.1",
		"go-tools@v0.1",
		"skill@latest",
	}
	for _, name := range valid {
		s := core.SkillName(name)
		if !s.ValidShape() {
			t.Errorf("CP-049: SkillName(%q).ValidShape() = false, want true (with @version)", name)
		}
	}
}

// TestSkillName_ValidShape_Invalid verifies that names violating the CP-049
// shape are rejected.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestSkillName_ValidShape_Invalid(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",                    // empty
		"CamelCase",           // uppercase
		"UPPER",               // uppercase
		"_underscore",         // starts with underscore
		"1starts-with-digit",  // starts with digit
		"has space",           // contains space
		"has@two@ats@here",    // multiple @ separators
		"@version-only",       // starts with @
		"-starts-with-hyphen", // starts with hyphen
	}
	for _, name := range invalid {
		s := core.SkillName(name)
		if s.ValidShape() {
			t.Errorf("CP-049: SkillName(%q).ValidShape() = true, want false (invalid shape)", name)
		}
	}
}

// TestSkillName_ValidShape_Zero verifies that the zero SkillName fails ValidShape.
//
// Spec ref: control-points.md §4.11.CP-049.
func TestSkillName_ValidShape_Zero(t *testing.T) {
	t.Parallel()

	var s core.SkillName
	if s.ValidShape() {
		t.Error("CP-049: SkillName zero value: ValidShape() = true, want false")
	}
}
