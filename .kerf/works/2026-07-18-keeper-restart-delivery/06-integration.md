# Pass 6 — Integration / cross-reference consistency review

Codename: `2026-07-18-keeper-restart-delivery` · pass 6 (integration)
Reviewer: independent (did not author the drafts). Adversarial cross-reference sweep across
all four amendment drafts + the changelog against the existing target specs and the
surrounding system specs.

Drafts under review (`05-spec-drafts/`): `session-keeper-amendment.md`,
`agent-input-amendment.md`, `scenario-harness-amendment.md`,
`park-resume-protocol-amendment.md`, plus `05-changelog.md`.

---

## 1. ID-collision / next-free-ID checks

| Spec | Amendment claims highest existing | Verified in target spec | New IDs | Verdict |
|---|---|---|---|---|
| session-keeper | SK-021 (§4.10), SK-INV-005 | SK-021 at §4.10; SK-INV-005 last invariant (`specs/session-keeper.md:242,276`) | SK-022…SK-037 + SK-INV-006 | PASS — next free, no collision, none renumbered |
| agent-input | AIS-018 (§4.9) | AIS-018 at §4.9 (`specs/agent-input.md:239`); AIS-007/008 RETIRED (not reused) | AIS-019, AIS-020 | PASS — next free |
| scenario-harness | SH-034 (§4.13), SH-INV-005 | SH-034 highest; SH-INV-005 last invariant | SH-035, SH-036 | PASS — next free |
| park-resume | contract-shape, no prefix | confirmed no requirement-prefix; §9 is prose | 0 new IDs (prose §9.5) | PASS |

All new requirement IDs are the correct sequential gap-fillers. No existing ID is
renumbered, reused, or retired.

## 2. Anchor-section fit checks

| Anchor referenced | Exists in target? | Fit | Verdict |
|---|---|---|---|
| session-keeper §4.11–§4.15 (new, after §4.10) | §4.10 is the last §4.x ("Deferred: keeper input migration") | Sequential append; clean | PASS |
| session-keeper §5 (SK-INV-006 after SK-INV-005) | §5 Invariants ends at SK-INV-005 | Clean append | PASS |
| agent-input §4.10 (new, after §4.9) | §4.9 is last §4.x (AIS-018) | Clean append | PASS |
| scenario-harness §4.14 (new, after §4.13) | §4.13 = SH-034 is last §4.x | Clean append | PASS |
| park-resume §9.5 "after Teardown-as-transition subsection of §9" | §9 = "Workflow sequences"; subsections are letter-labeled (W1/W3/W4) + "Teardown-as-transition", NOT decimal-numbered | Positionally fine; numbering style mismatch (see Finding F2) | PASS-with-note |

## 3. Surrounding-spec contradiction hunt (the 5 required verifications)

- **(a) Zero-threshold-change guardrail (NG1/SC-9).** PASS. SK-016 (`specs/session-keeper.md:204`)
  — "Gate ladder, thresholds, and bands are preserved" — is neither edited nor contradicted.
  The amendment's non-change note and SK-028 both restate SK-016 in force verbatim. No draft
  touches a band/gate/threshold; every new requirement adds a delivery channel, template,
  config surface, or situational read only. The operator-pinned bands owned by
  [operator-nfr.md §4.13 ON-059] are untouched.
- **(b) SK-022 "no `--wake`" vs an existing wake requirement.** PASS — vacuously. There is NO
  `wake`/`--wake` requirement anywhere in `specs/session-keeper.md` (grep: zero hits). `--wake`
  is a comms CLI feature, not a keeper-spec contract, so SK-022's prohibition cannot collide
  with a keeper requirement.
- **(c) SK-INV-006 consistency with existing SK invariants.** PASS. SK-INV-006 (leader
  delivery totality) is explicitly framed as the delivery-totality peer of SK-INV-005
  (bounded liveness / never-silence). SK-INV-006 is narrower (leader-only) and additive; it
  does not weaken or overlap SK-INV-001…SK-INV-005.
