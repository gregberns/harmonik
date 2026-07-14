//go:build specaudit

package specaudit_test

// hk-zs0.1 binding test — AR-052 spec-category front-matter.
//
// Spec ref: specs/architecture.md §4.0 AR-052.
//
// AR-052 states: "Every spec under `specs/` MUST declare, in its front matter,
// a `spec-category` of either `runtime-subsystem` or `foundation-cross-cutting`."
//
// # Audit frame
//
// This test is a spec-corpus sensor. It walks every .md file under specs/,
// parses the YAML front-matter block (enclosed in a ```yaml ... ``` fence at the
// top of the file), and asserts two properties per spec:
//
//  1. The `spec-category` field is present.
//  2. Its value is one of the two allowed enum members:
//     `runtime-subsystem` or `foundation-cross-cutting`.
//
// # Supplement exemption
//
// Files whose front matter declares `status: supplement` are sibling-file
// components of a multi-file spec split (e.g. reconciliation/schemas.md).
// They carry schema detail and are not independent specs; they are exempted
// from the spec-category requirement on the grounds that the main spec file
// (e.g. reconciliation/spec.md) already declares the category for the spec
// as a whole. The sensor skips supplement files and logs them.
//
// # Front-matter format
//
// The spec template wraps the YAML block in a Markdown fenced code block:
//
//	```yaml
//	---
//	title: <title>
//	spec-category: <value>
//	...
//	---
//	```
//
// The scanner opens each file, enters the code fence on the first ```yaml
// line, reads until the closing ``` line, and extracts key-value pairs of
// the form `key: value`. It does not use a full YAML parser — the front-matter
// grammar is intentionally kept simple.
//
// # Failure modes
//
//   - spec-category-missing — a non-supplement spec file has no `spec-category:` line.
//   - spec-category-invalid — the `spec-category:` value is not one of the two allowed members.
//
// # Helper prefix
//
// All package-level identifiers in this file use the ar052Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ar052FixtureSpecsDir returns the absolute path to the specs/ directory at the
// repository root.
func ar052FixtureSpecsDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar052FixtureSpecsDir: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar052_spec_category_frontmatter_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs")
}

// ar052FixtureAllowedCategories is the closed set of valid spec-category values
// per specs/architecture.md §4.0 AR-052.
var ar052FixtureAllowedCategories = map[string]bool{
	"runtime-subsystem":        true,
	"foundation-cross-cutting": true,
}

// ar052FixtureFrontMatter holds the parsed key-value fields extracted from a
// spec's YAML front-matter block.
type ar052FixtureFrontMatter struct {
	// fields maps each key to its raw string value (trimmed).
	fields map[string]string
	// found reports whether a front-matter block was present.
	found bool
}

// ar052FixtureParseFrontMatter opens specFile, locates the first ```yaml ... ```
// fenced block, and parses simple `key: value` lines within the YAML document
// delimiters (--- ... ---). It does not require a full YAML parser — the
// front-matter grammar used by specs/ is a flat key-value surface.
func ar052FixtureParseFrontMatter(t *testing.T, specFile string) ar052FixtureFrontMatter {
	t.Helper()

	//nolint:gosec // G304: path comes from ar052FixtureSpecsDir which resolves to repo's specs/ directory; not user input.
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("ar052FixtureParseFrontMatter: open %s: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	result := ar052FixtureFrontMatter{
		fields: make(map[string]string),
	}

	// State machine:
	//   outside    — before the ```yaml fence
	//   in-fence   — inside ```yaml ... ```, before the --- opener
	//   in-yaml    — inside the YAML document (--- ... ---)
	//   done       — after the closing --- of the first YAML document
	const (
		stateOutside = iota
		stateInFence
		stateInYAML
		stateDone
	)

	state := stateOutside
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch state {
		case stateOutside:
			if trimmed == "```yaml" {
				state = stateInFence
			}
		case stateInFence:
			if trimmed == "---" {
				// Opening YAML document delimiter.
				state = stateInYAML
				result.found = true
			} else if trimmed == "```" {
				// Empty fence; give up.
				state = stateDone
			}
		case stateInYAML:
			if trimmed == "---" {
				// Closing document delimiter — done with front matter.
				state = stateDone
			} else if trimmed == "```" {
				// Fence closed without a closing ---; treat as done.
				state = stateDone
			} else {
				// Parse simple `key: value` lines. Multi-line values (e.g.
				// depends-on lists) are not needed; we only care about scalar
				// fields. Lines starting with `-` or containing no `:` are
				// silently ignored.
				if idx := strings.IndexByte(trimmed, ':'); idx > 0 {
					key := strings.TrimSpace(trimmed[:idx])
					val := strings.TrimSpace(trimmed[idx+1:])
					// Only record the first occurrence of each key (YAML spec:
					// duplicate keys are implementation-defined; specs always
					// declare each key once).
					if _, exists := result.fields[key]; !exists {
						result.fields[key] = val
					}
				}
			}
		case stateDone:
			// Nothing further to parse.
		}

		if state == stateDone {
			break
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar052FixtureParseFrontMatter: scan %s: %v", specFile, scanErr)
	}

	return result
}

