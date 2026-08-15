package run

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

const dispatchTargetVersion = "harmonik-dispatch-target-v2"

// DispatchTargetNames are the durable tmux target names for one dispatch.
type DispatchTargetNames struct {
	SessionName string
	WindowName  string
}

// BuildDispatchTargetNames returns stable names for one resolved project and run.
// ProjectRealPath must be an absolute, clean path after symlink resolution.
func BuildDispatchTargetNames(
	projectRealPath string,
	runID core.RunID,
	claimTransitionID core.TransitionID,
	location ExecutionLocation,
) (DispatchTargetNames, error) {
	if projectRealPath == "" || !filepath.IsAbs(projectRealPath) || filepath.Clean(projectRealPath) != projectRealPath {
		return DispatchTargetNames{}, errors.New("run: project real path must be absolute and clean")
	}
	if uuid.UUID(runID).Version() != 7 {
		return DispatchTargetNames{}, errors.New("run: dispatch target run_id must be canonical UUIDv7")
	}
	if !claimTransitionID.IsUUIDv7() {
		return DispatchTargetNames{}, errors.New("run: dispatch target claim_transition_id must be canonical UUIDv7")
	}
	if err := location.validate(); err != nil {
		return DispatchTargetNames{}, err
	}

	preimage := append(make([]byte, 0, 256), dispatchTargetVersion...)
	var err error
	for _, value := range []string{
		projectRealPath,
		runID.String(),
		claimTransitionID.String(),
		string(location.Kind),
		location.WorkerName,
		location.Transport,
		location.Host,
		location.RepositoryPath,
	} {
		preimage, err = appendDispatchTargetField(preimage, value)
		if err != nil {
			return DispatchTargetNames{}, err
		}
	}

	sum := sha256.Sum256(preimage)
	digest := hex.EncodeToString(sum[:16])
	return DispatchTargetNames{
		SessionName: "harmonik-run-" + digest,
		WindowName:  "run-" + digest,
	}, nil
}

func appendDispatchTargetField(dst []byte, value string) ([]byte, error) {
	if len(value) > math.MaxUint32 {
		return nil, errors.New("run: dispatch target field is too large")
	}
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(value))) //nolint:gosec // bounded above.
	return append(dst, value...), nil
}
