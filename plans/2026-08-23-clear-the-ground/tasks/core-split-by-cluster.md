---
id: core-split-by-cluster
title: Extract internal/core/redaction — the first package out of the flat core
type: task
priority: 1
labels: [core, architecture, clear-the-ground]
depends_on: [core-cluster-map, lint-rekey-exclusion-list]
blocks: []
workstream: W3
batch: 4
---

## Problem

`internal/core` is 450 flat files with no internal import boundary — measured 2026-08-24 with
`find internal/core -maxdepth 1 -type f -name '*.go' | wc -l`; an earlier revision said 449. Nothing stops any part of it
depending on any other part. The fix is to move files into real packages. This task moves the first
one.

Two attempts have failed. **Both respected "one package per landing" and both still failed**, so the
rule was never the problem.

**Measured, from the two stranded run branches.** Attempt 1 is five commits ending `a8472fc9e` on
`run/01a03380-280a-7407-8b26-67e3a9ab3d9a`: 330 files, +2591/-2305, of which **288 files are outside
`internal/core`**. Attempt 2 is four commits ending `011b9dedf` on
`run/01a033c8-ecd3-73eb-bb12-25983fc92795`: 335 files, +2776/-2351, of which **293 are outside
`internal/core`**. Each attempt created exactly one new directory, `internal/core/ledger`. The large
diff is not extra scope. It is the cost of renaming `core.X` to `coreledger.X` at every use site.

**Root cause 1 — the package chosen.** `core/ledger` has the smallest outgoing dependency count in
the map, so the map's own "Move-order consequence" paragraph points an implementer straight at it.
The map never ranks by *incoming* references, and ledger is the worst package in the repo by that
measure: `BeadID` alone appears at 1,486 selector sites, and **283 files outside `internal/core`
name a ledger symbol**. Of attempt 2's non-core files, 161 are in `internal/daemon`, the package two
other lanes are actively rewriting.

**Root cause 2 — the window.** Attempt 1 spans 51 minutes of commits plus four review rounds;
attempt 2 spans 104 minutes plus three. Through both windows the integration branch took about one
commit per hour. Attempt 1 died on a rebase conflict in `internal/runmerge`. Attempt 2 rebased
cleanly and then failed `go vet`: `internal/runmerge/residualdeltamerge_test.go` names `core.BeadID`,
and that file did not exist when the run started. A repo-wide rename cannot outlast the interval
between other sessions' landings. The escape — leave an alias behind so old code keeps compiling —
is closed by this task's own no-shim rule, and a reviewer correctly rejected it on attempt 1.

**Root cause 3 — the map is not executable.** The map assigns `core/ledger` three files. Those three
could not move alone: `BeadRecord` needs `DependencyEdge`, and the status-narrowing method needs its
local `CoarseStatus`. The implementer had to pull `dependencyedge.go` and `edgekind.go` (mapped to
`workflow`) and `harmonikwritestatus.go` (mapped to `workspace`) across cluster lines, and decided
that inside the landing. **Every cluster in the map sits in one strongly connected component**, so
no cluster in it is landable as written. The map is a good inventory and a bad move order.

**Root cause 4 — comments were displaced by the move and then discarded by the repair.** Measured
2026-08-24 across attempt 1's five commits. `internal/queue/resume.go` carries a 27-line
`ResumeFromFailure` doc comment: the idempotent-no-op contract and its spec references. The move
commit (`58940d611`, "move ledger vocabulary into its own package") kept all 27 lines byte-identical
but left them tab-indented **inside** the `import (…)` block, so the file did not compile. The next
commit (`829001fb3`, "repair ledger split gate failures") deleted the misplaced block wholesale to
make the package build and hand-wrote a three-line summary in its place. **That is where 24 lines
went: 27 down to 3.** An earlier revision of this file said "about 21" and blamed the move; the move
displaced the comment, the repair destroyed it.

The restore is a third commit, `83f1dee00` ("remove ledger split bridges"), which put all 27 lines
back byte-identical. It is not `a8472fc9e`, which an earlier revision named: `a8472fc9e` touches 38
files but not `internal/queue/resume.go` at all. Attempt 2 never had this defect — all four of its
commits carry the full 27 lines. Each repair round lengthened the window that then lost the race.

**The lesson is about the repair, not the move.** A mechanical rewrite that strands a comment inside
an import block produces a file that will not build, and the fastest way to make it build is to
delete what is in the way. That is the moment to watch.

