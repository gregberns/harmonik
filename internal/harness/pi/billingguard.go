package pi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

func piRunIDIsNil(runID core.RunID) bool {
	return uuid.UUID(runID) == uuid.Nil
}

const piAuthFileName = "auth.json"

type piAuthFile struct {
	APIKey string `json:"api_key"`
}

func piDefaultHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi")
}

func piAuthIndicatesPersistentCredential(piHome string) (bool, error) {
	if piHome == "" {
		return false, nil
	}
	authPath := filepath.Join(piHome, piAuthFileName)
	data, err := os.ReadFile(authPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("piAuthIndicatesPersistentCredential: read %q: %w", authPath, err)
	}
	var auth piAuthFile
	if uerr := json.Unmarshal(data, &auth); uerr != nil {
		return false, fmt.Errorf("piAuthIndicatesPersistentCredential: parse %q: %w", authPath, uerr)
	}
	return strings.TrimSpace(auth.APIKey) != "", nil
}

func emitPiBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, apiKeyEnv string,
	outcome core.PiBillingGuardOutcome,
	reason string,
) {
	if bus == nil {
		return
	}
	pl := core.PiBillingGuardPayload{
		BeadID:     beadID,
		EnvVarName: apiKeyEnv,
		Outcome:    outcome,
		Reason:     reason,
	}
	if !piRunIDIsNil(runID) {
		pl.RunID = runID.String()
	}
	b, err := json.Marshal(pl)
	if err != nil {
		slog.WarnContext(ctx, "pi_billing_guard_event_marshal_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", err.Error())
		return
	}
	var emitErr error
	if piRunIDIsNil(runID) {
		emitErr = bus.Emit(ctx, core.EventTypePiBillingGuard, b)
	} else {
		emitErr = bus.EmitWithRunID(ctx, runID, core.EventTypePiBillingGuard, b)
	}
	if emitErr != nil {
		slog.WarnContext(ctx, "pi_billing_guard_event_emit_failed",
			"bead_id", beadID, "outcome", string(outcome), "error", emitErr.Error())
	}
}

func runPiBillingGuard(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	beadID, apiKeyFile, apiKeyEnv, piHome string,
) error {
	keyValue := resolvePiAPIKeyValue(apiKeyFile, apiKeyEnv)
	if strings.TrimSpace(keyValue) == "" {
		reason := fmt.Sprintf(
			"env var %s is absent or empty; Pi cannot launch without a provider API key (PI-040 fail closed)",
			apiKeyEnv)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}

	persisted, err := piAuthIndicatesPersistentCredential(piHome)
	if err != nil {
		reason := fmt.Sprintf(
			"PI-042 on-disk check failed reading %q: %v (fail closed)",
			piHome, err)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}
	if persisted {
		reason := fmt.Sprintf(
			"PI-042 on-disk check: %q/auth.json carries a persisted api_key; refusing to launch Pi over a persisted credential (fail closed)",
			piHome)
		emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardDenied, reason)
		return fmt.Errorf("pi billing guard: %s", reason)
	}

	emitPiBillingGuard(ctx, bus, runID, beadID, apiKeyEnv, core.PiBillingGuardAllowed,
		fmt.Sprintf(
			"env var %s is present and non-empty; no persisted Pi credential detected; Pi launch permitted (PI-040/PI-042)",
			apiKeyEnv))
	return nil
}
