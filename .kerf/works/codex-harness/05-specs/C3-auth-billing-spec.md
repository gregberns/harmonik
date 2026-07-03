# C3 — Codex auth/billing guard — change spec

## Requirements (from 03-components.md)
R3.1 strip `OPENAI_API_KEY` + `CODEX_API_KEY` from child env (empty overrides). R3.2 materialize/verify
`forced_login_method="chatgpt"` in `$CODEX_HOME/config.toml`. R3.3 **pre-flight** `codex login status`
assert → fail closed if not a ChatGPT plan. R3.4 stable writable `$CODEX_HOME`. R3.5 documented
MUST-TEST items.

## Research summary
`04-research/auth-billing/findings.md`: `codex login` (ChatGPT) bills the **subscription**;
`--with-api-key` bills the **API credit pool**. `codex exec` inherits `codex login` creds from
`~/.codex/auth.json` under `$CODEX_HOME`; tokens auto-refresh (needs the same writable HOME). **Env
leak landmine:** older codex silently used `OPENAI_API_KEY` over a ChatGPT login (#2341); current
interactive ignores a bare env key (#3286), but `codex exec` env precedence is **undocumented and
version-variable** — the daemon path is the riskiest. Mitigation is defense-in-depth: strip +
`forced_login_method=chatgpt` + pre-flight assert. The `#2000` auto-generated-org-key wart means a
subscription login is not proof against an org key existing.

## Approach
The codex adapter's launch path applies the SAME credential strip+empty-override pattern claude uses
(`claudehandler_chb006_024.go:196-204,246-253,292-296`), targeting OpenAI keys, plus two extra guards:
1. **Env strip (primary, R3.1):** build the codex child env from an allowlist; never inherit `.env`;
   explicitly drop `OPENAI_API_KEY` and `CODEX_API_KEY` and re-emit them as empty overrides so the
   tmux server's additive `-e` cannot leak live keys. This is the direct analog of the
   `ANTHROPIC_API_KEY` guard and the single most important defense.
2. **Config belt (R3.2):** before the first codex run, materialize (or verify) `$CODEX_HOME/config.toml`
   contains `forced_login_method = "chatgpt"` (analogous to materializing `.claude/settings.json`).
   Idempotent; do not clobber an operator's other config keys.
3. **Pre-flight assert (R3.3):** at codex-adapter init (before any task turn), run `codex login status`
   and parse the result; if it does NOT report a ChatGPT plan (e.g. shows an API-key source), **fail
   the run closed** with a clear `codex_billing_guard` event. Asserting post-turn would be too late.
4. **$CODEX_HOME (R3.4):** set explicitly to a stable writable path (default `~/.codex`); the daemon
   must NOT spawn codex with an empty/sandboxed HOME (would break token refresh and silently
   re-prompt for auth). **Inherited-CODEX_HOME coverage (change-spec-review):** if the daemon
   inherited a pre-existing `CODEX_HOME` pointing at a *logged-out* home, the R3.3 pre-flight
   `assertChatGPTPlan` catches it transitively — `codex login status` fails → run fails closed. R3.4
   sets `CODEX_HOME` deterministically rather than trusting the inherited value, and R3.3 is the
   backstop; the two together close the logged-out-home case.

## Files & changes
- **MODIFY** `internal/daemon/codexlaunchspec.go` (C2) — env builder applies the strip+empty-override
  for `OPENAI_API_KEY`/`CODEX_API_KEY`; set `CODEX_HOME`.
- **NEW** `internal/daemon/codexbillingguard.go` — `materializeForcedChatGPT(codexHome)` (R3.2) and
  `assertChatGPTPlan(ctx) error` (R3.3); emits `codex_billing_guard` typed events.
- **MODIFY** the codex `CodexHarness` init (C2) to call the pre-flight assert before the first
  `LaunchSpec`.
- **NEW (docs)** captured in C6 R6.3 — the MUST-TEST checklist (R3.5).

## Acceptance criteria
- AC3.1 Unit test: with `OPENAI_API_KEY` and `CODEX_API_KEY` present in the daemon env, the codex
  child env contains neither (empty override asserted).
- AC3.2 Unit test: `materializeForcedChatGPT` writes/verifies `forced_login_method = "chatgpt"`
  idempotently and preserves other keys in an existing `config.toml`.
- AC3.3 Unit/integration test: `assertChatGPTPlan` returns an error and the run fails closed (emits
  `codex_billing_guard`) when `codex login status` (faked) reports an API-key source; passes when it
  reports a ChatGPT plan.
- AC3.4 The codex launch sets `CODEX_HOME` to a non-empty writable path (asserted).

## Verification
```
go test ./internal/daemon/... -run 'CodexBilling|EnvStrip|ForcedChatGPT'
# pre-production (MUST-TEST, manual, on the pinned codex version):
#   1) export OPENAI_API_KEY=sk-... ; run a codex bead ; confirm `codex login status`/`/status`
#      still shows the ChatGPT plan AND that the strip prevented API billing (check usage dashboards)
#   2) audit the OpenAI org for a "Codex CLI (auto-generated)" key (#2000)
#   3) confirm `forced_login_method` is honored by `codex exec` on the pinned version
```

## Error handling / edge cases
- `codex login status` unparseable / codex not installed → fail closed (`codex_billing_guard`,
  `codex_not_available`), never proceed assuming subscription.
- `$CODEX_HOME` not writable → fail closed at init.
- An operator deliberately wanting the API path → out of scope (subscription-only by design); a future
  opt-in could relax it, but the default fails closed.

## Migration / back-compat
Only affects codex runs; claude unaffected. No existing config keys renamed.
