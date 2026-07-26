# Integration Review

**Work:** process-group-provenance · **Date:** 2026-07-22
**Drafts under review:** `05-spec-drafts/process-lifecycle.md` (v0.5.5 → 0.6.0), `handler-contract.md` (v0.7.0 → 0.8.0), `beads-integration.md` (v0.7.0 → 0.8.0)

## What this pass is for, and the failure it guards against

This work retires a mechanism — PGID-as-provenance, the daemon `setsid` MUST, PL-021c, and HC-044a's
`.lock` plus its argv-recycling discriminator — and installs a replacement: the
`HARMONIK_PROJECT_HASH` marker, PL-006e/f/g, and `HARMONIK_SESSION_GEN`. Three spec files are being
rewritten. **Thirty-six are not.**

So the failure this pass exists to catch is specific: an unchanged spec that still depends on the
retired mechanism, still cites a retired requirement ID, or — worst — still *mandates a behaviour the
new matcher discipline forbids*. None of those is visible from inside the drafts, because the drafts
are internally consistent. They are only visible from the corpus.

**Five findings. One (C1) is an unchanged spec actively mandating a practice PL-006f(2) forbids, on a
decision where being wrong destroys live work.** All five live outside the three drafted files, so
none is fixable by editing a draft; they become cross-spec coordination requests.

## Cross-Reference Checks Performed

Examined: all 37 `.md` files under `specs/`, plus `specs/operator-nfr/config-inventory.md` and
`specs/reconciliation/{spec,schemas}.md` — not only the modified three, per the pass criteria.

| # | Check | Method | Result |
|---|---|---|---|
| 1 | Retired-ID inbound citations | `PL-021c`, `OQ-PL-011`, `PL-006a` corpus-wide, minus the 3 drafted files | **1 finding** (C4) |
| 2 | Retired-mechanism dependencies | PGID / `setsid` / process-group used as *provenance*, corpus-wide | **0 contradictions**; 1 compatible use (N2) |
| 3 | Forbidden-practice conflicts | argv / `comm` / binary-path used as a *match*, corpus-wide | **1 finding** (C1) |
| 4 | Marker-obligation attribution | `HARMONIK_PROJECT_HASH` citations corpus-wide | **1 finding** (C3) |
| 5 | Retired-mechanism attribution | HC-044a `.lock` citations corpus-wide | **1 finding** (C2) |
| 6 | New-symbol collision | `HARMONIK_SESSION_GEN` corpus-wide | **0** — no collision (N4) |
| 7 | Uncited cross-spec dependency | PL-006e(6)'s dependency on the keeper session-state file | **1 finding** (C5) |
| 8 | New event types needing EV | event-type inventory across the 3 drafts | **0** — no new types (N3) |
| 9 | Cross-reference target validity | every `[<file>.md …]` target in the drafts | **3 dangling, all pre-existing** (P2) |
| 10 | Terminology consistency | `project_hash`, "provenance marker", "generation nonce" | **Consistent** |
| 11 | Version monotonicity | draft front matter vs current spec front matter | **Correct**, all three |
| 12 | Changelog accuracy | `05-changelog.md` claims vs actual draft content | **Accurate**; one structural gap (G1) |

## Contradictions Found

### C1 — `workspace-model.md` mandates argv-as-generation-discriminator, which PL-006f(2) forbids

**Highest-severity finding of this pass.** Not a stale pointer: a live normative requirement, in an
unchanged spec, instructing implementers to do the exact thing the new matcher discipline prohibits,
for the exact reason the prohibition exists.

`specs/workspace-model.md:708` (WM §4.8, staleness detection) requires:

> mtime MAY be used ONLY as a tie-breaker when the content-based probe is ambiguous (e.g., PID is
> live but **argv identifies a different harmonik-daemon generation** than the current one)

and `:350` routes the live-non-orphan case to "fail-fast coordination with HC-044a", whose detection
mechanism this work retires.

Draft PL-006f(2): the command line, `comm`, and binary path are "permissible **only as narrowing
filters applied after a marker match has already succeeded**, and never as the match itself." The
generation question in particular is what PL-006e(6) exists to answer, precisely because argv cannot
— harmonik's own operating contract manufactures argv-identical live twins across generations.

**Resolution: coordination request to WM (CR-1), not a draft edit.** The drafts are correct as
written; WM is outside this work's declared scope. WM's next revision must replace the
argv-generation clause with the PL-006e(6) `HARMONIK_SESSION_GEN` conjunct.

