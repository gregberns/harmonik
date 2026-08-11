package queue

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestCloneQueueDetachesEveryNestedMutableField(t *testing.T) {
	runID := "0197c454-0000-7000-8000-000000000001"
	receiptID := "0197c454-0000-7000-8000-000000000002"
	appendedAt := time.Date(2026, 8, 10, 1, 2, 3, 4, time.UTC)
	startedAt := appendedAt.Add(time.Minute)
	completedAt := startedAt.Add(time.Minute)
	input := &Queue{
		SchemaVersion:           1,
		QueueID:                 "0197c454-0000-7000-8000-000000000003",
		FailedRecoveryReceiptID: &receiptID,
		Name:                    QueueNameMain,
		Groups: []Group{{
			GroupIndex:  0,
			StartedAt:   &startedAt,
			CompletedAt: &completedAt,
			Items: []Item{{
				RunID:          &runID,
				AppendedAt:     &appendedAt,
				TemplateParams: map[string]string{"mode": "fixed"},
			}},
		}},
	}
	cloned := CloneQueue(input)

	*cloned.FailedRecoveryReceiptID = "changed-receipt"
	cloned.Groups[0].GroupIndex = 9
	*cloned.Groups[0].StartedAt = startedAt.Add(time.Hour)
	*cloned.Groups[0].CompletedAt = completedAt.Add(time.Hour)
	*cloned.Groups[0].Items[0].RunID = "changed-run"
	*cloned.Groups[0].Items[0].AppendedAt = appendedAt.Add(time.Hour)
	cloned.Groups[0].Items[0].TemplateParams["mode"] = "changed"
	cloned.Groups = append(cloned.Groups, Group{GroupIndex: 1})
	cloned.Groups[0].Items = append(cloned.Groups[0].Items, Item{})

	if *input.FailedRecoveryReceiptID != receiptID || len(input.Groups) != 1 ||
		input.Groups[0].GroupIndex != 0 || *input.Groups[0].StartedAt != startedAt ||
		*input.Groups[0].CompletedAt != completedAt || len(input.Groups[0].Items) != 1 ||
		*input.Groups[0].Items[0].RunID != runID || *input.Groups[0].Items[0].AppendedAt != appendedAt ||
		input.Groups[0].Items[0].TemplateParams["mode"] != "fixed" {
		t.Fatalf("clone mutation changed input: %+v", input)
	}
}

func TestCloneQueuePreservesNilShape(t *testing.T) {
	if CloneQueue(nil) != nil {
		t.Fatal("nil queue did not stay nil")
	}
	input := &Queue{Groups: []Group{{Items: []Item{{}}}}}
	cloned := CloneQueue(input)
	if cloned.FailedRecoveryReceiptID != nil || cloned.Groups[0].StartedAt != nil ||
		cloned.Groups[0].CompletedAt != nil || cloned.Groups[0].Items[0].RunID != nil ||
		cloned.Groups[0].Items[0].AppendedAt != nil || cloned.Groups[0].Items[0].TemplateParams != nil {
		t.Fatalf("nil shape changed: %+v", cloned)
	}

	nilGroups := CloneQueue(&Queue{})
	if nilGroups.Groups != nil {
		t.Fatalf("nil groups became non-nil: %#v", nilGroups.Groups)
	}
	nilItems := CloneQueue(&Queue{Groups: []Group{{}}})
	if nilItems.Groups[0].Items != nil {
		t.Fatalf("nil items became non-nil: %#v", nilItems.Groups[0].Items)
	}
	emptyGroups := CloneQueue(&Queue{Groups: []Group{}})
	if emptyGroups.Groups == nil {
		t.Fatal("empty groups became nil")
	}
	emptyItems := CloneQueue(&Queue{Groups: []Group{{Items: []Item{}}}})
	if emptyItems.Groups[0].Items == nil {
		t.Fatal("empty items became nil")
	}
	empty := CloneQueue(&Queue{Groups: []Group{{Items: []Item{{TemplateParams: map[string]string{}}}}}})
	if empty.Groups == nil || empty.Groups[0].Items == nil || empty.Groups[0].Items[0].TemplateParams == nil {
		t.Fatalf("non-nil empty shape became nil: %#v", empty)
	}
}

func TestCloneQueuePreservesCanonicalJSON(t *testing.T) {
	input := completionFixtureQueue()
	input.Groups[0].Items[0].TemplateParams = map[string]string{"mode": "fixed"}
	want, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(CloneQueue(&input))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("clone JSON changed\n got: %s\nwant: %s", got, want)
	}
}
