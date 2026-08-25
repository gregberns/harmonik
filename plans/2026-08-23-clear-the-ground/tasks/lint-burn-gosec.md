---
id: lint-burn-gosec
title: Fix the 182 security findings the build currently tolerates
type: task
priority: 1
labels: [lint, gate, gosec, clear-the-ground]
depends_on: [lint-rekey-exclusion-list]
blocks: [lint-burn-errcheck, lint-burn-gocritic]
workstream: W1
batch: 3
---

## Problem

`tools/lintreport/allow.txt` holds 956 tolerated findings. **182 of them are `gosec`** — measured
2026-08-24 with `awk -F'\t' '$2=="gosec"' tools/lintreport/allow.txt | wc -l`. These are security
findings the build is told to ignore, which is why the plan names this one first.

Where they sit:

| Package | Entries |
|---|---|
| `internal/daemon` | 109 |
| `cmd/harmonik` | 26 |
| `internal/runloop` | 8 |
| `internal/queue` | 5 |
| `internal/runmerge` | 4 |
| 21 other packages | 3 or fewer each |

**121 of the 182 are in `_test.go` files and 61 are in production code.** The production 61 are the
security surface; the test 121 are mostly file and subprocess handling in fixtures. The densest
single files are `internal/daemon/bandwidthtuner_test.go` (12), `internal/daemon/eagerfill_em063_test.go`
(10), `internal/daemon/quiesce_test.go` (6) and `internal/daemon/pasteinject.go` (6).

Sampled rule codes across `internal/daemon`, `cmd/harmonik`, `internal/core`, `internal/queue`,
`internal/runloop`, `internal/workspace` and `internal/watch`: G304 file inclusion via a variable
(39), G204 subprocess launched with a variable (37), G301 directory permissions (37), G306
`WriteFile` permissions (36), G115 integer overflow on conversion (4), G302 file permissions (3),
G101 hardcoded credentials (2), G104 unhandled error (2).

The permission findings (G301, G302, G306 — 76 of the sample) are the mechanical majority: a mode
constant changes. G204 and G304 need a judgement per site about whether the input is attacker-reachable.

## Scope

- `tools/lintreport/allow.txt` — the 182 lines whose second tab-separated field is `gosec`. The
  third field is a `# <path>:<line> <symbol>` comment that says where each one lives.
- The Go files those comments name.
- Nothing else. This task does not change `scripts/lint-allow.sh`, the ratchet, `tools/lintreport/`,
  or `.golangci.yml`.

Suggested order, because it puts the security value first and the risk last:

1. The 61 production entries, starting with `internal/daemon/pasteinject.go`,
   `internal/daemon/verdictexecutor_rc025a.go` and `internal/runloop/scenariogate.go`.
2. The permission findings in test fixtures — mode constants, no behaviour change.
3. The remaining test-file G204 and G304 entries.

## There is already work on this, and it is stranded

An attempt on 2026-08-24 ran five iterations. Iteration 1 was rejected for relocating tolerance into
144 inline `#nosec` comments — correctly rejected, and do not repeat it. **Iteration 4 is good work**:
commit `352e00e6b` removes 49 gosec identities with no `//nolint`, no `#nosec` and no new allow.txt
row, plus real permission narrowing (26 directories `0o755`->`0o750`, 19 files `0o644`->`0o600`) and
some genuine `errcheck` repairs.

**That commit is on no branch anybody merges.** Measured 2026-08-24: it is not an ancestor of
`work/charlie-batch-1` or `work/alpha-integration-merge`. It sits alone on
`run/01a03546-c530-7ead-985b-424eb447ac01`. The gosec row counts are 182 on the integration branch,
181 on the batch branch, and 133 on that run branch.

So start by rescuing it rather than by redoing it. Cherry-pick or re-apply `352e00e6b`, confirm the
count moves 182 -> 133, and carry on from there. Check what you rescue by reading the diff, not by trusting a
label: the rejected `#nosec` iteration was not committed to that branch, so what you find there
should contain no `#nosec` and no `//nolint` at all. If it does, you have the wrong commit — stop.

