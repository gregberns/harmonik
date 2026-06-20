# 14 — Keeper CLI Hardening (W5 + W7)

Bead: **hk-x7s** (`keeper CLI hardening: --project abs-normalize on marker verbs +
set-dispatching fail-open + pct-flag honesty + unknown-subcommand default`).
Guardrails followed: report `04-cli-footguns.md` (F1/F2/F3/F5) and the adversary's
`08-adversary-harm.md` §W5/§W7 (KEEP the `os.Getwd()` default; Abs-normalize only;
advisory warn only; banner/help/`default:`-case changes only).

## W5 (HIGH) — marker-verb `--project` parity + set-dispatching fail-open warning

### Abs-normalize at the chokepoint
`parseKeeperMarkerArgs` (the shared resolver for `set-dispatching` /
`clear-dispatching` / `restart-now`) now runs the resolved project dir through a new
single-chokepoint helper `normalizeProjectDir` → `filepath.Abs`. The two inline
copies (`ping`, `await-ack`, which don't go through `parseKeeperMarkerArgs`) call the
same helper. This brings all marker verbs to parity with `enable`/`doctor`/`goal-keeper`,
which already `filepath.Abs`-normalize. A relative `--project` (or a worktree CWD)
now resolves to the SAME `.harmonik/keeper/` dir the watcher uses (the watcher derives
its dir from `os.Getwd()`, always absolute). This closes round-1 bug-2a / report 04 F1.

**The `os.Getwd()` default is UNCHANGED** — adversary 08 §W5 verified it is load-bearing
for the live captain/dispatch scripts. Only the normalization was added; `--project` is
NOT required, and there is NO hard-fail when CWD lacks `.harmonik/`.

### set-dispatching fail-open WARNING (report 04 F5)
New `keeper.LiveKeeperPresent(projectDir, agent)` — a READ-ONLY liveness probe that
opens `<agent>.lock` and attempts a NON-BLOCKING shared `flock`: failure with
EAGAIN/EWOULDBLOCK means a live keeper holds the exclusive lock (present=true); success
(immediately released) or a missing lockfile means no live keeper (present=false). The
probe never disturbs a live keeper. `runKeeperSetDispatching` calls it after writing the
marker and emits a non-fatal stderr `WARNING — no live keeper found …` when absent.
**Exit stays 0** (fail-open): a keeper may legitimately start later.

## W7 (MEDIUM) — pct-flag honesty + unknown-subcommand default

1. **Fake 80/90 defaults → 0.** `--warn-pct`/`--act-pct` now default to `0` (= "unset →
   use abs band"), matching the actual behavior (only an EXPLICITLY-set flag flows
   through the pct-ceil seam via `fs.Visit`). The downstream `WarnPct`/`ActPct`
   `applyDefaults` already treat `<= 0` as unset → compiled default (80/90), so the
   live abs band is byte-unchanged; the help text is now honest. Help/doc comments
   updated accordingly.
2. **Honest effective-band banner.** New exported `keeper.EffectiveBandTokens` resolves
   the warn/act/force absolute tokens the gate ACTUALLY fires on (defaults applied + the
   shared tighten-only `min(abs, pctCeil*window)` formula). The startup banner now prints
   `effective band: warn=… act=… force=… tokens` (plus a `[pct ceils: …, tighten-only]`
   note only when a pct flag was set) instead of the raw `warn-pct=80 act-pct=90`.
3. **Tighten-only clamp.** No new code needed — `minAbsOrPctCeil` already returns the
   pct-based value ONLY when it is `< abs`, so a pct can move the threshold EARLIER but
   never LATER than the abs band. The help text now states this explicitly, and
   `EffectiveBandTokens` is pinned by test to honor it.
4. **`default:` unknown-subcommand case** in `cmd/harmonik/main.go` keeper dispatch: a
   typo'd verb (e.g. `restrt-now`) now prints `unknown keeper subcommand "restrt-now"` +
   `keeperTopUsage` and exits 2, instead of the misleading "this command is flag-only"
   fall-through. Tokens starting with `-` still fall through to watcher-mode/help.

## Tests
- `cmd/harmonik/keeper_cli_hardening_hkx7s_test.go`: relative `--project` resolves to the
  same dir as absolute; set-dispatching warns (exit 0) with no live keeper; unknown
  subcommand exits non-zero with the right message and no "flag-only".
- `internal/keeper/live_keeper_present_hkx7s_test.go`: `LiveKeeperPresent` true while
  locked / false after release / false for missing lock / false for traversal agent;
  `EffectiveBandTokens` defaults + tighten-only.

## Verification
`go build ./...`, `go vet ./cmd/harmonik/... ./internal/keeper/...`,
`go test ./cmd/harmonik/... ./internal/keeper/...` — all GREEN. `gofumpt -l` clean.
No live-tmux smoke run (per brief). Did NOT touch cycle.go/watcher.go/crewstart.go/
captain-launch.sh.

## Files changed
- `cmd/harmonik/keeper_cmd.go` — Abs chokepoint, ping/await-ack normalize, set-dispatching
  warning, pct defaults→0, honest banner, help text.
- `cmd/harmonik/main.go` — `default:` unknown-subcommand case.
- `internal/keeper/keeper.go` — `LiveKeeperPresent`.
- `internal/keeper/thresholds.go` — `EffectiveBandTokens`.
- `cmd/harmonik/keeper_cli_hardening_hkx7s_test.go`, `internal/keeper/live_keeper_present_hkx7s_test.go` — new tests.
