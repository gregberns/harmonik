package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
)

var subscribeStreamOnlyTypes = []string{"heartbeat", "subscription_gap"}

const subscribeSuggestMaxDistance = 3

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
