package main

// subscribetypes.go — the `--types` filter vocabulary for `harmonik subscribe`.
//
// A subscribe filter names event types. Nothing downstream ever objects to a
// name that does not exist: the daemon builds a map from the strings it is
// given, matches no event against it, and keeps the connection open. Idle
// heartbeats continue on cadence and carry the live active-run list, so the
// stream looks connected and correct. It delivers nothing, forever.
//
// That failure mode is why this file exists. The canonical monitor pattern in
// the harmonik-dispatch skill is a subscribe with an explicit --types list
// (usually run_completed and run_failed). One typo in that list turns the
// project's main observation surface into silence that reads as health — the
// observer concludes the work is still running. A subscribe that cannot match
// any event MUST say so at connect time, the way an unparseable
// --since-event-id already does (internal/daemon/socket.go handleSubscribe).
//
// The check is client-side on purpose. The vocabulary comes from the event
// registry compiled into THIS binary, so the CLI and the vocabulary it
// validates against can never disagree. A daemon-side check would instead make
// a newer client and an older daemon disagree about a type that is perfectly
// real.
//
// The trade is not hole-free, and the holes run the other way. An OLD binary
// talking to a NEWER daemon refuses a type that daemon really does emit. A
// --since-event-id replay reads envelopes off disk with no registry check, so a
// type that has since been renamed is refused here although the disk still
// holds it. Both fail loudly with exit 1, which is why the trade is still
// right: a wrong refusal is visible in a second, and silence that reads as
// health is not.
//
// This does not make the whole tree safe. The other socket subscribers in
// cmd/harmonik — run_via_daemon.go, smoke.go, decisions.go, comms.go — and the
// subscribes in scripts/ all hand-write their type strings and do NOT call this
// validator, so a typo in one of them still fails silently. run_via_daemon.go
// is the completion detector for `harmonik run`, so that gap is worth closing;
// every one of them is in package main and could call validateSubscribeTypes.
// Until then the guarantee here covers the operator-typed --types flag only.
//
// Bead ref: hk-subscribe-accepts-unknown-type-rd07b.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

// subscribeStreamOnlyTypes are line types a subscriber can ask for that are not
// registry event types. The daemon synthesizes these itself, outside the event
// bus, so they never appear in the event registry — but they DO appear in the
// stream, so refusing them would refuse a name the operator read there.
//
// "heartbeat" is the documented liveness probe (captain/SHUTDOWN.md uses
// `--types heartbeat`), and refusing it would break working commands.
// "subscription_gap" is the drop notice (internal/daemon/subscribe.go
// subscriptionGapLine).
var subscribeStreamOnlyTypes = []string{"heartbeat", "subscription_gap"}

// subscribeSuggestMaxDistance is the largest edit distance that still counts as
// a plausible typo. Three covers the realistic slips — a dropped suffix
// ("run_complete"), a transposition, a doubled character — without proposing an
// unrelated type for a name that was simply invented.
const subscribeSuggestMaxDistance = 3

// knownSubscribeTypes returns every value --types accepts, sorted. The event
// registry is the source of truth: it holds exactly the types the daemon can
// dispatch, and it is filled by package core's own init functions, so this list
// needs no maintenance when a new event type lands.
func knownSubscribeTypes() []string {
	registered := core.AllPayloadSchemaVersions()
	out := make([]string, 0, len(registered)+len(subscribeStreamOnlyTypes))
	for t := range registered {
		out = append(out, string(t))
	}
	out = append(out, subscribeStreamOnlyTypes...)
	sort.Strings(out)
	return out
}

// validateSubscribeTypes refuses a --types list that names an event type which
// does not exist. It reports every bad value in one error, names each one, and
// offers the nearest real types when the value looks like a typo.
//
// An empty list is the wildcard subscription and is always valid.
func validateSubscribeTypes(types []string) error {
	if len(types) == 0 {
		return nil
	}
	known := knownSubscribeTypes()
	inVocabulary := make(map[string]struct{}, len(known))
	for _, t := range known {
		inVocabulary[t] = struct{}{}
	}

	lines := make([]string, 0, len(types))
	seen := make(map[string]struct{}, len(types))
	for _, t := range types {
		if _, ok := inVocabulary[t]; ok {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		line := fmt.Sprintf("%q is not a known event type", t)
		if near := nearestSubscribeTypes(t, known); len(near) > 0 {
			line += fmt.Sprintf(" (did you mean %s?)", quoteAndJoin(near))
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return nil
	}
	return fmt.Errorf("--types: %s. Run `harmonik subscribe --list-types` to print all %d accepted types",
		strings.Join(lines, "; "), len(known))
}

// nearestSubscribeTypes returns the known types closest to bad by edit
// distance, at most three of them, and nothing at all when the closest is too
// far to be a typo. A value that is a substring of a real type (the dropped-
// suffix slip, "run_complete" for "run_completed") always qualifies.
func nearestSubscribeTypes(bad string, known []string) []string {
	best := subscribeSuggestMaxDistance + 1
	var matches []string
	for _, k := range known {
		d := subscribeTypeEditDistance(bad, k)
		if strings.Contains(k, bad) && d < subscribeSuggestMaxDistance {
			d = 0
		}
		switch {
		case d < best:
			best = d
			matches = []string{k}
		case d == best && best <= subscribeSuggestMaxDistance:
			matches = append(matches, k)
		}
	}
	if best > subscribeSuggestMaxDistance {
		return nil
	}
	if len(matches) > 3 {
		matches = matches[:3]
	}
	return matches
}

// subscribeTypeEditDistance is the Levenshtein distance between a and b. Event
// type names are short ASCII identifiers, so the two-row form is enough.
func subscribeTypeEditDistance(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := 0; j <= len(b); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = minOfThree(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func minOfThree(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// quoteAndJoin renders a suggestion list as `"a", "b" or "c"`.
func quoteAndJoin(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}
