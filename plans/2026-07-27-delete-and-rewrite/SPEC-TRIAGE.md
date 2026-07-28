# Spec traceability report — `specs/` as a rewrite oracle

**Date:** 2026-07-28 · **Repo:** `/Users/gb/github/harmonik` @ `phase1-session-restart-substrate`
**Commissioned by:** `plans/2026-07-27-delete-and-rewrite/NEXT_STEPS.md` §1 ("Build the traceability report — it is now the only spec-drift detector that could exist")
**Status:** read-only audit. No repo file was modified.

Machine data: `traceability.csv` (1,164 rows) and `trace.json` beside this file; generator `trace.py`.

---

## 0. Bottom line

`specs/` is **not** uniformly normative and must not be handed to a rewrite as one oracle.
Of 1,164 requirement IDs, **783 (67.3%) are cited in production Go**, **88 (7.6%) only in tests**, and
**293 (25.2%) appear in no Go file at all.** The orphans are concentrated: **six spec files hold 62%
of them**, and four of those six describe designs that have been deleted or superseded.

Three dispositions fall out: **12 spec files (660 IDs) are usable as an oracle today**, **19 need
triage before use**, and **4 should be deleted or gutted before the rewrite reads them**. A
stratified 119-ID sample of the orphans (40.6% of the frame) puts **48% at "citation missing, code is
fine"** but **40% actively wrong — and stale beats aspirational 2.2 : 1.**

---

## 1. The ID format, and the universe

Requirement IDs are declared in four interchangeable markdown shapes — there is no single canonical
form, which is itself a small finding:

| Shape | Example | Seen in |
|---|---|---|
| ATX heading | `#### SK-001 — Keeper composes over five ports` | session-keeper, handler-contract |
| Numbered heading | `### 8.4 HP-016 — Schema versioning` | handler-pause, event-model |
| Bold bullet, dash | `- **CL-030 — Digest produced by \`harmonik digest\`.**` | cognition-loop |
| Bold bullet, no dash | `- **PI-001** The work MUST land in three phases` | pi-harness |

The prefix is authoritative and registered: `specs/_registry.yaml` maps 31 prefixes to spec-ids
(`SK → session-keeper`, `ON → operator-nfr`, …). **An ID belongs to the spec that owns its prefix**,
regardless of which file the occurrence sits in — this is the correct attribution and it differs
from the one used in NEXT_STEPS.md (see §7, "where this report disagrees").

**Counts.** Scanning all 44 `.md` files under `specs/`:

| Form | Count | Included in the headline? |
|---|---:|---|
| Base requirement `PFX-NNN[a]` | **1,164** | **yes — this is the universe** |
| Open questions `OQ-PFX-NNN` | 153 | no — questions, not requirements |
| Invariants `PFX-INV-NNN` | 102 | no — counted separately |
| Env-var IDs `PFX-ENV-NNN` | 14 | no |
| Other sub-forms (`-EX-`, `-MIG-`, `-RIA-`) | 3 | no |
| **Total distinct ID-shaped tokens** | **1,436** | |

The 1,164 base figure reproduces NEXT_STEPS.md's "1,180 unique requirement IDs" to within 1.4%, and
the orphan count (293 / 25.2%) reproduces its "291 (25%)" to within 0.7%. **The prior measurement is
sound; this report extends rather than corrects it.**

**37 IDs are referenced but never defined** anywhere in `specs/` — dangling cross-references. These
are pre-classified category D (see §3).

---

## 2. Classification: CITED-IN-PRODUCTION / CITED-ONLY-IN-TESTS / UNCITED

Corpus: 2,601 tracked `.go` files — 900 production, 1,701 test (`_test.go` or under `testdata/`).
A requirement is CITED if its ID string appears anywhere in the file (in practice, a `// Spec:` comment).

| Class | Count | % |
|---|---:|---:|
| CITED-IN-PRODUCTION | 783 | 67.3% |
| CITED-ONLY-IN-TESTS | 88 | 7.6% |
| UNCITED | 293 | 25.2% |
| **Total** | **1,164** | |

**Per spec, sorted by orphan rate.** The `1-file` column counts IDs cited in exactly one production
file — a citation thin enough that it is usually a lone comment, not enforcement.

| Spec | IDs | PROD | TEST-only | UNCITED | orphan% | 1-file |
|---|---:|---:|---:|---:|---:|---:|
| `reconciliation/spec.md` | 43 | 30 | 13 | 0 | 0.0% | 9 |
| `control-points.md` | 63 | 57 | 5 | 1 | 1.6% | 10 |
| `workspace-model.md` | 59 | 42 | 16 | 1 | 1.7% | 10 |
| `claude-hook-bridge.md` | 29 | 28 | 0 | 1 | 3.4% | 6 |
| `beads-integration.md` | 53 | 41 | 9 | 3 | 5.7% | 10 |
| `run-state-machine.md` | 35 | 33 | 0 | 2 | 5.7% | 7 |
| `handler-contract.md` | 94 | 81 | 6 | 7 | 7.4% | 13 |
| `event-model.md` | 65 | 58 | 2 | 5 | 7.7% | 16 |
| `scenario-harness.md` | 37 | 34 | 0 | 3 | 8.1% | 7 |
| `queue-model.md` | 54 | 46 | 3 | 5 | 9.3% | 9 |
| `execution-model.md` | 93 | 78 | 6 | 9 | 9.7% | 15 |
| `process-lifecycle.md` | 60 | 42 | 11 | 7 | 11.7% | 9 |
| `replay-substrate.md` | 22 | 17 | 1 | 4 | 18.2% | 6 |
| `workflow-graph.md` | 55 | 41 | 4 | 10 | 18.2% | 9 |
| `sub-workflow-dispatch.md` | 10 | 8 | 0 | 2 | 20.0% | 5 |
| `agent-input.md` | 22 | 17 | 0 | 5 | 22.7% | 3 |
| `pi-harness.md` | 35 | 25 | 1 | 9 | 25.7% | 5 |
| `system-state.md` | 17 | 11 | 0 | 6 | 35.3% | 4 |
| `session-keeper.md` | 21 | 12 | 1 | 8 | 38.1% | 5 |
| `credential-isolation.md` | 14 | 7 | 1 | 6 | 42.9% | 3 |
| `operator-nfr.md` | 88 | 33 | 7 | 48 | 54.5% | 18 |
| `digest-command.md` | 11 | 4 | 0 | 7 | 63.6% | 1 |
| `handler-pause.md` | 42 | 11 | 1 | 30 | 71.4% | 5 |
| `cognition-loop.md` | 50 | 13 | 0 | 37 | 74.0% | 4 |
| `architecture.md` | 54 | 13 | 1 | 40 | 74.1% | 6 |
| `harness-contract.md` | 24 | 1 | 0 | 23 | 95.8% | 1 |
| `claude-launchspec.md` | 14 | 0 | 0 | 14 | 100.0% | 0 |
| **TOTAL** | **1,164** | **783** | **88** | **293** | **25.2%** | **196** |

**Six files hold 182 of the 293 orphans (62%)**: operator-nfr (48), architecture (40),
cognition-loop (37), handler-pause (30), harness-contract (23), claude-launchspec (14).

### 2a. Eight spec files declare no requirement IDs at all

