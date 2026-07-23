---
description: Validate the code you just committed with the repo's delta-scoped gate; fix and re-commit if red.
---

# /check — post-commit validation gate

Git hooks are OUT (lefthook is uninstalled; `.git/hooks/` holds only samples). Validation is now **agent-driven**, and this command is the replacement: run it after every non-trivial `git commit` to verify the code you just committed actually passes our checks.

## What to do

1. Run the fast per-commit gate on what you just committed:

   ```bash
   make check-fast
   ```

   `check-fast` is Tier 1: `fmt-check` (fail-closed), `go vet`, `go build`, `golangci-lint --new-from-rev=HEAD~1`, and `go test -short`. The `--new-from-rev` scope is the point — it judges **the lines your commit changed**, not the whole repo, so it answers "did my change pass" and can actually go green.

2. Read the output.
   - **Green (exit 0):** done. The commit stands.
   - **Red (non-zero):** something your commit introduced is broken. FIX THE ROOT CAUSE, then re-commit (amend or a follow-up fix commit). Do NOT suppress the finding, do NOT lower the gate, do NOT `--no-verify`. Re-run `/check` until green.

## Before you push / at a milestone

```bash
make check-short   # CI Tier 2 merge gate: fmt-check + golangci-lint --new-from-rev=origin/main + go test -short -race
make check-full    # Tier 3: + integration + scenario + crash suites (~10–15 min)
```

`check-short` is exactly what CI gates merges on; it must be green before you push.

## Do NOT use bare `make check` as the pass/fail gate

`make check`'s lint step is a **full** `golangci-lint run` (no `--new-from-rev`). By design that reports **~2,000 pre-existing legacy findings** and always exits non-zero (see `Makefile` §"LINT IS A MERGE-TIME GATE" ~lines 719–723) — it is a whole-repo legacy-debt audit / trend view, **not** a per-commit pass/fail gate. Its other steps (`-race` tests, `go mod tidy` check, coverage gate, `govulncheck`) are useful, but judge whether *your commit* passes by the `--new-from-rev` gates above, never by the full-lint exit code.