// TestAR052SpecCategoryFrontMatter is the binding test for hk-zs0.1 (AR-052).
//
// It walks every .md file under specs/ and asserts:
//
//  1. Front-matter block is present.
//  2. `spec-category` field is present in the front matter (unless the file
//     has `status: supplement`, in which case it is skipped).
//  3. The `spec-category` value is one of `runtime-subsystem` or
//     `foundation-cross-cutting`.
func TestAR052SpecCategoryFrontMatter(t *testing.T) {
	t.Parallel()

	specsDir := ar052FixtureSpecsDir(t)

	var specFiles []string
	walkErr := filepath.WalkDir(specsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".md") {
			specFiles = append(specFiles, path)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("TestAR052SpecCategoryFrontMatter: walk %s: %v", specsDir, walkErr)
	}
	if len(specFiles) == 0 {
		t.Fatalf("TestAR052SpecCategoryFrontMatter: no .md files found under %s", specsDir)
	}

	t.Logf("AR-052 sensor: found %d spec file(s) under %s", len(specFiles), specsDir)

	for _, specFile := range specFiles {
		specFile := specFile
		// Use the path relative to specs/ as the test name for readability.
		relPath, relErr := filepath.Rel(specsDir, specFile)
		if relErr != nil {
			relPath = specFile
		}
		t.Run(relPath, func(t *testing.T) {
			t.Parallel()

			fm := ar052FixtureParseFrontMatter(t, specFile)

			if !fm.found {
				t.Errorf(
					"AR-052 front-matter-missing: %s has no YAML front-matter block\n"+
						"  expected: a ```yaml --- ... --- ``` block near the top of the file\n"+
						"  detail:   every spec under specs/ MUST carry a front-matter block "+
						"per the spec template; its absence means the file is either not a "+
						"spec or has been accidentally stripped of its header",
					relPath,
				)
				return
			}

			// Supplement files are exempt from the spec-category requirement.
			// They are sibling components of a multi-file spec split and inherit
			// the category from the primary spec file.
			if status, ok := fm.fields["status"]; ok && status == "supplement" {
				t.Logf("AR-052 supplement-skip: %s has status=supplement; exempt from spec-category requirement", relPath)
				return
			}

			category, present := fm.fields["spec-category"]
			if !present || category == "" {
				t.Errorf(
					"AR-052 spec-category-missing: %s has no `spec-category` field in front matter\n"+
						"  expected: spec-category: runtime-subsystem\n"+
						"     or:    spec-category: foundation-cross-cutting\n"+
						"  detail:   AR-052 requires every spec under specs/ to declare "+
						"spec-category in its front matter; add the field before `spec-shape` "+
						"or `status` to keep the front matter readable",
					relPath,
				)
				return
			}

			if !ar052FixtureAllowedCategories[category] {
				t.Errorf(
					"AR-052 spec-category-invalid: %s declares spec-category=%q which is not "+
						"one of the two allowed values\n"+
						"  allowed: runtime-subsystem, foundation-cross-cutting\n"+
						"  detail:  AR-052 closes the spec-category enum at these two values "+
						"for MVH; any other value (including misspellings) is a hard failure; "+
						"check for trailing whitespace or incorrect hyphenation",
					relPath, category,
				)
			}
		})
	}
}
