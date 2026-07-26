# 05 — Changelog: process-group-provenance

**Work:** process-group-provenance · **Pass:** 5 (spec draft) · **Crew:** kilo · **Date:** 2026-07-22
**Beads:** hk-n93gq, hk-o7x4w, hk-g1qby, hk-c6dt2, hk-5z4ww

Three spec files change. Each draft in `05-spec-drafts/` is the **complete updated file**, not a
diff, and is named to match its target in `specs/`.

> Note for whoever maintains kerf: this file arrived carrying the **spec-draft** skeleton template
> ("Replace this comment and the TODOs below with the full updated spec file text"), not a changelog
> template. That is the filename-placeholder bug already filed. The content below is a changelog.

---

## `specs/process-lifecycle.md` — MODIFIED (0.5.5 → 0.6.0)

Carries most of the change. One new section, three new requirements, two retirements, six
amendments, and two open questions closed.

### New — §4.2a Process provenance

| ID | What it establishes | Design |
|---|---|---|
| **PL-006e** | The provenance marker: one mechanism (an inherited environment variable), its inheritance across `setsid`, a write obligation reaching launch-layer and `br` spawns, conditional darwin readability with an explicit fail-closed rule, a whitespace-free value constraint with its mechanical reason, and a generation nonce with a mint point that survives an in-place session reset. | `process-lifecycle-design.md` §3 |
| **PL-006f** | Matcher discipline: the darwin argv-strip rule and the forgery it prevents; the prohibition on command line, process name, and binary path as provenance; uniform fail-closed handling of unreadable inputs; no pid or pgid identity; a fixed evaluation order. | `process-lifecycle-design.md` §3 |
| **PL-006g** | The spawn-site register — every spawn site is conformant or exempt-with-reason, which is what turns PL-INV-005's sensor into something a reviewer can check. | `process-lifecycle-design.md` §3 |

### Retired, in writing

- **PL-006a's daemon-`setsid` MUST and the PGID half of the provenance marker.** Both rationales are
  void under PL-006e, and both are stated in the draft so a reader can tell a retirement from an
  omission. (Design §4, OD-3.)
- **PL-021c — pane orphan recovery.** Every step is conditioned on a window-name sentinel that no
  production spawn path produces, so the requirement has never been able to match anything. The draft
  records why, what carries the regime instead, and what restoring it would cost. It also states that
  retirement does **not** oblige removing the event fields the requirement introduced — two other
  clauses cite them as an additive-extension precedent, and removing them would break the payload
  compatibility those clauses rely on. (Design §4, research risk 4.)

### Amended

| Ref | Change | Resolves |
|---|---|---|
| PL-006 subprocess cleanup | The false claim that `br` children bear the marker is removed and corrected in place. The coverage boundary of the `PPID==1` filter is stated: the sweep is **origin-agnostic** and does reach an orphaned `setsid` descendant; it does not reach a process whose parent is still alive. | D1, D6 |
| PL-007 | "MUST NOT match on binary path **alone**" → those signals are never the match and may only narrow after a marker match. The draft says why the old form was insufficient: it permitted combining two non-provenance signals and calling the combination enough. | A6 |
| PL-017a(b) | The relay-grandchild exclusion is declared an exclusion input and inherits PL-006f(3)'s fail-closed rule. It previously failed **open** — an unreadable input meant the candidate was killed. | D7 |
| PL-021b §7 | The substrate kill verb's reach is stated as a three-row table: reached by the session kill, reached by PL-006 once orphaned, reached by nothing while still parented. The third row is dated and bounded. | A4, OD-5 |
| PL-INV-005 | Parentage is stated per spawn regime. The previous "daemon is the initial parent of every handler subprocess" was contradicted by PL-021b, which mandates the multiplexer, and by the conforming implementation. The sensor is restated against PL-006e and made checkable via the register. | D5, D2, OD-6 |
| PL-006a opening | The `realpath` case-fold pointer moves from OQ-PL-008 to OQ-PL-008a. | D8 |

### Open questions

- **OQ-PL-008 RESOLVED** — the environment is readable on darwin via `ps -E` with the strip rule;
  PGID is not the answer and is no longer a provenance value; no filesystem fallback. The draft
  records that the question was malformed rather than merely hard: it asked how to *read* a marker on
  a population that carries none.
