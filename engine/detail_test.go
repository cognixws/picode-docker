package engine

import (
	"strings"
	"testing"
)

// Decision table: an action only runs from the state it makes sense in.
func TestValidAction(t *testing.T) {
	for _, tc := range []struct {
		action, state string
		ok            bool
	}{
		{"start", "created", true}, {"start", "exited", true}, {"start", "running", false},
		{"stop", "running", true}, {"stop", "restarting", true}, {"stop", "exited", false},
		{"restart", "running", true}, {"restart", "exited", false},
		{"pause", "running", false},
	} {
		if got := ValidAction(tc.action, tc.state); got != tc.ok {
			t.Errorf("ValidAction(%q, %q) = %v, want %v", tc.action, tc.state, got, tc.ok)
		}
	}
}

func TestRowOfOffersOnlyValidActions(t *testing.T) {
	row := RowOf(Container{ID: "a", Name: "web", State: "running"})
	if strings.Join(row.Actions, ",") != "stop,restart" {
		t.Fatalf("actions = %v", row.Actions)
	}
	row = RowOf(Container{ID: "a", Name: "web", State: "exited"})
	if strings.Join(row.Actions, ",") != "start" {
		t.Fatalf("actions = %v", row.Actions)
	}
}

// ActionPast's participles are real English ("stopped", not "stoped" or
// "%sed"), the source of both a refusal and a success message.
func TestActionPastIsRealEnglish(t *testing.T) {
	want := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}
	if len(ActionPast) != len(want) {
		t.Fatalf("ActionPast = %v", ActionPast)
	}
	for action, past := range want {
		if ActionPast[action] != past {
			t.Errorf("ActionPast[%q] = %q, want %q", action, ActionPast[action], past)
		}
	}
}
