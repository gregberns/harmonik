package queue_test

// unknownqueueerror_test.go — the rendering of queue.UnknownQueueError.
//
// This is the one refusal three operator verbs share. `queue pause` and
// `queue resume` build it in the daemon (internal/daemon
// refuseUnknownQueueLocked); `queue cancel` builds it in the CLI
// (internal/queue/cli refuseUnknownCancelQueue). One renderer serves all three,
// so a change made for one of them lands on the other two, and nothing was
// holding that renderer to its output.
//
// The empty-PastTense DEFAULT is the load-bearing case here. It is what keeps
// the pause and resume messages byte-identical across the change that added the
// field for cancel's sake, and neither daemon call site sets it. A test that
// only ever sets the field measures nothing about the verbs that do not.
//
// Bead ref: hk-wka5o.

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
			// The daemon builds pause exactly like this: no PastTense, no
			// QueueID. This string is what an operator saw before the field
			// existed, and it must not have moved.
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
			// "canceld" is what the default would produce, which is why cancel
			// sets the field.
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
			// A uuid is not a name. `queue cancel --queue-id` is the only
			// selector that reaches this branch today.
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
			// An empty project has to read as an absence, not as an empty list.
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

	// Rewrite pause's two verb-shaped words into cancel's and the two messages
	// must be the same string.
	normalized := strings.ReplaceAll(pause, "queue pause`", "queue cancel`")
	normalized = strings.ReplaceAll(normalized, "was paused", "was cancelled")
	if normalized != cancel {
		t.Errorf("pause and cancel refusals differ beyond the verb:\n pause (verb-substituted): %q\n cancel:                   %q", normalized, cancel)
	}
}
