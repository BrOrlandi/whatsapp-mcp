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
	"github.com/BrOrlandi/whatsapp-mcp/internal/rabbit"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.Load()
	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	state := health.NewState()
	state.SetDatabase(true)

	evolutionClient := evolution.New(cfg.EvolutionURL, cfg.EvolutionAPIKey, cfg.EvolutionTimeout)
	go pollEvolution(ctx, evolutionClient, state, cfg.StatusPollInterval, logger)
	go pollDatabase(ctx, db, state)
	consumer := &rabbit.Consumer{URL: cfg.RabbitURL, Queue: cfg.RabbitQueue, Store: db, SetConnected: state.SetRabbit, OnPersisted: state.MarkEvent, Logger: logger}
	go func() {
		if err := consumer.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("RabbitMQ consumer stopped", "error", err)
		}
	}()
	go func() {
		if err := mcp.New(db, state, cfg.FreshnessWindow).Serve(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("MCP stdio stopped", "error", err)
		}
	}()

	httpServer := &http.Server{Addr: cfg.ListenAddr, Handler: httpapi.Handler(state, cfg.FreshnessWindow), ReadHeaderTimeout: 5 * time.Second}
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

type statusClient interface {
	Status(context.Context) (bool, string, error)
}

func pollEvolution(ctx context.Context, client statusClient, state *health.State, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		connected, _, err := client.Status(ctx)
		state.SetEvolution(err == nil && connected)
		if err != nil && ctx.Err() == nil {
			logger.Warn("Evolution status poll failed", "error", err)
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
