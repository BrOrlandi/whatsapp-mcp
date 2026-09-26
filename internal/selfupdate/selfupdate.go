// Package selfupdate lets the panel ask the server to update the gateway.
//
// A container cannot replace its own image: that takes the Docker daemon, and
// a gateway holding the Docker socket is a gateway whose every bug — and it
// reads text written by strangers all day — is root on the host. So the panel
// does not update anything. It writes a request into a directory the host
// shares with it, and an agent on the host, installed by install.sh as a
// systemd unit, notices the file, runs the same update.sh an operator would
// run by SSH, and writes its progress back next to the request.
//
// The directory is the whole protocol:
//
//	agent.json    written by the agent when it is installed: that it exists,
//	              and how it updates this deployment
//	request.json  written by the panel: the version to move to
//	status.json   written by the agent: where the update is, and how it ended
//
// Nothing the agent reads from request.json is executed or interpolated. It
// takes one field, the target version, checks it against a strict pattern and
// against the releases GitHub has published, and passes it to update.sh as the
// tag to move to.
package selfupdate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Agent is what the host agent says about itself.
type Agent struct {
	// Method is how the agent updates: "install" runs update.sh in an
	// install.sh checkout, "dokploy" moves the image tag through Dokploy's
	// API and redeploys.
	Method      string    `json:"method"`
	InstalledAt time.Time `json:"installed_at"`
}

// Request is one update the panel asked for.
type Request struct {
	ID          string    `json:"id"`
	Target      string    `json:"target"`
	RequestedAt time.Time `json:"requested_at"`
	RequestedBy string    `json:"requested_by,omitempty"`
}

// Status is the agent's account of an update.
type Status struct {
	ID        string    `json:"id"`
	Target    string    `json:"target"`
	State     string    `json:"state"`
	Phase     string    `json:"phase,omitempty"`
	Message   string    `json:"message,omitempty"`
	Log       []string  `json:"log,omitempty"`
	StartedAt time.Time `json:"started_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// The states an update goes through. Queued is the panel's own: the request is
// written and the agent has not picked it up yet.
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateSucceeded = "succeeded"
	StateFailed    = "failed"
)

// staleRequest is how long a request may sit unclaimed before the panel stops
// waiting for it: an agent that has not answered in that time is not running.
const staleRequest = 2 * time.Minute

var (
	// ErrUnavailable means no agent is installed on this host.
	ErrUnavailable = errors.New("no update agent is installed on this server")
	// ErrBusy means an update is already under way.
	ErrBusy = errors.New("an update is already under way")
	// ErrBadTarget means the version is not one the agent would accept.
	ErrBadTarget = errors.New("that is not a release version")
)

// versionPattern is the same pattern the agent applies before it trusts the
// target: a semantic version, nothing that could be read as a path or a flag.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z]+(\.[0-9A-Za-z]+)*)?$`)

// Updater reads and writes the shared directory.
type Updater struct {
	Dir string
	now func() time.Time
}

func New(dir string) *Updater {
	return &Updater{Dir: dir, now: time.Now}
}

// Agent reports the installed agent, or ErrUnavailable.
func (u *Updater) Agent() (Agent, error) {
	var agent Agent
	if u == nil || u.Dir == "" {
		return agent, ErrUnavailable
	}
	if err := readJSON(filepath.Join(u.Dir, "agent.json"), &agent); err != nil {
		return agent, ErrUnavailable
	}
	if agent.Method == "" {
		return agent, ErrUnavailable
	}
	return agent, nil
}

// Available reports whether the panel can offer the button at all.
func (u *Updater) Available() bool {
	_, err := u.Agent()
	return err == nil
}

// Current returns the update in progress or the last one that ran. A request
// the agent has not claimed yet is reported as queued, and one it never
// claimed as failed, so the page never waits forever on an agent that died.
func (u *Updater) Current() (Status, bool) {
	var status Status
	statusErr := readJSON(filepath.Join(u.Dir, "status.json"), &status)
	var request Request
	if readJSON(filepath.Join(u.Dir, "request.json"), &request) == nil && (statusErr != nil || status.ID != request.ID) {
		queued := Status{ID: request.ID, Target: request.Target, State: StateQueued, UpdatedAt: request.RequestedAt}
		if u.now().Sub(request.RequestedAt) > staleRequest {
			queued.State = StateFailed
			queued.Message = "O agente de atualização do servidor não pegou o pedido. Confira com: systemctl status whatsapp-mcp-updater.path"
		}
		return queued, true
	}
	if statusErr != nil {
		return Status{}, false
	}
	return status, true
}

// Busy reports whether an update is queued or running.
func (u *Updater) Busy() bool {
	status, ok := u.Current()
	return ok && (status.State == StateQueued || status.State == StateRunning)
}

// Request asks the agent to move to target.
func (u *Updater) Request(target, by string) (Request, error) {
	if _, err := u.Agent(); err != nil {
		return Request{}, err
	}
	if !versionPattern.MatchString(target) {
		return Request{}, ErrBadTarget
	}
	if u.Busy() {
		return Request{}, ErrBusy
	}
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return Request{}, err
	}
	request := Request{ID: hex.EncodeToString(raw), Target: target, RequestedAt: u.now().UTC(), RequestedBy: by}
	if err := writeJSON(filepath.Join(u.Dir, "request.json"), request); err != nil {
		return Request{}, fmt.Errorf("could not write the update request: %w", err)
	}
	return request, nil
}

func readJSON(path string, value any) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, value)
}

// writeJSON writes through a temporary file and a rename, so the agent never
// reads a half-written request.
func writeJSON(path string, value any) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, body, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
