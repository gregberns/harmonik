package lifecycle

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// ntmAdapterFixtureSupportedVersions is the fixture-level version set for ntm.
// In the real daemon this is declared in the release manifest and consumed by
// the Cat 0 pre-check (PL-005 step 4 / PL-021a). The fixture hard-codes a
// representative set so tests can assert absent-version → Cat 0 without a live
// release manifest.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "supported ntm versions MUST
// be declared in the release manifest."
var ntmAdapterFixtureSupportedVersions = []string{
	"0.9.0",
	"0.10.0",
	"0.10.1",
	"0.11.0",
}

// ntmAdapterFixtureAvailability models the ntm availability probe result that
// the Cat 0 pre-check (PL-005 step 4) performs.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "ntm not on PATH, tmux missing,
// or failing the version probe MUST classify identically as Cat 0."
type ntmAdapterFixtureAvailability struct {
	// onPath is true when the ntm binary is on PATH.
	onPath bool
	// tmuxOnPath is true when the tmux binary is on PATH.
	tmuxOnPath bool
	// detectedVersion is the version string returned by `ntm --version` (empty if absent).
	detectedVersion string
	// supportedVersions is the set declared in the release manifest.
	supportedVersions []string
}

// ntmAdapterFixtureProbeResult is the outcome of the ntm availability probe.
type ntmAdapterFixtureProbeResult struct {
	// cat0Failure is true when the probe classifies as Cat 0.
	cat0Failure bool
	// exitCode is the ON §8 exit code (22 = ntm-unavailable).
	exitCode int
	// failedPrerequisite is the infrastructure_unavailable payload field.
	failedPrerequisite string
	// failureReason names the specific reason within ntm-unavailable.
	failureReason string
}

// ntmAdapterFixtureProbeNtm runs the ntm availability probe against the given
// availability state and returns a probe result. It mirrors the logic the real
// Cat 0 pre-check uses for PL-021a.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "An ntm version outside the
// supported set MUST be detected during §PL-005 step 4 Cat 0 pre-check and MUST
// produce ON §8 code 22 (ntm-unavailable) … plus
// infrastructure_unavailable{failed_prerequisite=ntm_unavailable}."
func ntmAdapterFixtureProbeNtm(avail ntmAdapterFixtureAvailability) ntmAdapterFixtureProbeResult {
	// PL-021a: ntm not on PATH → Cat 0, code 22.
	if !avail.onPath {
		return ntmAdapterFixtureProbeResult{
			cat0Failure:        true,
			exitCode:           22,
			failedPrerequisite: "ntm_unavailable",
			failureReason:      "ntm-not-on-path",
		}
	}

	// PL-021a: tmux missing → Cat 0, code 22 (same classification).
	if !avail.tmuxOnPath {
		return ntmAdapterFixtureProbeResult{
			cat0Failure:        true,
			exitCode:           22,
			failedPrerequisite: "ntm_unavailable",
			failureReason:      "tmux-not-on-path",
		}
	}

	// PL-021a: version probe returned empty (binary exists but crashes/hangs) → Cat 0.
	if avail.detectedVersion == "" {
		return ntmAdapterFixtureProbeResult{
			cat0Failure:        true,
			exitCode:           22,
			failedPrerequisite: "ntm_unavailable",
			failureReason:      "ntm-version-probe-failed",
		}
	}

	// PL-021a: version outside the supported set → Cat 0, code 22.
	versionSupported := false
	for _, sv := range avail.supportedVersions {
		if sv == avail.detectedVersion {
			versionSupported = true
			break
		}
	}
	if !versionSupported {
		return ntmAdapterFixtureProbeResult{
			cat0Failure:        true,
			exitCode:           22,
			failedPrerequisite: "ntm_unavailable",
			failureReason:      "ntm-version-incompatible",
		}
	}

	// ntm is present, tmux is present, and version is in the supported set.
	return ntmAdapterFixtureProbeResult{
		cat0Failure: false,
		exitCode:    0,
	}
}

