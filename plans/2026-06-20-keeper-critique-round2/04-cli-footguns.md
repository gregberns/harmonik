# Keeper CLI Footguns — Round 2 (CLI/ergonomics lens)

Reviewed: `cmd/harmonik/keeper_cmd.go`, `keeper_enable_doctor_cmd.go`, `goalkeeper_cmd.go`,
`main.go` keeper dispatch (lines 437–488, 574–586). Round-1 positional fix (hk-nbft) is
respected — this report covers the rest of the surface.

---

## F1 — `--project` default = `os.Getwd()` is a SILENT cross-CWD footgun (HIGH)

The round-1 README bug 2a is **still live everywhere**, and worse on the marker verbs.

Every keeper command resolves the project dir from `os.Getwd()` when `--project` is
omitted:
- watcher: `keeper_cmd.go:118` (`projectDir, err := os.Getwd()`)
- marker verbs (set/clear/restart-now/ping/await-ack): `parseKeeperMarkerArgs` →
  `keeper_cmd.go:351`, and the inline copies at `:482-488` (ping) and `:556-563` (await-ack)
- enable/doctor: `:154-161` / `:446-453`
- goal-keeper: `goalkeeper_cmd.go:63-70`

A captain or sub-agent that runs `harmonik keeper restart-now --agent captain` from a
**worktree CWD** (the daemon routinely leaves agents in worktrees — see the project's own
"CWD drift breaks br/harmonik" rule) writes/reads `.harmonik/keeper/` under the *wrong*
project root. restart-now then can't find the live pane/handoff/sid and fails (or, for
set-dispatching, writes a marker the real watcher never sees → the keeper acts mid-dispatch).
This is the operator's classic "it silently did nothing" / "marker written under the wrong
project dir" pain — round 1 even called it out as the original restart-now bug
(`keeper_cmd.go:413-416`), but the *root enabler* (CWD-derived default) was never removed.

**Worse: the marker verbs do NOT `filepath.Abs` the `--project` value.** enable
(`:162-167`), doctor (`:454-459`), and goal-keeper (`:71-75`) all normalize with
`filepath.Abs`. The marker path builders (`parseKeeperMarkerArgs`, ping, await-ack) pass
the raw flag string straight into `filepath.Join(projectDir, ".harmonik", "keeper", …)`
(`internal/keeper/gates.go:12-25`, `gauge.go:28`). So `--project ./harmonik` or any
relative/symlink path resolves differently than the `os.Getwd()`-absolute path the watcher
uses — two commands "agree" on `--project` yet touch different files. This is an
inconsistency *between keeper subcommands* on the same flag.

**Fix now:** (a) `filepath.Abs` the resolved project dir in `parseKeeperMarkerArgs`, ping,
and await-ack for parity with enable/doctor — cheap, removes the relative-path mismatch.
(b) Stronger: make destructive verbs (restart-now, set/clear-dispatching) refuse to derive
the project from CWD when CWD is not a harmonik root (no `.harmonik/` present) — fail loud
with "pass --project" instead of silently writing into a worktree. Requiring `--project`
outright would break the common interactive case, so prefer the validate-or-Abs path.

---

## F2 — Inert/misleading `--warn-pct` / `--act-pct` are STILL exposed (MEDIUM)

Round 1 said the pct flags are inert/misleading on 1M windows. The hk-5da7 change
(`keeper_cmd.go:81-108, 159-170`) **partially** fixed this: an *explicitly set*
`--warn-pct/--act-pct` now feeds the pct-ceil seam and flows through
`min(abs, ceil*window)`, with a loud "honoring …" line. Good.

But three footguns remain:

1. **The flag still carries a default of 80/90** (`:65-66`) that is *only* honored when
   `fs.Visit` sees it explicitly set. So the help text and `--help` advertise
   "default 80 / default 90" yet the default is *never applied* — only an explicit value
   does anything. A reader who relies on the documented default gets silent no-op behavior
   (the exact misleading-default class round 1 flagged). The startup banner
   (`:208-209`) and the Cycler/Watcher configs (`ActPct: float64(actPctFlag)`,
   `WarnPct: float64(warnPctFlag)`, `:221`/`:246`) are *also* fed the raw 80/90 default
   regardless of whether the pct path is active — so the printed "warn-pct=80 act-pct=90"
   is reported even when the abs band is what actually fires.

2. **No tighten-only clamp.** The comment claims "a higher one is harmlessly capped by abs"
   — true for *act*, but an operator who sets `--act-pct 99` on a 1M window expecting a
   *later* restart gets the abs cap (215k ≈ 21%) instead, silently *earlier* than asked.
   The flag's stated semantics ("percentage that triggers action") don't match behavior.
   Recommend: clamp to tighten-only and SAY so, or rename to `--warn-pct-ceil`.

