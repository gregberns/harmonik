# Keeper Critique — Shell-Hook & Integration-Boundary Lens

**Critic angle:** the cross-language, cross-process seam between the Go keeper
(`internal/keeper/`) and the **five shell hooks** that bridge it to Claude
Code's hook system + tmux. Verdict up front: **the boundary is the single most
fragile part of the keeper. It is an under-observable, file-marker IPC protocol
shared between bash and Go, coupled to undocumented Claude-Code internals, with
silent fail-open everywhere — so when it breaks it breaks invisibly, which is
exactly the operator's lived experience.**

---

## 1. The integration topology

The keeper is not "a Go program." It is a Go poller talking to a Claude session
**only** through files in `.harmonik/keeper/<agent>.*`, written by bash hooks
that Claude Code invokes. There is **no direct channel**. Every signal crosses
the bash↔file↔Go seam:

| Channel | Writer (bash hook) | Claude event | Go reader | Purpose |
|---|---|---|---|---|
| `<agent>.ctx` | `keeper-statusline.sh` | `statusLine` (every UI repaint, ~2s) | `gauge.go ReadCtxFile` | pct / tokens / window / session_id gauge |
| `<agent>.sid` | `keeper-sessionstart-hook.sh` | `SessionStart` (startup/clear/resume) | `gauge.go` (PRIMARY identity) | authoritative session_id |
| `<agent>.idle` | `keeper-stop-hook.sh` | `Stop` (await-input boundary) | `gates.go CrispIdle` | idle boundary signal |
| `<agent>.precompact` | `keeper-precompact-hook.sh` | `PreCompact` (before auto-compaction) | `watcher.go`→`RunForPrecompact` | compaction backstop |
| `<agent>.managed` | Go (`keeper.go`) + cycle re-arm | — | both bash hooks AND Go | opt-in + session binding |

The action back to Claude (`/clear`, `/session-resume`) goes out a **sixth**
channel entirely: tmux `paste-buffer` + `send-keys` (`injector.go`). So the
keeper reads via files-written-by-hooks and writes via tmux keystroke
injection. Two completely different, both-fragile transports.

