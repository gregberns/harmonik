# Captain & Crew — Integration-Pass Review

Independent fresh-eyes review of the integration-pass artifacts. Reviewer is NOT the author.
Sources read in full: `01-problem-space.md`, `03-components.md`, `05-specs/c1..c4-spec.md`,
`06-integration.md`, `SPEC.md`.

---

## verdict: MINOR_GAPS

The slice is coherent and ready to decompose into tasks once two documentation back-ports land.
There are NO contradictions in the locked decisions and NO new requirements invented by SPEC.md.
The single material finding is that the **C4 spec body still describes the PRE-resolution Gap-1
attribution mechanism** (`crew list` / `Record.Epic`), which directly conflicts with the resolution
in 06/SPEC. An implementer reading only `c4-spec.md` would build the wrong (stale-prone)
attribution. This is a back-port omission, not a design defect — the right answer is already
decided in 06 §4 Gap 1.

---

## traceability

**Complete — no orphans.** All six success criteria (#1–#6 from `01-problem-space.md`, reproduced
verbatim in `SPEC.md`) map to a component and a real, existing AC. Spot-checks:

- **C3 §AC-3** (cited by #3 in both matrices) — EXISTS (`c3-spec.md:381`), and says exactly what
  the matrix claims: both surfaces (`comms send --topic status` + `br comments add <epic_id>`),
  on bead-close + ≤10-min timer + boot/drain bookends. ✓
- **C1 §AC-2** (cited by #4) — EXISTS (`c1-spec.md:347`): sibling race / duplicate close → exactly
  one emit; matches the claim. ✓
- **C4 §AC-6** (cited by #5) — EXISTS (`c4-spec.md:376`): crew keeper restart produces no
  Captain failure-surface, no spurious re-spawn. Matches. ✓
- **C1 §AC-1/§AC-5, C2 §AC-1/§AC-2, C3 §AC-1/§AC-4, C4 §AC-1/§AC-2/§AC-3** all exist and say what
  the 06 §7 and SPEC matrices claim. No bad citations found in either matrix.

**Minor traceability nit (not blocking):** `06 §7` row #4 cites "C4 §AC-3 (receive + surface)";
C4 AC-3 indeed covers receive-and-surface, but the *attribution method* it names is the stale
`crew list` lookup, not the resolved `br show --assignee` (see contract_consistency / amendment
back-port). The citation is valid; the cited text is just out of date.

---

## contract_consistency

Cross-spec byte-for-byte checks of the four named contracts:

### 1. Mission-handoff schema — INCONSISTENT (field-list citation drift)

- **06 §1 hop 2 + §3 table + SPEC.md** cite **6 fields**:
  `{schema_version, crew_name, queue, epic_id, goal, captain_name}`.
  (06:30 — `"{schema_version: 1, crew_name, queue, epic_id, goal, captain_name}"`;
  06:184 table header repeats the 6-field form; SPEC.md:139 same.)
- **c3-spec.md R-C3.1 (`:28`)** and the C3 §3.1 prose lock **5 fields**:
  `{crew_name, queue, epic_id, goal, captain_name}` — `schema_version` is NOT in the field list.
- **c4-spec.md R-C4.2 (`:38`), §3.1 step 2 (`:152`), CONTRACT NOTES (`:459`)** all cite the
  **5-field** form `{crew_name, queue, epic_id, goal, captain_name}`.

This is a real surface inconsistency: the owning spec (C3) and the writer (C4) list 5 fields; the
integration doc and SPEC.md list 6. **It is not a true semantic conflict** — the C3 §3.1 worked
example (`c3-spec.md:135-142`) AND the field-contract table (`c3-spec.md:155-162`) BOTH include
`schema_version: 1` as a required field. So the actual on-disk schema is 6-field and consistent;
only the inline `{...}` shorthand in C3's R-C3.1 / C4's references omits `schema_version`.
Recommend normalizing the shorthand to the 6-field form everywhere so a reader skimming R-C3.1 or
C4 CONTRACT NOTES doesn't author a 5-field handoff missing `schema_version`. Severity: minor.

### 2. `epic_completed` payload — CONSISTENT

`{epic_id, last_child_bead_id, closed_at}` matches byte-for-byte across emit side
(c1-spec.md §D1 `:99-103`, R-C1.1, §5 AC-1) and consume side (c4-spec.md R-C4.4 `:46`,
CONTRACT NOTES `:468`; 06 §3 table `:187`; SPEC.md). ✓

### 3. C2 verbs + exit codes — CONSISTENT

C4's consumed forms (`c4-spec.md:85-97`, CONTRACT NOTES `:472`) match C2's definitions exactly:
`crew start <name> --queue <q> --mission <path>` (daemon RPC, prints `session_id`, exit 17 if
daemon down, non-0 on collision/queue-bound/launch-fail — c2-spec.md §3.1 `:159`, §7 `:455-486`);
`crew stop <name> [--pause-queue]` (c2-spec.md §3.5); `crew list [--json]` (local read,
c2-spec.md §3.1 `:161`). C4's "non-0 not 17 on collision" matches C2 §7 `:455`. ✓

### 4. Beads `--assignee` mirror — INCONSISTENT on the READ side (the core finding)

- **Write side (C3):** `c3-spec.md §3.1 (:183)` + boot step 4 (`:229`) + the message-handling
  `assignment` branch (`:246`) — crew runs `br update <epic> --assignee <crew_name>`. ✓ matches
  06 §3 table and Gap-1 resolution.
- **Read side (C4):** **MISMATCH.** 06 §4 Gap 1 + SPEC.md C4 amendment say the Captain attributes
  via `br show <epic_id> --assignee`. But `c4-spec.md` itself says the OPPOSITE in three places:
  - §3.1 step 6a (`:173`): *"Cross-reference epic_id to the owning crew via `harmonik crew list`
    (the Record.Epic / Record.Queue fields)"*.
  - §5 step 1 (`:307`): *"Look up the owning crew: `harmonik crew list` (match Record.Epic/Queue)"*.
  - §7 row "epic_completed for an unknown/unassigned epic" (`:428`): keys on `Record.Epic`.
  - CONTRACT NOTES #1 (`:478-491`) *recommends* either a `crew set-epic` verb OR a C4-owned
    handoff-derived map — and explicitly says *"The handoff-derived map ... is what the context
    assumes today."* Neither of those is the resolved `br show --assignee` answer.

  So `c4-spec.md` reads the WRONG source (`Record.Epic`, which 06 §4 Gap 1 proves goes stale on a
  comms re-task). This is the material finding — see amendment_backport.

---

## contradictions

**One substantive contradiction** (already counted above): C4 spec body attributes `epic_completed`
via `crew list`/`Record.Epic`; 06 §4 Gap 1 and SPEC.md C4 amendment attribute via
`br show <epic> --assignee`. The integration doc resolved it correctly; the C4 component spec was
not updated to match.

On the other axes the specs are **internally consistent**:
- **Queue ownership** — one story everywhere: binding lives ONLY in the crew registry
  (`Record.Queue`); the queue model gets no owner field (c2-spec.md R-C2.1 `:48`, §7 `:461`;
  06 §3 table; c3-spec.md §A5). No contradiction.
- **Who closes beads** — daemon-only, agents never `br close` (c3-spec.md §3.2 step 4 `:267`,
  c4-spec.md `:112`/`:327`, 06 §5.2). Consistent.
- **session_id mint-vs-capture** — uniformly MINTED (caller-supplied UUID before launch), NOT
  captured/stdout-parsed (c2-spec.md §3.2 `:238`, R-C2.3; SPEC.md; 06 hop 3). The early
  problem-space text said "capture its session_id" (01:43-44, 03-components C2 `:30`) but c2-spec
  §2 R2 explicitly corrects this to minted; the normative specs and SPEC.md/06 all say minted.
  Stale wording in the bench docs only — not a spec contradiction.
- **interactive-vs-server remote-control** — uniformly INTERACTIVE `claude --remote-control
  "<name>"` (a real pasteable pane), server-mode explicitly rejected (c2-spec.md §2 R2 `:90`,
  §3.2; SPEC.md `:46`; 06 hop 3). Consistent.

---

## spec_faithfulness

**SPEC.md is faithful — it adds no new requirement or decision.** Every SPEC.md statement traces
to a component spec or to 06-integration.md:

- SPEC.md C1/C2/C3/C4 summaries are tight restatements of the respective `c*-spec.md` locked
  decisions + key-files lists. No new field, verb, AC, or path appears in SPEC.md that isn't in a
  component spec.
- The two SPEC.md "Integration-driven amendment" call-outs (C3 block `:159-164`, C4 block
  `:185-191`) cite `06-integration.md §4 Gap 1 / Gap 3` and restate them verbatim — they introduce
  nothing 06 doesn't already say.
- The "Open decisions" section (SPEC.md `:270-304`) is a faithful roll-up of C1 Open Questions 1–4
  and C3's one open decision — no new open item invented.
- SPEC.md correctly carries the path `.harmonik/crew/missions/<crew_name>.md` (matches c3-spec §3.1
  and 06 §1 hop 2). NOTE: c4-spec.md §3.1 step 2 (`:151`) and §3 (`:291`) use a DIFFERENT path
  `.harmonik/crew-missions/<crew_name>.md` (no nested `crew/missions`). 06 and C3 and SPEC.md all
  agree on `.harmonik/crew/missions/`; only C4's body uses the divergent `crew-missions/`. Minor
  path drift in C4 — fold into the back-port. (Not a SPEC.md fault; SPEC.md is correct.)

Faithful. The only path divergence is inside C4, not introduced by SPEC.md.

---

## amendment_backport: YES — back-port to BOTH c3-spec.md and c4-spec.md (C4 is the load-bearing one)

The Gap-1 (+ Gap-3) amendments live in 06 §4 and in SPEC.md's two call-out blocks. The question is
whether an implementer reading ONLY the component spec would build the right thing.

### c3-spec.md — ALREADY CORRECT (no contradiction; one clarity back-port recommended)

c3-spec.md already specifies the `br update <epic> --assignee <crew_name>` mirror on **boot**
(`:229`, boot step 4) AND on **comms re-task** (`:246`, the `topic == assignment` branch:
*"adopt the new epic: ... mirror `br update <new_epic> --assignee <crew_name>`"*). So a crew built
from c3-spec alone DOES mirror on every adopt — the behavior Gap 1 depends on is present.

What c3-spec does NOT say is **why** this is load-bearing (that the Captain's attribution depends
on it, so it MUST run on every adopt, not just for restart re-hydration). c3-spec §3.1 frames the
mirror as "the durable source-of-truth a restart re-hydrates from" (`:183`) — it under-states the
attribution dependency. Recommend a one-line elevation so a future editor doesn't weaken the
re-task mirror thinking it's only for restart.

- **BACK-PORT (c3-spec.md, minor/clarity):** at `c3-spec.md:246` (the `topic == assignment`
  message-handling bullet) and/or §3.1 `:183`, add: *"This `--assignee` mirror is NORMATIVE for the
  Captain's `epic_completed` attribution (06 §4 Gap 1), so it MUST run on EVERY epic the crew
  adopts — boot AND comms re-task — not only for restart re-hydration."* (SPEC.md's C3 call-out
  already states this; c3-spec should carry it inline.)

### c4-spec.md — MUST BACK-PORT (material; the body contradicts the resolution)

An implementer authoring the `captain` skill from `c4-spec.md` alone would wire attribution to
`crew list` / `Record.Epic` (the stale-prone source 06 §4 Gap 1 explicitly rejects), because that
is what the C4 body says in three places and the CONTRACT NOTES recommend a handoff-map, never
`--assignee`. Exact lines to change:

1. **`c4-spec.md:173` (§3.1 step 6a)** — replace
   *"Cross-reference epic_id to the owning crew via `harmonik crew list` (the Record.Epic /
   Record.Queue fields) — purely a lookup."*
   with → *"Attribute epic_id to the owning crew via `br show <epic_id> --format json` → `assignee`
   (the durable beads mirror the crew sets on every adopt; 06 §4 Gap 1). Do NOT use
   `crew list`/`Record.Epic` — it goes stale on a comms re-task."*

2. **`c4-spec.md:307` (§4 SKILL outline §5 step 1)** — replace
   *"Look up the owning crew: `harmonik crew list` (match Record.Epic/Queue)."*
   with → *"Attribute the owning crew: `br show <epic_id> --format json` → `assignee` (durable
   mirror, 06 §4 Gap 1)."*

