package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const testW, testH = 110, 30

func setup(t *testing.T) (*model, string) {
	t.Helper()
	dir := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hello\n\nsome *markdown*\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("line1\r\nline2\r\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644))
	must(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	m := newModel(defaultConfig(), dir)
	m.cfgPath = filepath.Join(t.TempDir(), "config.json") // never touch the real config
	m.Update(tea.WindowSizeMsg{Width: testW, Height: testH})
	return m, dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func key(m *model, k string) {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		msg = tea.KeyMsg{Type: tea.KeyCtrlS}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+p":
		msg = tea.KeyMsg{Type: tea.KeyCtrlP}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m.Update(msg)
}

func typeText(m *model, s string) {
	for _, r := range s {
		key(m, string(r))
	}
}

// checkView asserts the screen is exactly testH lines, none wider than testW
// (and exactly testW outside the editor, whose text lines may be shorter).
func checkView(t *testing.T, m *model) string {
	t.Helper()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) != testH {
		t.Errorf("view has %d lines, want %d", len(lines), testH)
	}
	for i, l := range lines {
		w := ansi.StringWidth(l)
		if w > testW || (m.mode != modeEditor && w != testW) {
			t.Errorf("line %d is %d wide, want %d: %q", i, w, testW, ansi.Strip(l))
		}
	}
	return ansi.Strip(v)
}

func TestBrowseAndPreview(t *testing.T) {
	m, _ := setup(t)
	v := checkView(t, m)
	for _, want := range []string{"sub", "readme.md", "notes.txt", "new md", "goterm", "BROWSE"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
	m.tab().focus("readme.md")
	v = checkView(t, m)
	if !strings.Contains(v, "some markdown") || strings.Contains(v, "*markdown*") {
		t.Errorf("markdown preview not rendered by glamour:\n%s", v)
	}
	m.tab().focus("notes.txt")
	if v = checkView(t, m); !strings.Contains(v, "1 line1") {
		t.Errorf("text preview should have line numbers:\n%s", v)
	}
	key(m, "l") // enter on a file opens the editor
	if m.mode != modeEditor {
		t.Fatal("enter on file should open editor")
	}
	checkView(t, m)
	key(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc on clean editor should close")
	}
}

func TestNewFileEditSave(t *testing.T) {
	m, dir := setup(t)
	key(m, "n")
	checkView(t, m)
	typeText(m, "today")
	key(m, "enter")
	path := filepath.Join(dir, "today.md")
	data, err := os.ReadFile(path)
	must(t, err)
	if !strings.HasPrefix(string(data), "# today") {
		t.Errorf("template not applied: %q", data)
	}
	if m.mode != modeEditor {
		t.Fatal("new file should open in editor")
	}
	typeText(m, "hi there")
	checkView(t, m)
	key(m, "esc") // dirty -> warns
	if m.mode != modeEditor {
		t.Fatal("first esc on dirty editor should not close")
	}
	key(m, "ctrl+s")
	key(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("esc after save should close")
	}
	data, _ = os.ReadFile(path)
	if !strings.HasSuffix(string(data), "hi there") {
		t.Errorf("edit not saved at end of file: %q", data)
	}
	if e, _ := m.tab().selected(); e.name != "today.md" {
		t.Errorf("cursor should be on new file, got %q", e.name)
	}
}

func TestEditorPreservesCRLF(t *testing.T) {
	m, dir := setup(t)
	m.tab().focus("notes.txt")
	key(m, "e")
	typeText(m, "X")
	key(m, "ctrl+s")
	key(m, "esc")
	data, _ := os.ReadFile(filepath.Join(dir, "notes.txt"))
	if string(data) != "Xline1\r\nline2\r\n" {
		t.Errorf("got %q", data)
	}
}

func TestMarkdownPreviewInEditor(t *testing.T) {
	m, _ := setup(t)
	m.tab().focus("readme.md")
	key(m, "e")
	key(m, "ctrl+p")
	if !m.editor.previewing {
		t.Fatal("ctrl+p should preview")
	}
	if v := checkView(t, m); !strings.Contains(v, "PREVIEW") {
		t.Error("preview pill missing")
	}
	key(m, "esc")
	key(m, "esc")
	if m.mode != modeNormal {
		t.Fatal("should be back to browsing")
	}
}

func TestMkdirRenameFilterTabs(t *testing.T) {
	m, dir := setup(t)
	key(m, "N")
	typeText(m, "a/b")
	key(m, "enter")
	if fi, err := os.Stat(filepath.Join(dir, "a", "b")); err != nil || !fi.IsDir() {
		t.Fatal("nested mkdir failed")
	}
	if e, _ := m.tab().selected(); e.name != "a" {
		t.Errorf("cursor should be on a, got %q", e.name)
	}

	key(m, "r")
	m.input.SetValue("")
	typeText(m, "renamed")
	key(m, "enter")
	key(m, "y")
	if _, err := os.Stat(filepath.Join(dir, "renamed")); err != nil {
		t.Fatal("rename failed")
	}

	key(m, "/")
	typeText(m, "note")
	if len(m.tab().entries) != 1 || m.tab().entries[0].name != "notes.txt" {
		t.Errorf("filter gave %v", m.tab().entries)
	}
	if v := checkView(t, m); !strings.Contains(v, "FILTER") {
		t.Error("filter mode pill missing")
	}
	key(m, "enter")
	checkView(t, m)
	key(m, "esc")
	if len(m.tab().entries) != 5 {
		t.Errorf("esc should clear filter, have %d entries", len(m.tab().entries))
	}

	m.tab().focus("sub")
	key(m, "l")
	checkView(t, m) // empty folder
	key(m, "t")
	if len(m.tabs) != 2 || m.tab().dir != filepath.Join(dir, "sub") {
		t.Fatal("new tab should open at current dir")
	}
	key(m, "h")
	key(m, "tab")
	if m.active != 0 || m.tab().dir != filepath.Join(dir, "sub") {
		t.Error("tabs should keep separate locations")
	}
	key(m, "x")
	if len(m.tabs) != 1 {
		t.Error("x should close tab")
	}
	checkView(t, m)
}

func TestHelpConfirmAndOpenWithRender(t *testing.T) {
	m, _ := setup(t)
	key(m, "?")
	checkView(t, m)
	key(m, "q")
	if m.mode != modeNormal {
		t.Fatal("any key closes help")
	}
	m.openers = []Opener{{Name: "Test", Cmd: "x"}}
	key(m, "E")
	if m.mode != modeOpenWith {
		t.Fatal("E should open picker")
	}
	if v := checkView(t, m); !strings.Contains(v, "1  Test") {
		t.Error("picker not shown")
	}
	key(m, "esc")
	key(m, "d")
	if v := checkView(t, m); !strings.Contains(v, "CONFIRM") {
		t.Error("confirm pill missing")
	}
	key(m, "n")
	if m.mode != modeNormal {
		t.Error("n should cancel delete")
	}
}

func TestNarrowTerminal(t *testing.T) {
	m, _ := setup(t)
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	v := m.View()
	for i, l := range strings.Split(v, "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("narrow line %d is %d wide", i, w)
		}
	}
}

func TestBookmarks(t *testing.T) {
	m, dir := setup(t)
	sub := filepath.Join(dir, "sub")

	// Empty panel renders.
	key(m, "b")
	if m.mode != modeBookmarks {
		t.Fatal("b should open bookmarks")
	}
	if v := checkView(t, m); !strings.Contains(v, "No bookmarks yet") {
		t.Error("empty state missing")
	}
	key(m, "esc")

	// B bookmarks the current folder; the prompt defaults to its name.
	m.cdTo(sub, "")
	key(m, "B")
	if m.input.Value() != "sub" {
		t.Errorf("default name %q", m.input.Value())
	}
	m.input.SetValue("")
	typeText(m, "work")
	key(m, "enter")
	if m.mode != modeNormal || len(m.cfg.Bookmarks) != 1 || m.cfg.Bookmarks[0].Path != sub {
		t.Fatalf("bookmark not added: %+v", m.cfg.Bookmarks)
	}
	if v := checkView(t, m); !strings.Contains(v, "★ work") {
		t.Error("title bar should show the bookmark name")
	}

	// Saved to the config file, other settings intact.
	cfg := defaultConfig()
	data, err := os.ReadFile(m.cfgPath)
	must(t, err)
	must(t, json.Unmarshal(data, &cfg))
	if len(cfg.Bookmarks) != 1 || cfg.Bookmarks[0].Name != "work" || len(cfg.Openers) == 0 {
		t.Errorf("config not saved properly: %s", data)
	}

	// B again on the same folder doesn't duplicate.
	key(m, "B")
	if m.mode != modeNormal || len(m.cfg.Bookmarks) != 1 {
		t.Error("duplicate bookmark")
	}

	// Add the root from inside the panel; it returns to the panel.
	key(m, "h")
	key(m, "b")
	key(m, "a")
	if v := checkView(t, m); !strings.Contains(v, "BOOKMARK") {
		t.Error("bookmark prompt label missing")
	}
	key(m, "enter")
	if m.mode != modeBookmarks || len(m.cfg.Bookmarks) != 2 || m.bmCursor != 1 {
		t.Fatalf("add from panel: mode %v, %+v", m.mode, m.cfg.Bookmarks)
	}
	checkView(t, m)

	// Reorder, rename, jump with a number key.
	key(m, "K")
	if m.cfg.Bookmarks[0].Path != dir || m.bmCursor != 0 {
		t.Error("K should move bookmark up")
	}
	key(m, "r")
	m.input.SetValue("")
	typeText(m, "root")
	key(m, "enter")
	if m.mode != modeBookmarks || m.cfg.Bookmarks[0].Name != "root" {
		t.Error("rename failed")
	}
	key(m, "2")
	if m.mode != modeNormal || m.tab().dir != sub {
		t.Errorf("2 should jump to work, at %s", m.tab().dir)
	}

	// Open in a new tab.
	key(m, "b")
	key(m, "j")
	key(m, "k")
	key(m, "t")
	if len(m.tabs) != 2 || m.tab().dir != dir {
		t.Error("t should open bookmark in a new tab")
	}

	// Missing folder: shown, jump refused.
	must(t, os.Remove(sub))
	key(m, "b")
	key(m, "j")
	checkView(t, m)
	key(m, "enter")
	if m.mode != modeBookmarks || !m.statusErr {
		t.Error("jumping to a missing folder should fail and stay in the panel")
	}

	// Remove (takes two presses).
	key(m, "d")
	if len(m.cfg.Bookmarks) != 2 {
		t.Error("one d should only ask")
	}
	key(m, "d")
	if len(m.cfg.Bookmarks) != 1 || m.cfg.Bookmarks[0].Name != "root" {
		t.Errorf("remove failed: %+v", m.cfg.Bookmarks)
	}
	data, _ = os.ReadFile(m.cfgPath)
	if strings.Contains(string(data), "work") {
		t.Error("removal not saved")
	}

	// Many bookmarks with long paths still fit, narrow too.
	for i := 0; i < 20; i++ {
		m.cfg.Bookmarks = append(m.cfg.Bookmarks, Bookmark{Name: fmt.Sprintf("b%d", i), Path: `C:\Users\someone\` + strings.Repeat(`very long folder name\`, 4)})
	}
	m.bmCursor = 18
	checkView(t, m)
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("narrow line %d is %d wide", i, w)
		}
	}
}
