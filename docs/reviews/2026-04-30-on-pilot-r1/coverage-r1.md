# ON Pilot — Coverage Review (r1)

`reviewer: coverage` · `protocol: pilot-review-protocol.md §3.1 v0.2` · `inputs: specs/operator-nfr.md v0.4.1 (995 lines, status reviewed) · docs/decompose-to-tasks/on-pilot.md v0.1.0 (352 lines) · docs/decompose-to-tasks/on-pilot-data.yaml (1653 lines, 84 mnem beads) · discipline.md v0.9`

## Summary line

**CLEAN.** Every numbered ID in ON §4 (1 envelope ON-ENV-001 + 59 numbered ON-NNN topical reqs = 60 first-class entries), §5 (4 active invariants), §6 (zero schemas — §6.1/§6.2/§6.3 explicitly omitted), and §8 (24 exit-code rows consolidated into the single `on-error.taxonomy` bead) is accounted for in the pilot. Retired §5 IDs (ON-INV-002, ON-INV-004) confirmed as never-reusable per spec §5 headers. Pilot §1 / §9 / yaml top-comment counts internally consistent (84 = 1 + 60 + 8 + 4 + 0 + 1 + 11). Pilot's spec-version reference matches `specs/operator-nfr.md` v0.4.1 exactly. Zero BLOCKER, zero MAJOR. Two MINOR informational notes (one labeling shorthand on the §8 row count; one ON-013b forward-reference observation) — neither alters coverage status.

---

## 1. Enumeration of source-spec IDs

### 1.1 §4 normative requirements (active) — 60 first-class entries

**§4.a envelope (1):** ON-ENV-001.

**§4.1..§4.11 numbered ON-NNN headers (59):** ON-001, ON-002, ON-003, ON-004, ON-005, ON-005a, ON-006, ON-007, ON-008, ON-009, ON-010, ON-011, ON-012, ON-013, ON-013a, ON-013c, ON-014, ON-015, ON-016, ON-017, ON-018, ON-019, ON-020, ON-020a, ON-021, ON-022, ON-023, ON-024, ON-025, ON-026, ON-027, ON-027a, ON-028, ON-029, ON-030, ON-030a, ON-031, ON-032, ON-033, ON-034, ON-035, ON-036, ON-037, ON-038, ON-039, ON-040, ON-041, ON-042, ON-043, ON-044, ON-045, ON-046, ON-047, ON-048, ON-049, ON-050, ON-051, ON-053, ON-054.

Direct `grep -E "^####? ON-[0-9]" specs/operator-nfr.md | wc -l` = **59**. Plus 1 envelope = **60 first-class §4 entries**. STATUS.md baseline says 61 — STATUS.md is off-by-1 per pilot's F-pilot-ON-1 (the v0.4.1 OQ-RC-009 resolution at spec line 949 is a §11 cross-ref note, NOT a numbered ON-NNN heading and not in §4). Direct read confirmed: line 949 is in §11 ("Open questions"), introduced by `> **Cross-ref note (OQ-RC-009 resolution, v0.4.1).**` — paragraph-shaped prose, not a §4 heading. **Pilot is correct; STATUS.md drift acknowledged.**

ON-013b is referenced in spec body (lines 306, 520) as "when ON-013b lands" / "per ON-013b daemon-instance handshake" — this is a forward-reference to a future requirement, NOT a current §4 entry; no header exists for ON-013b. ON-005b appears only inside `OQ-ON-005b` (an open-question label). Neither requires a bead.

### 1.2 §5 invariants — 4 active, 2 retired

- **Active:** ON-INV-001 (N-1 compat window joint hold), ON-INV-003 (secrets never unredacted), ON-INV-005 (every-subsystem reconstruction-contribution), ON-INV-006 (no control surface bypasses between-task invariant).
- **Retired (never reusable):** ON-INV-002 ("(retired in v0.3)" header at line 597; operational posture moved to §2.1a), ON-INV-004 ("(retired in v0.3)" header at line 611; was a restatement of §4).

Direct read confirms both retired headers are present with explicit "(retired in v0.3)" suffix and rationale.

### 1.3 §6 schemas — 0

