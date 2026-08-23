package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue/readiness"
)

var readinessNow = time.Date(2026, 8, 4, 18, 30, 0, 0, time.UTC)

type fakeLedger struct {
	beads map[core.BeadID]core.BeadRecord
	asked []string
}

func (f *fakeLedger) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	f.asked = append(f.asked, string(id))
	rec, ok := f.beads[id]
	if !ok {
		return core.BeadRecord{}, errors.New("ISSUE_NOT_FOUND")
	}
	return rec, nil
}

func openLedger() *fakeLedger {
	return &fakeLedger{beads: map[core.BeadID]core.BeadRecord{
		"hk-one":   {BeadID: "hk-one", Title: "correct a comment", Status: core.CoarseStatusOpen},
		"hk-two":   {BeadID: "hk-two", Title: "correct a second comment", Status: core.CoarseStatusOpen},
		"hk-three": {BeadID: "hk-three", Title: "correct a third comment", Status: core.CoarseStatusOpen},
		"hk-shut":  {BeadID: "hk-shut", Title: "already done", Status: core.CoarseStatusClosed},
	}}
}

func projectWithEventLog(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	events := filepath.Join(dir, ".harmonik", "events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatalf("mkdir events: %v", err)
	}
	if err := os.WriteFile(filepath.Join(events, "events.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write event log: %v", err)
	}
	return dir
}

func TestParseCaptureArgs_RefusesEveryInputItWouldOtherwiseHaveToGuess(t *testing.T) {
	whole := []string{
		"--select", "hk-one=a single-file comment edit",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=true",
		"--out", "/tmp/readiness.json",
	}

	if _, err := parseCaptureArgs(whole, io.Discard); err != nil {
		t.Fatalf("the whole argument list did not parse: %v", err)
	}

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"no selected item":  {[]string{"--item-count", "1", "--concurrency", "1", "--local=true", "--out", "/tmp/r.json"}, "--select"},
		"no item count":     {[]string{"--select", "hk-one=r", "--concurrency", "1", "--local=true", "--out", "/tmp/r.json"}, "--item-count"},
		"no concurrency":    {[]string{"--select", "hk-one=r", "--item-count", "1", "--local=true", "--out", "/tmp/r.json"}, "--concurrency"},
		"unstated locality": {[]string{"--select", "hk-one=r", "--item-count", "1", "--concurrency", "1", "--out", "/tmp/r.json"}, "--local"},
		"nowhere to write":  {[]string{"--select", "hk-one=r", "--item-count", "1", "--concurrency", "1", "--local=true"}, "--out"},
	} {
		_, err := parseCaptureArgs(tc.args, io.Discard)
		if err == nil {
			t.Errorf("%s: parsed, want a refusal", name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to name %s", name, err, tc.want)
		}
	}
}

