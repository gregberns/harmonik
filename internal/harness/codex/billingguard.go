package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func codexRunIDIsNil(runID core.RunID) bool {
	return uuid.UUID(runID) == uuid.Nil
}

const forcedLoginMethodKey = "forced_login_method"

const forcedLoginMethodValue = "chatgpt"

const codexConfigFileName = "config.toml"

const codexAuthFileName = "auth.json"

const codexConfigLockName = ".harmonik-config.toml.lock"

const codexConfigStagingName = ".harmonik-config.toml.staging"

var codexConfigWriteMu sync.Mutex

var errCodexConfigLockTimeout = fmt.Errorf(
	"codex billing guard: %w: config-lock acquire timed out (contended $CODEX_HOME)",
	handlercontract.ErrStructural)

const codexConfigLockTimeout = 10 * time.Second

const codexConfigLockRetryInterval = 25 * time.Millisecond

func acquireCodexConfigLock(ctx context.Context, codexHome string, timeout time.Duration) (func(), error) {
	lockPath := filepath.Join(codexHome, codexConfigLockName)
	//nolint:gosec // G304: fixed lockfile name beneath the caller's CODEX_HOME root.
	fd, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock %q: %w", lockPath, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		flockErr := syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			break
		}
		if !errors.Is(flockErr, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("flock %q: %w", lockPath, flockErr), fd.Close())
		}
		if time.Now().After(deadline) {
			return nil, errors.Join(
				fmt.Errorf("%w: %s after %s", errCodexConfigLockTimeout, lockPath, timeout),
				fd.Close())
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(
				fmt.Errorf("flock %q: %w", lockPath, ctx.Err()),
				fd.Close())
		case <-time.After(codexConfigLockRetryInterval):
		}
	}
	return func() {
		_ = syscall.Flock(int(fd.Fd()), syscall.LOCK_UN) //nolint:errcheck // unlock error non-actionable; close also drops the advisory lock
		if closeErr := fd.Close(); closeErr != nil {
			slog.WarnContext(ctx, "codex billing guard: close config lockfile",
				"path", lockPath, "error", closeErr.Error())
		}
	}, nil
}

func replaceCodexConfig(codexHome, cfgPath string, data []byte) error {
	tmpPath := filepath.Join(codexHome, codexConfigStagingName)
	//nolint:gosec // G304: fixed staging filename beneath the caller's CODEX_HOME root.
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("create staging file %q: %w", tmpPath, err)
	}
	discard := func(cause error, format string, args ...any) error {
		_ = os.Remove(tmpPath) //nolint:errcheck // cleanup of a staging file we are already failing on
		return fmt.Errorf(format+": %w", append(args, cause)...)
	}
	if _, werr := tmp.Write(data); werr != nil {
		return discard(errors.Join(werr, tmp.Close()), "write staging file %q", tmpPath)
	}
	if serr := tmp.Sync(); serr != nil {
		return discard(errors.Join(serr, tmp.Close()), "fsync staging file %q", tmpPath)
	}
	if cerr := tmp.Close(); cerr != nil {
		return discard(cerr, "close staging file %q", tmpPath)
	}
	if cerr := os.Chmod(tmpPath, 0o600); cerr != nil {
		return discard(cerr, "chmod staging file %q", tmpPath)
	}
	if rerr := os.Rename(tmpPath, cfgPath); rerr != nil {
		return discard(rerr, "rename %q over %q", tmpPath, cfgPath)
	}
	return nil
}

func materializeForcedLoginMethod(ctx context.Context, codexHome string) error {
	return materializeForcedLoginMethodWithin(ctx, codexHome, codexConfigLockTimeout)
}

func materializeForcedLoginMethodWithin(ctx context.Context, codexHome string, lockTimeout time.Duration) error {
	if codexHome == "" {
		return fmt.Errorf("materializeForcedLoginMethod: codexHome must be non-empty")
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil { //dirmode:allow not a .harmonik state dir: $CODEX_HOME holds codex's own login credentials, 0o700 by design (never widen to core.HarmonikDirMode)
		return fmt.Errorf("materializeForcedLoginMethod: mkdir %q: %w", codexHome, err)
	}

	if pinned, probeErr := configDeclaresChatGPTLogin(codexHome); probeErr == nil && pinned {
		return nil
	}

	codexConfigWriteMu.Lock()
	defer codexConfigWriteMu.Unlock()
	if pinned, probeErr := configDeclaresChatGPTLogin(codexHome); probeErr == nil && pinned {
		return nil
	}

	release, err := acquireCodexConfigLock(ctx, codexHome, lockTimeout)
	if err != nil {
		return fmt.Errorf("materializeForcedLoginMethod: %w", err)
	}
	defer release()

	cfgPath := filepath.Join(codexHome, codexConfigFileName)

	//nolint:gosec // G304: cfgPath is a fixed config.toml filename beneath the caller's CODEX_HOME root.
	existing, err := os.ReadFile(cfgPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("materializeForcedLoginMethod: read %q: %w", cfgPath, err)
	}

	if werr := replaceCodexConfig(codexHome, cfgPath, pinnedConfigContent(existing)); werr != nil {
		return fmt.Errorf("materializeForcedLoginMethod: %w", werr)
	}
	return nil
}

func pinnedConfigContent(existing []byte) []byte {
	wantLine := fmt.Sprintf("%s = %q", forcedLoginMethodKey, forcedLoginMethodValue)
	if len(existing) == 0 {
		return []byte(wantLine + "\n")
	}
	lines := strings.Split(string(existing), "\n")
	for i, line := range lines {
		if topLevelKeyOf(line) == forcedLoginMethodKey {
			lines[i] = wantLine
			return []byte(strings.Join(lines, "\n"))
		}
	}
	return []byte(strings.TrimRight(string(existing), "\n") + "\n" + wantLine + "\n")
}

func topLevelKeyOf(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return ""
	}
	eq := strings.IndexByte(trimmed, '=')
	if eq <= 0 {
		return ""
	}
	return strings.TrimSpace(trimmed[:eq])
}

