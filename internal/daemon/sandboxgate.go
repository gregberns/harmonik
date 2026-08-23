package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
	"github.com/gregberns/harmonik/internal/projectconfig"
)

func resolveGateAgentType(implHarness handlercontract.Harness, fromArtifacts core.AgentType) core.AgentType {
	if implHarness != nil {
		return implHarness.AgentType()
	}
	return fromArtifacts
}

func sandboxSpawnForRun(cfg projectconfig.SandboxConfig, agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig {
	if cfg.Backend != "srt" {
		return nil
	}
	if !cfg.HasHarness(string(agentType)) {
		return nil
	}
	if strings.HasPrefix(in.DaemonSockPath, "tcp://") {
		return nil
	}
	return &SrtSpawnConfig{ProfileInput: in}
}

const srtDefaultChildTmpDir = "/tmp/claude"

const srtChildTmpDirEnvVar = "CLAUDE_CODE_TMPDIR"

type srtWrap struct {
	// Argv is [srtBinary, "--settings", profilePath, agentArgv...].
	Argv []string

	// Env holds "KEY=VALUE" entries to APPEND to the environment of the process
	// named by Argv[0]. Never a complete environment.
	Env []string
}

func srtWrapArgv(spawn *SrtSpawnConfig, agentArgv []string) (srtWrap, error) {
	scratchDir := SandboxScratchDir(spawn.ProfileInput.WorktreePath)
	if err := os.MkdirAll(scratchDir, 0o700); err != nil { //dirmode:allow tighter on purpose: the run's sandbox scratch TMPDIR holds agent-spooled files, 0o700 by design
		return srtWrap{}, fmt.Errorf("create sandbox scratch TMPDIR %s: %w", scratchDir, err)
	}

	if err := os.MkdirAll(srtDefaultChildTmpDir, 0o700); err != nil { //dirmode:allow not a .harmonik state dir: srt's default /tmp/claude sandbox scratch TMPDIR, 0o700 by design
		return srtWrap{}, fmt.Errorf("create srt fallback TMPDIR %s: %w", srtDefaultChildTmpDir, err)
	}

	profileBytes, err := GenerateSandboxProfile(spawn.ProfileInput)
	if err != nil {
		return srtWrap{}, fmt.Errorf("generate srt profile: %w", err)
	}
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-"+spawn.ProfileInput.RunID+".json")
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		return srtWrap{}, fmt.Errorf("write srt profile to %s: %w", profilePath, err)
	}
	srtBin := spawn.SrtBinary
	if srtBin == "" {
		srtBin = "srt"
	}
	argv := make([]string, 0, 3+len(agentArgv))
	argv = append(argv, srtBin, "--settings", profilePath)
	argv = append(argv, agentArgv...)
	return srtWrap{
		Argv: argv,
		Env:  []string{srtChildTmpDirEnvVar + "=" + scratchDir},
	}, nil
}

func sandboxWrapExecArgv(spawn *SrtSpawnConfig, binary string, args []string) (wrappedBinary string, wrappedArgs, extraEnv []string, err error) {
	if spawn == nil {
		return binary, args, nil, nil
	}
	wrapped, wrapErr := srtWrapArgv(spawn, append([]string{binary}, args...))
	if wrapErr != nil {
		return "", nil, nil, wrapErr
	}
	return wrapped.Argv[0], wrapped.Argv[1:], wrapped.Env, nil
}

const srtEngagementMaxAttempts = 5

const srtEngagementCanaryMarker = "harmonik-srt-engagement-canary"

func srtEngagementCanaryPath(projectDir, runID string) string {
	return filepath.Join(projectDir, ".harmonik-srt-engagement-"+runID+".canary")
}

func verifySandboxEngaged(ctx context.Context, spawn *SrtSpawnConfig, canaryPath string, logf func(format string, args ...any)) error {
	profileBytes, err := GenerateSandboxProfile(spawn.ProfileInput)
	if err != nil {
		return fmt.Errorf("verifySandboxEngaged: generate probe profile: %w", err)
	}
	profilePath := filepath.Join(os.TempDir(), "harmonik-srt-engagement-"+spawn.ProfileInput.RunID+".json")
	if err := os.WriteFile(profilePath, profileBytes, 0o600); err != nil {
		return fmt.Errorf("verifySandboxEngaged: write probe profile: %w", err)
	}
	defer func() { removeProbeFile(profilePath, "probe profile", logf) }()

	srtBin := spawn.SrtBinary
	if srtBin == "" {
		srtBin = "srt"
	}
	script := fmt.Sprintf("echo %s > %s", srtEngagementCanaryMarker, shellQuoteSingle(canaryPath))

	var lastDetail string
	for attempt := 1; attempt <= srtEngagementMaxAttempts; attempt++ {
		removeProbeFile(canaryPath, "canary before attempt", logf)

		//nolint:gosec // G204: srtBin/profilePath/script are daemon-controlled, not attacker input.
		cmd := exec.CommandContext(ctx, srtBin, "--settings", profilePath, "-c", script)
		out, runErr := cmd.CombinedOutput()

		content, readErr := os.ReadFile(canaryPath)
		wrote := readErr == nil && strings.Contains(string(content), srtEngagementCanaryMarker)
		removeProbeFile(canaryPath, "canary after attempt", logf)

		if runErr != nil && !wrote {
			return nil // engaged: srt itself failed AND the denied write never landed
		}

		lastDetail = fmt.Sprintf("attempt %d/%d: srt_exit_err=%v canary_written=%v output=%q",
			attempt, srtEngagementMaxAttempts, runErr, wrote, strings.TrimSpace(string(out)))
		if logf != nil {
			logf("srt sandbox engagement probe observed apply-failure (%s); retrying (hk-tch4t transient) canary=%s",
				lastDetail, canaryPath)
		}
	}
	return fmt.Errorf("srt sandbox engagement verification FAILED after %d attempts — sandbox_init did not engage "+
		"(hk-tch4t/hk-5wdon apply-failure): %s", srtEngagementMaxAttempts, lastDetail)
}

func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func removeProbeFile(path, what string, logf func(format string, args ...any)) {
	rmErr := os.Remove(path)
	if rmErr == nil || errors.Is(rmErr, os.ErrNotExist) || logf == nil {
		return
	}
	logf("srt sandbox engagement probe: remove %s %s: %v", what, path, rmErr)
}
