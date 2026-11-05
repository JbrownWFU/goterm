package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type mode int

const (
	modeNormal mode = iota
	modePrompt
	modeConfirm
	modeFilter
	modeOpenWith
	modeEditor
	modeHelp
	modeBookmarks
	modeImage
	modeSort
	modeFind
)

type promptKind int

const (
	promptNewFile promptKind = iota
	promptNewDir
	promptRename
	promptBookmarkAdd
	promptBookmarkRename
)

// opDoneMsg reports the result of a background operation (e.g. delete).
type opDoneMsg struct {
	status string
	err    error
	focus  string
	undo   *undoOp // pushed if the operation succeeded
}

type model struct {
	cfg        Config
	tabs       []*tab
	active     int
	mode       mode
	showHidden bool

	input       textinput.Model
	prompt      promptKind
	confirmText string
	confirmFn   func() tea.Cmd

	openers    []Opener
	openCursor int
	openTarget string

	cfgPath   string // where bookmarks are saved
	bmCursor  int
	bmPending string // folder being bookmarked
	bmReturn  bool   // go back to the bookmark panel after the prompt

	imgs       map[string]*imgEntry // decoded thumbnails by imgKey
	imgOrder   []string
	imgLoading map[string]string
	imgRKey    string // last rendered image block
	imgRLines  []string
	imgPath    string // full-screen image view

	find      *findIndex // recursive file index for f
	findGen   int
	findCh    chan findBatchMsg
	findStop  chan struct{}
	findRes   []findMatch
	findTotal int
	findCur   int
	findOff   int

	editor *editor

	status     string
	statusErr  bool
	statusWarn bool // "press again" prompts

	armed string   // key waiting for its second press (quit, remove bookmark)
	undo  []undoOp // what u / ctrl+z reverses, newest last

	width, height int

	previewScroll int
	pvKey, pvPath string
	pvLines       []string

	md      *glamour.TermRenderer
	mdWidth int

	quitCD bool // write the final dir for the shell wrapper
}

func newModel(cfg Config, dir string) *model {
	ti := textinput.New()
	ti.CharLimit = 255
	ti.PromptStyle = lipgloss.NewStyle().Foreground(cPink).Bold(true)
	ti.TextStyle = lipgloss.NewStyle().Foreground(cBright)
	ti.PlaceholderStyle = styleDim
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(cPink)
	m := &model{
		cfg:        cfg,
		input:      ti,
		showHidden: cfg.ShowHidden,
		openers:    availableOpeners(cfg.Openers),
		cfgPath:    configPath(),
		imgs:       map[string]*imgEntry{},
		imgLoading: map[string]string{},
	}
	m.tabs = []*tab{m.newTab(dir)}
	return m
}

func (m *model) newTab(dir string) *tab {
	t := &tab{}
	if err := t.cd(dir, m.opts(), ""); err != nil {
		home, _ := os.UserHomeDir()
		_ = t.cd(home, m.opts(), "")
		m.setStatus("", err)
	}
	return t
}

func (m *model) tab() *tab { return m.tabs[m.active] }

func (m *model) Init() tea.Cmd { return nil }

func (m *model) setStatus(s string, err error) {
	m.statusWarn = false
	if err != nil {
		m.status, m.statusErr = err.Error(), true
		return
	}
	m.status, m.statusErr = s, false
}

func (m *model) bodyHeight() int { return max(1, m.height-4) }

func (m *model) reloadAll(focus string) {
	for i, t := range m.tabs {
		f := ""
		if i == m.active {
			f = focus
		}
		if err := t.reload(m.opts(), f); err != nil && i == m.active {
			m.setStatus("", err)
		}
	}
	m.pvKey = ""
}

