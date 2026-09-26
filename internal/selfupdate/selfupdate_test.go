package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func agentDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), []byte(`{"method":"install"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestNoAgentMeansNoButton(t *testing.T) {
	u := New(t.TempDir())
	if u.Available() {
		t.Fatal("available without agent.json")
	}
	if _, err := u.Request("0.3.0", "a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("request without agent: %v", err)
	}
	if New("").Available() || (*Updater)(nil).Available() {
		t.Fatal("available with no directory")
	}
}

func TestRequestIsQueuedThenFollowsTheAgent(t *testing.T) {
	dir := agentDir(t)
	u := New(dir)
	for _, bad := range []string{"", "latest", "0.3", "0.3.0; rm -rf /", "../0.3.0", "v0.3.0"} {
		if _, err := u.Request(bad, "a"); !errors.Is(err, ErrBadTarget) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	request, err := u.Request("0.3.0-beta.3", "admin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.ID) != 16 {
		t.Fatalf("id %q", request.ID)
	}
	status, ok := u.Current()
	if !ok || status.State != StateQueued || status.Target != "0.3.0-beta.3" || !u.Busy() {
		t.Fatalf("queued: %+v", status)
	}
	if _, err := u.Request("0.3.0-beta.3", "a"); !errors.Is(err, ErrBusy) {
		t.Fatalf("second request: %v", err)
	}

	// The agent claims the request and reports on it.
	_ = os.Remove(filepath.Join(dir, "request.json"))
	_ = writeJSON(filepath.Join(dir, "status.json"), Status{ID: request.ID, Target: request.Target, State: StateRunning, Phase: "pulling", UpdatedAt: time.Now()})
	status, _ = u.Current()
	if status.State != StateRunning || status.Phase != "pulling" || !u.Busy() {
		t.Fatalf("running: %+v", status)
	}
	_ = writeJSON(filepath.Join(dir, "status.json"), Status{ID: request.ID, Target: request.Target, State: StateSucceeded, UpdatedAt: time.Now()})
	if u.Busy() {
		t.Fatal("busy after success")
	}
	if _, err := u.Request("0.3.0-beta.4", "a"); err != nil {
		t.Fatalf("a new request after a finished one: %v", err)
	}
}

// An agent that never answers must not leave the page waiting forever.
func TestUnclaimedRequestFails(t *testing.T) {
	u := New(agentDir(t))
	if _, err := u.Request("0.3.0", "a"); err != nil {
		t.Fatal(err)
	}
	u.now = func() time.Time { return time.Now().Add(staleRequest + time.Minute) }
	status, _ := u.Current()
	if status.State != StateFailed || status.Message == "" || u.Busy() {
		t.Fatalf("stale: %+v", status)
	}
}
