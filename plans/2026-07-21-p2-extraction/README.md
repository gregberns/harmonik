# P2 EXTRACTION — master execution index

## What this folder is

`_plan.md` is the **strategy**: why the two god packages (`internal/daemon`, `internal/core`) get carved
up, what seams the work exits behind, and the unit ordering. The `E*.md` files are the **executable
per-unit plans** — each one reconciled against an adversarial challenge, with a step-by-step recipe, the
exact files that move, and the traps that are invisible to `go build`. This README is the **map and the
status board**: what ships next, what gates what, and the running measurement of the daemon shrinking.

---

## 1. Status board

Status derives from the **reconciled** verdict in each `E*.md`, not the optimistic first-pass recon.

| Unit | What leaves `internal/daemon` | Size (non-test / total moved) | Risk | Status | Plan file |
|---|---|---|---|---|---|
| **E1a** | codex harness impl → `internal/harness/codex` (+ shared trailer/env leaf) | 2,083 / 5,522 LOC, 16 files | LOW-MED | **NEEDS PREP SLICE** (`E1a-0` → `E1a-1`) | [E1a-codex-harness.md](E1a-codex-harness.md) |
| **E1b** | claude harness impl → `internal/harness/claude` | 843 / 2,221 LOC, 4 files + 2 struct evictions | **MEDIUM** | **NEEDS PREP SLICE** (`E1b-prep` → `E1b`) | [E1b-claude.md](E1b-claude.md) |
| **E1c** | pi harness impl → `internal/harness/pi` | 1,458 / 4,955 LOC, 13 files | MEDIUM | **NEEDS PREP SLICE** (hard-gated on E1a) | [E1c-pi.md](E1c-pi.md) |
| **E2** | crew wiring → `internal/crewrun` | 616 of 1,206 LOC portable, 7 files | MEDIUM | **E2a READY** / **E2b BLOCKED** (needs operator waiver) | [E2-crew.md](E2-crew.md) |
| **E3** | queue wiring → `internal/queuewiring` | 1,455 / 3,642 LOC, 14 files | MEDIUM | **E3a READY** / **E3b NEEDS PREP SLICE** | [E3-queue-wiring.md](E3-queue-wiring.md) |
| **E4** | reverse tunnel + code-sync → `internal/transport/{tunnel,codesync}` | 619 / 1,702 LOC, 5 files | MEDIUM | **READY** (E4a+E4b+opt E4c); **E4d PARKED → E5** | [E4-ssh.md](E4-ssh.md) |
| **E5** | DOT run-loop → `internal/runloop` | 13,950 non-test LOC, 18+ files | **HIGH** | **NEEDS PREP STREAM** (RT13 landable now; lift out of scope) | [E5-dot-runloop.md](E5-dot-runloop.md) |
| **E6** | `internal/core` split by type-family | 0 this cycle (31,821 non-test LOC inventoried) | HIGH if executed | **PARKED** | [E6-internalcore.md](E6-internalcore.md) |

### Shippable slices, in landing order

| Slice | Ships | Gated on | Status |
|---|---|---|---|
| `E3a` | `queuestore` + ledger-bridge + operator-event-consumer (818 non-test LOC, **zero outbound daemon coupling**) | nothing | READY — cleanest unit in P2 |
| `E4a` | `reversetunnel.go` → `internal/transport/tunnel` (11 exports) | nothing | READY |
| `E4b` | `codesync.go` → `internal/transport/codesync` (2 exports) | nothing | READY |
| `E2a` | crew launchspec + harness resolver + mission front-matter + idle reaper | nothing | READY |
| `E5-RT13` | merge-path carve-out → `runmerge` | nothing | READY (prep, not extraction) |
| `E1a-0` | `internal/harness/shared` trailer/env leaf + pi/daemon rewire + `harness-*` depguard block | nothing | prep — **owns the depguard block** |
| `E1a-1` | the codex move itself | `E1a-0` | after prep |
| `E1b-prep` | evict the mis-named `claudeRunCtx` universal launch DTO into `internal/harness/shared` | E1a landed (or owns the block itself) | prep |
| `E1b` | claude move | `E1b-prep`, E1a landed | sequential after E1a |
| `E1c` | pi move (+ prep slice B: exported `pi.RunCtx` / `pi.BuildLaunchSpec`) | **E1a hard** | sequential after E1a/E1b |
| `E4c` | `buildWorkerRegistry` → `internal/workers/bootwire.go` | E4a/E4b | OPTIONAL, separate PR (has a signature change) |
| `E3b` | `dispatchsegment` + `perqueuespendmeter` | prep slice (RunRegistry closure port) | after E3a |
| `E2b` | `crewstart.go` handler | prep slice `E2-P` **+ operator waiver of the no-new-seam rule** | BLOCKED |
| `E5-RT14` | **retire the open-coded agent_ready wait** — both remaining sites bind onto the pre-existing `dispatchSegment` seam | **nothing.** Explicitly NOT gated on E1/E4: the converted regions call no `reversetunnel.go` symbol and no harness symbol that has not already moved | **LANDED** (`229e6e91` · `cb89e35e` · `7d448afb`) |
| `E5` RT15→RT19 + lift | the run machine | E1a+E1b+E1c+E4 all landed | not staffable yet |
| `E5-RT19c` | clock-port the Working-phase watchdogs (32 raw wall-clock sites — the 30 measured plus two `time.Since` the inventory grep missed) | nothing hard; was **cheaper after RT15** (the ports bundle makes threading a clock nearly free) | **LANDED** (`f839121ff`) |

