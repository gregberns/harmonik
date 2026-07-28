# Keep / delete ledger

Generated 2026-07-27. Selection is mechanical — see `_plan.md` §2.
Validate after each step: `go build ./... && go test ./... && go test -tags=scenario ./test/scenario/... ./internal/daemon/...`

## DELETE 1 — spec-prose tests (37,927 LOC) — **LANDED 2026-07-27**
```
internal/specaudit/*_test.go carrying //go:build specaudit   # 129 files, greps specs/*.md
internal/specaudit/RELOCATED-ALLOWLIST.md
scripts/specaudit-lint.sh
+ build wiring: Makefile target & TAGGED_BUILD_TAGS, ci.yml step, scenario-gate.sh blocks
```

**KEPT** in `internal/specaudit/`: `doc.go` plus the 3 product-importing carve-outs
(`ar025_agent_type_regex`, `hqwn57_eventbus_interface`, `sh_inv005_declarative_loadable`).

**RETRACTED — `internal/workflow/scenario/` is NOT a delete target.** All 20 files import
`internal/core` + `internal/workflow` + `internal/workflow/dot` and drive the real engine
(`LoadDotWorkflow`, `DecideNextNode`) against `specs/examples/*.dot`. Deleting them drops
fixture coverage from 26/28 examples to 6/28. The original "0 LOC prod code" reasoning was
wrong: that measures files in the directory, not what the tests exercise.

## DELETE 2 — bead-ID-named test files (885 files, 255,664 LOC)

Selector:
```bash
find . -name '*_test.go' -not -path './vendor/*' | grep -Ei '(hk[0-9a-z]{4,}|cqdef|n5md|pl[0-9]{3}|qm[0-9]{3}|bk[0-9]{2}|sh[0-9]{3}|ar[0-9]{3}|ev[0-9]{3}|bi[0-9]{3}|chb[0-9]{3})'
```

**CARVE-OUT A — the entire `internal/workflow/scenario/` directory.** The step-2 selector
matches `sentry_triage_faithful_hko52fm20_test.go` via `hk[0-9a-z]{4,}`, which is one of the
20 retracted files above and the only coverage of `specs/examples/sentry-triage-faithful.dot`.
Exclude the directory from the selector:
`| grep -v '^./internal/workflow/scenario/'`

**CARVE-OUT B — do not delete these, they are the acceptance tier:**
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
internal/daemon/scenario_reviewloop_verdict_absent_salvage_hknhmbk_test.go
internal/daemon/scenario_remote_substrate_t4_claude_test.go
internal/daemon/scenario_reviewloop_em015de_hkintln_test.go
internal/daemon/scenario_decisions_restart_s5_hkqed_test.go
internal/daemon/scenario_trust_clobber_hkqx065_test.go
internal/daemon/scenario_terminated_locked_hjvl4_test.go
internal/daemon/scenario_subworkflow_dispatch_hkx9l_test.go
internal/daemon/shortskip_internal_hkt7s_test.go
internal/daemon/scenario_restart_recovery_ivzsl_test.go
internal/daemon/scenario_sentinel_bt4_trip_clear_hk5v3r_test.go
```

By package:
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

## DELETE 3 — dead production files (35 files, 4,276 LOC)

Zero production callers; referenced only by tests. Re-run detection after DELETE 2 — the set grows.
```
internal/core/policyexprenv.go
internal/core/budgetexhaustion_on048.go
internal/core/budgetexhaustion_rc018.go
internal/core/contextrestoreenforce_em046.go
internal/core/budgetcategorydefaults_on047.go
internal/core/budgetcounterstate_hka8bg25.go
internal/core/cp021_guard_invocation_s01.go
internal/core/configprecedence_hka8bg38.go
internal/core/failureclass_107gz.go
internal/core/cp010_gate_invocation_s01.go
internal/core/defaultroles_hka8bg29.go
internal/core/durability.go
internal/core/skillunion_cp050.go
internal/core/event_payload.go
internal/core/freedomprofiletightest_hka8bg33.go
internal/core/budgettightest_hka8bg21.go
internal/daemon/launchspecbuild.go
internal/daemon/composition_registry_hkndysh.go
internal/daemon/failure_class.go
internal/lifecycle/branchtip_em024a.go
internal/lifecycle/immediateabort_pl012.go
internal/lifecycle/jsonldivergence_em031.go
internal/lifecycle/readystate_pl009.go
internal/lifecycle/startupstate_plinv003.go
internal/keeper/nonce_provenance.go
internal/workspace/gitignorehygiene.go
internal/workspace/interruptstate_wm040.go
internal/workspace/implementerref_wm022.go
internal/workspace/conflictescalation_wm023.go
internal/workspace/conflictresolution_wm022a.go
internal/workspace/integrationbranch.go
internal/handler/classify_hc023.go
internal/handlercontract/skillresolution_hc047_hc048.go
internal/handlercontract/watchertest.go
internal/queue/resume.go
```

## KEEP
```
test/scenario/                       # 6 files, 2,093 LOC, 11/11 green in 27s, black-box
internal/daemon/scenario_*           # 26 //go:build scenario files — acceptance tier
internal/scenario/queue_*            # 9 real queue tests — relocate out of the harness pkg
internal/scenario/named_queues_*
specs/                               # 25,893 LOC — the rewrite oracle
internal/queue/transaction.go        # landed, reviewed durable substrate
```
