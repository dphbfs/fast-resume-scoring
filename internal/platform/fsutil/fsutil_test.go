package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out", "nested")
	path := filepath.Join(dir, "report.json")
	if err := WriteJSONAtomic(path, map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("replaced\n")); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "replaced\n" {
		t.Fatalf("content = %q (%v)", raw, err)
	}
	for p, want := range map[string]os.FileMode{path: 0o600, dir: 0o700} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", p, got, want)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir holds %d entries, want only the report (no temp files left)", len(entries))
	}
}
