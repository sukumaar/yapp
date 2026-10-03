package fileutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	for _, content := range []string{"old", "replacement"} {
		if err := AtomicWrite(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != content {
			t.Fatalf("content=%q err=%v", data, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("incorrect permissions: %v", err)
	}
	// Failed replacement must preserve the destination and clean up its temp file.
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(blocked, []byte("invalid"), 0o600); err == nil {
		t.Fatal("replaced a directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 || entries[0].Name() != "blocked" || entries[1].Name() != "config" {
		t.Fatalf("unexpected files: %v, %v", entries, err)
	}
}
