# Cluster spec-draft index — message-cluster (R-B)

Codename: `2026-07-18-keeper-restart-delivery` · pass 5 (spec-draft)

Index of every normative requirement cluster R-B adds (K2 deferral framing, K3 self-restart
payload, K4 configurable text). One-line summaries below; the full normative text lives in the
spec-named amendment file pointed to below.

## session-keeper.md → `05-spec-drafts/session-keeper-amendment.md` (v0.2.0 → v0.3.0)

### K2 — Deferral framing & the good-stopping-point contract (new §4.12)

- **SK-026** — The comms nudge body is a normative template with four required structural
  elements (defer-A, defer-B, good-stopping-point self-test, self-restart command + nonce); a
  template dropping any element is invalid and falls back to the compiled default.
- **SK-027** — The good-stopping-point self-test is the agent-legible four-part criterion (i–iv:
  between units, work re-derivable, no unanswered operator question, next session resumes with
  no redo); agent-owned, keeper nudges and bounds it.
- **SK-028** — The deferral sits under the unchanged FORCE-ACT + hard-ceiling backstop; changes
  no threshold value (NG1/SC-9); "take your time" is bounded, not open-ended.

### K3 — Agent-run self-restart as the default payload (new §4.13)

- **SK-029** — The agent-run `harmonik keeper restart-now --agent <name>` command is the default
  nudge payload; runs synchronously in its own process independent of the 300s handoff timeout;
  upholds SK-INV-001 (no `/clear` without a confirmed fresh handoff).
- **SK-030** — restart-now gains a net-new `--nonce <id>` flag, recorded on emitted
  events/journal; v1 semantics carry-for-audit, NOT hard-validate (no reject on mismatch).
- **SK-031** — Nonce provenance flows keeper cycle → message → command with no shared state:
  keeper mints `cyc-<ts>-<seq>` at cycle entry, message embeds it verbatim, agent runs it
  verbatim; auto-cycle marker and `--nonce` echo carry the same value.

### K4 — Configurable message text (new §4.14)

- **SK-032** — All nudge wording lives in the existing `.harmonik/config.yaml`
  `keeper.warn_messages` block, editable without a rebuild; accepts new leader defer-message
  keys plus a crew-message key defaulted off.
- **SK-033** — Message structure is normative, prose tunable via four fixed templated slots;
  `containsRestartNowCmd` validation is extended to all four elements — any override omitting a
  slot falls back to the compiled default.
- **SK-034** — `warn_messages` is re-read per tick, mtime-gated, for on-the-fly editing; scoped
  to `warn_messages` only (thresholds/bands/self-service stay startup-bound); unknown-key
  validation still applies.

## Notes

- ZERO threshold changes (NG1/SC-9); the backstop is preserved verbatim (SK-016 in force).
- No prior IDs renumbered or retired; sequential appends.
- Full normative prose, axes/tags, co-references, and revision-history entry: see
  `05-spec-drafts/session-keeper-amendment.md`.