---

## 2. Settled decisions — **ASSUMED-AND-PROCEEDING** (operator may veto any one)

These are `_plan.md` §"Questions for operator" 1–5, answered. Every `E*.md` recipe is written against
these answers. Vetoing one invalidates the recipes that depend on it (named per row).

| # | Question | **Assumed answer** | Invalidates if vetoed |
|---|---|---|---|
| 1 | Package home for harness impls | **Top-level `internal/harness/{claude,codex,pi}`**, not a daemon sub-package — P3 needs a container that links the harness without dragging the 57k-LOC monolith. Shared cross-harness helpers go to `internal/harness/shared` (leaf: core/handler/stdlib only). | E1a, E1b, E1c entirely |
| 2 | Freeze tripwire severity | **HARD CI failure** (depguard deny edge + freeze-gate script), not a review warning. 939 commits/60d will walk straight past a warning. | every unit's §5 gate step 6 |
| 3 | Timing of E6 (core split) | **PARKED.** Name the type-families, do not cut them. Core churn has already collapsed (~6/day → ~1.3/day); every one of the 13 families sits in a mutual cycle with another. | E6 only (already written as parked) |
| 4 | DECISIONS.md guardrail ratification | **Ratified.** substrate-v2's kill-criteria + boundary tests are the pre-agreed ground rule for P2 scope disputes: "is this in the unit?" is settled by the boundary test (`go list -deps \| grep daemon`), not by argument. | the §3.4 boundary-test gate in every unit |
| 5 | Crew harness for P2 | **Codex.** Run the P2 extraction crew on the Codex harness once E1a proves Codex operational (PRIORITY-0 token conservation); Claude reserved for the pure-move review gate. | staffing only, not the recipes |

**One decision this README escalates rather than assumes:** E2b requires an **explicit operator waiver**
of `_plan.md` §1's no-new-seam rule (it needs a brand-new `crewSpawner` port, which §5.1 makes a review-gate
rejection). Do not start E2b on an assumed answer. See [E2-crew.md](E2-crew.md).

---

## 3. Execution order and why

```
  now, in parallel (no interdependencies, disjoint files):
    E3a  ──┐
    E4a  ──┤   E4's LEAF half is deliberately early: reversetunnel.go / codesync.go
    E4b  ──┤   are clean leaves with zero daemon-internal free identifiers. Only the
    E2a  ──┤   workloop-embedded half (E4d) is gated on E5.
    RT13 ──┘

  strictly SEQUENTIAL (see below):
    E1a-0 → E1a-1 → E1b-prep → E1b → E1c

  then:
    E3b (after E3a + closure-port prep)
    E4c (optional, after E4a/E4b)
    E2b (BLOCKED on E2-P + operator waiver)

  last:
    E5  RT14 LANDED (needed nothing); RT15→RT19 then the lift [needs E1a+E1b+E1c+E4]
    E5  RT19c (Working-phase watchdogs) LANDED (needed nothing)
    E6  PARKED
```

