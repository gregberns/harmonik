# Dimension 4 spec — codex auth/billing guard

> Research dimension `auth-billing` (04-research/auth-billing/findings.md) → implementation.
> **Authoritative detail: `C3-auth-billing-spec.md`.** Dimension→component crosswalk + normative
> summary.

## Normative decision
codex MUST run on the **ChatGPT subscription** path (`codex login`), never the OpenAI API credit
pool (`--with-api-key`). Because `codex exec` env-var precedence is **undocumented and
version-variable**, enforce defense-in-depth (all three):
1. **Env strip:** drop `OPENAI_API_KEY` + `CODEX_API_KEY` from the codex child env with empty
   overrides — the direct analog of harmonik's `ANTHROPIC_API_KEY` guard
   (`claudehandler_chb006_024.go:196-204,292-296`). Single most important defense.
2. **Config belt:** materialize/verify `forced_login_method = "chatgpt"` in `$CODEX_HOME/config.toml`
   (idempotent; preserve other keys). Set `$CODEX_HOME` deterministically (writable, default
   `~/.codex`) for token refresh.
3. **Pre-flight assert:** run `codex login status` BEFORE the first task turn; fail the run closed
   (`codex_billing_guard` event) if it does not report a ChatGPT plan. (Post-spawn would be too late;
   also transitively catches an inherited logged-out `$CODEX_HOME`.)

## Implementing component
- **C3** (`C3-auth-billing-spec.md`): env strip (T10), `materializeForcedChatGPT` +
  `assertChatGPTPlan` pre-flight + events (T11).

## MUST-TEST (empirical, pre-production — see C6 R6.3 / T17)
1. Does the pinned `codex exec` honor `OPENAI_API_KEY`/`CODEX_API_KEY`? 2. Is `forced_login_method`
honored by `exec`? 3. Audit the OpenAI org for a "Codex CLI (auto-generated)" key (#2000) — a
subscription login is NOT proof against an org key. These are NOT closed by the three guards; they
gate enabling codex in production.
