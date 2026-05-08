package scenario

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// WorkspacePredicateKind is the predicate-kind discriminator for WorkspacePredicate.
// Spec ref: specs/scenario-harness.md §6.1 — RECORD WorkspacePredicate.
type WorkspacePredicateKind string

// WorkspacePredicateKind values per §6.1 ENUM declaration.
const (
	WorkspacePredicateKindFileExists           WorkspacePredicateKind = "file_exists"
	WorkspacePredicateKindFileContentsEqual    WorkspacePredicateKind = "file_contents_equal"
	WorkspacePredicateKindFileContentsMatch    WorkspacePredicateKind = "file_contents_match"
	WorkspacePredicateKindGitRefAt             WorkspacePredicateKind = "git_ref_at"
	WorkspacePredicateKindCommitTrailerPresent WorkspacePredicateKind = "commit_trailer_present"
)

// Valid reports whether k is one of the five declared WorkspacePredicateKind constants.
func (k WorkspacePredicateKind) Valid() bool {
	switch k {
	case WorkspacePredicateKindFileExists,
		WorkspacePredicateKindFileContentsEqual,
		WorkspacePredicateKindFileContentsMatch,
		WorkspacePredicateKindGitRefAt,
		WorkspacePredicateKindCommitTrailerPresent:
		return true
	default:
		return false
	}
}

// MarshalText implements encoding.TextMarshaler so WorkspacePredicateKind
// serialises correctly in JSON and YAML.
func (k WorkspacePredicateKind) MarshalText() ([]byte, error) {
	if !k.Valid() {
		return nil, fmt.Errorf("workspacepredicate: unknown kind %q", string(k))
	}
	return []byte(k), nil
}

// UnmarshalText implements encoding.TextUnmarshaler. It rejects any value
// that is not one of the five declared constants, ensuring scenario files
// loaded from JSON or YAML cannot smuggle unknown kinds past the boundary.
func (k *WorkspacePredicateKind) UnmarshalText(text []byte) error {
	v := WorkspacePredicateKind(text)
	if !v.Valid() {
		return fmt.Errorf("workspacepredicate: unknown kind %q", string(text))
	}
	*k = v
	return nil
}

// WorkspacePredicate is a declared expectation about workspace state at the
// end of scenario execution: file presence, file contents, git-ref target,
// or commit-trailer presence. Per-kind `Expected` semantics are declared in
// specs/scenario-harness.md §6.3.
//
// Spec ref: specs/scenario-harness.md §6.1 — RECORD WorkspacePredicate.
// Path safety: SH-022 — absolute-path predicates and path traversal are forbidden.
type WorkspacePredicate struct {
	Kind WorkspacePredicateKind `json:"kind" yaml:"kind"`

	// Path is a repo-relative path within the worktree.
	// Absolute paths (leading /) and path traversal (.. segments) are forbidden
	// per SH-022; the same scenario must be portable across operator machines.
	Path string `json:"path" yaml:"path"`

	// Expected holds the per-kind declared expectation. It is *string (not string)
	// because the spec declares String | None and file_exists REQUIRES nil (absence
	// of the field). A pointer cleanly distinguishes "absent" from "empty string";
	// using an empty string for file_exists would conflate the two cases and lose
	// the normative nil-required constraint from §6.3.
	// Per-kind semantics are declared in specs/scenario-harness.md §6.3.
	Expected *string `json:"expected,omitempty" yaml:"expected,omitempty"`

	// Description is an operator-facing label for this assertion.
	// Required (non-empty).
	Description string `json:"description" yaml:"description"`
}

// sha1Re matches a full 40-character lowercase hexadecimal SHA-1.
var sha1Re = regexp.MustCompile(`^[0-9a-f]{40}$`)

// hexOnlyRe matches any non-empty string that looks like hex (used to detect
// short-SHA attempts: hex but fewer than 40 chars is a short-SHA).
var hexOnlyRe = regexp.MustCompile(`^[0-9a-f]+$`)