2,891 lines of `specs/` are **unfalsifiable by construction** — they contain zero IDs of their own,
so no traceability check can ever run against them:

`promote.md` (162), `release-pipeline.md` (363), `hitl-decisions.md` (252), `flywheel-motion.md`
(450), `assessor-handoff-schema.md` (338), `crew-handoff-schema.md` (263),
`park-resume-protocol.md` (408), `pi-provider-switch.md` (655).

Four of them — `PR`, `RP`, `HD`, `FW` — hold a **reserved-but-never-used prefix** in
`_registry.yaml`. The registry lint checks only that a declared prefix is *present and unique* in
the registry, so it passes vacuously on all four. `flywheel-motion.md` is the sharpest case: its
front matter claims `status: normative`, it is load-bearing (§0.2 is the standing prohibition on
rebuilding the cognition loop), and it cannot be traced to a single line of Go.

### 2b. Two secondary signals, one useful and one broken

- **`harness-contract.md` was superseded, and the citation counts prove it.** `HN-*` appears in 5 Go
  files with **exactly one unique ID** (`HN-022`). `PI-*` — the pi-harness prefix — appears in
  **49 files with 25 unique IDs**. The Go tree has voted; `pi-harness.md` is the live harness
  contract and `harness-contract.md` is a historical document with a 95.8% orphan rate.
- **The `last-updated` front-matter field is not trustworthy as a staleness proxy.** It disagrees
  with the file's last git commit in **19 of 34 specs** (`workspace-model.md` claims 2026-05-13; git
  says 2026-07-21). Correlation between front-matter age and orphan rate is only r = 0.39. Specs
  touched in the last 35 days average 19.0% orphan vs 42.8% for older ones — the *direction* is
  right, but the field is too drifted to use as a gate. **Use the git date, not the front matter.**

### 2c. "CITED-IN-PRODUCTION" is an upper bound, not a measure of enforcement

A citation is an ID string in a `.go` file — almost always a comment. It proves someone once linked
the code to the requirement; it does **not** prove the code enforces it. **196 of the 783 cited
requirements (25%) are cited in exactly one production file.** A random sample of 10 of those:

| ID | File | What the citation actually is |
|---|---|---|
| CP-049 | `internal/core/skillname.go` | **real enforcement** — `skillNameShapeRegex enforces the CP-049 shape` |
| BI-010c | `internal/brcli/workflowlabelwrite_bi010c.go` | file-header `Spec ref:` on a purpose-built file — plausible |
| RSM-002 | `internal/mergeq/mergeq.go` | file-header narrative ("FIFO single-executor queue (RSM-002 / RSM-012)") |
| RC-012 | `internal/lifecycle/readystate_pl009.go` | doc comment on a bool field |
| SK-016 | `internal/keeper/cycle.go` | trailing `Spec ref:` covering a five-ID range |
| HC-057 | `internal/daemon/claudeheartbeat.go` | narrative "MVH carve-out (HC-057)" |
| ON-027 | `internal/lifecycle/reconciliationlock_rc002a.go` | parenthetical inside an unrelated comment |
| ON-001 | `internal/workspace/errors.go` | bracketed cross-reference in prose |
| CHB-022 | `cmd/harmonik-twin-claude/main.go` | narrative topology note |
| BI-032 | `internal/testhelpers/crashharness.go` | **test-helper file** counted as production by path |

