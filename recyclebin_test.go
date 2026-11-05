package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// Uses the real Recycle Bin with throwaway files, and restores them.
func TestRecycleAndRestore(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows Recycle Bin only")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "goterm-undo-test.txt")
	must(t, os.WriteFile(file, []byte("precious"), 0o644))
	folder := filepath.Join(dir, "goterm undo folder")
	must(t, os.MkdirAll(filepath.Join(folder, "inner"), 0o755))
	must(t, os.WriteFile(filepath.Join(folder, "inner", "x.md"), []byte("# x"), 0o644))

	for _, p := range []string{file, folder} {
		start := time.Now().Add(-2 * time.Second)
		if err := recycle(p); err != nil {
			t.Fatalf("recycle %s: %v", p, err)
		}
		if _, err := os.Stat(p); err == nil {
			t.Fatalf("%s still exists after recycle", p)
		}
		if err := restoreFromRecycleBin(p, start); err != nil {
			t.Fatalf("restore %s: %v", p, err)
		}
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "precious" {
		t.Errorf("file not restored intact: %q %v", b, err)
	}
	if b, err := os.ReadFile(filepath.Join(folder, "inner", "x.md")); err != nil || string(b) != "# x" {
		t.Errorf("folder not restored intact: %v", err)
	}
	// Restoring again finds nothing.
	if err := restoreFromRecycleBin(filepath.Join(dir, "never-deleted.txt"), time.Now().Add(-time.Minute)); err == nil {
		t.Error("expected not-found error")
	}
	// Refuses to overwrite.
	if err := restoreFromRecycleBin(file, time.Now().Add(-time.Minute)); err == nil {
		t.Error("expected already-exists error")
	}
}
