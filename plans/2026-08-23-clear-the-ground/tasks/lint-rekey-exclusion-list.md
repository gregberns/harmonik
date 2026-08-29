---
id: lint-rekey-exclusion-list
title: A tolerated lint finding must keep its identity when its file moves to another package
type: task
priority: 0
labels: [lint, gate, clear-the-ground]
depends_on: []
blocks: [lint-ratchet-mutation-proof, core-cluster-map, core-split-by-cluster, harnesspick-extract, handlerpause-extract, cli-extract-logic, lint-burn-context-plumbing, lint-burn-errcheck, lint-burn-error-returns, lint-burn-exhaustive, lint-burn-forbidigo, lint-burn-gocritic, lint-burn-last-three-linters, lint-burn-prealloc-unconvert, lint-burn-revive, lint-burn-unparam, lint-burn-unused]
workstream: W1
batch: 1
---

> **A `blocks:` line in this front matter enforces nothing. Dispatch reads bead edges.** The first
> bead for this task, `hk-lint-rekey-exclusion-list-gfry7`, carried none of these edges and was closed
> done on work that did not deliver the property below. The live bead is
> `hk-lint-rekey-exclusion-list-t9ebz` and it carries them. If you add a `blocks:` entry here, add the
> edge with `br dep add` in the same change.

## Read this first: half of this task already shipped

**The content-key migration LANDED in `fbe830453 fix(lint): key tolerated findings by content
identity`.** An earlier version of this file described the world before that commit and asked you to
build what it built. Do not rebuild it. Measured against the tree on 2026-08-25:

- `tools/lintreport/allow.txt` carries **0 rows in the legacy `<path><TAB><linter>` format** — every
  row is already keyed by content identity. The row COUNT depends on your branch — **923 at the
  post-merge integration tip `f42496152`**, and it was 956 at `eae64d47a` before the batch landed.
  Measure your own and see "Done when" item 4.
- `findingDigest`, `sourceFile.enclosing`, `identityNode` and `nodeName` all exist in
  `tools/lintreport/main.go`. Resolving a finding to its enclosing declaration is **not** work you
  have to write.
- The scheme that shipped is the one an earlier version of this file called "option B": a content
  hash of the finding. **That decision is made. Do not re-open it.**

**What did NOT ship is the property this task is named for.** Read the next section; it is the whole
task.

## The property

> **Moving a file to another package — including re-qualifying its type references at the moved
> sites — must not change the key of a tolerated finding.**

That sentence is the requirement. An earlier version of this file said "the key must contain neither a
file path nor a package name", and that phrasing is unusable in both directions. The key **is** a
sha256 digest, so it literally contains neither. And its inputs cannot exclude package names, because
every gofmt-printed Go declaration carries stdlib qualifiers and errcheck's own message text embeds
names like `os.Remove`. Invariance under a move is the testable property; absence of a substring is
not.

**The path itself is already invariant** and has been since `fbe830453`. `findingDigest` hashes only
`linter + "\x00" + normalizedText + "\x00" + body`; the path survives only in the trailing
`# path:line symbol` comment, which `readAllow` strips at the first `#`. So a pure `git mv` with no
content change already keeps its key. **That is not the broken case and proving it proves nothing.**
The broken case is the move that re-qualifies.

## Mechanism: the package name reaches the key by five routes

Measured 2026-08-25 with the repo's own binary and cross-checked against `go/types` ground truth.
Routes 1 to 4 must be closed; route 5 must deliberately be left open. Do not re-derive these.

**Every `989 findings` and `934 identities` figure in this file is an integration-tip (`eae64d47a`)
reading.** Row counts are branch-qualified throughout because they differ; these are not, and they
differ too. Treat them as evidence that the scheme works, not as numbers to reproduce.

1. **The printed enclosing declaration.** `enclosing` returns `format.Node` bytes and they go
   straight into the hash, so a qualifier written in the declaration is literal text in the hashed
   bytes. Measured: same file, line, linter and message, changing only `*RunHandle` to
   `*runregistry.RunHandle`, the digest changed.
