package main

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// TestExampleReplays runs the command on examples/ against the committed
// Jev recording and expects the committed output. It fails when a prompt,
// the tuning file, or the pipeline changes what is sent to Jev: re-record
// the examples (examples/README.md).
func TestExampleReplays(t *testing.T) {
	t.Setenv("JEV_REPLAY", "../../examples/jev-recording.json")
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("TUNING_FILE", "")
	t.Setenv("LOG_LEVEL", "error")

	app, err := initApp()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := app.Run(context.Background(), []string{"-q", "-requirements", "../../examples/requirements.json", "-resume", "../../examples/resume.md"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	want, err := os.ReadFile("../../examples/coverage.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Errorf("output differs from examples/coverage.json; re-record the examples (examples/README.md)\ngot:\n%s", stdout.String())
	}
}
