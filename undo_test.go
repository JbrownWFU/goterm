package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// act sends a key and runs any background work it starts (delete, undo of
// an edited new file), feeding results back like the real program would.
func act(m *model, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "ctrl+z":
		msg = tea.KeyMsg{Type: tea.KeyCtrlZ}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	_, cmd := m.Update(msg)
	drain(m, cmd)
}

func drain(m *model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			drain(m, c)
		}
	case opDoneMsg:
		_, next := m.Update(msg)
		drain(m, next)
	}
}

func TestUndoStack(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("uses the Windows Recycle Bin")
	}
	m, dir := setup(t)

	// 1. delete notes.txt (really goes to the Recycle Bin)
	m.tab().focus("notes.txt")
	act(m, "d")
	act(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err == nil {
		t.Fatal("notes.txt should be recycled")
	}
	if !strings.Contains(m.status, "u restores it") {
		t.Errorf("status = %q", m.status)
	}
	// 2. new nested folder
	act(m, "N")
	typeText(m, "a/b")
	act(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "a", "b")); err != nil {
		t.Fatal(err)
	}
	// 3. sort change (and a no-op one that shouldn't count)
	act(m, "S")
	act(m, "m")
	act(m, "m")
	key(m, "esc")
	// 4. open and close a tab
	act(m, "t")
	act(m, "x")
	if len(m.tabs) != 1 {
		t.Fatal("x should close the tab")
	}
	// 5. bookmark
	act(m, "B")
	act(m, "enter")
	if len(m.cfg.Bookmarks) != 1 {
		t.Fatal("bookmark not added")
	}
	if len(m.undo) != 5 {
		t.Fatalf("undo stack has %d steps, want 5", len(m.undo))
	}

	act(m, "u")
	if len(m.cfg.Bookmarks) != 0 || !strings.Contains(m.status, "↶") {
		t.Errorf("bookmark add not undone: %q", m.status)
	}
	act(m, "u")
	if len(m.tabs) != 2 || !strings.Contains(m.status, "reopened tab") {
		t.Errorf("tab not reopened: %q", m.status)
	}
	act(m, "u")
	if m.cfg.Sort.By != "name" || !strings.Contains(m.status, "sort back to") {
		t.Errorf("sort not undone: %+v %q", m.cfg.Sort, m.status)
	}
	act(m, "ctrl+z")
	if _, err := os.Stat(filepath.Join(dir, "a")); err == nil {
		t.Error("new folder a/b should be removed, including a")
	}
	act(m, "u")
	b, err := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if err != nil || string(b) != "line1\r\nline2\r\n" {
		t.Fatalf("notes.txt not restored intact: %q %v", b, err)
	}
	if !strings.Contains(m.status, "restored notes.txt from the Recycle Bin") {
		t.Errorf("status = %q", m.status)
	}
	if e, _ := m.tab().selected(); e.name != "notes.txt" {
		t.Errorf("cursor should land on the restored file, got %q", e.name)
	}
	checkView(t, m)
	act(m, "u")
	if !m.statusErr || !strings.Contains(m.status, "nothing to undo") {
		t.Errorf("status = %q", m.status)
	}
}

func TestUndoNewFileAndSave(t *testing.T) {
	m, dir := setup(t)
	path := filepath.Join(dir, "x.txt")
	act(m, "n")
	typeText(m, "x.txt")
	act(m, "enter")
	if m.mode != modeEditor {
		t.Fatal("new file should open the editor")
	}
	typeText(m, "hi")
	key(m, "ctrl+s")
	key(m, "esc")
	if b, _ := os.ReadFile(path); string(b) != "hi" {
		t.Fatalf("saved %q", b)
	}
	act(m, "u")
	if b, _ := os.ReadFile(path); string(b) != "" {
		t.Errorf("save not undone: %q (%s)", b, m.status)
	}
	act(m, "u")
	if _, err := os.Stat(path); err == nil {
		t.Errorf("new file not removed: %s", m.status)
	}

	// A save is not undone over a later outside change.
	m.tab().focus("main.go")
	act(m, "e")
	typeText(m, "// x\n")
	key(m, "ctrl+s")
	key(m, "esc")
	must(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("changed elsewhere"), 0o644))
	act(m, "u")
	if !m.statusErr || !strings.Contains(m.status, "changed since") {
		t.Errorf("status = %q", m.status)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(b) != "changed elsewhere" {
		t.Error("undo must not clobber outside changes")
	}
}

func TestFirstMissing(t *testing.T) {
	dir := t.TempDir()
	if got := firstMissing(dir, filepath.Join(dir, "a", "b", "c")); got != filepath.Join(dir, "a") {
		t.Errorf("got %q", got)
	}
	if got := firstMissing(dir, dir); got != "" {
		t.Errorf("got %q", got)
	}
}