2. **The linter's own message.** `normalizedText` is only `strings.Join(strings.Fields(text), " ")` —
   a whitespace collapse. Measured with `.tools/golangci-lint -E errcheck`: `Text` comes back as
   ``Error return value of `os.Remove` is not checked``. depguard messages quote the import path.
   Fixing the AST alone leaves this route open.

   **And a message can carry an ABSOLUTE LINE NUMBER, which falsifies this scheme's central claim.**
   `nilerr` emits ``error is not nil (line 195) but it returns nil``. That number is in `Text`, so it
   is in the digest. **"Moving unchanged code does not change the hash" is false for any
   line-number-bearing message** — and the trigger is not a package move, which is rare and
   deliberate, but ANY edit that inserts or deletes a line ABOVE the declaration. Measured: two rows
   whose printed declaration is byte-identical before and after a move re-keyed anyway, purely
   because `(line 73)` became `(line 74)`. Filed as `hk-k11fp`.

   **Scoped, and the scope is small: `nilerr` is the only linter that does this.** A scan of a real
   whole-tree lint capture found 40 messages carrying `line N` and every one is `nilerr`; the tree
   holds 16 `nilerr` rows. **The repair is to normalize absolute line references out of message text,
   the same way whitespace is already collapsed. DO NOT fix it by dropping the message from the
   digest** — the message is load-bearing discrimination, and it is what tells two findings from one
   linter on one declaration apart. **This task absorbs that repair**, because the message channel is
   already route 2 and regenerating rows you know will re-key on the next unrelated insertion is
   wasted work.
3. **Import paths, by two separate mechanisms.** `identityNode` accepts `*ast.ImportSpec`, so a
   finding on an import line hashes the import-path string literal — **3 depguard rows** are this
   shape. Separately, `enclosing`'s `best == nil` fallback concatenates **every** declaration in the
   file, import block included, so any import-list change re-keys those rows — **14 rows** resolve to
   `file scope`.
4. **Doc comments, and nobody named this one.** `main.go` parses with `parser.ParseComments`, and
   `format.Node` prints the `Doc` and `Comment` fields on `FuncDecl`, `GenDecl`, `TypeSpec`,
   `ValueSpec` and `Field`, so a doc comment is inside the hashed bytes. **34 of 989 findings sit in
   a declaration whose comments carry a module-internal qualifier** — real doc links and prose that a
   move rewrites, such as `// Use [core.WorkflowModeDot] for the` and
   `// HandleCrewStart implements crewrun.CrewHandler.HandleCrewStart.` A pure comment edit re-keys
   the finding too, which is churn the list does not need. **Exclude comments from the printed body.**
   Measured cost: 934 identities before, 934 after, 0 merge groups. The one linter whose finding IS
   about a comment is `revive`'s `exported` / `package-comments`, and its message carries the symbol
   name, so discrimination survives — the zero-merge number is the proof, not the argument.
5. **String literals, and this route is deliberately NOT closed.**
   `internal/daemon/t3_exploratory_test.go` holds both
   `"T3-02: daemon.Start did not return within %s after cancel"` and `"daemon.pid"`. A thorough move
   rewrites the first and must not rewrite the second, and nothing can tell them apart. Blanking
   literals was measured and is free today (934 identities, 0 merges) and is still **wrong**: it would
   let a genuine content change — swapping the binary in an `exec.Command` — ride an old exemption,
   which is the gate-bypass direction. **The line to hold: exclude comments because they can never
   change what a linter flags; keep string literals because they can.**

## What neutralization does NOT cover — measured, so you do not mistake a boundary for a bug

The 14 findings the judge refuses at the batch tip were traced to root cause on 2026-08-25.
**Expect 13 to be covered and ONE not to be** — but by three different routes, not one. The qualifier
walk covers 7; route 2's line normalization covers 3; route 4's comment exclusion covers 3. An
implementer who believes the qualifier walk covers all 14 will regenerate, see six still refused, and
not know whether the tool has a bug or has reached its limit.