3. **`c4-spec.md:428` (§7 table, "epic_completed for an unknown/unassigned epic" row)** — change
   the detection from *"the event's `epic_id` does not match any `Record.Epic` in `crew list`"* to
   *"`br show <epic_id> --assignee` is empty / matches no live crew in `crew list`."*

4. **`c4-spec.md:478-491` (CONTRACT NOTES #1)** — this flag is now RESOLVED by 06 §4 Gap 1. Replace
   the "Recommend: either C2 add `crew set-epic` ... OR C4 maintain its own handoff-derived map ...
   is what the context assumes today" recommendation with: *"RESOLVED (06 §4 Gap 1): attribute via
   the durable `br show <epic> --assignee` mirror the crew sets on every adopt; no `crew set-epic`
   verb and no C4 in-memory map. `crew set-epic` remains an optional future convenience for
   `crew list` display only."*

5. **(fold-in, minor) `c4-spec.md:151` & `:291`** — change the handoff path `.harmonik/crew-missions/`
   to `.harmonik/crew/missions/` to match C3/06/SPEC.md.

6. **(Gap 3, already correct in C4 body)** — c4-spec.md §7 and CONTRACT NOTES #3 (`:500-506`)
   already state the dual-surface (status line + `comms send --to operator`) with the
   `comms log --from <captain>` no-join fallback, matching 06 §4 Gap 3. No change needed beyond
   noting it's now LOCKED (drop the "Recommend" framing if desired). Minor.

Because C4's deliverable is the `captain` *skill* (authored at implementation time from this spec),
the cleanest path is to back-port the four §3.1/§4/§7/CONTRACT-NOTES edits into `c4-spec.md` now so
the skill author reads the resolved form in one place rather than having to cross-reference 06 §4.

---

## findings

```json
[
  {
    "severity": "major",
    "where": "c4-spec.md §3.1 step 6a (:173), §4 SKILL outline §5 step 1 (:307), §7 table (:428), CONTRACT NOTES #1 (:478-491)",
    "issue": "C4 spec body attributes epic_completed to the owning crew via `crew list`/`Record.Epic`, which 06 §4 Gap 1 explicitly REJECTS as stale on a comms re-task. An implementer building the captain skill from c4-spec alone wires the wrong (stale-prone) attribution. The resolved answer (`br show <epic> --assignee`) lives only in 06/SPEC, not in the component spec.",
    "suggested_fix": "Back-port the resolution into c4-spec at the four cited lines: attribute via `br show <epic_id> --format json` -> assignee; mark CONTRACT NOTES #1 RESOLVED. (Exact replacement text in amendment_backport.)"
  },
  {
    "severity": "minor",
    "where": "06 §1 hop 2/§3 table + SPEC.md vs c3-spec.md R-C3.1 (:28) + c4-spec.md R-C4.2/CONTRACT NOTES",
    "issue": "Mission-handoff inline field-list shorthand drifts: 06/SPEC cite 6 fields {schema_version, crew_name, queue, epic_id, goal, captain_name}; C3 R-C3.1 and C4 references cite 5 (omit schema_version). C3's worked example + field-contract table DO include schema_version, so the real schema is 6-field and consistent — only the shorthand drifts.",
    "suggested_fix": "Normalize the inline {...} shorthand to the 6-field form in c3-spec R-C3.1 (:28), C3 §3.1 prose, and all c4-spec references so no reader authors a 5-field handoff missing schema_version."
  },
  {
    "severity": "minor",
    "where": "c4-spec.md §3.1 step 2 (:151) and §4 SKILL outline §3 (:291)",
    "issue": "C4 body uses handoff path `.harmonik/crew-missions/<crew_name>.md`; C3 §3.1, 06 §1 hop 2, and SPEC.md all use `.harmonik/crew/missions/<crew_name>.md`. Path drift could produce a handoff the crew/C2 reads at the wrong location.",
    "suggested_fix": "Change C4's path to `.harmonik/crew/missions/<crew_name>.md` (fold into the C4 back-port)."
  },
  {
    "severity": "minor",
    "where": "c3-spec.md §3.1 (:183) and message-handling branch (:246)",
    "issue": "c3-spec already mirrors --assignee on boot AND re-task, but frames it as 'the source-of-truth a restart re-hydrates from' — under-stating that the Captain's epic_completed attribution depends on it running on EVERY adopt. A future editor could weaken the re-task mirror.",
    "suggested_fix": "Add a one-line elevation: this mirror is NORMATIVE for Captain attribution (06 §4 Gap 1), so it MUST run on every adopt (boot AND re-task), not only for restart re-hydration."
  },
  {
    "severity": "info",
    "where": "01-problem-space.md :43-44, 03-components.md C2 :30 (bench docs)",
    "issue": "Early bench docs say C2 'captures its session_id' (stdout-parse), which c2-spec.md §2 R2 corrects to MINTED. Normative specs + SPEC.md/06 all say minted; only the upstream bench wording is stale. Not a spec defect.",
    "suggested_fix": "Optional: when finalizing, scrub the 'capture' wording in the bench problem-space/components docs to 'mint' for consistency. Non-blocking."
  }
]
```

Counts: 1 major, 3 minor, 1 info.

---

## ready_for_tasks: true

Rationale: the design is internally consistent with no real semantic contradiction and SPEC.md
invents nothing; the one material finding (C4 attribution back-port) is a documentation sync of an
already-DECIDED resolution (06 §4 Gap 1), so it can be folded into the C4 task brief rather than
blocking decomposition — but it MUST be folded in, or the captain-skill implementer will build the
stale `Record.Epic` attribution.
