package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SortSpec is how folder listings are ordered (S opens the menu).
type SortSpec struct {
	By        string `json:"by"` // name, modified, size, type
	Reverse   bool   `json:"reverse"`
	DirsFirst bool   `json:"dirsFirst"`
}

var sortNames = map[string]string{
	"name":     "name",
	"modified": "modified, newest first",
	"size":     "size, largest first",
	"type":     "type (extension)",
}

func (s SortSpec) valid() bool { _, ok := sortNames[s.By]; return ok }

func (s SortSpec) isDefault() bool { return s.By == "name" && !s.Reverse && s.DirsFirst }

// label is the short indicator shown in the breadcrumb.
func (s SortSpec) label() string {
	arrow := map[bool]string{false: "↓", true: "↑"}[s.Reverse]
	l := s.By + " " + arrow
	if !s.DirsFirst {
		l += " · mixed"
	}
	return l
}

// listOpts controls how a folder is read.
type listOpts struct {
	hidden bool
	sort   SortSpec
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// naturalCmp compares case-insensitively with numbers by value, so
// "file2" sorts before "file10".
func naturalCmp(a, b string) int {
	a, b = strings.ToLower(a), strings.ToLower(b)
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if isDigit(a[i]) && isDigit(b[j]) {
			si, sj := i, j
			for i < len(a) && isDigit(a[i]) {
				i++
			}
			for j < len(b) && isDigit(b[j]) {
				j++
			}
			na, nb := strings.TrimLeft(a[si:i], "0"), strings.TrimLeft(b[sj:j], "0")
			if len(na) != len(nb) {
				return map[bool]int{true: -1, false: 1}[len(na) < len(nb)]
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			continue
		}
		if a[i] != b[j] {
			return map[bool]int{true: -1, false: 1}[a[i] < b[j]]
		}
		i++
		j++
	}
	switch {
	case len(a)-i < len(b)-j:
		return -1
	case len(a)-i > len(b)-j:
		return 1
	}
	return 0
}

func sortEntries(es []entry, s SortSpec) {
	sort.SliceStable(es, func(i, j int) bool {
		a, b := es[i], es[j]
		if s.DirsFirst && a.isDir != b.isDir {
			return a.isDir
		}
		c := 0
		switch s.By {
		case "modified":
			c = b.mod.Compare(a.mod) // newest first
		case "size":
			c = map[bool]int{true: -1, false: 1}[a.size > b.size]
			if a.size == b.size {
				c = 0
			}
		case "type":
			c = strings.Compare(strings.ToLower(filepath.Ext(a.name)), strings.ToLower(filepath.Ext(b.name)))
		default:
			c = naturalCmp(a.name, b.name)
		}
		if s.Reverse {
			c = -c
		}
		if c != 0 {
			return c < 0
		}
		return naturalCmp(a.name, b.name) < 0
	})
}

// --- sort menu (S) ---

func (m *model) opts() listOpts { return listOpts{hidden: m.showHidden, sort: m.cfg.Sort} }

func (m *model) updateSort(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.cfg.Sort
	switch msg.String() {
	case "esc", "q", "enter", "S", "ctrl+c":
		m.mode = modeNormal
		return m, nil
	case "u", "ctrl+z":
		return m, m.undoLast()
	case "n":
		s.By = "name"
	case "m":
		s.By = "modified"
	case "s":
		s.By = "size"
	case "t":
		s.By = "type"
	case "r":
		s.Reverse = !s.Reverse
	case "d":
		s.DirsFirst = !s.DirsFirst
	case "0":
		s = defaultConfig().Sort
	default:
		return m, nil
	}
	if s == m.cfg.Sort {
		return m, nil
	}
	m.snapshotSort()
	m.cfg.Sort = s
	m.reloadAll("")
	if err := updateConfigFile(m.cfgPath, func(c *Config) { c.Sort = s }); err != nil {
		m.setStatus("", err)
		return m, nil
	}
	desc := sortNames[s.By]
	if s.Reverse {
		desc += ", reversed"
	}
	m.setStatus("sorted by "+desc, nil)
	return m, nil
}

func (m *model) sortLines(w, h int) []string {
	s := m.cfg.Sort
	boxW := min(44, max(10, w-4))
	row := func(key, label string, on bool, value string) string {
		mark := "  "
		lbl := styleText.Render(label)
		if on {
			mark = lipgloss.NewStyle().Foreground(cPink).Render("● ")
			lbl = lipgloss.NewStyle().Foreground(cBright).Bold(true).Render(label)
		}
		r := " " + styleKey.Render(key) + "  " + mark + lbl
		if value != "" {
			r += styleDim.Render("  " + value)
		}
		return r
	}
	onOff := map[bool]string{true: "on", false: "off"}
	rows := []string{
		styleHeading.Render("⇅ Sort by"), "",
		row("n", "name", s.By == "name", ""),
		row("m", "modified", s.By == "modified", "newest first"),
		row("s", "size", s.By == "size", "largest first"),
		row("t", "type", s.By == "type", "by extension"),
		"",
		row("r", "reverse", s.Reverse, onOff[s.Reverse]),
		row("d", "folders first", s.DirsFirst, onOff[s.DirsFirst]),
		row("0", "reset", false, "name, folders first"),
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cTeal).
		Padding(0, 2).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	lines := []string{fit("", w)}
	for _, l := range strings.Split(box, "\n") {
		lines = append(lines, fit("  "+l, w))
	}
	lines = append(lines, fit("", w), fit(styleDim.Render(fmt.Sprintf("  saved as your default in %s", filepath.Base(m.cfgPath))), w))
	return padLines(lines, w, h)
}