// The operator's requirement, at the flag surface: several items, each with its
// own reason, in the order given.
func TestParseCaptureArgs_TakesSeveralItemsEachWithItsOwnReason(t *testing.T) {
	cfg, err := parseCaptureArgs([]string{
		"--select", "hk-one=one file, nothing tests it",
		"--select", "hk-two=adds a doc line",
		"--select", "hk-three=re-running reaches the same tree",
		"--exclude", "hk-shut=already closed",
		"--item-count", "3",
		"--concurrency", "3",
		"--local=true",
		"--out", "/tmp/readiness.json",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseCaptureArgs: %v", err)
	}

	if len(cfg.Selected) != 3 {
		t.Fatalf("selected = %d, want 3", len(cfg.Selected))
	}
	want := []core.BeadID{"hk-one", "hk-two", "hk-three"}
	for i, id := range want {
		if cfg.Selected[i].BeadID != id {
			t.Errorf("selected %d = %s, want %s; selection order is the order given", i, cfg.Selected[i].BeadID, id)
		}
		if cfg.Selected[i].RepeatSafeReason == "" {
			t.Errorf("%s carries no reason", id)
		}
	}
	if cfg.Selected[0].RepeatSafeReason == cfg.Selected[1].RepeatSafeReason {
		t.Error("two items share a reason; each --select carries its own")
	}
	if cfg.Posture != (readiness.Posture{Local: true, ItemCount: 3, Concurrency: 3}) {
		t.Errorf("posture = %+v", cfg.Posture)
	}
	if len(cfg.Excluded) != 1 || cfg.Excluded[0].BeadID != "hk-shut" {
		t.Errorf("excluded = %+v", cfg.Excluded)
	}
}

// A reason may contain the separator. Splitting on the last one, or on all of
// them, would truncate exactly the reasons that explain the most.
func TestSplitPair_SplitsOnTheFirstSeparatorAndRefusesEitherHalfMissing(t *testing.T) {
	bead, reason, err := splitPair("hk-one=idempotent: rerun=same tree", "select")
	if err != nil {
		t.Fatalf("splitPair: %v", err)
	}
	if bead != "hk-one" || reason != "idempotent: rerun=same tree" {
		t.Errorf("split = %q, %q", bead, reason)
	}

	for name, raw := range map[string]string{
		"no separator": "hk-one",
		"no bead":      "=a reason with no bead",
		"no reason":    "hk-one=",
	} {
		if _, _, err := splitPair(raw, "select"); err == nil {
			t.Errorf("%s: %q parsed, want a refusal", name, raw)
		}
	}
}

func TestRunQueueReadinessCapture_WritesARecordThatNamesEverySelectedItem(t *testing.T) {
	project := projectWithEventLog(t)
	out := filepath.Join(t.TempDir(), "evidence", "readiness.json")
	ledger := openLedger()

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", project,
		"--select", "hk-one=one file, nothing tests it",
		"--select", "hk-two=adds a doc line",
		"--exclude", "hk-shut=already closed",
		"--item-count", "2",
		"--concurrency", "2",
		"--local=true",
		"--out", out,
	}, readinessNow, func(string) (readiness.BeadReader, error) { return ledger, nil }, &stdout, &stderr)

	if code != readinessExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if strings.Join(ledger.asked, ",") != "hk-one,hk-two,hk-shut" {
		t.Errorf("ledger reads = %v, want the selected items then the excluded one", ledger.asked)
	}

	body, err := os.ReadFile(out) //nolint:gosec // G304: this test's own temp dir
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	var snap readiness.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		t.Fatalf("decode the record: %v", err)
	}
	if len(snap.Selected) != 2 {
		t.Fatalf("record names %d selected items, want 2", len(snap.Selected))
	}
	if snap.Posture.ItemCount != 2 || snap.Posture.Concurrency != 2 {
		t.Errorf("posture = %+v, want two items at concurrency two", snap.Posture)
	}
	if len(snap.Commands) != 3 {
		t.Errorf("commands = %+v, want one ledger read per candidate", snap.Commands)
	}
	if len(snap.Events.LogPaths) != 1 {
		t.Errorf("event logs = %v, want the project's own log discovered", snap.Events.LogPaths)
	}
	if !strings.Contains(stdout.String(), "2 items, 2 at the same time") {
		t.Errorf("stdout does not state the run shape: %s", stdout.String())
	}
}

// A stated item count that does not match the items named is the mistake the
// widened record exists to catch, and the command must not write a file for it.
func TestRunQueueReadinessCapture_RefusesAStatedCountThatDoesNotMatchTheItemsNamed(t *testing.T) {
	project := projectWithEventLog(t)
	out := filepath.Join(t.TempDir(), "readiness.json")

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", project,
		"--select", "hk-one=one file, nothing tests it",
		"--item-count", "3",
		"--concurrency", "3",
		"--local=true",
		"--out", out,
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code %d", code, readinessExitRejected)
	}
	if !strings.Contains(stderr.String(), "3 items, 1 are named") {
		t.Errorf("stderr does not say which two numbers disagree: %s", stderr.String())
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused capture still wrote a record file")
	}
}

func TestRunQueueReadinessCapture_RefusesAClosedItemUsingTheLiveStatus(t *testing.T) {
	project := projectWithEventLog(t)
	var stdout, stderr bytes.Buffer

	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", project,
		"--select", "hk-shut=the caller claims this is fine",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=true",
		"--out", filepath.Join(t.TempDir(), "readiness.json"),
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code", code)
	}
	if !strings.Contains(stderr.String(), "not open") {
		t.Errorf("stderr = %s, want the not-open refusal", stderr.String())
	}
}

// A capture that found no event log has no event evidence, and BI-013e requires
// some. Refusing here says which directory was searched.
func TestRunQueueReadinessCapture_RefusesWhenItFindsNoEventLog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", t.TempDir(),
		"--select", "hk-one=one file",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=true",
		"--out", filepath.Join(t.TempDir(), "readiness.json"),
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitUsage {
		t.Fatalf("exit = %d, want the usage code", code)
	}
	if !strings.Contains(stderr.String(), "--event-log") {
		t.Errorf("stderr = %s, want it to name the flag that fixes this", stderr.String())
	}
}

