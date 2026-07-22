# Track C — Enforcement Levers (ready-to-implement spec)

> **Scope:** turn on the structural enforcement levers that are OFF today, as a **ratchet**
> (grandfather every existing violation; cap only NEW/changed code). Mechanical,
> seam-independent, daemon-OFF. This doc is a config diff someone can apply immediately.
> Sources: `PLAN.md` §7 + §8 dec.#5, `ROADMAP.md` (Phase C), `research/06-architecture-and-fp.md` §6,
> and direct inspection of `.golangci.yml`, `Makefile`, `scripts/coverage-gate.sh`,
> `.github/workflows/ci.yml` (working tree 2026-07-13).

---

## 0. The finding that shapes everything: the ratchet already exists

The PLAN/dossier framed Track C as "no ratchet mechanism yet — add `//nolint` + tracking beads."
**That is already built.** The gating CI path does not run a full lint:

- **Merge gate = `make check-short`** (`.github/workflows/ci.yml:42`), which runs
  `.tools/golangci-lint run --new-from-rev=origin/main` (`Makefile:239`).
- **Pre-commit = `make check-fast`** runs `golangci-lint run --new-from-rev=HEAD~1` (`Makefile:218`).
- The **full** `golangci-lint run` (`Makefile:273` `check`, `:358` `lint`) is *deliberately not the
  gate* — it "fails on ~5666 pre-existing legacy issues" (`Makefile:341`); lint is a **merge-time
  gate via `--new-from-rev`, not a full-repo gate**.

**Consequence — the single most important design fact for Track C:** because the gate only reports
issues on new/changed lines, **enabling `cyclop`/`funlen`/`gocognit` grandfathers every existing
violator automatically, with ZERO `//nolint` directives and zero baseline file.** New code (and the
M3-extracted `runexec` functions, P1 `substrate`, etc.) is the only thing capped. This is exactly the
"least-noisy mechanism that caps new code" the PLAN asked for — it's `--new-from-rev`, and it's
already wired. Do **not** add per-function `//nolint`; do **not** hand-maintain a suppression list.

Residual edge (accepted): golangci reports funlen/cyclop/gocognit at the function's `func` line, and
`revgrep` filters by reported line — so editing the *interior* of an existing giant without touching
its signature line won't re-flag it. That is fine: those giants (beadRunOne, runWorkLoop) are being
rewritten by M3 anyway, and their *new* extracted functions get the full ceiling.

Coverage is the same story — a gate already exists (`scripts/coverage-gate.sh`, §3); the PLAN's "no
coverage gate exists today" is imprecise (there is no gate *in `.golangci.yml`*, which is what
dossier 06 §6f literally checked).

---

## 1. Complexity ceiling — the exact `.golangci.yml` changes

### 1.1 Blast radius (measured, non-test, `internal/` + `cmd/`)

Function-length distribution (brace-matched count of functions ≥ N lines):

| ≥ lines | funcs | | ≥ lines | funcs |
|---:|---:|---|---:|---:|
| 50 | 355 | | 200 | 25 |
| 80 | 156 | | 300 | 13 |
| 100 | **104** | | 500 | 8 |
| 150 | 44 | | 1000 | 4 |

Worst offenders: `beadRunOne` 2367, `startWithHooks` 1675, `runWorkLoop` 1544, `cmd main.run` 1269,
`dot/parser.parse` 842, `runKeeperDoctor` 796, `runBeadSubcommandIO` 692, `mergeRunBranchToMain` 525,
`queue Validate` 444, `keeper watcher.Run` 436, `socket handleSocketConn` 421. Packages with the most
≥80-line funcs: `cmd` 55, `internal/daemon` 39, `internal/keeper` 10. (Exact cyclomatic numbers not
computed — not needed: `--new-from-rev` grandfathers all of them regardless of count.)

Chosen thresholds are **principled, not blast-radius-constrained** (since existing violators are
auto-grandfathered): pick values a well-factored *new* function can meet, that still flag genuine
"extract me" cases.

### 1.2 The diff — apply to `.golangci.yml`

