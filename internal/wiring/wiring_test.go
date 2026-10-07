package wiring

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev"
	"github.com/dphbfs/fast-resume-scoring/internal/adapter/jev/replay"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/config"
	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
)

func TestNewClassifierPicksTheConfiguredClient(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	recording := filepath.Join(dir, "jev.json")
	if err := os.WriteFile(recording, []byte(`{"version": 1, "exchanges": {}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	base := config.DefaultConfig().Jev

	live := base
	live.APIKey = "k"
	c, err := NewClassifier(live, metrics.NewRecorder(), log)
	if _, ok := c.(*jev.Client); err != nil || !ok {
		t.Errorf("live: got %T, %v; want *jev.Client", c, err)
	}

	record := live
	record.RecordFile = filepath.Join(dir, "out.json")
	c, err = NewClassifier(record, metrics.NewRecorder(), log)
	if _, ok := c.(*replay.Recorder); err != nil || !ok {
		t.Errorf("record: got %T, %v; want *replay.Recorder", c, err)
	}

	play := base
	play.ReplayFile = recording
	c, err = NewClassifier(play, metrics.NewRecorder(), log)
	if _, ok := c.(*replay.Player); err != nil || !ok {
		t.Errorf("replay: got %T, %v; want *replay.Player", c, err)
	}
}
