package core_test

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// schemachangekind_hqwn39_test.go — tests for the §6.4 breaking-change
// classification table per EV-030.
//
// Spec ref: event-model.md §4.7 EV-030; §6.4 breaking-change table.
// Bead ref: hk-hqwn.39.

// wantBreaking is the expected IsBreaking() result for each of the nine §6.4
// change-kind rows.
var ev030ChangeKindFixtures = []struct {
	kind       core.SchemaChangeKind
	breaking   bool
	obligation string
}{
	// Row 1: Add optional field → non-breaking.
	{core.SchemaChangeAddOptionalField, false, "Accept; ignore unknown fields on older readers."},
	// Row 2: Add required field → breaking.
	{core.SchemaChangeAddRequiredField, true, "Older readers fail closed with typed error on missing field."},
	// Row 3: Rename field → breaking.
	{core.SchemaChangeRenameField, true, "Migration release; no on-the-fly rewrite."},
	// Row 4: Remove field → breaking.
	{core.SchemaChangeRemoveField, true, "Migration release."},
	// Row 5: Widen type → non-breaking.
	{core.SchemaChangeWidenType, false, "Accept widened values."},
	// Row 6: Narrow type → breaking.
	{core.SchemaChangeNarrowType, true, "Migration release."},
	// Row 7: Add enum variant (non-required semantics) → non-breaking.
	{core.SchemaChangeAddEnumVariant, false, "Older readers see the new variant as unknown; handlers MUST treat unknown variants as non-fatal."},
	// Row 8: Remove enum variant → breaking.
	{core.SchemaChangeRemoveEnumVariant, true, "Migration release."},
	// Row 9: Tighten validation → breaking.
	{core.SchemaChangeTightenValidation, true, "Migration release."},
}

// TestEV030_NineRowsExist verifies that exactly nine SchemaChangeKind constants
// are declared, matching the nine rows of the §6.4 table.
//
// Spec ref: event-model.md §6.4; EV-030 — "9 row classifications".
func TestEV030_NineRowsExist(t *testing.T) {
	t.Parallel()

	const wantRows = 9
	got := len(ev030ChangeKindFixtures)
	if got != wantRows {
		t.Errorf("EV-030 §6.4 table: fixture has %d rows, want %d", got, wantRows)
	}
}

// TestEV030_IsBreaking exercises all nine §6.4 rows per EV-030.
//
// Non-breaking rows: AddOptionalField, WidenType, AddEnumVariant.
// Breaking rows: AddRequiredField, RenameField, RemoveField, NarrowType,
// RemoveEnumVariant, TightenValidation.
func TestEV030_IsBreaking(t *testing.T) {
	t.Parallel()

	for _, fx := range ev030ChangeKindFixtures {
		fx := fx
		t.Run(fx.kind.String(), func(t *testing.T) {
			t.Parallel()

			got := fx.kind.IsBreaking()
			if got != fx.breaking {
				t.Errorf("EV-030: SchemaChangeKind %q IsBreaking() = %v, want %v",
					fx.kind, got, fx.breaking)
			}
		})
	}
}

// TestEV030_ReaderObligation verifies that every §6.4 row carries a non-empty
// reader-obligation string.
func TestEV030_ReaderObligation(t *testing.T) {
	t.Parallel()

	for _, fx := range ev030ChangeKindFixtures {
		fx := fx
		t.Run(fx.kind.String(), func(t *testing.T) {
			t.Parallel()

			got := fx.kind.ReaderObligation()
			if got == "" {
				t.Errorf("EV-030: SchemaChangeKind %q has empty ReaderObligation; all nine rows must declare one",
					fx.kind)
			}
			if got != fx.obligation {
				t.Errorf("EV-030: SchemaChangeKind %q ReaderObligation() = %q, want %q",
					fx.kind, got, fx.obligation)
			}
		})
	}
}

// TestEV030_Valid verifies that every declared constant is reported valid and
// that an unknown value is not.
func TestEV030_Valid(t *testing.T) {
	t.Parallel()

	for _, fx := range ev030ChangeKindFixtures {
		fx := fx
		t.Run(fx.kind.String(), func(t *testing.T) {
			t.Parallel()

			if !fx.kind.Valid() {
				t.Errorf("EV-030: SchemaChangeKind %q Valid() = false, want true", fx.kind)
			}
		})
	}

	t.Run("unknown_value", func(t *testing.T) {
		t.Parallel()

		k := core.SchemaChangeKind("not_a_real_kind")
		if k.Valid() {
			t.Error("EV-030: unknown SchemaChangeKind should not be Valid()")
		}
	})
}

// TestEV030_ParseSchemaChangeKind verifies round-trip parsing for all nine
// constants and rejection of unknown strings.
func TestEV030_ParseSchemaChangeKind(t *testing.T) {
	t.Parallel()

	for _, fx := range ev030ChangeKindFixtures {
		fx := fx
		t.Run(fx.kind.String(), func(t *testing.T) {
			t.Parallel()

			got, err := core.ParseSchemaChangeKind(fx.kind.String())
			if err != nil {
				t.Fatalf("EV-030: ParseSchemaChangeKind(%q) error = %v, want nil", fx.kind, err)
			}
			if got != fx.kind {
				t.Errorf("EV-030: ParseSchemaChangeKind(%q) = %q, want %q", fx.kind, got, fx.kind)
			}
		})
	}

	t.Run("unknown_returns_error", func(t *testing.T) {
		t.Parallel()

		_, err := core.ParseSchemaChangeKind("not_real")
		if err == nil {
			t.Error("EV-030: ParseSchemaChangeKind with unknown value should return error")
		}
		if !errors.Is(err, core.ErrUnknownSchemaChangeKind) {
			t.Errorf("EV-030: ParseSchemaChangeKind error = %v, want wrapping ErrUnknownSchemaChangeKind", err)
		}
	})
}

// TestEV030_AdditiveChangesAreNonBreaking verifies the EV-030 spec claim:
// "Additive changes (new optional field, type widening, new enum variant
// without required-semantics shift) are non-breaking."
func TestEV030_AdditiveChangesAreNonBreaking(t *testing.T) {
	t.Parallel()

	additiveKinds := []core.SchemaChangeKind{
		core.SchemaChangeAddOptionalField,
		core.SchemaChangeWidenType,
		core.SchemaChangeAddEnumVariant,
	}

	for _, k := range additiveKinds {
		k := k
		t.Run(k.String(), func(t *testing.T) {
			t.Parallel()

			if k.IsBreaking() {
				t.Errorf("EV-030: additive change kind %q must not be breaking per §6.4", k)
			}
		})
	}
}

// TestEV030_BreakingChangesRequireMigrationRelease verifies that every breaking
// change kind returns true from IsBreaking().
func TestEV030_BreakingChangesRequireMigrationRelease(t *testing.T) {
	t.Parallel()

	breakingKinds := []core.SchemaChangeKind{
		core.SchemaChangeAddRequiredField,
		core.SchemaChangeRenameField,
		core.SchemaChangeRemoveField,
		core.SchemaChangeNarrowType,
		core.SchemaChangeRemoveEnumVariant,
		core.SchemaChangeTightenValidation,
	}

	for _, k := range breakingKinds {
		k := k
		t.Run(k.String(), func(t *testing.T) {
			t.Parallel()

			if !k.IsBreaking() {
				t.Errorf("EV-030: breaking change kind %q must return IsBreaking() = true per §6.4", k)
			}
		})
	}
}