## Scope

**Exactly one package: `internal/core/redaction`.** Nothing else moves. The other packages are
follow-on tasks and are named at the end of this section.

### The files that move

Three files, out of `internal/core/` and into `internal/core/redaction/`:

- `internal/core/redaction.go` — `RedactedSentinel`, `RedactByFieldName`, `redactionCommonPrefixRe`.
- `internal/core/redactionregistry.go` — `RedactionRegistry`, `NewRedactionRegistry`, and the methods
  `RegisterPattern`, `RedactionMiddleware`, `allPatterns`.
- `internal/core/redaction_prop_test.go` — a white-box `package core` test that names only the five
  exported redaction symbols. It becomes `package redaction` with no other edit.

`internal/core/evinv006_redaction_sensor_hqwn52_test.go` **stays where it is.** The name is a trap:
it references no symbol from either moving file. It exercises `ErrSecretPrefixField` and the
unexported `scanConstructors`, both declared in `internal/core/eventregistry.go`. It does not name
the exported `ScanRegisteredPayloadsForSecretFields`, as an earlier revision of this file said; that
matters only because an unexported symbol is one more reason the test cannot leave `package core`.

### Why this package is first

Measured against the current worktree:

- **Nothing that stays in `internal/core` references it.** Only the three moving files name any
  redaction symbol. So not one file left behind is edited, no residual-core lint identity changes,
  and the question of whether `internal/core` may import a child package never arises.
- **It references nothing that stays.** Its only imports are stdlib. There is no import cycle and
  therefore no need for a bridge, a dot-import, or a temporary alias.
- **20 files outside `internal/core` reference it, at 50 sites** — against 283 files for ledger. The
  rename fits in a window short enough to land between other sessions' commits, which is the property
  both earlier attempts lacked.
- It is the largest subset of the map's proposed `core/observe` cluster that has both properties.
  The rest of that cluster — `trace.go`, `evidence.go`, `tracecontext.go` and the metric files —
  carries dependencies back into files that stay, so it cannot come now. `disklowpayload_hksxlb.go`
  has no such dependency, but it is still left out: `DiskLowPayload` is registered by
  `internal/core/eventreg_hqwn59.go`, so taking it would force an edit to a file that stays and would
  cost the zero-edit property this whole choice rests on.

**Count the consumers with a build-tag-blind method.** A type-checked pass over the default build
finds only **14 of the 20**, because six of them sit behind `//go:build scenario`: the two
`cmd/harmonik/scenario_decisions_*` tests, `internal/daemon/epiccompleted_scenario_hktfxjp_test.go`,
`internal/daemon/scenario_decisions_restart_s5_hkqed_test.go`,
`internal/daemon/scenario_terminated_locked_hjvl4_test.go` and
`internal/keeper/scenario_decisions_orphan_reap_s7_hk061_test.go`. An earlier revision said 15 and
counted a `DiskLowPayload` consumer, which is not one of the 20 — no file in the list names
`DiskLowPayload`. A tool that cannot see the scenario tier will leave those six broken, and a compile
break in a file the run never loaded is exactly how attempt 2 died. Use `git grep`, or load with the
`scenario` tag set.

Two production files is a small package. It is a real concern with a name, not a leftover: the
handler-side boundary in `internal/handlercontract` already re-exports it as its own thing.

### The consumers to rewrite

Twenty files. `internal/daemon` (10): `bootreconcile_test.go`,
`bootreconcile_unreadable_registry_test.go`, `decisionshandler_k4_kba_test.go`,
`epiccompleted_scenario_hktfxjp_test.go`, `resume_reconcile_live_run_test.go`,
`run_registry_has_no_writer_test.go`, `scenario_decisions_restart_s5_hkqed_test.go`,
`scenario_terminated_locked_hjvl4_test.go`, `stallfeed_test.go`,
`universal_run_registry_readers_test.go`. `internal/eventbus` (3): `busimpl.go`, `busimpl_test.go`,
`hc034_no_secret_in_event_log_test.go`. `internal/handlercontract` (2): `redaction.go`,
`redactionregistry.go`. `cmd/harmonik` (2): `scenario_decisions_gate_rz4_test.go`,
`scenario_decisions_list_1vl_test.go`. `internal/keeper` (2):
`scenario_decisions_orphan_reap_s7_hk061_test.go`, `watcher_decision_exempt_test.go`.
`internal/presence` (1): `reaper_test.go`.

