# Mega Code-Review — NITS (Claude lane)

Nit-severity items: cosmetic, naming, stale-comment, misleading-doc, non-runtime. Kept for completeness (not dropped), out of the main FINDINGS report. Grouped by file. Source RU noted per item. Low-impact but real — a maintainer editing near these will be misled or slowed.

---

## internal/core

**eventregistry.go**
- Stale "71" event-type count across doc comments vs ~169 registered; comments also say "79"/"169" — mutually inconsistent. (`:186`, RU-10)
- `CurrentPayloadSchemaVersion` is a pure alias with no non-test caller — duplicate exported API to keep in sync. (`:205`, RU-10)

**eventtype.go**
- Header says "~79 constants" but 179 EventType constants defined (>2x off); misrepresents §8 taxonomy coverage on a spec-anchored file. (`:6`, RU-10b)

**pertypecompat_hqwn38.go**
- Per-type schema-version / N-1 compat apparatus is entirely speculative: every row `{v1, prev0, CompatWindowHolds:true, AdditiveOnly:true}`; the single live reader only checks the unconditionally-true field. ~300-line table + registration API delivering nothing today. (`:52`, RU-10)

**daemonevents_hqwn59.go**
- Payload field self-flagged "stale, needs cross-spec amendment" with a dangling TODO; consumers can't tell which shape is authoritative. (`:272`, RU-10b)

**56 of 237 source files carry opaque bead-id filename suffixes** (agentevents_hqwn59.go, etc.) — navigation/grep-by-topic tax; contradicts the project's "say the thing, not the pointer" guidance. (RU-10b)

**Pervasive deferred-typing debt** — dozens of identifier fields (SkillVersion, BatchID, ControlPointName, CommitHash, OperatorID, GuardRef, HookInvocationID, ToolName, ...) are plain `string` with "TODO: hoist to typed alias" notes. (`core/*_hqwn59.go`, RU-10b)

---

## internal/handler

**claudehandler_chb006_024.go**
- Exported function name typo `DeriveCIaudeTranscriptPath` (capital-I for lowercase-l in "Claude") — baked into the package API. (RU-08)
- `os.UserHomeDir` failure falls back to literal `~` -> non-expandable transcript path. (`:705`, RU-08)

---

**substrate.go**
- `substrateSessionAdapter.CloseStdin` discards the caller context (uses `context.Background()`) -> a structured `InputPort` whose `CloseInput` blocks is unbounded here. Not a contract violation (the interface takes no ctx) but a silent loss of any deadline. (`:187`, RU-08 deep)

## internal/handlercontract

- Fragmented into ~50 one-spec-clause-per-file units (HC-xxx); related state scattered, filenames encode tracking IDs not content. Contradicts "say the thing, not the pointer." (RU-08)
- **orphancheck_hc044a.go** — detection-algorithm doc references a nonexistent parameter `ownedByDaemon(pid)`; the actual parameter is `ownedByCurrentGen map[int]bool`. Misleads a maintainer grepping for the helper. (`:160`, RU-08 deep)
- **watcher_hc011.go** — `isLineTooLong` compares `err == bufio.ErrTooLong` instead of `errors.Is`; works today (sentinel returned unwrapped) but brittle — a future wrapped `ErrTooLong` would misclassify to `PartialMessageSubReason`. (`:684`, RU-08 deep)

---

## internal/hookrelay

**hookrelay.go**
- `EmittedAtNs` is `time.Since(start).Nanoseconds()` — a build-latency delta, not the "invocation-start timestamp" its consumer-side doc (`hook/sessionstore.go:52`) claims. Observability-only, but misleads anyone correlating emission times across relay invocations. Rename to `build_latency_ns` or emit an actual timestamp. (`:223`, RU-14 deep)

---

## internal/codexwire

**codexwire.go**
- `parseResult` is a no-op stub returning nil with a doc comment describing behavior it doesn't implement — leftover scaffolding. (`:275`, RU-07)

---

## internal/lifecycle/tmux

**windowname.go**
- Unused constant `hashSuffixLen` — will drift from the actual hash width. (`:14`, RU-05b)

**orphansession.go / orphanwindow.go**
- `SweepOrphanHandlers` "killed" count includes processes already dead before SIGTERM — metric only. (`orphansweep.go:368`, RU-12)

---

## internal/queue

**append.go**
- Dead computed-and-discarded expression `_ = tailStart + i` in the deferred-event loop. (`:194`, RU-09)

**rpc.go**
- `appendUnderLock` disk-fallback safety comment relies on an invariant `submit` violates — false assurance. (RU-09)

---

