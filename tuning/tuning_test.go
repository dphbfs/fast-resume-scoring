package tuning

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dphbfs/fast-resume-tailoring/internal/domain"
	"github.com/dphbfs/fast-resume-tailoring/internal/platform/config"
)

func TestDefault(t *testing.T) {
	d := Default()
	if d.Source != "embedded" || len(d.Hash) != 12 {
		t.Errorf("source %q, hash %q", d.Source, d.Hash)
	}
	if w := d.MatchWeights(); w != (domain.MatchWeights{Intercept: 80.7, RoleMatch: 18.5, ExperienceShort: -55.2}) {
		t.Errorf("match weights = %+v", w)
	}
	fw := d.FitWeights()
	if fw.Credit[domain.StrengthPartial] != 0.6 || fw.TierWeight[domain.TierRequired] != 3 {
		t.Errorf("fit weights = %+v", fw)
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tuning.yaml")
	edited := strings.Replace(string(embedded), "intercept: 80.7", "intercept: 75", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(config.Tuning{File: path})
	if err != nil {
		t.Fatal(err)
	}
	if got.MatchScore.Intercept != 75 || got.Source != path || got.Hash == Default().Hash {
		t.Errorf("loaded = intercept %v, source %q, hash %q", got.MatchScore.Intercept, got.Source, got.Hash)
	}
	if def, err := Load(config.Tuning{}); err != nil || def.Source != "embedded" {
		t.Errorf("no file: %v, %v", def, err)
	}
}

func TestLoadRejects(t *testing.T) {
	for _, tt := range []struct {
		name, old, new, want string
	}{
		{"misspelled key", "  intercept: 80.7", "  intercpt: 80.7", "field intercpt not found"},
		{"empty text", "    if_true: The candidate's relevant experience or level is clearly below what the job asks for.",
			"    if_true: \"\"", "holistic.experience_short.if_true is empty"},
		{"missing fixed section", "      benefits: >-", "      perks: >-", `missing fixed option "benefits"`},
		{"zero tier weight", "    required: 3", "    required: 0", "fit_score.tier_weight.required: must be positive"},
		{"unknown credit", "    weak: 0.3", "    weak: 0.3\n    meh: 0.1", `fit_score.credit: unknown key "meh"`},
		{"wrong version", "version: 1", "version: 2", "version 2, want 1"},
	} {
		edited := strings.Replace(string(embedded), tt.old, tt.new, 1)
		if edited == string(embedded) {
			t.Fatalf("%s: %q not found in tuning.yaml", tt.name, tt.old)
		}
		path := filepath.Join(t.TempDir(), "tuning.yaml")
		if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(config.Tuning{File: path}); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}
