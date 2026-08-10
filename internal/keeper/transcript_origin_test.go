package keeper

import (
	"encoding/json"
	"testing"
)

func TestIsRealTranscriptTurn_SeparatesOperatorFromAutomation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "operator", text: "Please send this to alpha", want: true},
		{name: "keeper", text: AutomationMessage("keeper", "Context threshold crossed"), want: false},
		{name: "captain comms", text: AutomationMessage("comms", "[from captain] check inbox"), want: false},
		{name: "keeper slash command", text: "/session-handoff HANDOFF-alpha.md", want: false},
		{name: "claude command record", text: "<command-name>/session-handoff</command-name>", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if got := isRealTranscriptTurn("user", raw); got != tt.want {
				t.Fatalf("isRealTranscriptTurn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsRealTranscriptTurn_ArrayTextUsesOrigin(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`[{"type":"text","text":"[[harmonik-message:v1 origin=comms]]\n[from captain] check inbox"}]`)
	if isRealTranscriptTurn("user", raw) {
		t.Fatal("comms envelope in array transcript counted as operator activity")
	}
}
