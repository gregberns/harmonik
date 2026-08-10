package daemon_test

// verdict_override_unwired_test.go — the tripwire for RC-027's disclosure.
//
// Bead refs: hk-verdict-override-unwired-aqjxo (the operator-facing defect this
// disclosure fixed) and hk-rc027-verdict-override-unbuilt-m09k3 (the remaining
// feature work — connecting the three call sites below).
//
// RC-027's operator surface is complete and unreachable. `harmonik
// confirm-verdict` and `harmonik veto-verdict` are shipped and documented, and
// neither can succeed under any input, because three call sites do not exist:
// nothing calls VerdictConfirmationRegistry.Await, nothing calls ExecuteVerdict,
// and nothing reads a policy through core.PolicyRequiresConfirmation.
//
// Because of that, both CLI files and verdictoverride.go now tell the operator
// the feature is NOT CONNECTED. Those words are a claim about the call graph,
// and a claim about the call graph rots the moment someone changes the call
// graph. The failure this bead records is exactly that shape: verdictoverride.go
// carried an accurate "does not yet call Await" note for long enough that it
// became furniture, and no test ever disagreed with it.
//
// So this file asserts the claim itself. While the three call sites are absent
// the tests pass silently. The moment anyone adds one, they go red and name the
// files whose text must change in the same commit. That makes the disclosure
// self-invalidating rather than another comment waiting to go stale.
//
// # Why a source scan
//
// Absence of a caller is a property of the tree, not of any behavior a running
// test can observe — there is no value to assert and no seam to stub, because
// the whole point is that nothing runs. The repo already uses this idiom for the
// same class of claim: cmd/harmonik/verdict_hint_citations_test.go scans source
// to prove no message cites a verb that does not exist.
//
// The scan covers the WHOLE repository, not just internal/daemon. A production
// caller could reasonably land in cmd/harmonik or a run-loop package, and a
// package-local scan would call that "still unwired" while the CLI text quietly
// went false.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The three call-site patterns. Each matches a CALL and not the declaration:
// `.Await(` requires a receiver dot, which `func (r *Registry) Await(` lacks,
// and the two bare-function patterns are filtered against a `func ` prefix
// below. Doc comments and error strings mention these names without a paren
// ("ExecuteVerdict: lock Release"), so they do not match.
var (
	awaitCallRe   = regexp.MustCompile(`\.Await\(`)
	executeCallRe = regexp.MustCompile(`\bExecuteVerdict\(`)
	policyCallRe  = regexp.MustCompile(`\bPolicyRequiresConfirmation\(`)
)

// disclosureFiles are the operator-facing surfaces whose text asserts the
// feature is not connected. Any tripped tripwire names all of them, because the
// CLI files share one refusal path and one claim.
const disclosureFiles = "cmd/harmonik/confirm_verdict.go, cmd/harmonik/veto_verdict.go, " +
	"cmd/harmonik/usage.go, cmd/harmonik/verdict_unwired_disclosure_test.go, and " +
	"internal/daemon/verdictoverride.go"

// repoRoot walks up from the test's working directory to the directory holding
// go.mod.
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

// productionGoFiles returns every non-test .go file in the repository, as paths
// relative to the root. Test files are excluded: the three call sites all have
// test callers by design, and those are what prove the code works.
func productionGoFiles(t *testing.T) (root string, files []string) {
	t.Helper()
	root = repoRoot(t)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Skip VCS metadata, vendored trees, and fixture trees. testdata may
			// legitimately hold Go files that are not compiled into anything.
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

	// Guard the scan itself. harmonik has hundreds of production Go files, so a
	// small result means the walk broke — and a broken walk would report "no
	// production caller" forever, which is the exact failure this file exists to
	// prevent.
	if len(files) < 100 {
		t.Fatalf("scan found only %d production .go files under %s; the walk is "+
			"broken and must not be allowed to pass vacuously", len(files), root)
	}
	return root, files
}

// scanForCall returns the "file:line: text" of every non-declaration match of re
// across the repository's production Go files.
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
			// A declaration is not a caller.
			if strings.HasPrefix(strings.TrimSpace(line), "func ") {
				continue
			}
			hits = append(hits, rel+":"+itoa(i+1)+": "+strings.TrimSpace(line))
		}
	}
	return hits
}

// itoa avoids pulling strconv in for one call.
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
