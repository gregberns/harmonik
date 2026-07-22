# 07 — Step-0 build skeleton (NON-LIVE prep): codex-app-server Phase 1 T1 + T2

> codename:codex-app-server · parent epic hk-q3ovr · Codex lane (piter)
> **This is prep/design, NOT implementation.** Goal: front-load the exact buildable shape so the
> eventual implementer's worker turns are SHORT and low-token. Every choice is grounded in the repo's
> real conventions with a file citation. Skeleton = interface/type sketches + file paths + gate-as-test
> outlines. No full bodies.
>
> Authority: `plans/2026-07-11-codex-app-server-replan/PHASE-1-tap-serializer-reactor.md`;
> tasks `07-tasks.md` (T1 hk-893ct, T2 hk-tg5mo); wire surface `03-research/C1-protocol/findings.md`.

## Convention note that changes the package paths (read first)

`docs/foundation/project-level/subsystem-organization.md` §Go module layout shows an *aspirational*
nested tree (`internal/handler/contract`, `internal/adapter/br`). **The real repo tree is FLAT** — the
existing codex integration lives as `internal/handlercontract` (not `internal/handler/contract`), and
`internal/` is a flat list of ~40 single-word packages (`internal/queue`, `internal/schedule`,
`internal/agentmanifest`, `internal/crew`, …). Verified: `ls internal/` + import lines in
`internal/daemon/codexlaunchspec.go:35-37` (`internal/handler`, `internal/handlercontract`).

**Therefore both new packages take flat, single-word names** matching the live convention, each tagged
`codename:codex-app-server`. The `go-subsystem-add` skill's Step-1/Step-3 mechanics (doc.go +
`<pkg>.go` + `<pkg>_test.go`, one depguard `rules.<name>` entry) still apply verbatim; only the path
shape follows the flat live tree, not the doc's nested sketch.

---

## T1 — TAP (capture layer) — bead hk-893ct

### Package path

`internal/codextap/` — flat, single-word, matching the live `internal/queue` / `internal/schedule`
leaf-package convention (`ls internal/`). Scaffold per `go-subsystem-add` §2:

```
internal/codextap/
  doc.go              # package doc; cites this plan + specs (no imports)
  codextap.go         # Tap type + Splice()  (primary type per go-subsystem-add §2b)
  capture.go          # RawWriter (verbatim .jsonl sink) + on-disk layout
  index.go            # SEPARATE read-only correlation pass (never mutates capture)
  codextap_test.go    # external pkg smoke + the transparency gate
```

`doc.go` names the bead and spec per the skill's doc.go template
(`go-subsystem-add/SKILL.md` §2a).

### Shape — a transparent stdio splice (NO parsing-for-meaning)

app-server stdio is newline-delimited JSON: **one line = one frame**
(`PHASE-1-tap-serializer-reactor.md` §1; `C1-protocol/findings.md` §1 "stdio (newline-delimited
JSON)"). The tap tees each direction to raw `.jsonl` verbatim — it copies bytes, it does NOT decode
them for meaning. Byte preservation is the whole invariant: a wire surprise must land in the capture
intact, never be dropped or normalized.

```go
package codextap

// Direction tags which side of the stdio pipe a captured frame came from.
type Direction string

const (
    ToServer   Direction = "c2s" // client stdin  → codex app-server
    FromServer Direction = "s2c" // codex app-server → client stdout
)

// Tap splices a child codex app-server's stdio, teeing both directions to a raw
// verbatim .jsonl sink while passing bytes through untouched. It NEVER parses a
// frame for meaning — a line is an opaque byte run terminated by '\n'.
type Tap struct {
    sink *RawWriter
}

// Splice wires child stdio to the outer client and tees every byte to sink.
//   clientIn   : bytes the driving client writes  (→ child stdin,  tag ToServer)
//   childOut   : bytes the child app-server writes (→ client,      tag FromServer)
// Returns readers/writers to hand to the child process; each is an io.TeeReader
// (or a tee-Writer) whose second sink is the RawWriter. Pure byte plumbing.
func (t *Tap) Splice(clientIn io.Reader, childStdin io.Writer,
                     childStdout io.Reader, clientOut io.Writer) error

// teeLine is the per-direction copy primitive: it splits the passthrough stream
// on '\n' ONLY to attach a direction tag + framing to the capture; the bytes
// forwarded to the real peer are the original unmodified bytes. Splitting is for
// the capture sidecar, never for interpreting the frame.
```

