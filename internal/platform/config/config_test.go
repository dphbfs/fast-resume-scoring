package config

import (
	"testing"
	"time"
)

func mapEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(mapEnv(map[string]string{"TYPESAFE_API_KEY": "k"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jev.Model != "jev-latest" || cfg.Jev.MaxConcurrency != 8 || cfg.Jev.Timeout != 30*time.Second {
		t.Errorf("unexpected Jev defaults: %+v", cfg.Jev)
	}
	if cfg.Generative.Model != "" || cfg.Generative.MaxConcurrency != 1 {
		t.Errorf("unexpected Generative defaults: %+v", cfg.Generative)
	}
	if cfg.Pipeline.MaxWindowWords != 4 {
		t.Errorf("MaxWindowWords = %d, want 4", cfg.Pipeline.MaxWindowWords)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"missing api key", map[string]string{}},
		{"bad int", map[string]string{"TYPESAFE_API_KEY": "k", "JEV_MAX_CONCURRENCY": "many"}},
		{"bad duration", map[string]string{"TYPESAFE_API_KEY": "k", "JEV_TIMEOUT": "soon"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(mapEnv(tt.env)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}
