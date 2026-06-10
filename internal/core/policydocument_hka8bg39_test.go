package core

import (
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// CP-038: Policy schema version is per-document and N-1 readable
// specs/control-points.md §4.7.CP-038; operator-nfr.md §4.5.ON-018
// ---------------------------------------------------------------------------

// TestValidateSchemaVersion_Positive verifies that a policy document with a
// positive schema_version passes ValidateSchemaVersion (CP-038).
func TestValidateSchemaVersion_Positive(t *testing.T) {
	t.Parallel()

	for _, sv := range []int{1, 2, 10, 100} {
		sv := sv
		t.Run("schema_version_"+itoa(sv), func(t *testing.T) {
			t.Parallel()

			doc := PolicyDocument{Metadata: PolicyDocumentMeta{SchemaVersion: sv}}
			if err := doc.ValidateSchemaVersion(); err != nil {
				t.Errorf("ValidateSchemaVersion(%d) = %v, want nil", sv, err)
			}
		})
	}
}

// TestValidateSchemaVersion_ZeroRejected verifies that schema_version == 0
// (field absent from YAML or explicitly zero) is rejected (CP-038: must be
// positive).
func TestValidateSchemaVersion_ZeroRejected(t *testing.T) {
	t.Parallel()

	doc := PolicyDocument{Metadata: PolicyDocumentMeta{SchemaVersion: 0}}
	err := doc.ValidateSchemaVersion()
	if err == nil {
		t.Fatal("ValidateSchemaVersion(0) = nil, want ErrInvalidPolicySchemaVersion")
	}
	if !errors.Is(err, ErrInvalidPolicySchemaVersion) {
		t.Errorf("ValidateSchemaVersion(0) error = %v, want errors.Is(ErrInvalidPolicySchemaVersion)", err)
	}
}

// TestValidateSchemaVersion_NegativeRejected verifies that a negative
// schema_version is rejected (CP-038: must be positive).
func TestValidateSchemaVersion_NegativeRejected(t *testing.T) {
	t.Parallel()

	doc := PolicyDocument{Metadata: PolicyDocumentMeta{SchemaVersion: -1}}
	err := doc.ValidateSchemaVersion()
	if err == nil {
		t.Fatal("ValidateSchemaVersion(-1) = nil, want ErrInvalidPolicySchemaVersion")
	}
	if !errors.Is(err, ErrInvalidPolicySchemaVersion) {
		t.Errorf("ValidateSchemaVersion(-1) error = %v, want errors.Is(ErrInvalidPolicySchemaVersion)", err)
	}
}

// TestValidateSchemaVersion_ErrorMessageContainsValue verifies that the error
// message produced by ValidateSchemaVersion includes the offending value, so
// operators can diagnose missing or malformed schema_version fields.
func TestValidateSchemaVersion_ErrorMessageContainsValue(t *testing.T) {
	t.Parallel()

	doc := PolicyDocument{Metadata: PolicyDocumentMeta{SchemaVersion: 0}}
	err := doc.ValidateSchemaVersion()
	if err == nil {
		t.Fatal("ValidateSchemaVersion(0) = nil, want error")
	}
	msg := err.Error()
	if !strContains(msg, "0") {
		t.Errorf("ValidateSchemaVersion(0) error = %q, want offending value 0 in message", msg)
	}
}

// TestPolicyDocumentAcceptsSchemaVersion_CurrentAccepted verifies that a
// reader at version N accepts a document written at the same version N.
func TestPolicyDocumentAcceptsSchemaVersion_CurrentAccepted(t *testing.T) {
	t.Parallel()

	const readerVersion = 2
	if !PolicyDocumentAcceptsSchemaVersion(readerVersion, readerVersion) {
		t.Errorf("PolicyDocumentAcceptsSchemaVersion(%d, %d) = false, want true (same version)", readerVersion, readerVersion)
	}
}

// TestPolicyDocumentAcceptsSchemaVersion_NMinus1Accepted verifies that a
// reader at version N accepts a document written at version N-1 (the prior
// schema version), per §4.7.CP-038 and operator-nfr.md §4.5.ON-018.
func TestPolicyDocumentAcceptsSchemaVersion_NMinus1Accepted(t *testing.T) {
	t.Parallel()

	const readerVersion = 2
	const priorVersion = readerVersion - 1
	if !PolicyDocumentAcceptsSchemaVersion(priorVersion, readerVersion) {
		t.Errorf("PolicyDocumentAcceptsSchemaVersion(%d, %d) = false, want true (N-1 compat window)", priorVersion, readerVersion)
	}
}

// TestPolicyDocumentAcceptsSchemaVersion_NMinus2Rejected verifies that a
// reader at version N rejects a document written at version N-2 (too old;
// migration release required per ON-019).
func TestPolicyDocumentAcceptsSchemaVersion_NMinus2Rejected(t *testing.T) {
	t.Parallel()

	const readerVersion = 3
	const tooOldVersion = readerVersion - 2
	if PolicyDocumentAcceptsSchemaVersion(tooOldVersion, readerVersion) {
		t.Errorf("PolicyDocumentAcceptsSchemaVersion(%d, %d) = true, want false (N-2 is beyond compat window)", tooOldVersion, readerVersion)
	}
}

// TestPolicyDocumentAcceptsSchemaVersion_FutureAccepted verifies that a
// reader at version N accepts a document written at a higher version (N+1,
// N+2), treating unknown fields as non-fatal per ON-018 additive-only rule.
func TestPolicyDocumentAcceptsSchemaVersion_FutureAccepted(t *testing.T) {
	t.Parallel()

	const readerVersion = 2
	for _, futureVersion := range []int{readerVersion + 1, readerVersion + 2, readerVersion + 10} {
		futureVersion := futureVersion
		t.Run("future_"+itoa(futureVersion), func(t *testing.T) {
			t.Parallel()

			if !PolicyDocumentAcceptsSchemaVersion(futureVersion, readerVersion) {
				t.Errorf("PolicyDocumentAcceptsSchemaVersion(%d, %d) = false, want true (future version; additive-only)", futureVersion, readerVersion)
			}
		})
	}
}

// TestRegisterFromDocument_RejectsZeroSchemaVersion verifies that
// RegisterFromDocument rejects a policy document with schema_version == 0,
// which indicates the field was absent from the YAML source (CP-038).
func TestRegisterFromDocument_RejectsZeroSchemaVersion(t *testing.T) {
	t.Parallel()

	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{
			Name:          "zero-sv-policy",
			Version:       "1.0.0",
			Author:        "test",
			SchemaVersion: 0, // invalid: absent or explicitly zero
		},
	})

	reg := NewS02Registrar()
	err := reg.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("RegisterFromDocument with schema_version=0: got nil, want ErrInvalidPolicySchemaVersion")
	}
	if !errors.Is(err, ErrInvalidPolicySchemaVersion) {
		t.Errorf("RegisterFromDocument with schema_version=0: error = %v, want errors.Is(ErrInvalidPolicySchemaVersion)", err)
	}
}