- **(d) Retry-Enter loop (SK-025, NG3/hk-89g) vs the K1 comms path.** PASS — no contradiction.
  SK-024's decision table makes {comms, terminal-fallback} mutually exclusive per tick. The
  retry-Enter loop is preserved ONLY on the terminal-fallback branch (SK-025); the comms
  branch (SK-022) issues no pane write at all, so the retry loop is simply not on that path.
- **(e) restart-now upholds SK-INV-001 (never `/clear` without confirmed handoff).** PASS.
  SK-029 explicitly requires the restart-now path to uphold SK-INV-001 and enforce the same
  handoff-write-done-precedes-clear ordering (NG5), not bypass it. Consistent with SK-INV-001
  (`specs/session-keeper.md:252`).

## 4. Surrounding-system-spec cross-reference validity

- **event-model `agent_message`.** The drafts reference `agent_message` generically (the
  durable comms event delivered via `CommsBus.EmitAgentMessage`, per
  `specs/park-resume-protocol.md:75`). This event is REAL. **Note:** it is NOT defined in an
  event-model §8.10 taxonomy row — §8.10 is "Queue lifecycle." A stale reference in
  event-model itself (`event-model.md:370`) mis-cites "§8.10's `agent_message` family," but
  that is a pre-existing event-model defect, NOT introduced or relied upon by these drafts.
  Crucially, **none of the four drafts cite an event-model section number for `agent_message`**,
  so there is no broken §-reference here. AIS-019 asserts `agent_message` is "F-class"; the
  authoritative owner is the agent-comms contract (a skill, not a `specs/` file), and
  flywheel-motion classifies the `agent_message`/presence/heartbeat family as low-priority
  "chatter." The F-class annotation is not load-bearing to any requirement and is not a
  cross-reference; see Finding F4 (minor).
- **claude-hook-bridge (SK-037 / §9.1 dep add).** `specs/claude-hook-bridge.md` exists. SK-037
  names it as an out-of-scope external dependency (keystroke/operator-present signal). Target
  is real; the reference is accurate. See Finding F1 for the Depends-on-vs-co-reference
  placement.
- **crew-handoff-schema (declared NOT amended).** PASS. The park-resume amendment's rationale
  is sound: crew-handoff-schema.md owns the mission-file byte-contract (captain-authored
  frontmatter); the crew keeper message is keeper config (`keeper.warn_messages`), not
  frontmatter. Reviewed `specs/crew-handoff-schema.md` — nothing there references the keeper
  nudge, so leaving it unamended creates no dangling or now-inconsistent content.
- **presence substrate (SK-023 / AIS-020).** Both drafts cite `presence.TTL = 120s`,
  `presence.ComputePresenceRegistry` + `GetPresenceState`, and a 60s follower refresh beat.
  Verified against code: `internal/presence/presence.go:48` — `const TTL = 120 * time.Second`;
  the file comment (`presence.go:45`) records "C = 60s refresh cadence, TTL = ~2× = 120s"; the
  TTL matrix test confirms join-only → Online (grounding the "bare `comms join` also shows
  Online → necessary-but-not-sufficient" limitation stated identically in both SK-023 and
  AIS-020). Both function names exist. PASS.
- **`self_service.crews_enabled` (park-resume §9.5 item 2).** Referenced as an "existing"
  default-off flag. Verified real: `internal/daemon/projectconfig_hk9kgf_test.go` exercises
  `self_service.crews_enabled` → `SelfServiceEnabled`. PASS.
- **`cmd/harmonik-twin-session` (SH-035).** Verified real: `cmd/harmonik-twin-session/` exists
  alongside `harmonik-twin-claude`. PASS.

## 5. Bidirectional cross-reference table

