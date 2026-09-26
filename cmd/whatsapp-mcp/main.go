package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/config"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/httpapi"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcphttp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/rabbit"
	"github.com/BrOrlandi/whatsapp-mcp/internal/repair"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
	"github.com/BrOrlandi/whatsapp-mcp/internal/transcribe"
	"github.com/BrOrlandi/whatsapp-mcp/internal/version"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	logger.Info("starting whatsapp-mcp", "version", version.String())

	// The panel tells the operator when their instance is behind. This is the
	// only call this process makes to anything that is not theirs, it carries
	// nothing about them, and UPDATE_CHECK=false is all it takes to stop it.
	version.StartUpdateCheck(ctx, cfg.UpdateCheck, logger)
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	state := health.NewState()
	state.SetDatabase(true)

	// The message index is a projection of the stored events, so a decoder fix
	// repairs the past instead of leaving it unreadable. This checks on every
	// start and does nothing when the projection is already current.
	go repair.RunIfStale(ctx, db, func(report repair.Report, running bool) {
		state.SetReprojection(running, report.Events, report.Messages, report.OrphansAfter)
	}, logger)

	evolutionClient := evolution.New(cfg.EvolutionURL, cfg.EvolutionAPIKey, cfg.EvolutionTimeout)
	sessionKey, err := httpapi.NewSessionKey()
	if err != nil {
		logger.Error("generate session key", "error", err)
		os.Exit(1)
	}
	go pollEvolution(ctx, evolutionClient, db, state, cfg.StatusPollInterval, cfg.FreshnessWindow, logger)
	go pollDatabase(ctx, db, state)
	consumer := &rabbit.Consumer{URL: cfg.RabbitURL, Queues: cfg.RabbitQueues, Store: db, State: state, Logger: logger}
	go func() {
		if err := consumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("RabbitMQ consumer stopped", "error", err)
		}
	}()
	transcriber := transcribe.New(db)
	mcpServer := mcp.New(db, evolutionClient, state, cfg.FreshnessWindow).WithTranscriber(transcriber, cfg.PublicURL)
	if cfg.StdioEnabled {
		go func() {
			if err := mcpServer.Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("MCP stdio stopped", "error", err)
			}
		}()
	}

	// The licence automation is on by default: registrations go to an address
	// the email worker answers, and the wizard waits on the licence coming in
	// rather than on a person's inbox.
	webHandler := httpapi.NewWebHandler(db, evolutionClient, state, sessionKey, cfg.PublicURL, cfg.SetupToken, cfg.LicenseAuto, cfg.LicenseEmailDomain, cfg.LicenseAutoWait, transcriber)
	remoteMCP := mcphttp.New(mcpServer, apiKeyAuth{db}, logger)
	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           httpapi.FullHandler(state, cfg.FreshnessWindow, webHandler, remoteMCP),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      90 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	logger.Info("WhatsApp MCP gateway started", "listen_addr", cfg.ListenAddr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server stopped", "error", err)
		os.Exit(1)
	}
}

// probeInterval bounds how often the liveness probe reaches WhatsApp. It only
// runs while Evolution claims the session is down and nothing is arriving, and
// one round trip a minute is enough to keep a status page honest without
// hammering anyone.
const probeInterval = time.Minute

// probeSession asks WhatsApp whether a number has an account, using the
// instance's own. The answer is thrown away: what matters is that answering it
// requires the WhatsApp client to be connected, so a reply is proof of a live
// session and an error is not proof of anything either way.
//
// It needs the instance's token and its own number, both of which the listing
// carries. Without either there is nothing to ask, and the poll stands.
func probeSession(ctx context.Context, client instanceLister, instance evolution.Instance) bool {
	if instance.Token == "" || instance.Number == "" {
		return false
	}
	found, err := client.CheckNumbers(ctx, instance.Token, []string{instance.Number})
	return err == nil && len(found) > 0
}

// seedLastEvent recovers the last-event time from the message index, so the
// readiness rules have the same evidence after a restart that they had before
// it. A failure here is not worth refusing to start over: it only means the
// first minutes are judged by the poll alone, which is where this began.
func seedLastEvent(ctx context.Context, selection selectionReader, state *health.State, logger *slog.Logger) {
	selected, err := selection.SelectedInstance(ctx)
	if err != nil || selected == "" {
		return
	}
	coverage, err := selection.Coverage(ctx, selected)
	if err != nil {
		logger.Warn("could not read the index to recover the last event time", "error", err)
		return
	}
	if coverage.NewestAt.IsZero() {
		return
	}
	state.MarkEvent(coverage.NewestAt)
	state.MarkMessage(coverage.NewestAt)
	logger.Info("recovered the last event time from the index", "at", coverage.NewestAt)
}

