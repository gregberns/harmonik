# R4 — CE6 (hk-9mpk) Review: verified-restart wrapper routing

**Branch:** worktree-agent-abe7f050c4a02b7e1 (57dda95a) vs main 530773c3
**Verdict: APPROVE-WITH-NITS**

Scope: only `scripts/captain-tools/captain-launch.sh` + its byte-identical embedded
mirror `cmd/harmonik/captain-tools/captain-launch.sh`. No .go logic, no keeper code.
No scope creep.

## 1. Contract correctness — PASS
Wrapper (`keeper-restart-verified.sh`) contract: `<agent> [--project DIR] [--timeout] [--poll]`.
Generated file emits: `exec "$WRAPPER" "$CAP_NAME" --project "$HK_PROJECT" "$@"`.
Agent is positional `$1`, `--project DIR` next, pass-through `$@` adds only post-positional
flags — matches the wrapper's arg loop (lines 42–58) exactly. `$CAP_NAME` is always `$1`,
so the wrapper's `"${1:-}" == -*` guard (line 37) never false-trips. Correct arg order.

## 2. Path resolution (KEY CHECK) — PASS, but comment is misleading
The stated risk (does the generated file in `.harmonik/cognition/` resolve the wrapper via
its own `$0`?) **does not materialize**. The heredoc is **unquoted** (`<<EOF`), so
`$VERIFIED_RESTART_WRAPPER` is expanded **at generation time**, baking an ABSOLUTE path into
the generated file. `VERIFIED_RESTART_WRAPPER="$(cd "$(dirname "$0")" && pwd)/keeper-restart-verified.sh"`
is computed in captain-launch.sh's context, where `$0` IS captain-launch.sh in
`scripts/captain-tools/` (wrapper co-located, confirmed). The generated file's own `$0`
(in `.harmonik/cognition/`) is NEVER used for resolution. Same proven pattern as the existing
`captain-respawn.sh`. `\$@` correctly escaped to literal `$@`.

**NIT (non-blocking, doc-only):** the source comment (captain-launch.sh:~91-96 of diff) reads
"resolves the wrapper by the repo scripts/captain-tools/ path (dirname "$0")" — this implies
the GENERATED file uses `$0`, which it does not (and must not, or it would break). The
resolution happens at generation time in the parent. Recommend rewording to "the generation-time
absolute path of the co-located wrapper is baked in." Cosmetic; behavior is correct.

## 3. No regression — PASS
Stable lowercase `--session-id` minting (SID, line 63) untouched. `--respawn-cmd "$RESPAWN_SCRIPT"`
dead-pane self-heal still armed (line 117). `$CAP_NAME` (line 54) and `$HK_PROJECT` (required,
line 46) are both bound well before the new block at section 3b. New block inserted cleanly
between section 3 (respawn) and section 4 (keeper arm).

## 4. Sync guard — PASS
`diff scripts/... cmd/harmonik/...` → BYTE-IDENTICAL. `go test ./cmd/harmonik/ -run
'CaptainTools|CaptainLaunch|EmbedInSync'` → ok.

## 5. No scope creep — PASS

## Defects
None blocking. One doc nit (#2 above): misleading comment re `dirname "$0"`, file:
`scripts/captain-tools/captain-launch.sh` ~section 3b comment. Severity: trivial/cosmetic.
