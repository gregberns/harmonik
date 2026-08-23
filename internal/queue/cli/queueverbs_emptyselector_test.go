package cli_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/queue/cli"
)

type queueVerbSpy struct {
	mu       sync.Mutex
	requests []string
}

func (s *queueVerbSpy) record(raw []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, string(raw))
}

func (s *queueVerbSpy) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// TestQueueVerbs_EmptySelectorValue_IsRefused covers both verbs across
// all three spellings of an empty selector value. Each must reach the daemon
// with nothing, refuse with exit 2, print nothing that reads as success, and
// name on stderr WHICH selector arrived empty.
func TestQueueVerbs_EmptySelectorValue_IsRefused(t *testing.T) {
	t.Parallel()

	for _, verb := range []struct {
		name    string
		run     func(context.Context, []string, *strings.Builder, *strings.Builder) int
		success string // the word its happy path prints on stdout
	}{
		{"pause", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueuePause(ctx, a, o, e)
		}, "paused"},
		{"resume", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueueResume(ctx, a, o, e)
		}, "resumed"},
		{"recover", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueueRecover(ctx, a, o, e)
		}, "recovered"},
	} {
		for _, tc := range []struct {
			name string
			args []string
			// wantStderr is the whole phrase, not the flag spelling alone, so
			// the two refusal messages cannot pass for each other.
			wantStderr string
		}{
			{"queue-equals", []string{"--queue="}, `--queue was given an empty value`},
			{"queue-separate", []string{"--queue", ""}, `--queue was given an empty value`},
			{"positional-empty", []string{""}, `the queue name argument was empty`},
		} {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				projectDir := queueCliFixtureTempDir(t)
				spy := &queueVerbSpy{}
				queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
					spy.record(raw)
					return queueCliFixtureSuccessResponse(t, map[string]any{})
				})

				var out strings.Builder
				var errOut strings.Builder

				args := append([]string{"--project", projectDir}, tc.args...)
				got := verb.run(context.Background(), args, &out, &errOut)

				if seen := spy.seen(); len(seen) != 0 {
					t.Errorf("queue %s %v: sent %d request(s) to the daemon for a selector the caller never filled in: %q",
						verb.name, tc.args, len(seen), seen)
				}
				if got != 2 {
					t.Errorf("queue %s %v: exit = %d, want 2 — an empty selector value is an argument error, not a request for global scope; stdout=%q stderr=%q",
						verb.name, tc.args, got, out.String(), errOut.String())
				}
				if strings.Contains(out.String(), verb.success) {
					t.Errorf("queue %s %v: stdout claims success: %q", verb.name, tc.args, out.String())
				}
				if !strings.Contains(errOut.String(), tc.wantStderr) {
					t.Errorf("queue %s %v: stderr %q does not name which selector was empty (want %q)",
						verb.name, tc.args, errOut.String(), tc.wantStderr)
				}
			})
		}
	}
}

// TestQueueVerbs_NonEmptyName_StillReachesDaemon is the control. The
// refusal above must not cost either verb its ordinary path: a real name still
// builds a request, still carries that name, and still exits 0.
//
// Without this, deleting the body of both functions would pass the test above.
func TestQueueVerbs_NonEmptyName_StillReachesDaemon(t *testing.T) {
	t.Parallel()

	for _, verb := range []struct {
		name   string
		run    func(context.Context, []string, *strings.Builder, *strings.Builder) int
		wantOp string
	}{
		{"pause", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueuePause(ctx, a, o, e)
		}, "operator-pause"},
		{"resume", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueueResume(ctx, a, o, e)
		}, "operator-resume"},
	} {
		for _, form := range []struct {
			name string
			args []string
		}{
			{"positional", []string{"investigate"}},
			{"queue-flag", []string{"--queue", "investigate"}},
			{"queue-equals", []string{"--queue=investigate"}},
		} {
			t.Run(verb.name+"/"+form.name, func(t *testing.T) {
				t.Parallel()

				projectDir := queueCliFixtureTempDir(t)
				var capturedOp string
				var capturedQueue string
				queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
					msg := queueCliFixtureDecodeRequest(t, raw)
					queueCliFixtureCapture(t, msg, "op", &capturedOp)
					queueCliFixtureCapture(t, msg, "queue", &capturedQueue)
					return queueCliFixtureSuccessResponse(t, map[string]any{})
				})

				var out strings.Builder
				var errOut strings.Builder

				args := append([]string{"--project", projectDir}, form.args...)
				got := verb.run(context.Background(), args, &out, &errOut)

				if got != 0 {
					t.Errorf("queue %s %v: exit = %d, want 0; stderr=%q", verb.name, form.args, got, errOut.String())
				}
				if capturedOp != verb.wantOp {
					t.Errorf("queue %s %v: op = %q, want %q", verb.name, form.args, capturedOp, verb.wantOp)
				}
				if capturedQueue != "investigate" {
					t.Errorf("queue %s %v: queue = %q, want %q", verb.name, form.args, capturedQueue, "investigate")
				}
			})
		}
	}
}