// TestRegisterFromDocument_RejectsNegativeSchemaVersion verifies that
// RegisterFromDocument rejects a policy document with a negative schema_version
// (CP-038: must be positive).
func TestRegisterFromDocument_RejectsNegativeSchemaVersion(t *testing.T) {
	t.Parallel()

	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{
			Name:          "neg-sv-policy",
			Version:       "1.0.0",
			Author:        "test",
			SchemaVersion: -5,
		},
	})

	reg := NewS02Registrar()
	err := reg.RegisterFromDocument(doc)
	if err == nil {
		t.Fatal("RegisterFromDocument with schema_version=-5: got nil, want ErrInvalidPolicySchemaVersion")
	}
	if !errors.Is(err, ErrInvalidPolicySchemaVersion) {
		t.Errorf("RegisterFromDocument with schema_version=-5: error = %v, want errors.Is(ErrInvalidPolicySchemaVersion)", err)
	}
}

// TestRegisterFromDocument_AcceptsPositiveSchemaVersion verifies that
// RegisterFromDocument accepts a policy document with a valid positive
// schema_version (CP-038).
func TestRegisterFromDocument_AcceptsPositiveSchemaVersion(t *testing.T) {
	t.Parallel()

	doc := s02MarkAllSectionsPresent(PolicyDocument{
		Metadata: PolicyDocumentMeta{
			Name:          "valid-sv-policy",
			Version:       "1.0.0",
			Author:        "test",
			SchemaVersion: 1,
		},
	})

	reg := NewS02Registrar()
	err := reg.RegisterFromDocument(doc)
	if err != nil {
		t.Errorf("RegisterFromDocument with schema_version=1: got %v, want nil", err)
	}
}

// TestParseAndValidateSchemaVersion_ZeroInYAML verifies end-to-end that a
// policy YAML without a schema_version field (which YAML unmarshals to 0) is
// rejected by ValidateSchemaVersion (CP-038).
func TestParseAndValidateSchemaVersion_ZeroInYAML(t *testing.T) {
	t.Parallel()

	// schema_version is absent from the metadata block.
	data := []byte(`
metadata:
  name: no-schema-version
  version: "0.1.0"
  author: test

roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}

	// SchemaVersion field defaults to 0 when absent.
	if doc.Metadata.SchemaVersion != 0 {
		t.Fatalf("expected SchemaVersion=0 for absent field, got %d", doc.Metadata.SchemaVersion)
	}

	err = doc.ValidateSchemaVersion()
	if err == nil {
		t.Fatal("ValidateSchemaVersion() = nil, want ErrInvalidPolicySchemaVersion for absent schema_version")
	}
	if !errors.Is(err, ErrInvalidPolicySchemaVersion) {
		t.Errorf("ValidateSchemaVersion() error = %v, want errors.Is(ErrInvalidPolicySchemaVersion)", err)
	}
}

// TestParseAndValidateSchemaVersion_OneInYAML verifies end-to-end that a
// policy YAML with schema_version: 1 passes ValidateSchemaVersion (CP-038).
func TestParseAndValidateSchemaVersion_OneInYAML(t *testing.T) {
	t.Parallel()

	data := []byte(`
metadata:
  name: with-schema-version
  version: "0.1.0"
  author: test
  schema_version: 1

roles: []
freedom_profiles: []
gates: []
hooks: []
guards: []
budgets: []
`)
	doc, err := ParsePolicyDocument(data)
	if err != nil {
		t.Fatalf("ParsePolicyDocument: %v", err)
	}
	if err := doc.ValidateSchemaVersion(); err != nil {
		t.Errorf("ValidateSchemaVersion() = %v, want nil for schema_version=1", err)
	}
}
