# Bootstrap Subset (`hk-ahvq.41`) — Consolidated Synthesis

**Date:** 2026-05-05
**Parent bead:** `hk-ahvq.41` (decompose-to-tasks · "identify minimum bead subset that constitutes the bootstrap")
**Status:** v0.1 — synthesis complete (S07 pending)

> The parent bead `hk-ahvq.41` is **not** marked complete by this document: it is gated on the S07 scenario-harness spec being authored as a parallel work stream and its beads joining `scope:bootstrap` on load. See §7.

---

## §1. Working definition

The bootstrap subset is the smallest set of beads whose implementation produces a daemon binary that can:

1. Start cleanly (pidfile, socket, JSONL writer, daemon-state marker), pass startup self-checks, and reach `ready`.
2. Accept one trivial bead (e.g. `kind:non-agentic` no-op) via `br`, resolve it to a static linear DOT workflow (1–2 nodes), and execute end-to-end.
3. Spawn one twin handler subprocess (`claude-twin`) inside a `git worktree`, capture the watcher event stream, commit one checkpoint with structured trailers, merge back to integration, and close the bead.
4. Survive a clean shutdown + restart with zero state loss (resume / no-op reconciliation Cat 0).

Out of scope for v0 (deferred to first self-build cycles): Pi handler, real `claude-code` handler, sub-workflow recursion, control-points / gates beyond a trivial pass-through, freedom profiles, policy-engine guards, Cat 1–6 reconciliation, improvement loop (S09), CASS/memory, multi-run concurrency, operator pause/upgrade, agent-mail, adze.

User-resolved opening-pass questions applied throughout: **Q1 twin handler IN, Q2 Pi handler OUT, Q3 output = markdown doc + `scope:bootstrap` label, Q4 scenario harness IN.**

(Source: `bootstrap-subset-opening.md` §1; `core-scope.md` §"Ground rules"; `docs/bootstrap.md` §2.)

---

## §2. Final INCLUDE set — 291 beads

| Cluster | Spec | Epic | INCLUDE |
|---|---|---|---:|
| A — Process skeleton | PL | `hk-8mup` | 37 |
| B-WM — Workspace substrate | WM | `hk-8mwo` | 45 |
| B-EM + F — Workflow execution | EM | `hk-b3f` | 65 |
| C — Handler interface + twin | HC | `hk-8i31` | 46 (45 + 1 PULL_IN) |
| D — Event-bus skeleton | EV | `hk-hqwn` | 47 (42 + 5 PULL_INs) |
| E — Beads adapter | BI | `hk-872` | 36 (1 epic + 35 children) |
| Deferred — AR | `hk-zs0` | — | 5 |
| Deferred — ON | `hk-sx9r` | — | 6 |
| Deferred — RC | `hk-63oh` | — | 4 |
| Deferred — CP | `hk-a8bg` | — | **0** (fully deferred) |
| **Total** | | | **291** |

The IDs are enumerated below by cluster. Per-bead rationale lives in the cluster reports under `docs/decompose-to-tasks/bootstrap-subset/{pl,wm,em,hc,ev,bi}-bootstrap.md`, `ar-verification.md`, and `deferred-bootstrap.md`. This document references those rationales rather than re-pasting them.

> **Counts.** Closure-check (`closure-check.md`) is authoritative. The Pass-2 SUMMARY's headline "~271" undercounted because HC's and BI's bullet enumerations had top-line tally errors and AR was given the wrong sensor list. The 291 figure here = 285 from the closure-check sum (PL 37 + WM 45 + EM 65 + HC 45 + EV 42 + BI 36 + AR 5 + ON 6 + RC 4) + 6 PULL_INs.

### A — PL (Process skeleton, 37 beads)

Source: `pl-bootstrap.md`. Covers the entire pidfile/socket/startup/ready/shutdown/agent-supervision happy path plus the harness fixtures verifying it.

`hk-8mup.1, .2, .3, .4, .5, .6, .7, .8, .9, .10, .11, .12, .13, .14, .15, .16, .18, .19, .20, .21, .22, .23, .24, .26, .27, .30, .33, .35, .38, .43, .49, .50, .51, .52, .53, .54, .59`.

### B-WM — Workspace substrate (WM, 45 beads)

Source: `wm-bootstrap.md`. Worktree primitive + lease + branch naming + state machine + merge-back + sidecar + minimum orphan sweep + the 5 schemas + error taxonomy + 5 of 7 §10.2 fixtures.