**(a) Add to the `linters.enable:` list** (after `- depguard`, line ~40):

```yaml
    - depguard        # import-graph + component-layer rules (subsystem-organization.md)
    # ── Track C: complexity ceiling (ratchet via --new-from-rev; grandfathers existing) ──
    - funlen          # function length ceiling — new/changed code only
    - cyclop          # per-function cyclomatic complexity ceiling
    - gocognit        # cognitive complexity (better "hard to follow" signal than raw cyclo)
```

> `gocyclo` intentionally **omitted**: `cyclop` is the maintained superset of the same cyclomatic
> metric (plus package-average); running both double-reports. Enable `gocyclo` only if you later want
> its distinct per-file default behavior — not needed here.

**(b) Add to `linters.settings:`** (alongside `errcheck`, `revive`, etc., ~line 55–63):

```yaml
    funlen:
      lines: 100          # 104 existing funcs exceed this — all grandfathered by --new-from-rev
      statements: 60
      ignore-comments: true
    cyclop:
      max-complexity: 15  # per-function ceiling (linter default 10; 15 = headroom for real branching)
      package-average: 0  # disabled — rely on the per-function ceiling only
    gocognit:
      min-complexity: 20  # report cognitive complexity above 20 (golangci default 30; 20 is stricter)
```

**Threshold justification:**
- `funlen: 100 / 60` — a 100-line function is a firm "extract this" line; the queue island's
  largest handler (`Validate`, 444) is grandfathered, but new handlers should be far smaller. 104
  existing functions exceed 100 lines — all silently grandfathered by the gate.
- `cyclop: 15` — default is 10; 15 gives headroom for legitimate multi-branch logic while still
  catching the giants. `package-average` disabled because a package-average metric isn't
  line-attributable and interacts poorly with `--new-from-rev`.
- `gocognit: 20` — cognitive complexity weights nesting, so it's the better "unfollowable" signal;
  20 is stricter than the golangci default (30) and appropriate for new code held to the queue-island
  standard.

**(c) Add a complexity exclusion for tests + harness paths** (under `linters.exclusions.rules:`,
~line 42):

```yaml
      # Complexity ceilings target production logic, not table-driven tests or the
      # full-stack scenario harness / self-assert audit packages.
      - path: (_test\.go$|^internal/scenario/|^internal/specaudit/)
        linters:
          - funlen
          - cyclop
          - gocognit
```

Rationale: long table-driven `_test.go` functions are legitimate; `internal/scenario` is the
deferred full-stack harness (already exempt from depguard); `internal/specaudit` is 37.6k test LOC
of assertions. Everything else — including `cmd/` and `internal/daemon` — stays under the ceiling for
*new* code.

---

## 2. depguard fixes

### 2.1 Dead / inert rules — confirmed absent on disk

Verified: `internal/{orchestrator,policy,agentrunner,hook,memory,improvement,adapter,reconciler}`
and `internal/handler/{claudecode,pi,twin}` **do not exist**. Their depguard rules match zero files
(zero enforcement) and give false confidence that a boundary is guarded when it isn't.

| Rule (`.golangci.yml`) | Lines | Status | Action |
|---|---|---|---|
| `policy` | 280–282 | inert (no `internal/policy`) | **Comment-mark "reserved for M5"** (like the existing `orchestrator`/`improvement` stubs at 391–420) |
| `agentrunner` | 350–357 | inert | same — reserved for M5 |
| `hook` | 360–365 | inert | same — reserved for M5 |
| `memory` | 367–373 | inert | same — reserved for M5 |
| `adapter-br` | 319–321 | inert (**real one is `internal/brcli`**) | **Repoint or delete** — the live equivalent is `brcli`; see 2.2 |
| `adapter-ntm` | 322–324 | inert (no ntm adapter) | comment-mark reserved |
| `handler-impls` | 379–384 | inert (handler code is flat in `internal/handler/`) | keep as-is (harmless, documents intent) or comment-mark; its intent is covered by the live `handler-brcli-ban` |

