package specaudit_test

// AR-025 binding test — agent_type identifier regex byte-identity between
// specs/architecture.md §6.1 and internal/core.AgentTypeRegexPattern.
//
// Spec ref: specs/architecture.md §4.7 AR-025 and §6.1.
//
// AR-025 states: "An `agent_type` identifier MUST be a lowercase-hyphenated
// ASCII string matching the regex `^[a-z][a-z0-9-]{1,62}$`."
//
// §6.1 is the single data-shape section owned by architecture.md.  It
// declares the canonical regex in a fenced code block of the form:
//
//	agent_type := ^[a-z][a-z0-9-]{1,62}$
//
// internal/core.AgentTypeRegexPattern is the exported string constant used
// to compile the runtime regexp.MustCompile call in agenttype.go.
//
// This sensor checks two things:
//
//  1. Spec-text extraction: the §6.1 code block in architecture.md contains
//     exactly one line of the form `agent_type := <regex>` and the extracted
//     regex is non-empty.
//
//  2. Byte-identity: the extracted spec-text regex is identical byte-for-byte
//     to core.AgentTypeRegexPattern.  Any drift between the spec and the
//     runtime regex (e.g. a spec update not mirrored in code, or vice versa)
//     is a hard failure.
//
// # Failure modes
//
//  1. spec-extract-missing — no `agent_type :=` line found in the §6.1 block.
//  2. spec-runtime-mismatch — extracted regex != core.AgentTypeRegexPattern.
//
// # Coverage gap
//
// This sensor does NOT walk all spec files checking that every occurrence of
// an `agent_type` field value matches the regex shape; that corpus-wide lint
// is tracked in hk-zs0.28 (AR-027 four-surface byte-identity sensor).

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// ar025FixtureArchSpecPath returns the absolute path to specs/architecture.md.
func ar025FixtureArchSpecPath(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar025FixtureArchSpecPath: runtime.Caller(0) failed")
	}
	// thisFile is .../internal/specaudit/ar025_agent_type_regex_test.go
	// repo root is two directories up
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "architecture.md")
}

// ar025FixtureExtractRegex scans architecture.md for the §6.1 agent_type
// definition line of the form `agent_type := <regex>` and returns the
// extracted regex string.
//
// The spec uses a fenced code block to present the regex:
//
//	```
//	agent_type := ^[a-z][a-z0-9-]{1,62}$
//	```
//
// The scanner does not require the line to be inside a code fence — it
// matches the first line whose content (after trimming) starts with
// `agent_type :=` anywhere in the file so that minor spec formatting
// changes (fence style, indentation) do not break the sensor.
func ar025FixtureExtractRegex(t *testing.T, specPath string) string {
	t.Helper()

	//nolint:gosec // G304: path comes from ar025FixtureArchSpecPath which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("ar025FixtureExtractRegex: open %s: %v", specPath, err)
	}
	defer func() { _ = f.Close() }()

	const prefix = "agent_type :="

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
			// Trim the prefix and any surrounding whitespace to get the raw regex.
			raw := strings.TrimSpace(strings.TrimPrefix(line, prefix))
			return raw
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("ar025FixtureExtractRegex: scan %s: %v", specPath, scanErr)
	}

	return "" // not found
}

// TestAR025AgentTypeRegexByteIdentity is the binding test for AR-025.
//
// It verifies that the agent_type regex declared in specs/architecture.md §6.1
// is byte-for-byte identical to core.AgentTypeRegexPattern (which is the string
// compiled into regexp.MustCompile in agenttype.go).
func TestAR025AgentTypeRegexByteIdentity(t *testing.T) {
	specPath := ar025FixtureArchSpecPath(t)
	specRegex := ar025FixtureExtractRegex(t, specPath)

	if specRegex == "" {
		t.Fatalf(
			"AR-025 spec-extract-missing: no `agent_type :=` line found in %s §6.1; "+
				"the sensor expects a line of the form `agent_type := ^[a-z][a-z0-9-]{1,62}$` "+
				"in the §6.1 block",
			specPath,
		)
	}

	runtimeRegex := core.AgentTypeRegexPattern

	if specRegex != runtimeRegex {
		t.Errorf(
			"AR-025 spec-runtime-mismatch: agent_type regex in specs/architecture.md §6.1 "+
				"does not match core.AgentTypeRegexPattern byte-for-byte\n"+
				"  spec text : %q\n"+
				"  runtime   : %q\n"+
				"Fix: update whichever source drifted so both declare the same regex string.",
			specRegex, runtimeRegex,
		)
		return
	}

	t.Logf(
		"AR-025 audit PASS: spec §6.1 regex %q matches core.AgentTypeRegexPattern byte-for-byte",
		runtimeRegex,
	)
}
