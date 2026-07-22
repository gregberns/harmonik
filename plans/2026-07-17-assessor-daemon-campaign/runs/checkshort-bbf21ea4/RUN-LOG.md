# CI-Gate Signoff-Flaw Investigation — RUN-LOG (PIN bbf21ea4)

- **Trigger:** admiral 04:33Z — my baseline PASS was behaviorally-green but PR #31 CI failed; my acceptance gate never ran the CI merge gate (`make check-short`). Owned as a gate-contract defect (04:34Z ACK).
- **PIN:** bbf21ea4 (branch HEAD; admiral's gci fmt fix). origin/main = 0cba1ab4. merge-base = c9372014. Branch is 317 commits ahead of main.
- **Scratch:** /private/tmp/h-assessor/scratch-checkshort-bbf21ea4 @ bbf21ea4. golangci-lint reused from real repo `.tools/golangci-lint` (v2.3.0, pinned).

## TWO PARALLEL WORK ITEMS (admiral 04:33 + 04:40)

### A) MG POLICY EDIT — formal committed doc change (04:40Z mandate) — DIFF POSTED, awaiting admiral confirm
Made, not just proposed. Diff: `MG-policy.diff`.
- `good-enough-principles.md`: §2 four→five requirements; NEW **§2.5 MG — merge-gate green (CI parity)**. Principle: acceptance gate is the SUPERSET; CI-green necessary-but-never-sufficient; PASS impossible while any check-short check red. Lint blocks only on branch-introduced (`--new-from-rev`); pre-existing main debt = separate main-health escalation. Assess merge RESULT where feasible.
- `operating.md` §Merge-gate: NEW required leg **4b MG** + MG subagent in D1 delegation + step-6 verdict folds "red MG on branch-introduced = BLOCK". LT=2/XT=3/CR=4 numbering kept stable.
- **NEXT:** on admiral confirm → coordinate with captain to land committed+reviewed (load-bearing normative; not self-landed).

### B) 198-FINDING DELTA — quantify branch-introduced vs pre-existing-on-main — SUBAGENT IN FLIGHT
Subagent running in scratch: `make fmt-check` / `go vet` / `go build` / `golangci-lint --new-from-rev=0cba1ab4` (CI-blocking branch-introduced) / full `golangci-lint run` (branch total) / full lint on main worktree (pre-existing baseline) / `go test -short -race -p=1 -parallel=1 -timeout=20m` (the long one). Returns structured counts.
- **Key framing already established:** check-short's lint step uses `--new-from-rev=origin/main` by design → CI blocks ONLY on branch-introduced; the Makefile documents a full run surfaces ~5666 pre-existing legacy issues (main-health, not branch). The admiral's "198" is a scoped count; the subagent measures the real split.
- **NEXT:** fold numbers → report on --topic gate; classify how many of the 198 are branch-introduced (CI-blocking) vs pre-existing.

## My gap, confirmed against my own logs (04:37Z)
NONE of my baseline/keepalive legs invoked golangci-lint / fmt-check / check-short. Regression green-tree ran `go test -count=1` per-package WITHOUT `-race`. XT ran `-race` only on select packages (codexdriver/codexwire/isolation), never the full CI `-race` suite, never lint. → PASS meant ship-behaves, not merge-clean. Accurate root cause.

## STATUS: no PASS re-issued (per directive). Both items in flight.

## [~21:44] UPDATE — admiral CONFIRMED diff + 198-delta numbers in
**A) MG POLICY DIFF: APPROVED by admiral** (gate authority). NEXT = land committed+reviewed via captain. Edits still uncommitted on disk.
**B) 198-DELTA MEASURED (subagent, at bbf21ea4):**
- **CI-blocking `golangci-lint --new-from-rev=origin/main` = 198** (matches admiral's number exactly). By-linter: errcheck 83, gosec 51 (=134/198, 68%), noctx 9, govet 8, gocritic 8, errorlint 6, ctx 6, unparam 4, revive 4, forbidigo 4, depguard 4, prealloc 3, +tail.
- **Full branch = 6850 issues · Full main (0cba1ab4) = 7115 issues** → the branch has **265 FEWER** total issues than main. KEY INTERPRETATION: the 198 are NOT net-new defects — the branch reduced aggregate debt. They are pre-existing debt CLASSES (errcheck/gosec) that `--new-from-rev` attributes to branch-TOUCHED lines. CI blocks on them mechanically (why #31 failed), but the branch author did not write 198 new bugs. Fix path: clean the 198 touched-line issues OR repo-wide debt cleanup; this is main-health-adjacent, admiral's bucket-(2)/(3) call on ownership.
- **fmt-check: INCONCLUSIVE in scratch** — `.tools/gofumpt`/`gci` binaries absent (scratch never ran `make tools`); harness gap, not a branch failure. Admiral already established fmt-check PASSES on bbf21ea4 after their formatting fixes.
- **go vet / go build: empty output = clean pass** (both emit nothing on success).
- **go test -short -race: NOT captured** — subagent launched it in background and returned; `test-short-race.txt` is 0 bytes. INCOMPLETE — next session must re-run or check.

## [keeper-restart resume ~04:50Z] — LAND + REPORT
**A) MG POLICY DIFF — landing in flight.** admiral CONFIRMED (event 27). `-count=1` nit FIXED inline (good-enough §2.5 now matches operating.md 4b). Land request sent to CAPTAIN (event 019f78b5…): commit BOTH files via agent-config-reviewer, Trivial:false, commit body cites operator mandate + PR#31 false-green. Awaiting captain's landed SHA → then report to admiral on --topic gate. Edits still uncommitted on disk.
**B) 198-DELTA — REPORTED to admiral** (event 019f78b6…). Numbers folded from the ~21:44 measurement: 198 CI-blocking (`--new-from-rev`), errcheck 83 + gosec 51 = 68%; branch total 6850 < main 7115 by 265 → pre-existing debt classes on touched lines, NOT net-new. go vet/build clean; fmt-check passes on bbf21ea4; go test -short -race capture partial (all-green through internal/crew). Recommended the definitive full check-short attestation at the POST-MG-LANDING HEAD (landing advances HEAD, forces re-pin anyway).

