package core

import (
	"fmt"
)

// ProjectHash is the stable project-scoped identifier derived from the
// project root path.  It is the first 12 lowercase hexadecimal characters
// of SHA-256(realpath(project_root)).
//
// ProjectHash is a named type (not a Go alias) so that ProjectHash and other
// identifier types are not interchangeable at compile time.
// The value is always exactly 12 lowercase hex characters (6 SHA-256 bytes).
//
// Spec ref: process-lifecycle.md §6.1 ProjectHash; §4.2 PL-006a — "first 12
// hexadecimal characters of SHA-256(realpath(project_root))."
type ProjectHash string

// String returns the 12-character lowercase hex string value of the hash.
func (h ProjectHash) String() string {
	return string(h)
}

// MarshalText implements encoding.TextMarshaler.
// The output is the 12-character lowercase hex string.
func (h ProjectHash) MarshalText() ([]byte, error) {
	return []byte(h), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It accepts a 12-character lowercase hexadecimal string.
// Returns an error if the input is not exactly 12 lowercase hex characters.
func (h *ProjectHash) UnmarshalText(data []byte) error {
	s := string(data)
	if len(s) != 12 {
		return fmt.Errorf("core: ProjectHash: want 12 hex chars, got %d: %q", len(s), s)
	}
	for i, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return fmt.Errorf("core: ProjectHash: non-lowercase-hex char %q at position %d in %q", c, i, s)
		}
	}
	*h = ProjectHash(s)
	return nil
}
