# Keep / delete ledger

Generated 2026-07-27. Selection is mechanical — see `_plan.md` §2.
Validate after each step: `go build ./... && go test ./... && go test -tags=scenario ./test/scenario/... ./internal/daemon/...`

> **Re-audited 2026-07-30 against the tree.** Every count and every file list below was
> re-derived from git. Two of the three delete sets were wrong when written, and the
> corrections are inline. Do not re-use a number here without running the command beside it.

## DELETE 1 — spec-prose tests — **LANDED 2026-07-27 at `e99a52fff`**

Claimed 37,927 LOC. Measured: 129 test files and 37,281 lines under `internal/specaudit`,
37,362 lines with the build wiring. The claim over-counted by about 650 lines.

```
internal/specaudit/*_test.go carrying //go:build specaudit   # 129 files, greps specs/*.md
internal/specaudit/RELOCATED-ALLOWLIST.md
scripts/specaudit-lint.sh
+ build wiring: Makefile target & TAGGED_BUILD_TAGS, ci.yml step, scenario-gate.sh blocks
```

**KEPT** in `internal/specaudit/`: `doc.go` plus the 3 product-importing carve-outs
(`ar025_agent_type_regex`, `hqwn57_eventbus_interface`, `sh_inv005_declarative_loadable`).
Confirmed 2026-07-30: those four files are all that the directory holds.

**RETRACTED — `internal/workflow/scenario/` is NOT a delete target.** All 20 files import
`internal/core` + `internal/workflow` + `internal/workflow/dot` and drive the real engine
(`LoadDotWorkflow`, `DecideNextNode`) against `specs/examples/*.dot`. Deleting them drops
fixture coverage from 26/28 examples to 6/28. The original "0 LOC prod code" reasoning was
wrong: that measures files in the directory, not what the tests exercise.

## DELETE 2 — bead-ID-named test files — **LANDED 2026-07-28 at `ec66da798`**

> **The heading count was wrong on the day it was written.** It read "885 files, 255,664 LOC".
> Run the selector below at `9c1061da5`, the commit before the first deletion, and it returns
> **720 files / 199,899 LOC**. The by-package table further down sums to exactly 720, so this
> page disagreed with its own evidence. The commit removed **681 files / 185,114 lines**. The
> gap between 720 and 681 is the two carve-outs.
> 30 files still match the selector today.

Selector:
```bash
find . -name '*_test.go' -not -path './vendor/*' | grep -Ei '(hk[0-9a-z]{4,}|cqdef|n5md|pl[0-9]{3}|qm[0-9]{3}|bk[0-9]{2}|sh[0-9]{3}|ar[0-9]{3}|ev[0-9]{3}|bi[0-9]{3}|chb[0-9]{3})'
```

**CARVE-OUT A — the entire `internal/workflow/scenario/` directory.** The step-2 selector
matches `sentry_triage_faithful_hko52fm20_test.go` via `hk[0-9a-z]{4,}`, which is one of the
20 retracted files above and the only coverage of `specs/examples/sentry-triage-faithful.dot`.
Exclude the directory from the selector:
`| grep -v '^./internal/workflow/scenario/'`

**CARVE-OUT B — do not delete these, they are the acceptance tier.**
The list holds 28 paths, not the 26 quoted elsewhere in this plan. Checked 2026-07-30: 26 are
still in the tree. The two marked GONE went with the review-loop deletion on 2026-07-28
(`3cec5afd7`), which removed the mode they tested. A 27th scenario-tagged file,
`internal/daemon/rundriverfixture_test.go`, was never on this list and holds no test function.
```
internal/daemon/branchguard_test.go
internal/daemon/epiccompleted_scenario_hktfxjp_test.go
internal/daemon/mergetomain_perbead_target_hklgykq_test.go
internal/daemon/scenario_commit_gate_cap_hki8g59_test.go
internal/daemon/scenario_concurrent_multiqueue_hkumemp_test.go
internal/daemon/scenario_em012a_unlabeled_bead_dot_default_hk982_test.go
internal/daemon/scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go
internal/daemon/scenario_concurrent_dispatch_vn4_hkukhzu_test.go
internal/daemon/scenario_multibead_mergeconflict_serial_hktijaj_test.go
internal/daemon/scenario_flywheel_bt5_hk5pcr_test.go
internal/daemon/scenario_provenance_bt6_test.go
internal/daemon/scenario_gate_efficacy_hkv5dyg_test.go
internal/daemon/scenario_orphan_kill_hkbl2k6_test.go
internal/daemon/scenario_launch_liveness_slotleak_hk40c3y_test.go
internal/daemon/scenario_remote_substrate_localhost_test.go
internal/daemon/scenario_captain_crew_e2e_hkzi4ej_test.go
internal/daemon/scenario_queue_submit_dispatch_hksk00a_test.go
internal/daemon/scenario_remote_substrate_localhost_dot_test.go
internal/daemon/scenario_reviewloop_verdict_absent_salvage_hknhmbk_test.go   # GONE 2026-07-28
internal/daemon/scenario_remote_substrate_t4_claude_test.go
internal/daemon/scenario_reviewloop_em015de_hkintln_test.go                  # GONE 2026-07-28
internal/daemon/scenario_decisions_restart_s5_hkqed_test.go
internal/daemon/scenario_trust_clobber_hkqx065_test.go
internal/daemon/scenario_terminated_locked_hjvl4_test.go
internal/daemon/scenario_subworkflow_dispatch_hkx9l_test.go
internal/daemon/shortskip_internal_hkt7s_test.go
internal/daemon/scenario_restart_recovery_ivzsl_test.go
internal/daemon/scenario_sentinel_bt4_trip_clear_hk5v3r_test.go
```