§6 prose verbatim (line 636): "This spec does not introduce new persistent data types. Schemas referenced: …" with explicit follow-up at line 646: "§6.1, §6.2, §6.3 are intentionally omitted — this spec introduces no persistent data types, no YAML/JSON snippets, no tabular schemas of its own." §6.4 is schema-evolution prose; §6.5 is co-owned event payload routing (8 events owned by EV per §2.11(d.2)). **Zero §6 schema constructs.** Second corpus pilot to declare zero §6 ownership (mirrors PL's F-pilot-PL-1 zero-§8 instance per F-pilot-ON-3).

### 1.4 §8 errors — 24 exit-code rows (consolidated to 1 bead)

§8 table contains exit codes 0..23 inclusive = **24 rows total** (1 success + 23 non-zero failure codes). Direct count via `grep -cE "^\| [0-9]+ \|"` = 24. Pilot describes this as a "23-code authoritative table" — accurate as "23 non-zero failure codes" but technically the table has 24 numbered rows including code 0 = success. Single-bead form (`on-error.taxonomy`) per discipline v0.9 §2.11(c) + SHAPE-not-COUNT tiebreaker per F-pilot-WM-2; the 24 codes are sentinel VALUES of one vocabulary, not 24 independent codepaths.

### 1.5 Retired §4 IDs — 0

No `[retired]` or `(retired …)` markers on any §4 ON-NNN heading. (Both retired entries are in §5, not §4.)

---

## 2. Coverage verification

### 2.1 §4 reqs (60 first-class → 60 first-class beads)

Yaml has **84 mnem entries**; subtract 4 invariants + 1 taxonomy + 11 test-infra + 8 step beads (`on-027.s1..s7` + `s3a`) = **60 §4-mapped req beads**. One-to-one walk:

- ON-ENV-001 → `on-env-001` ✓
- ON-001..ON-004 → `on-001`..`on-004` ✓
- ON-005, ON-005a, ON-006 → `on-005`, `on-005a`, `on-006` ✓
- ON-007..ON-014 (incl. ON-013a, ON-013c) → `on-007`..`on-014`, `on-013a`, `on-013c` ✓
- ON-015..ON-017 → `on-015`..`on-017` ✓
- ON-018, ON-019 → `on-018`, `on-019` ✓
- ON-020, ON-020a, ON-021 → `on-020`, `on-020a`, `on-021` ✓
- ON-022..ON-029 (incl. ON-027a) → `on-022`..`on-029`, `on-027a` ✓
- ON-030, ON-030a, ON-031..ON-033, ON-053 → `on-030`, `on-030a`, `on-031`..`on-033`, `on-053` ✓
- ON-034..ON-040 → `on-034`..`on-040` ✓
- ON-041..ON-044, ON-050, ON-051, ON-054 → all matched ✓
- ON-045..ON-049 → `on-045`..`on-049` ✓

**No missed reqs. No phantom reqs.** Every `req: ON-NNN` field in the yaml maps to a real spec heading.

### 2.2 ON-027 split — 8 step beads (per F-pilot-ON-5 SPLIT decision)

Spec ON-027 body (line 357) names eight ordered drain steps: (1) dispatcher off; (2) checkpoint suspend; (3) handler-subprocess wait; (3a) `br`-CLI intent-log drain; (4) event-bus fsync; (5) memory flush; (6) workspace unlock; (7) orchestrator exit / enter `paused`. Yaml emits 8 step beads — `on-027.s1`, `on-027.s2`, `on-027.s3`, `on-027.s3a`, `on-027.s4`, `on-027.s5`, `on-027.s6`, `on-027.s7` — one per spec sub-rule. **All 8 sub-rules covered.** Umbrella bead `on-027` carries `blocks` edges to all 8 step beads + the F8c constraint adjuncts (`on-027a`, `on-029`).

### 2.3 §5 invariants (4 active → 4 sensor beads)

- ON-INV-001 → `on-inv-001` ✓
- ON-INV-003 → `on-inv-003` ✓
- ON-INV-005 → `on-inv-005` ✓
- ON-INV-006 → `on-inv-006` ✓

ON-INV-002 and ON-INV-004 retired in v0.3 — no beads minted (correct per discipline §2.5 "only ACTIVE invariants produce sensors"; pilot §1 + §4 + yaml top-comment all explicitly note "never reusable").

### 2.4 §6 schemas (0 schema beads — confirmed)

ON declares zero §6 schemas; yaml has zero `kind: schema` beads. Match per F-pilot-ON-3. Inline-prose normative content in ON-035 (structured-log shape), ON-036 (health-check `health_status` enum), and the §3 glossary `DaemonStatus` reference all resolve via cross-spec cites or inline-in-§4 text — none qualify as §6 RECORD/INTERFACE/ENUM/primitive declarations. The 8 §6.5 co-owned events route to EV-row beads via `on-NNN → ev-events.<name>` edges per §2.11(d.2).

### 2.5 §8 errors (24-row table → 1 single-bead taxonomy)

`on-error.taxonomy` (kind: error-taxonomy) covers all 24 exit-code rows. Pilot §6 enumerates the 24 codes (0=success through 23=orchestrator-agent-unavailable) inside the bead description. 16 intra-ON consumer beads emit `blocks` edges into `on-error.taxonomy` per §2.11(c.2): on-001, on-002, on-003, on-013, on-016, on-018, on-020, on-020a, on-027 (+ s4/s5/s6/s7), on-027a, on-029, on-031, on-032, on-041, on-048, on-053. Per direct yaml inspection, all 16 consumer edges are present.

### 2.6 Retired markers (0 in §4)

No spec-side `[retired]` markers on §4 ON-NNN headings. Pilot §1 says "No retired §4 IDs" — match. (The 2 retired IDs are in §5; both correctly exempted from sensor-bead emission.)

---

## 3. Counts and arithmetic verification

### 3.1 Pilot §1 "Counts" section

| Claim | Source-spec actual | Match |
|---|---|---|
| 60 active §4 normative requirements (1 envelope + 59 topical) | 1 envelope + 59 numbered §4 headers = 60 | ✓ |
| 0 §2.3 coalesces (12 candidates considered + rejected per F-pilot-ON-2) | 12 candidate clusters enumerated; each analyzed against §2.3 Tests 1/3 | ✓ |
| 0 §2.1a pure-cross-reference collapses | No ON req is a verbatim restatement of another with single in-spec cite | ✓ |
| 1 §2.2 multi-step split (ON-027 → umbrella + 8 step beads) | ON-027 spec body enumerates 8 steps (1, 2, 3, 3a, 4, 5, 6, 7); F8b "diverse code paths" tiebreaker applied | ✓ |
| 4 active §5 invariants (ON-INV-001/003/005/006); ON-INV-002/004 retired in v0.3 | §5 headers confirmed: 001/003/005/006 active, 002/004 marked "(retired in v0.3)" | ✓ |
| 0 §6 schemas (§6.1/§6.2/§6.3 explicitly omitted) | §6 omission declaration at spec line 646 | ✓ |
| 1 single-bead `on-error.taxonomy` covering §8 (described as "23 codes") | 24 §8 rows total (codes 0..23); single-bead form correct per SHAPE-not-COUNT | ✓ (with shorthand label note — see §4 below) |
| 11 test-infra beads (one per §4 sub-section group) | yaml lists 11 `on-test.*` beads | ✓ |
| Total: 84 beads (1 epic + 60 req + 8 step + 4 invariant + 0 schema + 1 taxonomy + 11 test-infra) | 1+60+8+4+0+1+11 = 84; yaml has 84 mnem entries | ✓ |
| Multiplier 83/60 = 1.40× | 83/60 = 1.383 (rounds to 1.40×) | ✓ |

### 3.2 Pilot §1 vs §9 internal consistency

§1 sanity tally and §9 load-gate sanity tally both print the same table (Spec parent 1; Requirement beads 60; Step beads 8; Sensor/invariant 4; Schema 0; Error-taxonomy 1; Test-infra 11; Total 84). Identical. Yaml top-comment (lines 36–60+) reproduces the same per-§4-subsection breakdown and total. Three sources internally consistent.

### 3.3 Pilot's spec-version reference

- Pilot §1 line 3: "against `specs/operator-nfr.md` v0.4.1 (status `reviewed`, last-updated 2026-04-25) and discipline v0.9".
- Yaml top comment line 2: "Drafted 2026-04-30 against specs/operator-nfr.md v0.4.1 (status: reviewed)".
- Yaml `spec` block line 519: `spec_version: "0.4.1"`.
- `specs/operator-nfr.md` front-matter line 11: `version: 0.4.1`. Line 9: `status: reviewed`. Line 14: `last-updated: 2026-04-25`.

All four reference points match exactly. **No stale-version flag.**

---

## 4. Findings

| # | Finding | Severity | Lane | Justification |
|---|---|---|---|---|
| 1 | §8 exit-code count described as "23 codes" in the `on-error.taxonomy` bead title and description, but the §8 table has 24 numbered rows (codes 0..23 inclusive, where 0=success). The bead description body enumerates all 24 entries correctly ("23 codes (0=success, 1=generic-failure, …, 23=orchestrator-agent-unavailable)") — only the "23-code" label is a count-shorthand. Same shorthand appears in pilot §1, §6, §9, and the yaml top-comment (consistently treated as "23 non-zero failure codes" with code 0 carried alongside). | MINOR | local | Internally consistent shorthand; coverage is complete. Optional editorial pass could clarify "24-row table covering codes 0..23 (1 success + 23 failure)" to remove the off-by-one optical illusion. NOT a coverage gap; flagged as informational. |
| 2 | ON-013b is referenced in spec body twice (lines 306, 520) as a forward reference ("when ON-013b lands"; "per ON-013b daemon-instance handshake") but has no §4 header in v0.4.1. Pilot acknowledges this on `on-050` ("verify daemon-instance handshake per ON-013b (when ON-013b lands; for MVH accept any daemon)") and treats it as a future-revision placeholder. | MINOR | local | Forward references to unminted IDs are a spec-hygiene observation, not a coverage gap. Pilot correctly does NOT mint a phantom `on-013b` bead. Recorded for ON spec author's r2 attention if ON-013b is intended to land in the next revision. |

**Total findings: 2 MINOR. 0 BLOCKER, 0 MAJOR.**

- Missed IDs: **0**
- Phantom IDs: **0** (every `req: ON-NNN` tag in the yaml maps to a real spec heading; ON-013b correctly NOT minted)
- Tally inconsistencies: **0** (pilot §1, §9, yaml top-comment all reconcile)
- Stale-version flags: **0** (v0.4.1 / status `reviewed` / last-updated 2026-04-25 confirmed in all 4 reference points)

---

## 5. Reviewer notes (informational, not findings)

- The STATUS.md baseline of "61 active §4 reqs" is wrong; pilot's count of 60 is correct (F-pilot-ON-1 records this as bookkeeping drift, NOT a discipline-patch candidate). STATUS.md correction is the appropriate response, not a pilot edit.
- Spec body cites WM normatively at six locations (ON-022 redaction-sink session-log; ON-024 sandbox; ON-027 step 6 workspace unlock; ON-030a marker write; ON-053 forensic file write; ON-INV-003 sensor) without WM in `depends-on`. Per discipline §3.2 + F-pilot-EV-3 baseline, these surface as INFORMATIONAL findings (NOT forward-deferred edges) — pilot correctly omits `forward:wm-*` placeholders. F-pilot-ON-6 records this as the inverse-direction sibling of F-pilot-PL-4. Out of scope for coverage review (it's a spec-author concern, not a coverage gap).
- ON-INV-006 has no test-infra harness coverage in the 11 listed `on-test.*` beads — pilot §4 explicitly notes "reviewer-enforced pending mechanical lint" for this invariant. Acceptable per ON's own §10.2 sensor design. ON-ENV-001 (envelope) likewise has no test-infra entry; envelope reqs are documentary in foundation-cross-cutting specs per AR-052.
- Coverage scope of this review excludes decomposition-quality (§3.2 reviewer) and reference-edge correctness (§3.3 reviewer). The F-pilot-ON-5 "ON-027 split DEVIATES from PL collapse precedent" finding is the highest-stakes class-lane signal but is a decomposition-quality concern, not a coverage concern; routed to the decomposition reviewer per protocol §3.

---

## 6. Verdict

**Coverage: CLEAN.** No BLOCKER / MAJOR findings; 2 MINOR informational notes that do not alter coverage status. Pilot is coverage-complete against `specs/operator-nfr.md` v0.4.1 and may proceed to decomposition + references review and synthesis without coverage-side patches.
