package config

import (
	"reflect"
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
	if cfg.Pipeline.MaxWindowWords != 4 || cfg.Pipeline.SectionBatchSize != 60 || cfg.Pipeline.MinRequirementMass != 0.7 {
		t.Errorf("unexpected Pipeline defaults: %+v", cfg.Pipeline)
	}
	if !reflect.DeepEqual(cfg.Checker, Checker{RetrievalK: 8, RetrievalFloor: 0.01, MinEvidenceMass: 0.5,
		RetrievalMode: "narrow", NarrowSizes: []int{16, 8}, NoulThreshold: 0.5, StrengthCriteria: "v5", GateThreshold: 0.5, GateWording: "v2", StrengthMode: "choice"}) {
		t.Errorf("unexpected Checker defaults: %+v", cfg.Checker)
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
		{"bad float", map[string]string{"TYPESAFE_API_KEY": "k", "PIPELINE_MIN_REQUIREMENT_MASS": "half"}},
		{"bad ints", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_NARROW_SIZES": "12,x"}},
		{"bad mode", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_RETRIEVAL_MODE": "fuzzy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(mapEnv(tt.env)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestLoadScoreModeNeedsGate(t *testing.T) {
	env := map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_STRENGTH_MODE": "score", "CHECKER_GATE_THRESHOLD": "0"}
	if _, err := load(mapEnv(env)); err == nil {
		t.Fatal("score mode without the gate: want error")
	}
}