## internal/sentinel

**governor.go**
- Redundant if/else staircase where both branches do `ConsecutiveLowWindows++`; with default config the moderate band is empty — misleads a reader into thinking it's treated differently. (`:517`, RU-23)

---

## internal/daemon

**dot_cascade.go**
- Stale absolute line-number cross-references in comments. (`:916,891`, RU-02)

**tmuxsubstrate.go**
- Production constructor `newPerRunSubstrate` calls a test-only seam `notifySubstrateRunner` on every construction — hot path per bead run. (RU-05a)

**projectconfig.go / router**
- (See FINDINGS Low items — these are borderline; nits here limited to comment/naming.)

---

## internal/keeper

**watcher.go**
- Direct `fmt.Printf` to stdout from a long-lived daemon path (emitWarn/maybeRespawn/maybeLivePaneRecover) — can't be routed/leveled/silenced. (`:1560,1615,1722`, RU-13)
- Staleness check uses pre-heartbeat modTime -> a just-refreshed gauge takes the stale branch for one tick. (`:1104`, RU-13)

---

## internal/scenario

**orchdrive.go**
- Stale TODO claims `TimeoutSecs` is unwired but `harness.go:463` already populates it. (`:90-94`, RU-22)

---

## internal/operatornfr

**doc.go**
- Frames `operatornfr` as exit-code-only but it now spans 8 obligation areas — misleading entry point. (RU-20)

**commandcodes.go**
- Hand-rolled `itoa` to avoid importing `strconv` — needless, untested negative branch. (`:252`, RU-20)

---

## internal/workflow

**dispatcher.go**
- Condition bridge keys evaluator lookup by raw condition string rather than edge identity — avoidable dot/core coupling. (RU-15)

---

## internal/workspace

**gitignorehygiene.go**
- Docstrings say "four required patterns" but `RequiredGitignoreEntries` has six. (RU-04b)

---

## internal/specaudit

**sh_inv005_declarative_loadable_test.go**
- `TestSHINV005SpecBodyAudit` greps spec prose for enforcement-phrase substrings — a prose lint masquerading as a conformance sensor; inflates the SH-INV-005 test count. (`:162`, RU-21)

---

## internal/keepertest

**canary_test.go**
- Redundant frozen anchor: `count` duplicates `started` (both 507). (`:28`, RU-19)

---

## cmd/harmonik

**confirm_verdict.go**
- `sendVerdictOverrideRequest` doc + stderr prefix hardcoded to `confirm-verdict` on the shared veto path — confusing during triage. (RU-16a)

**supervise_cmd.go**
- Unrecognised-verb error omits valid verbs (reap) — a hand-maintained duplicate of the switch, already drifted. (RU-17)

**init_cmd.go**
- goal-keeper schedule seeds `Argv[0]="harmonik"` from PATH rather than `os.Executable()` — inconsistent with the same file. (`:873`, RU-16b)

**eval_cmd.go**
- eval collect emits records in nondeterministic map-iteration order. (`:181`, RU-16b)

**eval_metrics_cmd.go**
- `evalGocycloMax` conflates "no gocyclo output" with a real max of 0. (`:296`, RU-16b)

**version.go**
- Observed version discards pre-release suffix -> guaranteed mismatch warning if pinned carries one. (`:98`, RU-18)

---

## cmd/harmonik/assets/scripts

**keeper-statusline.sh**
- Omits the `HARMONIK_KEEPER_AGENT` back-compat alias the three other hooks honor — divergent agent identity. (RU-24)

---

## internal/brcli

**show.go / version.go** — see cmd version.go above; brcli nits folded into FINDINGS Low.

---

## Test-only / harness nits (non-shipping)

- `internal/keeper*` / `l2_integration_test.go:163` — `drainTwin` 2s wall-clock idle timeout can produce a false failure under load/-race (test-only). (RU-19)
- `internal/specaudit/ar025_agent_type_regex_test.go:79` — regex matches the first `agent_type :=` anywhere, not scoped to §6.1 (test-only). (RU-21)

---

## Recovered minor items (batched — real but low-impact, dropped by first-pass condensers)

- `internal/lifecycle/tmux/osadapter.go:651` — isNotFoundErr/isNoSessionErr/isWindowCollisionErr rely on brittle substring matching. (RU-05b)
- `internal/keeper/tmuxresolve.go:312` — `recentTranscriptTurn` ignores scanner error, silently truncating on an over-long line. (RU-05b)
- `internal/eventbus/jsonlwriter.go:331` — ScanAfter can't distinguish a torn tail from genuine corruption; logs every replay. (RU-11)
