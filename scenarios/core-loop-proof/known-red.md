# core-loop-proof — known-RED cells

A **known-RED** cell is an assertion that is EXPECTED to fail today against a tracked
daemon defect. It is recorded here and self-tested as an expected-fail — it is NOT a
false-green, and it is deliberately excluded from T9's full-matrix green gate (the gate
is "full-matrix green **minus** the known-RED cells").

When the underlying defect lands, the assertion flips to `pass`, its expected-fail
self-test row breaks loudly, and that break is the signal to retire the entry here.

## `pi-dot:local` — the DOT review round trip never converges (hk-psla4)

**Recorded 2026-07-22 by the assessor, from the `650f359b` release gate.**

`pi-dot:local` fails `gap6` and `t10` whenever the reviewer returns REQUEST_CHANGES: the
implementer is re-dispatched, `implementer_phase_complete` fires, and the pass produces
**zero diff** (`diff_hash_prior == diff_hash_current`), so `review_fixup_stalled` fires at
iteration 2 and nothing lands on `core-loop-proof-dot-integ`.

**This is not a release-introduced red.** It was proven pre-existing by a controlled
differential — the identical cell, command, and isolated substrate at the release commit
`650f359b` and at the then-deployed `eb2b4f1a`, both single-daemon-verified, produced the
same signature line for line. Report: `.harmonik/reports/rel-650f359b-gate.md` §5a.2.

**Two traps that make this cell easy to misread, both of which cost real runs:**

1. **A green here can be a false green.** The cell only exercises the fix-up path if the
   reviewer actually asks for changes, and the reviewer verdict is nondeterministic. A run
   whose reviewer APPROVES on the first pass lands cleanly and reports the round trip as
   working — it never entered the path. Treat "landed" as informative only alongside a
   REQUEST_CHANGES in the same run's event log.
2. **`pi:local` flips green→red at a fixed SHA**, at the deployed commit as well as the
   release commit. A single green and a single red are equally weak evidence from this
   harness; only a differential across commits carries a conclusion.

**Retire this entry when** a run shows REQUEST_CHANGES at iteration 1 followed by an APPROVE
after re-dispatch, with the work landed on `core-loop-proof-dot-integ`.

**Amended 2026-08-13 (hk-vf4ju): that retire condition used to be unreachable, and it is
reachable now.** Non-convergence was not the only thing holding this cell red. Two assertion
defects held it red as well, on the OPPOSITE ordering — the very ordering the retire condition
above names. `gap6`'s same-model clause counted the reviewer's EMPTY model string as a foreign
model, and `gap1`'s last-wins read the reviewer's `claude-code` tier-3 `harness_selected` as
the run's harness. A converged run launches the reviewer LAST, so it would have gone red on
both. The daemon forcing the reviewer to claude-code is settled policy and is not the defect;
the assertions were written to a claim about it that was never true. Both are fixed in
`scripts/core-loop-assert.jq`, and the golden
`testdata/pi-dot-roundtrip-pass.ndjson` now carries the reviewer's selection events instead of
omitting them. Convergence (`hk-psla4`) is what remains.

Until then the forced LT gate (`make core-loop-lt` with `EXTRA_CELLS='pi-dot:local|pi|local'`)
**cannot be made green by any release**, and a release must not be blocked on it — the gate
cannot currently tell two commits apart. Scope the T9 full-matrix green gate as
"full-matrix green **minus** `pi-dot:local`" until `hk-psla4` lands.

### Operator caveat — live codex empty-model runs depend on the account default (not a known-RED cell)

This is an **operator caveat**, NOT a known-RED cell: it does not flip a matrix
assertion, and the codex cells stay GREEN because determinism in the matrix comes from
`harmonik-twin-codex`, not from live codex.

A live codex empty-model run (`codex exec` with no `--model`) resolves the model from the
ChatGPT **account default** in `$CODEX_HOME/config.toml`. That default only works if the
installed `codex-cli` can actually serve it. As of **2026-07-16** the ChatGPT account
default is rotating to **gpt-5.6-sol**, which **codex-cli 0.142.5 cannot serve** — it
returns HTTP 400 `"requires a newer version of Codex"`. So a live empty-model codex run can
fail even though the harness argv (no `--model`) is correct.

Mitigation for operators: either **pin a supported model** in `$CODEX_HOME/config.toml`, or
**upgrade `codex-cli`** to a version that serves the current default. The matrix itself is
unaffected — the codex cells run against `harmonik-twin-codex` (model-blind), so they stay
deterministic regardless of the live rotation. Ref: COORD c072.

### Retired

- **t10 — per-bead integration-branch targeting (hk-lgykq).** RETIRED 2026-07-07. The
  defect (the `landTaskBranch` / per-bead `lands_on` path was dead code, so every run
  landed on the daemon-wide default target instead of the bead's intended integration
  branch) was fixed by wiring the resolved `baseBranch` as the merge target into all five
  merge call sites (`internal/daemon/workloop.go`, commit `ca23da59`). Proven live by the
  daemon E2E test `TestMergeToMain_PerBeadIntegrationTargetLandsOnBranch` — a run lands on
  its intended branch with `main` byte-pinned; RED before the fix, GREEN after. The
  two-sided `assert_t10` golden coverage (wrong-branch → fail, intended-branch → pass)
  stays in `scripts/core-loop-assert-test.sh` as permanent assertion-logic tests.
