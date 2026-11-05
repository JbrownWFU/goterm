package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Narrow terminals drop low-priority footer hints but keep help and quit.
func TestFooterShrinksOnNarrowTerminal(t *testing.T) {
	m, _ := setup(t)
	wide := ansi.Strip(m.footer(testW))
	if !strings.Contains(wide, "E open with") || !strings.Contains(wide, "q q quit") {
		t.Fatalf("wide footer is missing hints: %q", wide)
	}
	for _, w := range []int{80, 60, 40, 25} {
		m.Update(tea.WindowSizeMsg{Width: w, Height: testH})
		f := m.footer(w)
		if got := ansi.StringWidth(f); got != w {
			t.Errorf("width %d: footer is %d wide", w, got)
		}
		plain := ansi.Strip(f)
		if !strings.Contains(plain, "? help") || !strings.Contains(plain, "q q quit") {
			t.Errorf("width %d: footer lost help/quit: %q", w, plain)
		}
		if strings.Contains(plain, "…") {
			t.Errorf("width %d: footer was truncated instead of trimmed: %q", w, plain)
		}
		for _, l := range strings.Split(m.View(), "\n") {
			if lw := ansi.StringWidth(l); lw != w {
				t.Errorf("width %d: view line is %d wide: %q", w, lw, ansi.Strip(l))
				break
			}
		}
	}
	narrow := ansi.Strip(m.footer(40))
	if strings.Contains(narrow, "E open with") {
		t.Errorf("narrow footer should drop the tail hints: %q", narrow)
	}
}

func TestMissingProgram(t *testing.T) {
	cases := []struct {
		cmd  []string
		want string
	}{
		{nil, ""},
		{[]string{"go", "version"}, ""},
		{[]string{"definitely-not-a-program-xyz"}, "definitely-not-a-program-xyz"},
		{[]string{"go", "-Command", "definitely-not-a-program-xyz --flag"}, "definitely-not-a-program-xyz"},
		{[]string{"go", "-Command", "go version"}, ""},
		// Script snippets aren't inspected; only the shell itself is checked.
		{[]string{"go", "-Command", "cd x; definitely-not-a-program-xyz"}, ""},
	}
	for _, c := range cases {
		if got := missingProgram(c.cmd); got != c.want {
			t.Errorf("missingProgram(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestVersionString(t *testing.T) {
	version = ""
	if v := versionString(); v == "" {
		t.Error("empty version")
	}
	version = "v9.9"
	if v := versionString(); v != "v9.9" {
		t.Errorf("got %q", v)
	}
	version = ""
}
