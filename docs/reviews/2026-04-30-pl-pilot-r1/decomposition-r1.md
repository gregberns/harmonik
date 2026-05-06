# PL Pilot — Decomposition-Quality Review (r1)

`reviewer: decomposition-quality (r1)` — drafted 2026-04-30 against `docs/decompose-to-tasks/pl-pilot.md` v0.1.0 + `pl-pilot-data.yaml` against `specs/process-lifecycle.md` v0.4.1 and discipline v0.9. Method follows `pilot-review-protocol.md` §3.2.

## Sample

15 beads sampled, weighted toward complex:

- **All 5 §5 invariants**: PL-INV-001 / 002 / 003 / 004 / 005
- **The 1 schema**: `pl-schema.daemon-status` (DaemonStatus ENUM completeness check)
- **2 of 10 test-infra**: `pl-test.startup-orphan-sweep-harness`, `pl-test.composition-root-lint`
- **7 §4 reqs**, weighted toward F8b candidates and STATUS.md flags:
  - `pl-005` (11-step startup F8b)
  - `pl-006` (6-bullet orphan sweep F8b)
  - `pl-011` (9-step shutdown F8b)
  - `pl-027` (5-sub-rule upgrade F8b)
  - `pl-014a` (concurrency ceiling — STATUS.md R2 blocker flag)
  - `pl-009` family — `pl-009`, `pl-009a`, `pl-009b` (ready protocol)

Coalesce-rejection spot-checks: F-pilot-PL-2(a) PL-002+002a+002b; F-pilot-PL-2(e) PL-014+014a.

§2.11(c) / §8-zero structural check (F-pilot-PL-1): all three sub-checks (a/b/c).

---

## Per-bead findings

### `pl-005` — Startup order is deterministic (F8b 11-step collapse)

- **Q1 (description fidelity).** MAJOR-leaning-MINOR. Description covers all 11 numbered steps (0..9 + 3a + 8a) at the right level of summary, including step 5's git-trailer walk (cites EM-017 + EM §6.2), step 6's `br ready` + BI-013/016, step 7's in-memory model build per EM §6.1, step 8a's marker reads (daemon.upgrading + daemon.state). **Omissions:** (i) step 5's filesystem-detection mechanism `git for-each-ref refs/heads/run/` is in the spec but not surfaced in the bead description; (ii) step 6's per-`br` invocation timeout `T ≤ 5s` from RC §8.1 is in the spec but not in the bead. Both are sub-clause omissions an implementer would catch by re-reading the spec; not divergence-causing. **MINOR / `local`.**
- **Q3 (multi-step F8b).** **MAJOR / `class`.** This is the highest-risk F8b decision in the pilot. The §2.2 signals fire ALL three (≥3 steps; each step independently testable; umbrella loses meaning when stripped). The pilot rests collapse on the F8b shared-function-body tiebreaker, citing `bootstrap_daemon` as one cohesive function body across 11 steps. **The F8b worked examples in v0.9 discipline (BI-031 step-4 sub-cases sharing one reissue state machine; EM-016's 3-op git atomic sequence; CP-040a; WM-016) all involve N steps that share an actual code path** — the same state machine, the same git object-write path, the same atomic discipline. PL-005's 11 steps have RADICALLY DIVERSE code paths: composition-root bootstrap (step 0), advisory-lock acquisition (step 1), event emission (step 2), orphan-sweep delegation (step 3 → PL-006), socket bind (step 3a), Cat 0 delegation (step 4 → RC-012), git log walk (step 5), Beads query (step 6), in-memory model build (step 7), RC dispatch delegation (step 8 → RC-008), marker reads (step 8a), ready transition (step 9). **A `bootstrap_daemon()` Go function calling 11 distinct sub-functions is not a "shared function body" in the F8b sense — it is a sequential composition of independent phases.** Several steps are pure delegations (3, 4, 8) where the actual code path lives in another spec's bead, which weakens the "umbrella loses meaning" claim. The decomposition-quality concern is that F8b appears to have been stretched beyond its v0.9 worked-example precedent envelope. Triage candidate for synthesis: either (a) the discipline §2.2 F8b clause needs tightening to disambiguate "cohesive function body" from "phase-sequenced umbrella function", or (b) PL-005 should split into an umbrella + 11 step beads (the pilot's "single bead" choice is defensible but deserves explicit reviewer signoff). Lane: `class` because the F8b precedent extension to 11-step diverse-code-path sequences is the kind of decision any future behavioural spec (RC, ON) will reproduce or reject; the discipline is currently silent on the shape envelope of F8b's "shared function body" tiebreaker.
- **Q4 / Q5.** N/A.