| n | cause | covered by this task? |
|---|---|---|
| 7 | pure re-qualification of a moved type | **yes** — neutralization plus regenerated rows |
| 3 | a **doc comment** rewritten to say `runregistry.X` | **no** — stripping walks `*ast.SelectorExpr`, and comment text is not an expression. Route 4 excludes comments from the body, which removes them from the hash entirely and therefore also covers this — confirm that is what happens rather than assuming it |
| 3 | a **line-number-bearing `nilerr` message** | **yes, but only via the route-2 line normalization above** — not by the qualifier fix |
| 1 | a **genuine API change** — `handle.aborted.Store(true)` became `handle.MarkAborted()` when an unexported field had to become an exported method on leaving the package, in `stalewatch.go` `checkRun` | **no, and it should not be.** The code really changed. This row must be re-keyed by hand and the change named |

**Neutralization is ONE-SIDED, and that is why a half-measure fails.** The rows in `allow.txt` hash
UN-neutralized text, and that text is full of qualifiers unrelated to any move — `core.RunID`,
`core.BeadID`, `queuewiring.NewQueueStore()`, `eventbus.NewBusImpl()`, `json.Marshal`. Strip on the
post-move side only and you produce text matching **neither** the old tree nor the new one. Measured:
**0 of 14** matched their stored rows that way.

**Applied to BOTH sides — meaning the rows are regenerated under the new rule — 10 of the 14
declarations become byte-identical across the move.** The design is sound. It cannot work without
rewriting the rows.

**So there is no partial landing.** Shipping neutralization without regenerating turns a 14-finding
failure into roughly a 900-finding one, because it changes the digest of essentially every finding
whose declaration contains any qualifier at all.

## The existing tests do not cover this, and one of them covers less than its name says

`TestFindingIdentitySurvivesCrossPackageMove` in `tools/lintreport/main_test.go` performs a real
cross-directory `git mv`, but its moved file carries no package qualifiers, so it pins path-invariance
only — the half that already works.

**It is weaker still than that.** It sets `Pos.Line = 2`, and in its own fixture
(`"package worker\n\nfunc Run() {\n\tprintln(\"work\")\n}\n"`) line 2 is the **blank line**. No
`identityNode` spans a blank line, so `enclosing` takes the `best == nil` whole-file fallback. The one
test named for cross-package moves never reaches `identityNode`, `nodeName`, or the
enclosing-declaration path at all.

`scripts/lint-allow-ratchet-test.sh` cannot help either: `scripts/lint-allow-ratchet.sh` never invokes
`lintreport` — every mention of it in that script is a comment — so the script is a pure `comm -23`
over allow-list text. Its cases fabricate digests rather than computing them. **A shell test of the
ratchet cannot prove anything about the hashing.**

## Scope

- `tools/lintreport/main.go` — `findingDigest`, `sourceFile.enclosing`, `identityNode`, `nodeName`.
  This is where the work is.

  **Parse each file TWICE, and get the ORDER right — the obvious order is wrong and fails silently.**
  You need comments present to LOCATE (route 3, doc-comment lines) and absent to PRINT (route 4).

  **Do NOT locate on a comment-bearing tree and then search a mode-`0` tree at the same line number.**
  A mode-`0` parse has `File.Comments` nil, so reprinting drops every comment line and the two trees
  no longer agree on line numbers. The search lands past short declarations, falls into the
  `best == nil` whole-file branch, and emits wrong digests **with no error at all**.

  **The correct order is the inverse:** strip the qualifiers and reprint from the COMMENT-BEARING
  tree, then reparse that output with mode `0`, then locate and derive from the reparsed tree. One
  coordinate system throughout.

  **The trap that will cost you a day if you skip it: `go/printer` reads POSITIONS.** Replacing a node
  in place leaves stale positions, so a naive in-place mutation prints differently from the same code
  genuinely written without the qualifier — subtly, and with no error. Strip through the AST, reprint
  the whole file, then REPARSE and re-derive from the reparsed tree.