**Decisive recommendation:** convert the six speculative-subsystem rules (`policy`, `agentrunner`,
`hook`, `memory`, `adapter-ntm`, and `adapter-br`'s speculative half) into **commented-out reserved
blocks** matching the existing `# orchestrator:` / `# improvement:` / `# scenario:` convention
(lines 386–420). This keeps the M5 design intent documented without pretending a boundary is
enforced. This is a **no-op for CI** (they match zero files today).

### 2.2 The `daemon` ceiling — enforce direction on the extracted packages

`daemon` (`.golangci.yml:426`) is `allow: [internal/]` with **no `deny`** — the sanctioned
composition root. A blanket deny on daemon is wrong (it legitimately imports every subsystem). The
real ceiling is the **inverse edge**: every package carved *out* of daemon must be forbidden from
importing daemon back, so extracted logic cannot leak home. This pattern is already in force for
`crew`, `keeper`, `presence`, `schedule`, `substrate`, `replay`, `codex-vertical` (each has
`deny: internal/daemon`).

- **`internal/substrate`** — already correct (`deny: internal/daemon` at line 190). No change.
- **`internal/runexec`** (created by **M3**; does not exist yet) — add this rule when the package
  lands, so the extracted run-lifecycle state machine is direction-locked:

```yaml
        # runexec: the extracted run-lifecycle state machine (M3 / codename:run-state-machine).
        # Functional core: pure Step(event)->[]action over the run FSM. MUST NOT import daemon
        # (the daemon is the imperative shell that drives it), preventing logic from leaking back.
        runexec:
          files: ["**/internal/runexec/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/eventbus"
            - "github.com/gregberns/harmonik/internal/substrate"
            - "github.com/gregberns/harmonik/internal/queue"
            - "github.com/gregberns/harmonik/internal/runexec"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "runexec is functional core; the daemon shell drives it — logic must not leak back (M3)" }
            - { pkg: "github.com/gregberns/harmonik/internal/workloop", desc: "runexec MUST NOT import the workloop it replaces (M3)" }
```

  (Trim the `allow` set to what the extraction actually needs; start minimal and add edges as M3
  requires them — the deny edges are the load-bearing part.)

- **Reduce the daemon-unconstrained surface over time:** the ~32/45 packages with no rule are a
  standing gap, but closing it is **M5 (daemon-decompose)** territory, not Track C. Track C's daemon
  lever is precisely the "extracted package denies daemon" pattern above — applied per carve.

### 2.3 The `queue` uuid mismatch (dossier 06 §2.4)

`queue` rule allows only `$gostd` + `core`, but `internal/queue/rpc.go:32` imports
`github.com/google/uuid`. Under an allow-list this is a latent violation. **Track C should add the
edge** (it's a real, in-use dependency, mirrored in `core`/`eventbus`/`workspace` allow-lists):

```yaml
        queue:
          files: ["**/internal/queue/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/google/uuid"   # rpc.go:32 (uuid.NewV7) — real dep, was missing
```

This is safe under the ratchet (adding an allowed edge only *removes* a potential finding).

---

## 3. Coverage floor

### 3.1 A gate already exists — extend it, don't build one

`scripts/coverage-gate.sh` (hk-pvcs.5, `codename:quality-system` lineage) is wired into `make check`
(`Makefile:289`) and `make check-full` (`Makefile:298`). It enforces, per package, off a
`coverage.baseline` protected-rule file (which exists, 2865 B, recalibrated 2026-06-10):

- **95%** for spec-named core subsystems (`HIGH_THRESHOLD_PACKAGES`: core, orchestrator, workspace,
  eventbus, handler, reconciler — note **orchestrator/reconciler don't exist**, same dead-rule
  smell as depguard).
- **90% floor** for every other `internal/**` package.
- **0.3pp** regression gate vs `coverage.baseline`.

**So the carve-target floor is already automatic:** a newly-extracted `internal/substrate` /
`internal/runexec` is an `internal/**` package, so the existing **90% floor applies to it the moment
it lands**. (Reality check: `internal/substrate` is currently at **83.1%, BELOW that floor** — so the
carve gate is RED today, which is exactly why the merge-gate wire-in is deferred; see §3.2/§6.) The
gaps are (a) it doesn't run on the *merge* gate, and (b) its high-threshold list is stale.

### 3.2 Concrete changes

1. **Prune the stale high-threshold entries.** In `scripts/coverage-gate.sh` remove
   `internal/orchestrator` and `internal/reconciler` from `HIGH_THRESHOLD_PACKAGES` (lines 187, 191)
   — they don't exist; leaving them is dead config. Add them back (or `runexec`) when M3/M5 create
   real packages. (Single-concern "protected rule file" commit per the file's own §POLICY.)

2. **Gate the carve targets at merge time, scoped.** `coverage-gate.sh` runs a full `go test
   -coverprofile ./...` — too heavy for `check-short`. Add a **path-scoped** coverage step to
   `check-short` (or a new `check-carve-coverage` target) that measures only the carve packages, so
   the floor gates at merge, not only at declared-done:

   ```make
   # Track C: merge-time coverage floor for the refactor carve targets only (fast, scoped).
   CARVE_PKGS = ./internal/substrate/... ./internal/runexec/...
   check-carve-coverage:
   	@go test -covermode=atomic -coverprofile=/tmp/carve.cov $(CARVE_PKGS) 2>/dev/null || true
   	@scripts/coverage-gate.sh /tmp/carve.cov
   ```

   **Leave `check-carve-coverage` STANDALONE — do NOT wire it into `check-short` yet.** `substrate`
   (the only carve package today) is at **83.1%, BELOW the 90% `internal/**` floor**, so wiring it into
   the merge gate would fail it. Wire-in is **GATED on substrate coverage reaching ≥90%** — a fix that
   touches `internal/**`, outside Track C's config-only scope. The gate is vacuously green only for
   absent packages (the script skips 0-statement / missing packages, lines 140–151).

3. **Seed baseline entries** for the carve targets in `coverage.baseline` at their measured value once
   they land, so the 0.3pp regression gate protects them going forward (ratchet-up on improvement).

**Net:** no new coverage tool — extend `coverage-gate.sh` (fix stale list) + add one scoped
**standalone** `check-carve-coverage` target (NOT yet wired into the merge gate — `substrate` is
below the floor; §3.2). The 90% floor *rule* for extracted packages is inherited for free, but
`substrate` does not pass it yet (83.1%).

---

## 4. Grandfather list — top violators needing tracking beads

These are auto-grandfathered by `--new-from-rev` (no `//nolint` needed), but each should get a
tracking bead so the debt is visible and mapped to the phase that pays it down. **Do not** hand-edit
these functions in Track C.

| Function | Lines | File | Phase that pays it down | Bead note |
|---|---:|---|---|---|
| `beadRunOne` | 2367 | `internal/daemon/workloop.go` | **M3** run-state-machine | extract → each new fn ≤ ceiling |
| `runWorkLoop` | 1544 | `internal/daemon/workloop.go` | **M3** | " |
| `mergeRunBranchToMain` | 525 | `internal/daemon/workloop.go` | **M3** (merge-queue split) | " |
| `startWithHooks` | 1675 | `internal/daemon/daemon.go` | **M5** daemon-decompose | wiring split |
| `run` | 1269 | `cmd/harmonik/main.go` | CLI cleanup (standalone) | subcommand extraction |
| `parse` | 842 | `internal/workflow/dot/parser.go` | standalone | parser refactor |
| `runKeeperDoctor` | 796 | `cmd/harmonik/keeper_enable_doctor_cmd.go` | standalone | |
| `runBeadSubcommandIO` | 692 | `cmd/harmonik/run.go` | standalone | |
| `Validate` | 444 | `internal/queue/validation.go` | low priority (island, well-tested) | acceptable long |
| `watcher.Run` | 436 | `internal/keeper/watcher.go` | **P1** session-restart-substrate | already in flight |
| `handleSocketConn` | 421 | `internal/daemon/socket.go` | M5 | |

**Recommendation:** file **one umbrella tracking bead** `codename:complexity-grandfather` listing the
above, then let M3/M5/P1 close their slices as they extract. Don't scatter 11 standalone beads.

---

## 5. Reconciliation note (advisory — do NOT modify these works)

Per `ROADMAP.md:51,104`, Track C **folds into existing works; it does not create a new codename.**

- **`quality-system`** (status: `tasks`, 15/15 beads) — this is the "phased core-loop-proof + **gate
  bootstrap**" work; `coverage-gate.sh` + `coverage.baseline` (hk-pvcs.5) are its offspring. The
  **coverage lever (§3)** folds here: it's a continuation of the same gate-bootstrap lineage
  (extend the script, prune stale packages, add the scoped merge-time step). No supersede.
- **`validation-net`** (status: `ready`, nominally 1/1 beads — but **hollow: 12 of 13 spec'd beads,
  incl. flagship VN4 `hk-ukhzu`, are ABSENT from the `br` DB; re-file pending**) — the acceptance-oracle
  protective net. The **complexity + depguard levers (§1, §2)** reconcile here as the "structural
  protective net" the ROADMAP names. Fold in. (Scope call B1 pending — see TASKS.md validation-net rows.)
- The `.golangci.yml` complexity/depguard diff itself is **direct (no kerf pass needed)** — ROADMAP
  marks Phase C "no (direct)". Only the coverage-script edits touch a `quality-system` artifact.
- **No net-new codename.** Do not spin up a `track-c-enforcement` kerf work; land the config diff
  directly and attach the grandfather umbrella bead under `quality-system`/`validation-net`.

---

## 6. Implementation order (land it green)

1. **`.golangci.yml` — enable + configure (§1).** Add `funlen`/`cyclop`/`gocognit` to `enable`, add
   their `settings`, add the test/harness `exclusions` rule. Same commit: the depguard `queue` uuid
   edge (§2.3) and the dead-rule comment-marking (§2.1).
2. **Verify grandfathering.** On a scratch no-op branch:
   `.tools/golangci-lint run --new-from-rev=origin/main` → **expect 0 new issues (green)**. This
   proves existing violators don't gate. (`make check-short` reproduces the full CI gate.)
3. **Confirm the ratchet bites.** Add a throwaway 120-line function on the branch and re-run step 2 →
   **expect a funlen finding** on the new function. Delete the probe. This proves new code is capped.
4. **Coverage (§3).** Prune stale `HIGH_THRESHOLD_PACKAGES` entries in `coverage-gate.sh`
   (single-concern protected-rule commit); add `check-carve-coverage` **as a STANDALONE target — do
   NOT wire it into `check-short`**. `substrate` (the only carve package today) is at **83.1%, BELOW
   the 90% floor**, so the carve gate is RED; wire-in is **gated on substrate ≥90%** (a fix touching
   `internal/**`, out of Track C scope).
5. **File the grandfather umbrella bead (§4)** `codename:complexity-grandfather`; link the M3/M5/P1
   slices.
6. **Land as ≤2 commits:** (a) `.golangci.yml` config + depguard cleanup; (b) coverage-script +
   Makefile. Keep the `coverage.baseline` protected-rule commit separate if any baseline entry
   changes (per the file's §POLICY). Merge-gate stays green throughout because `--new-from-rev` sees
   no new violations on these commits.
7. **When `internal/runexec` lands (M3):** add its depguard rule (§2.2) and its baseline entry (§3.3)
   in that same M3 change — Track C pre-authorizes both.

---

### Appendix — commands run for the blast-radius / infra findings
```
find internal cmd -name '*.go' ! -name '*_test.go' | (brace-match func lengths)   # §1.1 table
grep -nE 'new-from|coverage|golangci' Makefile                                    # §0 gate wiring
cat scripts/coverage-gate.sh ; ls coverage.baseline                               # §3 existing gate
grep 'run: make' .github/workflows/ci.yml                                         # merge gate = check-short
for p in orchestrator policy agentrunner hook memory ...; do test -d internal/$p  # §2.1 dead rules
```
</content>
</invoke>