Design rules (all from `PHASE-1-tap-serializer-reactor.md` §1 / `07-tasks.md` T1):
- Verbatim: forward original bytes; the capture is a faithful copy, never a re-encode.
- Never drops a line — backpressure on the capture sink must block, not discard.
- Any client can drive it (incl. the OpenAI VS Code extension) to harvest scenarios cheaply.
- Indexing/correlation is a **separate read-only pass** (`index.go`) so a wire surprise corrupts the
  index, never the capture.

### Capture file format + on-disk layout

Raw sink writes newline-delimited JSON, one captured frame per line, each wrapped in a thin **capture
envelope** that adds ONLY direction + monotonic sequence + wall time — the payload stays the exact
verbatim frame bytes as `json.RawMessage` (never re-marshaled). This mirrors the existing on-disk
JSONL convention in `internal/testhelpers/jsonlfixture.go` (one JSON object per line, exactly one
trailing `\n`, snake_case keys, torn-tail = missing final newline).

```
.harmonik/codex-captures/<session-id>/
  raw.jsonl        # append-only, verbatim; the GROUND TRUTH — never rewritten
  index.jsonl      # produced by the separate read-only pass; safe to delete/rebuild
  meta.json        # capture session metadata (client name, codex version, start ts)
```

Capture-envelope line shape (raw.jsonl):

```jsonc
{"seq":0,"dir":"c2s","t":"2026-07-11T14:22:11.001Z","frame":{...verbatim JSON-RPC frame...}}
```

- `frame` is `json.RawMessage` — the original bytes between newlines, byte-for-byte. No field of the
  frame is inspected by the tap.
- `seq` is a monotonic per-session counter for the correlation pass to order/pair request↔response.
- The **index pass** (`index.go`) reads `raw.jsonl` read-only and emits `index.jsonl`
  (id→method, request↔response pairing, turn/item grouping). It opens `raw.jsonl` `O_RDONLY`; a parse
  failure in the index is a red index test, never a mutation of the capture.

### The transparency / lossless GATE — executable test sketch

`PHASE-1-tap-serializer-reactor.md` §1 Gate + `07-tasks.md` T1 Gate: run a real client through the
tap for one happy turn; the child works E2E AND the concatenated captured `raw` frame-bytes
diff-match an **untapped control run**.

```go
// internal/codextap/codextap_test.go
package codextap_test

// L3-adjacent gate: env-gated live (CODEX_LIVE=1) OR driven by the T4 twin once
// it exists. Proves the tap is transparent + lossless by byte-diff vs a control.
func TestTapTransparentAndLossless(t *testing.T) {
    t.Parallel()
    if os.Getenv("CODEX_LIVE") != "1" { t.Skip("live wire-contract canary; CODEX_LIVE=1") }

    // 1. CONTROL: drive one happy turn against codex app-server with NO tap,
    //    recording the raw stdio bytes both directions (control transcript).
    control := runOneTurnUntapped(t)          // []capturedFrame, verbatim bytes

    // 2. TAPPED: same driver, same prompt, THROUGH codextap.Tap.Splice.
    tapped := runOneTurnThroughTap(t)          // reads .harmonik/codex-captures/<sid>/raw.jsonl

    // 3. Child worked E2E in BOTH: each transcript ends with a turn/completed frame.
    testhelpers.AssertTrue(t, endsWithTurnCompleted(control))
    testhelpers.AssertTrue(t, endsWithTurnCompleted(tapped))

    // 4. LOSSLESS: concatenated captured frame-bytes are byte-identical to control.
    //    (Compare per-direction concatenation; wall-time/seq envelope fields excluded —
    //     only the `frame` RawMessage bytes are diffed.)
    testhelpers.AssertBytesEqual(t, concatFrames(control, ToServer),   concatFrames(tapped, ToServer))
    testhelpers.AssertBytesEqual(t, concatFrames(control, FromServer), concatFrames(tapped, FromServer))
}
```