- **OQ-PL-008a NEW** — the `realpath` case-fold ambiguity, split out so that resolving the provenance
  half cannot silently close it. This was D8.
- **OQ-PL-011 SUPERSEDED** — its premise, that `setsid` escapes the marker, is false for an inherited
  environment variable. Its disposition ("handlers MUST NOT internally `setsid`") is withdrawn as
  unenforceable: it addressed a component outside this system.

---

## `specs/handler-contract.md` — MODIFIED (0.7.0 → 0.8.0)

- **HC-044** is restructured into parentage (per regime), process group (a **new** obligation:
  direct-exec subprocesses lead their own group and are killed group-wise), the declaration that a
  group is a kill handle with no provenance meaning, the marker cross-reference, and the honest limit
  of the group kill. The group clause is presented as an addition rather than a correction, because
  the previous text imposed no group rule at all — presenting it as a fix would misdescribe the
  change. The draft notes that existing code comments mis-attribute a join-the-daemon's-group rule to
  HC-044; that rule came from the now-retired PGID clause. (Design §2.)
- **HC-018**'s 5-second cleanup bound becomes per-group, with no per-descendant clock restart, and
  states the failure it forecloses: a depth-*n* tree taking 5*n* seconds while the requirement still
  claims 5, against an orphan sweep that anchors its own escalation interval to this bound. (Design §3.)
- **HC-044a** keeps its fail-fast obligation unchanged and replaces its detection mechanism. The
  never-implemented `.lock` pidfile, its liveness probe, and its argv-check recycling discriminator
  are retired in favour of the marker plus generation nonce. The argv check was forbidden by
  PL-006f(2) landing in the same revision, so leaving it would have shipped a contradiction inside one
  release. The two fail-closed polarities — unreadable means *do not kill* for a reaper, unreadable
  means *treat as held* for a launch — are stated side by side with the shared principle named, so a
  later harmonisation cannot invert one of them. (Design §4, OD-7.)

---

## `specs/beads-integration.md` — MODIFIED (0.7.0 → 0.8.0)

- **BI-014a** identifies orphan `br` subprocesses by the marker under PL-006f discipline. Binary path,
  pinned path, process name, and parent-PID-1 are demoted to post-match narrowing filters. The
  fail-closed trade is written into the requirement rather than delegated to the general rule, with
  its asymmetry stated: an orphan left alive causes recoverable, already-classified contention;
  killing the wrong `br` is silent and unrecoverable, and `br` is the operator's own CLI. The draft
  records that the prior text contradicted PL-007 with both MUSTs in force, and that the
  implementation obeyed neither — it matched on the process basename. (Design §2.1.)
- **BI-014b (NEW)** — the adapter MUST set the marker on every `br` subprocess it spawns, **explicitly**
  rather than by inheritance, because the daemon does not carry the marker in its own environment. The
  draft states that BI-014a and BI-014b must take effect together and what each half does alone: the
  matcher without the marker is a sweep that silently matches nothing; the marker without the matcher
  widens every reaper's candidate set with no new guard. (Design §2.2.)
- **OQ-BI-010 RESOLVED** — PL needs no `br`-specific enumeration extension; one marker and one matcher
  discipline already cover `br`. The question was blocked on the write side, which BI-014b now owns.
  (Design §2.3.)

---

## What the pass-4 review changed, carried into these drafts

The design review returned REQUEST_CHANGES on two overstated claims. Both corrections are load-bearing
in this text rather than cosmetic:

1. **The marker is not written by nothing.** The daemon handler path does write it; the launch layer
   and `br` do not. The drafted PL-006e(3) write obligation and the PL-006g register are scoped to the
   population that actually lacks it, and the drafted PL-006 coverage note does not claim the marker is
   universally absent.
2. **The orphaned `setsid` descendant is reachable.** Had the original framing been drafted, PL-021b §7
   would have told implementers the real leak was unfixable, and hk-o7x4w could have been closed as a
   known gap by the very change that fixes it. The drafted coverage tables in PL-006, PL-021b §7, and
   HC-044(e) each carry the covered case and the uncovered case separately.

