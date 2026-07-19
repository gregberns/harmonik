//go:build specaudit

package specaudit_test

// hk-hqwn.48 binding test — EV-INV-002 event-loss-between-fsyncs invariant
// (spec-corpus sensor phase).
//
// Spec ref: specs/event-model.md §5.EV-INV-002.
//
// EV-INV-002 states:
//
//	"A hard crash between fsync boundaries MAY lose events emitted in that window.
//	Producers satisfy this invariant via EV-018 (idempotent emission). Consumers
//	MUST be coded to handle a tail-truncated event stream on recovery per EV-014b.
//	This invariant is a two-sided operational covenant, not a producer-only claim."
//
// # Sensor scope
//
// This test is the spec-corpus phase of the hk-hqwn.48 sensor.  The full
// paired contract test (kill daemon between fsync boundaries; on restart confirm
// consumers resume from offset_checkpoint_event_id per EV-014d; confirm
// idempotent re-delivery does not produce double side effects) is blocked on the
// EventBus concrete implementation and MUST be added in a follow-up bead when
// the implementation lands.
//
// This sensor verifies that EV-INV-002 is correctly declared in the spec so
// that the runtime contract test (when written) has a stable declaration to
// exercise:
//
//  1. EV-INV-002 heading is present in specs/event-model.md.
//  2. Hard-crash loss declaration is present.
//  3. EV-018 producer pairing is declared.
//  4. EV-014b consumer tail-truncation pairing is declared.
//  5. Two-sided covenant declaration is present (not a producer-only claim).
//  6. Tags: mechanism is present in the EV-INV-002 body window.
//
// The related spec-corpus sensors that cover the component requirements are:
//   - hqwn20_consumer_idempotent_replay_test.go (EV-014b — consumer idempotency)
//   - hqwn22_consumer_recovery_replay_test.go (EV-014d — consumer-recovery replay)
//   - hqwn25_event_loss_acceptable_test.go (EV-017 — event loss acceptable)
//
// Together with those sensors, this test confirms the full two-sided covenant is
// declared and cross-referenced in the spec corpus.
//
// # Helper prefix
//
// All package-level identifiers in this file use the hqwn48Fixture prefix per
// the implementer-protocol.md helper-prefix discipline.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// hqwn48FixtureEventModelPath returns the absolute path to specs/event-model.md.
func hqwn48FixtureEventModelPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("hqwn48FixtureEventModelPath: runtime.Caller(0) failed")
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))
	return filepath.Join(repoRoot, "specs", "event-model.md")
}

// hqwn48FixtureEVINV002Heading matches the EV-INV-002 level-4 requirement heading.
var hqwn48FixtureEVINV002Heading = regexp.MustCompile(`^#### EV-INV-002 —`)

// hqwn48FixtureAnySectionHeading matches any Markdown heading (level 1–4),
// used to detect when the EV-INV-002 body window has ended.
var hqwn48FixtureAnySectionHeading = regexp.MustCompile(`^#{1,4} `)

// hqwn48FixtureTagsMechanism matches a "Tags: mechanism" line.
var hqwn48FixtureTagsMechanism = regexp.MustCompile(`^Tags:.*\bmechanism\b`)

// hqwn48FixtureBodyWindow is the maximum number of lines after the EV-INV-002
// heading to scan for requirement-body content.
const hqwn48FixtureBodyWindow = 12

// hqwn48FixtureLoadLines opens specFile and returns all lines (without newlines).
func hqwn48FixtureLoadLines(t *testing.T, specFile string) []string {
	t.Helper()
	//nolint:gosec // G304: specFile is constructed from runtime.Caller repo-root + known relative path; not user input
	f, err := os.Open(specFile)
	if err != nil {
		t.Fatalf("hqwn48FixtureLoadLines: open %q: %v", specFile, err)
	}
	defer func() { _ = f.Close() }()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if scanErr := scanner.Err(); scanErr != nil {
		t.Fatalf("hqwn48FixtureLoadLines: scan: %v", scanErr)
	}
	return lines
}

// hqwn48FixtureLocateEVINV002Body finds the EV-INV-002 heading line and
// returns the body lines that follow (up to the next heading or
// hqwn48FixtureBodyWindow lines).
func hqwn48FixtureLocateEVINV002Body(lines []string) (headingIdx int, body []string) {
	headingIdx = -1
	for i, line := range lines {
		if hqwn48FixtureEVINV002Heading.MatchString(line) {
			headingIdx = i
			break
		}
	}
	if headingIdx < 0 {
		return -1, nil
	}
	start := headingIdx + 1
	end := start
	for end < len(lines) && end < start+hqwn48FixtureBodyWindow {
		if hqwn48FixtureAnySectionHeading.MatchString(lines[end]) {
			break
		}
		end++
	}
	return headingIdx, lines[start:end]
}

// hqwn48FixtureBodyContains returns true if any line in body contains needle
// (case-insensitive substring match).
func hqwn48FixtureBodyContains(body []string, needle string) bool {
	needleLower := strings.ToLower(needle)
	for _, line := range body {
		if strings.Contains(strings.ToLower(line), needleLower) {
			return true
		}
	}
	return false
}

// TestHqwn48_EVINV002_HeadingPresent verifies that the EV-INV-002 heading is
// present in specs/event-model.md.
//
// A missing heading means either the invariant was removed from the spec or the
// heading syntax changed; both require updating this sensor.
func TestHqwn48_EVINV002_HeadingPresent(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	idx, _ := hqwn48FixtureLocateEVINV002Body(lines)
	if idx < 0 {
		t.Errorf(
			"EV-INV-002 heading not found in %q; "+
				"the invariant must be declared in §5 (specs/event-model.md §5.EV-INV-002)",
			filepath.Base(specFile),
		)
	}
}