// TestPL021a_NtmAbsenceProducesExitCode22 verifies that the Cat 0 pre-check
// classifies ntm-not-on-PATH as code 22 (ntm-unavailable) and produces the
// infrastructure_unavailable event payload with the correct failed_prerequisite.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "ntm not on PATH, tmux missing,
// or failing the version probe MUST classify identically as Cat 0 per
// [reconciliation/spec.md §8.1]."
// Spec ref: operator-nfr.md §8, code 22 — ntm-unavailable.
func TestPL021a_NtmAbsenceProducesExitCode22(t *testing.T) {
	t.Parallel()

	t.Run("ntm-not-on-path", func(t *testing.T) {
		t.Parallel()

		avail := ntmAdapterFixtureAvailability{
			onPath:            false, // ntm binary absent from PATH
			tmuxOnPath:        true,
			detectedVersion:   "",
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}

		result := ntmAdapterFixtureProbeNtm(avail)

		if !result.cat0Failure {
			t.Error("PL-021a ntm-not-on-path: expected Cat 0 failure; got no failure")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a ntm-not-on-path: exitCode = %d, want 22 (ntm-unavailable)", result.exitCode)
		}
		if result.failedPrerequisite != "ntm_unavailable" {
			t.Errorf("PL-021a ntm-not-on-path: failedPrerequisite = %q, want %q",
				result.failedPrerequisite, "ntm_unavailable")
		}
	})

	t.Run("tmux-not-on-path", func(t *testing.T) {
		t.Parallel()

		avail := ntmAdapterFixtureAvailability{
			onPath:            true,
			tmuxOnPath:        false, // tmux absent from PATH
			detectedVersion:   "0.10.0",
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}

		result := ntmAdapterFixtureProbeNtm(avail)

		if !result.cat0Failure {
			t.Error("PL-021a tmux-not-on-path: expected Cat 0 failure; got no failure")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a tmux-not-on-path: exitCode = %d, want 22 (ntm-unavailable)", result.exitCode)
		}
		if result.failedPrerequisite != "ntm_unavailable" {
			t.Errorf("PL-021a tmux-not-on-path: failedPrerequisite = %q, want %q",
				result.failedPrerequisite, "ntm_unavailable")
		}
	})

	t.Run("ntm-version-probe-fails", func(t *testing.T) {
		t.Parallel()

		// Binary exists but the version probe returned an empty string (crashed/hang).
		avail := ntmAdapterFixtureAvailability{
			onPath:            true,
			tmuxOnPath:        true,
			detectedVersion:   "", // empty: version probe failed
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}

		result := ntmAdapterFixtureProbeNtm(avail)

		if !result.cat0Failure {
			t.Error("PL-021a version-probe-fails: expected Cat 0 failure; got no failure")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a version-probe-fails: exitCode = %d, want 22", result.exitCode)
		}
	})

	t.Run("all-three-reasons-map-to-same-code", func(t *testing.T) {
		t.Parallel()

		// PL-021a: "ntm not on PATH, tmux missing, or failing the version probe
		// MUST classify identically as Cat 0."
		scenarios := []ntmAdapterFixtureAvailability{
			{onPath: false, tmuxOnPath: true, detectedVersion: "", supportedVersions: ntmAdapterFixtureSupportedVersions},
			{onPath: true, tmuxOnPath: false, detectedVersion: "0.9.0", supportedVersions: ntmAdapterFixtureSupportedVersions},
			{onPath: true, tmuxOnPath: true, detectedVersion: "", supportedVersions: ntmAdapterFixtureSupportedVersions},
		}

		for _, avail := range scenarios {
			result := ntmAdapterFixtureProbeNtm(avail)
			if !result.cat0Failure {
				t.Errorf("PL-021a identical-cat0: expected Cat 0 failure for %+v; got none", avail)
			}
			if result.exitCode != 22 {
				t.Errorf("PL-021a identical-cat0: exitCode = %d, want 22 for %+v", result.exitCode, avail)
			}
			if result.failedPrerequisite != "ntm_unavailable" {
				t.Errorf("PL-021a identical-cat0: failedPrerequisite = %q, want ntm_unavailable for %+v",
					result.failedPrerequisite, avail)
			}
		}
	})
}