| From → To | Target real+accurate? | Reciprocal? | Verdict |
|---|---|---|---|
| SK-022 → agent-input §4.10 AIS-019 | yes | AIS-019 → session-keeper §4.11 SK-022 | PASS (both directions) |
| SK-023 → agent-input §4.10 AIS-020 | yes | AIS-020 → session-keeper §4.11 SK-023 | PASS |
| SK-024 → park-resume §9.5 | yes (§9.5 is the new section) | park-resume §9.5 → SK-032, SK-029 | PASS |
| SK-032 / SK-033 → `restartNowCmdToken` / `containsRestartNowCmd` | implementation symbols, design-grounded | n/a | PASS |
| park-resume §9.5 → session-keeper §4.14 SK-032, §4.13 SK-029 | yes | SK-024 → park-resume §9.5 | PASS |
| SH-035 → session-keeper §10.2 | §10.2 exists (Test-surface obligations) | session-keeper NOT amended to acknowledge SH-035/036 | PASS-with-note (Finding F3, one-way) |
| SH-036 → session-keeper §5 SK-INV-006 | yes (new invariant) | n/a | PASS |
| SH-036 → architecture.md §4.6 | yes (cited elsewhere in SH spec) | n/a | PASS |
| SK-037 → claude-hook-bridge.md | file exists | n/a (out-of-scope external) | PASS |
| SK-014 NOTE → agent-input AIS-018 (pre-existing) | unchanged, still valid | n/a | PASS |

No draft links to removed/renamed content. No orphaned reference. The only asymmetry is
SH-035's one-way pointer (F3).

## 6. Terminology consistency across drafts

Scanned the four drafts for the load-bearing terms. Concepts are named consistently:
`presence-Online` (11; one incidental "presence Online"), `restart-now` (16; zero
`restart_now`), `self-restart` (9), `terminal fallback` (10 total). The nonce is called both
"cycle nonce" and `cycle_id`, but SK-031 explicitly defines the mapping (the `cyc-<ts>-<seq>`
`cycle_id` IS the nonce), so the dual naming is intentional and non-ambiguous. Only nit:
"terminal fallback" (6) vs hyphenated "terminal-fallback" (4) — same concept, cosmetic (F5).

## 7. Changelog accuracy (`05-changelog.md`)

PASS — exact match.
- Version bumps: SK 0.2.0→0.3.0, AIS 0.1.0→0.2.0, SH 0.2.3→0.2.4, park-resume 1.1.0→1.2.0 —
  all identical between changelog, each amendment's frontmatter block, and each amendment's
  revision-history row.
- IDs: SK-022…SK-037 (16 reqs) + SK-INV-006; AIS-019, AIS-020; SH-035, SH-036; park-resume 0
  new IDs — all match the drafts exactly.
- Section homes (K1 §4.11, K2 §4.12, K3 §4.13, K4 §4.14, K5 §4.15) match the amendment.

## 8. Registry / version-bump implications

- **No `specs/_registry.yaml` edit required by this work.** The registry stores only
  `spec-id`, `reserved` date, and `status` per prefix — NOT versions. All three affected
  prefixes (SK, AIS, SH) already have entries; no new prefix is introduced; no version field
  lives in the registry. Park-resume has no requirement-prefix, so no registry surface at all.
- **Pre-existing, out-of-scope drift (informational only):** the registry lists
  `SH: status: draft` (line 26), but `specs/scenario-harness.md` frontmatter status is
  `reviewed` (and the amendment keeps it `reviewed`). This registry-vs-spec status drift
  predates this work and is not caused by these drafts. Flagging for the caller's awareness;
  fixing it is not part of this kerf.

---

## 9. Findings and proposed resolutions

### F1 (MODERATE) — session-keeper §9.1 "Depends on" additions are not reflected in frontmatter, and would create a circular dependency if they were.

The session-keeper amendment adds `agent-input.md` and `claude-hook-bridge.md` to the §9.1
**"Depends on"** prose list, but its "Frontmatter" section bumps only `version` +
`last-updated` — it does NOT add either spec to the `depends-on:` YAML block. Today the
session-keeper frontmatter `depends-on:` (4 specs) and its §9.1 prose list (same 4 specs)
agree; this amendment breaks that correspondence.

Worse, the natural "fix" (add them to the frontmatter) would create a **circular hard
dependency**: `agent-input.md`'s frontmatter already `depends-on: session-keeper`, so adding
`session-keeper depends-on: agent-input` inverts an existing edge. And `claude-hook-bridge` is
named by SK-037 explicitly as an *out-of-scope external dependency the keeper does not
implement* — that is a co-reference, not a build/contract dependency.