// TestHqwn48_EVINV002_HardCrashLossDeclared verifies that the EV-INV-002
// body declares that a hard crash between fsync boundaries MAY lose events.
//
// This is the core claim of EV-INV-002: producers and consumers must be designed
// knowing that event loss is possible after a hard crash.
func TestHqwn48_EVINV002_HardCrashLossDeclared(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	_, body := hqwn48FixtureLocateEVINV002Body(lines)
	if len(body) == 0 {
		t.Fatalf("EV-INV-002 body not found; heading may be missing (run TestHqwn48_EVINV002_HeadingPresent)")
	}

	// The body must declare hard-crash loss acceptability.
	if !hqwn48FixtureBodyContains(body, "hard crash") {
		t.Errorf(
			"EV-INV-002 body does not declare hard-crash loss; "+
				"expected phrase 'hard crash' in body (EV-INV-002 core claim)\nbody:\n%s",
			formatBodyLines(body),
		)
	}
}

// TestHqwn48_EVINV002_EV018ProducerPairing verifies that the EV-INV-002 body
// references EV-018 (producer idempotent emission) as the producer-side
// satisfaction of the invariant.
//
// EV-018 is the mechanism by which producers ensure that re-emitting an event
// (after a crash+recovery) does not create duplicate side effects. The pairing
// is load-bearing: omitting EV-018 from the invariant declaration makes it a
// producer-only consumer-hostile obligation.
func TestHqwn48_EVINV002_EV018ProducerPairing(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	_, body := hqwn48FixtureLocateEVINV002Body(lines)
	if len(body) == 0 {
		t.Fatalf("EV-INV-002 body not found")
	}

	if !hqwn48FixtureBodyContains(body, "EV-018") {
		t.Errorf(
			"EV-INV-002 body does not reference EV-018 (producer idempotency pairing); "+
				"expected 'EV-018' in body (EV-INV-002 producer-side satisfaction)\nbody:\n%s",
			formatBodyLines(body),
		)
	}
}

// TestHqwn48_EVINV002_EV014bConsumerPairing verifies that the EV-INV-002 body
// references EV-014b (consumer tail-truncation obligation) as the consumer-side
// satisfaction of the invariant.
//
// EV-014b is the mechanism by which consumers tolerate tail-truncated event
// streams on recovery. Without this pairing, EV-INV-002 would be a
// producer-only claim that leaves consumers with no guidance.
func TestHqwn48_EVINV002_EV014bConsumerPairing(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	_, body := hqwn48FixtureLocateEVINV002Body(lines)
	if len(body) == 0 {
		t.Fatalf("EV-INV-002 body not found")
	}

	if !hqwn48FixtureBodyContains(body, "EV-014b") {
		t.Errorf(
			"EV-INV-002 body does not reference EV-014b (consumer tail-truncation pairing); "+
				"expected 'EV-014b' in body (EV-INV-002 consumer-side obligation)\nbody:\n%s",
			formatBodyLines(body),
		)
	}
}

// TestHqwn48_EVINV002_TwoSidedCovenantDeclared verifies that the EV-INV-002
// body explicitly declares this as a two-sided covenant, not a producer-only claim.
//
// The two-sided framing is load-bearing: it prevents implementers from treating
// EV-INV-002 as permission to ignore event loss on the consumer side. The spec
// must explicitly reject the "producer-only" interpretation.
func TestHqwn48_EVINV002_TwoSidedCovenantDeclared(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	_, body := hqwn48FixtureLocateEVINV002Body(lines)
	if len(body) == 0 {
		t.Fatalf("EV-INV-002 body not found")
	}

	// The body must declare the two-sided covenant (and reject producer-only framing).
	hasTwoSided := hqwn48FixtureBodyContains(body, "two-sided")
	hasNotProducerOnly := hqwn48FixtureBodyContains(body, "not a producer-only")
	if !hasTwoSided && !hasNotProducerOnly {
		t.Errorf(
			"EV-INV-002 body does not declare the two-sided covenant; "+
				"expected 'two-sided' or 'not a producer-only' in body "+
				"(EV-INV-002 must explicitly reject producer-only interpretation)\nbody:\n%s",
			formatBodyLines(body),
		)
	}
}

// TestHqwn48_EVINV002_TagsMechanismPresent verifies that the EV-INV-002 body
// window carries the "Tags: mechanism" line, conforming to the §8.9(g) orphan-
// lint criterion that every §5 invariant is mechanism-tagged.
func TestHqwn48_EVINV002_TagsMechanismPresent(t *testing.T) {
	t.Parallel()

	specFile := hqwn48FixtureEventModelPath(t)
	lines := hqwn48FixtureLoadLines(t, specFile)
	headingIdx, body := hqwn48FixtureLocateEVINV002Body(lines)
	if len(body) == 0 {
		t.Fatalf("EV-INV-002 body not found")
	}

	// Check the line immediately before the heading (Tags: is often written
	// before the heading in this spec's style) OR within the body.
	found := false
	if headingIdx > 0 && hqwn48FixtureTagsMechanism.MatchString(lines[headingIdx-1]) {
		found = true
	}
	if !found {
		for _, line := range body {
			if hqwn48FixtureTagsMechanism.MatchString(line) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf(
			"EV-INV-002 section does not carry 'Tags: mechanism'; "+
				"invariants in §5 MUST be mechanism-tagged (§8.9(g) orphan-lint criterion)\nbody:\n%s",
			formatBodyLines(body),
		)
	}
}

// formatBodyLines formats body lines for test failure messages.
func formatBodyLines(body []string) string {
	var sb strings.Builder
	for i, line := range body {
		sb.WriteString(fmt.Sprintf("  [%d] %s\n", i, line))
	}
	return sb.String()
}
