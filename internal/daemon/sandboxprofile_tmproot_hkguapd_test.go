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

// TestSandboxProfile_NeverGrantsWorldSharedTempRoot_hkguapd asserts that with no
// TmpDirs supplied — the shape both production call sites now build — the
// generator itself hardcodes no world-shared temp root.
//
// BE CLEAR ABOUT WHAT THIS DOES AND DOES NOT CATCH. It reads a test-local
// fixture, so the tie between it and production is prose, not code; a call site
// that started supplying a shared root again would not fail here. It is near
// tautological on its own — with TmpDirs nil, a shared root can only appear if
// the generator hardcodes one. It earns its place by catching the OVER-correction
// (someone adding "/tmp" to section 6a instead of "/tmp/claude"), which nothing
// else covers. The actual regression guard for hk-guapd is
// TestSandboxProfile_SharedTempRootIsRejected_hkguapd below.
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

// TestSandboxProfile_SharedTempRootIsRejected_hkguapd is the test that actually
// binds the defect. The two above assert on a test-local fixture; nothing in them
// would fail if someone re-added `TmpDirs: sandboxOSTmpDirs()` to a call site
// tomorrow, because the defect lived at the CALL SITES, not in the generator's
// literal output. The correspondence between "what the fixture builds" and "what
// production builds" would be maintained by prose alone — which is the pattern
// that let hk-guapd survive in the first place.
//
// The gate closes that. A world-shared root in TmpDirs is now a launch-time error,
// so reintroducing the ambient feed fails loudly at the point of use instead of
// silently restoring the over-grant. It fails CLOSED, matching the five validators
// GenerateSandboxProfile already applies to its other inputs.
//
// It is inert in production: every caller supplies nil. The only thing it can
// break is a caller that passes a shared root, which is exactly the thing.
func TestSandboxProfile_SharedTempRootIsRejected_hkguapd(t *testing.T) {
	t.Parallel()

	// Unnormalised forms are included deliberately: exact string comparison
	// alone would let "/tmp/" and "/tmp/../tmp" through, and a gate defeated by
	// a trailing slash is worse than none because it reads as protection.
	for _, root := range []string{"/tmp", "/tmp/", "/tmp/../tmp", "/private/tmp", "/var/tmp", "/"} {
		in := sandboxProfileFixture()
		in.TmpDirs = []string{root}

		if _, err := daemon.GenerateSandboxProfile(in); err == nil {
			t.Errorf("hk-guapd: GenerateSandboxProfile ACCEPTED the world-shared temp root %q. "+
				"srt expands every TmpDirs entry into a recursive write rule, so this grants the "+
				"run write access to every other process's scratch state on the box — the exact "+
				"defect hk-guapd fixed, reintroduced through the front door.", root)
		}
	}
}

// TestSandboxProfile_PerRunTempDirStillAccepted_hkguapd pins the escape hatch the
// gate promises. A rejection rule that also rejects the legitimate per-run case
// would push callers back toward the shared root, so the boundary is asserted
// from both sides.
func TestSandboxProfile_PerRunTempDirStillAccepted_hkguapd(t *testing.T) {
	t.Parallel()

	const perRun = "/tmp/harmonik-run-0199abcd"

	in := sandboxProfileFixture()
	in.TmpDirs = []string{perRun}

	allowWrite := hkguapdAllowWrite(t, in)

	found := false
	for _, got := range allowWrite {
		if got == perRun {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("hk-guapd: a PER-RUN temp dir %q was rejected or dropped. The gate is meant to "+
			"reject only world-shared roots; a caller that genuinely needs scratch space must "+
			"still have a way to ask for it, or it will reach for the shared root instead. "+
			"Got: %v", perRun, allowWrite)
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
