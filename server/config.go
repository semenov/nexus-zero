package main

import "os"

// Config holds runtime configuration loaded from environment variables.
type Config struct {
	DatabaseURL string
	Port        string
}

// Load reads configuration from environment variables, applying defaults where
// no value is present.
func Load() Config {
	cfg := Config{
		DatabaseURL: "postgres://localhost/messenger",
		Port:        "8080",
	}

	if v := os.Getenv("DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}

	return cfg
}