By package. **This table is the trustworthy count.** It sums to 720, it reproduces exactly at
`9c1061da5`, and it is what proves the 885 in the heading wrong.
```
 317 ./internal/daemon
  68 ./cmd/harmonik
  54 ./internal/core
  53 ./internal/lifecycle
  47 ./internal/specaudit
  24 ./internal/keeper
  19 ./internal/workspace
  19 ./internal/brcli
  15 ./internal/queue
  10 ./internal/scenario
  10 ./internal/projectconfig
   9 ./internal/handler
   8 ./internal/workflow
   6 ./internal/codextest
   6 ./cmd/harmonik-twin-claude
   5 ./internal/lifecycle/tmux
   5 ./internal/hookrelay
   5 ./cmd/harmonik/supervise
   4 ./internal/queue/cli
   3 ./test/scenario
   3 ./internal/sentinel
   3 ./internal/runloop
   3 ./internal/orchestrator
   3 ./internal/handlercontract
   3 ./internal/eventbus
   3 ./internal/cognition
   2 ./internal/sessioncapture
   2 ./internal/runmerge
   2 ./internal/harness/pi
   2 ./internal/crewrun
   1 ./internal/workflow/scenario
   1 ./internal/workflow/dot
   1 ./internal/workers
   1 ./internal/presence
   1 ./internal/hooksystem
   1 ./internal/agentmanifest
   1 ./.harmonik/context/lima-hkdqo9u-pending
```

## DELETE 3 — dead production files — **PARTLY LANDED 2026-07-28 at `afdfccbd0`**

Claimed 35 files / 4,276 LOC on the basis of zero production callers.
**Only 8 of the 35 were provably dead. The commit removed 1,744 lines. The other 27 files are
still in the tree**, checked file by file on 2026-07-30 and marked below.

Two names on the list are misleading even where they read as done:
`internal/daemon/launchspecbuild.go` went with the review-loop deletion on 2026-07-28
(`3cec5afd7`), not with this step. `internal/handlercontract/watchertest.go` went with an
`export_test.go` pair in the same commit.

**Do not re-run this list as a delete set.** Re-run the detection instead, against the tree as
it stands after DELETE 2. The plan's own §2 said the set grows after the test deletion. What it
did not say is that the pre-deletion set was also wrong in the other direction.

```
DELETED  internal/core/policyexprenv.go
STILL    internal/core/budgetexhaustion_on048.go
STILL    internal/core/budgetexhaustion_rc018.go
STILL    internal/core/contextrestoreenforce_em046.go
STILL    internal/core/budgetcategorydefaults_on047.go
STILL    internal/core/budgetcounterstate_hka8bg25.go
DELETED  internal/core/cp021_guard_invocation_s01.go
STILL    internal/core/configprecedence_hka8bg38.go
STILL    internal/core/failureclass_107gz.go
DELETED  internal/core/cp010_gate_invocation_s01.go
STILL    internal/core/defaultroles_hka8bg29.go
STILL    internal/core/durability.go
STILL    internal/core/skillunion_cp050.go
STILL    internal/core/event_payload.go
STILL    internal/core/freedomprofiletightest_hka8bg33.go
STILL    internal/core/budgettightest_hka8bg21.go
DELETED  internal/daemon/launchspecbuild.go            # went with the review-loop deletion
DELETED  internal/daemon/composition_registry_hkndysh.go
DELETED  internal/daemon/failure_class.go
STILL    internal/lifecycle/branchtip_em024a.go
STILL    internal/lifecycle/immediateabort_pl012.go
STILL    internal/lifecycle/jsonldivergence_em031.go
STILL    internal/lifecycle/readystate_pl009.go
STILL    internal/lifecycle/startupstate_plinv003.go
DELETED  internal/keeper/nonce_provenance.go
STILL    internal/workspace/gitignorehygiene.go
STILL    internal/workspace/interruptstate_wm040.go
STILL    internal/workspace/implementerref_wm022.go
STILL    internal/workspace/conflictescalation_wm023.go
STILL    internal/workspace/conflictresolution_wm022a.go
STILL    internal/workspace/integrationbranch.go
STILL    internal/handler/classify_hc023.go
STILL    internal/handlercontract/skillresolution_hc047_hc048.go
DELETED  internal/handlercontract/watchertest.go
STILL    internal/queue/resume.go
```

The same commit also deleted three production files this list never named —
`internal/cognition/gitdone_ev041.go`, `internal/cognition/missingheartbeat_ev040.go` and
`internal/cognition/twophasedone.go` — plus two `export_test.go` files and one bead-named test.
So the list was wrong in both directions on the day it was written.

## KEEP

Sizes re-measured 2026-07-30.
```
test/scenario/                       # 6 Go files, 2,120 LOC, 12 test funcs, black-box
internal/daemon/*                    # 27 //go:build scenario files, 51 test funcs — acceptance tier
internal/scenario/queue_*            # 9 real queue tests, 4,253 LOC — relocate out of the harness pkg
internal/scenario/named_queues_*
internal/scenario/single_active_per_name_test.go
specs/                               # 26,068 LOC across 34 top-level files — the rewrite oracle
internal/queue/transaction.go        # landed, reviewed durable substrate — 1,005 LOC
```

The "11/11 green in 27s" claim was not re-run and should not be trusted. The scenario tier
carried `continue-on-error` in CI until 2026-07-29 and read green over 20 straight failures.
The measured state as of 2026-07-29 is 8 deterministic failures plus load flakes. See
`_plan.md` §7.

`internal/queue/transaction.go` is not reached only from tests. `internal/queuewiring/store.go`
calls `queue.WriteReplacement`, and it has done so since 2026-07-27, before this program began.
