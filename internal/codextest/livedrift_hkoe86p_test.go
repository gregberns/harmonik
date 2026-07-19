package codextest_test

// Fresh-vs-frozen drift-diff gate (M6 WS3-codex-B).
//
// A codex "capture corpus" is a PINNED ORACLE: the frozen reference under
// testdata/codex-app-server/ is the source of truth for (a) the JSON-RPC method
// vocabulary the wire protocol is allowed to use and (b) the set of reactor
// cross-bus event-kinds the input-substrate reactor emits. When codex-A
// re-captures a fresh session (CODEX_LIVE=1), this gate diffs the fresh capture
// against the frozen reference and FAILS on drift:
//
//   - a NEW or RENAMED wire method — any method observed in the fresh capture
//     that is not present in codexwire.methodRegistry (the single source of
//     truth). The offending method is printed with a fix pointer.
//   - a reactor event-kind set that no longer matches the frozen reference —
//     any added or removed EmitType. The offending kinds are printed.
//
// HUMAN-GATE (decision, WS3-codex-B): the gate NEVER auto-writes the frozen
// corpus from a fresh capture. Promotion fresh→frozen is a deliberate human
// step (a corpus is a pinned oracle). Nothing in this file opens the frozen
// testdata for writing.
//
// The pure diff engine (driftDiffAgainstFrozen) is unit-tested below against
// SYNTHETIC captures — no live codex required. The live leg (TestCodexB_*Live)
// is default-SKIPPED unless CODEX_LIVE=1, and FATALs (never silently greens)
// when HARMONIK_REQUIRE_CODEX_DRIFT=1 is set but no fresh capture is available.
//
// Bead: hk-oe86p [codex-app-server / M6 WS3-codex-B]

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexinput"
	"github.com/gregberns/harmonik/internal/codexwire"
)

// ─── Drift-capture projection ────────────────────────────────────────────────

// driftCapture is the drift-relevant projection of a codex session capture:
// the set of JSON-RPC methods seen on the wire, and the set of reactor
// cross-bus event-kinds (EmitType strings) the input reactor produces for it.
// Sets are represented as string→struct{} for O(1) membership + easy diffing.
type driftCapture struct {
	Label             string
	Methods           map[string]struct{}
	ReactorEventKinds map[string]struct{}
}

// driftResult is the verdict of the gate. OK==true means no drift; otherwise
// Failures holds one human-readable line per drift, each ending in a fix
// pointer. The gate reports ALL failures at once (not first-only) so a human
// promoting a fresh capture sees the full delta in a single run.
type driftResult struct {
	OK       bool
	Failures []string
}

// frozenReactorEventKinds is the canonical reactor cross-bus event-kind set,
// sourced directly from the codexinput EmitType constants so it can never drift
// from the reactor's own vocabulary silently — if a constant is added/removed
// there, this set moves with it and the frozen-reference test below is what
// forces a human to re-bless the oracle.
func frozenReactorEventKinds() map[string]struct{} {
	return stringSet(
		string(codexinput.EmitInputSubmitted),
		string(codexinput.EmitInputAcked),
		string(codexinput.EmitInputStale),
		string(codexinput.EmitLaunchFailure),
	)
}

// ─── The gate (pure) ─────────────────────────────────────────────────────────