Uses `internal/testhelpers` assert helpers (`assert.go`) and the `jsonlFixture`-prefix helper
discipline for any shared fixtures (`internal/testhelpers/jsonlfixture.go` header).

### depguard matrix entry needed

Add ONE `rules.codextap` entry under `linters-settings.exclusions.depguard.rules` in `.golangci.yml`
(the live block that already holds `queue`, `schedule`, `agentmanifest`, `crew`, `keeper`). The tap is
a **leaf** — stdlib + self-import only, deny `internal/daemon`, exactly like the `schedule` /
`agentmanifest` leaf rules already present (`.golangci.yml` lines ~163, ~173):

```yaml
        # codextap: transparent stdio splice → verbatim .jsonl capture
        # (codename:codex-app-server, hk-893ct). Leaf: stdlib + self-import only.
        # MUST NOT import daemon. Byte-plumbing has no harmonik-internal deps.
        codextap:
          files: ["**/internal/codextap/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/codextap"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "codextap MUST NOT import daemon (leaf capture primitive)" }
```

Note: the global `llm-sdk-ban` rule (`.golangci.yml` ~line 97, denies `github.com/openai/`) already
covers this package — the tap must never link an SDK; it only shuttles bytes. Good, that is the
intended posture.

### testhelpers hookup

- Smoke test in external package `codextap_test`, `t.Parallel()` on every func (go-subsystem-add
  §2c; `docs/methodology/TESTING.md`).
- Reuse `internal/testhelpers` `assert.go` + `tempdir.go` for the capture-dir fixtures; if a shared
  raw-frame fixture is needed, follow `jsonlfixture.go`'s pattern (vend raw `[]byte` JSONL,
  `jsonlFixture`-prefixed helpers) so tests decode through the code under test, not a hand-rolled
  struct.

---

## T2 — SERIALIZER (wire-contract layer) — ★ FIRST CHECKPOINT — bead hk-tg5mo

### Package path

`internal/codexwire/` — flat leaf, same convention. Scaffold per `go-subsystem-add` §2:

```
internal/codexwire/
  doc.go              # package doc; cites plan + FIRST-CHECKPOINT gate
  envelope.go         # trusted JSON-RPC 2.0 envelope (strict)
  payload.go          # untrusted payload structs, each with Extra map
  registry.go         # the ONE method-registry table
  roundtrip.go        # parse → re-serialize helpers (harness support)
  codexwire_test.go   # round-trip conformance harness = the checkpoint gate
```

### Trusted-envelope vs untrusted-payload split

`PHASE-1-tap-serializer-reactor.md` §2 / `07-tasks.md` T2: the **envelope** is JSON-RPC 2.0 framing —
strict, we own it, we trust it. The **payload** is Codex method params/results — untrusted, may carry
fields we have not modeled; every payload struct preserves-and-counts unknown fields via
`Extra map[string]json.RawMessage`. JSON-RPC 2.0 field set from `C1-protocol/findings.md` §1-2
(`jsonrpc`/`id`/`method`/`params`/`result`/`error`).

