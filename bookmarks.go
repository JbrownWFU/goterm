package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Bookmarks: b opens the panel, B saves the current folder.

func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// bookmarkFor returns the index of the bookmark for dir, or -1.
func (m *model) bookmarkFor(dir string) int {
	for i, b := range m.cfg.Bookmarks {
		if samePath(b.Path, dir) {
			return i
		}
	}
	return -1
}

func (m *model) saveBookmarks() {
	if err := saveBookmarks(m.cfgPath, m.cfg.Bookmarks); err != nil {
		m.setStatus("", err)
	}
}

func defaultBookmarkName(dir string) string {
	if n := filepath.Base(dir); n != "" && n != "." && n != `\` && n != "/" {
		return n
	}
	return dir
}

// startAddBookmark asks for a name for dir (the current folder).
func (m *model) startAddBookmark(dir string, fromPanel bool) tea.Cmd {
	if i := m.bookmarkFor(dir); i >= 0 {
		m.setStatus(fmt.Sprintf("already bookmarked as %q", m.cfg.Bookmarks[i].Name), nil)
		if fromPanel {
			m.bmCursor = i
		}
		return nil
	}
	m.bmPending, m.bmReturn = dir, fromPanel
	return m.startPrompt(promptBookmarkAdd, "bookmark as › ", defaultBookmarkName(dir), "name")
}

// finishBookmarkPrompt handles the name typed for an add or rename.
func (m *model) finishBookmarkPrompt(kind promptKind, name string) {
	switch kind {
	case promptBookmarkAdd:
		m.snapshotBookmarks("bookmark add")
		m.cfg.Bookmarks = append(m.cfg.Bookmarks, Bookmark{Name: name, Path: m.bmPending})
		m.bmCursor = len(m.cfg.Bookmarks) - 1
		m.saveBookmarks()
		if !m.statusErr {
			m.setStatus(fmt.Sprintf("bookmarked %s as %q", m.bmPending, name), nil)
		}
	case promptBookmarkRename:
		if m.bmCursor < len(m.cfg.Bookmarks) && m.cfg.Bookmarks[m.bmCursor].Name != name {
			m.snapshotBookmarks("bookmark rename")
			m.cfg.Bookmarks[m.bmCursor].Name = name
			m.saveBookmarks()
		}
	}
	if m.bmReturn {
		m.mode = modeBookmarks
	}
}

func (m *model) openBookmarks() {
	m.mode = modeBookmarks
	if i := m.bookmarkFor(m.tab().dir); i >= 0 {
		m.bmCursor = i
	}
	m.bmCursor = max(0, min(m.bmCursor, len(m.cfg.Bookmarks)-1))
}

func (m *model) jumpBookmark(i int, newTab bool) {
	b := m.cfg.Bookmarks[i]
	if fi, err := os.Stat(b.Path); err != nil || !fi.IsDir() {
		m.setStatus("", fmt.Errorf("%q no longer exists: %s", b.Name, b.Path))
		return
	}
	m.mode = modeNormal
	if newTab {
		m.tabs = append(m.tabs, m.newTab(b.Path))
		m.active = len(m.tabs) - 1
	} else {
		m.cdTo(b.Path, "")
	}
	m.setStatus("→ "+b.Name, nil)
}

func (m *model) updateBookmarks(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	bms := m.cfg.Bookmarks
	n := len(bms)
	m.status, m.statusWarn = "", false
	armed := m.armed
	m.armed = ""
	switch k {
	case "esc", "q", "b", "ctrl+c":
		m.mode = modeNormal
	case "u", "ctrl+z":
		return m, m.undoLast()
	case "a", "B":
		return m, m.startAddBookmark(m.tab().dir, true)
	case "j", "down":
		m.bmCursor = min(m.bmCursor+1, n-1)
	case "k", "up":
		m.bmCursor = max(m.bmCursor-1, 0)
	case "g", "home":
		m.bmCursor = 0
	case "G", "end":
		m.bmCursor = max(0, n-1)
	case "enter", "l", "right":
		if n > 0 {
			m.jumpBookmark(m.bmCursor, false)
		}
	case "t":
		if n > 0 {
			m.jumpBookmark(m.bmCursor, true)
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < n {
			m.jumpBookmark(i, false)
		}
	case "r", "f2":
		if n > 0 {
			m.bmReturn = true
			return m, m.startPrompt(promptBookmarkRename, "rename bookmark › ", bms[m.bmCursor].Name, "name")
		}
	case "d", "delete", "x":
		if n > 0 && armed != "bm-remove" {
			m.armed = "bm-remove"
			m.warn(fmt.Sprintf("press %s again to remove %q", k, bms[m.bmCursor].Name))
		} else if n > 0 {
			gone := bms[m.bmCursor]
			m.snapshotBookmarks("removing bookmark " + gone.Name)
			m.cfg.Bookmarks = append(bms[:m.bmCursor:m.bmCursor], bms[m.bmCursor+1:]...)
			m.bmCursor = max(0, min(m.bmCursor, len(m.cfg.Bookmarks)-1))
			m.saveBookmarks()
			if !m.statusErr {
				m.setStatus(fmt.Sprintf("removed bookmark %q · u brings it back", gone.Name), nil)
			}
		}
	case "J", "shift+down":
		if m.bmCursor < n-1 {
			m.snapshotBookmarks("bookmark move")
			bms[m.bmCursor], bms[m.bmCursor+1] = bms[m.bmCursor+1], bms[m.bmCursor]
			m.bmCursor++
			m.saveBookmarks()
		}
	case "K", "shift+up":
		if m.bmCursor > 0 {
			m.snapshotBookmarks("bookmark move")
			bms[m.bmCursor], bms[m.bmCursor-1] = bms[m.bmCursor-1], bms[m.bmCursor]
			m.bmCursor--
			m.saveBookmarks()
		}
	}
	return m, nil
}

// bookmarkLines renders the panel shown in the preview column.
func (m *model) bookmarkLines(w, h int) []string {
	boxW := max(10, w-4)
	inner := boxW - 6 // border + padding
	rows := []string{styleHeading.Render("★ Bookmarks"), ""}
	if len(m.cfg.Bookmarks) == 0 {
		rows = append(rows,
			styleDim.Render("No bookmarks yet."),
			"",
			styleKey.Render("a")+styleDim.Render(" bookmarks the current folder"),
			styleDim.Render("(or press ")+styleKey.Render("B")+styleDim.Render(" while browsing)"))
	}
	for i, b := range m.cfg.Bookmarks {
		missing := false
		if fi, err := os.Stat(b.Path); err != nil || !fi.IsDir() {
			missing = true
		}
		num := fmt.Sprintf("%d", i+1)
		if i >= 9 {
			num = " "
		}
		here := ""
		if samePath(b.Path, m.tab().dir) {
			here = " ●"
		}
		path := b.Path
		if missing {
			path = "missing · " + path
		}
		// Long paths are trimmed from the left so the folder name stays visible.
		if pw := inner - 5; ansi.StringWidth(path) > pw && pw > 1 {
			path = "…" + ansi.TruncateLeft(path, ansi.StringWidth(path)-(pw-1), "")
		}
		if i == m.bmCursor {
			sel := styleSel.Width(inner - 1)
			rows = append(rows,
				styleSelMark.Render("▌")+sel.Render(fmt.Sprintf(" %s  %s%s", num, b.Name, here)),
				styleSelMark.Render("▌")+sel.Foreground(cMuted).Render("    "+path))
		} else {
			nameStyle := lipgloss.NewStyle().Foreground(cBlue).Bold(true)
			if missing {
				nameStyle = lipgloss.NewStyle().Foreground(cRed).Strikethrough(true)
			}
			rows = append(rows,
				" "+styleKey.Render(num)+"  "+nameStyle.Render(b.Name)+lipgloss.NewStyle().Foreground(cPink).Render(here),
				styleDim.Render("     "+path))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cYellow).
		Padding(0, 2).Width(boxW - 2).Render(strings.Join(rows, "\n"))
	lines := []string{fit("", w)}
	for _, l := range strings.Split(box, "\n") {
		lines = append(lines, fit("  "+l, w))
	}
	// Keep the selected bookmark visible when the list is taller than the column.
	if sel := 4 + 2*m.bmCursor; sel >= h {
		lines = lines[min(len(lines)-1, sel-h+2):]
	}
	return padLines(lines, w, h)
}
