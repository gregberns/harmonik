# Restructure Execution Plan — Changes From Review

What changed in `02-execution-plan.md` on 2026-09-07, and which finding each change answers. The review
is `03-review.md` (verdict APPROVE-WITH-CHANGES). The reconciliation with the sibling bus track is a
cross-plan decision recorded in both plans.

## Reconciliation with the bus track (cross-plan, not a numbered review finding)

Added the section "The sibling bus track lands into this module". It states the shared decision: the
bus lands FIRST as packages INSIDE this single `clean/` module — `clean/transport` (the Bus / Service /
Mount surface + in-mem router) and `clean/cli` (the shared env seam carrying `Env.Bus`) — NOT a separate
`libs/` module. The `libs/transport` + `tools/keeper` + separate-module split stays the END-STATE
promotion this plan already defers for every package. The two paths (`clean/transport`, `clean/cli`) and
the keeper sequencing (keeper is the late Track-B tail) are now named identically in both plans, so
neither depends on anything the other does not produce. The bus track needs only this plan's Phase-0
scaffold (task 10 done).

## Must-fix 1 — wall check is an allow-list, not a deny-list (review finding 1)

- Phase 0 step 3: rewrote `scripts/clean-wall.sh` from `grep -E '(internal|cmd)'` to an allow-list —
  any dependency under `github.com/gregberns/harmonik/` NOT under `clean/` is a breach. Named the four
  legacy prefixes (`evaltasks`, `scripts`, `test`, `tools`) the old deny-list was blind to.
- Phase 2 admission Hard gate 1: same allow-list form, so the entry check and the CI gate agree.
- Phase 0 proof-of-teeth (gate bullet + task 8): the planted breach is now a NON-core
  `tools/forbid-import` import, which exercises the real blind spot instead of the `internal/core`
  prefix even the buggy grep caught.

## Must-fix 2 — purity gate 3 adapter-shell exemption (review finding 2)

- Phase 2 admission Hard gate 3: added the explicit exemption. A single adapter file whose ENTIRE job is
  to wrap ONE effect behind the injected port may call that one effect directly (`substrate`'s
  `SystemClock.Now() { return time.Now() }` is the canonical case, so Port 0 no longer trips the gate it
  is built around). Domain code stays effect-free. The exemption is a stated rule, not a per-review
  wave-through.
- Per-commit reviewer purity bullet: mirrored the exemption so the reviewer applies the same rule.

## Must-fix 3 — D2 resolved by experiment, not policy (review finding 3)

- Added task 9 (Phase 0, size M): the legacy-lint-ratchet experiment — `git mv` one daemon-adjacent
  file out, run `make full`, confirm (a) it lints clean under `clean/.golangci.yml` and (b) no new
  finding fires in the files left behind, then revert.
- Added the matching Phase 0 gate bullet and folded it into the Phase 0 exit review bar.
- Rewrote the kill/stop "legacy lint ratchet" entry from a policy sentence ("freeze the allow list") to
  "resolved by task 9"; if red, the operator decides the remedy on real evidence.

## Must-fix 4 — a task lands the PRODUCTION vocabulary (review finding 4)

- Added task 14 (Phase 2, size M–L): build the clean vocabulary + the legacy↔clean adapter as reviewed
  production code that passes all three hard gates. The Phase-1 spike stays a throwaway that measures
  drag; it does not ship. Every spine port (tasks 15–17) now depends on task 14, and the Phase 2 order
  text says the spike does not ship a vocabulary.

## Must-fix 5 — Track B accounts for the `presence` tentacle (review finding 5)

- Track B "Then, in order": inserted step 3 "account for the `presence` tentacle" (keeper imports
  `core`, `dashboard`, `digest`, `presence`, `substrate`; `presence` pulls `eventbus` pulls `core`).
  Two routes: invert to a keeper-declared port (route a, keeps Track B spine-independent) or gate the
  move on Ports `eventbus` + `presence` (route b). Renumbered the later steps.
- Added task 24 (Track B step 3) and corrected task 27's (was 23) dependencies to include it.
- Added a Track B gate bullet for the presence edge.

## Must-fix 6 — un-serialize the DAG + split the queue task (review findings 6 and 8)

- Tasks 15 (`structuredlog`), 16 (`eventbus`), 17 (`queue` relocation) each depend only on task 14 —
  NOT on each other. Added a note that they carry no import edge between them and run in parallel,
  because throughput is the program's #1 risk.
- Task 21 (Track B step 0, extract `decide`) now depends on NOTHING in the clean module — it is
  in-place legacy refactoring and starts at once, no longer gated behind the Phase-0 scaffold.
- Split the old task 16: task 17 is the `queue` RELOCATION carrying the two-writer hazard behind the
  shim; task 18 fixes the `queue.json` two-writer hazard in place in clean as its own reviewed bead.
  Updated the "Notes on specific ports" queue bullet to match.

## Not changed (review findings left as-is on purpose)

- Finding 7 (promote a hermetic unit layer from soft signal to hard exit gate) was outside the six
  must-fixes assigned for this pass. The soft-signal framing and the "green in seconds" exit criterion
  are unchanged; an operator or a later pass can raise it to a hard gate.
