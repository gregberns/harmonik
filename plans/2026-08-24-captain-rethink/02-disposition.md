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

### The three load-bearing commands are fixed — 2026-08-24

`hk-zqz0w`, `hk-nqomq` and `hk-h22p2` are closed. The pass also found and fixed two defects
no bead had named, and filed two more it could not close honestly — `hk-mwmau` and `hk-qudd3`. Each fix was re-verified against live state
before it was written, and each rewritten command was then run on the live fleet.

| Bead | Was | Now |
|---|---|---|
| `hk-zqz0w` | Step 5d rebuilt the pane target by hand as `harmonik-<hash>-crew-<crew>:hk-crew-<crew>`. No window has that name. | Reads the `handle` field out of `crew list --json`. Verified against a crew on each of the two live handle shapes — `hk-alpha:1` and `harmonik-<hash>-crew-charlie:agent`. The stray-window row is re-signed to the real `harmonik-<hash>-run-<id>` form. |
| `hk-nqomq` | Absence was tested as "not in `comms who`" past a 120-second TTL, in six places across `STARTUP.md`, `SKILL.md` and `SHUTDOWN.md`. `comms who` prints `stale` rows for ten minutes past that TTL. | Five filter on `.status == "online"`; the sixth became a registry check instead — see the `hk-mwmau` row. The two windows are stated once, next to the classification table, with the source symbols named. |
| `hk-mwmau` (found here, filed) | `SHUTDOWN.md` confirmed a `crew stop` by checking the crew had left the bus. `crew stop` writes no leave beat, so a stopped crew stays listed for ten minutes and a successful stop reads as a failed one. | The registry is the confirming check; presence is demoted with the reason stated. The command gap itself is filed. |
| `hk-h22p2` | `br show <id> --format json` read as `.assignee`, in six places across `STARTUP.md`, `SKILL.md` and `SHUTDOWN.md`. `br show` returns an ARRAY, so every one of them errors. | All six use `.[0].assignee`, and each says why. |

**Two of the three beads undercounted their own sites.** `hk-nqomq` named three places and
there were six, across three files. `hk-h22p2` named three and there were six, across three
files — including `SHUTDOWN.md`, which no bead mentioned. Only `hk-zqz0w` had named all of
its sites. Grep the whole corpus for the broken form before calling a fix complete: an audit
that found a defect three times is not evidence there is no fourth.

**The sixth site was the one that mattered, and no audit had seen it.** `SHUTDOWN.md`
confirmed a `crew stop` by checking the crew had left the bus. `crew stop` emits no presence
leave beat, so a stopped crew stays listed for ten minutes — the check reads a successful
stop as a failed one, at the exact moment the captain is standing the fleet down. The text
now checks the registry instead. The command gap behind it is `hk-mwmau`.

**One gap is filed rather than closed.** The stray-run-session row is now correct for the
live shape, but a second, window-shaped run form exists on the non-local path. Naming it
means naming a bead-derived window nobody has captured live, and this commit exists to take
unverified citations OUT of the file. `hk-qudd3` carries it, with what to reproduce first.

**The fix for `hk-zqz0w` is the general lesson, not the specific one.** The command broke
because it built an identifier the system already publishes. Prefer the field over the
reconstruction anywhere the corpus does this — the same pass applied it to the four
`<session>:1` targets in the wedge-recovery block, which resolve today only because the
agent window happens to sit at index 1.

**But the field needs a guard the reconstruction did not.** Review caught this, and it is
the sharper half of the lesson. A hand-built tmux target fails LOUDLY — no such window. A
looked-up one can come back empty, from a name typo or from a registry record with a blank
handle, and `tmux capture-pane -t ""` then captures the CALLER's own pane and exits 0. The
captain reads its own text as a crew's pane-truth. Reproduced live before the guard was
written. Trading a loud failure for a silent wrong answer is not an improvement; every
lookup in the corpus is now tested for empty before it is used.

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
- **`crew stop` emitting the leave beat it already knows how to write** (`hk-mwmau`) would
  delete the ten-minute caveat this pass had to add to `SHUTDOWN.md`, and would take most of
  the reason out of the online-vs-listed explanation the classification table now carries.
- **A digest that printed the crew class** (healthy / zombie / ghost / idle) turns a table of
  rules into a column of answers.

Precedent, already banked: the recover-vs-resume rule was in six instruction files because the
boot digest printed the wrong verb. Fixing the script retired the reason for all six.

## What is left in Step 2

1. Reconcile A and B into one file — they agree closely enough that this is an edit, not a
   decision. **The command fixes have landed, so this is now the next step.** Reconcile over
   the CURRENT `STARTUP.md`, not over pass A's saved draft: the draft predates the fixes and
   carries all three broken commands.
2. Run the same fan-out on `SKILL.md` (21.3 KB) and `SHUTDOWN.md` (12.9 KB).
3. `SHUTDOWN.md` is 13 KB of deploy mechanics and is a **runbook the captain calls**, not
   standing instruction it carries. That is question 5 of the charter, and answering it moves a
   whole file rather than trimming one.
4. `orchestrator-rules/SKILL.md` (5.0 KB, 54 lines) — the operator has looked at it and wants
   it left alone for now.