// driftDiffAgainstFrozen is the pure drift gate. It PASSES (OK=true) when:
//   - every method in fresh.Methods is present in registeredMethods (i.e. in
//     codexwire.methodRegistry), AND
//   - fresh.ReactorEventKinds is exactly equal to frozen.ReactorEventKinds.
//
// It FAILS otherwise, with one Failures line per offending method / kind. The
// function has no side effects and never writes the frozen corpus.
func driftDiffAgainstFrozen(fresh, frozen driftCapture, registeredMethods []string) driftResult {
	res := driftResult{OK: true}
	registry := stringSet(registeredMethods...)

	// Check 1 — no method outside codexwire.methodRegistry (new/renamed method).
	for _, m := range sortedKeys(fresh.Methods) {
		if _, ok := registry[m]; !ok {
			res.OK = false
			res.Failures = append(res.Failures, fmt.Sprintf(
				"fresh capture %q introduced wire method %q which is NOT in codexwire.methodRegistry "+
					"(new or renamed method). FIX: if this method is real, add an entry to "+
					"methodRegistry in internal/codexwire/codexwire.go (plus its Params/Result types), "+
					"then re-run capture-vs-frozen and human-promote the fresh capture to the frozen "+
					"corpus. The gate NEVER auto-promotes.",
				fresh.Label, m))
		}
	}

	// Check 2 — reactor event-kind set matches the frozen reference (set equality).
	added, removed := setDiff(fresh.ReactorEventKinds, frozen.ReactorEventKinds)
	if len(added) > 0 {
		res.OK = false
		res.Failures = append(res.Failures, fmt.Sprintf(
			"fresh capture %q emitted reactor event-kind(s) %v absent from the frozen reference set %v. "+
				"FIX: if the reactor vocabulary legitimately grew, update codexinput EmitType constants "+
				"and human-promote the frozen reference; do NOT auto-promote.",
			fresh.Label, added, sortedKeys(frozen.ReactorEventKinds)))
	}
	if len(removed) > 0 {
		res.OK = false
		res.Failures = append(res.Failures, fmt.Sprintf(
			"fresh capture %q is MISSING reactor event-kind(s) %v present in the frozen reference set %v. "+
				"FIX: this usually means a regression dropped an emission path; investigate the reactor "+
				"before touching the oracle.",
			fresh.Label, removed, sortedKeys(frozen.ReactorEventKinds)))
	}

	return res
}

// ─── Capture loaders ─────────────────────────────────────────────────────────

// loadMethodsFromWireNDJSON extracts the set of JSON-RPC methods observed in a
// codex wire capture (one JSON-RPC frame per line). It uses codexwire.Parse so
// it sees exactly what the real integration sees — crucially, an UNKNOWN method
// still yields Frame.Method (with Kind==FrameKindRaw), which is precisely how a
// new/renamed method surfaces to the gate. Server responses (no method) and
// blank lines are ignored.
func loadMethodsFromWireNDJSON(data []byte) (map[string]struct{}, error) {
	methods := map[string]struct{}{}
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		frame, err := codexwire.Parse([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("wire line %d: %w", i+1, err)
		}
		if frame.Method != "" {
			methods[frame.Method] = struct{}{}
		}
	}
	return methods, nil
}

// ─── Small set helpers ───────────────────────────────────────────────────────