One further correction was found during this pass rather than by the reviewer, and it changed the
drafted text of PL-006e(6): the generation nonce cannot be minted per `harmonik start <role>`, because
an in-place session reset never re-invokes it and would leave one nonce spanning exactly the
generations the nonce exists to separate. The draft specifies a mint point that survives that event and
states why the multiplexer's session environment cannot serve as the delivery mechanism. See
`04-design/FINDING-generation-nonce-mint-point.md`.

---

## Traceability — bead → drafted text

| Bead | Drafted in |
|---|---|
| **hk-n93gq** — run children join the daemon's group, so no kill path reaches a grandchild | HC-044(b) own-group + group-directed kill; HC-044(e) and PL-021b §7 coverage tables; HC-018 group-scoped bound |
| **hk-o7x4w** — darwin orphan sweep is a no-op | PL-006e(4) darwin readability; PL-006f(1) strip rule; PL-006 coverage boundary; OQ-PL-008 resolution |
| **hk-g1qby** — `setsid` never called | PL-006a retirement note (the MUST is withdrawn, not quietly satisfied); OQ-PL-011 supersession |
| **hk-c6dt2** — `br` sweep unscoped | BI-014a identification rules; BI-014b write obligation; PL-006 false-sentence correction; PL-007 strengthening |
| **hk-5z4ww** — watcher reaper | PL-006e(6) generation nonce and its mint point; PL-006f(2) argv prohibition, which is what makes the current launch-path reaper non-conformant |

Every drafted requirement traces to a change design. No drafted text adds a requirement that no design
calls for. The one addition made during this pass — the mint-point correction in PL-006e(6) — is
recorded in `04-design/FINDING-generation-nonce-mint-point.md` and in the design's §8 risk 4.

---

## Post-review additions (2026-07-22, after the pass-5 APPROVE)

**`process-lifecycle.md` — PL-005 rationale note (added).** The pass-5 reviewer noted that the A7
rationale note the design asked to retain was missing from the draft. It is now present under
PL-005, explicitly non-normative: step 1 acquires the pidfile lock, which truncates the pidfile per
PL-002b, two steps before the step-3 orphan sweep. No current requirement reads prior-generation
pidfile content at sweep time, so the ordering is sound as written and is deliberately unchanged;
any future design that does read it MUST move that read ahead of step 1's truncation. Motivated by
`04-design/process-lifecycle-design.md` §4.1 (A7, withdrawn as a change, retained as a note).

**Not folded in, deliberately.** The reviewer's other minor — no normative MUST requires the reaper
to fire on session RESTART rather than only at launch — belongs to whichever spec owns the
launch/keeper reaper, not to these three files. Filed as bead **hk-3eurz** so it lands before or
alongside finalization instead of being smuggled into an out-of-scope spec.

**Open against the coverage table.** `FINDING-darwin-marker-unreadable-on-apple-binaries.md` records
a measured second non-coverage boundary on darwin: `ps -E` returns an empty environment for
Apple-signed system binaries (`/bin/sh`, `/bin/zsh`, `/bin/sleep`), while our own Go binaries,
homebrew binaries, and Xcode-framework binaries read fine. Polarity is safe — PL-006f(3) fails
closed, so such candidates are spared, never killed — but PL-021b §7 currently names only one
non-coverage row and this is an orthogonal second. Awaiting the captain's ruling on whether to add
the row and re-review the delta, or carry it as a named follow-up against finalization. The draft
text is unchanged pending that ruling.

**`process-lifecycle.md` — second non-coverage row added (captain ruling, 2026-07-22 12:23Z).** The
captain ruled that the darwin finding is carried into finalization as a documented row rather than a
separate review round. Applied in three places so they cannot drift apart:

- **PL-021b §7 coverage table** gains a fourth row: on darwin, any descendant whose executable is an
  Apple-signed system binary is reached by nothing. Stated as orthogonal to the third row — it
  applies to orphaned and still-parented candidates alike, applies whether or not `setsid` was
  called, and unlike the third row is NOT bounded by the root's lifetime. The polarity is stated
  (under-reaping, never killing an unowned process) together with an explicit prohibition on
  responding to it by relaxing PL-006f(3) or falling back to an argv match.
- **PL-006e(4)** gains the measurement and a pointer to that row. Clause (4) already stated the
  Apple-platform-binary limitation qualitatively; what was missing was that the limitation was never
  carried through into the coverage declaration, and that the affected population is generated
  continuously rather than being an edge case.
