package daemon

// decisionshandler_mailbox_pltjs_test.go — unit tests for the operator-mailbox
// topic + urgency extension of the hitl-decisions surface (bead hk-pltjs,
// pending operator sign-off — this EXTENDS the FINALIZED hitl-decisions SPEC,
// see internal/core.DecisionTopicOperatorMailbox).
//
// Asserts: raise carries topic/urgency through to the K3 projection; the
// decisions-list op's topic filter narrows the result; an invalid urgency is
// rejected (schema-level Valid()); an unset topic/urgency stays wire-compatible
// with the prior (untagged) behavior.
//
// Reuses the dk4* helpers from decisionshandler_k4_kba_test.go.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// dk4RaiseMailbox raises a decision with a topic + urgency and returns its
// decision_id.
func dk4RaiseMailbox(t *testing.T, s *dk4Setup, question, topic, urgency string) string {
	t.Helper()
	req := DecisionsRaiseRequest{
		Question: question,
		Options:  []string{"ack", "defer"},
		Topic:    topic,
		Urgency:  urgency,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("dk4RaiseMailbox: marshal: %v", err)
	}
	resultBytes, err := s.h.HandleDecisionsRaise(context.Background(), payload)
	if err != nil {
		t.Fatalf("dk4RaiseMailbox(%q): HandleDecisionsRaise: %v", question, err)
	}
	var res DecisionsRaiseResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("dk4RaiseMailbox: decode result: %v", err)
	}
	if res.DecisionID == "" {
		t.Fatal("dk4RaiseMailbox: empty decision_id")
	}
	return res.DecisionID
}

// dk4ListByTopic calls HandleDecisionsList with a topic filter.
func dk4ListByTopic(t *testing.T, s *dk4Setup, topic string) DecisionsListResult {
	t.Helper()
	payload, err := json.Marshal(DecisionsListRequest{Topic: topic})
	if err != nil {
		t.Fatalf("dk4ListByTopic: marshal: %v", err)
	}
	resultBytes, err := s.h.HandleDecisionsList(context.Background(), payload)
	if err != nil {
		t.Fatalf("dk4ListByTopic: HandleDecisionsList: %v", err)
	}
	var res DecisionsListResult
	if err := json.Unmarshal(resultBytes, &res); err != nil {
		t.Fatalf("dk4ListByTopic: decode result: %v", err)
	}
	return res
}

// TestDecisionsRaise_MailboxTopicAndUrgencyRoundTrip: raise with
// topic=operator-mailbox + urgency=blocker round-trips through the K3
// projection into decisions-list.
func TestDecisionsRaise_MailboxTopicAndUrgencyRoundTrip(t *testing.T) {
	s := dk4NewHandler(t)
	did := dk4RaiseMailbox(t, s, "Approve the redeploy?", core.DecisionTopicOperatorMailbox, string(core.DecisionUrgencyBlocker))

	res := dk4List(t, s, did)
	if len(res.Decisions) != 1 {
		t.Fatalf("list: got %d, want 1", len(res.Decisions))
	}
	item := res.Decisions[0]
	if item.Topic != core.DecisionTopicOperatorMailbox {
		t.Errorf("topic = %q, want %q", item.Topic, core.DecisionTopicOperatorMailbox)
	}
	if item.Urgency != string(core.DecisionUrgencyBlocker) {
		t.Errorf("urgency = %q, want %q", item.Urgency, core.DecisionUrgencyBlocker)
	}
}

// TestDecisionsList_TopicFilter: the decisions-list topic filter narrows the
// result to items on that exact topic — this is what `harmonik mailbox`
// (decisions list --topic operator-mailbox) relies on to scope the mailbox
// view without inventing a second bus.
func TestDecisionsList_TopicFilter(t *testing.T) {
	s := dk4NewHandler(t)
	mailboxID := dk4RaiseMailbox(t, s, "Sign off on the release?", core.DecisionTopicOperatorMailbox, string(core.DecisionUrgencyQuestion))
	_ = dk4Raise(t, s, "Pick a runner", []string{"a", "b"}, "bob", "") // untagged, not in the mailbox

	res := dk4ListByTopic(t, s, core.DecisionTopicOperatorMailbox)
	if len(res.Decisions) != 1 {
		t.Fatalf("topic filter: got %d, want 1", len(res.Decisions))
	}
	if res.Decisions[0].DecisionID != mailboxID {
		t.Errorf("topic filter returned %q, want %q", res.Decisions[0].DecisionID, mailboxID)
	}
}

// TestDecisionsRaise_InvalidUrgencyRejected: an urgency outside
// blocker|question|fyi is rejected at the schema level (Valid()) — no event
// emitted.
func TestDecisionsRaise_InvalidUrgencyRejected(t *testing.T) {
	s := dk4NewHandler(t)
	req := DecisionsRaiseRequest{
		Question: "Bad urgency?",
		Options:  []string{"a", "b"},
		Urgency:  "urgent!!1",
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := s.h.HandleDecisionsRaise(context.Background(), payload); err == nil {
		t.Fatal("raise with an invalid urgency: want an error, got nil")
	}
	if open := decisionsProjection(s.eventsPath); len(open) != 0 {
		t.Errorf("invalid urgency must emit no decision_needed, open set = %d, want 0", len(open))
	}
}

// TestDecisionsRaise_UntaggedStaysWireCompatible: a raise with no topic/urgency
// (the pre-hk-pltjs shape) still round-trips with both fields empty — the
// extension is additive.
func TestDecisionsRaise_UntaggedStaysWireCompatible(t *testing.T) {
	s := dk4NewHandler(t)
	did := dk4Raise(t, s, "Untagged question", []string{"a", "b"}, "alice", "")

	res := dk4List(t, s, did)
	if len(res.Decisions) != 1 {
		t.Fatalf("list: got %d, want 1", len(res.Decisions))
	}
	item := res.Decisions[0]
	if item.Topic != "" || item.Urgency != "" {
		t.Errorf("untagged raise: topic=%q urgency=%q, want both empty", item.Topic, item.Urgency)
	}
}