`hk-8mwo.1, .2, .3, .4, .5, .6, .7, .8, .9, .10, .11, .12, .15, .16, .17, .18, .19, .20, .21, .23, .24, .25, .26, .27, .28, .29, .30, .31, .32, .37, .38, .39, .40, .45, .59, .60, .61, .62, .63, .64, .65, .66, .67, .68, .70`.

### B-EM + F — Workflow execution (EM, 65 beads)

Source: `em-bootstrap.md` (final §3 enumeration). 11 substrate (B-EM) beads + 54 static-execution (F) beads. §6.1 schemas pulled in wholesale (16 of 17) — they form the type vocabulary the rest of the system consumes.

`hk-b3f.1, .2, .3, .4, .5, .6, .7, .8, .9, .10, .11, .12, .13, .14, .15, .16, .17, .18, .19, .20, .22, .23, .24, .25, .29, .30, .32, .33, .35, .36, .37, .38, .39, .40, .41, .42, .50, .51, .52, .53, .54, .61, .63, .66, .67, .68, .69, .70, .71, .72, .73, .74, .75, .76, .77, .78, .79, .80, .81, .82, .83, .85, .86, .87, .88`.

### C — HC (Handler interface + twin, 46 beads)

Source: `hc-bootstrap.md` plus PULL_IN `hk-8i31.15` (HC-013 Adapter surface is fixed) per closure-check. 45 beads from the report's bullet enumeration cover schemas + wire + handler/session + twin parity + minimum sentinels + S07 twin binary; the PULL_IN closes the dep edge from `.48` (DetectReady) and `.64` (one-watcher sensor).

`hk-8i31.1, .2, .3, .4, .5, .6, .7, .8, .10, .11, .12, .14, .15, .21, .24, .25, .26, .28, .34, .42, .43, .44, .45, .46, .47, .48, .49, .50, .51, .52, .53, .55, .58, .59, .64, .65, .67, .69, .71, .72, .73, .74, .75, .76, .77, .78`.

### D — EV (Event-bus skeleton, 47 beads)

Source: `ev-bootstrap.md` plus 5 §8-row PULL_INs per closure-check (PL emits all five on the daemon-startup path). 24 first-class beads (envelope, clock, bus, durability, replay invariant, type-system, schemas) + 23 §8 row children (18 base + 5 PULL_INs).

First-class (24): `hk-hqwn.1, .2, .3, .4, .7, .11, .12, .13, .16, .17, .19, .23, .24, .26, .29, .31, .41, .42, .43, .53, .54, .55, .57, .58`.

§8 rows base (18): `hk-hqwn.59.1, .59.2, .59.3, .59.6, .59.7, .59.8, .59.21, .59.22, .59.23, .59.24, .59.25, .59.37, .59.38, .59.39, .59.57, .59.58, .59.59, .59.60`.

§8 rows PULL_IN (5 — see §3): `hk-hqwn.59.44, .59.46, .59.61, .59.70, .59.71`.

### E — BI (Beads adapter, 36 beads)

Source: `bi-bootstrap.md`. 1 epic envelope + 35 children spanning §4.1–4.8 (selection / managed-data / write / read / propagation / authority / adapter), §6 schemas (minimal 6), §10.2 INV-001 sensor.

`hk-872, hk-872.1, .2, .3, .4, .5, .6, .7, .8, .9, .10, .11, .12, .13, .14, .15, .16, .18, .19, .20, .21, .22, .23, .24, .26, .27, .28, .29, .41, .43, .45, .46, .47, .48, .49, .52`.

### Deferred-AR (Architecture, 5 beads)