func (m *model) cdTo(dir, focus string) {
	if err := m.tab().cd(dir, m.opts(), focus); err != nil {
		m.setStatus("", err)
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case imgLoadedMsg:
		m.storeImage(msg)
		return m, nil
	case findBatchMsg:
		cmd := m.handleFindBatch(msg)
		return m, tea.Batch(cmd, m.wantImage())
	}
	mm, cmd := m.update(msg)
	if c := m.wantImage(); c != nil {
		cmd = tea.Batch(cmd, c)
	}
	return mm, cmd
}

func (m *model) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.editor != nil {
			m.editor.setSize(m.width, m.height)
		}
		return m, nil

	case opDoneMsg:
		m.setStatus(msg.status, msg.err)
		if msg.err == nil && msg.undo != nil {
			m.undo = append(m.undo, *msg.undo)
		}
		m.reloadAll(msg.focus)
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case modeEditor:
			return m.updateEditor(msg)
		case modePrompt:
			return m.updatePrompt(msg)
		case modeFilter:
			return m.updateFilter(msg)
		case modeConfirm:
			return m.updateConfirm(msg)
		case modeOpenWith:
			return m.updateOpenWith(msg)
		case modeBookmarks:
			return m.updateBookmarks(msg)
		case modeImage:
			return m.updateImage(msg)
		case modeSort:
			return m.updateSort(msg)
		case modeFind:
			return m.updateFind(msg)
		case modeHelp:
			m.mode = modeNormal
			return m, nil
		default:
			return m.updateNormal(msg)
		}
	}

	// Non-key messages (cursor blink etc.) go to whatever has focus.
	var cmd tea.Cmd
	switch m.mode {
	case modeEditor:
		if m.editor != nil {
			m.editor.ta, cmd = m.editor.ta.Update(msg)
		}
	case modePrompt, modeFilter, modeFind:
		m.input, cmd = m.input.Update(msg)
	}
	return m, cmd
}

