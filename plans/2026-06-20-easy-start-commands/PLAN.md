# Easy-start commands for captain / crew (+ keepers)

**Date:** 2026-06-20
**Author:** main session (operator request)
**Status:** REV 2 — operator decisions locked (2026-06-20). Q1/Q2/Q3/Q4 resolved below; now under critic/tester/architect review.

## Operator decisions (2026-06-20) — these override REV 1

- **D1 — Native Go, NOT a bash wrapper.** Shelling out to `captain-launch.sh` is **rejected**: bash is not unit-testable and the launcher must work across **macOS and Linux**. Reimplement the full launch sequence natively in Go (testable, cross-platform). Approach B is dead; **Approach A (native parity) is mandatory.** The bash script is retired to reference-only once parity lands.
- **D2 — Short flag name.** `--crew-name` is too long. **Unify on `--name`** for both captain and crew.
- **D3 — Mission is operator-supplied, never auto-stubbed, never silently reused.** Don't require one and don't fabricate one. **Critically: if a prior agent left a mission file, do NOT reuse it** — it may be stale (operator hit this today). A reused-stale-mission must be impossible by construction.
- **D4 — Crew keepers are force-cut by default.** Crews "can't get too big" — with no params, a crew keeper uses the **system default act/restart cutoffs** (same band as captain), NOT warn-only. The warn-only default is wrong.
- **D5 — Live keeper override with auto-revert (NEW requirement).** When the operator is actively working with an agent and needs it to continue past the cutoff, the agent needs a way to tell the keeper "**do not kill this session**" — a **live** switch, ideally a state in the keeper's state machine. **But the override is transient: once the session is finally restarted/cleared, it reverts to the default cutoff.** A held session must never become a permanently-unbounded session.
- **D6 — Fix the live-broken `start captain` as part of the proper native fix** (folded into D1, not a separate hot-fix).
- **D7 — Relaunch must be idempotent when the keeper outlived the agent (NEW).** Today, if you stop the captain agent but the keeper window keeps its tmux session alive, re-running the launcher throws (`tmux new-session -s <CAP_TMUX>` → "duplicate session", `captain-launch.sh:80`). The native launcher must **pre-flight**: if the target session already exists, reap the stale keeper + session (or respawn the agent window in place and rebind the keeper) instead of erroring. Re-launching a half-dead captain should Just Work.

## Problem

The operator must today launch the captain with:

```bash
export HK_PROJECT=/Users/gb/github/harmonik      # an env var they don't want to set
~/.claude/captain-tools/captain-launch.sh captain # a path they don't want to memorize
```

Two friction points, stated by the operator:
1. **The script path** — `~/.claude/captain-tools/captain-launch.sh` is not discoverable; it should be a `harmonik` subcommand.
2. **The `HK_PROJECT` env var** — required by the script (no default), must be set by hand.

The operator also wants crew (and the keepers that ride along) to be **just as easy** to start, and recalls earlier work to "get this into the tool" that **never landed**. It half-landed — see Current State.

**Goal:** launch captain or crew with a single, memorable, no-env-var command — e.g. `harmonik start captain` / `harmonik start crew paul` — with the keeper auto-armed in both cases.

## Current state (what already exists)

| Surface | Exists? | Notes |
|---|---|---|
| `scripts/captain-tools/captain-launch.sh` | ✅ | **Canonical, battle-tested.** Embedded + deployed to `~/.claude/captain-tools/` by `harmonik init` (`provisionCaptainTools`). Requires `HK_PROJECT`. Does the FULL job (see below). |
| `harmonik captain` / `harmonik start captain` (Go, `captain.go`) | ✅ but **PARTIAL & LIVE-BROKEN** | A deliberately **bare** launcher (hk-ly0n), **already routed** from both `harmonik captain` and `harmonik start captain` (`main.go:578-579`). `--project` defaults to cwd (needs no env var), mints session-id, wires keeper settings.json stanzas, launches tmux. **But it is NOT at parity** — see the gap table. A captain launched this way would break TODAY. **This is a latent live bug**, not just unfinished work: `start captain` is shipped and points at the reap-prone path. |
| `harmonik crew start <name> --queue <q> --mission <path>` (Go daemon RPC) | ✅ | Works, but **requires** `--queue` and `--mission` — not "incredibly easy". No `start crew` alias. |
| `harmonik keeper …` watcher | ✅ | Armed today by `captain-launch.sh` in a sibling tmux window. No standalone easy-start needed; it should ride along with the role. |
| Prior plan/bead for "fold launch into the CLI" | ❌ for captain | No bead exists for making `captain-launch.sh` a first-class command. The bare Go launcher (hk-ly0n) is as far as it got, with a list of explicitly-excluded follow-ups (below). |