**Proposed resolution (edit `session-keeper-amendment.md`):** Move both entries out of the
§9.1 "Depends on" amendment and into a **§9.3 "Co-references (read-only consumption)"**
amendment instead. Rationale: (i) AIS-019/AIS-020 *document the keeper's own consumption* of
the comms/presence surfaces — read-only consumption is exactly what §9.3 is for, and it
avoids inverting the agent-input→session-keeper edge; (ii) claude-hook-bridge is an
out-of-scope external dep (SK-037), which is informative, not a hard depends-on. This keeps
frontmatter `depends-on:` (4 specs) consistent with §9.1 (4 specs) and introduces no cycle.
If the authors instead intend a genuine mutual dependency, the alternative is to add both to
the frontmatter `depends-on:` AND state the accepted agent-input↔session-keeper cycle
explicitly — but the co-reference placement is cleaner and is recommended.

### F2 (MINOR) — park-resume "§9.5" numbering vs the letter-labeled §9 subsections.

`specs/park-resume-protocol.md` §9 ("Workflow sequences") has NO decimal subsections; its
children are letter-labeled (W1/W3/W4) plus "Teardown-as-transition." The amendment (and
SK-024, SK-032, the changelog) all call the new section "§9.5," which implies §9.1–§9.4 exist.
They do not.

**Proposed resolution:** Acceptable to keep "§9.5" as the first decimal subsection under §9
(it reads as "the fifth block in §9," which is literally true after W1/W3/W4/Teardown), OR
retitle it as a labeled subsection consistent with the W-series. Recommend keeping "§9.5" for
minimal churn since three other drafts already cite it by that number — but the authors should
consciously confirm the number rather than let it read as a dangling forward-implication.
No cross-reference breaks either way (all citers use "§9.5").

### F3 (MINOR) — SH-035's pointer to session-keeper §10.2 is one-directional.

SH-035 assigns ownership of the keeper pane/timing/handoff/operator-typing test coverage to
"the keeper subsystem ([session-keeper.md §10.2])," but the session-keeper amendment does NOT
touch §10.2 and adds no reciprocal co-reference to scenario-harness SH-035/SH-036. §10.2 does
exist and topically owns keeper test-surface obligations (it already names the keeper
integration tests and says "Cite [scenario-harness.md] cadence tiers once available"), so the
reference is accurate and not broken — but the K6 session-twin integration tier
(`harmonik-twin-session`) is not enumerated there.

**Proposed resolution (optional, recommended for symmetry):** Add a one-line note to the
session-keeper amendment's §10.2 (or its §9.3 co-references) acknowledging that the keeper
pane/timing/handoff/operator-typing coverage rides the session-twin integration tier per
[scenario-harness.md §4.14 SH-035/SH-036], with at most one wire-observable comms-fallback
scenario. This is not required for correctness (the design deliberately reuses the keeper's
*existing* integration tier, so no new session-keeper requirement is needed), but it closes
the one-way reference.

### F4 (MINOR / verify) — AIS-019's "`agent_message` (F-class)" annotation.

`agent_message` is a real event, but its durability class is owned by the agent-comms contract
(not a `specs/` file), and flywheel-motion treats the `agent_message`/presence/heartbeat family
as low-priority "chatter." The "(F-class)" parenthetical is not load-bearing to any requirement
and is not a cross-reference.

**Proposed resolution:** Confirm `agent_message` is actually fsync-boundary (F) per the
agent-comms delivery guarantee before shipping; if it is best-effort, drop the "(F-class)"
parenthetical from AIS-019 to avoid asserting a durability class the owner does not guarantee.
Low priority — does not affect any requirement's semantics.

### F5 (COSMETIC) — hyphenation of "terminal fallback" vs "terminal-fallback."

Both forms appear across the drafts. Harmless; normalize to one form (recommend the noun
"terminal fallback" unhyphenated, "terminal-fallback path" hyphenated as an adjective) if a
copy pass is done. Not required.

---

## 10. Final assessment

**COHERENT-WITH-FIXES.**

