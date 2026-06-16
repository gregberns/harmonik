package cli_test

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue/cli"
)

// hk-snjr — KEEPER parser-parity SPLIT C, verb boundary. Asserts that every
// queue verb rejects an UNRECOGNIZED leading-dash token loudly with exit 2
// (exitTransportError) — short-circuiting in the shared parser BEFORE any daemon
// dial — and that the flag form WINS over a positional where both are accepted.

// runQueueVerb is the common shape of all nine queue verb entry points.
type runQueueVerb func(ctx context.Context, subArgs []string, out, errOut io.Writer) int

func allQueueVerbs() map[string]runQueueVerb {
	return map[string]runQueueVerb{
		"append":          cli.RunQueueAppend,
		"cancel":          cli.RunQueueCancel,
		"dry-run":         cli.RunQueueDryRun,
		"list":            cli.RunQueueList,
		"pause":           cli.RunQueuePause,
		"resume":          cli.RunQueueResume,
		"set-concurrency": cli.RunQueueSetConcurrency,
		"status":          cli.RunQueueStatus,
		"submit":          cli.RunQueueSubmit,
	}
}

// TestQueueVerbsRejectUnrecognizedDashFlag pins exit 2 for a stray leading-dash
// token at the real verb boundary for all nine verbs. The shared parser rejects
// the token before any socket I/O, so these calls have no side effects and need
// no live daemon.
func TestQueueVerbsRejectUnrecognizedDashFlag(t *testing.T) {
	t.Parallel()
	const wantExit = 2 // exitTransportError
	for name, run := range allQueueVerbs() {
		name, run := name, run
		t.Run(name+"/leading", func(t *testing.T) {
			t.Parallel()
			var out, errOut strings.Builder
			if got := run(context.Background(), []string{"--bogus"}, &out, &errOut); got != wantExit {
				t.Fatalf("%s --bogus: exit = %d, want %d; stderr=%q", name, got, wantExit, errOut.String())
			}
		})
		t.Run(name+"/trailing-after-positional", func(t *testing.T) {
			t.Parallel()
			var out, errOut strings.Builder
			if got := run(context.Background(), []string{"somearg", "--bogus"}, &out, &errOut); got != wantExit {
				t.Fatalf("%s somearg --bogus: exit = %d, want %d; stderr=%q", name, got, wantExit, errOut.String())
			}
		})
	}
}

// TestQueuePauseResumeFlagWinsPositional proves the --queue flag WINS over a
// positional <name> for the verbs that accept both. The echo server captures the
// queue field the daemon would receive.
func TestQueuePauseResumeFlagWinsPositional(t *testing.T) {
	t.Parallel()

	verbs := map[string]runQueueVerb{
		"pause":  cli.RunQueuePause,
		"resume": cli.RunQueueResume,
	}
	for name, run := range verbs {
		name, run := name, run
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			projectDir := queueCliFixtureTempDir(t)
			var capturedQueue string
			queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
				var msg map[string]json.RawMessage
				if err := json.Unmarshal(raw, &msg); err == nil {
					if qBytes, ok := msg["queue"]; ok {
						_ = json.Unmarshal(qBytes, &capturedQueue)
					}
				}
				return queueCliFixtureSuccessResponse(t, map[string]any{})
			})

			var out, errOut strings.Builder
			got := run(context.Background(),
				[]string{"--project", projectDir, "--queue", "flagwin", "positionalname"},
				&out, &errOut)
			if got != 0 {
				t.Fatalf("%s --queue flagwin positionalname: exit = %d, want 0; stderr=%q", name, got, errOut.String())
			}
			if capturedQueue != "flagwin" {
				t.Errorf("%s flag-wins-positional: queue = %q, want %q", name, capturedQueue, "flagwin")
			}
		})
	}
}
