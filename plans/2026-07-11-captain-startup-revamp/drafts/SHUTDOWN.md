<!-- DRAFT — companion edit note for the live .claude/skills/captain/SHUTDOWN.md
     (startup-doc revamp Stage 2, per 02-cutover §2.4 precondition 5 + step 2.2).
     SHUTDOWN.md SURVIVES the revamp — boot is replaced by `harmonik agent brief`, but session
     end still needs a runbook, and the live one is mostly sound (banked-commit deploy,
     stand-down discipline, PINs, glance check, gotchas all stay). This note is the edit set,
     not a full rewrite: §1 is new text to insert at the top; §2 re-points every STARTUP.md /
     old-model reference; §3 records what deliberately does NOT change. Apply in the SAME
     landing as the captain-doc swap (cutover step 2.2), together with the
     `cmd/harmonik/assets/skills/captain/` mirror of the same file. -->

# SHUTDOWN.md — revamp edit note (tier-2 discipline confirmed + STARTUP re-point)

## 1. New lead section — the principle (insert after the title, before the posture table)

> **PRINCIPLE — leave the field in a state the next boot can trust.** The next captain boots
> from `harmonik agent brief` (which embeds `HANDOFF-captain.md`) plus ONE `harmonik digest`
> pass. The handoff is a CLAIM the digest overrides; the tier-2 docs are what the wake steps
> actually read. So durable state comes FIRST, the handoff comes LAST:
>
> 1. **Update tier-2 BEFORE writing any handoff** — in this order, committed immediately,
>    specific paths only (never `git add -A`):
>    - `lanes.json` — the authoritative lane registry; update FIRST on any lane change.
>    - `.harmonik/context/captain-lanes.md` — the ≤60-line snapshot, rewritten in place to
>      match shutdown state (stood-down lanes removed, blockers named).
>    - `.harmonik/context/direction-log.md` — if this session issued or relayed a direction
>      change, its entry must already exist (forced-write happens AT the change, not at
>      shutdown); shutdown only VERIFIES it is present, newest-first, with `expires:`.
> 2. **THEN write `HANDOFF-captain.md`** (the captain's file — NOT the generic `HANDOFF.md`):
>    a short claim that POINTS at tier-2 state rather than duplicating it. Keep its standing
>    "NOT A BOOT DOC" banner + the KEEPER-IDENTITY block + the KEEPER nonce.
>
> Why this order: any "state" captured only in the handoff dies with the handoff — the wake
> steps read tier-2, and the digest re-derives the live layer. A handoff that disagrees with
> tier-2 is a bug in the shutdown, not an alternate record.
>
> PINs (operator-gated items) follow the chain of communication: record them in the handoff
> AND send them to the **admiral** over comms — the admiral surfaces pending decisions to the
> operator when the operator is present. A PIN that only sits in a file is the failure mode.

Note: the live doc's Step 5 → Step 6 ordering already had state-capture before the handoff —
this section makes the order an explicit principle and extends it to lanes.json +
direction-log (previously unmentioned in SHUTDOWN.md).

## 2. Re-point away from STARTUP.md (mechanical replacements in the live doc)

| Live reference | Replace with |
|---|---|
| Header: "symmetric counterpart to STARTUP.md — boot builds the fleet" | "the boot counterpart is `harmonik agent brief` — its output IS the boot context; shutdown writes the state the brief + wake steps read" |
| Posture table: "The next captain runs STARTUP.md" | "the next captain boots via `harmonik agent brief --wake fresh`" |
| ON-059 row: "re-ground via STARTUP.md Steps 2–6 … (STARTUP.md re-derives it)" | "re-ground via `harmonik agent brief --wake keeper-restart` + ONE `harmonik digest` pass (the digest re-derives live state — do not snapshot queue/daemon state in the handoff body)" |
| Step 1: "(same as STARTUP.md Step 2, abbreviated)" | "(ONE `harmonik digest` pass)" |
| Step 2 announce comment: "STARTUP.md Step 0 identity guard" | "captain operating.md identity guard" |
| Step 5a: "STARTUP.md Step 0b READS captain-lanes.md at boot" | "the captain manifest wake step reads lanes.json + captain-lanes.md" (and add: lanes.json FIRST, commit immediately) |
| Step 6: "Write HANDOFF.md" + template header "Load …/captain/STARTUP.md FIRST … then this" | "Write `HANDOFF-captain.md`"; template header = the standing "NOT A BOOT DOC" banner (see drafts/HANDOFF-captain.md) — the load-STARTUP-FIRST line dies |
| Step 6 discipline: "flag it as stale input (STARTUP.md Step 2 wins)" | "flag it as a claim (`harmonik digest` wins)" |
| Step 6 template "# Deploy procedure … otherwise reference STARTUP.md" | "…otherwise reference `docs/daemon-redeploy.md`" |
| Glance check #1: "restart the daemon yourself (see STARTUP.md §2.1)" | "…(see `docs/daemon-redeploy.md` / captain SKILL.md §Errors & edges)" |
| Glance check #6: "the file STARTUP.md Step 0b reads — NOT SKILL.md §A" | "the file the manifest wake step reads — lanes.json is the registry, this is the snapshot" |
| WARN gotcha: "see STARTUP.md 'On-WARN procedure'" and "STARTUP.md Step 6 'Keeper arming'" + the hardcoded `--warn-abs-tokens 200000 --act-abs-tokens 215000` | point both at the **keeper skill** (owns the on-WARN procedure, arming, and the band values — standing no-hardcoded-thresholds rule; drop the literal numbers) |
| References: ".claude/skills/captain/STARTUP.md — the boot counterpart" | "`harmonik agent brief` + `.harmonik/agents/captain/operating.md` — the boot counterpart" |

## 3. Deliberately unchanged (do not re-litigate)

- Steps 2–4 (banked-commit deploy SOP, complete-lane stand-down, PIN format) and the
  load-bearing gotchas (ff-after-push, no-redeploy-mid-run, never-exit-on-WARN, keeper
  full-cycle safety) survive as-is — operational content, still correct.
- The never-exit-on-WARN rule keeps its teeth; only its pointer moves (keeper skill).
- Fail fast and loud still governs: shutdown never auto-repairs a surprise (zombie crew,
  diverged main) silently — surface it in the handoff and to the admiral.
