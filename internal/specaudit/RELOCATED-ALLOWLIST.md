# specaudit — relocated spec-prose test allowlist (M1-1)

The 129 test files listed below are **spec-drift lint** sensors: they walk
`specs/*.md` and assert structural invariants, executing ZERO product code.
They are relocated OUT of the default `go test ./...` run behind the
`//go:build specaudit` build constraint so the default build stays fast.

**Run them:** `make specaudit-lint` (equivalently
`go test -tags specaudit ./internal/specaudit/...` or `scripts/specaudit-lint.sh`).

CI runs them via the `make specaudit-lint` step in `.github/workflows/ci.yml`,
and `scripts/scenario-gate.sh` runs a dedicated `-tags=specaudit` pass whenever
`specs/`, `cmd/harmonik/`, or `internal/daemon/socket.go` changes.

**NOT tagged (3 product-importing carve-outs that stay under default `go test`):**
`ar025_agent_type_regex_test.go`, `hqwn57_eventbus_interface_test.go`,
`sh_inv005_declarative_loadable_test.go`.

## Tagged files (129)

- ar001_axes_test.go
- ar005_tags_test.go
- ar006_mechanism_no_llm_test.go
- ar013_envelope_declaration_test.go
- ar016_subsystem_go_package_test.go
- ar027_four_surfaces_test.go
- ar032_roles_test.go
- ar041_repo_sot_test.go
- ar052_spec_category_frontmatter_test.go
- bi010d_reset_adapter_sensor_test.go
- bi028_skill_launch_context_test.go
- chbinv003_xlach_test.go
- ev005_lifecycle_boundary_signals_test.go
- ev011_async_consumer_class_test.go
- ev011a_nonblocking_backpressure_test.go
- ev014c_observer_goroutine_queue_test.go
- ev023_divergence_evidence_read_test.go
- evinv001_events_observational_test.go
- extqueue_enqueue_retired_test.go
- hc004_launch_idempotent_test.go
- hc005_launchspec_delivery_test.go
- hc014_channel_closure_test.go
- hc015_mutex_discipline_test.go
- hc024a_socket_io_distinct_crash_test.go
- hc025_rate_limit_distinct_failure_test.go
- hc026b_drain_forced_hang_on_classified_test.go
- hc027_dead_letter_undeliverable_test.go
- hc028_secrets_env_prefix_test.go
- hc029_agent_started_no_env_test.go
- hc030_redaction_middleware_test.go
- hc033_event_schema_payload_check_test.go
- hc035_twins_handler_interface_test.go
- hc045a_claudecode_bridge_pointer_test.go
- hc045b_hookrelay_connection_regime_test.go
- hc046_skill_provisioning_test.go
- hc050_skill_declarations_launchspec_test.go
- hc052_execution_shape_evolution_test.go
- hcinv003_no_secret_in_eventbus_test.go
- hqwn05_uuidv7_hwm_test.go
- hqwn11_partial_order_contract_test.go
- hqwn12_subscription_declared_test.go
- hqwn13_synchronous_consumer_class_test.go
- hqwn16_observer_consumer_class_test.go
- hqwn17_consumer_class_optin_test.go
- hqwn18_class_conflict_startup_error_test.go
- hqwn20_consumer_idempotent_replay_test.go
- hqwn22_consumer_recovery_replay_test.go
- hqwn23_jsonl_durable_form_test.go
- hqwn24_durability_class_test.go
- hqwn25_event_loss_acceptable_test.go
- hqwn29_jsonl_append_only_test.go
- hqwn43_registry_startup_test.go
- hqwn46_secret_prefix_check_test.go
- hqwn48_evinv002_event_loss_invariant_test.go
- hqwn63_eventmodel_taxonomy_test.go
- on011_state_machine_serializable_test.go
- on012_improvement_pause_subtype_test.go
- on013_operator_events_per_transition_test.go
- on013c_command_idempotency_test.go
- on019_migration_release_paused_test.go
- on027_drain_step1_stop_queue_test.go
- on027s2_drain_step2_inflight_checkpoint_test.go
- on027s3_drain_step3_handler_wait_test.go
- on027s3a_drain_step3a_intent_log_test.go
- on027s4_drain_step4_eventbus_flush_test.go
- on027s5_drain_step5_memory_flush_test.go
- on027s6_drain_step6_workspace_unlock_test.go
- on027s7_drain_step7_orchestrator_exit_test.go
- on030_restart_reconstruction_test.go
- on035_structured_logs_test.go
- on042_multi_tenancy_deferred_test.go
- on044_distributed_tracing_deferred_test.go
- on051_multi_attach_arbitration_test.go
- on053_panic_forensic_file_test.go
- oninv006_no_control_surface_bypass_test.go
- pl007_orphan_sweep_deterministic_test.go
- pl010_degraded_state_cat0_test.go
- pl014a_concurrency_ceiling_test.go
- pl018a_panic_recovery_barrier_test.go
- pl020_composition_root_test.go
- pl027_upgrade_contract_daemon_test.go
- rc_section8_categories_test.go
- rc011_dag_uuidv7_ordering_test.go
- rc019a_evidence_corroboration_test.go
- rc028_reopen_bead_new_run_test.go
- rc029_intra_run_rollback_test.go
- rcinv001_workflow_uniqueness_test.go
- rcinv004_evidence_corroboration_guarantee_test.go
- sh_section8_taxonomy_test.go
- sh002_scenario_discovery_test.go
- sh003_declarative_loadable_test.go
- sh004_malformed_scenario_test.go
- sh005_name_uniqueness_test.go
- sh006_suite_load_phase_test.go
- sh007_execution_order_test.go
- sh009_twin_binary_discovery_test.go
- sh010_missing_twin_binary_test.go
- sh012_fixture_setup_test.go
- sh013_isolated_workspace_test.go
- sh015_fixture_teardown_test.go
- sh015a_workspace_snapshot_inplace_test.go
- sh020_jsonl_event_log_assertion_test.go
- sh022_workspace_state_predicates_test.go
- sh023_assertion_verdict_test.go
- sh024_event_log_capture_failure_test.go
- sh025_timeout_secs_test.go
- sh026_timeout_verdict_test.go
- sh028_no_external_network_test.go
- sh030_parameter_matrix_test.go
- sh031_sequential_scenarios_test.go
- sh032_harness_cli_test.go
- sh033_signal_handling_test.go
- sh034_result_durability_test.go
- shinv001_no_testmode_branches_test.go
- shinv002_workspace_reset_test.go
- shinv003_twin_only_test.go
- wm013d_released_path_reuse_forbidden_test.go
- wm029_session_log_read_only_test.go
- wm030_session_log_retention_default_test.go
- wm034_reopen_bead_fresh_run_id_test.go
- wm035_intra_run_rollback_keep_worktree_test.go
- wm036_verdict_enum_classification_test.go
- wm038_interrupt_state_sole_writer_test.go
- wm038a_interrupt_state_marker_test.go
- wm039_workspace_interrupted_emitter_test.go
- wm040_interrupt_state_reset_test.go
- wminv003_task_branch_append_only_test.go
- wminv005_canonical_path_no_registry_test.go
- zs04_baseline_axis_test.go
