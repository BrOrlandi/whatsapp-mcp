package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr         string
	PublicURL          string
	EvolutionURL       string
	EvolutionAPIKey    string
	EvolutionTimeout   time.Duration
	RabbitURL          string
	RabbitQueues       []string
	DatabaseURL        string
	FreshnessWindow    time.Duration
	StatusPollInterval time.Duration
	StdioEnabled       bool
	// SetupToken guards the first-run form. An installer that publishes a URL
	// to the internet cannot leave "create the administrator" open to whoever
	// reaches it first, so it generates a token and prints it; the form refuses
	// to create anything without it. Empty means unguarded, which is the right
	// default for a panel bound to loopback.
	SetupToken string
}

func Load() Config {
	return Config{
		ListenAddr: env("LISTEN_ADDR", ":8080"),
		// The address clients reach this gateway at. It is not a secret; it is
		// what the panel prints in the ready-to-paste client configuration.
		PublicURL:          env("PUBLIC_URL", "http://127.0.0.1:8080"),
		EvolutionURL:       env("EVOLUTION_URL", "http://evolution-go:4000"),
		EvolutionAPIKey:    os.Getenv("EVOLUTION_API_KEY"),
		EvolutionTimeout:   duration("EVOLUTION_TIMEOUT", 5*time.Second),
		RabbitURL:          env("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		RabbitQueues:       list("RABBITMQ_QUEUES"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		FreshnessWindow:    duration("FRESHNESS_WINDOW", 5*time.Minute),
		StatusPollInterval: duration("STATUS_POLL_INTERVAL", 15*time.Second),
		// The stdio transport is a local development convenience. The supported
		// path is the authenticated HTTP endpoint, so stdio stays off unless asked
		// for: a server process reading stdin has no client on the other end.
		StdioEnabled: os.Getenv("MCP_STDIO") == "true",
		SetupToken:   os.Getenv("SETUP_TOKEN"),
	}
}

// list reads a comma-separated override. An empty value means "use the
// built-in set", which is what keeps the queue list in one place.
func list(key string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []string
	for _, item := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if seconds, err := strconv.Atoi(value); err == nil {
			return time.Duration(seconds) * time.Second
		}
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}
