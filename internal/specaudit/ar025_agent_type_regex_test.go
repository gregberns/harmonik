package specaudit_test

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func ar025FixtureArchSpecPath(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("ar025FixtureArchSpecPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "architecture.md")
}

func ar025FixtureExtractRegex(t *testing.T, specPath string) string {
	t.Helper()

	//nolint:gosec // G304: path comes from ar025FixtureArchSpecPath which resolves against the repo's specs/ directory; not user input.
	f, err := os.Open(specPath)
	if err != nil {
		t.Fatalf("ar025FixtureExtractRegex: open %s: %v", specPath, err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("ar025FixtureExtractRegex: close %s: %v", specPath, err)
		}
	}()

	const prefix = "agent_type :="

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, prefix) {
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
