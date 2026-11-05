package main

// Find (f): fuzzy search for files and folders in the current folder and
// everything under it. The tree is walked breadth-first in the background
// and streamed in, so results appear immediately and nearby items come first.

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	findMaxItems = 300_000 // stop indexing after this many entries
	findMaxShown = 1000    // results kept after ranking
	findReuseFor = 2 * time.Minute
)

type found struct {
	rel   string // path relative to the search root
	low   []rune // lowercased rel, for matching
	base  int    // rune index where the base name starts
	isDir bool
	size  int64
	mod   time.Time
}

type findIndex struct {
	root   string
	hidden bool
	items  []found
	done   bool
	capped bool
	built  time.Time
}

type findMatch struct {
	idx   int
	score int
}

type findBatchMsg struct {
	gen    int
	items  []found
	done   bool
	capped bool
}

func newFound(rel string, isDir bool) found {
	low := []rune(strings.ToLower(rel))
	base := 0
	for i := len(low) - 1; i >= 0; i-- {
		if low[i] == '\\' || low[i] == '/' {
			base = i + 1
			break
		}
	}
	return found{rel: rel, low: low, base: base, isDir: isDir}
}

func walkTree(root string, hidden bool, skip map[string]bool, gen int, ch chan<- findBatchMsg, stop <-chan struct{}) {
	defer close(ch)
	send := func(msg findBatchMsg) bool {
		select {
		case ch <- msg:
			return true
		case <-stop:
			return false
		}
	}
	queue := []string{""}
	var batch []found
	last := time.Now()
	total := 0
	for len(queue) > 0 {
		select {
		case <-stop:
			return
		default:
		}
		rel := queue[0]
		queue = queue[1:]
		des, err := os.ReadDir(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for _, d := range des {
			name := d.Name()
			if !hidden && strings.HasPrefix(name, ".") {
				continue
			}
			isDir := d.IsDir() && d.Type()&fs.ModeSymlink == 0
			if isDir && skip[strings.ToLower(name)] {
				continue
			}
			r := filepath.Join(rel, name)
			f := newFound(r, isDir)
			if info, err := d.Info(); err == nil {
				f.size, f.mod = info.Size(), info.ModTime()
			}
			batch = append(batch, f)
			total++
			if isDir {
				queue = append(queue, r)
			}
			if total >= findMaxItems {
				send(findBatchMsg{gen: gen, items: batch, done: true, capped: true})
				return
			}
		}
		if len(batch) >= 5000 || time.Since(last) > 120*time.Millisecond {
			if !send(findBatchMsg{gen: gen, items: batch}) {
				return
			}
			batch, last = nil, time.Now()
		}
	}
	send(findBatchMsg{gen: gen, items: batch, done: true})
}

func nextFindBatch(ch <-chan findBatchMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// --- matching ---

func isWordStart(s []rune, i int) bool {
	if i == 0 {
		return true
	}
	switch s[i-1] {
	case '\\', '/', ' ', '_', '-', '.':
		return true
	}
	return false
}

func indexRunes(s []rune, from int, t []rune) int {
	for i := from; i+len(t) <= len(s); i++ {
		ok := true
		for j := range t {
			if s[i+j] != t[j] {
				ok = false
				break
			}
		}
		if ok {
			return i
		}
	}
	return -1
}

func subseq(s []rune, from int, t []rune, withPos bool) (gaps int, pos []int, ok bool) {
	j, last := 0, -1
	for i := from; i < len(s) && j < len(t); i++ {
		if s[i] == t[j] {
			if last >= 0 {
				gaps += i - last - 1
			}
			last = i
			if withPos {
				pos = append(pos, i)
			}
			j++
		}
	}
	return gaps, pos, j == len(t)
}

func span(start, n int) []int {
	p := make([]int, n)
	for i := range p {
		p[i] = start + i
	}
	return p
}

// matchTerm scores one search term against f: a substring of the file name
// beats a substring of the path, which beats scattered letters.
func matchTerm(f *found, t []rune, withPos bool) (int, []int, bool) {
	if i := indexRunes(f.low, f.base, t); i >= 0 {
		s := 100
		if i == f.base {
			s += 50
			if len(f.low)-f.base == len(t) || f.low[f.base+len(t)] == '.' {
				s += 60 // whole name (or name without extension)
			}
		} else if isWordStart(f.low, i) {
			s += 20
		}
		if withPos {
			return s, span(i, len(t)), true
		}
		return s, nil, true
	}
	if i := indexRunes(f.low, 0, t); i >= 0 {
		s := 60
		if isWordStart(f.low, i) {
			s += 10
		}
		if withPos {
			return s, span(i, len(t)), true
		}
		return s, nil, true
	}
	if gaps, pos, ok := subseq(f.low, f.base, t, withPos); ok {
		return max(5, 40-gaps), pos, true
	}
	if gaps, pos, ok := subseq(f.low, 0, t, withPos); ok {
		return max(1, 15-gaps/2), pos, true
	}
	return 0, nil, false
}

func matchFound(f *found, terms [][]rune, withPos bool) (int, []int, bool) {
	total := 0
	var all []int
	for _, t := range terms {
		s, pos, ok := matchTerm(f, t, withPos)
		if !ok {
			return 0, nil, false
		}
		total += s
		all = append(all, pos...)
	}
	depth := strings.Count(f.rel, string(filepath.Separator))
	return total - 3*depth - len(f.low)/10, all, true
}

func findTerms(q string) [][]rune {
	var out [][]rune
	for _, f := range strings.Fields(strings.ToLower(q)) {
		out = append(out, []rune(f))
	}
	return out
}

// --- model side ---

func (m *model) openFind() tea.Cmd {
	root := m.tab().dir
	m.mode = modeFind
	m.findCur, m.findOff = 0, 0
	m.input.Prompt = "find › "
	m.input.Placeholder = "part of a file or folder name — spaces separate terms"
	m.input.SetValue("")
	focus := m.input.Focus()
	if f := m.find; f != nil && f.done && f.root == root && f.hidden == m.showHidden && time.Since(f.built) < findReuseFor {
		m.refilter()
		return focus
	}
	return tea.Batch(focus, m.startIndex(root))
}

func (m *model) startIndex(root string) tea.Cmd {
	m.stopIndex()
	m.findGen++
	m.find = &findIndex{root: root, hidden: m.showHidden}
	skip := map[string]bool{}
	for _, s := range m.cfg.FindSkip {
		skip[strings.ToLower(s)] = true
	}
	ch, stop := make(chan findBatchMsg, 4), make(chan struct{})
	m.findCh, m.findStop = ch, stop
	go walkTree(root, m.showHidden, skip, m.findGen, ch, stop)
	m.refilter()
	return nextFindBatch(ch)
}

func (m *model) stopIndex() {
	if m.findStop != nil {
		close(m.findStop)
		m.findStop = nil
	}
}

func (m *model) closeFind() {
	m.mode = modeNormal
	m.input.Blur()
	if m.find != nil && !m.find.done {
		m.stopIndex()
		m.find = nil
		m.findGen++
	}
}

func (m *model) handleFindBatch(msg findBatchMsg) tea.Cmd {
	if msg.gen != m.findGen || m.find == nil {
		return nil
	}
	m.find.items = append(m.find.items, msg.items...)
	if msg.done {
		m.find.done, m.find.capped, m.find.built = true, msg.capped, time.Now()
		m.findStop = nil
	}
	if m.mode == modeFind {
		m.refilter()
	}
	if msg.done {
		return nil
	}
	return nextFindBatch(m.findCh)
}

func (m *model) refilter() {
	m.findRes = m.findRes[:0]
	m.findTotal = 0
	if m.find == nil {
		return
	}
	items := m.find.items
	terms := findTerms(m.input.Value())
	if len(terms) == 0 {
		m.findTotal = len(items)
		for i := 0; i < len(items) && i < findMaxShown; i++ {
			m.findRes = append(m.findRes, findMatch{idx: i})
		}
	} else {
		for i := range items {
			if s, _, ok := matchFound(&items[i], terms, false); ok {
				m.findRes = append(m.findRes, findMatch{idx: i, score: s})
			}
		}
		m.findTotal = len(m.findRes)
		sort.SliceStable(m.findRes, func(a, b int) bool { return m.findRes[a].score > m.findRes[b].score })
		if len(m.findRes) > findMaxShown {
			m.findRes = m.findRes[:findMaxShown]
		}
	}
	m.findCur = max(0, min(m.findCur, len(m.findRes)-1))
}

func (m *model) findSelected() (found, string, bool) {
	if m.find == nil || m.findCur >= len(m.findRes) {
		return found{}, "", false
	}
	f := m.find.items[m.findRes[m.findCur].idx]
	return f, filepath.Join(m.find.root, f.rel), true
}

func (m *model) updateFind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	page := max(1, m.bodyHeight()-1)
	switch msg.String() {
	case "esc", "ctrl+c":
		m.closeFind()
		return m, nil
	case "enter":
		return m, m.findJump(false)
	case "ctrl+o":
		return m, m.findJump(true)
	case "up", "ctrl+p", "shift+tab":
		m.findCur = max(0, m.findCur-1)
		return m, nil
	case "down", "ctrl+n", "tab":
		m.findCur = max(0, min(m.findCur+1, len(m.findRes)-1))
		return m, nil
	case "pgup":
		m.findCur = max(0, m.findCur-page)
		return m, nil
	case "pgdown":
		m.findCur = max(0, min(m.findCur+page, len(m.findRes)-1))
		return m, nil
	case "ctrl+r":
		m.findCur = 0
		return m, m.startIndex(m.tab().dir)
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m.findCur, m.findOff = 0, 0
		m.refilter()
	}
	return m, cmd
}

// findJump goes to the selected result: folders are entered, files are
// selected in their folder. With open, files are also opened.
func (m *model) findJump(open bool) tea.Cmd {
	f, abs, ok := m.findSelected()
	if !ok {
		return nil
	}
	m.closeFind()
	if f.isDir {
		m.cdTo(abs, "")
		m.setStatus("→ "+f.rel, nil)
		return nil
	}
	m.cdTo(filepath.Dir(abs), filepath.Base(abs))
	m.setStatus("→ "+f.rel, nil)
	if !open {
		return nil
	}
	if isImage(abs) {
		m.openImage(abs)
		return nil
	}
	return m.openEditor(abs, false)
}

// --- view ---

func (m *model) findRow(f *found, terms [][]rune, w int, selected bool) string {
	k := kindOf(entry{name: string(f.low[f.base:]), isDir: f.isDir})
	runes := []rune(f.rel)
	if f.isDir {
		runes = append(runes, filepath.Separator)
	}
	hit := map[int]bool{}
	if len(terms) > 0 {
		_, pos, _ := matchFound(f, terms, true)
		for _, p := range pos {
			hit[p] = true
		}
	}

	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(cSelBg)
		}
		return s
	}
	dim := bg(lipgloss.NewStyle().Foreground(cMuted))
	name := bg(lipgloss.NewStyle().Foreground(k.color).Bold(f.isDir))
	match := bg(lipgloss.NewStyle().Foreground(cPink).Bold(true).Underline(true))

	budget := w - 4 // marker, glyph, space, right margin
	start, prefix := 0, ""
	if len(runes) > budget && budget > 1 {
		start, prefix = len(runes)-(budget-1), "…"
	}
	var b strings.Builder
	b.WriteString(dim.Render(prefix))
	class := func(i int) int {
		switch {
		case hit[i]:
			return 2
		case i >= f.base:
			return 1
		}
		return 0
	}
	styles := []lipgloss.Style{dim, name, match}
	for i := start; i < len(runes); {
		c := class(i)
		j := i
		for j < len(runes) && class(j) == c {
			j++
		}
		b.WriteString(styles[c].Render(string(runes[i:j])))
		i = j
	}
	glyph := bg(lipgloss.NewStyle().Foreground(k.color)).Render(k.glyph + " ")
	if selected {
		row := styleSelMark.Render("▌") + glyph + b.String()
		if pad := w - ansi.StringWidth(row); pad > 0 {
			row += styleSel.Render(strings.Repeat(" ", pad))
		}
		return fit(row, w)
	}
	return fit(" "+glyph+b.String(), w)
}