**One of ten is enforcement.** Treat 67.3% as the ceiling on traceability, not the floor on
correctness — and treat the 196 single-file citations as a third tier needing the same polarity check
as the orphans. This is the same failure mode NEXT_STEPS.md measured from the other direction ("does
the named symbol exist?" scores 84% category A and misclassifies every stale requirement).

---

## 3. What the 293 orphans actually are

### Sampling method

**Stratified random sample, n = 119 of N = 293 (40.6%).** Strata are the owning spec files; each
stratum contributed `max(3, ceil(n_stratum/3))` IDs drawn with a fixed seed, so small specs are
over-sampled relative to proportional allocation (deliberate — it buys per-spec statements). All 26
specs with at least one orphan are represented; 4 specs were sampled exhaustively.

Each ID was classified by a reader applying the **polarity check**, not a symbol-existence check:
read the requirement verbatim → identify the MUST / MUST NOT / pinned value or path → grep for the
*mechanism* in Go → compare polarity and value, not existence. Four readers worked in parallel on
disjoint strata. **Absence of a `// Spec:` comment was explicitly ruled out as evidence**, since it
is true of every ID in the frame by construction.

### Results

| Cat | Meaning | n | % of sample | 95% CI | Extrapolated to 293 |
|---|---|---:|---:|---:|---:|
| **A** | **Implemented, untagged** — behavior exists and matches; only the citation is missing | 57 | **47.9%** | ±6.9pp | **~140** |
| **C** | **Stale** — described a real design since replaced or deleted; code does something different or opposite | 33 | **27.7%** | ±6.2pp | **~81** |
| **B** | **Aspirational** — never built | 15 | **12.6%** | ±4.6pp | **~37** |
| **D** | **Not a clean requirement** — unfalsifiable, duplicate, retired, or an ID-extraction artifact | 14 | **11.8%** | ±4.5pp | **~34** |

CIs are normal-approximation with a finite-population correction; they do **not** capture inter-rater
variance across the four readers, which is the larger uncertainty here. Treat the ordering
(A > C > B ≈ D) as solid and the individual points as ±7pp.

**B + C = 40.3% of orphans are actively wrong** — roughly **118 of 293**. This is close to the prior
audit's 36% and confirms its central claim: **stale dominates aspirational, 2.2 : 1.** The specs are
not wishful. They are rotted.

### The three categories, with the evidence that separates them

**A — implemented, untagged (~140 IDs).** Cheap. Examples: `SS-004`/`SS-005` are literal
transcriptions — `internal/daemon/stategather.go` `RollUpLabel`'s clauses match the spec's boolean
algebra term for term, including the fail-awake `WAITING ⊇ {UNSURE}` case. `SK-005` is stronger than
a match: `internal/keeper/ports.go` declares `type EmitterPort = Emitter`, a literal type alias, which
is exactly what the requirement demands. `RSM-028` is machine-enforced — `.golangci.yml` carries
depguard entries for `runexec` and `mergeq` denying `internal/daemon`, and one of them names RSM-028
in its message. `DC-003/006/009`, `CLS-011/022/023/031`, `EV-045/047`, `QM-007/056`, `RS-003/011/014`
all check out on every pinned value.

**C — stale (~81 IDs). This is the category that costs you, and it is invisible from the spec side.**
Every one of these has a real, existing Go symbol doing the opposite:

| ID | Spec pins | Code does |
|---|---|---|
| `HP-002` | `schema_version` read-set is `{1}`; anything else MUST refuse startup | `handlerStateSchemaVersionDaemon = 2` — the daemon **writes** the file the spec says to reject |
| `HN-012` | resolver MUST fail closed on unknown selector | `resolveHarness` has no error return; falls through to `return core.AgentTypeClaudeCode` |
| `HN-018` | `Teardown` MUST be a no-op for a `ProcessExit` harness | `internal/harness/codex/harness.go` `Teardown` calls `sess.Kill(...)` |
| `CLS-040` | `isHarmonikManagedWorktree` emits **iff** the canonicalized prefix matches | the function's own doc comment says "unlike an earlier revision, an empty `worktreeRootPath` does **NOT** force false" — the exact inverse |
| `PL-006f` | argv is never provenance, "never as the match itself" | `internal/lifecycle/agentwatcherreap.go` matches on `ps -eo pid,args` then SIGTERM/SIGKILL; `orphansweepbr.go` matches on `comm != "br"` |
| `SW-008` | unregistered context key MUST warn-and-drop | `internal/core/edgecascade.go` `ApplyContextUpdates` writes every key unconditionally; the validating variant has **zero production callers** |
| `EM-040` | validator MUST run before submitting, at the ingest RPC | no ingest RPC exists; validation moved to daemon-side at dispatch (`LoadDotWorkflowWithParams`) |
| `AR-018` | reconciliation is a DOT+YAML workflow-library entry introducing no shared types | reconciliation is hard-coded Go with shared `internal/core` types; no reconciliation `.dot` or `.yaml` exists |
| `AR-044` | the daemon MUST NOT read state from agent conversation transcripts | `internal/daemon/bandwidthtuner.go` reads `~/.claude/projects/*/*.jsonl` and auto-scales `--max-concurrent` from it; `CaptureLastPane` scrapes panes to gate launch |
| `ON-004c` | budget resets at **local**-midnight; unified USD meter | `spendMeterTodayKey()` uses `time.Now().UTC()`; the cap is a `bytesPerUSD = 100_000` byte proxy over daemon-side accrual only |

Note `ON-004c` specifically: NEXT_STEPS.md offers it as the *textbook category-A* example ("a missing
link, cheap to fix"). **It is category C.** The feature exists —
`envFlywheelBudgetUSDPerDay`, `defaultDailyBudgetUSD = 20.0` — and two pinned values are wrong. This
is the audit's own warning landing on the audit's own example, and it is the single best argument in
this report for why the polarity check cannot be skipped.

**B — aspirational (~37 IDs).** Never built, nothing deleted. `CP-059` + `HC-048b` (the egress
whitelist — zero Go hits across two specs, and because the policy YAML decoder is **non-strict**, an
author who follows the spec template gets a silently ignored key rather than an error).
`QM-006` (`.release-v1.json` retention anchor — zero hits repo-wide). `EV-031` (four-axis tags in the
event registry — `mustRegister` stores a name and a constructor and nothing else; 178 registrations,
zero tag data). `AIS-018` (keeper's `outcome_emitted` gate — the code still uses exactly the `.idle`
mtime the requirement says to replace). `ON-023` (compile-time `Secret`-type lint — no `type Secret`
anywhere; redaction is runtime-only via `core.RedactionRegistry`).

**D — not a clean requirement (~34 IDs).** Three distinct sub-kinds, all worth deleting rather than
triaging:
- **Extraction artifacts.** `CI-2`, `CI-6`, `CI-8` are not credential-isolation requirements at all —
  `specs/credential-isolation.md` declares only `CI-001..CI-007`. They are cross-spec *review-finding*
  numbers ("Contradiction/Issue N") appearing in prose and revision-history rows. Same for `SH-4`
  (a changelog row) and `EM-060` (a bookkeeping handle for an in-place amendment to EM-007).
- **Retired-but-retained.** `WM-017` ("retired in v0.3.0 … Do NOT reuse the ID"), `AIS-008`
  ("RETIRED per COORD c019 … the gate is moot"). These are history markers occupying requirement
  slots.
- **Document-constraining, not code-constraining.** `AR-013`, `AR-015`, `AR-048`, `AR-052`, `AR-053`
  all govern spec markdown ("every spec MUST declare `spec-category` in its front matter"). No Go
  code can implement or cite them. **AR-052's claim that its front-matter rule is "lint-enforced" is
  false** — no lint enforces it anywhere.

### Two corrections to leads the readers were given

- **depguard does not enforce AR-010/011/016.** `.golangci.yml` contains **zero** `AR-*` references;
  its rules cite BI-002, PL-INV-002, and RSM-028. Those three AR requirements are category A on their
  own merits, but the "already machine-enforced" claim in NEXT_STEPS.md is wrong.
- **`internal/specaudit` was not deleted.** Commit `e99a52fff` removed 129 prose sensors; the package
  survives with 4 files. It never contained a `spec-category`/envelope sensor, so AR-013 and AR-052
  lost nothing they had.

### Orphan rate screens; it does not decide

The strongest counterexample in the data: **`claude-launchspec.md` has the worst orphan rate in the
repo (100%, 14 of 14) and is one of the healthiest specs by content.** Four of five sampled IDs are
category A — the LaunchSpec subsystem is real, correct, and simply uncited. Meanwhile
`control-points.md` sits at 1.6% orphan and its one orphan, CP-059, is a phantom egress sandbox.
**Use orphan rate to choose reading order, never as a verdict.**

---

## 4. The regression-risk requirements, verified one by one

Each was re-derived from the spec text and the current code, deliberately adversarially. **Seven of
ten confirm, one is refuted, two are partial.** Three rows in NEXT_STEPS.md need correcting.

### CONFIRMED — obeying the spec regresses working behavior

**AR-017** (`specs/architecture.md` §4.5) — *the highest-leverage row.*
> "The only out-of-process actors admitted in MVH are: (a) agent handler subprocesses …, (b)
> orchestrator-agent sessions …, (c) `br` CLI invocations the daemon spawns … Introducing a fourth
> out-of-process actor class MUST proceed via the amendment protocol of §4.6; ad-hoc addition is
> forbidden."

At least **five** unlisted out-of-process actor classes are load-bearing today: the **tmux server**
(`internal/lifecycle/tmux/subcommand.go`, `internal/lifecycle/orphansweep.go`,
`cmd/harmonik/supervise/attach.go`); the **supervisor** (`cmd/harmonik/supervise/`, its own detached
tmux session); the **per-agent keeper watcher** (`internal/agentlaunch/keeperargv.go`,
`SpawnKeeperWindow`, one process per watched agent holding an exclusive flock); **`git`** — 96
`exec.CommandContext(ctx, "git", …)` sites, and git is completion authority per HN-009; and **`gh`**
(`cmd/harmonik/promote_cmd.go`, `release_cmd.go`). A rewrite treating this list as closed produces a
process model that cannot host the system that is running. **Not a missing feature — a wrong model.**

**AR-038 / AR-INV-007** (`specs/architecture.md` §4.9, §10.2) — *confirmed, and the audit's own caveat is refuted.*
> "A design proposal that introduces file-based agent-to-agent handoff … MUST be rejected or
> refactored." AR-INV-007: "Agent-to-agent coordination via files … is forbidden."

NEXT_STEPS.md hedges that the contradiction is with a skill, not with Go, because
"every occurrence in `cmd/harmonik/crew.go` is a comment." **That checked the wrong package.** The
handoff file is read and parsed by Go: `cmd/harmonik/crew.go` `resolveCrewStartArgs` parses a real
`--mission <path>` flag; `internal/crewrun/missionfrontmatter.go` `readMissionFrontMatter` does
`os.ReadFile` + `yaml.Unmarshal` and exports `ReadMissionModel` / `ReadMissionHarness`;
`internal/daemon/crewstart.go` consumes both and delivers the mission via `pasteCrewMission`, whose
payload is the handoff **path**. And `specs/crew-handoff-schema.md` normatively describes the same
flow. Obeying AR-038 means deleting `--mission`, `readMissionFrontMatter`, `pasteCrewMission`, and a
whole spec file. (`harmonik comms` is *not* a violation — it is daemon-mediated over a unix socket.)

**HN-003** (`specs/harness-contract.md` §4.2)
> "`LaunchSpec(rc)` MUST return the binary, argv, env, and cwd for exactly ONE spawn, as a pure
> value — it MUST NOT perform shared-scaffolding side effects (worktree-trust seeding,
> `agent-task.md` write, settings materialization, pre-exec messages)."

`internal/harness/claude/launchspec.go` `BuildLaunchSpec` performs **all four**, in a sequenced order
its own comments mark load-bearing ("MUST be after MaterializeClaudeSettings and BEFORE
SubstrateSpawn"): `MaterializeClaudeSettingsVia`, `EnsureWorktreeTrustVia`, `WriteAgentTaskVia`, and
`preExecMsgs`. The codex path does the same via `buildCodexRoutedLaunchSpec`. §4.8 HN-020 restates
the obligation, so it is not a slip. Enforcing it breaks every claude and codex run.

**HN-012** (`specs/harness-contract.md` §4.4) — *textbook polarity inversion.*
> "An unknown harness string at ANY tier MUST cause a resolve-time error (fail closed) … The
> resolver MUST NOT fall back to claude on an unknown or conflicting selector."

`internal/daemon/harnessresolve.go` `resolveHarness` has **no error return at all**. On an invalid or
duplicated tier-1 value it emits `bead_label_conflict` ("tier-1 harness input treated as absent …
precedence walk continues to tier 2") and falls through, ending
`return core.AgentTypeClaudeCode`. `resolveHarnessAgentTypeQuiet` replicates the fail-open walk. No
exported `ResolveHarness` exists. The design is deliberately fail-**open** with an observability
event. Implementing HN-012 flips every malformed `harness:` label from "runs on claude with a
warning" to "refuses to dispatch."

**CL-100** (`specs/cognition-loop.md` §4.12)
> "Substrate-extension entry point (`./.pi/extensions/flywheel/index.ts` for Pi; `cmd/flywheel/main.go`
> if Go-native) is the cognition-loop composition root. **Only this root may wire** the harness,
> substrate, watermark store, digest fetcher, wake-filter table, budget tracker …"

Neither path exists — `.pi/` is gone entirely, and there is no `cmd/flywheel/`. The budget tracker is
wired inside the daemon (`internal/daemon/spendmeter_hkk3f8g.go` `NewDaemonSpendMeter(bus)`, wired at
`internal/daemon/bootstate.go`), credentials at `cmd/harmonik/supervise/resolveapikey.go`. The word
**"Only"** is what makes it a trap: it is an exclusivity clause naming a deleted directory, so the
working in-daemon spend meter is a violation by construction — and `specs/flywheel-motion.md` §0.2
forbids rebuilding the root that would make it legal.

**ON-020g** (`specs/operator-nfr.md` §4.6) — *confirmed and sharper than stated.*
> "The daemon MUST support `harmonik upgrade --rollback` as a first-class command … the daemon MUST
> exec-replace back to `.harmonik/daemon.binary.prev` using the same fd-passing mechanism of ON-020f
> … absence of the file MUST fail rollback with §8 code 16."

No `upgrade` subcommand exists (`cmd/harmonik/usage.go` lists 30; not among them).
`HARMONIK_LISTENER_FD` and `.harmonik/daemon.binary.prev` appear in zero `.go` files. The only trace
is `internal/lifecycle/daemonpaths.go` `upgradingRelPath`, dead code with no non-test callers.
**But a rollback path does ship, under a different model:** `harmonik release rollback`
(`cmd/harmonik/release_cmd.go` `runReleaseRollback`, `internal/release/lastgood.go`, state at
`.harmonik/state/last-good-binary`) plus the supervisor-revive runbook in `docs/daemon-redeploy.md`.
That is copy-last-good + supervisor restart, **not** exec-replace with fd-passing. So ON-020g is not
merely a phantom — it is a phantom that **contradicts a shipped mechanism**, and a rewrite obeying it
builds a second incompatible rollback surface.

**ON-018** (`specs/operator-nfr.md` §4.5)
> "Every versioned on-disk or wire artifact … queue execution plan (persisted as `.harmonik/queue.json`
> with a `schema_version` field) … MUST maintain N-1 readability. A reader pinned to version N-1 MUST
> successfully parse and interpret artifacts written by version N."

`internal/queue/types.go` `UnmarshalQueue` does exact-match rejection —
`if q.SchemaVersion != schemaVersion { return Queue{}, fmt.Errorf("%w: got %d, want %d", ErrSchemaVersion, …) }`
with `const schemaVersion = 1`. This is **deliberate and specified elsewhere**: the doc comment cites
QM-002, and `specs/queue-model.md` says "MUST equal 1; forward-incompatible value refuses per
QM-002." The same fail-closed rule repeats in `QueueSubmitRequest`, `internal/queue/transaction.go`,
`internal/queue/cli/helpers.go`. **This is a spec-vs-spec contradiction** — ON-018 (foundation)
mandates forward tolerance, QM-002 (subsystem) mandates refusal, and the code implements QM-002. A
rewrite cannot satisfy both. Obeying ON-018 weakens a working, intentional guard.

### REFUTED

**CP-059** (`specs/control-points.md` §4.11) — **does not belong in a "would break working code" table.**
> "The resolved `egress_whitelist[]` value … MUST be propagated into `LaunchSpec.egress_whitelist` at
> claim time per [handler-contract.md §4.11.HC-048b]" — and, in the same requirement,
> "enforcement at that level is owned by the sandbox subsystem (S06) and is **deferred post-MVH**."

`egress_whitelist` has zero `.go` hits and zero hits in any policy YAML.
`internal/core/policydocument.go` `PolicyPermissionSchema` has exactly seven fields, none of them
egress. HC-048b is equally unimplemented. **There is no working behavior to regress** — nothing today
allows or denies egress, and an opt-in field defaulting to "absent = unrestricted" is byte-compatible
with today. CP-059 is category B (aspirational), and the requirement *says so itself*. The real cost
is scope inflation: a rewrite reading the audit's table would budget for an egress sandbox nobody
built. Move it to the phantom-subsystem list with ON-020g.

### PARTIAL

**ON-021** (`specs/operator-nfr.md` §4.6) — inert on its own.
> "An `upgrade` operation MUST NOT make any in-flight run unrecoverable … The cross-version state
> contract of §4.6.ON-020 MUST reject upgrades that would violate this invariant."

A conditional invariant over a subsystem that does not exist. The drain machinery it references *is*
real (`cmd/harmonik/supervise/pause.go`), but `--schema-version-query` has zero Go hits. It
constrains nothing, mandates nothing beyond ON-020, and cannot regress anything. Delete it *with* the
ON-020 block, but listing it beside ON-020g overstates it.

**ON-015** (`specs/operator-nfr.md` §4.4) — an orphan, not a trap.
> "Queue-format compatibility MUST be the union of (a) Beads schema compat … AND (b) harmonik's
> overlay schema compat … Both halves MUST be N-1 readable."

Half (a) has no implementation — no Beads schema-version check exists anywhere;
`queue-format-unsupported` appears only as comment text in
`internal/core/daemonevents_hqwn59.go`. Half (b): the `Harmonik-Bead-ID` trailer is real but
**write-only** (`cmd/harmonik/promote_cmd.go` stamps it on cherry-picks); no reader parses it with
version tolerance. Nothing versions the trailer, event bead-ID refs, or session-log metadata, so
"N-1 readable" is not a property anything could fail. Unfalsifiable over unimplemented. **The
substance of that audit row is ON-018's, not ON-015's.**

### A correction on the live bug

NEXT_STEPS.md files the `handler-state.json` defect (`hk-rr1dy`) under the ON-018 row. **Confirmed as
a real bug, but it is HP-016, not ON-018** — ON-018 does not cover `handler-state.json`.
`internal/daemon/handlerpause_persist_m0k0a.go` declares `const handlerStateSchemaVersionDaemon = 2`
with the in-line comment "Matches handlerStateSchemaVersion in cmd/harmonik/handler.go" — **that
comment is false**: `cmd/harmonik/handler.go` declares `const handlerStateSchemaVersion = 1` and
rejects with `if state.SchemaVersion > handlerStateSchemaVersion`. The daemon writes 2; the CLI
refuses it and tells the operator to upgrade a current binary. Verified in both files.

---

## 5. `cognition-loop.md` and the `operator-nfr.md` phantom half

### 5a. `cognition-loop.md` maps a deleted subsystem — CONFIRMED

`git show --stat 353fc3c1e` → `353fc3c1e88f366ede8d91e890481f4113eb045b`, **Thu 2 Jul 2026**,
subject *"chore: remove dead flywheel pi extension (auto-loaded on pi start, fork-bombs kerf next;
flywheel retired, replaced by captain)"*. **21 files changed, 9,038 deletions, 0 insertions** — all
under `.pi/extensions/flywheel/`: `index.ts` (721), `bridge.ts` (591), `dispatcher.ts` (458),
`tui-panel.ts` (284), `budget.ts`, `circuit-breaker.ts`, `router.ts`, `wake-filter.ts`,
`watchdog.ts`, `watermark.ts`, `debounce.ts`, 7 vitest files, `package-lock.json` (4,603).

`git log --diff-filter=D -- '.pi/extensions/flywheel/*'` returns **exactly that one commit**;
`git log --diff-filter=A 353fc3c1e..HEAD -- '.pi/*'` is **empty**. `.pi/` does not exist today. The
commit body states "Flywheel is retired (locked don't-revive)."

The spec was never updated: front matter still reads `owner: flywheel-author`,
`last-updated: 2026-05-31`, and CL-100 still names `./.pi/extensions/flywheel/index.ts` as the
composition root — a path deleted 32 days later.

`specs/flywheel-motion.md` §0.2 does explicitly forbid the rebuild, verbatim:
> **0.2 GRAFT posture [blocker A — locked]** — "The system MUST be built as an **incremental graft
> onto the live captain+daemon**, reusing shipped primitives. It MUST NOT rebuild the Architecture-B
> cognition-loop that *replaces* the interactive captain."

**Coverage:** 13 of 50 CL IDs appear anywhere in `.go` (26%). Live: CL-030/031/032/033 (`harmonik
digest` is a real command — `internal/digest/builder.go`, `types.go`, dispatched from
`cmd/harmonik/main.go`), CL-040 (`internal/digest/notes.go` `readOpenNotes` — read side only, no
writer), CL-082 (`cmd/harmonik/digest/watch.go`), CL-090/090a (`internal/daemon/spendmeter_hkk3f8g.go`,
wired in `bootstate.go`). Dead: CL-002 (`loop.lock`), CL-011 (the 70/90/100 fullness ladder),
CL-020/021 (fresh-context recycle, cacheable prefix), CL-052/053/054 (watermark + reacted_ledger),
CL-061/062/063 (wake filter, debounce, urgent), CL-071/072 (loop-side eager refill — the daemon twin
lives as EM-063 in `internal/daemon/eagerfill_em063.go`).

**Two corrections to the audit's salvage list:**
- **CL-051 is not free to salvage.** `internal/cognition/` (`twophasedone.go`, `gitdone_ev041.go`,
  `missingheartbeat_ev040.go`) has **zero non-test importers** in the whole module — only its own
  three `_test.go` files import it. This is exactly the `internal/operatornfr` unwired-theater
  pattern. Salvaging CL-051 means wiring it or deleting it, not keeping spec text.
- **CL-083 is refuted as written.** Its obligation is "*Loop* writes `.harmonik/cognition/heartbeat`
  at every outer-loop wake." **No Go code writes that file.** The two Go references are comments in
  `internal/supervise/daemon_watchdog.go` and `cmd/harmonik/supervise/shim.go` — the first refers to
  `bridge.ts`, which is deleted. What is live is only CL-083's final clause, the supervisor-owned
  daemon revival watchdog. `.harmonik/cognition/` does still exist, but for entirely different files
  (`captain.sentinel`, `captain.pid`, `config.json`, `loop-status.json`) written by
  `cmd/harmonik/captain.go` and `supervise/config.go`. The CL-003a surface (`loop.lock`, `state.json`,
  `notes.jsonl`, `heartbeat`, `dispatch-log.jsonl`) is written by nothing.

Confirmed salvageable: **CL-030, CL-032, CL-033, CL-090, CL-090a** (all with live consumers), plus
CL-031/CL-040 as behavior-exists-citation-missing.

### 5b. `operator-nfr.md` has a phantom half — CONFIRMED

`git show --stat c14ad11d2` → `c14ad11d286963224437767e4b07b6fa39c46507`, **Fri 17 Jul 2026**,
`refactor(operatornfr): delete unwired API + theater tests (A2)`. **48 files changed, 1 insertion,
14,682 deletions** — 45 of them `internal/operatornfr/*` (8 production `.go`, 37 tests) plus 3
dangling-reference fixups. Commit body: *"had a production API that NO product code imported."*
`internal/operatornfr/` does not exist today.

**The bisection.** Of 89 ON-* IDs, 41 appear anywhere in `.go`, only **34 in non-test `.go`**.
Test-only (no production anchor): ON-013c, ON-031, ON-050, ON-051, ON-056, ON-057, ON-058.

**Phantom sections — ~24 IDs, of which 18 have literally zero `.go` occurrences:**

| Section | IDs | Evidence |
|---|---|---|
| **§4.6 `harmonik upgrade` contract** (lines 445–517) | 10 — ON-020, ON-020a–h, ON-021 | Only ON-020a has code (`internal/lifecycle/daemonpaths.go` `upgradingRelPath` — and that is the daemon's own marker, with no non-test callers). ON-020, 020b–h, 021: **0 `.go` occurrences each.** No `upgrade` subcommand. The only `case "rollback":` in `cmd/harmonik/` is `release_cmd.go`, a different subsystem. The lone artifact is `internal/lifecycle/upgradeexec_pl027_test.go`, a test whose fixture models the upgrade protocol inside the test file — self-referential theater of exactly the kind c14ad11d2 deleted. |
| **§4.1 exit-code taxonomy** (135–257) | ~11 | ON-002, ON-003, ON-004e (`FLYWHEEL_MODEL_TIER1/2/3` — 0 hits anywhere), ON-004g have 0 production refs. ON-001/004/004c/004d survive via other homes. |
| **§4.2 integrity gate** (258–278) | 3 | ON-005 and ON-006: **0 `.go` occurrences.** Only ON-005a is live (`internal/core/failuremode.go`). |

**Real sections — ~65 IDs with genuine production anchors:** §4.13 ON-059 captain-initiated keeper
restart-now is the strongest (`internal/keeper/restartnow.go`, `cmd/harmonik/keeper_cmd.go`,
`watcher.go`, `injector.go`, `internal/core/keeperevents.go`, 5 dedicated test files); §4.3
operator-control (15 IDs, ON-007/008/008a/009/009a/010/011/013/013a/014 all in production);
§4.9 observability (ON-035 → `internal/structuredlog/`, ON-055 → `cmd/harmonik/subscribe.go` +
`internal/daemon/socket.go`); §4.11 resource budgets (ON-047 →
`internal/core/budgetcategorydefaults_on047.go`, ON-048 → `budgetexhaustion_on048.go`);
§4.5 ON-018/019 → `internal/core/policydocument.go`.

**The phantom is ~27% of the file, and it is contiguous** — §4.6 in particular is a clean 73-line
excision (lines 445–517).

---

## 6. Disposition, per spec file

All 44 `.md` files under `specs/`. **KEEP-NORMATIVE** = usable as a rewrite oracle today.
**TRIAGE** = real subsystem, but read it with the polarity check before believing any requirement.
**DELETE** = remove or gut before the rewrite reads it.

### KEEP-NORMATIVE — 12 files, 660 IDs

| Spec | orphan% | One-line reason |
|---|---:|---|
| `reconciliation/spec.md` | 0.0% | Zero orphans in 43 IDs — the only spec in the repo with perfect traceability. |
| `control-points.md` | 1.6% | 57 of 63 in production; the single orphan (CP-059) is a self-declared deferral, safe to carve out. |
| `workspace-model.md` | 1.7% | WM-026 atomic-write is cited across the tree; the one orphan (WM-017) is an explicitly retired ID. |
| `claude-hook-bridge.md` | 3.4% | 28 of 29 in production; CHB-026's per-connection FIFO matches `socket.go` `Serve` exactly. |
| `run-state-machine.md` | 5.7% | Both orphans (RSM-026, RSM-028) verified category A — RSM-028 is depguard-enforced. |
| `beads-integration.md` | 5.7% | Written with `internal/brcli`; carve out BI-014b, whose provenance-marker half never shipped. |
| `handler-contract.md` | 7.4% | 81 of 94 in production; carve out the HC-048b/HC-050 skill-and-egress provisioning block (never built). |
| `event-model.md` | 7.7% | Updated 2026-07-27 alongside the code; EV-045/047 verified verbatim against `internal/presence` and `internal/eventbus`. |
| `scenario-harness.md` | 8.1% | SH-010/SH-011 verified exact; its one D is a changelog-row artifact, not a requirement. |
| `queue-model.md` | 9.3% | QM-007/QM-056 match field-for-field; note QM-002 **conflicts with operator-nfr ON-018** — QM-002 is the one the code implements. |
| `execution-model.md` | 9.7% | 78 of 93 in production; carve out EM-040, whose ingest-time validation inverted to dispatch-time. |
| `process-lifecycle.md` | 11.7% | Updated with the code; carve out PL-006f, contradicted by two live argv-matching reapers. |

### TRIAGE — 12 files

| Spec | orphan% | One-line reason |
|---|---:|---|
| `claude-launchspec.md` | 100% | **Worst rate, near-best content** — 4 of 5 sampled are pure missing citations; add `// Spec:` comments and fix CLS-040, which now does the inverse of its "iff" clause. |
| `digest-command.md` | 63.6% | Orphanhood is 100% missing citations — DC-003/006/009 all verified against `internal/digest`; a tagging pass, nothing more. |
| `session-keeper.md` | 38.1% | The best-written oracle in the repo — SK-002/003/005 map 1:1 to `internal/keeper/ports.go`; only drift is `SetHold(sessionID)` vs the code's `SetHold()`. |
| `system-state.md` | 35.3% | `RollUpLabel` is a literal transcription of SS-004/SS-005; tag it and note `harmonik state`, not `harmonik status`, is the real command. |
| `workflow-graph.md` | 18.2% | Sound on graph semantics (WG-051/054 exact), but **WG-036's "the engine's example-loader looks there" is false** and WG-038's project-local deferral is dead — amend both. |
| `replay-substrate.md` | 18.2% | RS-003/011/014 verified exact and depguard-enforced; low orphan count, cheap to close out. |
| `sub-workflow-dispatch.md` | 20.0% | SW-009 is structurally guaranteed; SW-008's registered-key discipline is dead code with zero production callers — decide whether to wire or delete. |
| `agent-input.md` | 22.7% | AIS-005 verified; contains a retired-ID marker (AIS-008) and one aspirational item (AIS-018) to strip. |
| `pi-harness.md` | 25.7% | **The live harness contract** (25 unique IDs across 49 Go files) — but PI-002's seam prohibition is already violated and PI-082/085 are unbuilt pilot/spike text to delete. |
| `credential-isolation.md` | 42.9% | Only 14 IDs and the rate is inflated by extraction artifacts (CI-2/6/8 are review-finding numbers, not requirements) — fix the extraction, then re-measure. |
| `handler-pause.md` | 71.4% | **The spec is behind the code**: it lists per-account pause, auto-resume, and external-trigger resume as out of scope; all three ship. Rewrite §1.2 and HP-002/016, which pin `schema_version: 1` against a daemon that writes 2. |
| `operator-nfr.md` | 54.5% | **SPLIT** — §4.3/§4.9/§4.11/§4.13 are real and well-anchored (ON-059 keeper restart is the strongest requirement in the file); §4.6/§4.1/§4.2 are phantom (see DELETE-IN-PART below). |

### DELETE, or delete in part — 4 files

| Spec | orphan% | One-line reason |
|---|---:|---|
| `cognition-loop.md` | 74.0% | **Delete after salvage.** Maps a subsystem removed in `353fc3c1e` (2026-07-02, 9,038 lines); `flywheel-motion.md` §0.2 forbids rebuilding it. Salvage CL-030/032/033/090/090a (live in `internal/digest` + `internal/daemon/spendmeter_hkk3f8g.go`) and CL-003/041/060/071/080 into their real owners. **Do not salvage CL-051** (its package has zero non-test importers) **or CL-083** (nothing writes the heartbeat file). |
| `harness-contract.md` | 95.8% | **Delete as superseded.** `pi-harness.md` won in practice: `PI-*` appears in 49 Go files / 25 IDs, `HN-*` in 5 files / 1 ID. Six of eight sampled HN IDs are stale inversions. Salvage HN-007 and HN-024 (both verified A) into `pi-harness.md`; delete the rest. |
| `operator-nfr.md` §4.6 | — | **Delete the 73-line `harmonik upgrade` block (lines 445–517, ON-020/020a–h/021).** No `upgrade` subcommand ever existed (`git log -S` on `cmd/harmonik` is empty); the shipped path is `harmonik release rollback` + supervisor last-good, which ON-020g actively contradicts. Also delete ON-005/ON-006 (§4.2) and ON-002/003/004e/004g (§4.1) — zero Go hits each. |
| `architecture.md` | 74.1% | **SPLIT: triage §4.1–4.5, delete §4.0/§4.6/§4.10.** AR-010/011/016/036/039/045 are real. AR-013/015/048/052/053 constrain *documents*, not code, and cannot be traced by construction. AR-017 and AR-044 are load-bearing errors about the running system and must be rewritten before any rewrite reads them. |

### The eight files with no requirement IDs — decide what they are

None can be traced; all are currently labelled normative by `AGENT_INDEX.md`.

| File | Lines | Disposition |
|---|---:|---|
| `flywheel-motion.md` | 450 | **KEEP, reclassify.** `status: normative` and load-bearing (§0.2 is the standing prohibition on rebuilding the cognition loop) — but it is a *decision record*, not a requirements spec. Label it so, and release the unused `FW` prefix. |
| `crew-handoff-schema.md` | 263 | **KEEP, reclassify** — a schema doc that `internal/crewrun/missionfrontmatter.go` really implements. Give it IDs or move it to `docs/`. |
| `assessor-handoff-schema.md` | 338 | **TRIAGE** — same shape; verify it still matches the shipped assessor path. |
| `promote.md` | 162 | **TRIAGE** — `harmonik promote` ships in both push- and PR-mode; the spec has no IDs to check it against. Release the unused `PR` prefix or assign IDs. |
| `release-pipeline.md` | 363 | **TRIAGE** — `harmonik release {certify,yank,rollback}` ships; same problem. Release the unused `RP` prefix or assign IDs. **This file, not `operator-nfr.md` §4.6, is where the real upgrade/rollback contract belongs.** |
| `park-resume-protocol.md` | 408 | **TRIAGE** — verify against the live park/resume path before trusting it. |
| `pi-provider-switch.md` | 655 | **TRIAGE** — `status: supplement`; it borrows `PI-*`/`HC-*` IDs rather than declaring its own. Fold into `pi-harness.md` or give it a prefix. |
| `hitl-decisions.md` | 252 | **TRIAGE** — `internal/presence/decisions.go` implements the decision fold (EV-045); reconcile and release the unused `HD` prefix. |

### Not specs at all — move out of `specs/`

| Path | What it is |
|---|---|
| `specs/s01/reconciliation/prompts/cat-{2,3,6a}-investigator.md` | **LLM prompt templates** carrying spec front matter. Belongs in `docs/` or beside the code that uses them. |
| `specs/s01/reconciliation/README.md` | An index for the above. |
| `specs/examples/authoring-notes.md`, `README.md` | Authoring guidance for the `.dot` fixtures, not normative text. |
| `specs/examples/*.dot` (28 files) | Test fixtures presented as normative artifacts — 24 are cited by no spec. Covered separately in NEXT_STEPS.md §4; note WG-036 makes the *path* normative, so the move needs a spec amendment first. |

### Summary of dispositions

| Verdict | Files | IDs |
|---|---:|---:|
| KEEP-NORMATIVE | 12 | 660 |
| TRIAGE | 12 + 7 no-ID files | 393 |
| DELETE / DELETE-IN-PART | 4 (2 whole, 2 partial) | 111 |
| Move out of `specs/` | 6 `.md` + 28 `.dot` | 0 |

---

## 7. Where this report disagrees with `NEXT_STEPS.md` §1

The prior audit is directionally right and its headline numbers reproduce. Six corrections:

1. **Orphan rates for the suspect specs are worse than reported.** NEXT_STEPS.md attributes an ID to
   the file it *appears in*; this report attributes it to the spec that **owns its prefix**
   (`_registry.yaml`). Foreign-prefix IDs quoted inside a suspect spec are usually well-cited, so the
   old method diluted the rate. Corrected: `cognition-loop` 49% → **74%**, `handler-pause` 50% →
   **71%**, `architecture` 61% → **74%**, `digest-command` 41% → **64%**, `operator-nfr` 35% →
   **55%**, `harness-contract` 85% → **96%**, `claude-launchspec` 36% → **100%**.
2. **`claude-launchspec.md` is the worst file in the repo, and the audit missed it.** Listed at 36%;
   it is **100%** — zero of its 14 CLS IDs appear in any `.go` file, production or test. It was not
   on the suspect list at all.
3. **CP-059 does not belong in the "would break a rewrite" table.** It is aspirational, and the
   requirement *says so itself* ("deferred post-MVH"). There is no working behavior to regress. Its
   cost is scope inflation, which is the ON-020g failure mode, not the HN-003 one.
4. **AR-038's evidence is stronger than the audit believed, not weaker.** The audit hedges that the
   contradiction is with a skill rather than with Go. It is with Go — the audit checked
   `cmd/harmonik/crew.go` and missed `internal/crewrun/missionfrontmatter.go` and
   `internal/daemon/crewstart.go`, which read, parse, and act on the handoff file.
5. **The `handler-state.json` schema bug is HP-016, not ON-018.** Real bug, confirmed in both files
   (`handlerStateSchemaVersionDaemon = 2` vs `handlerStateSchemaVersion = 1`, with a false
   "Matches …" comment). But ON-018 does not cover `handler-state.json`; filing it under that row
   blurs two separate defects.
6. **Two of the eight cognition-loop salvage IDs do not survive.** CL-051's package
   (`internal/cognition/`) has zero non-test importers — it is the same unwired-theater pattern that
   `c14ad11d2` deleted. CL-083 is refuted: nothing writes `.harmonik/cognition/heartbeat`.

**One thing the audit called that this report confirms exactly:** the polarity check is the whole
exercise. Every CONFIRMED row in §4 is a case where the named symbol exists and the code does the
opposite. A script cannot find them.

---


---

## 8. Recommendations

1. **Fix `AGENT_INDEX.md` first.** Its `### Specs (normative)` heading is a single line and it is the
   proximate cause of the whole problem: it makes a 100%-orphan file and a 0%-orphan file
   indistinguishable to any agent that reads it. Replace with three lists matching §6. **This is the
   cheapest change in this report and it blocks the rewrite.**
2. **Ship `trace.py` as a generated report, never a test.** It runs in ~10 seconds with no build. Wire
   it as a non-blocking CI artifact in `.github/workflows/ci.yml`, publishing the per-spec table plus
   a diff against the previous run. **Do not make it a merge gate on the orphan count** — that
   incentivizes citation comments, which §2c shows are already 9-in-10 decorative. Gate on the one
   thing that is unambiguous: **a `// Spec:` citation naming an ID that no spec declares** (37 such
   dangling references exist today).
3. **Delete before you read.** In priority order: `operator-nfr.md` §4.6 (73 contiguous lines, zero
   Go, contradicts a shipped mechanism); `cognition-loop.md` after salvaging 10 IDs;
   `harness-contract.md` after salvaging HN-007/HN-024; `architecture.md` §4.0/§4.6/§4.10.
4. **Rewrite AR-017 before designing any process model.** It is the only requirement here that would
   cause a rewrite to produce a system that cannot host tmux, the supervisor, the keeper, or `git`.
5. **Resolve ON-018 vs QM-002 explicitly.** A foundation spec and a subsystem spec give opposite
   orders on schema compatibility. The code follows QM-002. Amend ON-018 to match, or the rewrite
   inherits an unsatisfiable pair.
6. **Do not spend the tagging budget first.** ~140 IDs need only a `// Spec:` comment, and that work
   is visible and satisfying. It is also worth almost nothing: it does not make the specs more true.
   **The ~81 stale requirements are what will damage a rewrite, and every one of them requires a
   reader.** Budget accordingly.

---

## Appendix A — the 293 uncited requirement IDs, by owning spec

**`operator-nfr.md`** (48) — ON-002 ON-003 ON-004a ON-004b ON-004c ON-004d ON-004e ON-004f ON-004g ON-005 ON-006 ON-012 ON-013b ON-013d ON-015 ON-017 ON-020 ON-020b ON-020c ON-020d ON-020e ON-020f ON-020g ON-020h ON-021 ON-023 ON-024 ON-025 ON-026 ON-027a ON-028 ON-030 ON-032 ON-034 ON-035a ON-036 ON-038 ON-039 ON-041a ON-041b ON-041c ON-042 ON-043 ON-044 ON-045 ON-046 ON-049 ON-053

**`architecture.md`** (40) — AR-008 AR-009 AR-010 AR-011 AR-013 AR-014 AR-015 AR-016 AR-017 AR-018 AR-019 AR-021 AR-022 AR-023 AR-026 AR-028 AR-029 AR-030 AR-031 AR-034 AR-035 AR-036 AR-037 AR-038 AR-039 AR-040 AR-041 AR-042 AR-043 AR-044 AR-045 AR-046 AR-047 AR-048 AR-049 AR-050 AR-051 AR-052 AR-053 AR-054

**`cognition-loop.md`** (37) — CL-001 CL-002 CL-003 CL-003a CL-010 CL-011 CL-012 CL-014 CL-020 CL-021 CL-022 CL-023 CL-031 CL-041 CL-042 CL-050 CL-052 CL-053 CL-054 CL-055 CL-056 CL-060 CL-061 CL-062 CL-063 CL-064 CL-070 CL-071 CL-072 CL-073 CL-080 CL-081 CL-090b CL-090c CL-090d CL-092 CL-100

**`handler-pause.md`** (30) — HP-001 HP-002 HP-003 HP-004 HP-005 HP-006 HP-009 HP-009a HP-013 HP-014 HP-015 HP-016 HP-020 HP-021 HP-022 HP-023 HP-024 HP-041 HP-042 HP-043 HP-051 HP-052 HP-053 HP-054 HP-060 HP-061 HP-065 HP-070 HP-071 HP-073

**`harness-contract.md`** (23) — HN-001 HN-002 HN-003 HN-004 HN-005 HN-006 HN-007 HN-008 HN-009 HN-010 HN-011 HN-012 HN-013 HN-014 HN-015 HN-016 HN-017 HN-018 HN-019 HN-020 HN-021 HN-023 HN-024

**`claude-launchspec.md`** (14) — CLS-001 CLS-002 CLS-002a CLS-003 CLS-004 CLS-010 CLS-011 CLS-020 CLS-021 CLS-022 CLS-023 CLS-030 CLS-031 CLS-040

**`workflow-graph.md`** (10) — WG-004 WG-020 WG-036 WG-037 WG-038 WG-048 WG-049 WG-050 WG-051 WG-054

**`execution-model.md`** (9) — EM-006 EM-008 EM-039 EM-040 EM-055 EM-059 EM-060 EM-061 EM-064

**`pi-harness.md`** (9) — PI-001 PI-002 PI-061 PI-080 PI-081 PI-082 PI-085 PI-090 PI-101

**`session-keeper.md`** (8) — SK-002 SK-003 SK-004 SK-005 SK-006 SK-007 SK-019 SK-021

**`digest-command.md`** (7) — DC-003 DC-004 DC-006 DC-008 DC-009 DC-010 DC-011

**`handler-contract.md`** (7) — HC-014 HC-016a HC-026b HC-048b HC-050 HC-052 HC-060

**`process-lifecycle.md`** (7) — PL-006b PL-006c PL-006e PL-006f PL-006g PL-008 PL-030

**`credential-isolation.md`** (6) — CI-005a CI-1 CI-2 CI-5 CI-6 CI-8

**`system-state.md`** (6) — SS-002 SS-004 SS-005 SS-008 SS-008a SS-009

**`agent-input.md`** (5) — AIS-000 AIS-005 AIS-007 AIS-008 AIS-018

**`event-model.md`** (5) — EV-005 EV-031 EV-045 EV-047 EV-6

**`queue-model.md`** (5) — QM-006 QM-007 QM-013 QM-056 QM-061

**`replay-substrate.md`** (4) — RS-003 RS-011 RS-014 RS-022

**`beads-integration.md`** (3) — BI-013b BI-014b BI-028

**`scenario-harness.md`** (3) — SH-010 SH-011 SH-4

**`run-state-machine.md`** (2) — RSM-026 RSM-028

**`sub-workflow-dispatch.md`** (2) — SW-008 SW-009

**`claude-hook-bridge.md`** (1) — CHB-026

**`control-points.md`** (1) — CP-059

**`workspace-model.md`** (1) — WM-017


## Appendix B — the 88 requirements cited ONLY in test code

A requirement whose only Go trace is a test is enforced by the test and by nothing else. If the test is deleted, the requirement becomes an orphan silently.

**`workspace-model.md`** (16) — WM-005a WM-008 WM-010 WM-011 WM-012 WM-013d WM-014 WM-018 WM-027 WM-028 WM-029 WM-032 WM-035 WM-036 WM-038 WM-039

**`reconciliation.md`** (13) — RC-001 RC-003 RC-004 RC-005 RC-006 RC-007 RC-008 RC-011 RC-015a RC-028 RC-029 RC-030 RC-031

**`process-lifecycle.md`** (11) — PL-007 PL-014a PL-015 PL-017 PL-018 PL-021 PL-022 PL-023 PL-025 PL-025a PL-026

**`beads-integration.md`** (9) — BI-001 BI-003 BI-004 BI-011 BI-012 BI-018 BI-021 BI-022 BI-023

**`operator-nfr.md`** (7) — ON-013c ON-031 ON-050 ON-051 ON-056 ON-057 ON-058

**`execution-model.md`** (6) — EM-009 EM-032 EM-033 EM-035 EM-037 EM-066

**`handler-contract.md`** (6) — HC-017 HC-019 HC-034 HC-049a HC-051 HC-053

**`control-points.md`** (5) — CP-003 CP-004 CP-019 CP-036 CP-048

**`workflow-graph.md`** (4) — WG-007 WG-043 WG-047 WG-052

**`queue-model.md`** (3) — QM-005 QM-051 QM-065

**`event-model.md`** (2) — EV-021 EV-038

**`architecture.md`** (1) — AR-020

**`credential-isolation.md`** (1) — CI-004a

**`handler-pause.md`** (1) — HP-030

**`pi-harness.md`** (1) — PI-100

**`replay-substrate.md`** (1) — RS-018

**`session-keeper.md`** (1) — SK-015