### `pl-006` — Orphan sweep precedes reconciliation (F8b 6-bullet)

- **Q1.** Description covers all 6 bullets (tmux, worktree locks, subprocess cleanup with handler + `br` sub-bullet, stale intent files left for RC, stale reconciliation locks with `flock` probe, completion event). All six bullet-level sub-rules from spec lines 245-250 are surfaced. **CLEAN.**
- **Q3 (multi-step F8b).** **CLEAN / `local`.** F8b application is sound here. All 6 bullets share (a) the PL-006a provenance-marker filter as a unifying input precondition, (b) the `daemon_orphan_sweep_completed` emission as a unifying epilogue, and (c) sequential composition inside one `orphan_sweep` Go function body in any plausible implementation. The bullets are PHASES of one filesystem-traversal sweep, not independent procedures. Distinct from PL-005's case: here the steps share a unifying filter+epilogue, which is the F8b shared-function-body criterion. Decision is faithful to F8b precedent.

### `pl-011` — Graceful shutdown drains in-flight runs (F8b 9-step)

- **Q1.** Description covers all 9 steps (transition draining → stop pulling → classify in-flight → wait → emit → flush → release leases → release pidfile + remove socket → exit). Step 3's three sub-classes (mid-agent-work / just-checkpointed / gate-pending) are surfaced. The step-3-complete signal (watcher-aggregation + (ii)+(iii) immediate quiescence) is in the spec but is NOT in the bead description — implementer would need spec lookup to discover the aggregation rule. MINOR omission. **MINOR / `local`.**
- **Q3 (multi-step F8b).** **MAJOR-borderline / `class`.** Same shape as PL-005 concern, weaker. 9 steps with diverse code paths: status transition, queue stop, classify-in-flight (3 sub-cases), bounded wait + SIGKILL escalation, event emission, fsync flush, WM lease release, pidfile + socket cleanup, exit. The classify-in-flight phase (step 3) is itself a 3-case sub-protocol with its own logic. Steps 6 (`fsync` flush), 7 (lease release per WM-013b), 8 (pidfile/socket cleanup), 9 (exit code branching) each have independent failure modes and independent testability. F8b is on the boundary. The shared-function-body argument (`graceful_shutdown` as one cohesive function) is stronger than for PL-005 because the drain protocol IS one logical phase with a single completion criterion (drain done OR drain timeout). Defensible, but synthesis should record the borderline call alongside PL-005's. Lane: `class` for the same reason as PL-005 — F8b's "shared function body" envelope needs explicit shape guidance.

### `pl-027` — Upgrade contract obligation (F8b 5-sub-rule)

