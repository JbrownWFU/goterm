package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func press(m *model, msg tea.KeyMsg) tea.Cmd {
	_, cmd := m.Update(msg)
	return cmd
}

func TestQuitNeedsTwoPresses(t *testing.T) {
	m, _ := setup(t)
	q := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}
	if isQuit(press(m, q)) {
		t.Fatal("one q should not quit")
	}
	if v := checkView(t, m); !strings.Contains(v, "press q again") {
		t.Errorf("quit warning missing:\n%s", v)
	}
	key(m, "j") // any other key disarms
	if isQuit(press(m, q)) {
		t.Fatal("q after another key should only arm again")
	}
	if !isQuit(press(m, q)) || !m.quitCD {
		t.Fatal("q q should quit and cd")
	}

	m, _ = setup(t)
	cc := tea.KeyMsg{Type: tea.KeyCtrlC}
	if isQuit(press(m, cc)) {
		t.Fatal("one ctrl+c should not quit")
	}
	if isQuit(press(m, q)) {
		t.Fatal("ctrl+c then q should not quit")
	}
	press(m, cc)
	if !isQuit(press(m, cc)) || m.quitCD {
		t.Fatal("ctrl+c twice should quit without cd")
	}
}

func TestRenameConfirmAndUndo(t *testing.T) {
	m, dir := setup(t)
	m.tab().focus("notes.txt")

	// Accidental: r, stray keys, enter, then anything but y -> nothing changes.
	key(m, "r")
	typeText(m, "jk")
	key(m, "enter")
	if m.mode != modeConfirm {
		t.Fatal("rename should ask for confirmation")
	}
	if v := checkView(t, m); !strings.Contains(v, `rename "notes.txt" → "notes.txtjk"?`) {
		t.Errorf("confirmation text missing:\n%s", v)
	}
	key(m, "enter")
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("enter must not confirm a rename")
	}

	// Unchanged name: no confirmation at all.
	key(m, "r")
	key(m, "enter")
	if m.mode != modeNormal {
		t.Error("unchanged name should not ask")
	}

	// Real rename, then undo.
	key(m, "r")
	m.input.SetValue("")
	typeText(m, "todo.txt")
	key(m, "enter")
	key(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "todo.txt")); err != nil {
		t.Fatal("y should rename")
	}
	if v := checkView(t, m); !strings.Contains(v, "u undoes") {
		t.Error("undo hint missing")
	}
	key(m, "j")
	key(m, "u")
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("u should undo the rename")
	}
	if e, _ := m.tab().selected(); e.name != "notes.txt" {
		t.Errorf("cursor should follow the undone file, got %q", e.name)
	}
	key(m, "u")
	if !m.statusErr || !strings.Contains(m.status, "nothing to undo") {
		t.Error("second u should have nothing to undo")
	}
}

func TestEditorCtrlCDoesNotClose(t *testing.T) {
	m, _ := setup(t)
	m.tab().focus("readme.md")
	key(m, "e")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if m.mode != modeEditor {
		t.Fatal("ctrl+c should not close the editor")
	}
	if v := checkView(t, m); !strings.Contains(v, "esc closes") {
		t.Error("ctrl+c hint missing")
	}
}
