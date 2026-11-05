package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// drainFind runs the background indexer to completion, as the program would.
func drainFind(t *testing.T, m *model) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for m.find != nil && !m.find.done {
		if time.Now().After(deadline) {
			t.Fatal("indexing did not finish")
		}
		if msg := nextFindBatch(m.findCh)(); msg != nil {
			m.Update(msg)
		}
	}
}

func findTree(t *testing.T, dir string) {
	t.Helper()
	for _, p := range []string{
		`a\b\c\deep-notes.md`,
		`a\b\other.txt`,
		`a\design\plan.md`,
		`docs\meeting notes 2026.md`,
		`node_modules\pkg\deep-notes.md`,
		`.hidden\deep-notes.md`,
		`src\DeepNotesParser.go`,
	} {
		full := filepath.Join(dir, p)
		must(t, os.MkdirAll(filepath.Dir(full), 0o755))
		must(t, os.WriteFile(full, []byte("# "+p+"\n"), 0o644))
	}
}

func resultNames(m *model) []string {
	var out []string
	for _, r := range m.findRes {
		out = append(out, m.find.items[r.idx].rel)
	}
	return out
}

func TestFind(t *testing.T) {
	m, dir := setup(t)
	findTree(t, dir)
	m.reloadAll("")

	key(m, "f")
	if m.mode != modeFind {
		t.Fatal("f should open find")
	}
	drainFind(t, m)
	all := resultNames(m)
	joined := strings.Join(all, "|")
	if strings.Contains(joined, "node_modules") || strings.Contains(joined, ".hidden") {
		t.Errorf("skipped folders were indexed: %v", all)
	}
	if !strings.Contains(joined, `a\b\c\deep-notes.md`) {
		t.Errorf("nested file missing: %v", all)
	}
	// Breadth-first: top-level entries come before nested ones.
	if strings.Contains(all[0], `\`) {
		t.Errorf("first result should be top level, got %q", all[0])
	}
	v := checkView(t, m)
	if !strings.Contains(v, "FIND") || !strings.Contains(v, "items in this folder and below") {
		t.Errorf("find view header/status wrong:\n%s", v)
	}

	// Fuzzy, multi-term; a match in the file name ranks first.
	typeText(m, "deep")
	res := resultNames(m)
	if len(res) != 2 || !strings.Contains(strings.Join(res, "|"), `a\b\c\deep-notes.md`) {
		t.Errorf("deep: %v", res)
	}
	m.input.SetValue("")
	typeText(m, "meet 2026")
	if res := resultNames(m); len(res) != 1 || res[0] != `docs\meeting notes 2026.md` {
		t.Errorf("multi-term: %v", res)
	}
	m.input.SetValue("")
	typeText(m, "dnp") // scattered letters: DeepNotesParser
	if res := resultNames(m); len(res) == 0 || res[0] != `src\DeepNotesParser.go` {
		t.Errorf("fuzzy: %v", res)
	}
	m.input.SetValue("")
	typeText(m, "zzzz")
	if v := checkView(t, m); !strings.Contains(v, "no matches") {
		t.Error("no-match message missing")
	}

	// Enter on a file: go to its folder with it selected.
	m.input.SetValue("")
	typeText(m, "plan")
	checkView(t, m)
	key(m, "enter")
	if m.mode != modeNormal || m.tab().dir != filepath.Join(dir, "a", "design") {
		t.Fatalf("enter should jump to the file's folder, at %s", m.tab().dir)
	}
	if e, _ := m.tab().selected(); e.name != "plan.md" {
		t.Errorf("file should be selected, got %q", e.name)
	}

	// Reopening in the same folder reuses the index; a folder result is entered.
	m.cdTo(dir, "")
	key(m, "f")
	if !m.find.done {
		t.Error("index should be reused")
	}
	typeText(m, "design")
	key(m, "enter")
	if m.tab().dir != filepath.Join(dir, "a", "design") {
		t.Errorf("enter on folder should cd into it, at %s", m.tab().dir)
	}

	// ctrl+o opens the file in the editor.
	m.cdTo(dir, "")
	key(m, "f")
	typeText(m, "other")
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.mode != modeEditor || !strings.HasSuffix(m.editor.path, "other.txt") {
		t.Error("ctrl+o should open the result in the editor")
	}
	key(m, "esc")

	// esc cancels; closing mid-index stops it.
	key(m, "h")
	key(m, "f")
	if m.find.done {
		t.Error("new root should re-index")
	}
	key(m, "esc")
	if m.mode != modeNormal || m.find != nil {
		t.Error("esc mid-index should cancel")
	}

	// Narrow.
	m.cdTo(dir, "")
	key(m, "f")
	drainFind(t, m)
	typeText(m, "notes")
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("narrow find line %d is %d wide: %q", i, w, ansi.Strip(l))
		}
	}
}

func TestNaturalSort(t *testing.T) {
	if naturalCmp("file2.md", "file10.md") >= 0 || naturalCmp("File10", "file9") <= 0 || naturalCmp("a", "A") != 0 {
		t.Error("natural compare wrong")
	}
}

func TestSort(t *testing.T) {
	m, dir := setup(t)
	now := time.Now()
	for i, f := range []struct {
		name string
		size int
	}{{"file10.md", 300}, {"file2.md", 10}, {"zeta.txt", 5000}, {"alpha.go", 1}} {
		p := filepath.Join(dir, f.name)
		must(t, os.WriteFile(p, make([]byte, f.size), 0o644))
		ts := now.Add(time.Duration(i) * time.Hour) // later entries are newer
		must(t, os.Chtimes(p, ts, ts))
	}
	m.reloadAll("")
	names := func() []string {
		var out []string
		for _, e := range m.tab().entries {
			out = append(out, e.name)
		}
		return out
	}

	got := strings.Join(names(), ",")
	if !strings.HasPrefix(got, "sub,alpha.go,file2.md,file10.md,main.go") {
		t.Errorf("default natural order: %s", got)
	}

	key(m, "S")
	if v := checkView(t, m); !strings.Contains(v, "Sort by") || !strings.Contains(v, "SORT") {
		t.Errorf("sort menu missing:\n%s", v)
	}
	key(m, "m")
	if n := names(); n[0] != "sub" || n[1] != "alpha.go" {
		t.Errorf("modified (newest first, dirs first): %v", n)
	}
	key(m, "d")
	key(m, "s")
	if n := names(); n[0] != "zeta.txt" || n[1] != "file10.md" {
		t.Errorf("size, mixed: %v", n)
	}
	key(m, "r")
	if n := names(); n[len(n)-1] != "zeta.txt" {
		t.Errorf("reversed size: %v", n)
	}
	key(m, "esc")
	if v := checkView(t, m); !strings.Contains(v, "size ↑ · mixed") {
		t.Errorf("breadcrumb sort indicator missing:\n%s", v)
	}

	// Saved to config, other settings intact.
	cfg := defaultConfig()
	data, err := os.ReadFile(m.cfgPath)
	must(t, err)
	must(t, json.Unmarshal(data, &cfg))
	if cfg.Sort != (SortSpec{By: "size", Reverse: true, DirsFirst: false}) || len(cfg.Openers) == 0 {
		t.Errorf("sort not saved: %+v", cfg.Sort)
	}

	key(m, "S")
	key(m, "0")
	key(m, "esc")
	if !m.cfg.Sort.isDefault() || strings.Contains(checkView(t, m), "⇅") {
		t.Error("reset should restore default and hide the indicator")
	}
}