Source: `ar-verification.md` (overrides `deferred-bootstrap.md`'s sensor list). Five `kind:invariant` sensor beads — opening-pass listed `.41` mistakenly as a sensor (it is `ar-042` meta-rule) and missed `.54` (`agent_type` regex, hard prerequisite of HC LaunchSpec / Handler).

`hk-zs0.50, .51, .52, .53, .54`.

### Deferred-ON (Operator NFR, 6 beads)

Source: `deferred-bootstrap.md` §3+§5 under Q4. Startup-failure catalog obligation + structural prerequisites + queue version-check + §8 23-code authoritative table + Q4-justified exit-code fixture.

`hk-sx9r.2, .3, .4, .20, .73, .74`.

### Deferred-RC (Reconciliation, 4 beads)

Source: `deferred-bootstrap.md` §4. Cat 0 trio + Cat 5 only — the "no-op resume" path. Cat 1–4, 6a, 6b, investigator-agent contract, verdict-executor, detectors, taxonomy umbrella all post-MVH.

`hk-63oh.16, .17, .62, .70`.

### Deferred-CP (Control Points, 0 beads)

Source: `deferred-bootstrap.md` §2. Fully deferred. Bootstrap workflow has no Gate / Hook / Guard / FreedomProfile / policy-engine touchpoints. CP can stay 0 / 85 through MVH.

---

## §3. PULL_INs applied (6)

Per `closure-check.md` §"Recommended INCLUDE additions". The closure check walked 792 `blocks` edges from the 285-bead INCLUDE set; 68 surfaced as violations against 48 unique targets. After classification, six were genuine additions (the rest were declarative AR §4 references, deferred mechanism forwards, RC test-infra, etc. — see §6). The six:

1. **`hk-hqwn.59.44` `reconciliation_category_assigned`** — emitted by PL-005 (`hk-8mup.10`) per `pl-bootstrap.md` §4. Also referenced by RC `.70`. Without this row in INCLUDE, PL emits an unregistered event type at startup.
2. **`hk-hqwn.59.46` `reconciliation_verdict_executed`** — emitted by PL-005 on every reconciliation pass (Cat 0 emission counts).
3. **`hk-hqwn.59.61` `daemon_degraded`** — emitted by PL-010 (`hk-8mup.19`); also referenced by RC `.17`. The Cat 0 degraded path is in MVH per opening §1.
4. **`hk-hqwn.59.70` `daemon_orphan_sweep_completed`** — emitted by PL-006 (`hk-8mup.11`). Cluster A startup orphan sweep is INCLUDE; its emission target must be too.
5. **`hk-hqwn.59.71` `infrastructure_unavailable`** — emitted by PL-010; also referenced by RC `.16, .62`. Cat 0 detection path emits this.
6. **`hk-8i31.15` Adapter surface is fixed (HC-013)** — depended on by INCLUDE `.48` (DetectReady) and `.64` (one-watcher sensor). HC report enumerated `.73` (Adapter interface schema) but missed `.15`, the rule that fixes the four-method Adapter surface (DetectReady, DetectRateLimit, CleanExitSequence, RotateAccount).

After PULL_IN: EV §8 row count 18 → 23 (of 78 total); HC INCLUDE 45 → 46. **Bootstrap subset = 291 beads, dependency-closed.**

---

## §4. RELAX dropped — `hk-sx9r.20`

Closure-check surfaced one borderline RELAX-candidate: `hk-sx9r.20` (ON-016 queue-schema version-check) cites `.22` (N-1 compat window) and `.77` (compat harness), both deferred. The candidate was either to drop `.20` (relying on Cat 0 `br --version` for version sanity) or keep `.20` and PULL_IN `.22` as a one-line declaration.

**Decision: keep `.20` in INCLUDE.** Bootstrap pins to v1, so the queue-schema version-check rule is trivially satisfied — the rule fires at startup, finds matching v1 schema, no-op passes. The N-1 compat window (`.22`) and compat harness (`.77`) only matter once a v2 schema exists; they remain deferred. The `.22` PULL_IN is unnecessary at v1 because `.20` does not actually need to consult the compat window when both producer and consumer are pinned to the same version. Cat 0 `br --version` (already INCLUDE via `hk-872.26`) provides the adapter-side sanity check.

Final count stays 291; `.20` is in the ON cluster set.

---

## §5. EXCLUDED summary (high-level)

The bootstrap subset omits ~517 beads (~64% of the corpus). High-level rationale, not exhaustive enumeration:

- **AR §4 declarative requirements (49 beads).** Beads `hk-zs0.1`–`.40`, `.42`–`.49` are spec-time declarations satisfied by structural conformance of A–F clusters (envelope declarations, role vocabulary, four-axis tagging). No dedicated implementation tasks at MVH.
- **CP fully deferred (85 beads, all of `hk-a8bg`).** Control-points are post-skeleton. Bootstrap workflow has no Gate / Hook / Guard / Budget / FreedomProfile / Role / Registry exercise paths.
- **RC post-MVH (75 of 79 beads).** Cat 1–4, 6a, 6b, investigator-agent contract, verdict-executor implementation, detectors, taxonomy umbrella, RC test-infra harnesses (`.74`, `.79`). Only Cat 0 trio + Cat 5 are bootstrap.
- **ON between-task and post-MVH (78 of 84 beads).** Pause / stop / upgrade / multi-daemon / silent-hang / RTO benchmark / shutdown drain umbrella. Only startup catalog + structural prerequisites + queue version-check + §8 table + exit-code fixture.
- **PL crash recovery, upgrade, sensor §5 invariants, and §4.7 ntm constraint-rules (22 beads).** Crash recovery is RC's first-self-build territory; upgrade exec-replacement is post-bootstrap; sensors are first-self-build.
- **WM conflict resolution, failed-run / verdict-driven re-run, interrupt state, sensor invariants, polish, S08 read-only, path-reuse subtle sensor (26 beads).** Happy path has no conflicts; CP-037 + ON config-precedence not in bootstrap.
- **EM sub-workflow recursion (7 beads), CP gates (3), backtracking + revision-loop (3), RETRY re-dispatch (1), reconciliation-coupled (3), post-MVH operational (2), perf payload (1), `.84` OutcomeKind variant (1), cross-subsystem authoring sensors (2).** 23 beads total.
- **HC silent-hang FSM, rate-limit + account rotation, advanced skill injection, watcher panic recovery + orphan-reconnect, redaction + secrets registry, crash-recovery harness, foundation declarative rules (compile-absorbed), execution-shape evolution metaclaims, trust audit + sole-publisher sensors (33 beads).**
- **EV post-MVH (`bead_terminal_transition_recovered`), HWM + clock-regression sophistication, async + back-pressure (EV-011/011a/014b/014c/014d), class-conflict acyclicity check, panic / shutdown sophistication, replay sophistication, tagging + amendment + schema-version + N-1 compatibility + breaking-change classification + tagging mechanism + source-subsystem registration + redaction registry + sensors + 62 §8 row children paired with deferred mechanisms (105 beads).**
- **BI crash-recovery / idempotency-key / intent-log family (15), orphan `br` subprocess sweep, advanced concurrency, Beads-CLI skill, release-engineering process, schemas not exercised, cross-store byte-equal sensor (40 beads).**
- **Pi handler.** No Pi-specific beads exist in HC; Pi consumer logic lives in PL/AR (orchestrator-agent boundary), all post-MVH.

Cluster reports under `docs/decompose-to-tasks/bootstrap-subset/{pl,wm,em,hc,ev,bi}-bootstrap.md` carry the per-bead exclude tables; `deferred-bootstrap.md` and `ar-verification.md` cover AR / ON / RC / CP.

---

## §6. IGNORE log — 61 forward-deferred references

Closure-check classified 61 of 68 dependency-closure violations as IGNORE. Recording the global rationales here once rather than per-bead:

1. **AR §4 declarative-requirement targets (38 edges, 28 unique targets).** AR §4 declarative beads (`zs0.1, .2, .3, .5, .7-.18, .25, .28, .33, .37-.49`) are satisfied by structural conformance and do not require implementation tasks. Cited from PL (4), WM (3), EM (5), HC (3), EV (2), ON (1), and 24 AR-internal sensor self-edges. **Global rationale: declarative obligations absorbed by code structure; no runtime task.**

2. **EV §8 row children paired with deferred mechanisms (3 edges).** `hqwn.11 → .59.12 hook_fired` (CP deferred); `hqwn.13 → .59.75 consumer_failed` (async/dead-letter EV-011/.14 deferred); `hqwn.58 → .59.50 store_divergence_detected` (RC detector deferred). **Global rationale: emitting subsystem out of bootstrap; row will be added when its mechanism lands.**

3. **CP forward-referenced primitive (2 edges).** `hk-a8bg.1` ControlPoint primitive cited by `hk-8mup.10, .33`. Bootstrap workflow has no ControlPoint nodes; PL ships an empty registry. **Global rationale: CP fully deferred per `core-scope.md` §10.**

4. **RC + WM-env forward references (9 edges).** RC self → deferred RC test-infra (4 edges: `63oh.16, .17 → .74`; `.62, .70 → .79`); RC `.17 → sx9r.53` heartbeats (post-MVH); WM `.45 → 63oh.65` Cat 3 (unreachable at first-start); RC `.18` detector emit replaced by direct PL emission of `.59.44` (covered by PULL_IN); WM envelope `8mwo.1 → sx9r.22, .32` declarative inheritance. **Global rationale: detector / harness / Cat-N mechanisms post-MVH; first-start Cat 0 + Cat 5 paths fire without them.**

The full violation table is in `closure-check.md` §"Violations — classified by recommendation".

---

## §7. S07 placeholder — pending

**S07 scenario-harness has no dedicated spec or epic in the corpus as of this synthesis.** `bootstrap.md` step 8 names S07; the decompose-to-tasks pass never authored an S07 spec or any S07 beads. The closest existing bead is `hk-8i31.77` (canonical twin handler binary, in the HC cluster, already INCLUDE).

**Resolution path:** S07 is being authored in a parallel work stream as a peer spec. It will produce its own beads later; **those beads will join `scope:bootstrap` on load**, bringing the total above 291.

This synthesis pass does not author S07 content. Until S07 lands:

- `hk-ahvq.41` parent bead remains open (do not flip status to `closed`).
- The 291 IDs labelled here form the **non-S07 bootstrap subset** — necessary, but not sufficient for the §1 working-definition acceptance test (which requires the harness to drive the round-trip).
- Forward-zero verification (`hk-ahvq.39`) and milestone close (`hk-ahvq.42`) carry the S07-pending caveat.

When S07 beads land:
1. Apply `scope:bootstrap` to the new IDs via the same `br update --add-label` command pattern (§8).
2. Re-run `br dep cycles` and re-run a closure-check over the expanded INCLUDE set.
3. Update §2's count and revision history; flip `hk-ahvq.41` to `closed` once `br dep cycles` is clean and the S07 INCLUDE list is dependency-closed.

---

## §8. Label application

Tool: `br update --add-label scope:bootstrap` per `br update --help` (Beads CLI v0.1.45). The `--add-label` flag accepts a single label per invocation but accepts multiple bead IDs. The agent (this synthesis pass) owns the operation; no user action required.

Command pattern (chunked for shell-arg sanity, ~30 IDs per invocation):

```sh
br update <id1> <id2> ... <id30> --add-label scope:bootstrap
```

Application: 291 IDs, 10 chunks (9 × 30 + 1 × 21), zero failures.

Sample output (chunk 1, first 5 IDs):

```
Updated hk-8mup.1: Subsystem envelope declaration (daemon-core / `internal/daemon`)
Updated hk-8mup.2: One daemon per project
Updated hk-8mup.3: Pidfile at `.harmonik/daemon.pid`
Updated hk-8mup.4: Pidfile lock is fd-lifetime advisory (`flock` / `F_OFD_SETLK`)
Updated hk-8mup.5: Atomic pidfile write via truncate-rewrite-keep-fd
```

Full log: `docs/decompose-to-tasks/bootstrap-subset/label-application.log`.

---

## §9. Validation

### `br dep cycles`

```
✓ No dependency cycles detected.
```

(Already known clean from prior session corpus-wide; re-confirmed post-labelling.)

### Label-count sanity

```
$ br list -l scope:bootstrap --limit 0 | grep -c "^❄"
291
```

Per-cluster verification (matches §2 table):

| Cluster | Expected | Verified |
|---|---:|---:|
| PL (`hk-8mup`) | 37 | 37 |
| WM (`hk-8mwo`) | 45 | 45 |
| EM (`hk-b3f`) | 65 | 65 |
| HC (`hk-8i31`) | 46 | 46 |
| EV (`hk-hqwn`) | 47 | 47 |
| BI (`hk-872`, incl. epic) | 36 | 36 |
| AR (`hk-zs0`) | 5 | 5 |
| ON (`hk-sx9r`) | 6 | 6 |
| RC (`hk-63oh`) | 4 | 4 |
| **Total** | **291** | **291** |

---

## §10. What this unblocks

- **`hk-ahvq.39` (forward-zero verification of the deferred-cluster carve-outs)** — can proceed with the caveat that S07-emitted forward-deferred references are not yet in the corpus. Verification result will need an S07-pending caveat in its closure note.
- **`hk-ahvq.42` (decompose-to-tasks milestone close)** — gated on S07 spec landing + S07 beads joining `scope:bootstrap` + a final closure-check over the expanded set. Until then, `.42` stays open.

`hk-ahvq.41` itself is **not** flipped to `closed` by this synthesis. The bootstrap subset is identified and labelled; the parent bead's acceptance criterion includes "subset is dependency-closed and sufficient for the §1 foothold scenario", and the S07 gap means the second clause is not yet met.

---

## §11. Revision history

- **v0.1 (2026-05-05).** Initial synthesis. Pass 2 cluster reports + ar-verification + closure-check consolidated. 291 beads labelled `scope:bootstrap`. RELAX-candidate `hk-sx9r.20` kept (rule trivially satisfied at v1). S07 placeholder section added; parent bead intentionally not closed.
