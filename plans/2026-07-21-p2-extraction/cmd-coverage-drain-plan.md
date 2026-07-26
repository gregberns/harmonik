# Plan: close the `cmd/harmonik` coverage gap (LANE 3a)

> Produced 2026-07-23 (session HANDOFF-alpha). Supersedes the QUALITY-AUDIT §4 gap-1 framing —
> see §"Audit correction" at the bottom. The remaining true gap is **floor + drain**, not *existence*.

## 1. Survey — measured reality

`cmd/harmonik` is **not** at zero coverage, and it is **no longer un-gated**. Two audit claims are stale:

| package | coverage | non-test LOC |
|---|---:|---:|
| `cmd/harmonik` | **45.9%** | ~26,830 (63 files, 124 test files exist) |
| `cmd/harmonik/supervise` | 48.4% | 2,648 |
| `cmd/harmonik/digest` | 28.3% | — |

QUALITY-AUDIT §4 gap-1 says cmd is "26,830 non-test LOC with NO coverage gate at all." True when written;
on 2026-07-22 commit `42010150d` added **`scripts/cmd-coverage-gate.sh` + `scripts/cmd-coverage.baseline`**,
wired into tier-2 `make check` (Makefile ~669, isolated GOCACHE). It is a **ratchet, not a floor**: records
current per-package coverage, fails on any ≥0.3pp regression, notices ≥0.3pp improvements. So cmd/** cannot
slide backward; what it lacks is a *floor* and a *drain plan*.

**Largest untested surface** (per-file avg coverage, lowest first):
- **0%, pure-logic, easy wins:** `write_review_verdict_cmd.go` (154), `veto_verdict.go` (172),
  `usage_cmd.go` (180), `greenlight_cmd.go` (127), `state_cmd.go` (235), `remote_control_prefix_cmd.go` (95),
  `goalkeeper_cmd.go` (219).
- **0%, harness/process-driven:** `supervise_cmd.go` (109), `sentinel_cmd.go` (392).
- **<40%, large logic bodies:** `smoke.go` (702, 19%), `sleepwake.go` (423, 17%), `reconcile.go` (291, 19%),
  `decisions.go`+`decisions_k4.go` (1,336, ~26%), `schedule.go` (519, 27%), `crew.go` (805, 34%),
  `captain.go` (651, 37%), `subscribe.go` (504, 37%), `release_cmd.go` (510, 37%), `init_cmd.go` (1,098, 42%),
  `agent.go` (325, 25%), `branch_reap_cmd.go` (240, 4%).

Pure-logic (unit-testable directly): verdict/greenlight/usage/state/decisions parsers, resolve_* config,
digest, migrate_rc_prefix, substrate_select. Harness/integration-scaffold needed: harness.go, run.go,
run_via_daemon.go, supervise_cmd, sentinel_cmd, smoke.go, comms follow-loops, keeper daemon paths.

## 2. Kerf-or-not — recommendation: **one kerf work** `codename:cmd-coverage`

CLAUDE.md gates a kerf work on "new subsystems / cross-subsystem refactors / cross-cutting contracts." A
coverage push on a single existing package is closer to a bench of trivial changes — but two factors tip it
to kerf: (a) it is **cross-cutting within cmd** (63 files, many parallel agents, needs a shared decomposition
+ acceptance contract so agents don't collide or write throwaway tests), and (b) it changes an
**enforced-config contract** (raising the ratchet to a floor is a "protected rule file" change per
coverage-gate design). The spec names the target %, the chunk map, and the gate change. Individual
test-writing tasks under it are trivial and skip their own kerf.

## 3. Parallelizable decomposition (independent chunks)

Group by file-cluster so agents never touch the same file.

| chunk | files | style | ~tests | effort |
|---|---|---|---:|---|
| **A — verdict/gate parsers** | write_review_verdict, veto_verdict, greenlight, confirm_verdict, goalkeeper | pure logic | 25–35 | 0.5d |
| **B — state/usage/misc cmds** | state_cmd, usage_cmd, remote_control_prefix, migrate_rc_prefix, substrate_select | pure logic | 20–30 | 0.5d |
| **C — decisions** | decisions, decisions_k4 | pure logic | 30–40 | 1d |
| **D — lifecycle cmds** | reconcile, branch_reap_cmd, release_cmd, promote_cmd | logic + light fakes | 25–35 | 1d |
| **E — schedule/sleepwake** | schedule, sleepwake | logic + clock fakes | 25 | 1d |
| **F — crew/captain/agent** | crew, captain, agent, start | table-driven arg parse + dispatch | 40 | 1.5d |
| **G — subscribe/comms follow** | subscribe, comms (follow loops) | needs bus fake/harness | 30 | 1.5d |
| **H — supervise/sentinel/smoke** | supervise_cmd, sentinel_cmd, smoke | integration scaffold | 20 | 2d |
| **I — digest & supervise subpkgs** | cmd/harmonik/digest, /supervise | logic | 25 | 1d |

Chunks **A–E are fully independent pure-logic, safe to run 5-wide immediately.** F–I need shared test helpers
(fake bus, fake daemon client, temp-repo fixture) — build those in a **prep task first**, then fan out.
**Sequence: prep-helpers → wave 1 (A–E) → wave 2 (F–I).**

## 4. Gate mechanism — extend, don't invent

The mechanism already exists (`cmd-coverage-gate.sh`). Changes:
1. As each chunk lands, run `scripts/cmd-coverage-gate.sh --write-baseline` in a dedicated "protected rule
   file" commit to **ratchet the baseline up** (matches the `coverage.baseline` bump convention).
2. Once `cmd/harmonik` clears a **floor of 70%**, add an absolute-floor arm to the gate (mirror
   `coverage-gate.sh`'s `FLOOR_THRESHOLD` pattern, cmd-specific) so it can't be silently ratcheted back down.
   70% matches the de-facto floor the command twins already hold (`harmonik-twin-*` sit 62–86%); do **not**
   target the internal-package 90% — cmd has irreducible thin process/`os.Exit` wiring.
3. Keep it in tier-2 `check` under isolated GOCACHE (already there). Do **not** add to `check-fast` (blows the
   15s budget, documented at Makefile ~663).

## 5. Acceptance — "done means…"

- `cmd/harmonik` ≥ 70% (from 45.9%); `cmd/harmonik/digest` ≥ 60% (from 28.3%); `cmd/harmonik/supervise` ≥ 65%
  (from 48.4%).
- No 0%-coverage file remains among the pure-logic set (chunks A–E).
- `cmd-coverage-gate.sh` gains an absolute-floor arm; `cmd-coverage.baseline` re-ratified at the new numbers
  via protected-rule commits.
- `make check` green; new tests are behavior-asserting (not coverage-padding `_ = fn()` calls) — reviewer
  confirms per chunk.
- Kerf spec `codename:cmd-coverage` finalized to `specs/` (if the kerf work is created).

## Audit correction (for the orchestrator to apply)

Update QUALITY-AUDIT §4 gap-1 and §2d: the "no coverage gate at all" / "no baseline entry" statements are now
false; the gate and baseline exist as of `42010150d`. The remaining true gap is *floor + drain*, not
*existence*.