The five substantive contradiction checks (a–e) all PASS: zero threshold changes, no wake
collision, SK-INV-006 consistent with the existing invariant set, the retry-Enter loop and the
comms path are cleanly disjoint, and restart-now upholds SK-INV-001. All new IDs are correct
next-free gap-fillers with no collisions; all anchor sections fit; the changelog is exact; no
registry edit is required; every cited external target (claude-hook-bridge, presence code,
crews_enabled, harmonik-twin-session) is real and accurate. crew-handoff-schema is correctly
left unamended with nothing dangling.

Only one finding rises above cosmetic (F1, a frontmatter/§9.1 mismatch that would become a
circular dependency if "fixed" naively). None blocks the design; all are local edits to the
drafts.

**Ordered edit list (apply to the drafts, not the live specs):**

1. **F1 — `session-keeper-amendment.md`:** relocate the `agent-input.md §4.10 AIS-019/AIS-020`
   and `claude-hook-bridge.md` entries from the §9.1 "Depends on" amendment into a §9.3
   "Co-references (read-only consumption)" amendment; leave the frontmatter `depends-on:` YAML
   unchanged (this is the no-cycle, frontmatter-consistent resolution).
2. **F3 — `session-keeper-amendment.md`:** add a one-line §10.2 (or §9.3) co-reference back to
   `scenario-harness.md §4.14 SH-035/SH-036` recording that the keeper's pane/timing/handoff/
   operator-typing coverage rides the session-twin integration tier (symmetry with SH-035).
3. **F2 — confirm** the "§9.5" number in `park-resume-protocol-amendment.md` is intended as the
   first decimal subsection under a letter-labeled §9 (no edit strictly required; conscious
   confirmation only, since SK-024/SK-032/changelog all cite "§9.5").
4. **F4 — `agent-input-amendment.md`:** verify `agent_message` is F-class per agent-comms; drop
   the "(F-class)" parenthetical from AIS-019 if it is not.
5. **F5 — optional copy pass:** normalize "terminal fallback"/"terminal-fallback."

Items 1 and 2 are the ones worth applying before finalize; 3–5 are confirm-or-optional.

---

## Resolution log (fixes applied by xray, 2026-07-18)

All five findings from the integration review were applied to the Pass-5 drafts:

- **F1 (moderate) — RESOLVED.** `session-keeper-amendment.md`: the agent-input.md +
  claude-hook-bridge.md cross-refs were moved out of the §9.1 "Depends on" block into
  **§9.3 Co-references** (verified session-keeper.md §9 = 9.1 Depends on / 9.2 Reverse deps /
  9.3 Co-references). Frontmatter `depends-on` is left unchanged, so no agent-input↔session-keeper
  dependency cycle is introduced. Revision-history entry updated to match.
- **F2 (minor) — RESOLVED.** `park-resume-protocol-amendment.md`: the new content is a named
  §9 subsection **"Crew keeper-message disposition (K7 — DEFERRED)"** (park-resume §9 uses
  named subsections W1/W3/W4/Teardown, no decimal numbering) — the earlier "§9.5" label was
  dropped. The two references in `session-keeper-amendment.md` and the `05-changelog.md`
  pointer were updated to the named-subsection form.
- **F3 (minor) — RESOLVED.** A reciprocal co-ref to **[scenario-harness.md §10.3 SH-035]** was
  added to session-keeper's §9.3 Co-references block (symmetry with SH-035's ref to this spec).
- **F4 (minor) — RESOLVED.** `agent-input-amendment.md` AIS-019: the "(F-class)" parenthetical
  was dropped; the requirement now states "a normal durable `agent_message`" without asserting
  a specific event-model durability class (that classification is event-model.md's to own).
- **F5 (cosmetic) — RESOLVED.** Standalone-noun "terminal-fallback" normalized to "terminal
  fallback" in SK-INV-006's heading; attributive compound forms (e.g. "terminal-fallback path")
  left hyphenated.

**Post-fix assessment: COHERENT.** No draft edits remain outstanding. The pre-existing
`_registry.yaml` SH status drift (draft vs reviewed) is noted as out of scope and left for a
separate cleanup.
