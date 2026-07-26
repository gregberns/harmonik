# Pass 5 spec-draft changelog — new requirement IDs per spec

Codename: `2026-07-18-keeper-restart-delivery` · pass 5 (spec-drafts)
Amendment drafts in `05-spec-drafts/`. IDs slot in as sequential gap-fillers after each
spec's highest existing ID; no existing IDs are renumbered or retired.

## specs/session-keeper.md  (v0.2.0 → v0.3.0)  — `session-keeper-amendment.md`

Highest existing before: requirement SK-021, invariant SK-INV-005.

K1 — Delivery channel & reachability (new §4.11):
- SK-022 — Leader nudge delivered over comms `agent_message`, not a terminal paste; no `--wake`.
- SK-023 — Reachability = presence-Online (< 120s) read in-process; necessary-but-not-sufficient limitation stated.
- SK-024 — Deterministic delivery decision table (comms vs terminal-fallback); no silent no-op.
- SK-025 — Terminal fallback preserves `injectTextClocked` + the 750ms-settle retry-Enter loop (NG3/hk-89g).

K2 — Deferral framing (new §4.12):
- SK-026 — Four required structural elements in the defer template.
- SK-027 — The four-part good-stopping-point self-test.
- SK-028 — Deferral sits under the unchanged FORCE-ACT backstop; zero threshold changes.

K3 — Agent-run self-restart (new §4.13):
- SK-029 — `keeper restart-now` as the default payload; independent of the 300s watch; upholds SK-INV-001.
- SK-030 — Net-new `--nonce` flag on restart-now, carry-for-audit (not hard-validate).
- SK-031 — Nonce provenance channel: mint (cycle entry) → embed (message) → record (events/journal).

K4 — Configurable message text (new §4.14):
- SK-032 — `keeper.warn_messages` external config home; edit without rebuild.
- SK-033 — Structure-normative / prose-tunable templated slots; extended `containsRestartNowCmd` validation.
- SK-034 — mtime-gated per-tick re-read of `warn_messages` only; thresholds stay startup-bound.

K5 — Situational-read sharpening (new §4.15):
- SK-035 — In-cycle operator-attached TOCTOU re-check.
- SK-036 — Reachability/liveness pre-check feeds the K1 delivery decision.
- SK-037 — Hook-bridge keystroke signal named as an out-of-scope external dependency.

Invariant (§5):
- SK-INV-006 — Leader nudge delivery is total (comms or terminal-fallback, never a silent no-op).

Total: 16 requirements (SK-022…SK-037) + 1 invariant (SK-INV-006).

## specs/agent-input.md  (v0.1.0 → v0.2.0)  — `agent-input-amendment.md`

Highest existing before: AIS-018.

- AIS-019 — Keeper is a recognized comms producer (`--from keeper`, `--topic keeper`).
- AIS-020 — Presence-Online as the producer reachability read; necessary-but-not-sufficient.

Total: 2 requirements (AIS-019, AIS-020).

## specs/scenario-harness.md  (v0.2.3 → v0.2.4)  — `scenario-harness-amendment.md`

Highest existing before: SH-034.

- SH-035 — Keeper pane/timing/handoff/operator-typing coverage rides the session-twin integration tier, outside the SH YAML contract.
- SH-036 — At most one wire-observable comms-fallback scenario MAY be added; the §10.1 conformance floor is untouched.

Total: 2 requirements (SH-035, SH-036).

## specs/park-resume-protocol.md  (v1.1.0 → v1.2.0)  — `park-resume-protocol-amendment.md`

Contract-shape spec — no requirement-ID prefix. K7 disposition added as a new normative-prose
§9 subsection "Crew keeper-message disposition (K7 — DEFERRED)" (defer crew message; ship
config hook default-off; activation gated on hk-220lv +
hk-4tjyj). crew-handoff-schema.md is NOT amended (crew message is keeper config, not
mission-file frontmatter).

Total: 0 new requirement IDs (prose disposition + changelog bump).
