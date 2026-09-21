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
)

// version is stamped at build time with -ldflags "-X main.version=...". It is
// "dev" for a local build, which is the honest answer for one.
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	logger.Info("starting whatsapp-mcp", "version", version)
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
	mcpServer := mcp.New(db, evolutionClient, state, cfg.FreshnessWindow)
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
	webHandler := httpapi.NewWebHandler(db, evolutionClient, state, sessionKey, cfg.PublicURL, cfg.SetupToken, cfg.LicenseAuto, cfg.LicenseEmailDomain, cfg.LicenseAutoWait)
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

// instanceLister is the slice of Evolution the readiness poll needs.
type instanceLister interface {
	FetchInstances(context.Context) ([]evolution.Instance, error)
}

// selectionReader is the slice of the store the readiness poll needs.
type selectionReader interface {
	SelectedInstance(context.Context) (string, error)
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
	for {
		connected := false
		selected, err := selection.SelectedInstance(ctx)
		if err == nil && selected != "" {
			instances, fetchErr := client.FetchInstances(ctx)
			err = fetchErr
			for _, instance := range instances {
				if instance.ID == selected {
					connected = instance.Status == evolution.StatusConnected
					break
				}
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