func configDeclaresChatGPTLogin(codexHome string) (bool, error) {
	cfgPath := filepath.Join(codexHome, codexConfigFileName)
	//nolint:gosec // G304: cfgPath is a fixed config.toml filename beneath the caller's CODEX_HOME root.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return false, fmt.Errorf("configDeclaresChatGPTLogin: read %q: %w", cfgPath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if topLevelKeyOf(line) != forcedLoginMethodKey {
			continue
		}
		eq := strings.IndexByte(line, '=')
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, "\"'")
		return val == forcedLoginMethodValue, nil
	}
	return false, nil
}

type codexAuthFile struct {
	OpenAIAPIKey string `json:"OPENAI_API_KEY"`
}

func authIndicatesAPIKeyLogin(codexHome string) (bool, error) {
	authPath := filepath.Join(codexHome, codexAuthFileName)
	//nolint:gosec // G304: authPath is a fixed auth.json filename beneath the caller's CODEX_HOME root.
	data, err := os.ReadFile(authPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("authIndicatesAPIKeyLogin: read %q: %w", authPath, err)
	}
	var auth codexAuthFile
	if uerr := json.Unmarshal(data, &auth); uerr != nil {
		return false, fmt.Errorf("authIndicatesAPIKeyLogin: parse %q: %w", authPath, uerr)
	}
	return strings.TrimSpace(auth.OpenAIAPIKey) != "", nil
}

func assertChatGPTPlan(codexHome string) error {
	if codexHome == "" {
		return fmt.Errorf("assertChatGPTPlan: codexHome must be non-empty")
	}

	ok, err := configDeclaresChatGPTLogin(codexHome)
	if err != nil {
		return fmt.Errorf("assertChatGPTPlan: cannot read config.toml: %w", err)
	}
	if !ok {
		return fmt.Errorf(
			"assertChatGPTPlan: %s/%s does not declare %s = %q; refusing to launch codex (fail closed)",
			codexHome, codexConfigFileName, forcedLoginMethodKey, forcedLoginMethodValue)
	}

	apiKeyLogin, err := authIndicatesAPIKeyLogin(codexHome)
	if err != nil {
		return fmt.Errorf("assertChatGPTPlan: cannot inspect auth.json: %w", err)
	}
	if apiKeyLogin {
		return fmt.Errorf(
			"assertChatGPTPlan: %s/%s carries a populated OPENAI_API_KEY (API-pool billing); refusing to launch codex (fail closed)",
			codexHome, codexAuthFileName)
	}

	return nil
}

func emitCodexBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, codexHome string,
	outcome core.CodexBillingGuardOutcome,
	reason string,
) {
	if bus == nil {
		return
	}
	pl := core.CodexBillingGuardPayload{
		BeadID:    beadID,
		CodexHome: codexHome,
		Outcome:   outcome,
		Reason:    reason,
	}
	if !codexRunIDIsNil(runID) {
		pl.RunID = runID.String()
	}
	b, err := json.Marshal(pl)
	if err != nil {
		slog.WarnContext(ctx, "codex_billing_guard_event_marshal_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", err.Error())
		return
	}
	var emitErr error
	if codexRunIDIsNil(runID) {
		emitErr = bus.Emit(ctx, core.EventTypeCodexBillingGuard, b)
	} else {
		emitErr = bus.EmitWithRunID(ctx, runID, core.EventTypeCodexBillingGuard, b)
	}
	if emitErr != nil {
		slog.WarnContext(ctx, "codex_billing_guard_event_emit_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", emitErr.Error())
	}
}

func runCodexBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, codexHome string,
) error {
	if err := materializeForcedLoginMethod(ctx, codexHome); err != nil {
		emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
			core.CodexBillingGuardDenied, "materialize failed: "+err.Error())
		return fmt.Errorf("codex billing guard: %w", err)
	}
	emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
		core.CodexBillingGuardMaterialized,
		fmt.Sprintf("%s = %q ensured in %s", forcedLoginMethodKey, forcedLoginMethodValue, codexConfigFileName))

	if err := assertChatGPTPlan(codexHome); err != nil {
		emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
			core.CodexBillingGuardDenied, err.Error())
		return fmt.Errorf("codex billing guard: %w", err)
	}
	emitCodexBillingGuard(ctx, bus, runID, beadID, codexHome,
		core.CodexBillingGuardAllowed, "ChatGPT plan confirmed; codex launch permitted")
	return nil
}
