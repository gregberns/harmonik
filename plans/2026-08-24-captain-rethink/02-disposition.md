# Step 2 — the STARTUP.md cut, and what the cut found underneath it

`.claude/skills/captain/STARTUP.md`, 408 lines / 22,266 bytes, put through the blind-cut
fan-out the plan calls for: three agents, same file, same rule, no sight of each other.

## The cut converges. Roughly two thirds comes out.

| Pass | Method | Result |
|---|---|---|
| A | Edit down under the cutting rule | 7,862 bytes, 155 lines — **−65 %** |
| B | Rewrite from first principles, then add back only pieces | 7,553 bytes, 142 lines — **−66 %** |
| C | Audit for dead / wrong / duplicated / scar text | ~11,400 bytes cuttable — **−51 %** |

A's cut file is kept at [`drafts/STARTUP-cut-passA.md`](drafts/STARTUP-cut-passA.md). B's is in
its report only; if it is needed, re-run the pass rather than trusting a memory of it.

**Both rewriters stopped above the 6,000-byte aim, independently, and gave the same reason.**
What is left is commands plus the exit codes, thresholds and field names welded to them — the
pieces the operator's standard says to keep. Neither cut prose to reach a number. Treat ~7,500
as the honest floor for this file at its current scope, and take the rest out by moving whole
responsibilities elsewhere rather than by trimming sentences.

**C put verified duplication at 10,354 bytes — 46 % of the file is text another file owns.**

## Ownership banners are not evidence

Four files in this corpus assert they hold the only copy of a rule. **Three are wrong.**
`harmonik-dispatch` says "this section owns the explanation" while six other files carry it.
`orchestrator-rules` `REFERENCE.md` claims the canonical wedge test while three files redefine
it, and `captain/SKILL.md` names a different owner for the same rule. A cut that trusts a
self-declared owner will keep duplicates. Diff the text.

## What the cut found underneath — eleven verified errors, filed

The cut is the smaller half of what this produced. Every item below was checked against source
or live state; the load-bearing three were re-verified independently before filing.

| Bead | What is wrong |
|---|---|
| `hk-qjgpo` **P0** | Every harmonik schedule is disabled. The ops-monitor has not run since 2026-07-09, so every "read `latest.json`" instruction reads whatever the last manual run produced. |
| `hk-zqz0w` | Step 5d's pane-verification command targets a tmux window name that was deleted. The step cannot run, and its documented failure response is to tear the crew down. |
| `hk-nqomq` | The zombie test uses a 120-second TTL. The stale cutoff is ten minutes and `comms who` lists stale rows, so a wedged crew reads healthy for eight minutes longer than the runbook says. Wrong in three places. |
| `hk-ql1ic` | `crew start` with a colliding **name** exits 0 and silently resumes. Only the queue half refuses — and the rule exists to stop double-staffing. |
| `hk-glxaj` | The ops-monitor runs 15 checks; the runbook maps 6. Unmapped ones were flagged at audit time. |
| `hk-h22p2` | `br show` returns an array, so the documented `.assignee` lookup errors. That is completion attribution, and the text around it forbids the registry route that appears to work. |
| `hk-chqin` | The idle-realign step reads a file nothing writes; a plan already ruled DELETE on it. |

Also stale in the file, not separately filed: the `hk-daemon-supervise` session name, the stray
worktree-window signature, `group_failure` as the dispatch-refusal token, `FORCE-ACT` as a
keeper concept, and the park spec's section numbers.

**Fix the commands before shortening the prose around them.** Five of these live inside text
the cut would otherwise rewrite, and a cut that carries a broken command forward has preserved
the wrong thing.

## The second list — rules that exist only because a tool says the wrong thing

The most valuable output of the pass, and the cheapest work on it. Each is a small fix
somewhere else that retires instruction text everywhere.

- **`comms` makes the caller re-type `--from` / `--agent`** when `$HARMONIK_AGENT` is already in
  the process. That one gap generates a paragraph in three skills.
- **A `crew start` collision error that named its holder** — "queue held by live crew alpha,
  last seen 40s ago, the lane is already staffed" — retires the never-rename rule from four
  files. Nobody renames around an error that says the lane is covered.
- **`crew start --verify`**, blocking until the crew is on the bus, makes Step 5d's whole
  two-axis ritual a postcondition of the call instead of a procedure to remember.
- **`harmonik crew nudge`** doing the clear-and-retype would retire the tmux block from three
  files. Better still, run the two-sample wedge detection inside the ops-monitor.
- **The never-`/quit` rule exists solely to countermand a `/quit` advisory another part of the
  system injects.** The fix is on the injecting side.
- **`harness` in the crew mission schema** is only ever documented as "leave this unset". A
  field that is always to be left unset is not a field.
- **A digest that printed the crew class** (healthy / zombie / ghost / idle) turns a table of
  rules into a column of answers.

Precedent, already banked: the recover-vs-resume rule was in six instruction files because the
boot digest printed the wrong verb. Fixing the script retired the reason for all six.

## What is left in Step 2

1. Reconcile A and B into one file — they agree closely enough that this is an edit, not a
   decision. Do it **after** the command fixes land.
2. Run the same fan-out on `SKILL.md` (21.3 KB) and `SHUTDOWN.md` (12.9 KB).
3. `SHUTDOWN.md` is 13 KB of deploy mechanics and is a **runbook the captain calls**, not
   standing instruction it carries. That is question 5 of the charter, and answering it moves a
   whole file rather than trimming one.
4. `orchestrator-rules/SKILL.md` (5.0 KB, 54 lines) — the operator has looked at it and wants
   it left alone for now.
