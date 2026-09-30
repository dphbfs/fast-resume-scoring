// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the full runtime configuration.
type Config struct {
	Jev        Jev
	Generative Generative
	Pipeline   Pipeline
}

// Jev configures the TypeSafe classifier client.
type Jev struct {
	APIKey         string        // TYPESAFE_API_KEY (required)
	BaseURL        string        // TYPESAFE_BASE_URL
	Model          string        // JEV_MODEL
	MaxConcurrency int           // JEV_MAX_CONCURRENCY
	MaxRetries     int           // JEV_MAX_RETRIES
	Timeout        time.Duration // JEV_TIMEOUT
}

// Generative configures the optional OpenAI-compatible client. An empty
// Model means "not configured".
type Generative struct {
	BaseURL        string        // OPENAI_BASE_URL
	APIKey         string        // OPENAI_API_KEY
	Model          string        // OPENAI_MODEL (no default)
	MaxConcurrency int           // GEN_MAX_CONCURRENCY
	Timeout        time.Duration // GEN_TIMEOUT
}

// Pipeline tunes the Requirement Extractor.
type Pipeline struct {
	MaxWindowWords int // PIPELINE_MAX_WINDOW_WORDS
}

// Load reads Config from the environment, applying defaults.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	e := env{getenv: getenv}
	cfg := Config{
		Jev: Jev{
			APIKey:         getenv("TYPESAFE_API_KEY"),
			BaseURL:        e.str("TYPESAFE_BASE_URL", "https://api.typesafe.ai"),
			Model:          e.str("JEV_MODEL", "jev-latest"),
			MaxConcurrency: e.int("JEV_MAX_CONCURRENCY", 8),
			MaxRetries:     e.int("JEV_MAX_RETRIES", 4),
			Timeout:        e.duration("JEV_TIMEOUT", 30*time.Second),
		},
		Generative: Generative{
			BaseURL:        e.str("OPENAI_BASE_URL", "https://api.openai.com/v1"),
			APIKey:         getenv("OPENAI_API_KEY"),
			Model:          getenv("OPENAI_MODEL"),
			MaxConcurrency: e.int("GEN_MAX_CONCURRENCY", 1),
			Timeout:        e.duration("GEN_TIMEOUT", 60*time.Second),
		},
		Pipeline: Pipeline{
			MaxWindowWords: e.int("PIPELINE_MAX_WINDOW_WORDS", 4),
		},
	}
	if e.err != nil {
		return Config{}, e.err
	}
	if cfg.Jev.APIKey == "" {
		return Config{}, fmt.Errorf("config: TYPESAFE_API_KEY is required")
	}
	return cfg, nil
}

// env reads typed values and keeps the first parse error.
type env struct {
	getenv func(string) string
	err    error
}

func (e *env) str(key, def string) string {
	if v := e.getenv(key); v != "" {
		return v
	}
	return def
}

func (e *env) int(key string, def int) int {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return n
}

func (e *env) duration(key string, def time.Duration) time.Duration {
	v := e.getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil && e.err == nil {
		e.err = fmt.Errorf("config: %s: %w", key, err)
	}
	return d
}