func (m *model) findView() string {
	w, h := m.width, m.bodyHeight()
	lw := w * 55 / 100
	if w < 70 {
		lw = w
	}
	vw := w - lw - 1

	if m.findCur < m.findOff {
		m.findOff = m.findCur
	}
	if m.findCur >= m.findOff+h {
		m.findOff = m.findCur - h + 1
	}
	terms := findTerms(m.input.Value())
	var left []string
	for i := m.findOff; i < len(m.findRes) && len(left) < h; i++ {
		left = append(left, m.findRow(&m.find.items[m.findRes[i].idx], terms, lw, i == m.findCur))
	}
	if len(left) == 0 {
		msg := "no matches"
		if m.find != nil && !m.find.done {
			msg = "searching…"
		}
		left = append(left, fit(styleDim.Italic(true).Render("  "+msg), lw))
	}
	left = padLines(left, lw, h)

	var right []string
	if vw > 0 {
		if f, abs, ok := m.findSelected(); ok {
			right = m.previewFor(entry{name: filepath.Base(abs), isDir: f.isDir, size: f.size, mod: f.mod}, abs, vw, h)
		} else {
			right = padLines(nil, vw, h)
		}
	}

	var b strings.Builder
	b.WriteString(m.titleBar() + "\n")
	b.WriteString(m.breadcrumb(w) + "\n")
	sep := styleSep.Render("│")
	for i := 0; i < h; i++ {
		b.WriteString(left[i])
		if vw > 0 {
			b.WriteString(sep + right[i])
		}
		b.WriteString("\n")
	}

	info := ""
	if m.find != nil {
		n := len(m.find.items)
		switch {
		case !m.find.done:
			info = lipgloss.NewStyle().Background(cBarBg).Foreground(cYellow).Render(fmt.Sprintf(" indexing… %s items", commas(n)))
		case m.find.capped:
			info = lipgloss.NewStyle().Background(cBarBg).Foreground(cYellow).Render(fmt.Sprintf(" stopped at %s items (search a smaller folder)", commas(n)))
		default:
			info = styleBarDim.Render(fmt.Sprintf(" %s items in this folder and below", commas(n)))
		}
	}
	right2 := ""
	if len(terms) > 0 {
		right2 = pill(fmt.Sprintf("%s matches", commas(m.findTotal)), cSubtle, cText)
	}
	b.WriteString(bar(w, m.modePill()+info, right2) + "\n")
	in := " " + m.input.View()
	hint := hints(false, [][2]string{{"↑/↓", "select"}, {"enter", "go to"}, {"ctrl+o", "open"}, {"esc", "cancel"}})
	if gap := w - lipgloss.Width(in) - lipgloss.Width(hint); gap > 0 {
		in += strings.Repeat(" ", gap) + hint
	}
	b.WriteString(fit(in, w))
	return b.String()
}

func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