func (m *model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.tab()
	m.status, m.statusWarn = "", false
	k := msg.String()
	armed := m.armed
	m.armed = ""

	switch k {
	// Quitting takes two presses of the same key, so a stray q or a
	// reflexive ctrl+c (copy) doesn't close everything.
	case "q":
		if armed == "q" {
			m.quitCD = true
			return m, tea.Quit
		}
		m.armed = "q"
		m.warn("press q again to quit (any other key cancels)")
	case "Q", "ctrl+c":
		if armed == k {
			m.quitCD = false
			return m, tea.Quit
		}
		m.armed = k
		m.warn(fmt.Sprintf("press %s again to quit without changing folder (q q quits and cds here)", k))
	case "u", "ctrl+z":
		return m, m.undoLast()

	// navigation
	case "j", "down":
		t.move(1)
	case "k", "up":
		t.move(-1)
	case "g", "home":
		t.cursor = 0
	case "G", "end":
		t.cursor = len(t.entries) - 1
		t.clamp()
	case "ctrl+d", "pgdown":
		t.move(m.bodyHeight() / 2)
	case "ctrl+u", "pgup":
		t.move(-m.bodyHeight() / 2)
	case "h", "left", "backspace":
		if p, ok := t.parentDir(); ok {
			m.cdTo(p, filepath.Base(t.dir))
		}
	case "l", "right", "enter":
		if e, ok := t.selected(); ok {
			if e.isDir {
				m.cdTo(t.selectedPath(), "")
			} else if isImage(e.name) {
				m.openImage(t.selectedPath())
			} else {
				return m, m.openEditor(t.selectedPath(), false)
			}
		}
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			m.cdTo(home, "")
		}
	case "J":
		m.previewScroll += 3
	case "K":
		m.previewScroll = max(0, m.previewScroll-3)
	case ".":
		m.showHidden = !m.showHidden
		m.reloadAll("")
		m.setStatus(map[bool]string{true: "showing hidden files", false: "hiding dotfiles"}[m.showHidden], nil)
	case "R", "ctrl+r":
		m.reloadAll("")
		m.setStatus("refreshed", nil)

	// filter
	case "/":
		m.mode = modeFilter
		m.input.Prompt = "/ "
		m.input.Placeholder = "type to filter"
		m.input.SetValue(t.filter)
		m.input.CursorEnd()
		return m, m.input.Focus()
	case "esc":
		if t.filter != "" {
			name := ""
			if e, ok := t.selected(); ok {
				name = e.name
			}
			t.filter = ""
			t.applyFilter()
			t.focus(name)
		}

	// file operations
	case "n":
		return m, m.startPrompt(promptNewFile, "new file › ", "", "name (.md added if no extension)")
	case "N":
		return m, m.startPrompt(promptNewDir, "new folder › ", "", "name (a/b/c works)")
	case "r", "f2":
		if e, ok := t.selected(); ok {
			return m, m.startPrompt(promptRename, "rename › ", e.name, "")
		}
	case "d", "delete":
		if e, ok := t.selected(); ok {
			path := t.selectedPath()
			what := "file"
			if e.isDir {
				what = "folder"
				if !dirIsEmpty(path) {
					what = "folder and everything in it"
				}
			}
			m.confirm(fmt.Sprintf("send %s %q to the Recycle Bin?", what, e.name), func() tea.Cmd {
				name := e.name
				return func() tea.Msg {
					start := time.Now().Add(-2 * time.Second)
					if err := recycle(path); err != nil {
						return opDoneMsg{err: err}
					}
					return opDoneMsg{
						status: "recycled " + name + " · u restores it",
						undo:   &undoOp{desc: "delete of " + name, fn: undoDelete(path, start)},
					}
				}
			})
		}
	case "e":
		if e, ok := t.selected(); ok {
			if e.isDir {
				m.setStatus("", fmt.Errorf("e edits files; use E to open a folder in another program"))
			} else if isImage(e.name) {
				m.openImage(t.selectedPath())
			} else {
				return m, m.openEditor(t.selectedPath(), false)
			}
		}
	case "E":
		target := t.selectedPath()
		if target == "" {
			target = t.dir
		}
		if len(m.openers) == 0 {
			m.setStatus("", fmt.Errorf("no openers available; edit %s", configPath()))
		} else {
			m.openTarget, m.openCursor, m.mode = target, 0, modeOpenWith
		}

	// clipboard
	case "y":
		p := t.selectedPath()
		if p == "" {
			p = t.dir
		}
		m.copy(p)
	case "Y":
		m.copy(t.dir)

	// tabs
	case "t":
		m.tabs = append(m.tabs, m.newTab(t.dir))
		m.active = len(m.tabs) - 1
	case "x":
		if len(m.tabs) > 1 {
			m.pushUndo("tab close", undoCloseTab(t, m.active))
			m.setStatus("closed tab "+t.label()+" · u reopens it", nil)
			m.tabs = append(m.tabs[:m.active:m.active], m.tabs[m.active+1:]...)
			m.active = min(m.active, len(m.tabs)-1)
		} else {
			m.setStatus("", fmt.Errorf("can't close the last tab (q quits)"))
		}
	case "tab", "]":
		m.active = (m.active + 1) % len(m.tabs)
	case "shift+tab", "[":
		m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < len(m.tabs) {
			m.active = i
		}

	// external
	case "c":
		if err := openTerminalTab(t.dir, m.cfg.ClaudeCommand); err != nil {
			m.setStatus("", err)
		} else {
			m.setStatus("opened Claude in a new terminal tab", nil)
		}
	case "s":
		if err := openTerminalTab(t.dir, m.cfg.ShellCommand); err != nil {
			m.setStatus("", err)
		} else {
			m.setStatus("opened a shell in a new terminal tab", nil)
		}
	case "o":
		if p := t.selectedPath(); p != "" {
			if err := revealInExplorer(p); err != nil {
				m.setStatus("", err)
			} else {
				m.setStatus("opened Explorer with "+filepath.Base(p)+" selected", nil)
			}
		} else if err := openInExplorer(t.dir); err != nil {
			m.setStatus("", err)
		} else {
			m.setStatus("opened this folder in Explorer", nil)
		}

	case "?":
		m.mode = modeHelp

	// bookmarks
	case "b", "'":
		m.openBookmarks()
	case "B":
		return m, m.startAddBookmark(t.dir, false)

	// sort & find
	case "S":
		m.mode = modeSort
	case "f", "ctrl+f":
		return m, m.openFind()
	}
	return m, nil
}