## How these actually clear — measured, not guessed

Taken on 2026-08-24 by building scratch modules outside the repo and running this repo's own pinned
`golangci-lint` v2.3.0 against each candidate. `go.mod` is go 1.25, so `os.Root` is available.

| Change | Result |
|---|---|
| `os.ReadFile(filepath.Clean(p))` | **clears G304** |
| `os.OpenRoot(dir)` + `r.Open(name)` | **clears G304** |
| `_ = someCall(ctx, deps)` | **DO NOT USE.** Clears G104 and immediately trips `errcheck` |
| const binary path in `exec.Command` | **clears G204** |
| extract a helper taking the args as parameters | clears G204 — **do not use for this purpose, see Limits** |
| allowlisting a binary via `switch` into a local | still flagged |
| `filepath.Clean` on a subprocess ARGUMENT | still flagged |

**G304 is the big one.** It is the largest single bucket — a sample on 2026-08-24 put it near 40 of
the 133 residual identities, and the exact figure is yours to measure, not to inherit. `filepath.Clean`
clears it in one line.
**Prefer `os.Root`/`OpenRoot` where the path is genuinely rooted in a known directory.** `Clean` is a
weak sanitizer — gosec trusts it, but it does not stop `../` escaping a root, so a `Clean` that
silences the linter can leave the actual traversal bug in place. Clearing the finding and fixing the
bug are different acts; do the second one where it is cheap.

The permission findings (`G301`, `G306`) are ordinary work with no judgment in them.

**`G104` is a trap and the obvious fix is wrong.** `.golangci.yml` sets `errcheck: check-blank: true`,
so `_ = someCall()` silences gosec and raises an `errcheck` finding in the same breath — a NEW finding
outside the allow list, which fails done-when 2 and which Limit 1 forbids you to add. The reviewer
skill states this outright: "`_ =` does not satisfy check-blank". The only honest dispositions are to
handle the error, or to use the house `Close()` idiom where that is what the site is
(`defer func() { err = errors.Join(err, f.Close()) }()` — see
`.claude/skills/agent-reviewer/SKILL.md` §Deferred `Close()` for the three landed forms). Note that
the existing `//nolint:errcheck,gosec` comments on the background-loop calls in `internal/daemon`
tests are the escape hatch this task forbids you to ADD; leave the ones already there alone and do
not copy the pattern.

The population that resists all of the above is **G204 with a tainted argument** — a subprocess whose
arguments derive from a variable. Those, plus the handful of sites whose whole purpose is to run an
arbitrary binary, are what the residual set is for.

## Done when

1. `awk -F'\t' '$2=="gosec"' tools/lintreport/allow.txt | wc -l` prints `0`, OR it prints the size
   of the **named residual set** and every remaining row is listed in your commit body with a
   one-line reason. Those are the only two acceptable endings.

   **This criterion was rewritten on 2026-08-24 because the old one was not reachable and cost an
   implementer five iterations.** It demanded `0` while the Limits below forbid every escape hatch,
   and a small number of findings can be cleared by neither. The residual set is the repair. It is
   the same disposition this file already gives the `gocognit` co-tenants: name the row, say why,
   move on.

   **Two hard constraints on the residual set, so it cannot become a place to put the work you did
   not do:**

   - **Only `G204`-with-a-tainted-argument rows and arbitrary-binary-by-design rows may enter it by
     default.** A `G304`, `G301`, `G306` or `G104` row in your residual set is a defect in the
     submission and the reviewer should reject it — each of those has a disposition stated above,
     `G104`'s being the paragraph under the table rather than a row in it. This is mechanically
     checkable: name the rule for every row you list.
   - **The small tail — `G115`, `G101`, `G302`, `G404` — is not covered by either.** Roughly a
     dozen rows fall outside both the table and the default residual set. That is not a licence to
     ignore them and it is not a reason to stop. Fix them where a fix is obvious (`G115` is an
     integer conversion that wants a bounds check; `G101` is usually a false positive on a constant
     NAME that looks like a credential). Where it is not obvious, a row may enter the residual set
     with its rule named and a sentence saying why — the same bar as everything else. Say in your
     commit body how many came in by this route. If it is more than a handful, that is a finding
     about this task file and worth reporting back rather than absorbing.
   - **The set may not exceed 20 rows.** If your honest set is bigger than that, the answer is not a
     bigger set — stop and say so, the way the previous attempt correctly did. That number is a
     ceiling chosen to be clearly above the handful of genuinely-undecidable sites and clearly below
     the count that would let this criterion be satisfied by giving up.

   Without both of these the criterion is tautological: the `wc -l` prints a number for ANY number,
   and the ratchet in done-when 3 passes on a one-row shrink, so "remove one row and write reasons
   for the rest" would technically satisfy it. It does not.
