# Quality-enforcement diagnosis — why the flywheel ships bugs (2026-07-07, admiral)

Five parallel investigators (recurring-failure root cause · gate effectiveness · manifest
enforcement · deploy governance · assessor readiness) converged on ONE root cause.

## Root cause
**Every gate is authored as prose/config but never made mechanically fail-closed.** The last
edge before every merge, close, and deploy is an LLM writing a string about its own work
(commit trailer, comms "PASS", "acknowledged"). Mechanical checks verify *form* (JSON parses,
`-short` exits 0), never *substance* (did an independent reviewer run for THIS diff; did the
acceptance behavior actually execute). Detection designs (assessor, deterministic bead-query
gate, corpus ratchet, provenance manifest) exist on paper / partly approved but are NOT the
enforced, non-bypassable default — so every class stays "closed-by-playbook," and a playbook
makes re-recovery faster, not recurrence rarer. That is the "faster at delivering bugs" flywheel.

## Smoking guns (concrete)
- Commit `6bb2a6c2` landed with NO reviewer trailer — reviewer never ran. Strict git hooks
  (`lefthook.yml`) were never installed; `make agent-review` hits a stub that exits 0.
- `main` has NO branch protection (GitHub: "not protected"); `ci.yml` + `scenario.yml` are
  `continue-on-error: true` → a red check reports SUCCESS. That's why PR #20's red just sits.
- The per-commit reviewer is test-blind by contract (reads diff, never runs `go test`).
- Pre-deploy GATE-0 + the assessor verdict are prose addressed to the admiral, enforced by no
  code. GATE-0 caught this session's regression ONLY because the admiral hand-ran it.
- The corpus/remediation ratchet (a finding can't close until a regression scenario is committed)
  is designed + approved (`plans/2026-07-06-quality-system/09-remediation-loop-design.md`) but
  not enforced.
- Corpus: state/lifecycle-drift + false-signal misdiagnosis ≈ 46% of shipped failures — classes
  no current gate is shaped to catch. Twice this session a FIX re-instantiated a bug.

## Priority insight
The initiative was building the TEST BATTERY (matrix runner, twin, scenarios). But a perfect
battery behind a fail-open gate changes nothing — the tests already exist and still don't block.
**Enforcement-first:** flip the gates advisory→enforced FIRST (days, cheap), before more
test-authoring (weeks). Initiative was ordered wrong.

## Plan (priority order) — becomes epic `codename:quality-enforcement`
- **A. Gates fail-closed (bleeding-stopper):** install git hooks + CI check that live hooks ==
  `lefthook.yml`; branch-protect `main` + drop `continue-on-error`; land real `agent-reviewer/run`
  writing a diff-keyed verdict the commit hook cross-checks (stub fails CLOSED until then); only
  APPROVE committable.
- **B. Done + deploy behavioral:** deterministic done-check (grep/assert) between APPROVE and
  close; move assessor block-query INTO `harmonik promote`/deploy (refuse while open P0/P1
  `found-by:*`); interlock binary-swap on machine-readable PASS for the deploy SHA.
- **C. Provenance + liveness:** stamp `binary_commit_hash` (ldflags) + serialize daemon_config;
  make `events.jsonl` the authoritative liveness contract, kill synthetic-heartbeat masking.
- **D. Ratchet (permanent):** corpus-closure rule enforced as default (fix flips red cell green
  AND commits a regression scenario before a finding closes).
- **E. Assessor → real gate:** merge matrix runner (EXISTS on PR #20 branch, not unbuilt) →
  close 2 small schema/query gaps → conformance floor as execution gate → first live gate on
  twin epic `hk-pnjgh`.

## Governance model (replaces "integration→main = human PR", which operator never set)
Admiral+operator plan→specs→beads · captain coordinates+done-checks · crew implements ·
ASSESSOR rigorously verifies build (LT live-verify + XT break-test + CR independent review) ·
admiral deploys on PASS + zero open P0/P1 blockers. Human governs the plan, not the merge.
Full draft: this dir will hold `01-governance-draft.md` (from the governance investigator).