### The parity gap: `harmonik captain` (Go) vs `captain-launch.sh`

The Go command's own header comment lists what it excludes. These are **load-bearing**, not cosmetic:

| Capability | `captain-launch.sh` | `harmonik captain` (Go) | Consequence if missing |
|---|---|---|---|
| `captain.sentinel` + `captain.pid` | ✅ | ❌ | **Daemon orphan-sweep reaps the captain session.** This alone makes the Go path unusable in prod. |
| Hashed tmux namespace `harmonik-<hash>-captain` | ✅ (via `harmonik project-hash`) | ❌ (plain `--tmux captain`) | **Doubly unprotected:** `probeCaptainSentinel` (`orphansweep.go:518`) only protects the *hashed* session name — the Go path's plain `captain` session wouldn't be skipped even if it *did* write a sentinel. |
| Keeper **watcher** armed in sibling window | ✅ (`harmonik keeper --agent … --tmux … --warn-abs …`) | ❌ (only settings.json stanzas) | Gauge exists but nothing acts on fill — no warn/act, no restart-now. |
| Window-nesting (agent + keeper siblings) | ✅ (hk-z036) | ❌ | Self-heal + window-granular restart invariants broken. |
| Self-heal respawn (`--respawn-cmd`, hk-opuv) | ✅ | ❌ (excluded) | Dead captain pane never auto-recovers. |
| Verified-restart wrapper (hk-9mpk/hk-uldg) | ✅ generated | ❌ | Explicit restarts are assumed, not confirmed-landed. |

**Conclusion:** the Go launcher is ~30% of the script. Bringing it to native parity means re-implementing six load-bearing, subtle behaviors in Go — high risk of divergence from the script that already works.

## Design

### 1. Command surface

Add one umbrella verb with a strict, agent-proof parsing rule:

```
harmonik start captain                 # bare; all defaults
harmonik start crew <name>             # one bare positional = the name
harmonik start captain --name … --tmux …          # advanced: ALL named, NO positional
harmonik start crew --name paul --queue … --mission …   # --name (D2), not --crew-name
```

`harmonik captain` and `harmonik crew start <name>` remain as-is (back-compat). **NOTE — `start captain` is NOT net-new:** it already routes (`main.go:578-579`) to the broken bare Go launcher. So ES2 *redirects an existing live route*, it does not add one. `start crew` IS net-new (no route exists today).

### 2. The parsing rule (the operator's requirement)

> "Positional parameters shouldn't be allowed because agents screw it up. But keep it easy: `harmonik start crew paul` OR, if you add other params, the position option goes away and you provide named flags `--crew-name paul`."

Encoded as a **positional-XOR-flags** rule, validated in a shared `runStart` dispatcher:

- Token 1 after `start` = **role** (`captain` | `crew`). Always positional (it's a subcommand selector).
- **Simple form:** role + **at most one** bare positional (the name; captain takes none). No flags.
- **Advanced form:** the moment **any `--flag`** appears, **zero** bare positionals are allowed — the name must come via `--name` (D2).
- **Mixing** a bare name positional with any flag ⇒ hard error:
  `harmonik start crew: positional name not allowed alongside flags — use --name paul`

This removes every multi-positional ordering footgun (no `start crew paul alpha-q mission.md`), while keeping the 90% path a two-word command.

**Implement as a thin pre-parse in `runStart` BEFORE delegating** to the existing `runCaptainSubcommand` / crew handlers — do NOT retrofit the XOR rule into stdlib-`flag` (captain) or the manual loop (crew). Critically (Approach B), the XOR validation and `--help` must run **before** any `exec` of the shell script, or they're swallowed by the script's own arg parsing.

### 3. Captain implementation — NATIVE Go full parity (Approach A, mandated by D1)

`harmonik start captain` / `harmonik captain` is **reimplemented natively in Go** to do everything `captain-launch.sh` does — testable and cross-platform (macOS + Linux). No bash on the launch path. The existing partial `captain.go` (hk-ly0n) is the seed; it must grow the six missing load-bearing behaviors:

1. **Project + env:** `project = --project or cwd`. Compute the project hash in-process (reuse the `project-hash` code, not a shell-out). Operator sets no `HK_PROJECT` — it's internal.
2. **Hashed namespace:** launch into `harmonik-<hash>-captain` (via `lifecycle.TmuxSessionName`), not plain `captain`, so reap/restart tooling and `probeCaptainSentinel` (`orphansweep.go:518`) recognize it.
3. **Sentinel + pid:** write `.harmonik/cognition/captain.sentinel` + `captain.pid` so the orphan sweep skips it. **(Fixes the D6 live bug.)**
4. **Window-nesting:** agent window + sibling keeper window (hk-z036), built via the tmux substrate helpers in Go.
5. **Keeper watcher armed** in the keeper window with the real warn/act band (not just settings.json stanzas).
6. **Self-heal respawn + verified-restart:** today these are *generated bash files* (`captain-respawn.sh`, `captain-restart-verified.sh`) that the keeper invokes. **Cross-platform/testable replacement:** make them **first-class `harmonik` subcommands** the keeper calls — e.g. `harmonik captain respawn --session-id … --tmux …` and the existing `harmonik keeper restart-now`/`await-ack` verbs — so nothing depends on emitting bash. This is the main net-new design surface (see architect questions).

**Tmux is still the substrate** (that's not bash — it's the same `tmux` calls, issued from Go via `exec.Command`, already how the daemon spawns sessions in `internal/daemon/tmuxsubstrate.go`). "No bash" means no logic in a `.sh` file we can't unit-test, NOT "no tmux".

**Why A despite the larger size:** D1 is non-negotiable — the launcher must be unit-tested and run on Linux. The script's six behaviors are reimplemented once in Go with table-tested argv assembly (the captain.go test seam already captures the tmux argv without spawning). The daemon already owns Go equivalents for most of this (crew spawn, crew keeper window, sentinel handling) — so it's consolidation, not greenfield. The bash script becomes reference-only.

*Alternative A (native Go parity)* stays on the table if the operator wants to retire the shell script entirely long-term — but that's a bigger, separate effort, not what "make it easy to start" needs.

### 4. Crew easy-start

`harmonik start crew <name>` fills the two currently-required flags with sane defaults:

- `--queue` defaults to `<name>-q` (one named queue per crew, matching the captain's per-lane convention).
- `--mission` (D3): operator-supplied via the flag; **optional, never auto-stubbed**. The decisive rule — **never silently reuse a prior agent's mission file.** If `.harmonik/crew/missions/<name>.md` already exists from a previous crew, it may be stale (operator hit this today). Options to make stale-reuse impossible by construction: (a) refuse to start without `--mission` when a same-named mission already exists, demanding an explicit path/confirm; or (b) treat `--mission` as the only source and never read the on-disk default. **No fabricated stub.** Pick the exact mechanic in ES3; the invariant is "a crew never boots on an unintended old mission."
- **Keeper:** crews ALREADY get a sibling keeper window — the daemon spawns it via `spawnCrewKeeperWindow` (`internal/daemon/tmuxsubstrate.go:1400`). But `crewKeeperWindowArgv` arms it **`--warn-only`, with NO `--act-abs-tokens` and NO `--respawn-cmd`** (`tmuxsubstrate.go:1373-1385`). **D4 says this default is WRONG:** a crew "can't get too big", so with no params the crew keeper must use the **system default act/restart cutoffs** (same band as captain) and be force-cut. Lifting it means crews need the same respawn/verified-restart entrypoints as the captain — which §3 step 6 already makes Go subcommands, so this is shared work, not duplicated bash.

### 5. Keepers + the live-override-with-auto-revert (D5)

No standalone `start keeper` verb — the keeper **rides along** with its role (captain via §3, crew via §4), now both force-cut by default (D4).

**New requirement (D5): a live "hold" the operator/agent can flip when actively co-working, that auto-reverts on restart.** Design sketch (for architect review):
- A keeper **state** (in the keeper's state machine) — call it `HOLD` — that **suspends the ACT cutoff** (no clear/restart) while leaving WARN signalling intact. Set via a new verb, e.g. `harmonik keeper hold --agent <name>` (and `harmonik keeper release --agent <name>` to drop it early).
- **Auto-revert is the load-bearing invariant:** the HOLD must be cleared the moment the session is finally restarted/cleared, so a held session can never become permanently unbounded. Mechanism candidates: store HOLD as **session-scoped transient state keyed by the live `--session-id`** so a clear→resume (new conversation continuity but post-restart) drops it; or clear it explicitly in the restart-now / cycle path. The invariant: **HOLD never survives a restart.**
- Open design point: should HOLD also expire on a timer (e.g. auto-release after N minutes) as a backstop in case the operator walks away? Flag for architect.

## Work breakdown (proposed beads, under `codename:easy-start`)

1. **ES1** — `runStart` dispatcher + positional-XOR-flags pre-parser + unit tests for the mixing-error and both forms. Add the `start crew` route (net-new); keep `start captain` routing but point it at the native launcher (ES2). `--name` everywhere (D2). *(pure arg logic)*
2. **ES2** — **extract the shared nesting+keeper-arm helper** (review outcome A) from the daemon's `spawnCrewKeeperWindow`, then native captain launch through it (D1/D6): in-process project-hash, `harmonik-<hash>-captain` namespace, sentinel+pid, window-nesting, keeper-watcher armed with the real band. **Idempotent pre-flight (D7):** existing-session → reap stale keeper/session or respawn-in-place, never error. **Add a window-nesting argv seam** (none today) + table-tested argv. Redirects the live-broken `start captain`/`captain` routes.
3. **ES3** — `harmonik captain respawn` subcommand for the `--respawn-cmd` seam; **delete** the verified-restart wrapper and arm the keeper to call `keeper restart-now` directly (review B). **Re-author** the respawn tests against the new argv (the bash-grep tests die with ES8).
4. **ES4** — `harmonik start crew <name>`: `--queue=<name>-q` default; **mission split rule** (D3/review D): fresh-start = `--mission` only, ignore disk; keeper-restart = re-read disk. Wire into `crew start`.
5. **ES5** — **crew keeper force-cut by default** (D4): `crewKeeperWindowArgv` (`tmuxsubstrate.go:1373`) off `--warn-only` onto the shared act+respawn band/entrypoints from ES2/ES3.
6. **ES6** — **keeper HOLD state + auto-revert** (D5): `keeper hold`/`release` verbs, session-id-scoped transient state, cleared on restart-now/cycle; tests prove HOLD never survives a restart. *(Likely its own codename — biggest net-new surface.)*
7. **ES7** — top-level `usage.go` + `harmonik start --help`; docs in captain/crew/keeper skills + AGENTS.md router; mark `captain-launch.sh` reference-only / retired.
8. **ES8** — retire `captain-launch.sh` from the launch path once ES2/ES3 land parity (keep for reference or delete + drop the embed/sync test).

## Risks / notes

- **Embedded-asset sync gotcha:** until `captain-launch.sh` is retired (ES8), editing it requires copying to the embedded path or `TestCaptainLaunchShEmbedInSync` goes red. Belongs to ES7/ES8, not ES6.
- **Back-compat:** keep `harmonik captain` and `harmonik crew start <name> …` working; `start` is additive.
- **`--name` is NOT a session collision risk:** crews spawn as `harmonik-<hash>-crew-<name>` (`tmuxsubstrate.go:1268`), captain as `harmonik-<hash>-captain`. A crew named `captain` → `crew-captain` — no tmux/sentinel collision. (A comms-identity / `HARMONIK_AGENT` collision is possible — add a one-line guard.)
- **Keeper session_id flips on /clear** (`internal/keeper/sessionid.go`) and the related subtleties (lowercase SID, `--resume` not `--session-id`, agent-window-only respawn, no-dup-keeper, sentinel/pid refresh) are **process-choreography invariants the native port must preserve** — argv tests do NOT cover them; see the testability caveat below.

## Review outcomes — REV 3 (critic + tester + architect, 2026-06-20)

### A — Consolidation (architect, load-bearing; the plan's biggest miss)

The daemon **already** spawns the crew + sibling-keeper windows natively in Go: `spawnCrewKeeperWindow` (`tmuxsubstrate.go:1400`) does window-nesting, `os.Executable()` keeper resolution, and `shellJoinArgv` quoting — **the exact quoting/nesting the bash launcher fought.** The captain is structurally *a crew + a sentinel*. So ES2/ES3/ES5 must **NOT** write a third implementation in `captain.go` (bash=1, daemon=2). Instead: **extract the daemon's nesting+keeper-arm into a shared `internal/` helper** (argv-builder + injectable run-func seam — `captain.go:45` already has the seam) consumed by both the daemon spawn path and the CLI captain launcher. Captain's only true deltas vs. a crew: the `harmonik-<hash>-captain` namespace and `captain.sentinel`+`captain.pid`. **This turns D1 from "rewrite six bash behaviors" into "extract one helper + add the sentinel + flip the crew keeper band" — and is what neutralizes the regression risk.**

### B — R1 self-heal/verified-restart shape (architect)

Keep the keeper's `--respawn-cmd` seam (it's `sh -c "<cmd>"` at `watcher.go:1474` — cross-platform), but point it at a **`harmonik captain respawn` subcommand** (table-testable argv) instead of a generated `.sh`. **DELETE the verified-restart wrapper entirely:** `keeper restart-now` (`restartnow.go:68`) already does the synchronous verified work in-process; the bash wrapper added only arg-prebinding. Arm the keeper to call `keeper restart-now` directly.

### C — R2 HOLD design — RESOLVED (architect + tester)

- **Key the HOLD marker by live `--session-id`** — `.harmonik/keeper/<agent>.hold.<sessionID>`, mirroring `IsSleeping`'s `.sleeping.<sessionID>` (`gates.go:155-162`). Because the session_id is **re-minted at every `/clear`** (`cycle.go:915`), a HOLD keyed on the old id becomes **structurally unreachable** after a restart — auto-revert is guaranteed by construction, not by remembering to unlink in six exit paths. **Agent-keyed HOLD is the trap** (it would survive restart) — adversarial test H8 is mandatory.
- **Gate ALL destructive paths, not just the Cycler:** `MaybeRun` (cycle), `maybeRespawn` (`watcher.go:1435`), `maybeLivePaneRecover` (`watcher.go:1520`). **DECISION: the hard-ceiling restart (`HardCeilingRestartFn`, `watcher.go:506`) OVERRIDES HOLD** — true overflow protection wins over an operator hold (surfaced as a deliberate carve-out, not implicit). WARN signalling stays on under HOLD (D5).
- **Timer backstop is MANDATORY, not optional** (critic + architect): it's the only thing that covers operator-walks-away, machine-crash-then-same-SID-resume, and daemon-restart — the escape routes a pure session-id key misses. Store an RFC3339 timestamp as marker content (as `.dispatching` already does, `gates.go:125`); gate treats a marker older than N min as expired. Default N ~30–60 min, configurable via the keeper config block.

### D — R5 stale-mission — RESOLVED (critic)

Neither "refuse-if-exists" nor "flag-only" as stated — both break **keeper-restart re-hydration**, where a crew is *supposed* to re-read its own just-written `.harmonik/crew/missions/<name>.md`. The rule must split by path: **fresh `start crew`** → `--mission` is the only source, on-disk default is ignored (no stale reuse possible); **keeper-restart** → re-read disk (it's the agent's own mission, not stale). D3's invariant holds without breaking restart.

### E — Testability caveat (tester + critic, honest framing of D1)

Native Go makes **~70%** unit-testable (XOR parse, project-hash, sentinel/pid writes, keeper band selection, HOLD gating). But **tmux-takes-effect and `/clear`-injection-into-a-live-pane are integration-only on Linux in ANY language** — the six process-choreography invariants (above) are runtime sequencing, not argv shape, so argv tests don't cover them. **D1's real payoff is "one consolidated, tested Go path" (no bash + no triple-divergence), not "everything becomes a unit test."** ES2 must add a window-nesting argv seam (none exists today); ES3 must **re-author** respawn tests (today they grep the bash file — that coverage dies with ES8).

## Sequencing — ship the operator's actual ask first (critic)

The easy-start UX is small and must not be gated behind the rewrite or HOLD. Three independent tranches:

- **Tranche 1 — the ask (small, days):** ES1 unified `start` + XOR parse + `--name`, ES2/ES3 native captain launch **via the shared helper (A)** so `start captain` is correct (fixes D6), ES4 `start crew <name>` defaults + D3 split rule, ES7 docs. Delivers "incredibly easy to start" + kills the live-broken route.
- **Tranche 2 — crew force-cut (D4):** ES5 flips `crewKeeperWindowArgv` off `--warn-only` onto the shared act+respawn band. Independent.
- **Tranche 3 — HOLD (D5), separate codename, explicit go/no-go:** ES6 with the resolved C design (session-id key + mandatory timer + all-paths gating + hard-ceiling carve-out). Highest correctness risk; lands last.

**Verdicts:** architect NEEDS-WORK→SOUND with consolidation (A); tester GAPS (close the ES2 seam + ES3 re-author); critic TRIM (sequence as above). All three folded above.
