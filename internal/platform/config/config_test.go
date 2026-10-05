package config

import (
	"reflect"
	"strings"
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
		NarrowSizes: []int{16}, GateThreshold: 0.5, SkipCappedGrading: true, GateFirst: true}) {
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
		{"cost options without gate", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_GATE_THRESHOLD": "0"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(mapEnv(tt.env)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestLoadRejectsRemovedSettings(t *testing.T) {
	for _, name := range removedSettings {
		env := map[string]string{"TYPESAFE_API_KEY": "k", name: "x"}
		_, err := load(mapEnv(env))
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s set: err = %v, want an error naming it", name, err)
		}
	}
}
