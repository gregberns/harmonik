// hk-guapd: the sandbox profile must never grant write access to a world-shared
// temp root.
//
// THE DEFECT THIS GUARDS. sandboxOSTmpDirs() used to feed os.TempDir() into
// SandboxProfileInput.TmpDirs at both production call sites, and
// GenerateSandboxProfile expands every TmpDirs entry into a RECURSIVE write rule.
// os.TempDir() honours $TMPDIR and falls back to "/tmp" when TMPDIR is UNSET, so
// a daemon started with TMPDIR=/tmp — or with no TMPDIR at all — handed every
// sandboxed run write access to the whole of /tmp.
//
// WHY IT WENT UNSEEN FOR MONTHS, and why this file exists rather than a comment:
// an over-wide allowWrite does not announce itself. It presents as the sandbox
// being BROKEN. TestSandboxAcceptance_WriteToMainDenied_hki0377 failed 3/3 under
// TMPDIR=/tmp and passed 3/3 under a per-user TMPDIR, at load average 7.53 —
// inside the band the "srt fails to apply under fork saturation" theory predicted
// failure in. srt was applying correctly in both columns; the acceptance test's
// own fixture repo sat inside the grant, so the sandbox correctly PERMITTED the
// write the test expected to be denied. The wrong conclusion (a transient srt
// apply-failure) was recorded in a retry loop's log line and read as established
// fact by everyone who came after.
//
// These assertions are cheap and deterministic. The acceptance test that actually
// exercises srt is scenario-tier and load-sensitive; this one is neither, so it is
// the guard that runs on every commit.
package daemon_test

import (
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// worldSharedTempRoots are the temp roots shared by every user and every process
// on the host. Granting any of them recursively defeats confinement outright.
// "/var/tmp" is included because it is the other POSIX shared-temp location and
// is equally a plausible $TMPDIR value, even though this repo has not been seen
// using it.
var worldSharedTempRoots = []string{"/tmp", "/private/tmp", "/var/tmp"}

// TestSandboxProfile_NeverGrantsWorldSharedTempRoot_hkguapd is the regression
// guard. It asserts on the DEFAULT production-shaped input — the one with no
// TmpDirs supplied, which is what both call sites now build — that no
// world-shared temp root appears in allowWrite.
//
// The /tmp/claude and /private/tmp/claude entries (section 6a, hk-cdpxu) are
// expected and are NOT violations: srt injects TMPDIR=/tmp/claude into every
// sandboxed child regardless of the parent's TMPDIR, so that specific
// subdirectory must be writable for TMPDIR-honouring tools to work at all. The
// distinction this test draws is exactly the one that matters — a named
// subdirectory is confinement, the root above it is not.
func TestSandboxProfile_NeverGrantsWorldSharedTempRoot_hkguapd(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	if len(in.TmpDirs) != 0 {
		t.Fatalf("hk-guapd: fixture must supply no TmpDirs (production supplies none); got %v", in.TmpDirs)
	}

	allowWrite := hkguapdAllowWrite(t, in)

	for _, root := range worldSharedTempRoots {
		for _, got := range allowWrite {
			if got == root {
				t.Errorf("hk-guapd: allowWrite grants the world-shared temp root %q. "+
					"srt expands this into a RECURSIVE write rule, so every sandboxed run "+
					"could write every other run's scratch state. Grant a PER-RUN directory "+
					"instead. Full allowWrite: %v", root, allowWrite)
			}
		}
	}
}

// TestSandboxProfile_SrtScratchTmpStillGranted_hkguapd pins the other side of the
// boundary, so a future over-correction that strips the srt scratch dir along with
// the shared root fails loudly here rather than as an ENOENT deep inside a
// sandboxed `go build`. Without this, the obvious "remove all /tmp entries"
// simplification looks correct and silently breaks every TMPDIR-honouring tool.
func TestSandboxProfile_SrtScratchTmpStillGranted_hkguapd(t *testing.T) {
	t.Parallel()

	allowWrite := hkguapdAllowWrite(t, sandboxProfileFixture())

	for _, want := range []string{"/tmp/claude", "/private/tmp/claude"} {
		found := false
		for _, got := range allowWrite {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("hk-guapd: allowWrite lost srt's injected scratch TMPDIR %q (hk-cdpxu). "+
				"srt sets TMPDIR=/tmp/claude in every sandboxed child regardless of the parent "+
				"environment; without this entry any tool that honours TMPDIR fails with ENOENT "+
				"inside the sandbox. Got: %v", want, allowWrite)
		}
	}
}

// TestSandboxProfile_ExplicitSharedTempRootIsStillHonored_hkguapd documents the
// deliberate limit of this fix, so the next reader does not mistake it for a
// guarantee it is not.
//
// GenerateSandboxProfile is a pure generator: it still appends whatever TmpDirs a
// caller passes. The fix removed the AMBIENT feed (os.TempDir() at the two
// production call sites), which is where the defect actually lived; it did not add
// a validation gate. That was deliberate — a hard failure on a shared root would
// fire inside `make check-short` and `make check-race-full`, both of which run
// under TMPDIR=/tmp (Makefile:453, :465), turning a security hardening into a
// broken gating suite. Mitigations that cause the harm they prevent are their own
// failure mode.
//
// So: the profile generator trusts its caller. The guard is that production has no
// caller which supplies a shared root, asserted above. If a future change adds one,
// THIS test still passes and the first test still passes — the protection would
// have to come from review. Stating that plainly here is the point.
func TestSandboxProfile_ExplicitSharedTempRootIsStillHonored_hkguapd(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.TmpDirs = []string{"/tmp"}

	allowWrite := hkguapdAllowWrite(t, in)

	found := false
	for _, got := range allowWrite {
		if got == "/tmp" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hk-guapd: GenerateSandboxProfile silently DROPPED an explicitly-passed "+
			"TmpDirs entry. If that behaviour was added on purpose, this test is now the "+
			"stale one and should be replaced by an assertion that generation FAILS loudly "+
			"— but a silent drop is the worst of the three options, because a caller that "+
			"needs a per-run temp grant would lose it without any signal. Got: %v", allowWrite)
	}
}

// hkguapdAllowWrite generates a profile and returns its allowWrite list.
func hkguapdAllowWrite(t *testing.T, in daemon.SandboxProfileInput) []string {
	t.Helper()

	data, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("hk-guapd: GenerateSandboxProfile: %v", err)
	}

	var parsed struct {
		Filesystem struct {
			AllowWrite []string `json:"allowWrite"`
		} `json:"filesystem"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("hk-guapd: profile is not valid JSON: %v\n%s", err, data)
	}
	if len(parsed.Filesystem.AllowWrite) == 0 {
		t.Fatalf("hk-guapd: filesystem.allowWrite is empty — the profile shape changed and this "+
			"test is no longer reading the field it thinks it is. A silently-empty read here "+
			"would make every assertion in this file vacuously pass, which is the failure mode "+
			"this fixture exists to prevent. Raw profile:\n%s", data)
	}
	return parsed.Filesystem.AllowWrite
}
