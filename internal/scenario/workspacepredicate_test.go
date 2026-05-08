package scenario

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// workspacePredicateFixtureFileExists returns a valid file_exists predicate.
func workspacePredicateFixtureFileExists(t *testing.T) WorkspacePredicate {
	t.Helper()
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindFileExists,
		Path:        "output/result.json",
		Expected:    nil,
		Description: "result file must exist",
	}
}

// workspacePredicateFixtureFileContentsEqual returns a valid file_contents_equal predicate.
func workspacePredicateFixtureFileContentsEqual(t *testing.T) WorkspacePredicate {
	t.Helper()
	val := "hello world\n"
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindFileContentsEqual,
		Path:        "output/result.txt",
		Expected:    &val,
		Description: "result file must contain expected text",
	}
}

// workspacePredicateFixtureFileContentsMatch returns a valid file_contents_match predicate.
func workspacePredicateFixtureFileContentsMatch(t *testing.T) WorkspacePredicate {
	t.Helper()
	pattern := `^status: (ok|done)$`
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindFileContentsMatch,
		Path:        "output/status.txt",
		Expected:    &pattern,
		Description: "status file must match pattern",
	}
}

// workspacePredicateFixtureGitRefAtSHA returns a valid git_ref_at predicate using a full SHA-1.
func workspacePredicateFixtureGitRefAtSHA(t *testing.T) WorkspacePredicate {
	t.Helper()
	sha := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindGitRefAt,
		Path:        "refs/heads/main",
		Expected:    &sha,
		Description: "main branch must be at expected commit",
	}
}

// workspacePredicateFixtureGitRefAtRef returns a valid git_ref_at predicate using a ref name.
func workspacePredicateFixtureGitRefAtRef(t *testing.T) WorkspacePredicate {
	t.Helper()
	ref := "refs/heads/main"
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindGitRefAt,
		Path:        "refs/heads/feature",
		Expected:    &ref,
		Description: "feature branch must point to main",
	}
}

// workspacePredicateFixtureCommitTrailerPresent returns a valid commit_trailer_present predicate.
func workspacePredicateFixtureCommitTrailerPresent(t *testing.T) WorkspacePredicate {
	t.Helper()
	key := "Harmonik-Run-ID"
	return WorkspacePredicate{
		Kind:        WorkspacePredicateKindCommitTrailerPresent,
		Path:        "refs/heads/main",
		Expected:    &key,
		Description: "HEAD commit must carry Harmonik-Run-ID trailer",
	}
}

// workspacePredicateFixtureStrPtr is a helper to take address of a string literal.
func workspacePredicateFixtureStrPtr(s string) *string {
	return &s
}

func TestWorkspacePredicateKindValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input WorkspacePredicateKind
		want  bool
	}{
		{"file_exists", WorkspacePredicateKindFileExists, true},
		{"file_contents_equal", WorkspacePredicateKindFileContentsEqual, true},
		{"file_contents_match", WorkspacePredicateKindFileContentsMatch, true},
		{"git_ref_at", WorkspacePredicateKindGitRefAt, true},
		{"commit_trailer_present", WorkspacePredicateKindCommitTrailerPresent, true},
		{"empty string", WorkspacePredicateKind(""), false},
		{"unknown value", WorkspacePredicateKind("file_missing"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("WorkspacePredicateKind(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestWorkspacePredicateKindMarshalText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   WorkspacePredicateKind
		wantErr bool
	}{
		{"file_exists", WorkspacePredicateKindFileExists, false},
		{"file_contents_equal", WorkspacePredicateKindFileContentsEqual, false},
		{"file_contents_match", WorkspacePredicateKindFileContentsMatch, false},
		{"git_ref_at", WorkspacePredicateKindGitRefAt, false},
		{"commit_trailer_present", WorkspacePredicateKindCommitTrailerPresent, false},
		{"invalid kind", WorkspacePredicateKind("bogus"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data, err := tc.input.MarshalText()
			if tc.wantErr {
				if err == nil {
					t.Errorf("MarshalText() expected error for %q, got nil", string(tc.input))
				}
				return
			}
			if err != nil {
				t.Fatalf("MarshalText() unexpected error: %v", err)
			}
			if string(data) != string(tc.input) {
				t.Errorf("MarshalText() = %q, want %q", string(data), string(tc.input))
			}
		})
	}
}

func TestWorkspacePredicateValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		input      WorkspacePredicate
		wantValid  bool
		wantReason string // substring that must appear in rejection reason if wantValid=false
	}{
		// file_exists
		{
			name:      "file_exists: nil expected → valid",
			input:     workspacePredicateFixtureFileExists(t),
			wantValid: true,
		},
		{
			name: "file_exists: non-nil expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindFileExists,
				Path:        "some/file.txt",
				Expected:    workspacePredicateFixtureStrPtr("x"),
				Description: "should be invalid",
			},
			wantValid:  false,
			wantReason: "file_exists",
		},

		// file_contents_equal
		{
			name:      "file_contents_equal: non-nil expected → valid",
			input:     workspacePredicateFixtureFileContentsEqual(t),
			wantValid: true,
		},
		{
			name: "file_contents_equal: nil expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindFileContentsEqual,
				Path:        "some/file.txt",
				Expected:    nil,
				Description: "should be invalid",
			},
			wantValid:  false,
			wantReason: "file_contents_equal",
		},

		// file_contents_match
		{
			name:      "file_contents_match: valid regex → valid",
			input:     workspacePredicateFixtureFileContentsMatch(t),
			wantValid: true,
		},
		{
			name: "file_contents_match: invalid RE2 pattern → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindFileContentsMatch,
				Path:        "some/file.txt",
				Expected:    workspacePredicateFixtureStrPtr("[unclosed"),
				Description: "bad regex",
			},
			wantValid:  false,
			wantReason: "valid RE2 pattern",
		},
		{
			name: "file_contents_match: nil expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindFileContentsMatch,
				Path:        "some/file.txt",
				Expected:    nil,
				Description: "nil pattern",
			},
			wantValid:  false,
			wantReason: "file_contents_match",
		},

		// git_ref_at
		{
			name:      "git_ref_at: full 40-char SHA → valid",
			input:     workspacePredicateFixtureGitRefAtSHA(t),
			wantValid: true,
		},
		{
			name:      "git_ref_at: refs/heads/main ref name → valid",
			input:     workspacePredicateFixtureGitRefAtRef(t),
			wantValid: true,
		},
		{
			name: "git_ref_at: HEAD ref name → valid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindGitRefAt,
				Path:        "refs/heads/main",
				Expected:    workspacePredicateFixtureStrPtr("HEAD"),
				Description: "HEAD is a valid ref name",
			},
			wantValid: true,
		},
		{
			name: "git_ref_at: short-SHA (hex < 40 chars) → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindGitRefAt,
				Path:        "refs/heads/main",
				Expected:    workspacePredicateFixtureStrPtr("abc123"),
				Description: "short SHA forbidden",
			},
			wantValid:  false,
			wantReason: "short-SHA",
		},
		{
			name: "git_ref_at: nil expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindGitRefAt,
				Path:        "refs/heads/main",
				Expected:    nil,
				Description: "nil sha",
			},
			wantValid:  false,
			wantReason: "git_ref_at",
		},

		// commit_trailer_present
		{
			name:      "commit_trailer_present: non-empty key → valid",
			input:     workspacePredicateFixtureCommitTrailerPresent(t),
			wantValid: true,
		},
		{
			name: "commit_trailer_present: empty string expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindCommitTrailerPresent,
				Path:        "refs/heads/main",
				Expected:    workspacePredicateFixtureStrPtr(""),
				Description: "empty trailer key",
			},
			wantValid:  false,
			wantReason: "commit_trailer_present",
		},
		{
			name: "commit_trailer_present: nil expected → invalid",
			input: WorkspacePredicate{
				Kind:        WorkspacePredicateKindCommitTrailerPresent,
				Path:        "refs/heads/main",
				Expected:    nil,
				Description: "nil trailer key",
			},
			wantValid:  false,
			wantReason: "commit_trailer_present",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, reason := tc.input.validate()
			if ok != tc.wantValid {
				t.Errorf("validate() ok = %v, want %v; reason = %q", ok, tc.wantValid, reason)
			}
			if !tc.wantValid && tc.wantReason != "" {
				if !strings.Contains(reason, tc.wantReason) {
					t.Errorf("validate() reason = %q, want it to contain %q", reason, tc.wantReason)
				}
			}
		})
	}
}