```go
package codexwire

// Envelope is the TRUSTED JSON-RPC 2.0 frame. Strict: unknown top-level keys are
// a hard decode error (we own this layer). Exactly one of {Method}, {Result|Error}
// distinguishes request/notification vs response — validated, not shrugged.
// Wire surface: C1-protocol/findings.md §1 (JSON-RPC 2.0), §2 (method set).
type Envelope struct {
    JSONRPC string          `json:"jsonrpc"`          // MUST == "2.0" (strict)
    ID      *json.RawMessage `json:"id,omitempty"`    // request/response id; absent = notification
    Method  string          `json:"method,omitempty"` // present = request or notification
    Params  json.RawMessage `json:"params,omitempty"` // untrusted payload; routed via registry
    Result  json.RawMessage `json:"result,omitempty"` // untrusted payload (response)
    Error   *RPCError       `json:"error,omitempty"`   // JSON-RPC error object
}

// RPCError is the strict JSON-RPC 2.0 error object (code/message/data).
// -32001 "Server overloaded; retry later" is a known code (findings.md §4).
type RPCError struct {
    Code    int             `json:"code"`
    Message string          `json:"message"`
    Data    json.RawMessage `json:"data,omitempty"`
}

// Payload is implemented by every modeled method's params/result struct. The Extra
// map is the preserve-and-count contract: any wire field not matched by an explicit
// struct field lands here, so the round-trip harness can COUNT unmodeled fields and
// re-serialize them losslessly. Optional-by-default (pointer/omitempty) per plan §2.
type Payload interface {
    UnknownFields() map[string]json.RawMessage // returns the Extra map for counting
}

// Example modeled payload — thread/started notification (findings.md §2).
// Every field optional-by-default; tighten to required only when the corpus proves
// the field is ALWAYS present (plan §2 "optional-by-default; tighten... when the
// corpus proves a field always present").
type ThreadStartedParams struct {
    Thread *ThreadRef `json:"thread,omitempty"`
    Extra  map[string]json.RawMessage `json:"-"` // populated by a custom UnmarshalJSON
}
func (p *ThreadStartedParams) UnknownFields() map[string]json.RawMessage { return p.Extra }

type ThreadRef struct {
    ID        *string `json:"id,omitempty"`        // server-minted; caller cannot mint (findings.md §3)
    SessionID *string `json:"sessionId,omitempty"`
    Extra     map[string]json.RawMessage `json:"-"`
}
```

The preserve-and-count mechanism is a per-struct custom `UnmarshalJSON` that decodes known fields, then
funnels the remaining keys into `Extra` (the standard "decode into map, delete known keys" idiom).
Re-serialize merges explicit fields + `Extra` so an unmodeled field survives the round trip verbatim.

### The ONE method-registry table

`PHASE-1-tap-serializer-reactor.md` §2: method strings live in ONE registry table — drift = a one-line
edit; unknown method → typed-raw, not a crash. Model only methods present in the corpus. Sketch:

```go
package codexwire

// methodRegistry maps a JSON-RPC method string to a constructor for its params
// payload. This is the SINGLE source of method truth: adding/renaming a method is
// a one-line edit here. An unknown method resolves to RawPayload (typed-raw), NOT
// a panic/error — a real message we haven't modeled is captured, never dropped.
var methodRegistry = map[string]func() Payload{
    // Handshake + thread lifecycle — the minimum happy-turn set (findings.md §2).
    "initialize":       func() Payload { return &InitializeParams{} },
    "thread/start":     func() Payload { return &ThreadStartParams{} },
    "thread/started":   func() Payload { return &ThreadStartedParams{} },   // notification
    "turn/start":       func() Payload { return &TurnStartParams{} },
    "turn/started":     func() Payload { return &TurnStartedParams{} },
    "item/started":     func() Payload { return &ItemParams{} },
    "item/completed":   func() Payload { return &ItemParams{} },
    "turn/completed":   func() Payload { return &TurnCompletedParams{} },
    // ... only methods the captured corpus actually contains get an entry.
}

// RawPayload is the typed-raw fallback for an unmodeled method. It preserves the
// whole params blob in Extra so an unknown method round-trips losslessly and the
// conformance harness can flag it (zero-unknown-methods is a RED test, per gate).
type RawPayload struct{ Raw json.RawMessage }
func (p *RawPayload) UnknownFields() map[string]json.RawMessage { return nil }

// payloadFor returns the modeled payload for method, or (RawPayload, false) when
// the method is not in the registry. `ok=false` is what the gate asserts on.
func payloadFor(method string) (Payload, bool) {
    if ctor, ok := methodRegistry[method]; ok { return ctor(), true }
    return &RawPayload{}, false
}
```

### Optional-by-default field policy