// The log rotates. A capture that spans a rotation reads more than one file, and
// a record that could name only the live one would drop the other in silence.
func TestDiscoverEventLogs_FindsTheLiveLogAndItsRotationsInAStableOrder(t *testing.T) {
	harmonikDir := filepath.Join(t.TempDir(), ".harmonik")
	events := filepath.Join(harmonikDir, "events")
	if err := os.MkdirAll(events, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"events.jsonl", "events.jsonl.2", "events.jsonl.1", "other.txt"} {
		if err := os.WriteFile(filepath.Join(events, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	got := discoverEventLogs(harmonikDir)
	if len(got) != 3 {
		t.Fatalf("discovered %v, want the live log and its two rotations and not the .txt", got)
	}
	for i, want := range []string{"events.jsonl", "events.jsonl.1", "events.jsonl.2"} {
		if filepath.Base(got[i]) != want {
			t.Errorf("log %d = %s, want %s; the order is stable across captures", i, filepath.Base(got[i]), want)
		}
	}
}

// Every host fact is required. A missing daemon count is the dangerous one: it
// would read as zero daemons alive, which CLEARS the ceiling. That is a gate
// failing open, which is the one direction it must never fail.
func TestParseValidateArgs_RefusesAnUnmeasuredHostFactRatherThanReadingItAsZero(t *testing.T) {
	whole := []string{
		"--snapshot", "/tmp/readiness.json", "--out", "/tmp/validation.json",
		"--harness", "claude", "--repo-target", "/tmp/s", "--scratch-repo", "/tmp/s",
		"--queue-kind", "stream", "--item-count", "2", "--concurrency", "2",
		"--load-average", "4.5", "--cpu-count", "10", "--free-disk-gb", "120", "--daemons-alive", "1",
	}
	if _, err := parseValidateArgs(whole, io.Discard); err != nil {
		t.Fatalf("the whole argument list did not parse: %v", err)
	}

	for _, flagName := range []string{
		"--snapshot", "--out", "--harness", "--repo-target", "--scratch-repo", "--queue-kind",
		"--load-average", "--cpu-count", "--free-disk-gb", "--daemons-alive",
	} {
		args := dropFlag(whole, flagName)
		_, err := parseValidateArgs(args, io.Discard)
		if err == nil {
			t.Errorf("%s missing: parsed, want a refusal", flagName)
			continue
		}
		if !strings.Contains(err.Error(), strings.TrimPrefix(flagName, "--")) {
			t.Errorf("%s missing: err = %v, want it to name the flag", flagName, err)
		}
	}
}

func dropFlag(args []string, name string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			i++ // skip the value too
			continue
		}
		out = append(out, args[i])
	}
	return out
}

func TestRunQueueReadinessValidate_AcceptsAManyItemRunAndSaysSoInItsExitCode(t *testing.T) {
	snapshotPath := captureForTest(t, 3)
	outPath := filepath.Join(t.TempDir(), "validation.json")

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessValidate([]string{
		"--snapshot", snapshotPath, "--out", outPath,
		"--harness", "claude", "--repo-target", "/tmp/s", "--scratch-repo", "/tmp/s",
		"--queue-kind", "stream", "--item-count", "3", "--concurrency", "3",
		"--load-average", "4.5", "--cpu-count", "10", "--free-disk-gb", "120", "--daemons-alive", "1",
	}, readinessNow, &stdout, &stderr)

	if code != readinessExitOK {
		t.Fatalf("exit = %d, stderr = %s, stdout = %s", code, stderr.String(), stdout.String())
	}
	verdict := readVerdict(t, outPath)
	if !verdict.Accepted {
		t.Fatalf("verdict refused a three-item run: %+v", verdict.Rejections)
	}
	if len(verdict.SelectedBeads) != 3 {
		t.Errorf("verdict names %q, want all three items", verdict.SelectedBeads)
	}
	if verdict.Plan.Concurrency != 3 || verdict.Plan.ItemCount != 3 {
		t.Errorf("verdict records items=%d concurrency=%d, want the shape it judged", verdict.Plan.ItemCount, verdict.Plan.Concurrency)
	}
}

// A gate whose refusal appears only in a file is a gate a script walks past.
func TestRunQueueReadinessValidate_ReturnsANonZeroCodeAndStillWritesTheVerdict(t *testing.T) {
	snapshotPath := captureForTest(t, 1)
	outPath := filepath.Join(t.TempDir(), "validation.json")

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessValidate([]string{
		"--snapshot", snapshotPath, "--out", outPath,
		"--harness", "pi", // refused: reaches a remote endpoint
		"--repo-target", "/tmp/s", "--scratch-repo", "/tmp/s",
		"--queue-kind", "stream", "--item-count", "1", "--concurrency", "1",
		"--load-average", "4.5", "--cpu-count", "10", "--free-disk-gb", "120", "--daemons-alive", "1",
	}, readinessNow, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code %d", code, readinessExitRejected)
	}
	verdict := readVerdict(t, outPath)
	if verdict.Accepted {
		t.Error("a refused run wrote an accepted verdict")
	}
	if len(verdict.Rejections) == 0 {
		t.Error("the verdict names no reason it refused")
	}
	if !strings.Contains(stdout.String(), "refused") {
		t.Errorf("stdout does not say it refused: %s", stdout.String())
	}
}

// The validator judges the record on disk, not the plan's own claims about it.
func TestRunQueueReadinessValidate_RefusesAPlanThatContradictsTheRecordItWasGiven(t *testing.T) {
	snapshotPath := captureForTest(t, 1)
	outPath := filepath.Join(t.TempDir(), "validation.json")

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessValidate([]string{
		"--snapshot", snapshotPath, "--out", outPath,
		"--harness", "claude", "--repo-target", "/tmp/s", "--scratch-repo", "/tmp/s",
		"--queue-kind", "stream",
		"--item-count", "5", "--concurrency", "5", // the record judged one item
		"--load-average", "4.5", "--cpu-count", "10", "--free-disk-gb", "120", "--daemons-alive", "1",
	}, readinessNow, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code", code)
	}
	verdict := readVerdict(t, outPath)
	if !hasRejection(verdict, readiness.RejectPostureMismatch) {
		t.Errorf("rejections = %+v, want the posture mismatch", verdict.Rejections)
	}
}

func TestRunQueueReadinessValidate_RefusesARecordItCannotRead(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "readiness.json")
	if err := os.WriteFile(bad, []byte(`{"schema_version": 99}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessValidate([]string{
		"--snapshot", bad, "--out", filepath.Join(dir, "validation.json"),
		"--harness", "claude", "--repo-target", "/tmp/s", "--scratch-repo", "/tmp/s",
		"--queue-kind", "stream", "--item-count", "1", "--concurrency", "1",
		"--load-average", "4.5", "--cpu-count", "10", "--free-disk-gb", "120", "--daemons-alive", "1",
	}, readinessNow, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code", code)
	}
	if !strings.Contains(stderr.String(), "schema version") {
		t.Errorf("stderr = %s, want it to name the unreadable version", stderr.String())
	}
}

func TestRunQueueReadiness_NamesItsTwoVerbsAndRefusesAnythingElse(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runQueueReadiness(context.Background(), []string{"validat"}, &stdout, &stderr); code != readinessExitUsage {
		t.Errorf("exit = %d on a misspelled verb, want the usage code", code)
	}
	if !strings.Contains(stderr.String(), "capture, validate") {
		t.Errorf("stderr = %s, want it to name both verbs", stderr.String())
	}

	stdout.Reset()
	if code := runQueueReadiness(context.Background(), []string{"--help"}, &stdout, &stderr); code != readinessExitOK {
		t.Errorf("--help exit = %d, want 0", code)
	}
	for _, want := range []string{"capture", "validate"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help does not name %q: %s", want, stdout.String())
		}
	}
}

func captureForTest(t *testing.T, n int) string {
	t.Helper()
	project := projectWithEventLog(t)
	out := filepath.Join(t.TempDir(), "readiness.json")

	args := []string{"--project", project}
	for _, sel := range []string{
		"hk-one=one file, nothing tests it",
		"hk-two=adds a doc line",
		"hk-three=re-running reaches the same tree",
	}[:n] {
		args = append(args, "--select", sel)
	}
	args = append(args,
		"--item-count", strconv.Itoa(n),
		"--concurrency", strconv.Itoa(n),
		"--local=true",
		"--out", out,
	)

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), args, readinessNow,
		func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)
	if code != readinessExitOK {
		t.Fatalf("capture for the fixture failed: %s", stderr.String())
	}
	return out
}

func readVerdict(t *testing.T, path string) readiness.Validation {
	t.Helper()
	body, err := os.ReadFile(path) //nolint:gosec // G304: this test's own temp dir
	if err != nil {
		t.Fatalf("read the verdict: %v", err)
	}
	var v readiness.Validation
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("decode the verdict: %v", err)
	}
	return v
}

func hasRejection(v readiness.Validation, want readiness.RejectionReason) bool {
	for _, r := range v.Rejections {
		if r.Reason == want {
			return true
		}
	}
	return false
}

// The one run-shape rule the operator kept. It has to be reachable from the
// command, or "local" reduces to "nobody passed --remote-worker".
func TestRunQueueReadinessCapture_RefusesARunTheOperatorStatedIsNotLocal(t *testing.T) {
	out := filepath.Join(t.TempDir(), "readiness.json")

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", projectWithEventLog(t),
		"--select", "hk-one=one file, nothing tests it",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=false",
		"--out", out,
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitRejected {
		t.Fatalf("exit = %d, want the refusal code %d", code, readinessExitRejected)
	}
	if !strings.Contains(stderr.String(), "not a local run") {
		t.Errorf("stderr = %s, want the not-local refusal", stderr.String())
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused capture still wrote a record file")
	}
}

// The two finding lists are too many fields for a command line, so they arrive
// in a file. The evidence standard on them is the package's, and the file must
// carry them into the record intact.
func TestRunQueueReadinessCapture_CarriesBothFindingListsInFromAFile(t *testing.T) {
	dir := t.TempDir()
	findingsPath := filepath.Join(dir, "findings.json")
	findings := findingsFile{
		StaleFindings: []readiness.StaleFinding{{
			FindingID:     "hk-v4wer",
			CheckedSource: "internal/daemon/dot_cascade_helpers.go",
			FixingCommit:  "27c7a5fb8",
			FocusedProof:  "TestDotNode_FailureSignalAfterACommitFailsTheNode",
			Disposition:   "stale; current source classifies the outcome first",
		}},
		CurrentFindings: []readiness.CurrentFinding{{
			RecordID: "hk-still-open",
			Evidence: "the sweep globs the wrong archive layout",
			Source:   "internal/lifecycle/queuearchivesweep.go",
		}},
	}
	body, err := json.Marshal(findings)
	if err != nil {
		t.Fatalf("marshal findings: %v", err)
	}
	if err := os.WriteFile(findingsPath, body, 0o600); err != nil {
		t.Fatalf("write findings: %v", err)
	}

	out := filepath.Join(dir, "readiness.json")
	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", projectWithEventLog(t),
		"--select", "hk-one=one file, nothing tests it",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=true",
		"--findings", findingsPath,
		"--out", out,
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitOK {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	recordBody, err := os.ReadFile(out) //nolint:gosec // G304: this test's own temp dir
	if err != nil {
		t.Fatalf("read the record: %v", err)
	}
	var snap readiness.Snapshot
	if err := json.Unmarshal(recordBody, &snap); err != nil {
		t.Fatalf("decode the record: %v", err)
	}
	if len(snap.StaleFindings) != 1 || snap.StaleFindings[0].FixingCommit != "27c7a5fb8" {
		t.Errorf("stale findings = %+v, want the one from the file", snap.StaleFindings)
	}
	if len(snap.CurrentFindings) != 1 || snap.CurrentFindings[0].RecordID != "hk-still-open" {
		t.Errorf("current findings = %+v, want the one from the file", snap.CurrentFindings)
	}
}

// A findings file that is not a findings file must stop the capture, not land
// an empty pair of lists in a record that reads as "nothing was found".
func TestRunQueueReadinessCapture_RefusesAFindingsFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "findings.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := runQueueReadinessCapture(context.Background(), []string{
		"--project", projectWithEventLog(t),
		"--select", "hk-one=one file",
		"--item-count", "1",
		"--concurrency", "1",
		"--local=true",
		"--findings", bad,
		"--out", filepath.Join(dir, "readiness.json"),
	}, readinessNow, func(string) (readiness.BeadReader, error) { return openLedger(), nil }, &stdout, &stderr)

	if code != readinessExitUsage {
		t.Fatalf("exit = %d, want the usage code", code)
	}
	if !strings.Contains(stderr.String(), "findings") {
		t.Errorf("stderr = %s, want it to name the file it could not read", stderr.String())
	}
}