// validate returns (ok, reason). reason is non-empty iff ok is false, and
// contains an operator-readable explanation of the rejection. Exposed as
// package-private so tests can assert specific rejection paths without
// exporting.
func (p WorkspacePredicate) validate() (ok bool, reason string) {
	// Description must be non-empty.
	if p.Description == "" {
		return false, "description must be non-empty"
	}

	// Kind must be a declared value.
	if !p.Kind.Valid() {
		return false, fmt.Sprintf("unknown kind %q", string(p.Kind))
	}

	// Path safety per SH-022: absolute paths and traversal are forbidden.
	if filepath.IsAbs(p.Path) {
		return false, "path must be repo-relative (absolute path forbidden per SH-022)"
	}
	// Detect Windows-style absolute paths (e.g., C:\...).
	if len(p.Path) >= 3 && p.Path[1] == ':' && (p.Path[2] == '/' || p.Path[2] == '\\') {
		return false, "path must be repo-relative (absolute path forbidden per SH-022)"
	}
	// Detect .. traversal segments using path.Clean (slash-based; portable).
	cleaned := path.Clean(p.Path)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return false, "path must not contain traversal (.. segments forbidden per SH-022)"
	}
	// Also check for raw .. components in the original path string.
	for _, segment := range strings.Split(p.Path, "/") {
		if segment == ".." {
			return false, "path must not contain traversal (.. segments forbidden per SH-022)"
		}
	}

	// Per-kind Expected validation per §6.3.
	switch p.Kind {
	case WorkspacePredicateKindFileExists:
		// §6.3: expected MUST be absent (None); presence of the file is the predicate.
		if p.Expected != nil {
			return false, "file_exists: Expected must be nil (file presence is the sole predicate per §6.3)"
		}

	case WorkspacePredicateKindFileContentsEqual:
		// §6.3: expected is the literal byte-equal contents (UTF-8 string).
		if p.Expected == nil {
			return false, "file_contents_equal: Expected must be non-nil (literal file contents required per §6.3)"
		}

	case WorkspacePredicateKindFileContentsMatch:
		// §6.3: expected is a Go RE2 pattern; must compile.
		if p.Expected == nil {
			return false, "file_contents_match: Expected must be non-nil (RE2 pattern required per §6.3)"
		}
		if _, err := regexp.Compile(*p.Expected); err != nil {
			return false, fmt.Sprintf("file_contents_match: Expected is not a valid RE2 pattern: %v", err)
		}

	case WorkspacePredicateKindGitRefAt:
		// §6.3: expected is a 40-char hex SHA-1 OR a ref name; short-SHA is forbidden.
		if p.Expected == nil {
			return false, "git_ref_at: Expected must be non-nil (SHA-1 or ref name required per §6.3)"
		}
		val := *p.Expected
		if val == "" {
			return false, "git_ref_at: Expected must be non-empty"
		}
		// If it looks like hex (all [0-9a-f]) but is NOT exactly 40 chars, it is a short-SHA.
		if hexOnlyRe.MatchString(val) && !sha1Re.MatchString(val) {
			return false, "git_ref_at: short-SHA forms are forbidden; use full 40-char hex SHA-1 or a ref name (§6.3)"
		}
		// A full 40-char SHA-1 is always valid; ref names are accepted heuristically.
		// No further structural check is applied to ref names (e.g. refs/heads/main, HEAD).

	case WorkspacePredicateKindCommitTrailerPresent:
		// §6.3: expected is the trailer key (e.g., Harmonik-Run-ID); must be non-empty.
		if p.Expected == nil {
			return false, "commit_trailer_present: Expected must be non-nil (trailer key required per §6.3)"
		}
		if *p.Expected == "" {
			return false, "commit_trailer_present: Expected must be non-empty (trailer key must not be blank per §6.3)"
		}
	}

	return true, ""
}

// Valid reports whether the WorkspacePredicate is structurally well-formed
// according to §6.1 (record shape) and §6.3 (per-kind Expected semantics)
// of specs/scenario-harness.md.
//
// Path safety is enforced per SH-022: absolute paths and traversal (..
// segments) are rejected. Kind validation and per-kind Expected constraints
// are enforced as declared in the §6.3 interpretation table.
//
// Valid does NOT perform filesystem I/O, regexp matching against actual file
// contents, or git operations; those are caller responsibilities at assertion-
// evaluation time.
func (p WorkspacePredicate) Valid() bool {
	ok, _ := p.validate()
	return ok
}