// instanceLister is the slice of Evolution the readiness poll needs.
// CheckNumbers is in it as a liveness probe rather than for its answer: it is
// a round trip to WhatsApp's own servers, so a reply of any kind proves the
// session is up. See probeSession.
type instanceLister interface {
	FetchInstances(context.Context) ([]evolution.Instance, error)
	CheckNumbers(context.Context, string, []string) ([]evolution.Presence, error)
}

// selectionReader is the slice of the store the readiness poll needs. Coverage
// is in it because the index is what the gateway remembers across a restart:
// the process forgets when the last event arrived, its own database does not.
type selectionReader interface {
	SelectedInstance(context.Context) (string, error)
	Coverage(context.Context, string) (store.Coverage, error)
}

// pollEvolution derives readiness from the instance the panel actually
// selected. Evolution resolves the target instance from the key on the request,
// so a global status call answers for no instance in particular; listing the
// instances and looking up the selected one is the only reading that matches
// what the operator chose.
//
// Connection events are the primary signal and arrive on their own queues; this
// poll exists to recover the truth after a restart and to notice a silent drop.
func pollEvolution(ctx context.Context, client instanceLister, selection selectionReader, state *health.State, interval, freshness time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	// Start from what the index already knows. A restart wipes the in-memory
	// notion of "last event", and the first poll then has nothing to weigh
	// against Evolution's own instance record — so a deploy landing in a quiet
	// minute announced a disconnected WhatsApp on a session that had delivered
	// a message seconds earlier. The newest indexed message is that evidence,
	// and it survives the restart because it is in Postgres.
	seedLastEvent(ctx, selection, state, logger)
	var lastProbe time.Time
	for {
		connected := false
		var probe evolution.Instance
		selected, err := selection.SelectedInstance(ctx)
		if err == nil && selected != "" {
			instances, fetchErr := client.FetchInstances(ctx)
			err = fetchErr
			for _, instance := range instances {
				if instance.ID == selected {
					connected = instance.Status == evolution.StatusConnected
					probe = instance
					break
				}
			}
		}
		// Evolution's instance record has been seen holding "disconnected"
		// across a reconnect, through a burst of two hundred messages a
		// minute, for as long as the process lived. On a quiet account there
		// is no traffic to contradict it, so the panel would report a dead
		// session indefinitely — which is what it did. Before believing it,
		// ask WhatsApp something.
		if err == nil && !connected && !state.Snapshot().Receiving(freshness) && time.Since(lastProbe) > probeInterval {
			lastProbe = time.Now()
			if probeSession(ctx, client, probe) {
				logger.Warn("Evolution reports the instance disconnected, but WhatsApp answered a live query through it; treating the session as up and its record as stale")
				connected = true
			}
		}
		if err == nil {
			// The window is the freshness window: if the index is being kept
			// current, the session feeding it is up, whatever Evolution's own
			// record says about it.
			if state.ObserveInstance(connected, freshness) {
				logger.Warn("Evolution reports the instance disconnected while its messages keep arriving; its instance record is stale until something reconnects it")
			}
		} else if ctx.Err() == nil {
			state.SetEvolution(false)
			logger.Warn("Evolution readiness poll failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func pollDatabase(ctx context.Context, db *store.Store, state *health.State) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		state.SetDatabase(db.Healthy(ctx))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// apiKeyAuth authenticates an MCP client against the stored API keys and
// records the use, so the panel can show a credential nobody uses any more.
type apiKeyAuth struct{ store *store.Store }

func (a apiKeyAuth) Authenticate(ctx context.Context, secret string) (string, error) {
	key, err := a.store.ResolveAPIKey(ctx, secret)
	if err != nil {
		return "", err
	}
	go func() {
		touchCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.store.TouchAPIKey(touchCtx, key.ID)
	}()
	return key.InstanceID, nil
}

// NoteClient records which AI tool is holding this credential, as the tool
// itself reported in the MCP handshake. The panel shows it in place of a key
// prefix, which is the difference between "Claude Desktop" and "wamcp-a1b2c3…".
func (a apiKeyAuth) NoteClient(ctx context.Context, secret, name, version string) {
	_ = a.store.NoteAPIKeyClient(ctx, secret, name, version)
}