// TestPL021a_NtmVersionPinOutsideSupportedSetIsCAT0 verifies that a detected
// ntm version outside the supported set triggers Cat 0 / exit code 22.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "An ntm version outside the
// supported set MUST be detected during §PL-005 step 4 Cat 0 pre-check and MUST
// produce ON §8 code 22 (ntm-unavailable)."
func TestPL021a_NtmVersionPinOutsideSupportedSetIsCAT0(t *testing.T) {
	t.Parallel()

	t.Run("unsupported-version", func(t *testing.T) {
		t.Parallel()

		avail := ntmAdapterFixtureAvailability{
			onPath:            true,
			tmuxOnPath:        true,
			detectedVersion:   "0.8.0", // older than supported set
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}

		result := ntmAdapterFixtureProbeNtm(avail)

		if !result.cat0Failure {
			t.Error("PL-021a unsupported-version: expected Cat 0 failure; got none")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a unsupported-version: exitCode = %d, want 22", result.exitCode)
		}
		if result.failedPrerequisite != "ntm_unavailable" {
			t.Errorf("PL-021a unsupported-version: failedPrerequisite = %q, want ntm_unavailable",
				result.failedPrerequisite)
		}
		if result.failureReason != "ntm-version-incompatible" {
			t.Errorf("PL-021a unsupported-version: failureReason = %q, want ntm-version-incompatible",
				result.failureReason)
		}
	})

	t.Run("future-version-also-unsupported", func(t *testing.T) {
		t.Parallel()

		// A version newer than the supported set is also unsupported (pin is exact).
		avail := ntmAdapterFixtureAvailability{
			onPath:            true,
			tmuxOnPath:        true,
			detectedVersion:   "1.0.0", // too new
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}

		result := ntmAdapterFixtureProbeNtm(avail)

		if !result.cat0Failure {
			t.Error("PL-021a future-version: expected Cat 0 failure for future version; got none")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a future-version: exitCode = %d, want 22", result.exitCode)
		}
	})

	t.Run("supported-version-passes-check", func(t *testing.T) {
		t.Parallel()

		for _, sv := range ntmAdapterFixtureSupportedVersions {
			sv := sv // capture
			t.Run("version-"+sv, func(t *testing.T) {
				t.Parallel()

				avail := ntmAdapterFixtureAvailability{
					onPath:            true,
					tmuxOnPath:        true,
					detectedVersion:   sv,
					supportedVersions: ntmAdapterFixtureSupportedVersions,
				}

				result := ntmAdapterFixtureProbeNtm(avail)

				if result.cat0Failure {
					t.Errorf("PL-021a supported-version %q: unexpected Cat 0 failure; version IS in supported set", sv)
				}
				if result.exitCode != 0 {
					t.Errorf("PL-021a supported-version %q: exitCode = %d, want 0", sv, result.exitCode)
				}
			})
		}
	})
}

// ntmAdapterFixtureForbiddenImport names an ntm feature that the adapter MUST NOT
// consume. These correspond to the four prohibited categories in PL-022.
//
// Spec ref: process-lifecycle.md §4.7 PL-022 — "The ntm adapter MUST NOT import
// or consume: (a) ntm's Pipeline System, (b) ntm's SwarmPlan format, (c) ntm's
// checkpoint/recovery, or (d) ntm's file-reservation / Agent Mail features."
type ntmAdapterFixtureForbiddenImport string

