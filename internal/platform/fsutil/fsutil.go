// Package fsutil writes private files atomically. Outputs can hold resume
// and posting text, so directories are created 0700 and files 0600, and a
// reader never sees a half-written file.
package fsutil

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path through a private temp file in the
// same directory, then renames it into place.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	f, err := os.CreateTemp(dir, ".tmp-*") // created 0600
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// WriteJSONAtomic writes v as indented JSON with a trailing newline.
func WriteJSONAtomic(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return WriteFileAtomic(path, append(raw, '\n'))
}