Re-derive the list before you start rather than trusting it — another session may have added a
consumer. `git grep -nE 'core\.(RedactionRegistry|NewRedactionRegistry|RedactByFieldName|RedactedSentinel)[^A-Za-z0-9_]'`
outside `internal/core` gives it.

### The rename mechanism — run a script, do not hand-edit

Both earlier attempts died on the clock, not on the design. `.harmonik/batch-gate/core-rename.sh`
exists to remove the clock as a factor. **Read it before you run it.** It is short and it is the
intended mechanism for the consumer rename in this task.

What it does, in order: move the cluster files with `git mv` and rewrite their `package` clause;
find the consumer files with `grep -rl` on the symbol names, which is build-tag blind and therefore
sees the scenario tier; rewrite `core.X` to `<alias>.X` with `gofmt -r`, one rule per symbol; undo
`gofmt -r`'s one known miss, which is that it rewrites composite-literal KEYS that must stay bare;
add the import; then run `goimports`, `.tools/gci` and `.tools/gofumpt` so the result passes this
repo's format gate.

The session that wrote it reports a whole-cluster run in about 46 seconds, against a hand pass that
took about three hours, with 311 of its 335 files byte-identical to the hand result. Those figures
are that session's, not a measurement in this file. Re-time it yourself; the number that matters is
whether the landing fits inside the roughly one-commit-per-hour gap on the integration branch.

**What it does not cover. None of these is optional work you may skip because the script skipped it.**

- **It is written for the `ledger` cluster, not for `redaction`.** Its `MOVED`, `SYMS`, `PKG` and
  `ALIAS` values are all ledger. Re-target all four before running it, or it will move files this
  task forbids you to move.
- **24 of the 335 files did not match the hand pass.** The session that ran it did not enumerate
  them. Treat the script's output as a draft to read, not a result to commit.
- **It compiles nothing and tests nothing.** It is a text transform. `go build`, `go vet -tags=scenario`
  and `make full` are still yours, and `go vet -tags=scenario` is the one that catches the scenario
  tier that killed attempt 2.
- **It does not touch doc comments or doc links.** Criterion 4 below requires the qualifier change at
  five measured doc-link sites and nothing else. Do that by hand and audit it.
- **Its last step edits `.golangci.yml`, and this task says that file should not change.** The script
  adds a depguard allow entry so `internal/core` may import its own subpackage. Decision 3 below says
  this task needs no such entry, because nothing left behind imports `redaction`. Delete that step or
  do not run it. If you find you need it, Decision 3 is wrong and that is a finding to report.
- **The script is gitignored** — `.gitignore` excludes `/.harmonik/*`. It is machine-local and it is
  not in a fresh clone. If it is absent, the mechanism is still right; write it again from the
  description above rather than falling back to a hand pass.

### Decisions already made — do not re-open any of these mid-landing

1. **Package name `redaction`, import alias `coreredaction`.** This matches the `coreledger`
   spelling both earlier attempts used and both reviewers accepted.
2. **`internal/handlercontract/redaction.go` and `redactionregistry.go` are not shims you created.**
   They are a spec-blessed boundary (`specs/handler-contract.md` §4.7.HC-030, HC-032) that predates
   this task. Repoint their alias at the new package. Do not delete them and do not widen them.
3. **`.golangci.yml` should need no change.** Consumer `depguard` allow entries are prefix matches —
   the `dispatch` rule says so in its own comment — so `internal/core` already covers
   `internal/core/redaction`. The `core` rule's glob `**/internal/core/**` covers the new directory
   and its allow list already carries `pgregory.net/rapid`, which the property test needs. Verify
   this rather than assume it. If depguard turns out to match exactly, one added allow entry is the
   only change permitted, and say so in the commit body.
4. **`tools/lintreport/allow.txt` should need no change.** The two tolerated findings in
   `redaction_prop_test.go` are keyed by content, and moving a file without touching a symbol body
   leaves the key byte-identical. The stale path in the trailing comment is an aid, not part of the
   key — the file header says so. Leave it stale.
5. **A package comment is required and is the one piece of new prose allowed.** Put it in a new
   `internal/core/redaction/doc.go` or above the `package` clause of `redaction.go`. One or two
   sentences.

### Follow-on tasks — do not do them here

The rest of the split is a separate task per package, and the order is settled by measurement, not
by the map.

