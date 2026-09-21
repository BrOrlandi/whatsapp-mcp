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
	// LicenseAuto registers the Evolution licence with an address on
	// LicenseEmailDomain, whose mail is clicked by the licence Email Worker —
	// activation then needs no person at all. False keeps the operator's own
	// email and their own click on the magic link.
	LicenseAuto        bool
	LicenseEmailDomain string
	// LicenseAutoWait is how long the automatic registration is given before
	// the wizard stops claiming a licence is on its way and offers the manual
	// flow instead. The worker's click normally lands in seconds, so a wait
	// this long past it means something between here and that inbox is broken
	// — a routing rule, the worker, the licensing server's delivery — and none
	// of those get better by waiting longer.
	LicenseAutoWait time.Duration
	// UpdateCheck lets the panel ask GitHub whether a newer release exists, so
	// an operator finds out from the panel they already look at rather than
	// from noticing the repository moved. It is an outbound call from their
	// server, carrying nothing about the instance, and this is the one variable
	// that stops it.
	UpdateCheck bool
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
		// Automatic licence registration is the default: an install that can
		// reach its own email domain's worker comes up licensed with nobody
		// clicking anything. Opting out is a one-variable change.
		LicenseAuto:        boolEnv("EVOLUTION_LICENSE_AUTO", true),
		LicenseEmailDomain: env("EVOLUTION_LICENSE_EMAIL_DOMAIN", "brorlandi.xyz"),
		LicenseAutoWait:    duration("EVOLUTION_LICENSE_AUTO_WAIT", 3*time.Minute),
		UpdateCheck:        boolEnv("UPDATE_CHECK", true),
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

// boolEnv reads an on/off variable that defaults to on. "false" and "0" are
// the off states; anything else is on, which keeps a typo from silently
// disabling the licence automation and sending an operator to a form.
func boolEnv(key string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "false", "0":
		return false
	case "true", "1":
		return true
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