3. **No CLI surface for `force-act-abs-tokens`** (`:154-158`, config-only) while the pct
   flags exist — asymmetric. Minor.

**Fix now:** drop the fake 80/90 defaults to 0 (so help reads "unset → use abs band") and
make the startup banner print the *effective resolved* band (abs + any ceil), not the raw
flag. Removing the flags entirely is the cleaner call but is a published-surface change —
surface to operator.

---

## F3 — A typo'd verb silently degrades to watcher-mode-with-positional (MEDIUM)

`main.go:463-487`: the keeper verb switch matches only the 7 known verbs. **Any
unrecognized first token falls through to `runKeeperSubcommand(subArgs)`**, where it lands
as a positional and is rejected by `resolveKeeperAgent` (`keeper_cmd.go:321-325`) with
exit 2. So `harmonik keeper restrt-now --agent captain` (typo) prints
*"unexpected positional argument(s) 'restrt-now' — this command is flag-only"* — a
confusing message that doesn't say "unknown subcommand: restrt-now". The operator thinks
the *flag* form is wrong, not that they fat-fingered the verb. There's no
"did you mean restart-now?" and no list of valid verbs. For a destructive command (a
mistyped restart-now silently does nothing) this is a real recovery footgun.

**Fix now:** add a `default:` case to the verb switch that prints "unknown keeper
subcommand %q" + the verb list (keeperTopUsage) and exits 2. Cheap, removes the misleading
"flag-only" message for the common typo case.

---

## F4 — `goal-keeper` exit-code inconsistency: positional rejection is exit 1, keeper is exit 2 (LOW)

`goalkeeper_cmd.go:57-60` rejects unexpected positionals with **exit 1**; every keeper verb
rejects positionals with **exit 2** (`resolveKeeperAgent` :324). Same class of misuse, two
exit codes across sibling subcommands. A script that branches on exit 2 = "CLI misuse" will
misclassify goal-keeper. Also `goal-keeper` parse error → exit 1 (`:53`) vs keeper verbs'
exit 2 for unrecognized flags. Harmonize on 2 = usage/misuse.

---

## F5 — restart-now / set-dispatching FAIL-OPEN on a wrong-project marker (HIGH, ties to F1)

This is the failure *mode* F1 produces, called out separately because it's the operator's
named recurring pain. `set-dispatching` (`keeper_cmd.go:373-383`) writes the marker and
returns 0 with **no verification that a watcher for that agent/project is actually live**.
If the CWD/`--project` is wrong (F1), or no keeper watcher is running for that agent, the
marker is written successfully (exit 0, "success") yet protects nothing — the keeper either
isn't watching that path or doesn't exist. The operator gets a green exit and a false sense
the dispatch is now guarded. There is no `keeper status`/liveness probe that
set-dispatching could consult. (restart-now is better here — it's synchronous and *does*
fail loud on no-pane — but it still silently targets the wrong dir under F1.)

**Fix:** have set/clear-dispatching warn (non-fatal) when no `<agent>.lock` /live keeper is
present for the resolved project, so "I set the marker but nobody's watching" is visible.
Pairs naturally with the F1 fix.

---

## Lower-severity notes

- **`enable`/`doctor` accept a bare positional agent** (`parseKeeperEnableArgs` :128-137,
  `parseKeeperDoctorArgs` :424-433) while every *other* keeper verb is flag-only (hk-5da7).
  This is the exact inconsistency hk-nbft targets, but note it spans BOTH enable and doctor,
  and the "flag wins positional" rule means `enable --agent X Y` silently ignores `Y`
  (it goes into `rest`, unused) — a silent-drop the hk-nbft fix should also close.
- **doctor "binary stale" uses mtime > 30 days** (`:504`) — a freshly `go install`'d binary
  on a checkout with an old mtime, or a Nix/store path, can false-positive. Cosmetic.
- **`await-ack` reuses exit 1 for "argument error OR working-dir error"** but exit 3 for
  timeout/no-pane (`:578-583`). The no-pane case is folded into 3 (timeout) — an operator
  can't distinguish "keeper dead" from "wrong pane name" by exit code alone.

---

## Summary table

| ID | Footgun | Severity | Fix now? |
|----|---------|----------|----------|
| F1 | `--project` defaults to CWD + marker verbs skip `filepath.Abs` → wrong `.harmonik/keeper/` dir | HIGH | YES — Abs-normalize + validate-or-require |
| F5 | set-dispatching fails open (exit 0) when no watcher / wrong dir | HIGH | YES (with F1) |
| F2 | `--warn-pct/--act-pct` fake defaults + no tighten-only clamp; banner prints raw not effective band | MEDIUM | YES — defaults→0, print effective band |
| F3 | Typo'd verb → misleading "flag-only" instead of "unknown subcommand" | MEDIUM | YES — add `default:` verb case |
| F4 | goal-keeper exit 1 vs keeper exit 2 for same misuse class | LOW | optional |
