package queue

import "testing"

func TestShutdownDrainIntentIsOneShot(t *testing.T) {
	q := &Queue{Status: QueueStatusActive}
	if err := PauseQueueForRestart(q); err != nil {
		t.Fatal(err)
	}
	if q.Status != QueueStatusPausedByDrain || !q.ResumeOnStart {
		t.Fatalf("shutdown pause = status %q resume_on_start=%v", q.Status, q.ResumeOnStart)
	}
	if err := ResumeQueueFromDrain(q); err != nil {
		t.Fatal(err)
	}
	if q.Status != QueueStatusActive || q.ResumeOnStart {
		t.Fatalf("resumed queue = status %q resume_on_start=%v", q.Status, q.ResumeOnStart)
	}
}

func TestOperatorDrainDoesNotSetRestartIntent(t *testing.T) {
	q := &Queue{Status: QueueStatusActive, ResumeOnStart: true}
	if err := PauseQueueForDrain(q); err != nil {
		t.Fatal(err)
	}
	if q.Status != QueueStatusPausedByDrain || q.ResumeOnStart {
		t.Fatalf("operator pause = status %q resume_on_start=%v; want paused/false", q.Status, q.ResumeOnStart)
	}
}