- `tools/lintreport/main_test.go` — the fixtures. See "Done when" item 3.
- `tools/lintreport/allow.txt` — regenerated if and only if the key changes. See "Regeneration".
- `scripts/lint-allow-ratchet.sh` — **not in scope at all. Do not edit this file.** Two separate beads
  own the two changes it needs: `hk-ym2nn` deletes the dead `is_legacy` / `legacy_pairs` branch, and
  `hk-h4h78` widens the comparison window. Keeping both out of this commit is what lets the migration
  be audited by comparing a row count before against a row count after.

**Out of scope:** `.golangci.yml`, which every lint task is forbidden to edit; the set of tolerated
findings, which may fall in a later task and may not rise here; and rename detection, which is
forbidden below.

## The decision you have to make, and how to justify it

Route 1 and route 2 both need the package qualifier neutralized, and neutralizing it needs you to tell
a package qualifier (`runregistry.RunHandle`) from a field or method selection (`cfg.Timeout`,
`h.Close()`). Both are `*ast.SelectorExpr`. **Blanket-rewriting every selector destroys real content
and is not acceptable.**

**Use the import set. This was measured against `go/types` ground truth on 2026-08-25 and the cheap
route wins on the numbers.** You may still reject it, but you must beat these figures to justify the
expensive one.

Over all 48,162 `SelectorExpr` nodes with an `Ident` base, in the 373 files that carry findings, the
import-set answer was compared against `types.Object` being a `*types.PkgName`:

| | count |
|---|---|
| examined | 48,162 |
| import set agrees with `go/types` | 48,136 — **99.946%** |
| false positive (calls it a package, it is a local) | **4**, at 3 sites |
| false negative (misses a real package) | 22 |

**Scope the strip to MODULE-INTERNAL imports only.** Filter to paths under the module prefix, read
from `go.mod`, and fail closed if it cannot be read. `os.Remove` cannot change under a package move,
so stripping external qualifiers buys nothing and costs discrimination on 443 more findings. All 22
false negatives are external (`gopkg.in/yaml.v3`, whose last path element is not its package name),
so module-only scoping makes them unreachable.

**The two error directions are not symmetric, and that is what makes this safe.** A false negative
costs nothing: the qualifier stays and the row is exactly as fragile as it is today. A false positive
costs a merged key — and a merge needs the ENTIRE printed declaration to become byte-identical to
another one, with the same linter and the same canonicalized message. Measured across all 989
findings **at integration tip `eae64d47a`**: **zero merges**, including at the 3 shadowing sites (`schedule.store` and `schedule.wakeC`
in `internal/daemon/scheduler.go`, `substrate.tmuxAdapter` in `internal/daemon/bootworkloop.go`).

**Delete the qualifier. Do not replace it with a sentinel.** The obvious design — `pkg.Sel` becomes
`‹pkg›.Sel`, preserving that a qualifier existed — fails the requirement. When a file leaves
`internal/daemon`, the files left behind rewrite bare `RunHandle` to `runregistry.RunHandle` AND the
moved file rewrites bare `RunRegistry` to `daemon.RunRegistry`. Invariance needs `pkg.Foo` and `Foo`
to hash the same, which only deletion gives. Deletion is also alias-invariant for free, which this
tree needs: it carries 14 distinct aliases for internal packages — `ltmux`, `tmux`, `tmuxPkg` and
`tmuxpkg` all name `internal/lifecycle/tmux`, and `runpkg` names `internal/run`.

**`go/types` is rejected on measured cost, not on principle.** `packages.Load("./...")` at
`NeedTypes|NeedTypesInfo|NeedDeps` takes **4.6 s warm** against **1.37 s** for the entire current
derive over all 989 findings. `golang.org/x/tools` is in neither `go.mod` nor `go.sum` and pulls
**7 new modules** onto a module with **8** direct requires — including `x/telemetry`, in the build
graph of a quality gate. That is 0.054% of accuracy on a difference that produced zero merges.
`golang.org/x/tools` is in neither `go.mod` nor `go.sum` today; verified 2026-08-25.