- **Revision history** restated from one non-coverage row to two.

Measurement: macOS 26.3.2, `/bin/sh`, `/bin/zsh`, `/bin/sleep` return an empty environment with no
error; harmonik's own Go binaries, package-manager binaries, and framework-resident interpreters read
correctly. Full write-up and reproduce recipe:
`FINDING-darwin-marker-unreadable-on-apple-binaries.md`.

---

## Cross-spec coordination requests

Added by the pass-6 integration check (`06-integration.md`). None of these is a defect in this work.
Each is an unchanged spec that this work's landing makes stale, wrong, or resolvable. Carried here
because this section is the corpus convention for a revision that moves other specs under their
editors — `operator-nfr.md` v0.4.0 and `event-model.md` v0.5.2 both carry one — and without it the
next editor of WM / CL / EV / SK has no way to learn their spec changed.

| ID | Target | Request | Severity |
|---|---|---|---|
| **CR-1** | `workspace-model.md` §4.8 (`:708`, `:350`) | Replace the argv-generation-discriminator clause with the PL-006e(6) `HARMONIK_SESSION_GEN` conjunct; repoint the HC-044a fail-fast coordination to the marker mechanism. **Filed as bead hk-3ncno (P1).** | **High** |
| **CR-2** | `workspace-model.md` `:325` NOTE | Record that HC-044a no longer names a lock filename; close OQ-WM-005 as resolved-by-convergence on `.harmonik/lease.lock`. | Medium |
| **CR-3** | `claude-launchspec.md` `:152` | Repoint the `HARMONIK_PROJECT_HASH` obligation from PL-006a to PL-006e(3) — PL-006a now explicitly disclaims the marker. | Medium |
| **CR-4** | `event-model.md` §8.7.14 (`:255`) | Annotate `tmux_windows_killed` / `tmux_kill_window_survivors` as sourced from retired PL-021c. **Do NOT rewrite the `:1808` revision-history entry** — it was accurate when written, and the fields themselves stay. | Low |
| **CR-5** | `session-keeper.md` §4.1 SK-003 | State normatively that the managed-session value differs across generations, since PL-006e(6) now depends on that property. | Medium |

**CR-1 is the one that cannot be left as a line in a changelog,** which is why it is also a bead.
`workspace-model.md:708` decides whether a workspace lease is **stale** — the decision that
authorises reclaiming a workspace — and its normative example of how to tell generations apart is
"argv identifies a different harmonik-daemon generation than the current one". That is precisely what
PL-006f(2) forbids, for precisely the reason it forbids it: this system's own operating contract
manufactures command-line-identical live twins, because every crew keeps a watcher armed for its
whole session. Every other request here is a stale pointer that misleads a reader. This one instructs
an implementer to make a destructive decision using the one signal this work proves cannot carry it.

**One pre-existing defect found and deliberately not bundled:** `claude-launchspec.md:123` cites
PL-006a for `daemonBinaryPath` / `os.Executable()`, which no PL revision — current or drafted — has
ever contained. Unrelated to this work and not repaired by it; it is kept out of CR-3 so that a
straightforward repoint is not rejected as scope creep.

---

## Post-approval edits (2026-07-22, disclosed)

Two changes were made to the drafts after the pass-5 independent APPROVE. Both are additive and
neither alters an approved normative claim; they are recorded here rather than folded in silently.

1. **PL-006e(6) gains a citation to `[session-keeper.md §4.1 SK-003]`.** The clause already specified
   reading the generation from "the harmonik-owned session-state file the keeper updates on every
   cycle"; the file was simply never named. The integration pass confirmed the surface exists
   (SK-003's managed-session write-back) — the dependency was real, only uncited. The sentence noting
   that PL now depends on a per-generation-uniqueness property SK does not yet promise is added with
   it, and is the substance of CR-5.
2. **This changelog gains the Cross-spec coordination requests section above** (G1 in the integration
   pass).

Neither passes 6 nor 7 was independently reviewed: this crew session cannot spawn subagents, and the
work was directed to finalize and park. The **normative spec text was reviewed and approved at pass
5**, and passes 6 and 7 produce analysis and a task plan rather than spec text — with the single
exception of edit (1) above, which is disclosed here for that reason.
