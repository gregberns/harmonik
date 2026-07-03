# C3 — Multi-tenant settings & global tooling — Design

> Pass 4 (`design`) of `fleet-portability`, component **C3**. Synthesis of two
> independent proposals, reconciled against the live tree (verified 2026-06-13).
> Scope: make harmonik's contributions to **shared global surfaces** — the global
> `~/.claude/settings.json` keeper hooks, `~/.claude/captain-tools/` scripts, and the
> `/tmp/hk-*` state files — project-namespaced so N fleets coexist on one machine
> without one project's bootstrap perturbing another's.

---

## 0. VERIFY-FIRST result — the critical risk, resolved

**RQ-C3 critical risk (research Risk #1): "Does the Claude Code harness allow N coexisting
Stop/PreCompact hooks, or does it merge/overwrite by hook-type?"**

**RESOLVED — YES, N coexisting groups are supported.** Verified two ways:

1. **Schema.** `hooks.<Event>` is a JSON **array of matcher-groups** — each
   `{matcher, hooks:[{type,command}]}`. Claude Code fires *every* group whose `matcher`
   matches the event (`matcher:""` = always-match), in array order. There is **no
   merge-or-overwrite-by-type** semantics. The live `~/.claude/settings.json` confirms the
   array shape (`"Stop": [ {group} ]`).
2. **Code.** harmonik's own `appendHookGroup` (`cmd/harmonik/keeper_enable_doctor_cmd.go:764-793`)
   already **appends** a new matcher-group rather than replacing — so two appended groups
   would both fire today. The collision is NOT a harness limitation.

**Therefore: NO single-dispatcher-hook fallback is needed for Stop/PreCompact.** The
collision is *self-inflicted* by harmonik's dedup key: `findHookForScript`
(`:686-728`) and `updateHookCommand` (`:731-761`) match an existing group **purely by
script basename** (`strings.Contains(cmd, scriptBasename)`, `:722` / `:755`), ignoring the
`HARMONIK_PROJECT=` value. So project B's `keeper enable` finds project A's group (same
basename `keeper-stop-hook.sh`), takes the in-place "update" branch (`mergeHookStanza
:626-633`), and **rewrites A's `HARMONIK_PROJECT` to B's path** — silently breaking A's
keeper (research F-C3.1, P0).

**The fix is a one-line-conceptual change to the dedup key:** basename →
`(basename, HARMONIK_PROJECT=<projectDir>)`. Two projects then produce two distinct groups
that coexist as siblings in the array and both fire. The merge stays **additive** — it never
touches a peer project's group nor the operator's own non-keeper hooks.

**The one genuine single-global surface is `statusLine`** — a scalar object, not an array;
the harness allows exactly **one** `statusLine.command`. This is the only surface that needs
a different treatment (see §1.2).

---

## 1. Decisions (+ rationale)

### D1 — keeper Stop/PreCompact: project-keyed dedup, additive append (NOT a dispatcher hook)

Change the dedup key from script-basename to **`(scriptBasename, "HARMONIK_PROJECT="+projectDir)`**.
Project B no longer matches A's group → falls through to `appendHookGroup` → a second group
is appended. N per-project groups coexist; the harness fires all; each writes to its own
`$HARMONIK_PROJECT/.harmonik/keeper/<agent>.{idle,ctx}`.

**Why (over a dispatcher hook):** both proposals independently reached this, and the schema +
`appendHookGroup` verify it. A dispatcher hook would add a script + a project-resolution
indirection for **zero benefit** on an array surface that already supports coexistence. The
key change is minimal, additive, and leaves operators' own hooks and peers' groups untouched.

### D2 — statusLine: ONE shared project-AGNOSTIC stanza, resolve project from inherited env (NOT a cwd-walk dispatcher)

`statusLine` is a scalar — only one command can live there. **Decision: write a single,
project-independent statusLine command** (strip the `HARMONIK_PROJECT=<dir>` prefix from the
statusLine command only — keep it on the Stop/PreCompact hooks). Each Claude session already
inherits `HARMONIK_PROJECT` from its launch env (captain-launch.sh exports it; `crew start`
exports it; the keeper hooks set it per-event), and `keeper-statusline.sh` already resolves
`PROJECT="${HARMONIK_PROJECT:-${PWD}}"` (`scripts/keeper-statusline.sh:53`) and writes
`.ctx` under `$PROJECT/.harmonik/keeper/<agent>.ctx`. So a single shared stanza routes each
session's `.ctx` write to the correct project **via the session's own inherited env**.

This extends the **existing hk-nm32w pattern** ("a single global entry works for all
concurrent sessions; each session derives its identity at runtime") from *agent-name* to
*project*. The statusLine "collision" dissolves: all projects converge on the **same**
project-agnostic stanza → there is nothing to overwrite (mergeStatusLineStanza becomes a
no-op after the first enable).

**Why NOT Proposal 2's `cwd`-walk + `~/.claude/keeper-projects.d/` registry dispatcher:**
**that mechanism is built on a false premise.** The statusLine JSON that Claude Code pipes to
stdin does **NOT carry `cwd`/`workspace`** — verified against the actual script: it reads
only `.context_window.used_percentage`, `.context_window.total_input_tokens`,
`.context_window_size`, `.session_id`, and `.model` (`scripts/keeper-statusline.sh:3-5,66-98`);
no keeper script reads `.cwd` anywhere (grep-confirmed). So a dispatcher cannot resolve the
project from `cwd`. The inherited-env approach (D2) needs **no** registry, **no** new
dispatcher script, and **no** dependency on C2's session-name scheme — it reuses the
resolution the script already performs.

- **Guard (env unset):** if `$HARMONIK_PROJECT` is unset at statusLine runtime (operator
  launched bare `claude` outside fleet tooling), the script falls back to `$PWD`
  (`:53`) — acceptable; a fleet session's CWD is its project root, so `.ctx` still lands
  correctly. `doctor` drops the per-project `HARMONIK_PROJECT=` sub-check for statusLine
  (it is intentionally absent now) and instead asserts the stanza is present with
  `type:command` (the hk-hs1-required field).

### D3 — `harmonik project-hash [--project DIR]` — new read-only subcommand (shared C2/C3 primitive)

Add a tiny read-only subcommand that prints `tmuxStartHashDir(projectDir)`
(`internal/lifecycle/tmux/subcommand.go:229`) — the **single authoritative accessor** the
shell launch-layer uses to get the same hash the Go core uses, with no reimplemented sha256
in bash. **Both proposals independently recommend this.** It satisfies the "one hashing
scheme" constraint (`02-components.md §1`, `01-problem-space.md` constraint). It is consumed
by C3's de-hardcoded scripts (`captain-launch.sh`, `hk-keeper.sh`, the supervise launcher)
**and** by C2's Go session-namers' shell-facing peers. **Owned by C3** (C3 needs it first),
exposed for C2.

- **Graceful degradation:** every shell call site guards with
  `HASH6="$(harmonik project-hash --project "$P" 2>/dev/null || true)"` so a stale binary
  on PATH degrades to today's un-qualified name rather than breaking launch.

### D4 — version + de-hardcode the captain-tools scripts into the repo

Move `captain-launch.sh` and `crewlog.sh` out of the operator-only `~/.claude/captain-tools/`
into **`scripts/captain-tools/`** (version control). C1 (`init`) provisions them to
`~/.claude/captain-tools/` (the install location the `--respawn-cmd` contract at
`keeper_cmd.go:286` points at), **only if absent** (never clobber an operator copy). Embed via
`//go:embed` in a new `cmd/harmonik/init_assets.go`, kept in sync with the repo copies by a
guard test (embedded bytes == repo bytes).

De-hardcode three things:
- `HK_PROJECT="${HK_PROJECT:-/Users/gb/github/harmonik}"` →
  `"${HK_PROJECT:-${HARMONIK_PROJECT:-$(git rev-parse --show-toplevel 2>/dev/null || pwd)}}"`.
- `crewlog.sh` transcript path `~/.claude/projects/-Users-gb-github-harmonik/...` →
  computed slug `SLUG="$(echo "$PROJ" | sed 's#/#-#g')"` (Claude Code's projects-dir naming).
- captain + keeper session names → project-qualified via `harmonik project-hash` (the
  **C2-intersecting** change; the exact qualified format is C2's published contract — this
  script *consumes* the hash, it does not define the scheme).

### D5 — `/tmp/hk-last-good-binary` → per-project `<projectDir>/.harmonik/state/last-good-binary`

Change `DefaultLastGoodStatePath()` (`internal/release/lastgood.go:27`) to take `projectDir`
and return `filepath.Join(projectDir, ".harmonik", "state", "last-good-binary")`. Two callers
thread `projectDir`: `cmd/harmonik/release_cmd.go:357` and
`cmd/harmonik/supervise/shim.go:196`.

**Why (chose Proposal 2 over Proposal 1's `/tmp/hk-<hash6>-` rename):** moving it under the
already-isolated `.harmonik/` is strictly cleaner than re-qualifying a `/tmp` global, and it
**realizes the spec's own post-1.0 target** — `release-pipeline.md §7.2` already names
`~/.harmonik/state/last-good-binary` as the successor to `/tmp/hk-last-good-binary`. No
hash needed, no `/tmp` clutter, no orphan-sweep concern. **No migration:** if the per-project
file is absent on first read, start fresh — the old `/tmp` value belonged to whichever project
last wrote it and is untrustworthy.

### D6 — `/tmp/hk-daemon.log` + `hkdkeeper` session: qualify both by hash in hk-keeper.sh

`scripts/hk-keeper.sh:44` defaults `LOG="${HK_LOG:-/tmp/hk-daemon.log}"` and
`SESS="${HK_SESS:-hkdkeeper}"` — both un-qualified globals; two projects interleave logs and
collide on the `hkdkeeper` tmux session. Qualify both defaults:
```bash
HASH6="$(harmonik project-hash --project "$PROJ" 2>/dev/null || echo default)"
LOG="${HK_LOG:-/tmp/hk-${HASH6}-daemon.log}"
SESS="${HK_SESS:-hkdkeeper-${HASH6}}"
```
The `hkdkeeper` session is deliberately **not** `harmonik-`-prefixed (per the script's own
header, to dodge the orphan sweep) — a bare `hkdkeeper-<hash6>` suffix keeps both the
sweep-immunity and per-project distinctness. `$HK_LOG`/`$HK_SESS` overrides still win.

### D7 — `/tmp/hk-daemon-supervise.sh`: retire the hand-authored artifact; the in-binary `harmonik supervise` is the supervisor-of-record; ship a de-hardcoded shell fallback

**Critical finding (both proposals, corroborated by code + research F-C3.3 + Risk #4):
there is NO Go or shell generator for `/tmp/hk-daemon-supervise.sh`.** It appears only as
(1) a doc reference in `specs/release-pipeline.md:255`, (2) descriptive Go comments
(`subcommand.go:173`, `main.go:778`), and (3) memory/research. The live file with hardcoded
`PROJECT=/Users/gb/github/harmonik` is a **hand-reconstructed operator recovery artifact**
("RECONSTRUCTED 2026-06-08"), not something harmonik writes.

**The canonical, in-binary, already-per-project supervisor exists:** `harmonik supervise start`
(`cmd/harmonik/supervise/start.go`) runs the supervisor loop in-process — launches the
flywheel in tmux session `FlywheelSessionName(projectDir)` = `harmonik-<hash6>-flywheel`,
runs `harmonik supervise _shim <projectDir>`, reads per-project `.harmonik/cognition/config.json`,
holds a per-project `supervisor.lock`. It takes `projectDir` everywhere; **zero `/tmp`
globals.**

**Decision (two parts):**
1. **Retire the `/tmp` script from the supported surface; repoint docs at `harmonik supervise`.**
   The durable fix is to make the in-binary supervisor the supervisor-of-record. Update
   `specs/release-pipeline.md:255` and §7.2 to name `harmonik supervise` instead of
   `hk-daemon-supervise.sh`; annotate the Go comments at `subcommand.go:173` / `main.go:778`
   as "legacy out-of-band; superseded by `harmonik supervise`".
2. **Ship a de-hardcoded shell fallback for operators who launch the supervisor loop
   out-of-band.** Add `scripts/hk-supervise.sh` (sibling of `hk-keeper.sh`) with **no
   hardcoded PROJECT/BIN**: resolve `PROJECT` from `$HK_PROJECT`/arg/`git rev-parse`, `BIN`
   from `command -v harmonik` (fallback `$HOME/go/bin/harmonik`, fail loudly if neither),
   and name its session `hk-<hash6>-daemon-supervise` (via `harmonik project-hash`). This
   replaces the ad-hoc `/tmp` reconstruction with a checked-in, project-qualified artifact;
   its runtime self-copy, if any, is the qualified `/tmp/hk-<hash6>-daemon-supervise.sh`.

### D8 — project-qualify `SupervisorSessionName` (C2-intersection) — keep the `hk-` (not `harmonik-`) prefix

`internal/lifecycle/tmux/subcommand.go:182` `const SupervisorSessionName = "hk-daemon-supervise"`
→ **function** `SupervisorSessionName(projectDir string) string` returning
`"hk-" + tmuxStartHashDir(projectDir) + "-daemon-supervise"`. The live consumer
`ResolveDaemonSpawnSession` (`:213`) and the `resolvedaemonsession_hk9vp51_test.go` tests
update to pass `projectDir`.

**Decision: keep the supervise + keeper helper sessions on the `hk-` prefix, NOT
`harmonik-`** — so they remain **outside** PL-006's `harmonik-<project_hash>-` orphan-sweep
namespace (`process-lifecycle.md:263` sweeps the `harmonik-<project-hash>-` prefix). This is
why `hk-keeper.sh:5` already uses the non-`harmonik`-prefixed `hkdkeeper` session.
Project-qualification gives coexistence; staying on `hk-` keeps these two session classes
out of the sweep entirely, sidestepping the PL-006d live-owner-sentinel requirement for them.
(Crew sessions, which DO live under the swept `harmonik-<hash>-` namespace, are C2's problem
to sentinel-protect — out of C3 scope.)

**Note — C2 coordination:** D8 is squarely on the C2/C3 boundary (C2 owns launch-layer
session naming). C3 documents the *constraint* (qualify the supervisor session; keep it on
`hk-`; this is the source of the hash for the shell scripts). The actual `SupervisorSessionName`
signature change should land in C2 (or C3, by agreement) but MUST use the same
`tmuxStartHashDir`. Flag for the captain (see §4).

### D9 — spec home for the multi-tenancy invariant: `specs/operator-nfr.md` (ON-series)

Add the multi-tenancy invariant as a new **ON-requirement** in `specs/operator-nfr.md` — the
research candidate (F-C3.4) and the natural home (ON owns operability NFRs; there is no spec
section today governing the global `~/.claude/` surface or `/tmp` globals). **Chosen over
Proposal 1's PL-031** because the invariant is an operator-NFR (shared-surface hygiene), not a
process-lifecycle session-naming rule. PL already owns the session-naming half (extended by
C2); ON owns the global-surface half (C3).

---

## 2. Mechanism (file:line)

### 2.1 keeper Stop/PreCompact — project-keyed dedup (`cmd/harmonik/keeper_enable_doctor_cmd.go`)

| Function | Line | Change |
|---|---|---|
| `findHookForScript` | `:686-728` | add `wantProject string` param; the `:722` match becomes `strings.Contains(cmd, scriptBasename) && strings.Contains(cmd, "HARMONIK_PROJECT="+wantProject+" ")` (trailing space = exact, prevents `/path/A` matching `/path/AB`). `wantProject==""` ⇒ basename-only (back-compat). |
| `updateHookCommand` | `:731-761` | same `wantProject` guard on the `:755` inner match — an in-place normalize only ever rewrites *this* project's group. |
| `mergeHookStanza` | `:623-638` | add `wantProject` param; thread to find/update. No match for B's own project ⇒ `appendHookGroup` ⇒ second sibling group. |
| call sites | `:215-216` | pass `cfg.projectDir`. |
| doctor find calls | `:440,:457` | pass `cfg.projectDir` so doctor validates *this* project's group, not any project's (today it green-checks if any stanza exists, masking a missing-for-this-project hook). |

`appendHookGroup` (`:764-793`) is unchanged — it already appends.

### 2.2 statusLine — single project-agnostic stanza (same file)

| Function | Line | Change |
|---|---|---|
| `statusLineCmd` build | `:203-205` | drop the `HARMONIK_PROJECT=%s` prefix on the **statusLine** command only (keep it on `stopHookCmd` :206 and `precompactHookCmd` :209). Command becomes bare `<scriptsDir>/keeper-statusline.sh`. |
| `mergeStatusLineStanza` | `:594-617` | unchanged logic (still keys on `keeper-statusline.sh` basename) — but now all projects write the *same* project-agnostic command, so after the first enable it is a no-op `unchanged`. |
| doctor statusLine check | `:407-416` | drop the `HARMONIK_PROJECT=` sub-check (`:414-416`) for statusLine (intentionally absent); keep the `type:command` check (`:417+`, hk-hs1). |

Runtime routing is already correct: `keeper-statusline.sh:53` resolves
`PROJECT="${HARMONIK_PROJECT:-${PWD}}"` and writes `$PROJECT/.harmonik/keeper/<agent>.ctx`.

### 2.3 `harmonik project-hash` subcommand (new)

New `cmd/harmonik/projecthash_cmd.go`: parse `[--project DIR]` (default CWD, `filepath.Abs`),
print `tmuxStartHashDir(projectDir)` + newline, exit 0. Read-only, no side effects. Reuses
`internal/lifecycle/tmux/subcommand.go:229` (exported or via a thin `lifecycle.ComputeProjectHash`
wrapper — confirm which is import-cycle-safe; `tmuxStartHashDir` is package-private to the
tmux package, so the subcommand calls `lifecycle.ComputeProjectHash`, the public equivalent
named in the comment at `:226`).

### 2.4 captain-tools (versioned + de-hardcoded)

- New: `scripts/captain-tools/captain-launch.sh`, `scripts/captain-tools/crewlog.sh` (copies
  of the live scripts, de-hardcoded per D4).
- New: `cmd/harmonik/init_assets.go` with
  `//go:embed captain-tools/captain-launch.sh captain-tools/crewlog.sh` (embed pattern per
  `internal/daemon/standardgraph.go:26`).
- `init` (C1) writes them to `~/.claude/captain-tools/` (chmod 0755) **only if absent**.
- Guard test: embedded bytes == `scripts/captain-tools/` bytes (standardgraph-sync style).

### 2.5 `/tmp` globals + supervisor

| Surface | File:line | Change |
|---|---|---|
| last-good | `internal/release/lastgood.go:27` | `DefaultLastGoodStatePath(projectDir)` → `<projectDir>/.harmonik/state/last-good-binary`; callers `release_cmd.go:357`, `supervise/shim.go:196` thread `projectDir`. |
| daemon log + keeper session | `scripts/hk-keeper.sh:44` | qualify `LOG` + `SESS` by `harmonik project-hash` (D6). |
| supervise fallback | new `scripts/hk-supervise.sh` | de-hardcoded PROJECT/BIN; session `hk-<hash6>-daemon-supervise`. |
| supervisor session name | `internal/lifecycle/tmux/subcommand.go:182` | `const` → `func SupervisorSessionName(projectDir)`; update `:213` + `resolvedaemonsession_hk9vp51_test.go:56,74,79,119` (C2-coordinated, D8). |

### 2.6 Spec changes (spec-first project)

| Spec file | Change |
|---|---|
| `specs/operator-nfr.md` (new ON-req under §4) | Multi-tenancy invariant (D9) — see §3 below for exact text. |
| `specs/release-pipeline.md:255` + §7.2 | Pre-1.0 last-good path → `<projectDir>/.harmonik/state/last-good-binary`; replace `hk-daemon-supervise.sh` references with `harmonik supervise` (D5, D7). |
| `specs/process-lifecycle.md` | Note `harmonik supervise` (per-project flywheel session + `.harmonik/cognition/`) is the canonical supervisor; the `/tmp` script is retired/legacy. C2 extends the PL session-naming contract to cover the project-qualified supervisor/keeper names (D8). |

### 2.7 Tests

- `keeper_enable_doctor_cmd_test.go`: `TestKeeperEnable_TwoProjectsCoexist` — enable agent
  `cap` for `/tmp/projA` then `/tmp/projB` against one fake settings file; assert
  `hooks.Stop` has **2** groups (one per `HARMONIK_PROJECT`), re-enabling `/tmp/projA`
  leaves count at 2 (idempotent), a pre-existing operator Stop group survives, and the
  single statusLine stanza is project-agnostic.
- embed-sync guard test (`init_assets`): embedded captain-tools bytes == repo bytes.
- `lastgood` test: `DefaultLastGoodStatePath(dir)` returns the per-project path; absent file
  ⇒ `ErrNoLastGood` (fresh start, no migration).

---

## 3. Risk resolution

### R1 (research Risk #1, CRITICAL) — harness may not allow N coexisting Stop hooks
**RESOLVED, decisively (§0).** Schema = array-of-matcher-groups firing all matches;
`appendHookGroup` already appends. No dispatcher hook. Fix = add `HARMONIK_PROJECT` to the
dedup key. **No verify-first remaining for Stop/PreCompact.**

### R1b — the statusLine scalar (the ONE true singleton)
**RESOLVED (D2).** Single project-agnostic stanza; each session's inherited `HARMONIK_PROJECT`
routes its own `.ctx`. **Rejected Proposal 2's cwd-walk dispatcher — verified false premise:**
the statusLine JSON does not carry `cwd` (script reads only context_window/session_id/model;
no keeper script reads `.cwd`). The chosen approach reuses the script's existing
`PROJECT="${HARMONIK_PROJECT:-${PWD}}"` resolution — zero new script, zero registry, zero C2
dependency.

### R2 (research Risk #2) — settings.json is shared with the operator + non-harmonik tooling
**RESOLVED.** The basename+project find is scoped to `keeper-*-hook.sh` groups — a
non-harmonik Stop group is never matched or rewritten. The merge is strictly additive (can
only add a group or update a same-project one). Backup is already taken (`:190-197`).

### R3 (research Risk #3) — captain-launch.sh mints session ids AND names (published contract)
**Flagged for the review gate.** Versioning the script intersects the Captain&Crew published
contract (crew/captain session-id minting) and C2's session-name qualification. The
session-name/`--session-id` changes route through an **independent reviewer alongside C2**
(constraint from `01-problem-space.md`; research Risk #3). C3 owns the *script file*; C2 owns
the *qualified name format*.

### R4 (research Risk #4) — `/tmp/hk-daemon-supervise.sh` is reconstructed at runtime, not generated
**RESOLVED by re-framing (D7):** there is no generator to fix. The in-binary
`harmonik supervise start` IS the supervisor-of-record (already per-project, zero `/tmp`).
Fix retires the hand-authored artifact, repoints the one doc reference, and ships a
de-hardcoded shell fallback for out-of-band operators.

### R5 — `SupervisorSessionName` is a live `const` consumed by `ResolveDaemonSpawnSession`
Bounded change: `const` → `func(projectDir)`; one live consumer (`:213`) + 4 test refs.
Keep the `hk-` prefix to stay outside the PL-006 sweep (D8). **Coordinate with C2** (it owns
session naming).

### R6 — hash-collision across two repos
6 bytes (48 bits) of sha256; birthday-bound negligible for a handful of local projects —
the same guarantee the core already relies on (`tmuxStartHashDir`, PL-006a). No new risk.

---

## 4. Open / verify-first items for the captain

1. **C2/C3 ownership of the `SupervisorSessionName` const→func change (D8) and the
   captain-launch.sh session-name qualification (D4/R3).** These straddle the C2/C3 boundary
   (C2 owns launch-layer session naming; C3 owns the script files + the supervisor session
   class). **Recommend:** C3 ships `harmonik project-hash` + the script files +
   `lastgood`/`hk-keeper.sh`; the `SupervisorSessionName` signature change and the exact
   crew/captain/keeper qualified-name *format* land in C2 against the same `tmuxStartHashDir`.
   The session-name + `--session-id`-minting edits go through an **independent reviewer with
   C2** (published Captain&Crew contract). *Captain: confirm the C2/C3 split before
   dispatching, so the two components don't both edit `subcommand.go:182`.*

2. **Spec home of the multi-tenancy invariant: `operator-nfr.md` (D9) vs `process-lifecycle.md`
   (Proposal 1's PL-031).** Chosen ON-series; confirm with the spec-owner that a new ON-req
   under §4 is preferred over a PL section. *Low-risk; default to ON unless told otherwise.*

3. **`harmonik project-hash` exposure path** — `tmuxStartHashDir` is package-private to the
   tmux package; the subcommand should call the public `lifecycle.ComputeProjectHash` (named
   in the `:226` comment). Verify that public function exists and is import-cycle-safe before
   implementation (one-line check at impl time).

4. **No verify-first remains on the critical risk** (harness N-hooks) — it is resolved.

---

## 5. Proposed ON-requirement text (D9)

> **ON-0xx — Multi-tenant global-surface isolation.** Harmonik's contributions to surfaces
> shared across projects on one machine — the global `~/.claude/settings.json` keeper hook
> stanzas, the `~/.claude/captain-tools/` scripts, and `/tmp/hk-*` state files — MUST be
> project-namespaced so that N fleets coexist without one project's bootstrap perturbing
> another's. Specifically: (a) keeper Stop/PreCompact hook groups are deduplicated on the pair
> `(script basename, HARMONIK_PROJECT=<projectDir>)` and coexist as sibling entries in the
> `hooks.<Event>` array; the statusLine (a scalar) is a single project-agnostic stanza that
> resolves project from each session's inherited `HARMONIK_PROJECT` env; merges MUST be
> additive and MUST NOT rewrite a peer project's or the operator's own hooks. (b) captain-tools
> scripts are versioned in `scripts/captain-tools/`, embedded in the binary, and provisioned by
> `harmonik init`, resolving project + the per-project hash at runtime — no literal path.
> (c) per-project daemon state (last-good binary, daemon log, supervisor session) MUST live
> under the project's own `.harmonik/` or carry the PL-006a `<project_hash>` qualifier; the
> in-binary `harmonik supervise` is the canonical per-project supervisor.

---

## Summary of edits

| Surface | File:line | Change |
|---|---|---|
| Hook dedup key | `keeper_enable_doctor_cmd.go:686,731,623` + callers `:215-216,:440,:457` | basename → (basename, `HARMONIK_PROJECT=`); additive append (D1) |
| statusLine | `keeper_enable_doctor_cmd.go:203-205,594-617`; doctor `:407-416` | drop per-project prefix on statusLine only; single shared stanza; resolve from env (D2) |
| Hash accessor | new `cmd/harmonik/projecthash_cmd.go` → `lifecycle.ComputeProjectHash` | `harmonik project-hash` (D3) |
| captain-tools | new `scripts/captain-tools/{captain-launch,crewlog}.sh`; `cmd/harmonik/init_assets.go` embed; init provisions | version + de-hardcode (D4) |
| last-good | `internal/release/lastgood.go:27` + callers `release_cmd.go:357`, `supervise/shim.go:196` | per-project `.harmonik/state/last-good-binary` (D5) |
| daemon log + keeper sess | `scripts/hk-keeper.sh:44` | qualify `LOG`+`SESS` by hash (D6) |
| supervise | new `scripts/hk-supervise.sh`; retire `/tmp/hk-daemon-supervise.sh`; doc repoint | de-hardcode + retire (D7) |
| supervisor session name | `internal/lifecycle/tmux/subcommand.go:182,213` + test `:56,74,79,119` | `const`→`func(projectDir)`, keep `hk-` prefix (D8, C2-coord) |
| Spec — invariant | `specs/operator-nfr.md` new ON-req | multi-tenancy invariant (D9) |
| Spec — release | `specs/release-pipeline.md:255,§7.2` | last-good path + `harmonik supervise` (D5,D7) |
| Spec — lifecycle | `specs/process-lifecycle.md` | canonical supervisor note; PL session-naming extension (C2) |
| Tests | `keeper_enable_doctor_cmd_test.go`; embed-sync; lastgood | two-project coexistence; embedded==repo; per-project path |

---

## Decisiveness summary

- **No dispatcher hook for Stop/PreCompact** — harness supports N coexisting groups
  (verified: schema + `appendHookGroup`). Fix = `HARMONIK_PROJECT` in the dedup key.
- **No dispatcher for statusLine either** — Proposal 2's cwd-walk is built on a false
  premise (the statusLine JSON has no `cwd`). One project-agnostic stanza + inherited-env
  resolution, extending hk-nm32w.
- **last-good → per-project `.harmonik/state/`** (Proposal 2) — realizes the spec's own
  post-1.0 target; cleaner than a `/tmp` hash-rename.
- **`/tmp/hk-daemon-supervise.sh` has no generator** — retire it; `harmonik supervise start`
  is the per-project supervisor-of-record; ship a de-hardcoded shell fallback.
- **`harmonik project-hash`** is the single shell-facing hash source for C2+C3.
