package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cfpperche/picode-docker/engine"
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
		if got := validAction(tc.action, tc.state); got != tc.ok {
			t.Errorf("validAction(%q, %q) = %v, want %v", tc.action, tc.state, got, tc.ok)
		}
	}
}

func TestToRowOffersOnlyValidActions(t *testing.T) {
	row := toRow(engine.Container{ID: "a", Name: "web", State: "running"})
	if strings.Join(row.Actions, ",") != "stop,restart" {
		t.Fatalf("actions = %v", row.Actions)
	}
	row = toRow(engine.Container{ID: "a", Name: "web", State: "exited"})
	if strings.Join(row.Actions, ",") != "start" {
		t.Fatalf("actions = %v", row.Actions)
	}
}

// actionPast's participles are real English ("stopped", not "stoped" or
// "%sed"), the source of both the refusal message and the success message.
func TestActionPastIsRealEnglish(t *testing.T) {
	want := map[string]string{"start": "started", "stop": "stopped", "restart": "restarted"}
	if len(actionPast) != len(want) {
		t.Fatalf("actionPast = %v", actionPast)
	}
	for action, past := range want {
		if actionPast[action] != past {
			t.Errorf("actionPast[%q] = %q, want %q", action, actionPast[action], past)
		}
	}
}

// An unknown action is refused before Docker is even reached, and the copy
// names the three real ones — never "pause", which this server does not
// offer.
func TestContainerActionRefusesAnUnknownVerbFirst(t *testing.T) {
	old := secret
	secret = "s3cret"
	t.Cleanup(func() { secret = old })
	os.Unsetenv("PICODE_DOCKER_HOST") // if this reached connect(), it would hang or fail slowly
	req := httptest.NewRequest("POST", "/containers/x/action", strings.NewReader(`{"action":"pause"}`))
	req.Header.Set("X-PiCode-Proxy-Secret", "s3cret")
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	withAuth(func(w http.ResponseWriter, r *http.Request) { containerAction(w, r, r.PathValue("id")) })(w, req)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "start, stop or restart") {
		t.Fatalf("code = %d body %s", w.Code, w.Body.String())
	}
}

// Decision table: only a request with the right secret is answered.
func TestListContainersRefusesWithoutTheSecret(t *testing.T) {
	old := secret
	secret = "s3cret"
	t.Cleanup(func() { secret = old })
	req := httptest.NewRequest("GET", "/containers", nil)
	w := httptest.NewRecorder()
	withAuth(listContainers)(w, req)
	if w.Code != 403 {
		t.Fatalf("code = %d", w.Code)
	}
	req.Header.Set("X-PiCode-Proxy-Secret", "s3cret")
	// No Docker socket reachable in this environment: still answers, as an
	// upstream failure (502), never a hang or a panic.
	w = httptest.NewRecorder()
	os.Setenv("PICODE_DOCKER_HOST", "unix:///nonexistent/engine.sock")
	defer os.Unsetenv("PICODE_DOCKER_HOST")
	withAuth(listContainers)(w, req)
	if w.Code != 502 {
		t.Fatalf("code = %d body %s", w.Code, w.Body.String())
	}
}