Every modeled payload field is a pointer + `omitempty` (optional) at first. A field is promoted to
required (non-pointer, presence-asserted) ONLY when the captured corpus proves it is always present
(`PHASE-1-tap-serializer-reactor.md` §2). This keeps the serializer permissive against the real wire
until the corpus earns each tightening.

### Round-trip conformance harness — the operator's FIRST CHECKPOINT gate

`PHASE-1-tap-serializer-reactor.md` §2 Gate / `07-tasks.md` T2 Gate: every captured real message
round-trips (parse → re-serialize → semantic-equal); **zero unmodeled fields** (or explicitly waived
with reason); **zero unknown methods**. A real message our types can't handle is a RED test, not a
runtime shrug. This is L0 (never live) — it runs against the T1 capture corpus.

```go
// internal/codexwire/codexwire_test.go
package codexwire_test

// FIRST CHECKPOINT gate (L0, CODEX_LIVE=0). Feeds every captured frame from the
// T1 corpus through parse → re-serialize → semantic-equal, counting unmodeled
// fields and asserting zero unknown methods.
func TestCorpusRoundTripConformance(t *testing.T) {
    t.Parallel()
    frames := loadCapturedFrames(t, "testdata/corpus")  // raw.jsonl frames from T1 tap

    var unmodeledFields, unknownMethods int
    for _, raw := range frames {
        env := codexwire.MustParse(t, raw)               // strict envelope decode

        // (a) unknown-method count — registry miss is the gate's hard failure.
        if _, ok := codexwire.PayloadForTest(env.Method); !ok && env.Method != "" {
            unknownMethods++
            t.Errorf("unknown method not in registry: %q", env.Method)
        }

        // (b) semantic round-trip: re-serialize and compare as normalized JSON
        //     (key-order-insensitive), NOT byte-equal.
        out := codexwire.Reserialize(t, env)
        testhelpers.AssertJSONSemanticEqual(t, raw, out)

        // (c) preserve-and-count: any field that fell into Extra is unmodeled.
        unmodeledFields += codexwire.CountUnknownFields(env)
    }

    // The operator's checkpoint asserts BOTH counters are zero (waivers listed
    // explicitly with a reason, per plan §2).
    testhelpers.AssertEqual(t, 0, unknownMethods,  "zero unknown methods")
    testhelpers.AssertEqual(t, 0, unmodeledFields, "zero unmodeled fields (or waived w/ reason)")
}
```

### depguard matrix entry needed

Add `rules.codexwire` — leaf: stdlib + self-import, deny `internal/daemon` (same shape as `codextap`).
The global `llm-sdk-ban` (denies `github.com/openai/`) already applies and is the correct posture: the
serializer models the wire with hand-written Go types, it does NOT import an OpenAI SDK.

```yaml
        codexwire:
          files: ["**/internal/codexwire/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/codexwire"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "codexwire MUST NOT import daemon (leaf wire-contract primitive)" }
```

### testhelpers hookup

- L0 conformance test in external package `codexwire_test`, `t.Parallel()` on every func.
- Reuse `AssertEqual`/assert helpers from `internal/testhelpers/assert.go`. If an
  `AssertJSONSemanticEqual` helper does not yet exist, add it to `internal/testhelpers` (or a
  local `_test.go` helper with the package-concept prefix `codexwireFixture…` per
  `go-subsystem-add` §4 / helper-prefix discipline).
- Corpus fixtures come from T1's `raw.jsonl` output, checked into `internal/codexwire/testdata/corpus/`.

---

## Open decisions for the implementer (genuinely undecidable without the real captured corpus)

1. **Which methods to model FIRST.** The plan mandates "model only methods present in the corpus."
   The registry sketch above lists the happy-turn set (`initialize`, `thread/start`/`started`,
   `turn/start`/`started`, `item/*`, `turn/completed`) as the *likely* first set — but the actual
   first tranche is whatever T0/T1 capture contains. Do not pre-model `thread/fork`, `thread/compact/start`,
   `mcpServer/*`, etc. until a captured frame demands it.