func TestWorkspacePredicatePathSafety(t *testing.T) {
	t.Parallel()

	makeBase := func(path string) WorkspacePredicate {
		return WorkspacePredicate{
			Kind:        WorkspacePredicateKindFileExists,
			Path:        path,
			Expected:    nil,
			Description: "path safety test",
		}
	}

	tests := []struct {
		name      string
		path      string
		wantValid bool
		wantHint  string
	}{
		{
			name:      "valid relative path",
			path:      "subdir/file.txt",
			wantValid: true,
		},
		{
			name:      "valid single-segment path",
			path:      "file.txt",
			wantValid: true,
		},
		{
			name:      "absolute path rejected",
			path:      "/etc/passwd",
			wantValid: false,
			wantHint:  "absolute",
		},
		{
			name:      "traversal path rejected",
			path:      "../escape/file.txt",
			wantValid: false,
			wantHint:  "traversal",
		},
		{
			name:      "mid-path traversal rejected",
			path:      "a/../../etc/passwd",
			wantValid: false,
			wantHint:  "traversal",
		},
		{
			name:      "double-dot only rejected",
			path:      "..",
			wantValid: false,
			wantHint:  "traversal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pred := makeBase(tc.path)
			ok, reason := pred.validate()
			if ok != tc.wantValid {
				t.Errorf("validate() ok = %v, want %v; reason = %q", ok, tc.wantValid, reason)
			}
			if !tc.wantValid && tc.wantHint != "" {
				if !strings.Contains(reason, tc.wantHint) {
					t.Errorf("validate() reason = %q, want it to contain %q", reason, tc.wantHint)
				}
			}
		})
	}
}

func TestWorkspacePredicateDescriptionRequired(t *testing.T) {
	t.Parallel()

	pred := WorkspacePredicate{
		Kind:        WorkspacePredicateKindFileExists,
		Path:        "some/file.txt",
		Expected:    nil,
		Description: "",
	}

	ok, reason := pred.validate()
	if ok {
		t.Fatal("validate() returned true for empty description, want false")
	}
	if !strings.Contains(reason, "description") {
		t.Errorf("validate() reason = %q, want it to contain %q", reason, "description")
	}
}

func TestWorkspacePredicateJSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input WorkspacePredicate
	}{
		{
			name:  "file_exists",
			input: workspacePredicateFixtureFileExists(t),
		},
		{
			name:  "file_contents_equal",
			input: workspacePredicateFixtureFileContentsEqual(t),
		},
		{
			name:  "file_contents_match",
			input: workspacePredicateFixtureFileContentsMatch(t),
		},
		{
			name:  "git_ref_at full SHA",
			input: workspacePredicateFixtureGitRefAtSHA(t),
		},
		{
			name:  "git_ref_at ref name",
			input: workspacePredicateFixtureGitRefAtRef(t),
		},
		{
			name:  "commit_trailer_present",
			input: workspacePredicateFixtureCommitTrailerPresent(t),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got WorkspacePredicate
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if !reflect.DeepEqual(tc.input, got) {
				t.Errorf("round-trip mismatch:\n  in:  %+v\n  out: %+v", tc.input, got)
			}
		})
	}
}

func TestWorkspacePredicateJSONExpectedOmitEmpty(t *testing.T) {
	t.Parallel()

	// When Expected is nil (file_exists), the marshaled JSON MUST NOT contain
	// the "expected" key (omitempty contract on the struct tag).
	pred := workspacePredicateFixtureFileExists(t)
	data, err := json.Marshal(pred)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	if _, ok := raw["expected"]; ok {
		t.Errorf("marshaled JSON contains 'expected' key when Expected is nil; got %s", data)
	}
}