**Why E1a/E1b/E1c must be sequential, not parallel.** All three edit the same two files —
`internal/daemon/harnessregistry.go` (registry assembly) and `internal/daemon/export_test.go` (the shim
block). E1b additionally does a 182-reference rename across 42 daemon test files, and E1a/E1c both rewrite
`picommit.go` / `codexcommit.go` neighbours. Run in parallel they produce merge-conflict warfare in exactly
the files `_plan.md` R3 names. One at a time, one release each.

**Why E1a goes first inside E1.** It owns the `internal/harness/shared` cross-harness commit/env helpers
and the `harness-*` depguard block. **E1c is hard-blocked on E1a putting those helpers in
`internal/harness/shared`, not in `internal/harness/codex`** — `harness/pi` importing `harness/codex` is the
"daemon back-edge in disguise" §6 forbids. Tell whoever runs E1a before they start.

**Why E4's leaf half goes early.** It is unblocked, it is P3-critical, and it touches none of the files
E1/E2/E3 touch. Do NOT attempt E4d (the workloop-embedded remote bits) — `remoteBeadCtx` is declared inside
`beadRunOne`'s body and has no name outside it; it cannot compile as a move.

**Why E5 is last and is a stream.** The RT ports seam is far shallower than `_plan.md` §2 assumes:
`RunPorts` has 5 reference sites total, and `RunEnv` / `SharedHandles` are declared and referenced nowhere
(dead code). ~10% of the designed ports work exists. Outbound coupling is 165 non-method daemon symbols
across 55 files. Every prior unit drains part of that set; that is what makes E5 possible at all.

**Merge-conflict hygiene (`_plan.md` R3).** `workloop.go`, `dot_cascade.go`, `reviewloop.go`,
`dot_gate.go`, `sessioncontext_chb023.go` and `pasteinject.go` are **modified-uncommitted on this branch
right now**. Land or stash that work before starting any unit, and land each prep slice the same day it
starts. Never leave a half-renamed tree overnight.

---

## 4. The invariant gate every unit clears

`_plan.md` §5, as a copy-pasteable block. Run from `/Users/gb/github/harmonik`. Substitute `<newpkg>`
(e.g. `./internal/harness/codex/...`) and `<unit>`.

```bash
# ── 0. BEFORE the unit: capture the differential-test baseline ────────────────
# (per 00-test-oracle-baseline.md — internal/daemon is ALREADY RED at HEAD;
#  the gate is "no NEW failures", not zero. Serialize this run: no agent fan-out.)
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/p2-<unit>-before-failures.txt

# ── 1. PURE-MOVE REVIEW ───────────────────────────────────────────────────────
# Diff must be git mv + package rename + import fixups. Every logic delta called
# out explicitly in the PR body, or the review gate rejects. Extraction must NOT
# invent a new seam — it exits behind a pre-existing one.
git diff --stat HEAD
git log --format='%H' -1 && make agent-review     # APPROVE required to commit

# ── 2. DEPGUARD / BOUNDARY TEST (the scope-dispute ground rule, §3.4) ─────────
go build ./internal/... ./cmd/...                 # NOT ./... — plans/ has an untagged main
go vet   ./internal/... ./cmd/...
go vet -tags scenario        ./internal/daemon/...
go vet -tags specaudit       ./internal/specaudit/...
go vet -tags e2e_real_claude ./internal/daemon/...
go list -deps <newpkg> | grep 'gregberns/harmonik/internal/daemon' \
  && { echo "BOUNDARY FAIL"; exit 1; } || echo "BOUNDARY OK"
.tools/golangci-lint run <newpkg> ./internal/daemon/...   # FULL run, not --new-from-rev:
                                                          # moved files are 100% new lines

# ── 3. DIFFERENTIAL GREEN (replaces "go test ./... is green") ────────────────
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/p2-<unit>-after-failures.txt
comm -13 /tmp/p2-<unit>-before-failures.txt /tmp/p2-<unit>-after-failures.txt   # MUST be empty
make specaudit-lint            # tagged spec-drift sensors — path-pinned tests live here
make test-scenario             # scenario tier; the strongest behavior oracle for E1
make fmt-check

# ── 4. RUNTIME PROOF (units with a run surface: E1, E4, E5) ──────────────────
# E1: drive one codex bead + one claude bead through DOT (impl→review→merge),
#     confirm identical outcome vs pre-extraction.  E4: drive one remote bead.
# Cannot be cleared at design time. Either run it, or name in the PR the bead
# that owns the deferred proof. NEVER record a §5.4 pass you did not run.

# ── 5. BUG SCAN ───────────────────────────────────────────────────────────────
ubs $(git diff --name-only HEAD | grep '\.go$')

# ── 6. FREEZE TRIPWIRE + RELEASE ─────────────────────────────────────────────
scripts/<concern>-freeze-gate.sh          # hard CI failure, per settled decision #2
# One unit = one bead = one release. Publish the shrink number in the close comment:
ls internal/daemon/*.go | grep -v _test | wc -l
ls internal/daemon/*.go | grep -v _test | xargs cat | wc -l
```