2. **Which fields are required vs optional.** Everything starts optional; the corpus decides each
   promotion. Undecidable until frames exist.
3. **Exact JSON-RPC `id` type.** Modeled as `*json.RawMessage` (JSON-RPC allows string OR number).
   Whether Codex uses strings, numbers, or both is a corpus question — the RawMessage choice is
   deliberately un-committal until the corpus shows it.
4. **`turn/completed` token-usage / `thread/tokenUsage/updated` shape** — flagged in findings.md §4 as
   server-side accounting; the concrete field names were summarized from a web fetch (findings.md §Open
   questions #4: "re-verify against raw README before treated as normative"). Model from captured bytes,
   not from the findings prose.
5. **Capture session-id source.** `raw.jsonl` lives under `.harmonik/codex-captures/<session-id>/` —
   whether `<session-id>` is the codex thread_id (server-minted, only available AFTER `thread/started`)
   or a tap-local capture id minted at splice time. Recommend a tap-local id (available immediately;
   correlate to thread_id in the index pass) — but confirm against the first real capture.
6. **Semantic-equal normalization rules.** Key-order-insensitive is settled; whether whitespace/number-
   formatting (e.g. `1.0` vs `1`) needs canonicalization depends on what codex actually emits.

## What stays OUT (deferred — do NOT build in T1/T2)

- **No persistent client, no supervised sidecar, no reconnect/backpressure/watchdog** — that is
  `hk-nzzos`, "the design's real cost," built LAST and sized to what the live suite shows actually
  breaks (`PHASE-1-tap-serializer-reactor.md` §Deferred; `07-tasks.md` §Explicitly DEFERRED). The tap
  is a passive splice; the serializer is pure types + a table. Neither owns a connection lifecycle.
- **No reactor / EventSource / Effector state machine** — that is T3 (hk-5co9a), downstream of T2.
- **No digital twin** — T4 (hk-swc8p). T1's `raw.jsonl` is merely its future input.
- **No client-path wiring** (crew-start routing, launch-spec, boot-seed, session-id, keeper-branch —
  hk-l63b9/lrf30/8efdl/0ysh3/6z72r) — PARKED on Phase-1 evidence.
- **No "can Codex orchestrate" bet** (Spike B) — its own throwaway spike; flag operator before Phase 2.
- The tap must **never parse a frame for meaning** and the serializer must **never import an OpenAI
  SDK** (both enforced by the global `llm-sdk-ban` depguard rule) — keeping both packages pure keeps
  the deferred client cost cleanly separable.

---

### Grounding citations (every choice → repo file)

- Flat package naming: `internal/` tree (`ls`), `internal/daemon/codexlaunchspec.go:35-37` imports.
- Scaffold (doc.go/`<pkg>.go`/`<pkg>_test.go`, external test pkg, `t.Parallel()`): `.claude/skills/go-subsystem-add/SKILL.md` §2, §5.
- depguard leaf-rule shape (allow stdlib+self, deny daemon): `.golangci.yml` `schedule`/`agentmanifest`/`crew`/`keeper` rules.
- Global SDK ban already covering both packages: `.golangci.yml` `llm-sdk-ban` (denies `github.com/openai/`).
- On-disk JSONL format (one object/line, trailing `\n`, snake_case, raw `[]byte` fixtures, `jsonlFixture` prefix): `internal/testhelpers/jsonlfixture.go`.
- Assert/tempdir/clock helpers: `internal/testhelpers/{assert,tempdir,clock}.go`.
- Codex integration idioms (buildCodexEnv, CODEX_HOME, argv-invocation, credential strip): `internal/daemon/codexlaunchspec.go`, `codexharness.go`.
- Wire surface (JSON-RPC 2.0, Thread/Turn/Item, method set, server-minted id, -32001): `03-research/C1-protocol/findings.md` §1-4.
- Layer shapes + gates + deferrals: `plans/2026-07-11-codex-app-server-replan/PHASE-1-tap-serializer-reactor.md` §1-2, §Deferred; `07-tasks.md` T1/T2.
