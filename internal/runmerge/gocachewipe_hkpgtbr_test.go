package runmerge

// gocachewipe_hkpgtbr_test.go — the merge gate's behaviour when another process
// deletes the Go build cache out from under it (hk-pgtbr / hk-gjbpp).
//
// The fixture below is REAL compiler output, captured on 2026-07-23 by running
// `go build ./internal/... ./cmd/...` in this repo against a GOCACHE that a
// concurrent `go clean -cache` was wiping every 4 seconds. That run exited 1
// with 30 `could not import` lines; the same command against a private GOCACHE,
// with the identical concurrent wipe pointed at the other directory, exited 0
// with no output. The mechanism is not a hypothesis.
//
// Every test here asserts BOTH directions: the vanished-cache signature is
// retried / classified retryable, and a genuine compile error is NOT — because a
// guard that treats every red as transient is worse than no guard.

import (
	"context"
	"strings"
	"testing"
	"time"
)

// coldCacheOutput is the captured signature (paths rewritten to the shared
// default GOCACHE the daemon and crews actually collide on).
const coldCacheOutput = `# github.com/gregberns/harmonik/cmd/harmonik/supervise
cmd/harmonik/supervise/assetskew.go:20:2: could not import context (open /Users/gb/Library/Caches/go-build/ae/ae6a37ef2ad6fd74e9c42f26bc2f888044d48298e2e4e2ad6e6c681d88205ac7-d: no such file or directory)
cmd/harmonik/supervise/assetskew.go:21:2: could not import fmt (open /Users/gb/Library/Caches/go-build/7d/7d74fbbfe6eadca42dba3961764792024e96f2590e6637c1a733bd87c9afb24e-d: no such file or directory)
cmd/harmonik/supervise/config.go:19:2: too many errors`

// genuineCompileOutput is what a real regression looks like: no cache path, no
// "no such file". A bead that produces this MUST be rejected, not retried.
const genuineCompileOutput = `# github.com/gregberns/harmonik/internal/runmerge
internal/runmerge/merge.go:412:9: undefined: notAFunction
internal/runmerge/merge.go:415:2: declared and not used: buildDir`

type stubErr string

func (e stubErr) Error() string { return string(e) }

// scriptedRunner returns a mergeBuildRunner that replays outs/errs in order and
// records how many times it was called.
func scriptedRunner(calls *int, outs []string, errs []error) mergeBuildRunner {
	return func(_ context.Context, _ string, _ []string) ([]byte, error) {
		i := *calls
		*calls++
		if i >= len(outs) {
			i = len(outs) - 1
		}
		return []byte(outs[i]), errs[i]
	}
}

// TestRunMergeBuildStep_RetriesOnlyTheVanishedCacheSignature is the both-ways
// assertion on the retry loop: the cache-wipe signature is retried until it
// clears, and a genuine compile error is returned on the FIRST attempt.
func TestRunMergeBuildStep_RetriesOnlyTheVanishedCacheSignature(t *testing.T) {
	t.Parallel()
	noWait := []time.Duration{0, 0} // two retries, no sleeping in a unit test

	t.Run("vanished cache is retried and recovers", func(t *testing.T) {
		t.Parallel()
		calls := 0
		run := scriptedRunner(&calls,
			[]string{coldCacheOutput, coldCacheOutput, ""},
			[]error{stubErr("exit status 1"), stubErr("exit status 1"), nil})

		out, err := runMergeBuildStep(context.Background(), run, "/x", []string{"build", "./..."}, noWait)
		if err != nil {
			t.Fatalf("want recovery on the third attempt, got err=%v out=%q", err, out)
		}
		if calls != 3 {
			t.Fatalf("want 3 attempts (initial + 2 retries), got %d", calls)
		}
	})

	t.Run("genuine compile error is NOT retried", func(t *testing.T) {
		t.Parallel()
		calls := 0
		// Attempts 2 and 3 are scripted to SUCCEED. If the loop retried, the
		// step would report success and a real regression would land on main.
		run := scriptedRunner(&calls,
			[]string{genuineCompileOutput, "", ""},
			[]error{stubErr("exit status 1"), nil, nil})

		out, err := runMergeBuildStep(context.Background(), run, "/x", []string{"build", "./..."}, noWait)
		if err == nil {
			t.Fatal("genuine compile error was retried away — a real regression would merge")
		}
		if calls != 1 {
			t.Fatalf("want exactly 1 attempt for a genuine failure, got %d", calls)
		}
		if !strings.Contains(string(out), "undefined: notAFunction") {
			t.Fatalf("want the genuine compiler output preserved, got %q", out)
		}
	})

	t.Run("retry budget is bounded", func(t *testing.T) {
		t.Parallel()
		calls := 0
		// The wipe never stops. The step must give up, not spin.
		run := scriptedRunner(&calls,
			[]string{coldCacheOutput},
			[]error{stubErr("exit status 1")})

		if _, err := runMergeBuildStep(context.Background(), run, "/x", []string{"vet", "./..."}, noWait); err == nil {
			t.Fatal("want failure when the cache wipe never stops")
		}
		if calls != 3 {
			t.Fatalf("want the schedule honoured exactly (3 attempts), got %d", calls)
		}
	})

	t.Run("a cancelled context stops retrying", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		calls := 0
		run := scriptedRunner(&calls,
			[]string{coldCacheOutput},
			[]error{stubErr("exit status 1")})

		if _, err := runMergeBuildStep(ctx, run, "/x", []string{"build", "./..."},
			[]time.Duration{time.Hour}); err == nil {
			t.Fatal("want the initial failure returned")
		}
		if calls != 1 {
			t.Fatalf("want no retry under a cancelled context, got %d attempts", calls)
		}
	})
}

// TestIsRetryableReason_ColdCacheMergeBuildIsInfrastructure asserts the
// classification both ways: a merge_build_failed carrying the vanished-cache
// signature is infrastructure (retry), any other merge_build_failed is the
// bead's fault (reject).
func TestIsRetryableReason_ColdCacheMergeBuildIsInfrastructure(t *testing.T) {
	t.Parallel()
	// The reason string the gate actually builds (merge.go runMergeBuildGate).
	coldReason := "merge_build_failed (go build): exit status 1\n" + coldCacheOutput
	genuineReason := "merge_build_failed (go build): exit status 1\n" + genuineCompileOutput

	for _, tc := range []struct {
		name   string
		reason string
		want   bool
	}{
		{"vanished cache is retryable", coldReason, true},
		{"genuine compile error is not", genuineReason, false},
		{
			"vet step is classified the same way",
			"merge_build_failed (go vet): exit status 1\n" + coldCacheOutput, true,
		},
		{"push failure stays non-retryable", "push_failed: remote rejected", false},
		{"rebase conflict stays retryable", "rebase_conflict: patch does not apply", true},
		{"non-ff stays retryable", "non_ff_merge (attempt 1)", true},
		{"fmt gate stays retryable", "merge_fmt_failed: gofmt -l", true},
		{"strip-run-context stays non-retryable", "strip_run_context_failed: bad trailer", false},
		{
			"a bead whose own text mentions the cache is still rejected",
			"push_failed: could not import foo: no such file", false,
		},
	} {
		if got := IsRetryableReason(tc.reason); got != tc.want {
			t.Errorf("%s: IsRetryableReason = %v, want %v", tc.name, got, tc.want)
		}
	}
}
