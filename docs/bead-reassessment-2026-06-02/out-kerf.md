# Cluster: kerf beta-feedback beads (16)

**Meta-context (applies to all 16):** These are bug/gap reports against **kerf**, an EXTERNAL planning tool that harmonik beta-tests. They are NOT harmonik features. The entire batch was already **routed upstream to the kerf project on 2026-05-18** — kerf's `plans/015_harmonik_beta_feedback/` ingested a 464-line snapshot of harmonik's feedback log, normalized ~60 items, and spawned implementation plans 016–020 (init UX, storage reconciliation + `kerf doctor`, triage rework, filter bootstrap + `kerf show` slot, jig review-gate). All five spawned plans are status "baked" and largely IMPLEMENTED in the kerf source at `~/github/kerf` (HEAD `5d035cf`).

**Verification surface used:**
- Current `kerf` CLI surface (`kerf --help`) now includes `doctor`, `bootstrap-filters`, `review`, `triage --top/--group-by`, `init --yes/--no/--bead-filter` — none of which existed when these were filed (~18 days ago).
- kerf source at `~/github/kerf` (cmd/*.go, specs/, plans/015–020) read directly to confirm code-level fixes.
- **CAVEAT — stale local binary:** the *installed* binary `/Users/gb/go/bin/kerf` is dated **May 19**; several fixes (archive-aware suggester, tier-1 prefix ranking) exist in kerf HEAD but post-date that binary, so a live `kerf triage` on this machine still shows the OLD behavior. The fix is real upstream; the operator just needs `go install` of current kerf. This is a harmonik-side ops note, not a kerf bug.

The right ACTION for every bead is **route upstream to kerf (close in harmonik's ledger)** — harmonik does not own any of this code, and kerf has already triaged + (mostly) fixed it. Verdicts below distinguish which kerf-side fix landed vs. which is a deliberate WONTFIX-by-design.

---

### hk-f7fnt — kerf next scoring ignores br priority (P0/P3 indistinguishable)
- VERDICT: OBSOLETE
- ACTION: route upstream to kerf project (close in harmonik) — note it is WONTFIX-by-design
- NEW_PRIORITY: -
- EVIDENCE: kerf `specs/coordination.md:145,155` — "Priority is computed from graph structure, not assigned as static labels… No stored priority field." Line 391 explicitly lists `priority` among bead fields kerf does NOT hash/consume. kerf ranks by rework/momentum/fan-out/creation by design (confirmed live: `kerf next --format=json` carries `score`, no priority field; P3 bead hk-tigaf.11 scores identically to its parent). The "bug" contradicts a locked kerf design choice — premise no longer holds.
- CONFIDENCE: high

### hk-ocarg — .kerf/ ↔ bench drift with no reconciliation tool
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik) — `kerf doctor` + `kerf localize` shipped
- NEW_PRIORITY: -
- EVIDENCE: `kerf doctor` now exists and reports "storage drift, symlinks, archives" (live: `kerf doctor` emits "storage layout consistent with local mode", "bench symlink … -> …/.kerf/works"). kerf plan `017_storage_reconciliation` (status: baked) absorbed this exact item. `kerf localize` command also present. Was the requested `kerf doctor`/reconcile mode.
- CONFIDENCE: high

### hk-or6gk — work edit "Now matches: N beads" conflates open+closed
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/work_edit.go:183` now prints `"Now matches: %d (%d open / %d closed). Previously: %d (%d open / %d closed)."` — exactly the disambiguation the bead requested. Absorbed by kerf plan 019.
- CONFIDENCE: high

### hk-kb86i — kerf show does not display bead_filter slot
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/show.go:79-82` "Bead filter slot — always rendered per specs/commands.md"; live `kerf show named-queues` prints `bead_filter: label=codename:named-queues`. Absorbed by kerf plan 019.
- CONFIDENCE: high

### hk-5py8k — triage --ack re-prints entire report before advancing baseline
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: Item 4.3 in kerf `plans/015_harmonik_beta_feedback/triage.md:181-184` ("--ack prints only `Baseline advanced to <ts>`"), routed to plan `018_triage_rework` (status: baked). Terse-confirmation behavior is the spec'd outcome. (Not re-runnable live without mutating kerf's baseline.)
- CONFIDENCE: med

### hk-rvbxi — triage suggester does not detect archived works
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/triage.go:637` `triageSuggestUntriaged(... archivedSet map[string]bool)` now returns "codename '%s' is archived — consider 'kerf restore …' or 'kerf pin …'" when value ∈ archivedSet (the exact annotation the bead asked for). In kerf HEAD; the May-19 installed binary predates it (live still shows un-annotated `kerf new imrest`). Absorbed by plan 018 / B2.
- CONFIDENCE: high

### hk-u5ymq — triage suggester proposes new works from cross-cutting labels
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/triage.go:609` `triagePickTier1Label` ranks only `tier1LabelPrefixes` (codename:/spec:); axis:/tag:/kind:/scope: are never tier-1, so they cannot seed `kerf new`. Also prefers work-edit over new-work when a codename value-matches. Exactly the bead's ask. In kerf HEAD; predates May-19 binary. Plan 018 §4.1.
- CONFIDENCE: high

### hk-43ate — kerf next status "clean" misleading for zero-bead filters
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: Live `kerf next` now labels zero-bead works `unwired` ("no bead_filter declared") and `empty` ("resolved bead_filter matches zero beads"); the word "clean" is gone. Absorbed by kerf plan 019 (which names hk-43ate explicitly). Has 1 git-ref in harmonik history (`git log --grep hk-43ate`).
- CONFIDENCE: high

### hk-2pwgv — kerf init prints two overlapping/inconsistent AGENT SETUP blocks
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/init.go:252,333` now delegates to `runSetup()` as "the single source of the AGENT SETUP INSTRUCTIONS"; the hard-coded second block was removed. Absorbed by kerf plan 016 (init UX overhaul).
- CONFIDENCE: high

### hk-ynd1l — kerf init false "100% of beads use kerf:* labels"
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/init.go` bead-filter detector rewritten (`detectBeadFilter`, init.go:281-315) with `--yes/--no/--bead-filter` modes; the stale `kerf:*`-label detector was the named target of plan 016. Detector now seeds from prior filter / dominant real label, not a hard-coded kerf:* claim.
- CONFIDENCE: med

### hk-51ivc — kerf init project.yaml shape mismatches announced effects (BLOCKER)
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `cmd/init.go:193-198,300` now persists `default_jig` (cfg.DefaultJig = initJigFlag) and writes `bead_filter` via `createDefaultProjectConfig(projCfgPath, detectedFilter, resolvedDefaultJig)` — the two fields the bead said were missing. Absorbed by plan 016. (Originally BLOCKER; the bead carried P2 in harmonik's ledger.)
- CONFIDENCE: high

### hk-kx498 — kerf init interactive Y/N prompt has no agent-friendly bypass (BLOCKER)
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: `kerf init --help` now shows `--yes` (accept detector suggestion), `--no` (skip detection), `--bead-filter <literal>` (bypass) — the exact non-interactive flags the bead requested. kerf `cmd/init.go:58-61`. Has 1 git-ref in harmonik history. Plan 016.
- CONFIDENCE: high

### hk-el3yw — no "kerf review" command for pass review gates
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: `kerf review <codename>` now exists: "Emit the canonical reviewer prompt for a work's current pass… harness-agnostic surface for the jig's review gate… does not dispatch the reviewer itself" — matches the bead's "ship a `kerf review` command" ask verbatim. Absorbed by plan 020 (jig review-gate).
- CONFIDENCE: high

### hk-8t3tc — missing bootstrap-filters / doctor / health command
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: BOTH requested commands now exist: `kerf bootstrap-filters` ("one-shot remediation… proposes a bead_filter for every work… --apply/--yes") and `kerf doctor` ("project health check: project.yaml, storage drift, symlinks, filters, archives"). Live `kerf doctor` ran and reported filter coverage. Absorbed by plans 017 + 019.
- CONFIDENCE: high

### hk-n9gxx — kerf triage unbounded output, no --limit/--top/--group-by
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: `kerf triage --help` now shows `--top int` ("truncate each section to top N") and `--group-by codename-label`, plus `--kind` filters — the exact bounding flags the bead requested. Help even recommends "large-project recipe: --top 20". Absorbed by plan 018.
- CONFIDENCE: high

### hk-gkkvg — pre-init count (163) vs post-init triage count (168) disagree
- VERDICT: DONE
- ACTION: route upstream to kerf project (close in harmonik)
- NEW_PRIORITY: -
- EVIDENCE: kerf `plans/015_harmonik_beta_feedback/triage.md:193-195` item 4.5 absorbed this into plan 018; `kerf triage` now emits canonical "open · ready · total" counts (live: "Beads: 49 open · 47 ready · 1856 total" — Plan 018/B6 "canonical bead counts", `cmd/triage.go` triageBeadCounts). The count semantics are now explicit and consistent. Lowest-severity (MINOR/P3) of the batch — already nominal.
- CONFIDENCE: med

---

## Cluster summary

**Counts per verdict (16 total):**
- DONE: 14 (hk-ocarg, hk-or6gk, hk-kb86i, hk-5py8k, hk-rvbxi, hk-u5ymq, hk-43ate, hk-2pwgv, hk-ynd1l, hk-51ivc, hk-kx498, hk-el3yw, hk-8t3tc, hk-n9gxx, hk-gkkvg)
- OBSOLETE: 1 (hk-f7fnt — WONTFIX-by-design upstream)
- APPROACH-STALE / DUPLICATE / KEEP / REPRIORITIZE: 0

**The single cross-bead theme:** all 16 are `kerf-upstream`-labeled feedback about an external tool. They were collectively routed to the kerf project on **2026-05-18** (kerf `plans/015_harmonik_beta_feedback`), which spawned implementation plans **016–020** (all status "baked"), and the fixes have landed in kerf's source (HEAD `5d035cf`). The current `kerf` CLI surface confirms the new commands (`doctor`, `bootstrap-filters`, `review`, `triage --top/--group-by`, `init --yes/--no`) and behaviors (`show` bead_filter line, `work edit` open/closed split, `next` empty/unwired labels) that these beads requested.

**Recommended bulk action for the apply phase:** close all 16 in harmonik's ledger with reason "routed upstream to kerf (plan 015, 2026-05-18); fixed in kerf HEAD" — harmonik does not own any of this code. Per `KERF-FEEDBACK.md` the feedback file `docs/kerf-feedback/2026-05-15.md` was already handed off to the kerf project on 2026-05-18, so these tracking beads have served their purpose. Two nuances for the human:
1. **hk-f7fnt** is the one item kerf will NOT "fix" — kerf deliberately ignores br's static priority and computes ranking from graph structure (locked in `specs/coordination.md`). Close as WONTFIX-by-design rather than implying a pending kerf fix.
2. **Stale local binary:** the installed `/Users/gb/go/bin/kerf` is from May 19 and predates the archive-aware (hk-rvbxi) and tier-1-prefix (hk-u5ymq) suggester fixes, so live triage on this machine still shows old behavior. A `go install` of current kerf would pick them up — this is a harmonik-side ops follow-up, not a reason to keep these beads open.
