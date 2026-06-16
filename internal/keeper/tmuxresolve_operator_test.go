package keeper

import (
	"fmt"
	"testing"
	"time"
)

// TestOperatorActiveSince is the white-box unit for the human-vs-remote-control
// distinction at the heart of hk-0t5s. The previous probe counted ANY attached
// client; these cases encode the NEW contract — a client only counts as an
// actively-present operator when its `#{client_activity}` is recent, so an
// idle / remote-control attach (frozen activity) no longer suppresses the
// act-path. The "idle attached client" case is RED against the old any-client
// behavior and GREEN against operatorActiveSince.
func TestOperatorActiveSince(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_781_618_670, 0)
	const window = 5 * time.Minute
	epoch := func(d time.Duration) string {
		return fmt.Sprintf("%d", now.Add(-d).Unix())
	}

	cases := []struct {
		name string
		out  string
		want bool
	}{
		{"no clients (empty output)", "", false},
		{"only whitespace", "  \n\t\n", false},
		{
			// The operator's remote-control / iOS workflow: a terminal stays
			// attached but its keystrokes go through Claude, not tmux, so
			// client_activity is frozen far in the past. Must NOT suppress.
			name: "single idle attached client",
			out:  epoch(16*24*time.Hour) + "\n",
			want: false,
		},
		{
			name: "single just-attached client (activity == now)",
			out:  epoch(0) + "\n",
			want: true,
		},
		{
			name: "active typist within the window",
			out:  epoch(90*time.Second) + "\n",
			want: true,
		},
		{
			name: "client exactly at the window boundary",
			out:  epoch(window) + "\n",
			want: true,
		},
		{
			name: "client just past the window",
			out:  epoch(window+time.Second) + "\n",
			want: false,
		},
		{
			// Many idle clients + one active → active wins (operator IS typing
			// into the pane locally; suppress to avoid racing keystrokes).
			name: "mixed: idle clients plus one active",
			out:  epoch(2*time.Hour) + "\n" + epoch(time.Hour) + "\n" + epoch(30*time.Second) + "\n",
			want: true,
		},
		{
			name: "multiple clients, all idle",
			out:  epoch(time.Hour) + "\n" + epoch(2*time.Hour) + "\n",
			want: false,
		},
		{
			// A future timestamp (clock skew) is treated as active — the safe,
			// suppress-leaning direction.
			name: "future activity timestamp",
			out:  epoch(-time.Minute) + "\n",
			want: true,
		},
		{
			name: "unparseable line is skipped",
			out:  "not-a-number\n",
			want: false,
		},
		{
			name: "unparseable line skipped, trailing active line counts",
			out:  "garbage\n" + epoch(10*time.Second),
			want: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := operatorActiveSince(tc.out, now, window); got != tc.want {
				t.Errorf("operatorActiveSince(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}
