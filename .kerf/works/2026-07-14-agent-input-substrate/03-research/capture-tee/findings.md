# 03-research / capture-tee — C4 live capture tee (apptap production wiring)

> Pass 3 (Research), capture-tee component (C4). Grounds Change-Design for splicing
> `internal/apptap.Tap` onto the M2 structured-input driver as apptap's first production consumer.
> All file:line verified against the tree on `phase1-session-restart-substrate`, 2026-07-14
> (parent-written; sub-agent returned text).

## Research questions
1. apptap API — full surface; does Tap require owning the child; process-ownership impedance vs a
   driver that owns its own process?
2. Corpus persistence patterns — capture-fixtures, codextest corpus, P1 keeper corpus, substrate
   replay corpus conventions.
3. Redaction — the event-model secret-scan precedent; does it apply to a raw byte stream?
4. ROADMAP orphan + shared-recorder case with the P1 "record keeper live" follow-up.
5. Session/file layout + retention/rotation precedents.

---

## Q1 — apptap API and process-ownership impedance

**API** (`internal/apptap/tap.go`, package = tap.go + tap_test.go only):
- `Tap` struct (:48-75): Binary (:50), Args (:53), InCapture io.Writer (:58), OutCapture io.Writer
  (:63), Stdin io.Reader (:66), Stdout io.Writer (:70), Stderr io.Writer (:74, "passed through
  unchanged; not captured").
- `Run()` (:82-145) is the ONLY method and **owns the child**: exec.Command(Binary, Args...) (:96),
  StdinPipe/StdoutPipe (:99-106), Start (:108), blocks in io.Copy until child stdout closes (:135),
  then Wait (:141). Splice goroutines wired before Start (:80-81).
- Capture: io.MultiWriter(childIn, InCapture) (:113-116), io.MultiWriter(stdout, OutCapture)
  (:119-122). Doc invariants (:9-24): transparent / lossless / verbatim ("no parsing, no framing, no
  drops"); protocol-agnostic — "any client or child drives it" (:21).

**Zero production consumers — CONFIRMED.** grep -rn apptap over *.go hits only tap.go + tap_test.go;
the only non-Go reference is specs/replay-substrate.md.

**Process-ownership impedance — REAL and HIGH.** Run() hard-codes exec.Command + pipes + Wait; no
constructor wraps already-held pipes, no exposed child handle (no kill/signal). If the M2 driver owns
the child (likely — it manages lifecycle/restart/JSON framing), Tap can't be used as-is. Shapes: (a)
driver delegates spawning to Tap.Run (awkward — Run blocks until exit, returns only the exit error, no
cmd exposure), or (b) refactor the tee to the pure MultiWriter splice and leave spawning to the caller.
`specs/replay-substrate.md` RS-016 (:181-186) anticipates the split: the tee "MUST own no file format
and MUST NOT open or name any file — persistence is the consumer's io.Writer", RS-005 forbids folding an
os/exec-importing tee into the stdlib-only substrate leaf; the only sanctioned co-location is
`internal/substrate/tap` with a file-scoped depguard quarantine for os/exec. A pipes-only tee drops the
quarantine problem entirely.

**Second impedance — fail-closed capture:** doc calls capture "a passive side-channel" (:7), but
io.MultiWriter is synchronous and fail-closed — a slow/erroring capture writer back-pressures or aborts
the production stream (tap.go:56-57: "Write errors on InCapture are propagated and abort the tap"). A
production tee must choose fail-closed (current) vs buffered/best-effort; C4's spec owes this.

## Q2 — Corpus persistence patterns

**Codex corpus** (Makefile): `capture-fixtures` (Makefile:123-133) — "deliberate, budget-capped corpus
capture", requires CODEX_LIVE=1; output testdata/codex-app-server/corpus/<session>.jsonl; ledger
testdata/codex-app-server/corpus/CAPTURE-LOG.md (manual update required); NOT part of make test/CI;
token budget one minimal turn. **Reality:** the corpus dir holds only raw-session-01.jsonl (6.6 KB, 23
frames per codexdigitaltwin/twin_test.go:11); **no CAPTURE-LOG.md exists anywhere** — the ledger RS-018
and the Makefile require was never written. L3 live test (codextest/l3_live_hkoe86p_test.go) is a wire
canary only and does NOT write corpus.

**Keeper corpus (P1):** no live recorder — corpus is EXTRACTED from the frozen event log:
`scripts/extract-keeper-corpus.py` reads .harmonik/events/baseline-2026-07-13/events.jsonl →
testdata/keeper-cycles/baseline-2026-07-13/ (cycles/<agent>__<cycle_id>.jsonl, per-cycle summary.json
goldens, manifest.json, EXTRACT-LOG.md ledger w/ source + script sha256 + counts — 507 cycles; ~4.0 MB).
Makefile:143-152: "No re-capture target exists (unlike codex capture-fixtures) … a deterministic
rebuild, not a token-capped capture."

**Substrate replay engine** (`internal/substrate/replay.go`, 201 lines): ReplayCodec[E] (:59),
Twin[E] (:82-85 wraps corpus io.Reader), NewTwin (:104), 1 MB default scanner buffer (:10-11)
WithBufferSize (:96). Encoding "append-only NDJSON" (replay-substrate.md:122). RS-018 item 3 (:204):
corpus under testdata/<vertical>/corpus/*.jsonl + CAPTURE-LOG.md. RS-INV-004 (:266-268): multi-appender
corpora MUST be EventID-sorted before replay.

## Q3 — Redaction precedent

Three live layers (the "event-model secret-scan" precedent):
1. **HC-031 field-name redaction** (handler-contract.md:575-577): field NAME matches
   `(?i)(secret|token|password|api[_-]?key|auth)` → "<redacted>". Code: internal/core/redaction.go:26.
2. **HC-032 per-handler value patterns** (handler-contract.md:581-585): value regexes (sk-ant-*)
   registered at init. Code: internal/core/redactionregistry.go:26; hooked into the bus at
   internal/eventbus/busimpl.go:41-43 (redact before append AND dispatch), busimpl.go:222.
3. **Normative anchors:** EV-035 (event-model.md:855-858), EV-036 (:862 compile-time no-secret-named
   field), fail-closed redaction_failed §8.8.5 (:269), EV-INV-006 (:956-958). Regression:
   internal/eventbus/hc034_no_secret_in_event_log_test.go.
4. **Commit-time:** scripts/secret-scan.sh — ERE deny-list over staged diff; lefthook-wired. Normative:
   CI-007, credential-isolation.md:72.

**Gap for C4:** all of the above operates on structured event payloads (field-name maps) or staged
diffs. The C4 tee captures a **raw byte stream** — HC-031 field-name redaction can't apply pre-parse,
and redaction-before-persist conflicts with apptap's "verbatim" invariant (tap.go:17-18). HC-034
(handler-contract.md:597-599: "No secret value MAY appear in any persisted … session log as stored to
disk") arguably binds a persisted capture file. Mitigant: HC-028 (:555-557) — secrets travel via
HARMONIK_SECRET_* env NOT the stdio stream, so the INPUT direction is structurally secret-free; the
OUTPUT direction (agent echoes a key it read) is the risk.

## Q4 — ROADMAP orphan + shared-recorder question

- ROADMAP.md:94 (decompose's "line 69" is stale): "`apptap` never wired to a production capture path …
  fold into M2 … + a P1 follow-up to record keeper live". Same table :96 (JSONL rotation orphan), :100
  (real-Claude restart integration test → P1 L3 or M2).
- TASKS.md:100 (M2-4) makes the shared-recorder call an explicit design-pass item: "corpus
  persistence/rotation/budget, redaction/secret-scan (event-model precedent), shared recorder infra with
  the P1 'record keeper live' follow-up".
- **Keeper has NO byte-stream recorder today.** internal/keeper/awaitack.go:59-63 PaneCapturer is polled
  tmux capture-pane snapshots (lossy pane text), not a stream; internal/keepertwin/codec.go replays
  events, not bytes. **Shared-recorder case is weak at the byte level** — keeper's input is tmux pane
  state (C3 reduces tmux to observation-only), M2's driver is a stdio stream. Shareable: the
  persistence/ledger/rotation CONVENTIONS (corpus dir layout, CAPTURE-LOG/EXTRACT-LOG ledger, budget-cap
  discipline, drift canary) — i.e. RS-018's template, not a common recorder type.

## Q5 — Session/file layout + retention precedents

- **Per-project event log:** .harmonik/events/events.jsonl — **89.5 MB, single file, no rotation**
  (matches ROADMAP.md:96 orphan). Frozen baselines snapshot as sibling dirs (baseline-2026-07-13/). No
  rotation code in internal/eventbus.
- **Per-session (worktree-scoped) artifacts:** workspace-model.md:73,551 — session-log dir at
  ${workspace_path}/.harmonik/sessions/${session_id}/ + harmonik.meta.json sidecar; S06 pre-creates +
  fsyncs before workspace_leased (:422); handlers own contents; S08/CASS reads read-only (:614). Natural
  home for a per-run captured corpus (per-session dir, sidecar join key, session_log_location HC-010).
- **Retention precedents:** internal/daemon/brhistoryrotate.go:41-48 keep-most-recent-20
  (brHistoryRotationDefaultKeep); per-close trim workloop.go:948-970; worktree age-prune
  internal/daemon/orphansweep.go:881 (DefaultHarmonikWorktreeMaxAgeDays). These are the ONLY
  rotation/retention mechanisms; nothing for JSONL/event streams.

## Patterns to follow
- **RS-016** (replay-substrate.md:181-186): tee owns no file format / opens no file; persistence is a
  caller-supplied io.Writer. Keep Tap byte-pure; persistence + redaction in the consumer.
- **RS-018 vertical template** (:198-214): corpus at testdata/<vertical>/corpus/*.jsonl + CAPTURE-LOG.md
  + drift canary + <V>_LIVE Makefile gate pair. C5 expects exactly this from C4.
- **Ledger discipline:** keeper's EXTRACT-LOG.md (source + sha + counts) is the executed example;
  codex's CAPTURE-LOG.md was declared but never written — make ledger emission mechanical.
- **Per-session home:** ${workspace_path}/.harmonik/sessions/${session_id}/ (WM §4.7) with the meta
  sidecar as join key; one capture file per direction fits the layout + CASS indexing.
- **Redaction hook:** reuse core.RedactionRegistry value-pattern shape (HC-032) as a post-capture scrub
  rather than inventing a mechanism; secret-scan.sh's ERE deny-list is the vocabulary.

## Risks / conflicts
1. **Process-ownership impedance (HIGH).** Tap.Run spawns+waits on the child itself, no pipe-wrapping
   constructor, no child handle; the M2 driver almost certainly owns the process. Expect a Tap API
   extension (pipes-in constructor) or reduction of C4 to the MultiWriter splice — RS-016 pushes this.
2. **Fail-closed capture back-pressure.** MultiWriter makes capture synchronous — a full disk / slow
   writer stalls or aborts live agent I/O. Spec must choose abort vs degrade.
3. **Verbatim vs redaction tension.** apptap's verbatim invariant collides with HC-034's
   no-secret-on-disk at persist time; field-name redaction needs parsed JSON, the tee is pre-parse
   bytes. Likely resolution: verbatim in-memory tee + HC-032-style value-pattern scrub in the
   persisting writer (or a post-capture scan gate before the file is retained).
4. **No live-recorder precedent to copy.** Both existing corpora were produced offline (spike capture;
   event-log extraction). C4 is genuinely first; capture-fixtures discipline exists on paper but its
   ledger was never written and its L3 test doesn't record. Don't over-trust "existing pattern".
5. **Shared-recorder scope creep.** Keeper has no byte stream to tee (pane snapshots only). Sharing
   should stop at conventions (layout/ledger/canary), not a common recorder abstraction.
6. **Retention unsolved upstream.** events.jsonl is 89.5 MB with no rotation. C4 must not inherit that
   defect — per-session capture files need an explicit retention/budget rule (nearest precedents:
   keep-N brhistoryrotate.go:48, age-prune orphansweep.go:881).
7. **Stale citation:** decompose's "ROADMAP.md:69" now resolves to :94; carry the corrected line forward.