**None of the five names below is a task file yet.** Checked 2026-08-24: no file in this directory
carries any of these ids, and no other plan document mentions them. They are named work, not
scheduled work. Read a slug here as a description of a job somebody still has to write up, never as
a promise that a brief exists. If you need one of them done, write the task file first.

- `core-split-metrics` — not written. The next package that has been measured clean.
- `lint-identity-survives-qualifier` — **not written, and stated as a prerequisite.**
  `tools/lintreport` `findingDigest` hashes the symbol body, so inserting `corex.` mints a new
  identity and the ratchet reads it as new debt. Attempt 2 built 99 lines of this inside the split
  landing.
- `core-child-import-policy` — **not written, and stated as a prerequisite.** May residual
  `internal/core` import a child at all? Attempt 2 answered this in flight by adding an allow entry
  and `list-mode: strict` to the `core` depguard rule.
- `core-cluster-map-revision` — not written. Re-cuts the map into landable slices ranked by consumer
  file count, and fixes the one file the map has already fallen behind on:
  `internal/core/runworktreerecovery_hknqvqr.go` exists and the map names no owner for it, so the
  count is 240 production files and not the 239 the map claims.
- `core-comment-review` — not written. Named by `PLAN.md` §W3.3.

The two marked as prerequisites bind any landing where a file that STAYS in `internal/core` must
gain a qualifier. **This task is not such a landing** — nothing left behind is edited, which is the
whole reason `redaction` goes first. So neither prerequisite blocks this task, and neither has to be
written before it runs. They block the ones after it.

## Done when

Every item is structural. **No item is a line count, and the size of the diff is not evidence of
anything.**

1. `internal/core/redaction/` exists and holds `redaction.go`, `redactionregistry.go`,
   `redaction_prop_test.go`, and at most one new `doc.go`.
2. `internal/core/redaction.go`, `internal/core/redactionregistry.go` and
   `internal/core/redaction_prop_test.go` no longer exist at the root of `internal/core`, and
   `internal/core/evinv006_redaction_sensor_hqwn52_test.go` still does.
3. **The boundary holds in both directions.** No file remaining in `internal/core` imports
   `internal/core/redaction`, and `internal/core/redaction` imports nothing under
   `github.com/gregberns/harmonik`. Both are one `go list -deps` away.
4. **The doc comments moved byte-identical.** A comment-only diff of the three moved files against
   their pre-move source is empty. The only comment change permitted anywhere in the landing is the
   qualifier inside a doc link — `[core.RedactionRegistry]` becomes `[coreredaction.RedactionRegistry]`
   — at the five measured sites: three in `internal/eventbus/busimpl.go` and two in
   `internal/handlercontract/redactionregistry.go`. Run the audit and say in the commit body that
   you ran it.
5. No temporary bridge, alias, or dot-import survives the landing, because none was needed. If you
   find yourself wanting one, the landing has gone wrong — stop and report it rather than adding one.
6. `tools/lintreport/allow.txt` and `.golangci.yml` are unchanged, or the commit body names the one
   permitted change and why measurement 3 or 4 above required it.
7. `make full` is green. It is the merge decision and it never approves on a timeout, an OOM, a
   compile failure, or a passing retry.
8. The landing is one commit, or two if the package comment is separated out.

## Limits

- **A move is a move.** Comment edits are a separate landing, and `PLAN.md` §W3.3 already puts the
  comment re-examination after the moves as a distinct step. The measured instance is
  `internal/queue/resume.go`, whose `ResumeFromFailure` contract comment went from 27 lines to 3
  during attempt 1 — not in the move itself, but in the commit that repaired the move's build
  failure. If a mechanical rewrite strands a comment inside an import block, put the comment back
  exactly as it was. Do not re-summarise it, do not shorten it, and do not improve it. Deleting a
  misplaced comment is the quickest way to make the package compile, and it is the wrong one.
- **Do not combine a move with a change.** Attempt 1 also deleted `emitImplPresence` from
  `internal/daemon/workloop.go` — pre-existing dead code, and a reviewer correctly blocked it. Dead
  code you notice becomes its own bead.
- **Move only the three named files.** If a fourth file looks like it has to come too, that is a
  finding, not a decision: stop and report it. Crossing a cluster line inside the landing is what
  made both earlier attempts unreviewable.
- **Do not extract a second package.** The whole point of this task is that the landing is small
  enough to survive other sessions writing to the same branch while it runs.
- Do not touch `internal/core/ledger` or either stranded run branch. Those two attempts are
  preserved and reviewed; a third attempt at ledger is not this task.