**Justify the choice with a measured number, not an argument.** Whatever you pick, a qualifier that
gets stripped makes two previously distinct declarations hash the same, so one tolerated row can
grandfather a second finding. Report, in the commit body: **how many rows on your branch collide
under your scheme.** If the answer is zero, say so and say how you measured it. If it is not zero, name the
colliding rows and say why the trade is acceptable. This number is measurable with the existing
binary, so an unmeasured claim is not acceptable here.

**Route 3 has a root cause, and fixing it is not what it looks like.** The 14 `file scope` rows are
not really file-scope. `enclosing` compares the finding's line against `n.Pos()`, and `FuncDecl.Pos()`
is the `func` keyword while `GenDecl.Pos()` is `TokPos` — so a finding **on a doc comment line**
matches no `identityNode` and falls through to the whole-file branch.
`internal/runloop/waitsocketgrace.go:26` is the doc comment for `ExitInfo`, not file scope.

**Add a `startPos(n)` that returns `n.Doc.Pos()` when `Doc != nil`, and drop the import `GenDecl` from
the fallback concatenation. That takes the file-scope population from 14 to 3.** The 3 that remain are
all `revive package-comments: should have a package comment` at line 1 of the `evaltasks/eval-*`
files. There is no move-invariant key for a finding whose subject is the file itself, and you should
not invent one: **fix them** — one comment line each — and delete the rows. That takes the fallback's
live population to zero, which is the only honest way to stop defending it.

**Leave the 3 depguard rows alone, and the reason matters.** An earlier note in this file asked you to
check whether they are already invariant. The BODY is: `import "github.com/expr-lang/expr"` is
byte-identical after any move of the importing file. The MESSAGE is not:

    internal/core/policydocument.go:8  import 'gopkg.in/yaml.v3' is not allowed from list 'core'

`list 'core'` is the depguard rule name, and the rule is selected by a path glob
(`files: ["**/internal/core/**"]`). Move the file and the message names a different list, or the
finding disappears. **That is correct behaviour and must not be canonicalized away.** A depguard
finding is a statement about which package may import what; a move that changes which list applies is
a policy change, not a re-key, and the gate should challenge it.

**Measured at batch tip `d5673dee4` on 2026-08-25, and it narrows this:** none of the findings the
judge currently refuses is an `ImportSpec` row or a `file scope` row. All the refused ones sit in
`internal/daemon`. All 14 `file scope` rows are `revive` findings in `internal/runloop` and
`evaltasks`, and every one of them is matched and tolerated. **So route 3 is not implicated in the
current red at all.** Fix it because a move will hit it, not because it is failing now.

**Expect the judge to report stale rows, and do not read that as a failure.** At the batch tip 25
allow-list rows match no current finding. The judge prints a line naming them clean and **exits 0
regardless**. Removing them is out of scope for this task.

## Done when

1. **A cross-package move that RE-QUALIFIES a type reference leaves the key unchanged.** This is the
   property. Not a pure `git mv` — that already passes and proves nothing.
2. **A genuinely new tolerated finding is still refused.** The re-key must not weaken the gate. Note
   that the ratchet's own proof of this is reopened (`hk-lint-ratchet-mutation-proof-hdju9`) and two
   live bypasses are filed (`hk-h4h78`, `hk-ym2nn`), so do not lean on a green ratchet as evidence —
   assert against `lintreport`'s own judge.
3. **The fixture is self-contained inside `tools/lintreport` and computes real digests.** An earlier
   version of this file made the acceptance rest on the four extraction tasks. Two of them
   (`runregistry-extract`, `spendmeter-extract`) have landed and the other two
   (`handlerpause-extract`, `harnesspick-extract`) are blocked on THIS task, so that test could never
   run. Build a fixture that performs the move and the re-qualification itself. It must cover, at
   minimum: a type reference that gains a qualifier; a symbol left behind that gains a qualifier for
   the moved package; an `*ast.ImportSpec` finding; and a `file scope` finding.
   **Do not fabricate digests.** Compute them from real Go sources through the real code path. The
   existing shell cases fabricate, which is why they prove nothing.
