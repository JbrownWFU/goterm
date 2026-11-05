package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	editorMaxBytes = 2 << 20
	editorMaxLines = 10000 // hard limit of bubbles/textarea
)

// editor is the built-in full-screen text editor (e).
type editor struct {
	path     string
	ta       textarea.Model
	original string
	crlf     bool
	isMD     bool

	// onSave is told the file's bytes before and after each save (for undo).
	onSave func(path string, before []byte, existed bool, after []byte)

	confirmDiscard bool
	previewing     bool
	pvLines        []string
	pvOffset       int

	msg    string
	msgErr bool

	render func(src string, width int) string
	w, h   int
}

func newEditor(path string, w, h int, atEnd bool, render func(string, int) string) (*editor, tea.Cmd, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if len(data) > editorMaxBytes {
		return nil, nil, fmt.Errorf("%s is too large for the built-in editor; use E", humanSize(int64(len(data))))
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, nil, errors.New("binary file; use E to open it with another program")
	}
	s := string(data)
	crlf := strings.Contains(s, "\r\n")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if strings.Count(s, "\n") >= editorMaxLines {
		return nil, nil, fmt.Errorf("file has more than %d lines; use E", editorMaxLines)
	}

	ta := textarea.New()
	ta.CharLimit = 0
	ta.MaxHeight = 0
	ta.MaxWidth = 0
	ta.ShowLineNumbers = true
	ta.Prompt = " "
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().Background(cBarBg)
	ta.FocusedStyle.CursorLineNumber = lipgloss.NewStyle().Background(cBarBg).Foreground(cPink).Bold(true)
	ta.FocusedStyle.LineNumber = lipgloss.NewStyle().Foreground(cSubtle)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(cText)
	ta.FocusedStyle.EndOfBuffer = lipgloss.NewStyle().Foreground(cSubtle)
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(cSubtle)
	ta.Cursor.Style = lipgloss.NewStyle().Foreground(cPink)
	ta.SetValue(s)

	ext := strings.ToLower(filepath.Ext(path))
	e := &editor{
		path:     path,
		ta:       ta,
		original: s,
		crlf:     crlf,
		isMD:     ext == ".md" || ext == ".markdown",
		render:   render,
	}
	e.setSize(w, h)
	cmd := e.ta.Focus()
	if !atEnd {
		e.ta, _ = e.ta.Update(tea.KeyMsg{Type: tea.KeyCtrlHome})
	}
	return e, cmd, nil
}

func (e *editor) setSize(w, h int) {
	e.w, e.h = w, h
	e.ta.SetWidth(w)
	e.ta.SetHeight(max(1, h-2))
}

func (e *editor) dirty() bool { return e.ta.Value() != e.original }

func (e *editor) setMsg(s string, isErr bool) { e.msg, e.msgErr = s, isErr }

func (e *editor) save() {
	v := e.ta.Value()
	out := v
	if e.crlf {
		out = strings.ReplaceAll(v, "\n", "\r\n")
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(e.path); err == nil {
		perm = fi.Mode().Perm()
	}
	before, rerr := os.ReadFile(e.path)
	if err := os.WriteFile(e.path, []byte(out), perm); err != nil {
		e.setMsg("save failed: "+err.Error(), true)
		return
	}
	if e.onSave != nil && !bytes.Equal(before, []byte(out)) {
		e.onSave(e.path, before, rerr == nil, []byte(out))
	}
	e.original = v
	e.setMsg("saved "+filepath.Base(e.path), false)
}

// update handles a key. It returns true when the editor should close.
func (e *editor) update(msg tea.KeyMsg) (bool, tea.Cmd) {
	k := msg.String()
	if k != "esc" {
		e.confirmDiscard = false
	}
	switch k {
	case "ctrl+c":
		// Reflexive copy shouldn't close the editor.
		e.setMsg("ctrl+c doesn't copy or quit here · esc closes the editor", true)
		return false, nil
	case "ctrl+s":
		e.save()
		return false, nil
	case "esc":
		if e.previewing {
			e.previewing = false
			return false, nil
		}
		if e.dirty() && !e.confirmDiscard {
			e.confirmDiscard = true
			e.setMsg("unsaved changes: ctrl+s to save, esc again to discard", true)
			return false, nil
		}
		return true, nil
	case "ctrl+p":
		if !e.isMD {
			e.setMsg("preview is only for markdown files", true)
			return false, nil
		}
		e.previewing = !e.previewing
		if e.previewing {
			e.pvLines = strings.Split(e.render(e.ta.Value(), e.w), "\n")
			e.pvOffset = 0
		}
		e.msg = ""
		return false, nil
	}

	if e.previewing {
		page := max(1, e.h-2)
		switch k {
		case "j", "down":
			e.pvOffset++
		case "k", "up":
			e.pvOffset--
		case "ctrl+d", "pgdown", " ":
			e.pvOffset += page / 2
		case "ctrl+u", "pgup":
			e.pvOffset -= page / 2
		case "g", "home":
			e.pvOffset = 0
		case "G", "end":
			e.pvOffset = len(e.pvLines)
		}
		e.pvOffset = min(e.pvOffset, max(0, len(e.pvLines)-page))
		e.pvOffset = max(e.pvOffset, 0)
		return false, nil
	}

	e.msg = ""
	if k == "tab" {
		e.ta.InsertString("  ")
		return false, nil
	}
	var cmd tea.Cmd
	e.ta, cmd = e.ta.Update(msg)
	return false, cmd
}

func (e *editor) view() string {
	// Header: mode pill, file name, modified dot ... folder on the right.
	modePill := pill("EDIT", cGreen, cDark)
	if e.previewing {
		modePill = pill("PREVIEW", cMagenta, cDark)
	}
	k := kindOf(entry{name: filepath.Base(e.path)})
	name := lipgloss.NewStyle().Background(cBarBg).Foreground(k.color).Bold(true).
		Render(" " + k.glyph + " " + filepath.Base(e.path))
	if e.dirty() {
		name += lipgloss.NewStyle().Background(cBarBg).Foreground(cOrange).Render(" ●")
	}
	dir := styleBarDim.Render(filepath.Dir(e.path) + " ")
	header := bar(e.w, modePill+name, dir)

	bodyH := max(1, e.h-2)
	var body string
	if e.previewing {
		lines := make([]string, 0, bodyH)
		for i := e.pvOffset; i < len(e.pvLines) && len(lines) < bodyH; i++ {
			lines = append(lines, fit(e.pvLines[i], e.w))
		}
		body = strings.Join(padLines(lines, e.w, bodyH), "\n")
	} else {
		body = e.ta.View()
	}

	// Footer: message or key hints ... line position pill.
	var left string
	switch {
	case e.msg != "" && e.msgErr:
		left = pill("✗", cRed, cDark) + styleBar.Foreground(cRed).Render(" "+e.msg)
	case e.msg != "":
		left = pill("✓", cGreen, cDark) + styleBar.Foreground(cGreen).Render(" "+e.msg)
	case e.previewing:
		left = hints(true, [][2]string{{"j/k", "scroll"}, {"ctrl+p/esc", "back to editing"}})
	default:
		hs := [][2]string{{"ctrl+s", "save"}, {"esc", "close"}}
		if e.isMD {
			hs = append(hs, [2]string{"ctrl+p", "preview"})
		}
		left = hints(true, hs)
	}
	pos := pill(fmt.Sprintf("ln %d/%d", e.ta.Line()+1, e.ta.LineCount()), cAccent, cBright)
	footer := bar(e.w, left, pos)
	return header + "\n" + body + "\n" + footer
}