**Note the direction of the risk.** WM's rule decides that a lease is *stale*, which authorises
reclaiming a workspace. Left in place beside the new marker, an implementer reading WM and PL
together receives contradictory instructions about the one decision where being wrong destroys live
work. This is why CR-1 is filed as a bead rather than left as a changelog line.

### C2 — `workspace-model.md` NOTE cites an HC-044a mechanism this work retires

`specs/workspace-model.md:325` carries a three-way-disagreement NOTE (OQ-WM-005): WM names
`${workspace_path}/.harmonik/lease.lock`, "HC-044a currently names
`.harmonik/worktrees/<run_id>/.lock`", PL-006 names `.harmonik/lease.lock`.

The HC draft retires HC-044a's `.lock` pidfile entirely (HC v0.8.0 revision history: "the
never-implemented per-run `.lock` pidfile, its liveness probe, and its argv-check recycling
discriminator are retired in favour of the PL-006e marker plus generation nonce").

**This is an improvement, not a regression.** The three-way filename disagreement collapses to
two-way, and the remaining two — WM and PL — already agree on `.harmonik/lease.lock`. OQ-WM-005
becomes *resolvable*, where before it was not.

**Resolution: coordination request to WM (CR-2).** Update the NOTE to record that HC no longer names
a filename; close OQ-WM-005 as resolved-by-convergence. Recording this matters because an unrevised
NOTE keeps implementers treating a settled question as open.

### C3 — `claude-launchspec.md` attributes the marker obligation to PL-006a, which now disclaims it

`specs/claude-launchspec.md:152`: baseEnv "MUST already include `HARMONIK_PROJECT_HASH` **per
PL-006a**".

Draft PL-006a is explicit that this is no longer true (`process-lifecycle.md:386`): "PL-006a defines
the `project_hash` primitive and the tmux session-name namespace only; **it does not define the
marker**." The obligation now lives at PL-006e(3).

**The obligation survives unchanged** — CL is not wrong about what must happen, only about which
requirement says so. But the citation is load-bearing in a specific way: PL-006e(3) widens the write
obligation to *every* harmonik-spawned process, a stronger claim than the one CL currently inherits.
An implementer following CL's pointer lands on a requirement that now explicitly says "not me."

**Resolution: coordination request to CL (CR-3).** Repoint PL-006a → PL-006e(3). No draft edit.

### C4 — `event-model.md` §8.7.14 sources two payload fields to the retired PL-021c

`specs/event-model.md:255` annotates `tmux_windows_killed` and `tmux_kill_window_survivors []int` as
"(PL-021c)"; `:1808` repeats the attribution in EV's own revision history. The PL draft retires
PL-021c — it was conditioned on a window-name sentinel no production spawn path produces, so it has
never been able to fire.

**The fields are unaffected.** They are additive and EV-029/§6.4-tolerated; consumers ignore unknown
fields. Only the source annotation dangles. EV's revision history at `:1808` is an immutable
historical record and MUST NOT be rewritten — the citation was accurate when written.

**Resolution: coordination request to EV (CR-4), lowest severity here.** EV's next revision should
mark the §8.7.14 taxonomy-row annotation as sourced from a retired requirement. No draft edit and no
field removal is implied by this work.

### C5 — PL-006e(6) depends on a session-keeper-owned surface without citing it

PL-006e(6) specifies the conforming nonce mechanism as: the long-lived process "records the current
generation at the moment it starts, reading it from **the harmonik-owned session-state file the
keeper updates on every cycle**."

That file exists and is spec'd: `specs/session-keeper.md` §4.1 SK-003 gives `GaugePort` the
`SetManagedSession` write-back and the `.managed` gate-ladder read. **So the dependency is real, not
fabricated** — which is the failure this check was hunting, and it is absent. Two gaps remain, both
worth stating rather than leaving to a reader:

1. **PL-006e(6) does not cite `session-keeper.md`.** Every other cross-spec dependency in the drafts
   carries a bracketed citation; this one is prose-only, so the coupling is invisible to anyone
   editing SK.
2. **SK does not state the property PL now relies on** — that the recorded value differs across
   generations. It is *measured* true (three generations, only the session id differing;
   `04-design/FINDING-generation-nonce-mint-point.md`), and SK-003's per-cycle write-back makes it
   true in practice, but PL-006e(6)'s correctness now rests on a property SK never promises.

