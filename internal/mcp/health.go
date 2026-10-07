package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

// Check is one verdict of the health report.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok, warn or fail
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

// Health is the answer to "is this working?": one overall verdict and the
// checks it was made from, each with what to do when it is not ok.
type Health struct {
	Status   string         `json:"status"`
	Summary  string         `json:"summary"`
	Checks   []Check        `json:"checks"`
	Activity store.Activity `json:"activity"`
	At       time.Time      `json:"checked_at"`
}

// healthInputs is everything the verdict depends on, gathered first so the
// judgement itself is a pure function that can be tested.
type healthInputs struct {
	Now         time.Time
	Uptime      time.Duration
	Snapshot    health.Snapshot
	HasSession  bool
	SessionErr  error
	Activity    store.Activity
	ActivityErr error
	Gaps        []store.Gap
	MaxSilence  time.Duration
	Panel       string
}

const defaultMaxSilence = 6 * time.Hour

func (s *Server) health(ctx context.Context, session Session, a arguments) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	now := time.Now()
	in := healthInputs{Now: now, Uptime: now.Sub(s.started), Snapshot: s.state.Snapshot(), HasSession: session.InstanceID != "",
		MaxSilence: time.Duration(a.MaxSilenceHours * float64(time.Hour)), Panel: s.panelBase()}
	if in.HasSession {
		in.Activity, in.ActivityErr = s.index.Activity(ctx, session.InstanceID, now)
		if gaps, err := s.index.IndexGaps(ctx, session.InstanceID, store.Silence, 20); err == nil {
			for _, g := range gaps {
				if g.Until.After(now.Add(-7 * 24 * time.Hour)) {
					in.Gaps = append(in.Gaps, g)
				}
			}
		}
	} else {
		_, in.SessionErr = s.Session(ctx)
	}
	return textResult(evaluate(in), false)
}

func evaluate(in healthInputs) Health {
	if in.MaxSilence <= 0 {
		in.MaxSilence = defaultMaxSilence
	}
	h := Health{At: in.Now.UTC(), Activity: in.Activity}
	add := func(name, status, detail, fix string) {
		h.Checks = append(h.Checks, Check{Name: name, Status: status, Detail: detail, Fix: fix})
	}
	snap := in.Snapshot

	add("gateway", "ok", fmt.Sprintf("answering, version %s, up for %s", version.String(), round(in.Uptime)), "")
	if snap.DatabaseConnected {
		add("database", "ok", "the message index is reachable", "")
	} else {
		add("database", "fail", "the gateway's PostgreSQL is unreachable", "check the postgres-mcp container on the server (docker compose ps)")
	}
	if snap.RabbitConnected {
		add("queue", "ok", "consuming Evolution's events from RabbitMQ", "")
	} else {
		add("queue", "fail", "the RabbitMQ queue is unreachable, so no new message is indexed", "check the rabbitmq container on the server")
	}
	if snap.EvolutionConnected {
		add("evolution", "ok", "Evolution Go answers and reports the instance connected", "")
	} else {
		add("evolution", "warn", "Evolution Go does not report the instance as connected", "open the control panel's WhatsApp tab: "+in.Panel+"/whatsapp")
	}

	switch {
	case !in.HasSession:
		detail := "no WhatsApp instance is selected"
		if in.SessionErr != nil {
			detail = in.SessionErr.Error()
		}
		add("paired", "fail", detail, "connect WhatsApp in the control panel: "+in.Panel+"/whatsapp")
	case snap.WhatsApp.State == "logged_out":
		add("paired", "fail", "WhatsApp logged this device out", "pair again in the control panel's WhatsApp tab")
	case snap.WhatsApp.State == "banned":
		add("paired", "fail", "WhatsApp temporarily banned this number: "+snap.WhatsApp.Reason, "wait for the ban to lift; sending in bulk is what usually causes it")
	case snap.WhatsApp.State == "connected" || snap.EvolutionConnected:
		add("paired", "ok", "WhatsApp is connected as a linked device", "")
	default:
		add("paired", "warn", "WhatsApp's last reported state is "+snap.WhatsApp.State, "if it lasts, reconnect in the control panel's WhatsApp tab")
	}

	a := in.Activity
	switch {
	case !in.HasSession:
	case in.ActivityErr != nil:
		add("receiving", "fail", in.ActivityErr.Error(), "check the database")
	case a.NewestIncoming == nil:
		add("receiving", "warn", "no message from anyone has been indexed yet", "the first sync after pairing may still be running; check again in a few minutes")
	default:
		age := in.Now.Sub(*a.NewestIncoming)
		detail := fmt.Sprintf("last message received %s ago (%s UTC); %d messages in the last hour, %d in 24 hours",
			round(age), a.NewestIncoming.UTC().Format("2006-01-02 15:04"), a.LastHour, a.LastDay)
		switch {
		case age <= in.MaxSilence:
			add("receiving", "ok", detail, "")
		case age <= 24*time.Hour:
			add("receiving", "warn", detail, "quiet for longer than usual: normal overnight or in a quiet account; to be sure, have someone send this account a message and check again")
		default:
			add("receiving", "fail", detail, "a whole day without any incoming message usually means the device is not receiving: check the WhatsApp tab of the panel, and that the phone is online")
		}
	}

	if len(in.Gaps) > 0 {
		longest := in.Gaps[0]
		for _, g := range in.Gaps {
			if g.Until.Sub(g.Since) > longest.Until.Sub(longest.Since) {
				longest = g
			}
		}
		add("coverage", "warn", fmt.Sprintf("%d window(s) in the last 7 days with no message in any conversation; the longest is %.0f hours from %s UTC",
			len(in.Gaps), longest.Until.Sub(longest.Since).Hours(), longest.Since.UTC().Format("2006-01-02 15:04")),
			"an outage of the gateway leaves these; sync_history with before set to the end of a window tries to refill it, and whatsapp_status lists them")
	} else if a.NewestAny != nil {
		add("coverage", "ok", "no silent windows in the last 7 days", "")
	}

	h.Status = "ok"
	var failing, warning []string
	for _, c := range h.Checks {
		switch c.Status {
		case "fail":
			failing = append(failing, c.Name)
		case "warn":
			warning = append(warning, c.Name)
		}
	}
	switch {
	case len(failing) > 0:
		h.Status = "fail"
		h.Summary = fmt.Sprintf("not working: %v", failing)
	case len(warning) > 0:
		h.Status = "warn"
		h.Summary = fmt.Sprintf("working, with warnings: %v", warning)
	default:
		h.Summary = "working: connected to WhatsApp and receiving messages"
	}
	return h
}

func round(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}
