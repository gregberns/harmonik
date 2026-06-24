package queue

import "testing"

func TestNormaliseQueueNameSoakT5(t *testing.T) {
	if got := NormaliseQueueName(""); got != QueueNameMain {
		t.Errorf("NormaliseQueueName(%q) = %q, want %q", "", got, QueueNameMain)
	}
	if got := NormaliseQueueName("leto-ev"); got != "leto-ev" {
		t.Errorf("NormaliseQueueName(%q) = %q, want %q", "leto-ev", got, "leto-ev")
	}
}
