package queue_test

import (
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/queue"
)

func TestUnknownQueueErrorRendersEachVerb(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  queue.UnknownQueueError
		want string
	}{
		{
			name: "pause takes the default past tense",
			err: queue.UnknownQueueError{
				Verb:           "pause",
				NormalizedName: "mian",
				KnownNames:     []string{"canary"},
			},
			want: "no queue named \"mian\": `harmonik queue pause` changed nothing and no queue was paused; queues that exist: canary",
		},
		{
			name: "resume takes the default past tense",
			err: queue.UnknownQueueError{
				Verb:           "resume",
				NormalizedName: "mian",
				KnownNames:     []string{"canary", "main"},
			},
			want: "no queue named \"mian\": `harmonik queue resume` changed nothing and no queue was resumed; queues that exist: canary, main",
		},
		{
			name: "cancel overrides the past tense",
			err: queue.UnknownQueueError{
				Verb:           "cancel",
				PastTense:      "cancelled",
				NormalizedName: "mian",
				KnownNames:     []string{"canary"},
			},
			want: "no queue named \"mian\": `harmonik queue cancel` changed nothing and no queue was cancelled; queues that exist: canary",
		},
		{
			name: "a queue_id selector reads as an id, not a name",
			err: queue.UnknownQueueError{
				Verb:       "cancel",
				PastTense:  "cancelled",
				QueueID:    "00000000-0000-7000-8000-000000000000",
				KnownNames: []string{"canary"},
			},
			want: "no queue with id \"00000000-0000-7000-8000-000000000000\": `harmonik queue cancel` changed nothing and no queue was cancelled; queues that exist: canary",
		},
		{
			name: "no queues at all",
			err: queue.UnknownQueueError{
				Verb:           "pause",
				NormalizedName: "mian",
			},
			want: "no queue named \"mian\": `harmonik queue pause` changed nothing and no queue was paused; queues that exist: none are loaded",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("UnknownQueueError.Error() =\n %q\nwant\n %q", got, tc.want)
			}
		})
	}
}

// TestUnknownQueueErrorVerbsDifferOnlyWhereIntended is the anti-drift check the
// shared renderer needs: pause and cancel are the same sentence about the same
// mistake, and the only words that may differ between them are the verb and its
// past tense. A future edit that reworded one of them alone would show up here.
func TestUnknownQueueErrorVerbsDifferOnlyWhereIntended(t *testing.T) {
	t.Parallel()

	pause := (&queue.UnknownQueueError{
		Verb:           "pause",
		NormalizedName: "mian",
		KnownNames:     []string{"canary"},
	}).Error()
	cancel := (&queue.UnknownQueueError{
		Verb:           "cancel",
		PastTense:      "cancelled",
		NormalizedName: "mian",
		KnownNames:     []string{"canary"},
	}).Error()

	normalized := strings.ReplaceAll(pause, "queue pause`", "queue cancel`")
	normalized = strings.ReplaceAll(normalized, "was paused", "was cancelled")
	if normalized != cancel {
		t.Errorf("pause and cancel refusals differ beyond the verb:\n pause (verb-substituted): %q\n cancel:                   %q", normalized, cancel)
	}
}
