<!-- DRAFT — proposed replacement for .harmonik/context/direction-log.md
     (startup-doc revamp Stage 2 companion, per 02-cutover §0.3 / step 2.3 + 03-operator-decisions.md).
     Compaction applied per the file's OWN header contract (cap / newest-first / LAPSE):
     - STRUCK, expired per the LAPSE rule: 07-09 ~02:33Z v0.5.0-quiesce (expired 07-11; superseded
       by the re-stand — 5 lanes running) and 07-08 ~00:20Z pi re-scope (expired 07-11; core goal
       PROVEN + CLOSED, pi-provider-switch LANDED hk-m6uu2.*).
     - STRUCK, superseded/consumed: 07-10 ~11:5xZ TRACK-A-only (explicitly retired by the 07-11
       ~01:35Z parallel directive); 07-11 ~01:25Z GATE-0-sole-gate and 07-11 ~01:54Z GATE-0
       verdict (both consumed — flagship epic hk-hcrvb CLOSED 07-11 03:10Z, fix deployed
       hk-j0p1r / PR #30, prod canary green on 59089968; stdin forensics live in bead hk-y20d2).
       Git is the archive; nothing is annotated in place.
     - STRUCK, consumed (verified ~07:45Z): 07-11 ~00:40Z watch-restored + hk-vdqe2 — hk-vdqe2
       is CLOSED (done) with the operator's HARD MERGE GATE (e2e twin repro + permanent
       regression scenario) recorded verbatim IN THE BEAD (`br show hk-vdqe2`), which is its
       durable home; watch-restored is plain fleet state (captain-lanes.md), and the
       watch/watchdog/flywheel earn-their-keep question is queued RESEARCH per
       03-operator-decisions.md Q7, not a direction entry.
     - Ordering fixed to newest-first (live file had the newest entry at the BOTTOM, under
       expired ones, so the "newest RETURN-PATH is ground truth" marker pointed at the wrong entry).
     Banner removed on deploy; deploys in the SAME action as captain-lanes.md + lanes.json
     (cutover step 2.3), committed immediately, specific paths only. -->

<!-- TIER: 2 (sequencing intent across direction changes)
     LOADED BY: admiral + captain, via the manifest wake step (`harmonik agent brief` reads
                project.yaml -> lanes.json + captain-lanes.md -> this file), BEFORE acting.
     OWNER: admiral. ONE entry per direction CHANGE. Crews never write here.

     PRINCIPLE: this file holds the ONE thing no other doc holds — WHY we paused X for Y and in
     what ORDER we resume. It is sequencing INTENT, never status: if an entry no longer changes
     what the reader does next, it does not belong here. Git is the archive. Guardrails that
     keep the principle true:
     - Newest-first · ~3-5 lines/entry · cap ~10 entries / ~60 lines. DELETE, don't annotate:
       superseded/consumed/expired entries are REMOVED, not marked "SUPERSEDED". No archive.
     - Fields per entry: WHAT / WHY / ORDER / RETURN-PATH / expires: (RFC3339).
     - ON EXPIRY the DEFAULT is LAPSE -> revert to the standing autonomous posture, NEVER a
       hold. The admiral audit OWNS an expired-but-present entry: re-confirm with the operator
       or strike it.
     - No 4th priority list: the operator priority order lives in lanes.json +
       admiral-initiatives.md; entries here POINT at it.
     - Full forced-write/forced-read discipline: .harmonik/context/CLAUDE.md. -->

# Direction log — sequencing intent across direction changes

> The NEWEST entry's RETURN-PATH is ground truth for sequencing intent — this is what a fresh
> /clear destroys. Factual claims are still claims: `harmonik digest` overrides them.

## 2026-07-11 ~06:2xZ — operator (direct to captain): CODEX OPTION B KILLED (hard no, never revisit) → piter to app-server research · expires: 2026-07-13T00:00:00Z
WHAT: Option B (daemon re-invokes `codex exec resume` per wake) is PERMANENTLY DEAD — do not
      discuss or re-propose. piter's Codex-as-crew lane (epic hk-q3ovr) pivots to a FULL kerf
      work on the resident `codex app-server` path (resident orchestrator; could it retire the keeper).
WHY:  ZFC grounds (B puts cognition in the framework); the real prize is a resident
      long-context orchestrator. Decide by research before any implementation.
ORDER: piter drives the kerf work (problem-space → research → design → ratification, admiral
      surfaces); NO implementation before ratification. Option-B artifacts are dead context.
RETURN-PATH: piter owns a live codex-app-server kerf work; hk-l63b9 stays OPEN-PARKED until the
      ratified design names the path; ratification is a pending operator decision (admiral surfaces).

## 2026-07-11 ~01:35Z — operator (via admiral, events 019f4ed0/019f4ed3): PARALLEL 5-lane posture, IRON RULE no-fleet-freeze · expires: 2026-07-13T00:00:00Z
WHAT: Single-focus flagship posture RETIRED → staff the operator-set priority order IN PARALLEL,
      file-disjoint, every non-conflicting slot full. IRON RULE (emphatic): NEVER hold the whole
      pipeline for one bead/initiative — review/assessor gates are per-ITEM, never a fleet freeze.
WHY:  max throughput across the real initiatives; capacity idled under the single-lane posture.
ORDER: the priority table lives in admiral-initiatives.md + lanes.json (no copy here). gb-mbp
      live re-enable stays OPERATOR-HELD (remote lane tests locally). internal/daemon collision:
      stilgar (hk-ih5k6) has right-of-way; hawat holds workloop.go/reversetunnel.go beads until it lands.
RETURN-PATH: all 5 lanes staffed + verified (snapshot: captain-lanes.md); pending operator
      decisions ride with the admiral, surfaced when the operator is present.
