package daemon_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	awaitCallRe   = regexp.MustCompile(`\.Await\(`)
	executeCallRe = regexp.MustCompile(`\bExecuteVerdict\(`)
	policyCallRe  = regexp.MustCompile(`\bPolicyRequiresConfirmation\(`)
)

const disclosureFiles = "cmd/harmonik/confirm_verdict.go, cmd/harmonik/veto_verdict.go, " +
	"cmd/harmonik/usage.go, cmd/harmonik/verdict_unwired_disclosure_test.go, and " +
	"internal/daemon/verdictoverride.go"

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found walking up from the test working directory; "+
				"the scan cannot locate the repository root and must not pass vacuously "+
				"(started at %s)", dir)
		}
		dir = parent
	}
}

func productionGoFiles(t *testing.T) (root string, files []string) {
	t.Helper()
	root = repoRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "testdata", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	if len(files) < 100 {
		t.Fatalf("scan found only %d production .go files under %s; the walk is "+
			"broken and must not be allowed to pass vacuously", len(files), root)
	}
	return root, files
}

func scanForCall(t *testing.T, re *regexp.Regexp) []string {
	t.Helper()
	root, files := productionGoFiles(t)

	var hits []string
	for _, rel := range files {
		// #nosec G304 -- paths come from a WalkDir of the repo, not from input.
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if !re.MatchString(line) {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "func ") {
				continue
			}
			hits = append(hits, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
		}
	}
	return hits
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestAwaitHasNoProductionCaller fails when a run can park awaiting an operator
// decision. That is good news for the feature and it makes the CLI text false,
// so the text must change in the same commit.
func TestAwaitHasNoProductionCaller(t *testing.T) {
	hits := scanForCall(t, awaitCallRe)
	if len(hits) > 0 {
		t.Fatalf("VerdictConfirmationRegistry.Await now has %d production caller(s):\n\t%s\n\n"+
			"This is not a defect — it means RC-027 is being connected. But the "+
			"operator surfaces still say the feature is NOT CONNECTED, which is now "+
			"false. Remove that disclosure from %s, and delete this test file once "+
			"all three call sites exist.",
			len(hits), strings.Join(hits, "\n\t"), disclosureFiles)
	}
}

// TestExecuteVerdictHasNoProductionCaller guards the second gap. It matters on
// its own: wiring Await while ExecuteVerdict stays dead would leave the commands
// exactly as unreachable as they are now, and is the mistake the bead's own
// framing ("only the executor-side Await call remains") invites.
func TestExecuteVerdictHasNoProductionCaller(t *testing.T) {
	hits := scanForCall(t, executeCallRe)
	if len(hits) > 0 {
		t.Fatalf("ExecuteVerdict now has %d production caller(s):\n\t%s\n\n"+
			"The verdict-executor is reachable, so there is now a verdict-execution "+
			"step that RC-027 could pause. Re-check the NOT CONNECTED text in %s, and "+
			"drop scripts/reachability.baseline's ExecuteVerdict entry if the "+
			"reachability gate now sees it.",
			len(hits), strings.Join(hits, "\n\t"), disclosureFiles)
	}
}

// TestPolicyRequiresConfirmationHasNoProductionCaller guards the third gap: no
// policy's confirm_required field is ever read, so even a reachable executor
// would never choose to pause.
func TestPolicyRequiresConfirmationHasNoProductionCaller(t *testing.T) {
	hits := scanForCall(t, policyCallRe)
	if len(hits) > 0 {
		t.Fatalf("core.PolicyRequiresConfirmation now has %d production caller(s):\n\t%s\n\n"+
			"Some production path now reads a policy's confirm_required field. Re-check "+
			"the NOT CONNECTED text in %s.",
			len(hits), strings.Join(hits, "\n\t"), disclosureFiles)
	}
}