This is **six integration surfaces** for one logical job ("watch context, clear
when full"). Each is an independent failure mode.

---

## 2. Per-hook environmental assumptions (each = a failure mode)

### `keeper-statusline.sh` (the gauge — load-bearing)
- **Assumes the field path `.context_window.used_percentage`** (line 66) — an
  *empirically reverse-engineered* Claude-Code statusLine JSON schema, not a
  documented contract. A harness update renaming/moving this field silently
  zeroes the gauge: PCT becomes empty → `exit 0` (line 69), gauge never updates
  → Go sees a stale gauge → `continue` past all triggers. **The keeper goes
  blind with zero error surfaced.**
- **Assumes `jq` on PATH.** No jq → every `jq` call returns empty → PCT empty →
  silent `exit 0`. Keeper blind, no signal.
- **Window-size inference is a pile of heuristics** (lines 84–110): tries two
  JSON paths, then an env override, then *greps the model string for the
  literal `[1m]`* to guess 1,000,000. This is brittle string-matching on a
  display field. `claude-opus-4-8 [1m]` (this very session) depends on that
  grep; a format change to `(1M context)` breaks token-based gating and silently
  falls back to pct-only.
- **Agent-name derivation** falls back to `tmux display-message -p '#S'`. If the
  statusLine process is somehow not in tmux, AGENT="default" and every session
  writes the same `default.ctx` — the multi-writer `.ctx` clobber the comments
  themselves warn about (hk-67k, hk-igt).

### `keeper-sessionstart-hook.sh` (identity — load-bearing)
- Same jq assumption → no jq means **no `.sid` write**, so identity silently
  falls back to the race-prone gauge id — re-introducing exactly the
  multi-writer ambiguity `.sid` exists to eliminate (gauge.go:49-54).
- **No positional-arg fallback** for AGENT (unlike stop/precompact hooks), and
  the no-tmux branch `exit 0`s entirely. Inconsistent fallback chains across the
  five hooks (see §4).

### `keeper-stop-hook.sh` (idle signal)
- Pure `touch` of `.idle`. The **only** crisp-idle signal. If this hook is not
  installed, `CrispIdle` never returns true → the `MaybeRun` cycle path can
  **never fire** (act_pct path requires CrispIdle). A *missing* hook here =
  silently disabled keeper, not an error. There is no "is the Stop hook wired?"
  liveness check at runtime.

### `keeper-precompact-hook.sh` (backstop) — see §5, it is the worst.

### `hk-keeper.sh` (the *daemon* keeper — different thing, namespace collision)
- Confusingly named: this is the **daemon supervisor**, NOT the session-keeper.
  Sharing the "keeper" name across two unrelated subsystems is itself a
  comprehension hazard for operators already confused by the keeper.
- `set -euo pipefail` + a `while true` relaunch loop: liveness = `pgrep -f
  "harmonik --project $PROJ"`. A path with shell metacharacters or a second
  project whose path is a prefix would mis-match. `rm -f .../daemon.sock` (line
  77) is a destructive op gated only on pgrep — the fork-bomb-adjacent class.

---

## 3. Shared decision logic that drifts (Go ↔ bash duplication)

The `.managed` opt-in guard is **enforced in three places**:
1. `keeper-precompact-hook.sh` Gate 1 (line 95)
2. `keeper-stop-hook.sh` (implicitly — it always touches, no managed check
   actually — so the Stop hook touches `.idle` even for UNMANAGED sessions)
3. Go `cycle.go MaybeRun` Gate 1 (line 538) and `RunForPrecompact` Gate 1
   (line 982), explicitly commented "*defensive since the shell script also
   checks*" (precompact doc line 71).

This is **the same business rule re-implemented in two languages**. The
precompact doc literally says the Go check is "defensive since the shell script
also checks" — i.e. the authors know it's duplicated and chose belt-and-braces.
But belt-and-braces means the *can't-cycle bounded-fallback* contract (§5) now
spans both copies: the bash hook decides whether to block, the Go side decides
whether to clear — and if their notions of "managed" ever diverge (e.g. a
`.managed` file that's stale/foreign — a known failure signature), the bash hook
blocks compaction while Go refuses to cycle. The doc claims this is handled by
the "block at most once" marker, but that only bounds it to *one* lossy
compaction per wave; it does not make the two sides agree.

**Agent-name derivation** is duplicated 4× across hooks with **inconsistent
fallback chains** (statusline: env→tmux→default; sessionstart: env→alias→tmux→
*exit*; stop/precompact: env→alias→arg→tmux→default). Four slightly-different
copies of the same 8-line block. Any fix to the derivation rule must be applied
4 times or they drift. This is textbook accidental sprawl.

The **path-traversal guard** (`*/*|*..*`) is in precompact and sessionstart but
**absent from stop-hook and statusline** — those `touch`/write
`${KEEPER_DIR}/${AGENT}.idle|.ctx` with an unvalidated AGENT. Inconsistent
security posture across copies of the same pattern.

---

## 4. Error handling: silent fail-open is pervasive (and it is *why* it
"keeps breaking")

Every hook is designed to **never disrupt the session** — laudable for safety,
catastrophic for observability:

- statusline: bad field / no jq / NA → `exit 0`, no gauge, **no log, no event**.
- sessionstart: no jq / no sid / no tmux → `exit 0`, identity silently degraded.
- stop: `set -euo pipefail` but the final `touch` — if `mkdir -p` or `touch`
  fails (perms), `set -e` aborts the hook *non-zero*, which Claude Code may
  surface as a hook error to the user, the **one** place a hook can be noisy,
  and it's the trivial one.
- precompact: every gate is fail-open (`exit 0`).

The net effect: **when the keeper silently stops working, nothing in the system
says so.** The operator only discovers it when a pane overflows — the exact
outcome the keeper exists to prevent. There is no end-to-end "is the gauge
fresh AND identity bound AND hooks installed AND watcher polling?" healthcheck
that runs continuously; `keeper doctor` is a manual point-in-time probe. A
silent fail-open IPC with no liveness alarm is an **anti-pattern for a watchdog**
— a watchdog must be loud when it dies.

---

## 5. The PreCompact hook races the watcher — structurally

This is the sharpest finding. The PreCompact hook runs **synchronously inside
Claude** and must return *immediately* (block=exit 2 or allow=exit 0). The
watcher is an **async 5s poller** (`watcher.go:143` PollInterval default 5s).
The hook's only action is to write a marker and block; the actual cycle happens
on a *later* watcher tick. So:

1. PreCompact fires, hook writes `.precompact`, exits 2, compaction blocked.
2. Claude is now **frozen waiting** — compaction suppressed, no cycle yet.
3. Up to 5s later (one poll interval) the watcher *might* run `RunForPrecompact`.

But step 3 is **gated on a fresh, non-foreign gauge.** Look at `watcher.go`:
the precompact check (line 765) is **inside the fresh-gauge branch**, *after*
the `continue` statements for gauge-absent (623), gauge-stale (662), and
foreign-session (711). **If the gauge is stale or the session is foreign — both
known, frequent failure signatures — the watcher `continue`s and never even
looks at the `.precompact` marker.** The marker sits there. The "block at most
once" fallback then saves the session only on the *next* PreCompact fire by
falling through to lossy native compaction.

So the documented "intent-preserving backstop" **degrades to exactly the lossy
compaction it was built to prevent** precisely in the states the keeper is
already known to get stuck in (stale gauge after `/clear`, foreign `.managed`).
The race isn't a corner case; it's aligned with the system's main failure modes.

**Two things both trying to reset context?** Yes — when `MaybeRun` (act_pct
path) and PreCompact fire close together, both can call into the cycler in the
same tick (lines 754 and 766 run sequentially in one tick). The anti-loop /
`lastFiredSID` state is the only thing preventing a double `/clear`; correctness
of "don't clear twice" rests entirely on shared mutable state in the `Cycler`
that the bash side has no visibility into. The bash hook blocks native
compaction *and* the Go watcher may be mid-`MaybeRun` cycle — two reset paths,
coordinated only by an in-process Go mutex/field the hook can't see.

---

## 6. Coupling to Claude-Code internals that will break on harness updates

The keeper is welded to **undocumented or version-volatile** Claude-Code
surfaces:
- **statusLine JSON schema** — field path empirically verified (comment line 6
  of keeper-statusline.sh), plus dual-path probing for `context_window_size`
  "in some versions / nested in others" (line 85). They're already
  compensating for *observed* schema churn. This *will* drift again.
- **Hook event names** `PreCompact`/`SessionStart`/`Stop` and their
  `source=startup|clear|resume` semantics, and the claim "Stop fires only at
  await-input boundaries (verified by Anthropic)" (stop-hook line 4). All of
  these are harness contracts the keeper has no control over.
- **Exit-code 2 = decision:block** for PreCompact — a hook-protocol detail.
- **`/clear` + `/session-resume`** as slash commands delivered by simulated
  keystrokes — depends on the REPL accepting bracketed paste then a separate
  Enter (the hk-89g submit race the injector works around with settle+retry).
  Statusline-format, hook-protocol, slash-command-text, and paste-submit timing
  are **four** independent harness dependencies.

A Claude-Code release that touches any one of these silently degrades or breaks
the keeper, and (per §4) does so without an alarm.

---

## 7. Is the Go + 5-shell split justified?

**Partially, then no.** There is a real reason hooks must be shell: Claude Code
invokes hooks as `command` strings, so *something* shell-ish has to be the
entry point. That justifies thin shims. But these are **not thin shims** —
statusline is 122 lines of jq parsing, schema-probing, model-string grepping,
and window-size inference; precompact is 114 lines implementing a stateful
"block-at-most-once" protocol with its own marker lifecycle. **Real decision
logic lives in bash, untestable by the Go test suite**, duplicated against Go
copies, with no shared source of truth.

The justified design would be: hooks are ~5-line shims that pipe stdin to a
`harmonik keeper hook <event>` Go subcommand, which does ALL parsing, schema
handling, agent-name derivation, marker writes, and managed checks **in one
tested place**. Instead the logic is smeared across bash (untested) + Go
(tested but duplicating bash). The bash side has, as far as the file tree shows,
no unit tests at all — the `*_test.go` suite tests the Go marker helpers and
gauge parser, not the hooks that *produce* those markers and gauge files. So
the most failure-prone half of the integration is the **untested** half. That
directly answers the brief's core question #3: **everything is imperatively
tied together across a bash/Go seam that the test suite cannot exercise
end-to-end, which is why it keeps breaking.**

---

## 8. Summary of findings (severity-ranked)

| # | Finding | Severity |
|---|---|---|
| 5 | PreCompact backstop is `continue`'d-past on stale/foreign gauge → degrades to lossy compaction in the keeper's own known-broken states; sync-hook/async-poller race | **CRITICAL** |
| 4 | Pervasive silent fail-open + no continuous liveness alarm → keeper dies invisibly (the operator's lived experience) | **CRITICAL** |
| 6 | Welded to ≥4 undocumented/volatile Claude-Code surfaces (statusLine schema, hook protocol, slash text, paste-submit timing) | **HIGH** |
| 7 | Real logic in untested bash, duplicated against Go; no end-to-end test of the hook→file→Go seam | **HIGH** |
| 1 | Six independent integration channels for one job; each an independent failure mode | **HIGH** |
| 3 | `.managed` rule + agent-name derivation duplicated across bash/Go with drift (4 inconsistent fallback chains, inconsistent path-traversal guards) | **MEDIUM** |
| 2 | `hk-keeper.sh` (daemon supervisor) name-collides with session-keeper; `rm` socket gated only on pgrep | **MEDIUM** |

**One-line verdict:** The keeper's worst architectural flaw is its
integration boundary — a six-channel, silent-fail-open, bash↔file↔Go↔tmux IPC
welded to undocumented Claude-Code internals, with the load-bearing logic
living in untested shell and the PreCompact backstop racing the poller in
exactly the states the keeper already fails in; it should be collapsed to thin
shims over one tested `harmonik keeper hook` subcommand with a loud liveness
alarm.