**Known-flaky allowlist** (re-run in isolation before calling any of these a regression):
`TestThroughput_TenBeadsAtMaxFour` (hard pre-existing, out of scope), `TestScenario_Hk6ynv4_SubscribeStream_EndToEnd`,
`TestStopHookE2E_TwinRelayFastPath`, `TestStopHookE2E_TwinRelayWaitGrace`,
`TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace`, `TestT6_10BeadSequentialDrain`.

**Traps the default gate does NOT catch** — this is why steps 2 and 3 carry tagged/audit runs:
`//go:build specaudit` tests hardcode file paths (`hc045a_claudecode_bridge_pointer_test.go` pins
`internal/daemon/claudeharness.go`; `wminv003_task_branch_append_only_test.go:416` allowlists
`internal/daemon/codexcommit.go` by literal path); `conformance_m4c7_test.go` reads source files by
hard-coded path and `t.Fatalf`s on a move; `scripts/coverage-gate.sh` inherits the 95% core threshold to
any new `internal/core/*` sub-package; `revive`'s `exported` + `package-comments` rules fire on every
renamed identifier whose doc comment still leads with the old lowercase name, and on any new package
lacking a `// Package x …` block above the package clause.

---

## 5. Progress ledger

Baseline measured **2026-07-22** on `phase1-session-restart-substrate` (HEAD `34509e60` + staged
gitprobe/harness-shared work). Both scopes recorded so later measurements are comparable — **`_plan.md`
§0's "631 non-test files / 56,583 LOC" is a different count** (631 ≈ total `.go` files recursive, 651
today); use the numbers below, not the plan's.

| Date | Unit | daemon non-test files (top-level / recursive) | daemon non-test LOC (top-level / recursive) | delta |
|---|---|---|---|---|
| 2026-07-22 | *(baseline, pre-extraction)* | **126** / **134** | **57,197** / **58,971** | — |

Reference totals at baseline: all `internal/daemon` `.go` files recursive = **651 files / 214,425 LOC**
(so ~156k LOC of it is test mass). `internal/core` = 504 files / 105,397 LOC (239 non-test / 31,821).

Measurement commands (run both scopes, record both):

```bash
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
find internal/daemon            -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon            -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
```

Projected drops, for sanity-checking a unit's close comment (top-level non-test LOC):
E4a+E4b −619 · E3a −818 · E1a −2,083 · E1c −1,458 · E1b −843 · E2a −616 · E3b −637.

---

## 6. Already landed (E1a preparatory work, in the tree now)

Staged-but-uncommitted on `phase1-session-restart-substrate`. **Do not revert; build on it.**

- **`internal/gitprobe/`** — read-only git probe helpers extracted out of daemon, with its own depguard
  allow/deny block (`.golangci.yml:164-178`; allow `$gostd` + `internal/lifecycle/tmux` + self, deny
  `internal/daemon`). Several daemon files already rewired to call it instead of inline git. Its package
  doc is the canonical statement of the no-new-seam discipline: injecting the probe as a function field
  "would have paid for a seam twice and invented one the extraction plan says not to invent."
- **`internal/harness/shared/seedprompt.go`** — moved from `internal/daemon/agentseedprompt.go`.
  `codexlaunchspec.go` already imports it. So `internal/harness/shared` **exists**.
- **Open debt from that landing:** `internal/harness/**` has **no depguard block yet** (verified — grep
  `harness` in `.golangci.yml` returns only comments). Whichever slice lands first (`E1a-0`) MUST add it;
  otherwise the shared leaf ships unfenced and E1c inherits the debt.
- **Orphaned coverage from that landing:** `internal/daemon/agentseedprompt_test.go` asserts pi+codex
  resume-seed-prompt parity in one table, but its production half already moved. It cannot move to
  `harness/codex` (pi half) and cannot stay unchanged (codex half). E1a and E1c both must handle it.