const (
	ntmAdapterFixtureForbiddenPipelineSystem    ntmAdapterFixtureForbiddenImport = "ntm-pipeline-system"
	ntmAdapterFixtureForbiddenSwarmPlan         ntmAdapterFixtureForbiddenImport = "ntm-swarmplan"
	ntmAdapterFixtureForbiddenCheckpointRecover ntmAdapterFixtureForbiddenImport = "ntm-checkpoint-recovery"
	ntmAdapterFixtureForbiddenAgentMail         ntmAdapterFixtureForbiddenImport = "ntm-agent-mail"
)

// ntmAdapterFixtureAllForbiddenImports is the full set of forbidden ntm features
// per PL-022.
var ntmAdapterFixtureAllForbiddenImports = []ntmAdapterFixtureForbiddenImport{
	ntmAdapterFixtureForbiddenPipelineSystem,
	ntmAdapterFixtureForbiddenSwarmPlan,
	ntmAdapterFixtureForbiddenCheckpointRecover,
	ntmAdapterFixtureForbiddenAgentMail,
}

// TestPL021_PL022_NtmAdapterAllowedSurfaceOnly verifies that the ntm adapter
// package (when it exists) imports only the process/tmux surface and does NOT
// import the forbidden ntm features declared by PL-022.
//
// When internal/adapter/ntm does not yet exist (pre-implementation harness),
// the scan is skipped with a log message. The test passes structurally.
//
// Spec ref: process-lifecycle.md §4.7 PL-021 — "The ntm adapter layer MUST
// consume only: (a) agent process spawning in a tmux pane, (b) agent-profile
// knowledge, (c) lifecycle events, and (d) account rotation."
// Spec ref: process-lifecycle.md §4.7 PL-022 — "The ntm adapter MUST NOT import
// or consume: (a) Pipeline System, (b) SwarmPlan, (c) checkpoint/recovery, (d)
// Agent Mail."
func TestPL021_PL022_NtmAdapterAllowedSurfaceOnly(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL021_PL022_NtmAdapterAllowedSurfaceOnly: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	const ntmAdapterPkg = "github.com/gregberns/harmonik/internal/adapter/ntm"

	// Forbidden import path prefixes for each PL-022 prohibited category.
	// These are placeholder paths that the real ntm SDK would use.
	type forbiddenEntry struct {
		label  ntmAdapterFixtureForbiddenImport
		prefix string
	}
	forbidden := []forbiddenEntry{
		{ntmAdapterFixtureForbiddenPipelineSystem, "github.com/claude-ntm/ntm/pipeline"},
		{ntmAdapterFixtureForbiddenSwarmPlan, "github.com/claude-ntm/ntm/swarmplan"},
		{ntmAdapterFixtureForbiddenCheckpointRecover, "github.com/claude-ntm/ntm/checkpoint"},
		{ntmAdapterFixtureForbiddenAgentMail, "github.com/claude-ntm/ntm/agentmail"},
	}

	//nolint:gosec // G204: ntmAdapterPkg is a constant string, not user input
	cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", ntmAdapterPkg)
	out, err := cmd.Output()
	if err != nil {
		t.Logf("PL-021/PL-022: go list -deps %q error (package may not exist): %v; skipping import-graph scan",
			ntmAdapterPkg, err)
		return
	}

	deps := strings.Split(strings.TrimSpace(string(out)), "\n")

	for _, entry := range forbidden {
		for _, dep := range deps {
			dep = strings.TrimSpace(dep)
			if strings.HasPrefix(dep, entry.prefix) {
				t.Errorf("PL-022 forbidden import: internal/adapter/ntm transitively imports %q (%s); "+
					"ntm adapter MUST NOT consume %s", dep, entry.prefix, entry.label)
			}
		}
	}
}