func (m *model) copy(s string) {
	if err := clipboard.WriteAll(s); err != nil {
		m.setStatus("", err)
		return
	}
	m.setStatus("copied "+s, nil)
}

func (m *model) confirm(text string, fn func() tea.Cmd) {
	m.mode, m.confirmText, m.confirmFn = modeConfirm, text, fn
}

func (m *model) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	fn := m.confirmFn
	m.confirmFn = nil
	if (msg.String() == "y" || msg.String() == "Y") && fn != nil {
		m.setStatus("working…", nil)
		return m, fn()
	}
	m.setStatus("cancelled", nil)
	return m, nil
}

func (m *model) isBookmarkPrompt() bool {
	return m.prompt == promptBookmarkAdd || m.prompt == promptBookmarkRename
}

func (m *model) startPrompt(kind promptKind, label, value, placeholder string) tea.Cmd {
	m.mode, m.prompt = modePrompt, kind
	m.input.Prompt = label
	m.input.Placeholder = placeholder
	m.input.SetValue(value)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *model) updatePrompt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeNormal
		m.input.Blur()
		if m.isBookmarkPrompt() && m.bmReturn {
			m.mode = modeBookmarks
		}
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		m.input.Blur()
		if m.isBookmarkPrompt() {
			if val != "" {
				m.finishBookmarkPrompt(m.prompt, val)
			} else if m.bmReturn {
				m.mode = modeBookmarks
			}
			return m, nil
		}
		if val == "" {
			return m, nil
		}
		switch m.prompt {
		case promptNewFile:
			return m, m.createFile(val)
		case promptNewDir:
			m.createDir(val)
		case promptRename:
			m.confirmRename(val)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// firstComponent returns the first path element of a relative name, so that
// "notes/today.md" focuses "notes" in the listing.
func firstComponent(name string) string {
	parts := strings.FieldsFunc(filepath.Clean(name), func(r rune) bool { return r == '/' || r == '\\' })
	if len(parts) == 0 {
		return name
	}
	return parts[0]
}

func (m *model) createFile(name string) tea.Cmd {
	t := m.tab()
	if filepath.Ext(name) == "" {
		name += ".md"
	}
	path := filepath.Join(t.dir, name)
	if _, err := os.Stat(path); err == nil {
		m.setStatus("", fmt.Errorf("%s already exists", name))
		return nil
	}
	top := firstMissing(t.dir, filepath.Dir(path))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		m.setStatus("", err)
		return nil
	}
	content := ""
	if isMarkdown(path) {
		content = expandTemplate(m.cfg.NewFileTemplate, name)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		m.setStatus("", err)
		return nil
	}
	m.pushUndo("new file "+name, undoCreateFile(path, []byte(content), top))
	m.reloadAll(firstComponent(name))
	return m.openEditor(path, true)
}

func (m *model) createDir(name string) {
	path := filepath.Join(m.tab().dir, name)
	if _, err := os.Stat(path); err == nil {
		m.setStatus("", fmt.Errorf("%s already exists", name))
		return
	}
	top := firstMissing(m.tab().dir, path)
	if err := os.MkdirAll(path, 0o755); err != nil {
		m.setStatus("", err)
		return
	}
	m.pushUndo("new folder "+name, undoCreateDir(top))
	m.reloadAll(firstComponent(name))
	m.setStatus("created "+name+" · u removes it", nil)
}

func (m *model) warn(s string) {
	m.status, m.statusErr, m.statusWarn = s, false, true
}

