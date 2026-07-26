# Change Design review — round 1

**Verdict: REQUEST_CHANGES** (framing, not redesign). Independent reviewer, captain-routed
2026-07-22 11:24Z; verdict relayed 11:45Z. Changes applied by kilo the same hour; both are
recorded in-place in the design files as dated corrections rather than silent rewrites.

## Approved without change

B4-strict as the OD-1 resolution · marker write-coverage across the launch layer and `br` ·
matcher discipline (argv-strip rule, fail-closed on unreadable input, generation nonce) ·
retiring the PL-006a `setsid` MUST and HC-044a's per-run `.lock` · the two named regimes.
Claim 3 (the two withdrawn pass-2 amendments) clean.

## Finding 1 — "nothing writes the marker" is a snapshot, not a code fact · FIXED

**Reviewer:** the daemon **handler** path writes `HARMONIK_PROJECT_HASH` on every handler spawn
(`internal/daemon/workloop.go:1102-1104`, the hk-nvrvp fix, live, reaching both the direct-exec
and tmux-hosted branches). "Exists nowhere / empty set" is false as a statement about the code.

**Verified before accepting** (I do not stamp a reviewer's claim unchecked): the code does what
the reviewer says. Re-measured the box with the daemon running (pid 88350) — still **zero**
marked processes across 14 matches, because no handler run was in flight. So both statements
are true of different populations, and that reconciliation is the actual finding: the marker's
coverage is **inverted with respect to lifetime** — present on the short-lived handler
subprocesses, absent on the long-lived population (launch-layer agents, watchers, `br`) that the
sweeps enumerate and that actually leaks.

**Applied:** `process-lifecycle-design.md` §0.2 Finding 1 rewritten with the code citation, the
re-measurement, the lifetime-inversion framing, and a dated note recording what the earlier text
claimed. Downstream mention in `beads-integration-design.md` §1 narrowed to the claim that was
always doing the work there (the daemon does not set the marker on itself, so a `br` child
inherits nothing). B4-strict is unaffected — marker-on-handlers supports it and does not
resurrect B1/B2/B3.

## Finding 2 — the non-coverage row is too pessimistic · FIXED (the important one)

**Reviewer:** the `setsid` grandchild that actually leaks is orphaned to init — measured, pid
8610, a `comms recv --follow`, `PPID==1`, dead group leader, survivor of 44 reaped tmux
sessions — and `lifecycle.OSHandlerProcessLister.ListOrphanHandlerPIDs`
(`orphansweep.go:194-264`) is **origin-agnostic**, so marker write coverage (PL-006e(3), in
scope) plus the darwin `ps -E` read (B4, in scope) means *this design reaps it*. As written the
spec told implementers the real leak was unfixable, and invited closing hk-o7x4w as a known gap
at the moment the design closes it.

**Verified before accepting:** the sweep selects on `PPID==1` **and** a marker match and never
asks how the process became parentless — confirmed by reading it. On darwin it currently
`continue`s past every candidate because `ReadProcessEnviron` has no `/proc`, which is the D3
no-op and which B4 fixes. Independently, my own boot sweep this cycle found the same pid 8610
in a fleet-wide measurement taken before I saw the verdict.

**Applied:** `process-lifecycle-design.md` §6 restructured. The blanket "not achievable" row is
replaced by a split: **orphaned `setsid` descendant — COVERED**, conditional on the two in-scope
changes, with the live pid-8610 instance recorded; **still-parented `setsid` descendant (root
alive) — NOT COVERED**, which is the true residual. The follow-on paragraph now names precisely
what stands between the existing sweep and full coverage (its `PPID==1` pre-filter) and why
removing it is its own work. `handler-contract-design.md` §2(d) updated to state the HC-044 limit
at its true width.

**Taking the reviewer's irony as stated:** the honest non-coverage row was the least accurate
part of the work. A non-coverage row drawn wider than the evidence is its own failure mode — it
hides a fix behind a disclaimer. That is now written into §6 as a second-direction warning
alongside the original one.

## Raised by kilo during the same round, not by the reviewer

**The generation nonce cannot be minted where this pass proposed.** Pass 4 §8 risk 4 suggested
minting `HARMONIK_SESSION_GEN` per `harmonik start <role>`. A keeper restart never re-invokes
`harmonik start` — it is a `/clear` in the same pane driven by the same live agent process — so
that mint is constant across exactly the generations it exists to separate. Measured with three
same-pane watcher generations compared field by field. Written up in
`FINDING-generation-nonce-mint-point.md`; §8 risk 4 now marks the proposed mint point as wrong
and names the replacement (CLI stamps the generation at arm time from the harmonik-owned
`.managed` file). Verdict-independent — it does not touch either reviewed claim.

## Disposition

Two required changes applied, one self-found correction recorded. Advancing to **spec-draft**
(pass 5) per the captain's instruction. No second review round requested by the reviewer.