- **Q1.** All 5 sub-rules (exec-replacement / skip-path under exec / fd-passing / marker write / event emission) covered with concrete mechanics: `HARMONIK_UPGRADE=1`, `HARMONIK_LISTENER_FD`, `net.FileListener(os.NewFile(fd, ""))` adoption, `FD_CLOEXEC` re-set, `daemon.upgrading` marker via WM-026 atomic discipline, exit code 14 on hash mismatch, three operator-* event emissions. **CLEAN.**
- **Q3 (multi-step F8b).** **CLEAN / `local`.** F8b application is sound. The 5 sub-rules form a single `upgrade_handoff` function body where exec-replacement is inseparable from fd-passing (both must execute around the same `execve` call), marker write is inseparable from the event emission ordering ("emit `operator_upgrading` before exec; write marker before exec; execve"), and skip-path detection on the new binary is a check at the top of the same `bootstrap_daemon` entry. Genuinely tight coupling. Distinct from PL-005's loose coupling. F8b precedent envelope clearly satisfied.
- **Spec-cite check on STATUS.md flag.** STATUS.md flagged "sub-rule (iii) socket-rebind self-contradicts MUST-NOT-unlink." Spot-check: spec sub-rule (iii) at lines 547-549 says new binary MUST NOT call `bind()` — instead MUST call `net.FileListener(os.NewFile(fd, ""))`. Pilot description matches the spec faithfully (does not contradict). The STATUS.md flag may refer to a spec-level concern (PL-003 says "remove stale socket on startup before binding" while PL-027 says new binary doesn't `bind()` at all on upgrade path) — but that is a spec-correctness question outside this reviewer's remit per protocol §7.

### `pl-014a` — Per-daemon concurrency ceiling

- **Q1 (description fidelity).** **MINOR / `local`.** Two omissions vs spec:
  1. Spec says "MUST attempt `setrlimit` to raise the soft limit to **`min(4096, hard)`**". Pilot description says "MUST attempt `setrlimit` to raise" but omits the explicit target value `min(4096, hard)`. An implementer would need to look up the spec for the target.
  2. Spec says "MUST log a warning on failure" of the `setrlimit` raise. Pilot description does not surface this clause.
  Both are sub-clause omissions an implementer would catch by re-reading PL-014a; not divergence-causing. **MINOR.** STATUS.md's R2 blocker flag is more about the cross-daemon coordination forward edge to ON-041 (which IS captured as `forward:on-041` per the F-pilot-PL-4 named-obligation pattern) than about the bead description itself.
- **Q3 / Q4 / Q5.** N/A.

### `pl-009` — Ready criteria

- **Q1.** Description summarises the 5 ready criteria. Spec has SIX bulleted criteria (lines 304-309): orphan sweep complete; Cat 0 passed; git walk + Beads query complete; in-memory model built; reconciliation dispatch complete (with sub-clause about per-run RC-013 emission OR investigator route); every in-flight run received category emission per RC-013. The pilot collapses the last two bullets into one ("reconciliation dispatch complete (every in-flight run has received category emission per RC-013 — investigator workflows MAY remain in-flight)"); the merge is faithful to the spec's intent because the second-to-last bullet's sub-clause already names RC-013 — but a strict bullet-count reader would flag it. **MINOR / `local`** (judgment call; collapse is defensible because the spec's two bullets restate the same RC-013 emission requirement at different scopes).
- **Q2 (coalesce rejection F-pilot-PL-2(c)).** Reasoning sound. PL-009b's three-mechanism ready-protocol surface (socket probe / sd_notify / ready-file fallback) is genuinely independently testable from PL-009's transition criteria. PL-009a's Cat 3 fallback path is a behavioural reroute orthogonal to the criteria predicate. Test 3 (split reduces to "see anchor") fails — none of the three reduce to PL-009. Rejection correctly applied.

### `pl-009a` — Auto-resolver failure routes to Cat 3

- **Q1.** All 4 lettered sub-rules from spec (a) emit reconciliation_category_assigned with original category; (b) re-classify into Cat 3; (c) dispatch investigator per RC-008; (d) proceed toward ready with run_id in investigator_run_ids[]. Daemon MUST-NOT clauses (no block on ready, no leave-unclassified-at-ready) covered. Cat 6 escalation reference to RC §8.11a covered as informational. **CLEAN.**

### `pl-009b` — Ready-protocol surface for external callers

- **Q1.** All three mechanisms (socket probe with backoff parameters / `sd_notify("READY=1")` on systemd / `.harmonik/daemon.ready` fallback) covered. The OQ-PL-002 `T_ready_wait = 60s` cap surfaced. Spec's "External callers MUST NOT assume daemon ready from pidfile/socket presence alone" surfaced. **CLEAN.**

### `pl-014` — Agent subprocesses children of daemon (single cmd.Wait reaper)

- **Q1.** Description covers parentage discipline + provenance marker requirement + the `*exec.Cmd` ownership rule (exactly one goroutine, exactly one `cmd.Wait()`). Watcher-supervision delegation to HC-011 / HC-024 noted. The "zombie produced if `cmd.Wait()` not called — PL-INV-005 violation regardless of `kill(pid, 0)` reporting" sentence faithfully translated from spec. **CLEAN.**
- **Q2 (coalesce rejection F-pilot-PL-2(e)).** Sound. Parentage (OS-level child-of-daemon relationship + spawn-site provenance marker) and concurrency ceiling (`getrlimit` + `min(soft/8, 1024)` math + `dispatch_deferred` emission) are distinct code paths with no shared data shape. Test 1 fails. Rejection correct.

### `pl-inv-001` — Sensor: one daemon per project (pidfile + content match)

- **Q4 (sensor real-mechanism).** Concrete: pidfile lock held by daemon's fd AND content parses AND parsed PID = `getpid()` AND parsed PGID = `getpgrp()` AND parsed `daemon_instance_id` = in-memory id. Five-clause conjunction is a real verification predicate, not a restatement. Backwards-compat fallback to two-line v0.4.0 pidfile noted per PL-002b reader-tolerance. **CLEAN.**
- **Edge correctness.** Predecessors: pl-002, pl-002a, pl-002b. Spec line 585 cites all three. Faithful.

### `pl-inv-002` — Sensor: deterministic daemon (`go-arch-lint` + binary import-graph)

- **Q4.** Two real verification mechanisms named: build-time `go-arch-lint` rule on `internal/daemon` package imports asserting no LLM SDK in transitive closure; binary-level import-graph scan. Both are concrete tooling steps an implementer can wire. Not a restatement. **CLEAN.**
- **Edge correctness.** Predecessors include `ar-inv-007` per the explicit invariant→invariant ID-cite (PL-INV-002 body cites AR-INV-007 by ID; F-refs-EV-6 / D-WM-3 precedent applies). Pilot correctly emits this edge. Faithful.

### `pl-inv-003` — Sensor: orphan sweep completes before classification

- **Q4.** Sensor mechanism is the in-memory `orphan_sweep_complete_at: Timestamp` flag plus an assertion-at-detector-dispatch site (every PL-005 step 8 path MUST assert non-nil before invoking any RC detector); assertion failure panics per PL-018a. Concrete, testable. **CLEAN.**

### `pl-inv-004` — Sensor: socket-path exclusivity

- **Q4.** Sensor mechanism is "the daemon that holds the pidfile lock is the exclusive owner of the bound socket fd; second daemon observing `EADDRINUSE` MUST exit with ON §8 code 6 per PL-008a; the exit path is the sensor." Concrete. The exit-code-6 path serves as the runtime sensor. **CLEAN.**

### `pl-inv-005` — Sensor: agent subprocess parentage

- **Q4.** **MINOR / `local`.** Sensor description: "every spawn site MUST set the provenance marker of PL-006a (env var + PGID). A subprocess without the marker is not a harmonik-owned subprocess by definition and MUST NOT be reaped by PL-006." This is closer to a re-declaration of the spawn-site obligation than a verification predicate. The predicate "MUST set the marker" is what `pl-014` and `pl-006a` already require. A real PL-INV-005 sensor would be (e.g.) a static-analysis lint that scans every `os/exec.Cmd` spawn site in the codebase for `SysProcAttr.Setpgid==true` and `Env` containing `HARMONIK_PROJECT_HASH`, OR a runtime test fixture that asserts every live handler subprocess has the env var set. The bead's sensor description names neither. **Borderline restatement.** Bumping to MINOR rather than MAJOR because the spec's PL-INV-005 sensor line itself is similarly thin (line 617) — the spec is the source of the under-specification, not the pilot — but the pilot could surface the static-analysis-lint or runtime-fixture mechanism as the verification surface.

### `pl-schema.daemon-status` — DaemonStatus ENUM (Q5)

- **Q5 (ENUM completeness).** Spec §6.1 declares 7 enum values with one-line semantics each: `starting`, `reconciling`, `degraded`, `ready`, `paused`, `draining`, `stopped`. Bead description enumerates all 7 verbatim with their semantics. The PL-vs-ON ownership boundary noted ("PL owns starting → reconciling → ready prefix and pre-ready degraded; ON §4.3 owns paused/draining/stopped"). The spec NOTE about post-`ready` degradation NOT corresponding to a transition (deferred as OQ-PL-009) is captured. The N-1 ON-018 compatibility clause for unknown statuses is captured. **CLEAN.** All 7 values present; no schema completeness gap.

### `pl-test.startup-orphan-sweep-harness` — Test infra

- **Q1.** Test description covers seeding tmux sessions / stale worktree locks / re-parented handler subprocesses (with and without provenance marker) / re-parented `br` subprocesses / stale reconciliation locks (with and without `Harmonik-Verdict-Executed: true` per RC-002b) / stale intent files. Asserts orphan-sweep payload counts including `br_subprocesses_killed` + `reconciliation_locks_removed`. Asserts subprocesses lacking marker are NOT killed. Asserts active-acquisition locks NOT removed. Asserts `orphan_sweep_complete_at` flag set before any reconciliation detector runs. Maps cleanly to spec §10.2. **CLEAN.**

### `pl-test.composition-root-lint` — Test infra

- **Q1.** Build-time `go-arch-lint` rule + binary-level import-graph scan (matches PL-INV-002 sensor). Wiring test for event bus + control-point registry + handler registry + skill registry + policy registry instantiation in composition root. Panic-barrier test asserts ON §8 code 19. The five registries enumerated align with PL-020a's enumeration ("event bus, control-point registry, handler registry, skill registry, policy registry"). **CLEAN.**

---

## Missing-coalesce smell scan

Walked the §4 prefix clusters for plausibly-missed coalesces (clusters of `<prefix>-NNN/NNNa/NNNb` siblings sharing a code path):

- **PL-002 / 002a / 002b (pidfile + lock primitive + atomic write).** F-pilot-PL-2(a) rejection sound (test 3 fails: fd-lifetime advisory primitive testable independently from atomic write).
- **PL-003 / 003a / 003b (socket + wire format + pre-ready rejection).** Plausibly-missed cluster — NOT enumerated in F-pilot-PL-2 rejection list. Test 1 (single shape/path): borderline — PL-003 is bind + `chmod(0600)` + stale-removal, PL-003a is JSON-RPC method-name inventory, PL-003b is the pre-ready rejection state machine. Three distinct code paths: fs operations, wire protocol, request validation. Test 1 effectively fails. Coalesce correctly NOT applied. Note that the F-pilot-PL-2 finding only enumerated 7 candidates considered (a..g); PL-003 family wasn't surfaced. Suggest noting in pilot revision: "PL-003 family considered and rejected at test 1". MINOR / `local`.
- **PL-006 / 006a (orphan sweep + provenance marker).** PL-006a defines the marker; PL-006 enumerates the sweep. The marker is consumed by PL-006 + PL-007 + PL-014 + PL-INV-005. Test 1 fails (marker is a data-shape rule; sweep is a procedure). Correctly separate.
- **PL-021 / 021a / 022 / 023 (ntm adapter family).** Four reqs covering allowed-surface / version-pin / forbidden-surface / boundary-rule. Distinct code paths (capability whitelist, version probe, capability blacklist, build-time review rule). Correctly separate.
- **PL-024 / 025 / 025a / 026 (crash semantics family).** Stale pidfile, startup-reconciliation re-run, lifecycle pairing tolerance, agent-crash routing. Four orthogonal concerns. Correctly separate.

**No actionable missing-coalesce smell.** F-pilot-PL-2 should add PL-003 family to its rejection enumeration for completeness (MINOR documentation tightening).

---

## Over-split smell scan

- **No multi-step protocols of 2 steps minted as splits** — pilot has 0 splits. ✓
- **No step beads whose descriptions reduce to sub-bullets of parent** — pilot has 0 step beads. ✓
- **F8b applications:** PL-005 and PL-011 (see per-bead findings above) raise a different concern — F8b being applied to phase-sequenced umbrellas where the steps DON'T share a function body in the BI-031 / EM-016 sense. This is the inverse of over-split smell — it's a possible *over-collapse* concern.

---

## §2.11(c) / §8-zero structural check (F-pilot-PL-1)

Three sub-checks:

- **(a) PL §8 actually says "no taxonomy ownership".** **VERIFIED.** Spec line 682 reads verbatim: *"This spec does not own a failure taxonomy. Startup failure modes are cataloged per §PL-008 (obligation owned by [operator-nfr.md §4.1 ON-003]); §PL-008a names the codes this spec consumes from the authoritative ON §8 taxonomy."* Pilot's F-pilot-PL-1 quotation is faithful.
- **(b) PL §4 reqs that name error codes correctly cite the OWNING spec's taxonomy.** **VERIFIED.** PL-008a body enumerates 11 codes (5/6/7/8/9/10/14/19/22/23 plus "9: filesystem-unwritable mapping" carve-out) all cited as `[operator-nfr.md §8] code N`. Pilot bead description preserves the consumer-of-ON taxonomy framing. Edges: pl-008a → forward:on-NNN (since ON pilot not loaded); pl-008a → ev-events.daemon-startup-failed (active EV edge for the emission shape). The 11-code enumeration as `forward:on-NNN` is accurate per F-pilot-PL-4 named-obligation pattern.
- **(c) §2.11(c.2) anti-pattern N/A check — no local taxonomy bead to consume.** **VERIFIED.** Pilot has 0 `pl-error.taxonomy` beads. The §2.11(c.2) anti-pattern (`<spec>-error.taxonomy → <req>` inverted-direction edge) cannot fire because there is no `pl-error.taxonomy` bead. The PL pilot is the **first corpus instance** where the anti-pattern is structurally N/A; pilot's framing of this as "the v0.9 §2.11(c.2) clause gracefully degrades when a spec is purely a consumer of sibling taxonomies" is correct.

**F-pilot-PL-1 confirmed RESOLVED/CONFIRMATION lane.** No discipline patch needed; the §2.11(c.2) v0.9 clause already covers this gracefully.

---

## Summary

| Severity | Count | Beads |
|---|---|---|
| BLOCKER | 0 | — |
| MAJOR | 2 | `pl-005` (F8b 11-step over-collapse concern), `pl-011` (F8b 9-step borderline) |
| MINOR | 5 | `pl-005` (sub-clause omissions), `pl-011` (step-3 aggregation rule omission), `pl-014a` (`min(4096, hard)` + warn-on-failure omissions), `pl-009` (6→5 bullet collapse), `pl-inv-005` (sensor borderline restatement), F-pilot-PL-2 enumeration completeness (PL-003 family rejection not listed) |
| CLEAN | 8 | `pl-006`, `pl-027`, `pl-009a`, `pl-009b`, `pl-014`, `pl-inv-001`, `pl-inv-002`, `pl-inv-003`, `pl-inv-004`, `pl-schema.daemon-status`, both test-infra beads sampled, F-pilot-PL-1 |

(Counts above add minors > 5 because some beads carry both MINOR and CLEAN observations across different probes; the Severity column is the worst per-bead grade.)

### Lane assignment

- **`class` (discipline-lane candidates):**
  1. **PL-005 / PL-011 F8b application beyond worked-example precedent envelope.** v0.9 §2.2 F8b worked examples (BI-031 sub-cases sharing one state machine; EM-016 3-op git atomic with one function body; CP-040a; WM-016) all involve N steps that share an actual code path. PL-005 (11 steps, radically diverse code paths, several pure delegations) and PL-011 (9 steps, with step 3 itself a 3-case sub-protocol) push the F8b envelope into "phase-sequenced umbrella function" territory. **Recommendation for synthesis:** the discipline §2.2 F8b clause may need a tightening to disambiguate "shared function body" (one cohesive state machine / one shared code path) from "phase-sequenced umbrella" (sequential composition of independent phases). Triage probe (generality): YES — RC and ON pilots will both contain multi-step protocols (RC's 11-category taxonomy + Cat 3a/3b/3c sub-detector sequence; ON-027's cross-subsystem shutdown ordering) where the same question fires. The PL F8b decision will set precedent. Bias toward over-flagging the discipline gap.

- **`local`:** all other findings (sub-clause omissions, F-pilot-PL-2 enumeration completeness, sensor borderline restatement on PL-INV-005).

### Recommendations

1. **Discipline-lane (synthesis decision):** add a §2.2 F8b clarification on the boundary between "cohesive function body" and "phase-sequenced umbrella". If the clarification lands, PL-005 / PL-011 may need to re-decompose into umbrella + step beads; if F8b is held to current envelope, document explicitly that 9–11 phase-sequenced steps with diverse code paths qualify when wrapped in a single named function. Either outcome unblocks RC + ON pilots.
2. **Pilot-lane:** apply MINOR sub-clause completeness patches to `pl-005`, `pl-011`, `pl-014a`, `pl-009`. Bump pilot v0.1.0 → v0.1.1.
3. **Pilot-lane (low-priority):** F-pilot-PL-2 enumeration list should add the PL-003 family to its 7-candidate rejection roster for completeness (currently has 7 candidates a..g; PL-003 family rejection not enumerated).

### Out of scope for this reviewer

- F-pilot-PL-1 (zero §8 ownership) — confirmed sound; no decomposition concern.
- F-pilot-PL-3 (the F8b applications themselves) — see PL-005 / PL-011 lane-class concern above; PL-006 + PL-027 F8b applications are sound.
- F-pilot-PL-4 (cycle-break NOTE vs named-obligation tension producing 26 §3.2-violation forwards) — references reviewer is the appropriate lane for the §3.2 violation finding; this reviewer notes only that the on-edges are correctly tagged forward-deferred and no decomposition error exists.
- F-pilot-PL-5 (term-use chains) — sampling shows term-uses correctly emit edges per §3.1 step 5; no decomposition concern.
- F-pilot-PL-6 (PL §4.a status) — confirmed PL authored §4.a in r1; not a grandfather case; pl-env-001 is correctly first-class.