4. **Land this as THREE commits. The shape is FORCED — it is not a style choice.**

   **You are blocked on `hk-iw11o` and cannot start until it lands.** That bead teaches the ratchet
   to express a key-scheme migration. Without it this task cannot be committed at all: today's
   ratchet on a faithful re-key reports **934 added pairs and exits 1**, and the ratchet runs in
   `core`, `fast` and `full` alike.

   **Why the repair cannot be its own commit.** `hk-iw11o` branches on the DECLARED SCHEME VERSION
   in the allow-list header. A commit that only swaps re-fingerprinted rows does not change the
   scheme — the code moved, the keying rule did not — so the version is unchanged, the ratchet takes
   the CHEAP text-compare path, and it fails on the added rows. **The row repair must ride inside the
   commit that declares the new scheme, or it cannot land.**

   - **Commit 1 — delete the rows the judge reports clean.** Pure deletion, allowed under any gate.
     State the before and after counts, from YOUR branch.
   - **Commit 2 — fix the three `revive package-comments` findings** in the `evaltasks/eval-*` files,
     one comment line each, and delete their three rows. Pure deletion of rows; count falls by three.
     Takes the `file scope` population to zero live rows.
   - **Commit 3 — the scheme migration. Everything else happens here, at once.** Bump the
     `# key-scheme:` header, land the keying change, regenerate every row, and repair the
     re-fingerprinted rows in the same commit. **Name the move that caused each repaired row in the
     commit body** — that is the part a reviewer can check and the tool cannot.

   **The branch starts RED and it is not your fault. Do not widen scope to go green.** At batch tip
   `d5673dee4` the judge refuses 14 findings and `make fast` runs `lint-allow`, so `make fast` is red
   before you touch anything. Commit 3 is what clears it. **Every count comes from YOUR branch and
   none from this file** — the list holds 956 rows at integration tip `eae64d47a` and 923 at batch
   tip `d5673dee4` with byte-identical `tools/lintreport` code on both, and a batch cut after the
   merge carries a third number. An earlier version of this file named 575, which would have told you
   to delete 381 rows. **A literal is wrong here whichever branch you take it from.**

   **Commit 3 must ALSO drop every stale row, and this is the trap most likely to read as a bug.**
   `hk-iw11o`'s check requires that no new row exists which no old row maps to, and a row whose
   finding is gone cannot map. `lintreport -write` drops stale rows automatically, so a regenerating
   migration is fine — but a HAND-EDITED migration that preserves them fails, and the failure looks
   like a defect in the gate rather than a rule you broke. Commit 1 removes most of them; do not
   reintroduce any.

5. **You did NOT touch `scripts/lint-allow-ratchet.sh`.** An earlier version of this file asked you to
   delete the dead `is_legacy` / `legacy_pairs` branch here. **That was a scope collision and it is
   withdrawn:** deleting that branch is the entire content of `hk-ym2nn`, which owns it. Doing it
   here would put a gate change inside a re-key commit and destroy the property the whole migration
   is built to protect — that this commit is a pure 1:1 remap, auditable by comparing two row counts.
   Leave the branch alone. `hk-ym2nn` deletes it separately and is not blocked on this task.

## Regeneration

Your scheme changes the digest of existing rows, so every row has to be re-derived. **Do not use
`-write`.** It is a RESEED, not a merge: it regenerates the whole list from the report plus the tree as
it stands and never reads the previous list, so it silently adopts any finding the tree has picked up.

**Build a `-remap` mode instead. It is strictly stronger and it cannot launder.** It:

1. reads the existing `allow.txt` and the report;
2. computes **both** the old and the new digest for every finding;
3. **refuses** if any finding's OLD digest is absent from the existing list — that means the tree
   carries untolerated debt, so abort rather than adopt it;
4. **refuses** if two old rows map to one new row;
5. writes only rows whose old digest was already present, and prints the stale rows it dropped, with
   their locations.

`-remap` cannot adopt a finding that was not already tolerated, because it only ever maps rows that
already existed. The file format is unchanged (`digest TAB linter TAB # comment`), so
`scripts/lint-allow-ratchet.sh`'s `comm -23` keeps working untouched. **Note that `-remap` does not
exist — `main.go` declares only `-allow` and `-write`. You are writing it.**

**Rule 3 fires on the 14, and repairing them is the answer — not relaxing it.** "Refuse if any
finding's OLD digest is absent" hits precisely the findings the judge already refuses, because
*refused* means the digest is not in the list. And the re-key does not dissolve that: after
neutralization those findings hash to a NEW digest while the list still holds their PRE-MOVE digest,
so the lookup misses either way. Nothing inside the re-key rescues a row whose stored digest was
computed on a tree that no longer exists — you have to put the current digests back.

**That repair rides inside commit 3**, which is where the scheme version changes and therefore the
only commit that can carry an added row at all. Handle those rows explicitly there and rule 3 stops
firing.

**Keep rule 3 exactly as written.** It is the property that stops the tool adopting untolerated debt,
and it turned out to carry the soundness proof of `hk-iw11o`'s cheap check as well: a genuinely new
tolerated finding is refused BY THIS RULE, because its old-scheme digest was never in the old list.
**The answer to it firing is to repair the tree, never to relax the rule.** Relaxing it would have
destroyed the proof before anyone discovered it was needed.

**The remap was a bijection where it was measured** — 934 identities to 934, zero splits, zero merges
— which is why a 1:1 remap is the right shape. **That reading is from integration tip `eae64d47a`, and
934 is an IDENTITY count, not a row count**: the list is 923 rows at the batch tip and 956 at
integration, and one identity can carry more than one finding. **Re-measure both on your own branch.**
The zero-merges figure is the whole safety argument for the import-set choice, so if it does not
reproduce on your branch, stop and say so rather than proceeding.

**Land the already-clean rows as a SEPARATE, EARLIER commit.** The judge reports some rows as clean —
their finding is gone and the row is waiting to be deleted. A remap drops them, so the row count falls
and the re-key commit stops being a pure 1:1 map. Delete them first, in their own commit, and the
re-key commit is then trivially auditable: same count in, same count out. Say both numbers in the
commit body.

## Limits

- **Do not re-attempt rename detection.** No `git diff -M`, no similarity index, no path history.
  **Operator ruling, 2026-08-23: change the keying rule rather than teach the ratchet to detect
  renames.** A rename-aware ratchet was built and reverted the same day after an adversarial review
  found three working routes to forge a rename and mint a free exemption. That code left no trace in
  this repository, so treat the three routes as a caution you cannot audit. The ruling does not
  depend on them.
- **Do not tolerate anything new.** The count may fall in a later task; it may not rise here.
- **Do not re-open the keying scheme.** Content hashing shipped in `fbe830453`. This task makes it
  invariant, not different.
- **Do not change the ratchet's comparison window.** `hk-h4h78` owns it and is blocked on this bead,
  because a fixed comparison base reads a legitimately re-keyed row as new debt until this task lands.
- The `LINT_ALLOW_LIST` override already refuses a non-`.txt` path (`case "$allow" in ... *.txt) ;;`).
  That Limit is spent. Leave the override working so the self-test can still point at a scratch list.
- **Do not touch `--uniq-by-line`.** It defaults to true and is set nowhere, so golangci-lint keeps at
  most one finding per source line and the gate never sees the rest — about 24% of the tree's
  findings. Turning it off adds roughly 310 rows and makes this commit unauditable. It is a real
  defect and it is filed as `hk-e0ybh`; it lands on its own, not here.