## [~22:14] UPDATE — FULL check-short battery COMPLETE at bbf21ea4 (gap at line 32/36 now closed)
Re-ran the entire battery in scratch. Artifacts in this dir: fmt-check.txt, go-vet.txt, go-build.txt, lint-new-from-main.txt, lint-full-branch.txt, lint-full-main.txt, test-short-race.txt.

- **fmt-check: PASS** (exit 0 — `.tools` binaries present this run; prior INCONCLUSIVE resolved).
- **go vet: PASS** (exit 0, empty output). **go build: PASS** (exit 0, empty output).
- **golangci-lint `--new-from-rev=main` (CI-blocking): 198** (exit 1). By-linter, issue-lines only (sums to 198): errcheck 83, gosec 51, noctx 9, gocritic 8, errorlint 6, unparam 4, revive 4, forbidigo 4, depguard 4, prealloc 3, nolintlint 3, gocognit 3, cyclop 3, contextcheck 3, staticcheck 2, nilerr 2, copyloopvar 2, unconvert 1, nakedret 1, exhaustive 1, containedctx 1. (Prior "govet 8 / ctx 6" were regex artifacts from message-embedded parens; corrected here.)
- **Full branch lint: 6850** · **Full main (0cba1ab4) lint: 7115** → branch has 265 FEWER; confirms prior interpretation.
- **`go test -short -race -count=1 -p=1 -parallel=1 -timeout=20m ./...`: FAIL (go exit=1).** 82 ok pkgs, 6 no-test, **2 FAILing packages, no data races, no panics**:
  1. **internal/daemon — FLAKY, environmental, NOT a branch defect.** Both `TestSandboxAcceptance_WriteToMainDenied_hki0377` + `TestSandbox_WriteToMainDenied_i0377` fail self-diagnosed: "transient sandbox_init apply-failure under fork saturation is the diagnosed cause, see hk-tch4t" (retried 3/3, all observed allowed writes). macOS Seatbelt flake under full-suite load; package ran 1157s (near 20m cap). Known/tracked (hk-tch4t).
  2. **internal/specaudit — GENUINE BRANCH-INTRODUCED FAILURE.** `TestSHINV005CorpusLint/core-loop-proof/scratch-config-overlay.yaml`: the scenario file `scenarios/core-loop-proof/scratch-config-overlay.yaml` fails SH-INV-005 strict-mode corpus lint — `field codex not found` (line 29) + `field harnesses not found` (line 32) in `scenario.ScenarioFile`. File is branch-introduced (added commit 134a9781 `feat(core-loop-proof): M6 WS4-3 provision scratch config`, touched 4ec0045f; absent from main, tracked on branch HEAD). This is real spec-drift a merge would carry in, and check-short would block on it. NEEDS A FIX (either the YAML schema/fields or the scenario file) — not a flake, not pre-existing-on-main.