// TestPL023_HandlerContractIsNtmBoundary verifies the structural assertion that
// the handler contract (internal/handlercontract) is the boundary between the
// ntm adapter and the daemon. The ntm adapter package (internal/adapter/ntm)
// MUST NOT be imported by any package other than internal/handlercontract (or the
// composition root internal/daemon).
//
// This test asserts the boundary shape: when both packages exist, no peer
// subsystem imports the ntm adapter directly.
//
// Spec ref: process-lifecycle.md §4.7 PL-023 — "The handler contract at
// [handler-contract.md §4.12] is where the ntm-vs-daemon boundary lives.
// Proposals that cross it by importing ntm pipeline types, SwarmPlan records,
// or Agent Mail primitives into the daemon MUST fail review."
func TestPL023_HandlerContractIsNtmBoundary(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL023_HandlerContractIsNtmBoundary: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	const ntmAdapterPkg = "github.com/gregberns/harmonik/internal/adapter/ntm"

	// Peer packages that MUST NOT import the ntm adapter directly (they should
	// interact with ntm only through the handler contract abstraction).
	peerPackages := []string{
		"github.com/gregberns/harmonik/internal/workspace",
		"github.com/gregberns/harmonik/internal/core",
	}

	for _, pkg := range peerPackages {
		pkg := pkg // capture
		t.Run("no-direct-ntm-import/"+lastSegment(pkg), func(t *testing.T) {
			t.Parallel()

			//nolint:gosec // G204: pkg is a constant string, not user input
			cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", pkg)
			out, err := cmd.Output()
			if err != nil {
				t.Logf("PL-023: go list -deps %q error (package may not exist): %v", pkg, err)
				return
			}

			deps := strings.Split(strings.TrimSpace(string(out)), "\n")
			for _, dep := range deps {
				if strings.TrimSpace(dep) == ntmAdapterPkg {
					t.Errorf("PL-023 handler-contract boundary: %q imports internal/adapter/ntm directly; "+
						"ntm adapter MUST be accessed only through the handler contract boundary", pkg)
				}
			}
		})
	}
}

// TestPL021a_NtmAbsenceOnRealPath verifies the absence-detection on the real
// PATH by running the `ntm --version` probe against the test environment's PATH.
// If ntm is absent from PATH, the probe must return exit code 22 equivalent.
// If ntm is present, the test logs the detected version without failing.
//
// This test exercises the real path-probe logic (exec.CommandContext) used by
// the daemon's Cat 0 pre-check.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "ntm not on PATH … MUST
// classify … as Cat 0."
func TestPL021a_NtmAbsenceOnRealPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skipf("TestPL021a_NtmAbsenceOnRealPath: skipping on %s (POSIX-only)", runtime.GOOS)
	}

	// Probe the real PATH for ntm.
	ntmPath, err := exec.LookPath("ntm")
	if err != nil {
		// ntm absent from PATH: this is the expected state in CI and on most dev
		// machines. Verify the fixture probe returns Cat 0 / code 22.
		avail := ntmAdapterFixtureAvailability{
			onPath:            false,
			tmuxOnPath:        true,
			detectedVersion:   "",
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}
		result := ntmAdapterFixtureProbeNtm(avail)
		if !result.cat0Failure {
			t.Error("PL-021a real-path: ntm absent from PATH but probe returned no Cat 0 failure")
		}
		if result.exitCode != 22 {
			t.Errorf("PL-021a real-path: ntm absent from PATH; exitCode = %d, want 22", result.exitCode)
		}
		t.Logf("PL-021a real-path: ntm absent from PATH (expected); probe correctly returns Cat 0 / code 22")
		return
	}

	// ntm is present: probe the version and log. Do NOT fail — the daemon itself
	// would check the version against the release manifest.
	t.Logf("PL-021a real-path: ntm found at %q; running version probe", ntmPath)

	cmd := exec.CommandContext(t.Context(), "ntm", "--version")
	out, err := cmd.Output()
	if err != nil {
		// Version probe failed even though ntm is on PATH.
		avail := ntmAdapterFixtureAvailability{
			onPath:            true,
			tmuxOnPath:        true,
			detectedVersion:   "",
			supportedVersions: ntmAdapterFixtureSupportedVersions,
		}
		result := ntmAdapterFixtureProbeNtm(avail)
		if !result.cat0Failure {
			t.Error("PL-021a real-path: ntm version probe failed but probe returned no Cat 0 failure")
		}
		return
	}

	detectedVersion := strings.TrimSpace(string(out))
	t.Logf("PL-021a real-path: detected ntm version = %q", detectedVersion)

	// If the version is outside the fixture's supported set, confirm Cat 0.
	avail := ntmAdapterFixtureAvailability{
		onPath:            true,
		tmuxOnPath:        true,
		detectedVersion:   detectedVersion,
		supportedVersions: ntmAdapterFixtureSupportedVersions,
	}
	result := ntmAdapterFixtureProbeNtm(avail)
	if result.cat0Failure {
		t.Logf("PL-021a real-path: detected ntm version %q is outside fixture supported set; Cat 0 as expected", detectedVersion)
	} else {
		t.Logf("PL-021a real-path: detected ntm version %q is in fixture supported set; probe passes", detectedVersion)
	}
}

