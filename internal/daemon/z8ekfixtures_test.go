package daemon

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

func newNoOpRecorderZ8ek() *tmux.RecordingRunner {
	return &tmux.RecordingRunner{
		CmdFunc: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "true")
		},
	}
}

func z8ekRunID(t *testing.T) core.RunID {
	t.Helper()
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("mint run id: %v", err)
	}
	return core.RunID(u)
}

func decodeBase64FromScript(t *testing.T, script string) string {
	t.Helper()
	const pfx = "printf %s '"
	i := strings.Index(script, pfx)
	if i < 0 {
		t.Fatalf("no printf in script: %q", script)
	}
	rest := script[i+len(pfx):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("unterminated base64 in script: %q", script)
	}
	raw, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	return string(raw)
}
