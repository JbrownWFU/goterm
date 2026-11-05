package main

// Global undo (u / ctrl+z): every change goterm makes pushes a step that
// reverses it. Undo pops the most recent step and says what it did.

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const undoDepth = 50

// undoFn reverses one action. It returns a status message, or a command
// for slow work (which reports back with opDoneMsg).
type undoFn func(m *model) (status string, cmd tea.Cmd, err error)

type undoOp struct {
	desc string // "delete of notes.txt", shown if the undo fails
	fn   undoFn
}

func (m *model) pushUndo(desc string, fn undoFn) {
	m.undo = append(m.undo, undoOp{desc: desc, fn: fn})
	if len(m.undo) > undoDepth {
		m.undo = m.undo[len(m.undo)-undoDepth:]
	}
}

func (m *model) undoLast() tea.Cmd {
	if len(m.undo) == 0 {
		m.setStatus("", errors.New("nothing to undo"))
		return nil
	}
	op := m.undo[len(m.undo)-1]
	m.undo = m.undo[:len(m.undo)-1]
	status, cmd, err := op.fn(m)
	if err != nil {
		m.setStatus("", fmt.Errorf("can't undo %s: %v", op.desc, err))
		return nil
	}
	if status != "" {
		left := ""
		if n := len(m.undo); n > 0 {
			left = fmt.Sprintf(" · %d more to undo", n)
		}
		m.setStatus("↶ "+status+left, nil)
	}
	return cmd
}

// reloadAt refreshes the listings, putting the cursor on path if it's in
// the current folder.
func (m *model) reloadAt(path string) {
	focus := ""
	if samePath(filepath.Dir(path), m.tab().dir) {
		focus = filepath.Base(path)
	}
	m.reloadAll(focus)
}

// recycleCmd sends path to the Recycle Bin in the background.
func recycleCmd(path, status string) tea.Cmd {
	return func() tea.Msg {
		if err := recycle(path); err != nil {
			return opDoneMsg{err: err}
		}
		return opDoneMsg{status: status}
	}
}

// --- undo steps for each kind of action ---

func undoDelete(path string, deletedAfter time.Time) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		if err := restoreFromRecycleBin(path, deletedAfter); err != nil {
			return "", nil, err
		}
		m.reloadAt(path)
		return fmt.Sprintf("restored %s from the Recycle Bin", filepath.Base(path)), nil, nil
	}
}

func undoRename(from, to string) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		if _, err := os.Stat(to); err != nil {
			return "", nil, fmt.Errorf("%s is gone", filepath.Base(to))
		}
		if _, err := os.Stat(from); err == nil && !strings.EqualFold(from, to) {
			return "", nil, fmt.Errorf("%s exists again", filepath.Base(from))
		}
		if err := os.Rename(to, from); err != nil {
			return "", nil, err
		}
		m.reloadAt(from)
		return fmt.Sprintf("renamed %s back to %s", filepath.Base(to), filepath.Base(from)), nil, nil
	}
}

// firstMissing returns the topmost folder between base and target that
// doesn't exist yet (what MkdirAll(target) would create first), or "".
func firstMissing(base, target string) string {
	top := ""
	for p := target; len(p) > len(base); p = filepath.Dir(p) {
		if _, err := os.Stat(p); err == nil {
			break
		}
		top = p
		if filepath.Dir(p) == p {
			break
		}
	}
	return top
}

func hasFiles(dir string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// undoCreateFile removes a new file (and any folders made for it): deleted
// outright if it still holds only what goterm put in it, otherwise sent to
// the Recycle Bin.
func undoCreateFile(path string, initial []byte, top string) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		cur, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("%s is gone", filepath.Base(path))
		}
		name := filepath.Base(path)
		if bytes.Equal(cur, initial) {
			if err := os.Remove(path); err != nil {
				return "", nil, err
			}
			if top != "" && !hasFiles(top) {
				_ = os.RemoveAll(top)
			}
			m.reloadAll("")
			return "removed new file " + name, nil, nil
		}
		m.setStatus("working…", nil)
		return "", recycleCmd(path, "↶ sent new file "+name+" to the Recycle Bin (it had been edited)"), nil
	}
}

// undoCreateDir removes a new folder: deleted if nothing was put in it,
// otherwise sent to the Recycle Bin.
func undoCreateDir(top string) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		if _, err := os.Stat(top); err != nil {
			return "", nil, fmt.Errorf("%s is gone", filepath.Base(top))
		}
		name := filepath.Base(top)
		if !hasFiles(top) {
			if err := os.RemoveAll(top); err != nil {
				return "", nil, err
			}
			m.reloadAll("")
			return "removed new folder " + name, nil, nil
		}
		m.setStatus("working…", nil)
		return "", recycleCmd(top, "↶ sent new folder "+name+" to the Recycle Bin (files had been added)"), nil
	}
}

// undoSave puts a file back the way it was before an editor save, unless
// it has changed since.
func undoSave(path string, before, after []byte, existed bool) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		cur, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("%s is gone", filepath.Base(path))
		}
		if !bytes.Equal(cur, after) {
			return "", nil, errors.New("the file has changed since that save")
		}
		if !existed {
			return "", nil, errors.New("the file didn't exist before")
		}
		if err := os.WriteFile(path, before, 0o644); err != nil {
			return "", nil, err
		}
		m.reloadAt(path)
		return fmt.Sprintf("put %s back to how it was before your save", filepath.Base(path)), nil, nil
	}
}

func (m *model) snapshotBookmarks(desc string) {
	old := append([]Bookmark(nil), m.cfg.Bookmarks...)
	m.pushUndo(desc, func(m *model) (string, tea.Cmd, error) {
		m.cfg.Bookmarks = old
		m.bmCursor = max(0, min(m.bmCursor, len(old)-1))
		if err := saveBookmarks(m.cfgPath, old); err != nil {
			return "", nil, err
		}
		return "undid " + desc, nil, nil
	})
}

func (m *model) snapshotSort() {
	old := m.cfg.Sort
	m.pushUndo("sort change", func(m *model) (string, tea.Cmd, error) {
		m.cfg.Sort = old
		m.reloadAll("")
		if err := updateConfigFile(m.cfgPath, func(c *Config) { c.Sort = old }); err != nil {
			return "", nil, err
		}
		return "sort back to " + sortNames[old.By], nil, nil
	})
}

func undoCloseTab(t *tab, at int) undoFn {
	return func(m *model) (string, tea.Cmd, error) {
		at = min(at, len(m.tabs))
		m.tabs = append(m.tabs[:at], append([]*tab{t}, m.tabs[at:]...)...)
		m.active = at
		_ = t.reload(m.opts(), "")
		return "reopened tab " + t.label(), nil, nil
	}
}