func stringSet(ss ...string) map[string]struct{} {
	s := make(map[string]struct{}, len(ss))
	for _, v := range ss {
		s[v] = struct{}{}
	}
	return s
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// setDiff returns the keys in a not in b (added) and the keys in b not in a
// (removed), each sorted for deterministic messages.
func setDiff(a, b map[string]struct{}) (added, removed []string) {
	for k := range a {
		if _, ok := b[k]; !ok {
			added = append(added, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			removed = append(removed, k)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

// ─── Unit tests — the pure gate (no live codex) ──────────────────────────────

// frozenReference builds the frozen-oracle projection the unit tests diff
// against: the full registered method vocabulary + the canonical reactor kinds.
func frozenReference() driftCapture {
	return driftCapture{
		Label:             "frozen",
		Methods:           stringSet(codexwire.RegisteredMethods()...),
		ReactorEventKinds: frozenReactorEventKinds(),
	}
}

// TestCodexB_DriftGate_PassesOnCleanCapture proves the gate PASSES when a fresh
// capture is identical to the frozen reference (methods ⊆ registry, kinds ==).
func TestCodexB_DriftGate_PassesOnCleanCapture(t *testing.T) {
	t.Parallel()
	frozen := frozenReference()

	// A clean fresh capture uses only registered methods and the exact frozen
	// reactor kinds. Use a realistic happy-path subset of the wire vocabulary.
	fresh := driftCapture{
		Label: "fresh-clean",
		Methods: stringSet(
			"initialize", "initialized", "thread/start", "thread/started",
			"turn/start", "turn/started", "turn/completed",
		),
		ReactorEventKinds: frozenReactorEventKinds(),
	}

	res := driftDiffAgainstFrozen(fresh, frozen, codexwire.RegisteredMethods())
	if !res.OK {
		t.Fatalf("expected clean fresh capture to PASS, got failures:\n%s",
			strings.Join(res.Failures, "\n"))
	}
}

// TestCodexB_DriftGate_FailsOnNewMethod proves the gate BITES on a new/renamed
// method: it must FAIL and the message must name the offending method and carry
// a fix pointer (the methodRegistry location).
func TestCodexB_DriftGate_FailsOnNewMethod(t *testing.T) {
	t.Parallel()
	frozen := frozenReference()

	const offender = "turn/begin" // a rename of turn/start — not in the registry
	fresh := driftCapture{
		Label: "fresh-renamed",
		Methods: stringSet(
			"initialize", "initialized", "thread/start", offender,
		),
		ReactorEventKinds: frozenReactorEventKinds(),
	}

	res := driftDiffAgainstFrozen(fresh, frozen, codexwire.RegisteredMethods())
	if res.OK {
		t.Fatal("expected fresh capture with a renamed method to FAIL, but gate passed")
	}
	joined := strings.Join(res.Failures, "\n")
	if !strings.Contains(joined, offender) {
		t.Fatalf("failure message must NAME the offending method %q; got:\n%s", offender, joined)
	}
	if !strings.Contains(joined, "methodRegistry") {
		t.Fatalf("failure message must carry a fix pointer to methodRegistry; got:\n%s", joined)
	}
}

// TestCodexB_DriftGate_FailsOnReactorKindDrift proves the gate BITES when the
// reactor event-kind set no longer matches the frozen reference — both an added
// kind and a removed kind must be reported.
func TestCodexB_DriftGate_FailsOnReactorKindDrift(t *testing.T) {
	t.Parallel()
	frozen := frozenReference()

	// Added an unknown kind, dropped agent_input_stale.
	fresh := driftCapture{
		Label:   "fresh-kinddrift",
		Methods: stringSet("initialize"),
		ReactorEventKinds: stringSet(
			string(codexinput.EmitInputSubmitted),
			string(codexinput.EmitInputAcked),
			string(codexinput.EmitLaunchFailure),
			"agent_input_teleported", // bogus new kind
		),
	}

	res := driftDiffAgainstFrozen(fresh, frozen, codexwire.RegisteredMethods())
	if res.OK {
		t.Fatal("expected reactor-kind drift to FAIL, but gate passed")
	}
	joined := strings.Join(res.Failures, "\n")
	if !strings.Contains(joined, "agent_input_teleported") {
		t.Fatalf("must report the ADDED kind; got:\n%s", joined)
	}
	if !strings.Contains(joined, string(codexinput.EmitInputStale)) {
		t.Fatalf("must report the MISSING kind %q; got:\n%s", codexinput.EmitInputStale, joined)
	}
}

// TestCodexB_LoadMethodsFromFrozenCorpus exercises the wire loader against the
// real frozen corpus so the method-extraction path is covered without a live
// codex. Every method the frozen corpus contains MUST already be registered —
// if this fails, the checked-in corpus itself has drifted from methodRegistry.
func TestCodexB_LoadMethodsFromFrozenCorpus(t *testing.T) {
	t.Parallel()
	const corpus = "../../testdata/codex-app-server/corpus/raw-session-01.jsonl"
	data, err := os.ReadFile(corpus)
	if err != nil {
		t.Fatalf("read frozen corpus %s: %v", corpus, err)
	}
	methods, err := loadMethodsFromWireNDJSON(data)
	if err != nil {
		t.Fatalf("load methods from frozen corpus: %v", err)
	}
	if len(methods) == 0 {
		t.Fatal("frozen corpus yielded zero methods — loader or fixture is broken")
	}

	fresh := driftCapture{Label: corpus, Methods: methods, ReactorEventKinds: frozenReactorEventKinds()}
	res := driftDiffAgainstFrozen(fresh, frozenReference(), codexwire.RegisteredMethods())
	if !res.OK {
		t.Fatalf("frozen corpus drifted from methodRegistry — corpus needs re-blessing:\n%s",
			strings.Join(res.Failures, "\n"))
	}
}

// ─── Live leg — fresh re-capture vs frozen (default-skipped) ──────────────────

// codexDriftRequireOrSkip mirrors the WS1.3 rsb12RequireSSHOrSkip anti-false-
// green pattern. By default (no fresh capture available, no REQUIRE flag) it
// SKIPs so the suite is clean on boxes without live codex. With
// HARMONIK_REQUIRE_CODEX_DRIFT=1 it FATALs instead — so an environment that
// INTENDS to run the live drift gate fails loudly rather than silently greening.
func codexDriftRequireOrSkip(t *testing.T, msg string) {
	t.Helper()
	if os.Getenv("HARMONIK_REQUIRE_CODEX_DRIFT") == "1" {
		t.Fatalf("%s (HARMONIK_REQUIRE_CODEX_DRIFT=1)", msg)
	}
	t.Skipf("%s", msg)
}

// TestCodexB_FreshVsFrozenLive is the live leg: it loads a FRESH codex capture
// produced by codex-A (CODEX_LIVE=1 `make recapture-codex-corpus`, path given by
// CODEX_FRESH_CAPTURE) and runs the drift gate against the frozen reference.
//
// It is default-SKIPPED: without CODEX_LIVE=1 there is no fresh capture to
// diff. When CODEX_LIVE=1 but the fresh-capture path is unset/unreadable, it
// defers to codexDriftRequireOrSkip so HARMONIK_REQUIRE_CODEX_DRIFT=1 cannot be
// silently greened.
//
// HUMAN-GATE: on drift this test FAILS; it does not touch the frozen corpus.
// Promotion fresh→frozen stays a deliberate human step.
func TestCodexB_FreshVsFrozenLive(t *testing.T) {
	if os.Getenv("CODEX_LIVE") != "1" {
		t.Skip("CODEX_LIVE=1 required for the live fresh-vs-frozen drift gate (default-skipped)")
	}
	path := os.Getenv("CODEX_FRESH_CAPTURE")
	if path == "" {
		codexDriftRequireOrSkip(t,
			"live drift gate needs a fresh capture: set CODEX_FRESH_CAPTURE to the "+
				"wire.ndjson produced by codex-A (`make recapture-codex-corpus`)")
		return
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: CODEX_FRESH_CAPTURE is an operator-set test env var, not user input
	if err != nil {
		codexDriftRequireOrSkip(t, fmt.Sprintf("read fresh capture %s: %v", path, err))
		return
	}
	methods, err := loadMethodsFromWireNDJSON(data)
	if err != nil {
		t.Fatalf("parse fresh capture %s: %v", path, err)
	}
	if len(methods) == 0 {
		t.Fatalf("fresh capture %s yielded zero methods — capture is empty or malformed", path)
	}

	fresh := driftCapture{Label: path, Methods: methods, ReactorEventKinds: frozenReactorEventKinds()}
	res := driftDiffAgainstFrozen(fresh, frozenReference(), codexwire.RegisteredMethods())
	if !res.OK {
		t.Fatalf("FRESH CAPTURE DRIFTED from the frozen oracle — do NOT auto-promote; a human must "+
			"review each item before re-blessing the corpus:\n%s", strings.Join(res.Failures, "\n"))
	}
	t.Logf("live drift gate GREEN: fresh capture %s matches the frozen oracle", path)
}
