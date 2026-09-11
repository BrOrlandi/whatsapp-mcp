package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	ListenAddr         string
	EvolutionURL       string
	EvolutionAPIKey    string
	EvolutionTimeout   time.Duration
	RabbitURL          string
	RabbitQueue        string
	DatabaseURL        string
	FreshnessWindow    time.Duration
	StatusPollInterval time.Duration
}

func Load() Config {
	return Config{
		ListenAddr:         env("LISTEN_ADDR", ":8080"),
		EvolutionURL:       env("EVOLUTION_URL", "http://evolution-go:4000"),
		EvolutionAPIKey:    os.Getenv("EVOLUTION_API_KEY"),
		EvolutionTimeout:   duration("EVOLUTION_TIMEOUT", 5*time.Second),
		RabbitURL:          env("RABBITMQ_URL", "amqp://guest:guest@rabbitmq:5672/"),
		RabbitQueue:        env("RABBITMQ_QUEUE", "message"),
		DatabaseURL:        os.Getenv("DATABASE_URL"),
		FreshnessWindow:    duration("FRESHNESS_WINDOW", 5*time.Minute),
		StatusPollInterval: duration("STATUS_POLL_INTERVAL", 15*time.Second),
	}
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