// confirmRename asks before renaming, so keys typed into an accidentally
// opened rename prompt can't silently change a name.
func (m *model) confirmRename(newName string) {
	t := m.tab()
	e, ok := t.selected()
	if !ok {
		return
	}
	if newName == e.name {
		m.setStatus("name unchanged", nil)
		return
	}
	if strings.ContainsAny(newName, `/\`) {
		m.setStatus("", fmt.Errorf("rename can't move files between folders"))
		return
	}
	oldPath := t.selectedPath()
	m.confirm(fmt.Sprintf("rename %q → %q?", e.name, newName), func() tea.Cmd {
		m.rename(oldPath, newName)
		return nil
	})
}

func (m *model) rename(oldPath, newName string) {
	oldName := filepath.Base(oldPath)
	newPath := filepath.Join(filepath.Dir(oldPath), newName)
	// A case-only rename on Windows stats as "exists", so allow that.
	if _, err := os.Stat(newPath); err == nil && !strings.EqualFold(newName, oldName) {
		m.setStatus("", fmt.Errorf("%s already exists", newName))
		return
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		m.setStatus("", err)
		return
	}
	m.pushUndo("rename of "+oldName, undoRename(oldPath, newPath))
	m.reloadAll(newName)
	m.setStatus(fmt.Sprintf("renamed %s → %s · u undoes", oldName, newName), nil)
}

func (m *model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.tab()
	switch msg.String() {
	case "esc", "ctrl+c":
		t.filter = ""
		t.applyFilter()
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case "enter":
		m.mode = modeNormal
		m.input.Blur()
		return m, nil
	case "down", "ctrl+n":
		t.move(1)
		return m, nil
	case "up", "ctrl+p":
		t.move(-1)
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if v := m.input.Value(); v != t.filter {
		t.filter = v
		t.cursor = 0
		t.applyFilter()
	}
	return m, cmd
}

func (m *model) updateOpenWith(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "esc", "q", "ctrl+c":
		m.mode = modeNormal
	case "j", "down":
		m.openCursor = min(m.openCursor+1, len(m.openers)-1)
	case "k", "up":
		m.openCursor = max(m.openCursor-1, 0)
	case "enter", "l":
		m.launch(m.openCursor)
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if i := int(k[0] - '1'); i < len(m.openers) {
			m.launch(i)
		}
	}
	return m, nil
}

func (m *model) launch(i int) {
	m.mode = modeNormal
	o := m.openers[i]
	if err := launchOpener(o, m.openTarget); err != nil {
		m.setStatus("", fmt.Errorf("%s: %w", o.Name, err))
		return
	}
	m.setStatus(fmt.Sprintf("opened %s in %s", filepath.Base(m.openTarget), o.Name), nil)
}

func (m *model) openEditor(path string, atEnd bool) tea.Cmd {
	ed, cmd, err := newEditor(path, m.width, m.height, atEnd, m.renderMarkdown)
	if err != nil {
		m.setStatus("", err)
		return nil
	}
	ed.onSave = func(p string, before []byte, existed bool, after []byte) {
		if existed {
			m.pushUndo("save of "+filepath.Base(p), undoSave(p, before, after, existed))
		}
	}
	m.editor, m.mode = ed, modeEditor
	return cmd
}

func (m *model) updateEditor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	closed, cmd := m.editor.update(msg)
	if closed {
		m.editor = nil
		m.mode = modeNormal
		m.reloadAll("")
	}
	return m, cmd
}

func (m *model) renderMarkdown(src string, w int) string {
	ww := max(20, w-4)
	if m.md == nil || m.mdWidth != ww {
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(m.cfg.GlamourStyle),
			glamour.WithWordWrap(ww),
		)
		if err != nil {
			return src
		}
		m.md, m.mdWidth = r, ww
	}
	out, err := m.md.Render(src)
	if err != nil {
		return src
	}
	return strings.Trim(out, "\n")
}