2. `make lint-allow` exits 0 with the tree in that state — no `gosec` finding falls outside the list,
   because there is no `gosec` entry left in it.
3. `scripts/lint-allow-ratchet.sh` exits 0. Both of its windows must pass: the identity set only
   shrank in the working tree, and it only shrank in the commit.
4. `make fast` is green, and every test that covered an edited file still runs and still passes. A
   finding removed by deleting or skipping the test that reached it is not removed.

## Limits

- **Never widen the allow list.** This is the standing rule of the program. An entry leaves the list
  because the finding is fixed. Do not re-key around it, do not add a `//nolint` directive, and do
  not delete the code's doc comments to satisfy a metric. `.golangci.yml` is not an escape hatch
  either: excluding `gosec` from a path there is the same act one file over.
- **Do not extract a helper for the sole purpose of defeating the linter.** gosec's taint analysis
  works inside one function, so moving a subprocess call into a helper that takes its arguments as
  parameters makes a `G204` finding disappear without changing what the program does. That is a
  suppression spelled differently, and the no-escape-hatch rule above is about the ACT, not the
  syntax. If the extraction makes the code better on its own terms, do it and say so. If its only
  effect is a quieter linter, put the row in the residual set instead and say why. Refusing the
  cosmetic version is what keeps the residual set honest.
- **Do not delete or skip a test to remove a finding.** A fixture that reads a file by a computed
  path can take a checked, rooted path. It does not have to stop existing.
- **Do not use `-write`.** `go run ./tools/lintreport -allow ... -write` rebuilds the whole list from
  the current findings, so it silently ADDS anything new alongside the removals. Remove lines by
  hand and prove the removal with `make lint-allow`.
- **Co-tenants re-fingerprint.** The key is a hash of the linter, the normalized message, and the
  formatted enclosing declaration — see `tools/lintreport/main.go` `findingDigest` and `enclosing`.
  Editing a declaration changes the key of EVERY tolerated finding inside it. **43 of these 182
  entries share a declaration with a different linter's entry** (17 with `errcheck`, 8 with
  `gocritic`, 4 with `unparam`, 4 with `gocognit`, 3 with `errorlint`, the rest scattered). When you
  fix one of those, the co-tenant goes red. Fix the co-tenant too and delete both lines. **Never
  paste the new hash into the list** — the count did not rise, but that is still a widening and the
  ratchet is right to refuse it.
- **`lint-burn-gocritic` runs after this task** because those two share 8 declarations, and two lanes
  editing the same declaration at once each re-fingerprint the other's tolerated findings — both
  gates go red with no legal repair. Finish and land this one first.
- **Lesser overlaps have no hard ordering, so check before you edit a shared declaration.** This task
  shares 4 declarations with `lint-burn-unparam` and 3 with `lint-burn-error-returns` (the `errorlint`
  rows). Neither pair is serialized. Before editing a declaration, grep `tools/lintreport/allow.txt`
  for its file and name, and see whether another lane owns a row inside it.
- **Four of those co-tenants are `gocognit` entries.** `gocognit`, `cyclop` and `funlen` are
  deliberately last in this program: they should vanish because a function got smaller, not because
  somebody fixed them in place. If a `gosec` fix in such a declaration would drag you into
  restructuring the function, stop and leave that one row for the run-machine lane. Say which rows
  you left and why.
