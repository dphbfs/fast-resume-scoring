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
	if cfg.Run.Deadline != 120*time.Second {
		t.Errorf("unexpected Run defaults: %+v", cfg.Run)
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
		{"zero run deadline", map[string]string{"TYPESAFE_API_KEY": "k", "RUN_DEADLINE": "0s"}},
		{"negative run deadline", map[string]string{"TYPESAFE_API_KEY": "k", "RUN_DEADLINE": "-5s"}},
		{"NaN probability", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_GATE_THRESHOLD": "NaN"}},
		{"probability above 1", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_RETRIEVAL_FLOOR": "1.5"}},
		{"zero concurrency", map[string]string{"TYPESAFE_API_KEY": "k", "JEV_MAX_CONCURRENCY": "0"}},
		{"zero retrieval K", map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_RETRIEVAL_K": "0"}},
		{"zero Jev timeout", map[string]string{"TYPESAFE_API_KEY": "k", "JEV_TIMEOUT": "0s"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := load(mapEnv(tt.env)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestLoadNarrowSizesNone(t *testing.T) {
	cfg, err := load(mapEnv(map[string]string{"TYPESAFE_API_KEY": "k", "CHECKER_NARROW_SIZES": "none"}))
	if err != nil || cfg.Checker.NarrowSizes == nil || len(cfg.Checker.NarrowSizes) != 0 {
		t.Errorf("NarrowSizes = %v, err %v; want empty", cfg.Checker.NarrowSizes, err)
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

func TestDefaultConfigIsValidAndMatchesLoad(t *testing.T) {
	d := DefaultConfig()
	if err := d.Validate(); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
	cfg, err := load(mapEnv(map[string]string{"TYPESAFE_API_KEY": "k"}))
	if err != nil {
		t.Fatal(err)
	}
	d.Jev.APIKey = "k"
	if !reflect.DeepEqual(cfg, d) {
		t.Errorf("load with no settings = %+v, want DefaultConfig %+v", cfg, d)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	c := DefaultConfig()
	c.Jev.MaxConcurrency, c.Checker.MinEvidenceMass = 0, -0.1
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "JEV_MAX_CONCURRENCY") || !strings.Contains(err.Error(), "CHECKER_MIN_EVIDENCE_MASS") {
		t.Errorf("err = %v, want both problems", err)
	}
}
