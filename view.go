package main

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// fit truncates or pads s (which may contain ANSI codes) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

func padLines(lines []string, w, h int) []string {
	if len(lines) > h {
		lines = lines[:h]
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	return lines
}

func messageLines(msg string, w, h int, st lipgloss.Style) []string {
	return padLines([]string{fit(st.Render("  "+msg), w)}, w, h)
}

func isMarkdown(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".md" || ext == ".markdown"
}

// hints renders "key desc · key desc" for footers.
func hints(onBar bool, keys [][2]string) string {
	ks, ds, ss := styleKey, styleDim, styleSep
	if onBar {
		ks, ds, ss = ks.Background(cBarBg), ds.Background(cBarBg), ss.Background(cBarBg)
	}
	var parts []string
	for _, k := range keys {
		parts = append(parts, ks.Render(k[0])+ds.Render(" "+k[1]))
	}
	return ds.Render(" ") + strings.Join(parts, ss.Render(" · ")) + ds.Render(" ")
}

// renderList draws a column of entries: a selection marker, a type glyph,
// the name in its type color and (optionally) a right-aligned size.
func renderList(entries []entry, cursor, offset, w, h int, focused, showSize bool) []string {
	lines := make([]string, 0, h)
	for i := offset; i < len(entries) && len(lines) < h; i++ {
		e := entries[i]
		k := kindOf(e)
		name := e.name
		if e.isDir {
			name += string(filepath.Separator)
		}
		size := ""
		if showSize && !e.isDir && w > 26 {
			size = " " + humanSize(e.size) + " "
		}
		nameW := max(1, w-3-len(size))
		nameTxt := fit(name, nameW)

		var line string
		switch {
		case i == cursor && focused:
			line = styleSelMark.Render("▌") + styleSel.Render(k.glyph+" "+nameTxt+size)
		case i == cursor:
			line = styleSelDim.Render(" " + k.glyph + " " + nameTxt + size)
		default:
			st := lipgloss.NewStyle().Foreground(k.color)
			if e.isDir {
				st = st.Bold(true)
			}
			line = " " + lipgloss.NewStyle().Foreground(k.color).Render(k.glyph) + " " +
				st.Render(nameTxt) + styleDim.Render(size)
		}
		lines = append(lines, fit(line, w))
	}
	return padLines(lines, w, h)
}

func (m *model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	if m.mode == modeEditor && m.editor != nil {
		return m.editor.view()
	}
	if m.mode == modeHelp {
		return m.helpView()
	}
	if m.mode == modeImage {
		return m.imageView()
	}
	if m.mode == modeFind {
		return m.findView()
	}

	t := m.tab()
	w, h := m.width, m.bodyHeight()

	// Column widths: parent | current | preview, with 1-cell separators.
	pw := w * 15 / 100
	if w < 70 {
		pw = 0
	}
	cw := w * 35 / 100
	vw := w - pw - cw - 1
	if pw > 0 {
		vw--
	}

	// Current column.
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+h {
		t.offset = t.cursor - h + 1
	}
	t.offset = max(0, min(t.offset, len(t.entries)-h))
	var cur []string
	if len(t.entries) == 0 {
		msg := "empty folder"
		if t.filter != "" {
			msg = "no matches for “" + t.filter + "”"
		}
		cur = messageLines(msg, cw, h, styleDim.Italic(true))
	} else {
		cur = renderList(t.entries, t.cursor, t.offset, cw, h, true, true)
	}

	// Parent column.
	var par []string
	if pw > 0 {
		pc := -1
		base := filepath.Base(t.dir)
		for i, e := range t.parent {
			if strings.EqualFold(e.name, base) {
				pc = i
				break
			}
		}
		po := max(0, min(pc-h/2, len(t.parent)-h))
		par = renderList(t.parent, pc, po, pw, h, false, false)
	}

	// Preview column.
	var pv []string
	if m.mode == modeOpenWith {
		pv = m.openWithLines(vw, h)
	} else if m.mode == modeSort {
		pv = m.sortLines(vw, h)
	} else if m.mode == modeBookmarks || (m.mode == modePrompt && m.isBookmarkPrompt() && m.bmReturn) {
		pv = m.bookmarkLines(vw, h)
	} else {
		pv = m.previewLines(vw, h)
	}

	sep := styleSep.Render("│")
	var b strings.Builder
	b.WriteString(m.titleBar() + "\n")
	b.WriteString(m.breadcrumb(w) + "\n")
	for i := 0; i < h; i++ {
		if pw > 0 {
			b.WriteString(par[i] + sep)
		}
		b.WriteString(cur[i] + sep + pv[i] + "\n")
	}
	b.WriteString(m.statusLine(w) + "\n")
	b.WriteString(m.footer(w))
	return b.String()
}

func (m *model) titleBar() string {
	left := pill("◆ goterm", cAccent, cBright)
	for i, t := range m.tabs {
		label := fmt.Sprintf("%d %s", i+1, t.label())
		if i == m.active {
			left += styleBar.Render(" ") + pill(label, cPink, cDark)
		} else {
			left += styleBar.Render(" ") + lipgloss.NewStyle().Background(cSubtle).Foreground(cText).Padding(0, 1).Render(label)
		}
	}
	right := ""
	if i := m.bookmarkFor(m.tab().dir); i >= 0 {
		right += lipgloss.NewStyle().Background(cBarBg).Foreground(cYellow).Bold(true).Render("★ "+m.cfg.Bookmarks[i].Name) + styleBar.Render("  ")
	}
	if m.showHidden {
		right += lipgloss.NewStyle().Background(cBarBg).Foreground(cYellow).Render("◌ hidden files shown  ")
	}
	right += styleBarDim.Render("? help ")
	return bar(m.width, left, right)
}

// breadcrumb shows the current folder as colored path segments, trimmed
// from the left when it doesn't fit.
func (m *model) breadcrumb(w int) string {
	t := m.tab()
	vol := filepath.VolumeName(t.dir)
	rest := strings.Trim(t.dir[len(vol):], `\/`)
	segs := []string{}
	if vol != "" {
		segs = append(segs, vol)
	}
	if rest != "" {
		segs = append(segs, strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' })...)
	}
	suffix := ""
	if t.filter != "" {
		suffix = "  " + pill("/ "+t.filter, cYellow, cDark)
	}
	if !m.cfg.Sort.isDefault() {
		suffix += "  " + pill("⇅ "+m.cfg.Sort.label(), cTeal, cDark)
	}
	arrow := styleSep.Render(" › ")
	render := func(from int) string {
		var parts []string
		if from > 0 {
			parts = append(parts, styleDim.Render("…"))
		}
		for i := from; i < len(segs); i++ {
			if i == len(segs)-1 {
				parts = append(parts, styleCrumbEnd.Render(segs[i]))
			} else {
				parts = append(parts, styleCrumb.Render(segs[i]))
			}
		}
		return " " + strings.Join(parts, arrow) + suffix
	}
	for from := 0; from < len(segs); from++ {
		if s := render(from); ansi.StringWidth(s) <= w {
			return fit(s, w)
		}
	}
	return fit(render(max(0, len(segs)-1)), w)
}

func (m *model) modePill() string {
	switch m.mode {
	case modeFilter:
		return pill("FILTER", cYellow, cDark)
	case modePrompt:
		return pill(map[promptKind]string{promptNewFile: "NEW FILE", promptNewDir: "NEW FOLDER", promptRename: "RENAME", promptBookmarkAdd: "BOOKMARK", promptBookmarkRename: "BOOKMARK"}[m.prompt], cTeal, cDark)
	case modeConfirm:
		return pill("CONFIRM", cRed, cDark)
	case modeOpenWith:
		return pill("OPEN WITH", cCyan, cDark)
	case modeBookmarks:
		return pill("★ BOOKMARKS", cYellow, cDark)
	case modeSort:
		return pill("⇅ SORT", cTeal, cDark)
	case modeFind:
		return pill("⌕ FIND", cPink, cDark)
	}
	return pill("BROWSE", cAccent, cBright)
}

func (m *model) statusLine(w int) string {
	t := m.tab()
	left := m.modePill()
	switch {
	case m.status != "" && m.statusErr:
		left += styleBar.Foreground(cRed).Bold(true).Render(" ✗ " + m.status)
	case m.status != "" && m.statusWarn:
		left += styleBar.Foreground(cYellow).Bold(true).Render(" ⚠ " + m.status)
	case m.status != "":
		left += styleBar.Foreground(cGreen).Render(" ✓ " + m.status)
	default:
		if e, ok := t.selected(); ok {
			k := kindOf(e)
			info := " " + e.mod.Format("Jan 02 2006 15:04")
			if !e.isDir {
				info = " " + humanSize(e.size) + " ·" + info
			}
			left += lipgloss.NewStyle().Background(cBarBg).Foreground(k.color).Render(" "+k.glyph+" "+e.name) +
				styleBarDim.Render(" ·"+info)
		}
	}
	right := ""
	if n := len(t.entries); n > 0 {
		right = pill(fmt.Sprintf("%d/%d", t.cursor+1, n), cSubtle, cText)
	}
	return bar(w, left, right)
}

func (m *model) footer(w int) string {
	switch m.mode {
	case modePrompt, modeFilter:
		return fit(" "+m.input.View(), w)
	case modeConfirm:
		return fit(" "+styleErr.Render(m.confirmText)+"  "+styleKey.Render("y")+styleDim.Render(" yes · ")+
			styleKey.Render("any key")+styleDim.Render(" cancel"), w)
	case modeOpenWith:
		return fit(hints(false, [][2]string{{"j/k", "select"}, {"enter/1-9", "open"}, {"esc", "cancel"}}), w)
	case modeBookmarks:
		return fit(hints(false, [][2]string{{"enter/1-9", "jump"}, {"t", "in new tab"}, {"a", "add this folder"},
			{"r", "rename"}, {"d", "remove"}, {"J/K", "reorder"}, {"esc", "close"}}), w)
	}
	return fit(hints(false, [][2]string{
		{"f", "find"}, {"u", "undo"}, {"o", "explorer"}, {"n", "new md"}, {"N", "mkdir"}, {"e", "edit"}, {"E", "open with"},
		{"b", "bookmarks"}, {"S", "sort"}, {"c", "claude"}, {"t", "tab"}, {"/", "filter"},
		{"?", "help"}, {"q q", "quit"},
	}), w)
}

func (m *model) openWithLines(w, h int) []string {
	var rows []string
	rows = append(rows, styleHeading.Render("Open with"), styleDim.Render(filepath.Base(m.openTarget)), "")
	for i, o := range m.openers {
		row := fmt.Sprintf(" %d  %s ", i+1, o.Name)
		if i == m.openCursor {
			rows = append(rows, styleSelMark.Render("▌")+styleSel.Render(row))
		} else {
			rows = append(rows, " "+styleKey.Render(fmt.Sprintf("%d", i+1))+styleText.Render("  "+o.Name))
		}
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(0, 2).Width(min(40, max(10, w-6))).Render(strings.Join(rows, "\n"))
	lines := []string{fit("", w)}
	for _, l := range strings.Split(box, "\n") {
		lines = append(lines, fit("  "+l, w))
	}
	lines = append(lines, fit("", w), fit(styleDim.Render("  edit the list in "+configPath()), w))
	return padLines(lines, w, h)
}

// previewHeader is the first line of the preview column.
func previewHeader(e entry, fi fs.FileInfo, w int, extra string) string {
	k := kindOf(e)
	name := lipgloss.NewStyle().Foreground(k.color).Bold(true).Render(" " + k.glyph + " " + e.name)
	meta := ""
	if !fi.IsDir() {
		meta = humanSize(fi.Size())
	}
	if extra != "" {
		if meta != "" {
			meta += " · "
		}
		meta += extra
	}
	if meta != "" {
		name += styleDim.Render("  " + meta)
	}
	return fit(name, w)
}

// previewLines returns the right-hand column for the selected entry, cached
// by path, width and modification time.
func (m *model) previewLines(w, h int) []string {
	t := m.tab()
	e, ok := t.selected()
	if !ok {
		return padLines(nil, w, h)
	}
	return m.previewFor(e, t.selectedPath(), w, h)
}

// previewFor previews any entry at path (the selection, or a find result).
func (m *model) previewFor(e entry, path string, w, h int) []string {
	fi, err := os.Stat(path)
	if err != nil {
		return messageLines(err.Error(), w, h, styleErr)
	}
	if path != m.pvPath {
		m.pvPath = path
		m.previewScroll = 0
	}
	if !fi.IsDir() && isImage(e.name) {
		return m.imagePreview(e, path, fi, w, h)
	}
	key := fmt.Sprintf("%s|%d|%d|%v|%v", path, w, fi.ModTime().UnixNano(), m.showHidden, m.cfg.Sort)
	if key != m.pvKey {
		m.pvKey = key
		m.pvLines = m.buildPreview(e, path, fi, w)
	}
	head, body := m.pvLines[0], m.pvLines[1:]
	if !fi.IsDir() {
		m.previewScroll = max(0, min(m.previewScroll, len(body)-(h-1)))
		body = body[m.previewScroll:]
	}
	return padLines(append([]string{head}, body...), w, h)
}

func (m *model) buildPreview(e entry, path string, fi fs.FileInfo, w int) []string {
	if fi.IsDir() {
		entries, err := readDir(path, m.opts())
		if err != nil {
			return []string{previewHeader(e, fi, w, ""), fit(styleErr.Render("  "+err.Error()), w)}
		}
		head := previewHeader(e, fi, w, fmt.Sprintf("%d items", len(entries)))
		if len(entries) == 0 {
			return []string{head, fit(styleDim.Italic(true).Render("  empty folder"), w)}
		}
		if len(entries) > 1000 {
			entries = entries[:1000]
		}
		return append([]string{head}, renderList(entries, -1, 0, w, len(entries), false, false)...)
	}

	const maxRead = 256 << 10
	f, err := os.Open(path)
	if err != nil {
		return []string{previewHeader(e, fi, w, ""), fit(styleErr.Render("  "+err.Error()), w)}
	}
	defer f.Close()
	data, _ := io.ReadAll(io.LimitReader(f, maxRead))
	if bytes.IndexByte(data, 0) >= 0 {
		return []string{previewHeader(e, fi, w, "binary"), fit(styleDim.Italic(true).Render("  binary file: press E to open it"), w)}
	}
	if len(data) == 0 {
		return []string{previewHeader(e, fi, w, ""), fit(styleDim.Italic(true).Render("  empty file"), w)}
	}

	if isMarkdown(path) {
		out := []string{previewHeader(e, fi, w, "markdown")}
		for _, l := range strings.Split(m.renderMarkdown(string(data), w), "\n") {
			out = append(out, fit(l, w))
		}
		return out
	}
	text := strings.ReplaceAll(string(data), "\r", "")
	text = strings.ReplaceAll(text, "\t", "    ")
	raw := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := []string{previewHeader(e, fi, w, fmt.Sprintf("%d lines", len(raw)))}
	numW := len(fmt.Sprint(len(raw)))
	for i, l := range raw {
		num := styleSep.Render(fmt.Sprintf(" %*d ", numW, i+1))
		out = append(out, fit(num+styleText.Render(l), w))
	}
	return out
}

func (m *model) helpView() string {
	sections := []struct {
		title string
		color lipgloss.Color
		keys  [][2]string
	}{
		{"Navigate", cBlue, [][2]string{
			{"j/k ↑/↓", "move"}, {"h/l ←/→", "parent / enter folder"}, {"enter", "enter folder or edit file"},
			{"g/G", "top / bottom"}, {"ctrl+d/u", "half page"}, {"enter on image", "full-screen view (j/k flip)"}, {"~", "home folder"},
			{"J/K", "scroll preview"}, {"/", "filter (esc clears)"}, {".", "toggle dotfiles"}, {"R", "refresh"},
		}},
		{"Files", cGreen, [][2]string{
			{"n", "new file (.md by default, opens in editor)"}, {"N", "new folder (a/b/c ok)"},
			{"r / F2", "rename (y to confirm)"}, {"u / ctrl+z", "undo (delete, rename, new, save, …)"}, {"d / del", "send to Recycle Bin"},
			{"e", "edit in built-in editor"}, {"E", "open with... (Notepad++, VS Code, ...)"},
			{"y / Y", "copy path of selection / current folder"},
		}},
		{"Find & sort", cPink, [][2]string{
			{"f / ctrl+f", "find in this folder and all below (fuzzy)"},
			{"", "enter go to · ctrl+o open · ctrl+r re-index"},
			{"S", "sort: name, modified, size, type; reverse"},
		}},
		{"Bookmarks", cYellow, [][2]string{
			{"b", "bookmarks: enter/1-9 jump, t new tab, a add,"}, {"", "r rename, d remove, J/K reorder"},
			{"B", "bookmark the current folder"},
		}},
		{"Tabs & terminal", cMagenta, [][2]string{
			{"t / x", "new tab / close tab"}, {"tab / shift+tab", "next / previous tab"}, {"1-9", "jump to tab"},
			{"c", "Claude in a new Windows Terminal tab here"}, {"s", "shell in a new Windows Terminal tab here"},
			{"o", "show in Explorer (selected)"},
		}},
		{"Editor", cOrange, [][2]string{
			{"ctrl+s", "save"}, {"ctrl+p", "toggle markdown preview"}, {"esc", "close (twice to discard changes)"},
		}},
		{"Quit", cRed, [][2]string{
			{"q q", "quit and cd to this folder (with the gt wrapper)"},
			{"Q Q", "quit without changing folder (ctrl+c twice too)"},
		}},
	}
	render := func(ss []int) string {
		var rows []string
		for _, i := range ss {
			s := sections[i]
			rows = append(rows, lipgloss.NewStyle().Foreground(s.color).Bold(true).Render("▍"+s.title))
			for _, k := range s.keys {
				rows = append(rows, "  "+styleKey.Render(fmt.Sprintf("%-16s", k[0]))+styleText.Render(k[1]))
			}
			rows = append(rows, "")
		}
		return strings.Join(rows[:len(rows)-1], "\n")
	}
	var body string
	if m.width >= 120 {
		// Two columns side by side so it fits a normal-height terminal.
		left := lipgloss.NewStyle().Width(62).Render(render([]int{0, 1, 2}))
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, render([]int{3, 4, 5, 6}))
	} else {
		body = render([]int{0, 1, 2, 3, 4, 5, 6})
	}
	title := pill("◆ goterm", cAccent, cBright) + styleDim.Render("  keyboard reference")
	foot := styleDim.Render("config: "+configPath()) + "\n" + styleDim.Italic(true).Render("press any key to close")
	content := title + "\n\n" + body + "\n\n" + foot
	pad := 1
	if lipgloss.Height(content)+4 > m.height {
		pad = 0
	}
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(cAccent).
		Padding(pad, 3).Render(content)
	placed := lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
	lines := strings.Split(placed, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for i := range lines {
		lines[i] = fit(lines[i], m.width)
	}
	return strings.Join(padLines(lines, m.width, m.height), "\n")
}
