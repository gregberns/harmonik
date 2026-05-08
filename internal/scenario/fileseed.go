package scenario

import (
	"encoding/base64"
	"fmt"
	"strconv"
)

// FileSeedEncoding is the encoding discriminator for FileSeed.Contents.
// It declares the two values permitted by specs/scenario-harness.md §6.1.
type FileSeedEncoding string

const (
	// FileSeedEncodingUTF8 indicates that FileSeed.Contents is a plain UTF-8
	// string. This is the default when Encoding is empty or omitted.
	FileSeedEncodingUTF8 FileSeedEncoding = "utf8"

	// FileSeedEncodingBase64 indicates that FileSeed.Contents is a
	// standard-alphabet base64-encoded byte string (base64.StdEncoding).
	FileSeedEncodingBase64 FileSeedEncoding = "base64"
)

// Valid reports whether e is one of the two declared FileSeedEncoding constants
// or the empty string (which callers MUST treat as utf8).
func (e FileSeedEncoding) Valid() bool {
	switch e {
	case FileSeedEncodingUTF8, FileSeedEncodingBase64, "":
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so FileSeedEncoding serialises
// correctly in JSON and YAML definitions.
// The empty string is not a declared spec value and is rejected on marshal.
func (e FileSeedEncoding) MarshalText() ([]byte, error) {
	switch e {
	case FileSeedEncodingUTF8, FileSeedEncodingBase64:
		return []byte(e), nil
	default:
		return nil, fmt.Errorf("fileseedencoding: unknown value %q; must be utf8 or base64", string(e))
	}
}

// UnmarshalText implements encoding.TextUnmarshaler.
// It rejects any value that is not one of the two declared constants.
func (e *FileSeedEncoding) UnmarshalText(text []byte) error {
	v := FileSeedEncoding(text)
	switch v {
	case FileSeedEncodingUTF8, FileSeedEncodingBase64:
		*e = v
		return nil
	default:
		return fmt.Errorf("fileseedencoding: unknown value %q; must be utf8 or base64", string(text))
	}
}

// FileSeed is a single file to seed into the synthetic project root before
// scenario orchestration begins. See specs/scenario-harness.md §6.1.
//
// Default semantics (caller responsibility — the struct does NOT auto-populate):
//   - Encoding == "" is treated as FileSeedEncodingUTF8 by callers.
//   - Mode == "" is treated as "0644" by callers.
type FileSeed struct {
	// Encoding declares how Contents is encoded.
	// "" and "utf8" are both treated as plain UTF-8 by callers.
	// "base64" means Contents is standard-alphabet base64 (base64.StdEncoding).
	// Default (empty): utf8.
	Encoding FileSeedEncoding `json:"encoding,omitempty" yaml:"encoding,omitempty"`

	// Contents holds the file body, encoded per Encoding.
	// Required (may be empty string for an empty file).
	Contents string `json:"contents" yaml:"contents"`

	// Mode is the octal POSIX file mode string (e.g., "0755").
	// Default (empty): "0644". Must be parseable as an octal integer in [0, 0777].
	Mode string `json:"mode,omitempty" yaml:"mode,omitempty"`
}

// Valid reports whether f is structurally well-formed per specs/scenario-harness.md §6.1:
//   - Encoding must be one of: "" (defaults to utf8), "utf8", "base64".
//   - Contents must be valid base64 (base64.StdEncoding) when Encoding == "base64".
//   - Mode, when non-empty, must be parseable as an octal uint32 in [0, 0777].
func (f FileSeed) Valid() bool {
	if !f.Encoding.Valid() {
		return false
	}

	if f.Encoding == FileSeedEncodingBase64 {
		if _, err := base64.StdEncoding.DecodeString(f.Contents); err != nil {
			return false
		}
	}

	if f.Mode != "" {
		v, err := strconv.ParseUint(f.Mode, 8, 32)
		if err != nil {
			return false
		}
		if v > 0o777 {
			return false
		}
	}

	return true
}