**Resolution: half draft-edit, half coordination request.** Gap 1 is inside a file this work owns and
is fixed at finalization by adding the `[session-keeper.md §4.1 SK-003]` citation — a one-reference
addition asserting nothing new. Gap 2 is SK's to state, and becomes CR-5.

*Deliberately not applied here:* editing a draft during the integration pass, after the pass-5
reviewer approved the text, would fold a change in silently. It is recorded as a finalization action
so the change stays visible.

## Consistency Issues Found

**Terminology is consistent across the three drafts.** `project_hash` is used uniformly and matches
its use in the five unchanged specs that carry it (PL, BI, WM, ON, cognition-loop). "provenance
marker" (26 uses) and the short form "the marker" (44) are used in the standard
introduce-then-shorten pattern, never as competing terms. "generation nonce" is spelled one way
throughout, and `HARMONIK_SESSION_GEN` is its only symbol.

**Version bumps are correct and monotone:** PL 0.5.5 → 0.6.0, HC 0.7.0 → 0.8.0, BI 0.7.0 → 0.8.0,
each matching the current spec's front matter. All three keep `status: reviewed`.

**No structural inconsistency found** between the drafts. The two-halves-of-one-change constraint —
marker-write and matcher-discipline shipping together — is stated in both PL-006f and BI-014b rather
than in one place and assumed in the other.

### Checks that found nothing — recorded so they are not re-run blind

**N1 — `operator-nfr.md`'s PL-006a citations are correct and unaffected.** ON cites PL-006a at
`:714`, `:739`, `:773`, `:799`, `:1025`, `:1032`, in every case for the `project_hash` / 12-hex
`daemon_id` primitive. PL-006a retains exactly that role. No action.

**N2 — ON-053 records PGID in the panic forensic file. Not a contradiction.** ON-053 (`:624`) writes
"the daemon's PID, PGID, project_hash, and binary commit hash" to `.harmonik/panic-<timestamp>.log`.
This work retires PGID as a **provenance value** while explicitly retaining it as a **kill handle**
(HC-044: "a process group is declared a kill handle carrying no provenance meaning"). Recording a
PGID as post-mortem forensic data is neither. **Stated explicitly because it is the most likely thing
for a later reader to "fix" wrongly** — deleting PGID from a forensic record because "PGID was
retired" would remove real diagnostic value.

**N3 — no new event types.** The drafts introduce no event type absent from `event-model.md`;
`daemon_orphan_sweep_completed`, `bead_in_progress_reset`, and `bead_sync_failed` all pre-exist. No
EV taxonomy addition is required.

**N4 — `HARMONIK_SESSION_GEN` does not collide.** Zero occurrences corpus-wide. It also sits outside
the `HARMONIK_SECRET_*` strip and the credential deny-list of `[credential-isolation.md §4.1 CI-002]`,
so nothing in the CL env-assembly path removes it before children inherit it — checked because a
nonce stripped at the launch boundary would fail silently.

**N5 — BI-014a and BI-014b ship together, as PL-006f requires.** Both the matcher (BI-014a) and the
write obligation (BI-014b) are present in the BI draft, with the paired-change constraint stated in
the spec text itself.

## Cross-Reference Validity

Every bracketed cross-reference in the three drafts was extracted and its target checked. **The
drafts introduce no new dangling target.** Three targets do not resolve, and all three are inherited
corpus-wide — see P2 under *Pre-existing issues*.

Reverse direction — nothing links to removed content and nothing was orphaned — is covered by C1–C4,
which are exactly the inbound references to retired or reassigned material. PL-INV-005 has no citer
outside `process-lifecycle.md`, so its restatement orphans nothing.

## Changelog Verification

`05-changelog.md` was checked claim-by-claim against the drafts. Every requirement it names —
PL-002b, PL-005, PL-006, PL-006a, PL-006e, PL-006f, PL-006g, PL-007, PL-008, PL-008a, PL-011,
PL-017a, PL-021b, PL-021c, PL-INV-005, HC-018, HC-044, HC-044a, BI-010, BI-014a, BI-014b — is present
in the corresponding draft in the state the changelog describes. The two dated non-coverage rows and
the darwin measurement are present in PL-021b §7 and reflected in the revision history. **No
inaccuracies.**

**G1 — one structural gap.** The changelog has no *Cross-spec coordination requests* section. That is
the corpus convention for a revision that makes other specs stale — `operator-nfr.md` v0.4.0 and
`event-model.md` v0.5.2 both carry one, and it is how the next editor of WM / CL / EV / SK learns
their spec moved under them. The section content is below; it is carried as a finalization action.