// TestQueueVerbs_NoSelectorAtAll_KeepsUsageError pins the boundary the
// fix must not cross. Giving NO selector is a different act from giving an
// empty one, and it keeps the usage error it already had — not the new
// empty-value wording, which would tell a caller their value was empty when
// they typed no value at all.
func TestQueueVerbs_NoSelectorAtAll_KeepsUsageError(t *testing.T) {
	t.Parallel()

	for _, verb := range []struct {
		name string
		run  func(context.Context, []string, *strings.Builder, *strings.Builder) int
	}{
		{"pause", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueuePause(ctx, a, o, e)
		}},
		{"resume", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueueResume(ctx, a, o, e)
		}},
		{"recover", func(ctx context.Context, a []string, o, e *strings.Builder) int {
			return cli.RunQueueRecover(ctx, a, o, e)
		}},
	} {
		t.Run(verb.name, func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)

			var out strings.Builder
			var errOut strings.Builder

			got := verb.run(context.Background(), []string{"--project", projectDir}, &out, &errOut)

			if got != 2 {
				t.Errorf("queue %s bare: exit = %d, want 2; stderr=%q", verb.name, got, errOut.String())
			}
			if !strings.Contains(errOut.String(), "usage:") {
				t.Errorf("queue %s bare: stderr %q lost its usage error", verb.name, errOut.String())
			}
			if strings.Contains(errOut.String(), "empty value") || strings.Contains(errOut.String(), "was empty") {
				t.Errorf("queue %s bare: stderr %q tells a caller who typed no value that their value was empty", verb.name, errOut.String())
			}
		})
	}
}

// TestQueueVerbs_RecoverNonEmptyName_StillReachesDaemon is recover's half of the
// control above. It is separate because recover is the only verb of the three
// that reads its response: a payload with no durable receipt is refused, so the
// fixture has to answer with one.
func TestQueueVerbs_RecoverNonEmptyName_StillReachesDaemon(t *testing.T) {
	t.Parallel()

	for _, form := range []struct {
		name string
		args []string
	}{
		{"positional", []string{"investigate"}},
		{"queue-flag", []string{"--queue", "investigate"}},
		{"queue-equals", []string{"--queue=investigate"}},
	} {
		t.Run(form.name, func(t *testing.T) {
			t.Parallel()

			projectDir := queueCliFixtureTempDir(t)
			var capturedOp string
			var capturedQueue string
			queueCliFixtureStartEchoServer(t, projectDir, func(raw []byte) []byte {
				msg := queueCliFixtureDecodeRequest(t, raw)
				queueCliFixtureCapture(t, msg, "op", &capturedOp)
				queueCliFixtureCapture(t, msg, "queue", &capturedQueue)
				return queueCliFixtureSuccessResponse(t, map[string]any{
					"queue":         "investigate",
					"result":        "no-op",
					"receipt":       map[string]any{"receipt_id": "r-1"},
					"rearmed":       []string{},
					"rearmed_count": 0,
				})
			})

			var out strings.Builder
			var errOut strings.Builder

			args := append([]string{"--project", projectDir}, form.args...)
			got := cli.RunQueueRecover(context.Background(), args, &out, &errOut)

			if got != 0 {
				t.Errorf("queue recover %v: exit = %d, want 0; stderr=%q", form.args, got, errOut.String())
			}
			if capturedOp != "queue-recover" {
				t.Errorf("queue recover %v: op = %q, want %q", form.args, capturedOp, "queue-recover")
			}
			if capturedQueue != "investigate" {
				t.Errorf("queue recover %v: queue = %q, want %q", form.args, capturedQueue, "investigate")
			}
		})
	}
}