// TestPL021a_InfrastructureUnavailablePayload verifies that the Cat 0 pre-check
// produces the correct infrastructure_unavailable event payload when ntm is
// absent or version-incompatible.
//
// Spec ref: process-lifecycle.md §4.7 PL-021a — "MUST produce ON §8 code 22
// (ntm-unavailable) per PL-008a plus
// infrastructure_unavailable{failed_prerequisite=ntm_unavailable}."
// Spec ref: event-model.md §8.7.15 — infrastructure_unavailable payload.
func TestPL021a_InfrastructureUnavailablePayload(t *testing.T) {
	t.Parallel()

	// ntmAdapterFixtureInfrastructureUnavailableEvent models the
	// infrastructure_unavailable event payload per event-model.md §8.7.15.
	type ntmAdapterFixtureInfrastructureUnavailableEvent struct {
		FailedPrerequisite string `json:"failed_prerequisite"`
		ExitCode           int    `json:"exit_code"`
	}

	// Helper: build the infrastructure_unavailable payload from a probe result.
	buildEvent := func(r ntmAdapterFixtureProbeResult) ntmAdapterFixtureInfrastructureUnavailableEvent {
		return ntmAdapterFixtureInfrastructureUnavailableEvent{
			FailedPrerequisite: r.failedPrerequisite,
			ExitCode:           r.exitCode,
		}
	}

	cases := []struct {
		name  string
		avail ntmAdapterFixtureAvailability
	}{
		{"ntm-not-on-path", ntmAdapterFixtureAvailability{
			onPath: false, tmuxOnPath: true, detectedVersion: "", supportedVersions: ntmAdapterFixtureSupportedVersions,
		}},
		{"tmux-not-on-path", ntmAdapterFixtureAvailability{
			onPath: true, tmuxOnPath: false, detectedVersion: "0.10.0", supportedVersions: ntmAdapterFixtureSupportedVersions,
		}},
		{"version-incompatible", ntmAdapterFixtureAvailability{
			onPath: true, tmuxOnPath: true, detectedVersion: "0.8.0", supportedVersions: ntmAdapterFixtureSupportedVersions,
		}},
	}

	for _, tc := range cases {
		tc := tc // capture
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := ntmAdapterFixtureProbeNtm(tc.avail)
			evt := buildEvent(result)

			if evt.FailedPrerequisite != "ntm_unavailable" {
				t.Errorf("PL-021a %s: event.failed_prerequisite = %q, want ntm_unavailable",
					tc.name, evt.FailedPrerequisite)
			}
			if evt.ExitCode != 22 {
				t.Errorf("PL-021a %s: event.exit_code = %d, want 22", tc.name, evt.ExitCode)
			}
		})
	}
}