## Cross-spec coordination requests

None of these is a defect *in* this work. Each is an unchanged spec that this work's landing makes
stale, wrong, or resolvable.

| ID | Target | Request | Severity |
|---|---|---|---|
| CR-1 | `workspace-model.md` §4.8 (`:708`, `:350`) | Replace the argv-generation-discriminator clause with the PL-006e(6) `HARMONIK_SESSION_GEN` conjunct; repoint the HC-044a fail-fast coordination to the marker mechanism | **High** — currently mandates a forbidden practice on a destructive decision |
| CR-2 | `workspace-model.md` `:325` NOTE | Record that HC-044a no longer names a lock filename; close OQ-WM-005 as resolved-by-convergence on `.harmonik/lease.lock` | Medium |
| CR-3 | `claude-launchspec.md` `:152` | Repoint the `HARMONIK_PROJECT_HASH` obligation from PL-006a to PL-006e(3) | Medium |
| CR-4 | `event-model.md` §8.7.14 (`:255`) | Annotate `tmux_windows_killed` / `tmux_kill_window_survivors` as sourced from retired PL-021c. Do NOT rewrite the `:1808` revision-history entry | Low |
| CR-5 | `session-keeper.md` §4.1 SK-003 | State normatively that the managed-session value differs across generations, since PL-006e(6) now depends on it | Medium |

## Actions carried into finalization

1. **Add the `[session-keeper.md §4.1 SK-003]` citation to PL-006e(6)** (C5 gap 1) — reference only,
   asserts nothing new.
2. **Add a "Cross-spec coordination requests" section to `05-changelog.md`** carrying CR-1..CR-5 (G1).
3. **File CR-1 as a bead**, not only as a changelog line. It is the one request whose non-landing
   leaves a spec actively instructing implementers to identify a process generation by command line.
4. Carry the two pass-5 reviewer minors already agreed: the second darwin non-coverage row (captain's
   ruling, applied) and the A7 truncate-before-read rationale note under PL-005 (applied).

## Pre-existing corpus issues — recorded, not fixed

**P1 — `claude-launchspec.md:123` cites PL-006a for a requirement PL has never contained.** CL says
`daemonBinaryPath` is "set from `os.Executable()` at daemon startup per PL-006a". Neither the current
PL v0.5.5 nor the v0.6.0 draft contains `os.Executable` or `daemonBinaryPath` anywhere. A pre-existing
fabricated citation, **not** introduced by this work and not repaired by it. Worth filing separately;
deliberately not bundled into CR-3, because mixing a real repoint with an unrelated pre-existing
defect is how the repoint gets rejected as scope creep.

**P2 — three corpus-wide dangling link targets.** The drafts link to `testing.md` (5 refs),
`core-scope.md` (4), and `reconciliation.md` (3). None resolves: `specs/reconciliation/` is a
directory (`spec.md`, `schemas.md`), and `testing.md` / `core-scope.md` are absent entirely. These are
inherited — `testing.md` is referenced by 12 existing spec files, `reconciliation.md` by 8,
`core-scope.md` by 2. Corpus-wide link hygiene is its own task.

## Final Assessment

**Coherent, with four external repairs required and one internal citation to add.**

The three drafts are internally consistent with each other and with the 36 specs they do not modify,
with the single exception of C1. The retirements are clean: PL-021c, the daemon `setsid` MUST, the
PGID provenance half, and HC-044a's `.lock` plus argv discriminator each have their inbound references
accounted for, and the one live conflict is identified rather than inherited silently.

The finding most worth carrying forward is **C1**, and specifically its shape. This work's whole
thesis is that identifying a process by its command line is unsafe on this system, because the system
manufactures command-line-identical twins. `workspace-model.md` contains that exact practice, applied
to deciding whether a workspace lease is dead — and WM was never in scope, so nothing in the pass-1
through pass-5 chain would have looked at it. **A spec corpus can hold a retired practice in a file
nobody opened.** That is the argument for this pass existing at all, rather than advancing straight
from an approved draft to finalization.

The second is **N2**. PGID survives as a kill handle and as forensic data; it dies only as
provenance. The drafts say this precisely. The risk is a later reader who remembers only "PGID was
retired" and strips it from the panic file. Recorded so that reading is available in writing.

**Recommendation: advance to `tasks`,** with the four finalization actions carried and CR-1 filed as a
bead before this work is parked.
