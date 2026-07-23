---
description: Validate the just-committed code with the full make check gate; fix and re-commit if red.
---

# /check — post-commit validation gate

Git hooks are OUT (lefthook is uninstalled; `.git/hooks/` holds only samples). Validation is now **agent-driven**, and this command is the replacement: run it after every non-trivial `git commit` to verify the code you just committed actually passes our checks.

## What to do

1. Run the full gate on the committed tree:

   ```bash
   make check
   ```

   `check` is the tier-2 gate: `fmt-check`, `go vet`, `go build`, full `golangci-lint`, `go test -race`, `go mod tidy` drift check, coverage gate, and `govulncheck`.

2. Read the output.
   - **Green (exit 0):** done. The commit stands.
   - **Red (non-zero):** something the commit introduced is broken. FIX THE ROOT CAUSE, then re-commit (amend or a follow-up fix commit). Do NOT suppress the finding, do NOT lower the gate, and do NOT `--no-verify` your way past it. Re-run `/check` until it comes back green.

## If a full check per commit is too slow

`make check` is a few minutes. If running it after *every* commit drags, split it:

- **`make check-fast`** per commit — fmt-check, vet, build, `golangci-lint --new-from-rev`, and `go test -short` on changed packages (~15s target).
- **`make check`** at push / milestone boundaries — the full race + coverage + vuln gate.

Fast-per-commit is a convenience, not a license to skip the full gate: `make check` must be green before you push.