**BOTTOM LINE for MG attestation:** at bbf21ea4, check-short is NOT green — two independent reds. The lint red (198) is the known branch-touched-line debt story. The NEW finding is the specaudit red: a genuine branch-introduced schema-drift test failure that must be fixed before this branch can pass check-short. The daemon sandbox reds are the known hk-tch4t environmental flake and do not by themselves block a clean attestation. Re-pin + re-run after MG lands as previously planned, but the specaudit fix is required regardless of HEAD advance.

## [keeper-restart resume, ~04:5xZ] — DEFINITIVE ATTESTATION VERDICT DELIVERED = MERGE-BLOCK
Independently reproduced the specaudit red (go test ./internal/specaudit -run TestSHINV005CorpusLint → FAIL, exact line 29/32 errors). Root-cause CORRECTED vs the subagent's "fixture drift" framing: `scratch-config-overlay.yaml` is a harmonik CONFIG OVERLAY (codex.stale_wal_max_bytes + harnesses.pi), NOT a scenario — SH-INV-005 walks every *.yaml under scenarios/ (only twin-scripts/ skipped) and strict-parses each as ScenarioFile. FIX = move the overlay OUT of the scenarios/ glob, not extend the schema.
- **Already filed as hk-uhxwd** (P3, codename:code-revamp, known-issue) — but MIS-FRAMED. Added a br comment correcting all three: (1) it's a CI MERGE-BLOCKER not P3; (2) BRANCH-INTRODUCED (134a9781, absent from main) not pre-existing debt; (3) config-overlay-in-wrong-dir not fixture drift. PROPOSED P3→P1 + remediation:blocking + release-epic scope + crew route. I do not close/claim/reopen; admiral adjudicates severity/owner.
- **Reported to admiral** on --topic gate (event 019f78cf…): check-short = MERGE-BLOCK; branch NOT mergeable as-is; PR#31 keeps failing until hk-uhxwd fixed + 198 dispositioned; reds are docs-landing-independent so persist at post-landing HEAD → fix is a crew task. NO PASS re-issued.
- **NEXT:** await (a) captain's MG-landing SHA → report to admiral; (b) admiral's hk-uhxwd severity/route adjudication; (c) after the specaudit fix + MG lands, re-pin to post-fix HEAD and re-run the definitive check-short for a clean MG attestation.

## [~05:2xZ] — MG POLICY COMMITTED @ b624c3ab ✅
Captain landed it (committer #3 in the serial window). Verified: both files, Trivial:false, Reviewed-By: agent-config-reviewer, Review-Verdict: CLEAN, commit body cites operator mandate + PR#31 false-green. good-enough §2.5 + operating leg-4b are now normative gate contract. Reported to admiral (event 019f78d9…). **BOTH admiral-tasked tracks DONE: MG policy committed + 198-delta reported.** Remaining = admiral adjudication (hk-uhxwd severity/route + 198 disposition) → crew fix (move overlay out of scenarios/ glob) → re-pin + definitive check-short for the clean PR#31 gate evidence.
