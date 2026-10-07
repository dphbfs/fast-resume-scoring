package replay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dphbfs/fast-resume-scoring/internal/platform/metrics"
	"github.com/dphbfs/fast-resume-scoring/internal/port"
)

// fakeClassifier answers every request with P(yes) = 0.7 and counts calls.
type fakeClassifier struct {
	calls int
	err   error
}

func (f *fakeClassifier) Classify(_ context.Context, req port.ClassifyRequest) (port.ClassifyResponse, error) {
	f.calls++
	if f.err != nil {
		return port.ClassifyResponse{}, f.err
	}
	p := 0.7
	answers := map[string]port.Answer{}
	for id := range req.Questions {
		answers[id] = port.Answer{Type: port.Noul, Noul: &p}
	}
	return port.ClassifyResponse{Model: "jev-test", Answers: answers, Usage: port.Usage{InputTokens: 42}}, nil
}

func request(state string) port.ClassifyRequest {
	return port.ClassifyRequest{
		State: map[string]any{"text": state},
		Questions: map[string]port.Question{
			"q1": {Type: port.Noul, Instructions: "is it Go?"},
			"q2": {Type: port.Choice, Instructions: "which?", Criteria: map[string]any{"a": "A", "b": nil}},
		},
	}
}

func TestKeyIsStableAndContentSensitive(t *testing.T) {
	t.Parallel()
	k1, err := Key(request("Go"))
	if err != nil {
		t.Fatal(err)
	}
	for range 20 { // map iteration order must not matter
		k, err := Key(request("Go"))
		if err != nil {
			t.Fatal(err)
		}
		if k != k1 {
			t.Fatalf("key changed between equal requests: %s vs %s", k, k1)
		}
	}
	k2, err := Key(request("Rust"))
	if err != nil {
		t.Fatal(err)
	}
	if k2 == k1 {
		t.Fatal("different requests got the same key")
	}
}

func TestRecordThenReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rec", "jev.json")

	live := &fakeClassifier{}
	rec, err := NewRecorder(live, path)
	if err != nil {
		t.Fatal(err)
	}
	want, err := rec.Classify(ctx, request("Go"))
	if err != nil {
		t.Fatal(err)
	}

	// A second recorder extends the same file.
	rec2, err := NewRecorder(live, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec2.Classify(ctx, request("Kafka")); err != nil {
		t.Fatal(err)
	}
	if live.calls != 2 {
		t.Fatalf("real calls = %d, want 2", live.calls)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("recording mode = %o, want 600", perm)
	}

	m := metrics.NewRecorder()
	player, err := NewPlayer(path, m)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"Go", "Kafka"} {
		got, err := player.Classify(ctx, request(state))
		if err != nil {
			t.Fatalf("replay %s: %v", state, err)
		}
		if got.Model != want.Model || *got.Answers["q1"].Noul != 0.7 || got.Usage.InputTokens != 42 {
			t.Errorf("replay %s = %+v, want the recorded response", state, got)
		}
	}
	if _, err := player.Classify(ctx, request("Rust")); !errors.Is(err, ErrNotRecorded) {
		t.Errorf("unrecorded request: err = %v, want ErrNotRecorded", err)
	}
	if c := m.Summary().Counters; c["jev.replayed"] != 2 || c["jev.replay_misses"] != 1 {
		t.Errorf("counters = %v, want 2 replayed, 1 miss", c)
	}
}

func TestRecorderDoesNotSaveFailures(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "jev.json")
	rec, err := NewRecorder(&fakeClassifier{err: errors.New("boom")}, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Classify(context.Background(), request("Go")); err == nil {
		t.Fatal("want the wrapped client's error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("recording written after a failure: %v", err)
	}
}

func TestNewPlayerRejectsBadFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, content := range map[string]string{
		"not json":      "{",
		"wrong version": `{"version": 99, "exchanges": {}}`,
	} {
		path := filepath.Join(dir, name+".json")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewPlayer(path, metrics.NewRecorder()); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := NewPlayer(filepath.Join(dir, "missing.json"), metrics.NewRecorder()); err == nil {
		t.Error("missing file: want an error")
	}
}
