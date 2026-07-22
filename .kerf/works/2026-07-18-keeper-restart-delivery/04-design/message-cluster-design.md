# Cluster design — message-cluster (R-B)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4 (change-design)

Cluster-level navigation + summary doc for research cluster **R-B**. Design decisions are
summarized faithfully here; normative text and full rationale live in the spec-named files
pointed to at the bottom.

## Scope & components covered

R-B defines *what the nudge says and does* once R-A has decided to deliver it over comms:

- **K2 — deferral framing & the good-stopping-point contract:** the message body template.
- **K3 — agent-run self-restart payload:** the command the agent runs to restart itself.
- **K4 — configurable message text:** where the wording lives and how it can be edited.

## Key design decisions

**K2 — the body is a normative template with four required structural elements** (prose
tunable, presence not): (1) defer condition A — if mid-conversation with the operator, finish
the exchange first; (2) defer condition B — if mid-task, finish the in-flight unit first; (3)
the good-stopping-point self-test; (4) the self-restart command (K3) carrying the cycle nonce.

**K2 — the good-stopping-point self-test is a four-part, agent-legible criterion:** a good
stop is one where nothing needed to continue lives only in the agent's context — (i) between
discrete units, not mid-edit/mid-plan/mid-tool-sequence; (ii) in-flight work committed or
trivially re-derivable; (iii) no unanswered operator question held; (iv) the next session
resumes from handoff + durable substrate with no redo. This self-assessment is agent-owned;
the keeper nudges and bounds it, it cannot read the agent's context and must not claim to
detect a task boundary.

**K2 — the deferral is legitimized only because the backstop is unchanged.** "Take your time"
is safe precisely because FORCE-ACT still cuts a never-idle session unconditionally and the
hard ceiling still trips session-independently. Zero threshold changes (NG1/SC-9); the
deferral is bounded, not open-ended.

**K3 — the agent-run `restart-now` command is the default payload.** `harmonik keeper
restart-now --agent <name>` runs a fully synchronous verify → freshness-check → ACK → `/clear`
→ brief in its own process, wholly independent of the cycle's 300s handoff-timeout window.
That is what closes the timeout gap: a handoff written at T+301s (after the keeper's watch
aborted) still restarts cleanly, because restart-now never consults the already-aborted cycle
timer. It upholds SK-INV-001 — it does not inject `/clear` without a confirmed fresh handoff;
it enforces the same ordering, it does not bypass it.

**K3 — the one real net-new gap is the nonce.** Today the keeper's cycle nonce does not flow
into restart-now: two disjoint schemes exist (the auto-cycle `cyc-...` KEEPER marker and
restart-now's never-validated `rn-<ms>` echo nonce), and restart-now has no `--nonce` flag. So
K3 adds a `--nonce <id>` flag (copying `ping`'s existing one), records the nonce on emitted
events/journal for audit, and defines a provenance channel with no shared state: keeper mints
the `cyc-id` at cycle entry → the K2 message embeds it verbatim in the `restart-now --nonce`
command string → the agent runs it verbatim. v1 semantics are **carry-for-audit, not
hard-validate** — the separate restart-now process does not hold the keeper's live cycle id,
matching how `ping` already treats its nonce.

**K4 — the config home already exists and is already wired.** `.harmonik/config.yaml` →
`keeper.warn_messages.{default_warn_text,actionable_warn_text}` is threaded to
`WatcherConfig`; editing the YAML needs no rebuild. K4 extends the same block with the new
leader defer-message keys (and a crew-message key defaulted off, per K7).

**K4 — structure-normative / prose-tunable, partially realized already.**
`containsRestartNowCmd` already rejects a custom actionable text that drops the `restart-now`
command and falls back to the compiled default. K4 extends this to validate all four K2
structural elements the same way, templating the four slots (defer-A, defer-B, stopping-point
test, restart-now+nonce) so an override fills only prose around fixed slots and cannot
silently drop a load-bearing element.

**K4 — "on the fly" editing.** Config is read once at keeper startup today, so an edit needs a
keeper bounce. K4 adds an mtime-gated per-tick re-read of *just* the `warn_messages` block
(stat each poll, re-parse only that sub-block on change). Scoped to `warn_messages` only —
thresholds, bands, and self-service flags stay startup-bound (no live-reload of any
load-bearing decision constant). Strict unknown-key validation still applies to the re-read.

## Requirement IDs this cluster produces

- session-keeper.md: **SK-026, SK-027, SK-028** (K2 §4.12); **SK-029, SK-030, SK-031** (K3
  §4.13); **SK-032, SK-033, SK-034** (K4 §4.14).

## Normative text and full rationale

- Design detail: `04-design/session-keeper-design.md` (K2, K3, K4 sections).
- Normative draft: `05-spec-drafts/session-keeper-amendment.md` (SK-026…SK-034).
- Cluster spec-draft index: `05-spec-drafts/message-cluster.md`.
- Grounding research: `03-research/message-cluster/findings.md`.
