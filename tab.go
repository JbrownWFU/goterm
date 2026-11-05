package main

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

type entry struct {
	name  string
	isDir bool
	size  int64
	mod   time.Time
}

// tab is one independent browsing location.
type tab struct {
	dir     string
	all     []entry // everything in dir
	entries []entry // after filter
	parent  []entry // contents of the parent dir (left column)
	cursor  int
	offset  int
	filter  string
}

func readDir(dir string, o listOpts) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(des))
	for _, d := range des {
		name := d.Name()
		if !o.hidden && strings.HasPrefix(name, ".") {
			continue
		}
		e := entry{name: name, isDir: d.IsDir()}
		if info, err := d.Info(); err == nil {
			e.size, e.mod = info.Size(), info.ModTime()
		}
		// Symlinks / junctions: classify by what they point to.
		if d.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(dir, name)); err == nil {
				e.isDir = fi.IsDir()
			}
		}
		out = append(out, e)
	}
	sortEntries(out, o.sort)
	return out, nil
}

// cd switches the tab to dir. On error the tab is left unchanged.
func (t *tab) cd(dir string, o listOpts, focus string) error {
	all, err := readDir(dir, o)
	if err != nil {
		return err
	}
	t.dir, t.all, t.filter, t.cursor, t.offset = dir, all, "", 0, 0
	t.loadParent(o)
	t.applyFilter()
	if focus != "" {
		t.focus(focus)
	}
	return nil
}

// reload re-reads the directory, keeping the cursor on focus (or the
// currently selected entry if focus is empty).
func (t *tab) reload(o listOpts, focus string) error {
	if focus == "" {
		if e, ok := t.selected(); ok {
			focus = e.name
		}
	}
	all, err := readDir(t.dir, o)
	if err != nil {
		return err
	}
	t.all = all
	t.loadParent(o)
	t.applyFilter()
	if !t.focus(focus) {
		t.clamp()
	}
	return nil
}

func (t *tab) loadParent(o listOpts) {
	t.parent = nil
	if p, ok := t.parentDir(); ok {
		t.parent, _ = readDir(p, o)
	}
}

func (t *tab) parentDir() (string, bool) {
	p := filepath.Dir(t.dir)
	return p, p != t.dir
}

func (t *tab) applyFilter() {
	if t.filter == "" {
		t.entries = t.all
	} else {
		f := strings.ToLower(t.filter)
		t.entries = make([]entry, 0, len(t.all))
		for _, e := range t.all {
			if strings.Contains(strings.ToLower(e.name), f) {
				t.entries = append(t.entries, e)
			}
		}
	}
	t.clamp()
}

func (t *tab) focus(name string) bool {
	for i, e := range t.entries {
		if strings.EqualFold(e.name, name) {
			t.cursor = i
			return true
		}
	}
	return false
}

func (t *tab) move(d int) {
	t.cursor += d
	t.clamp()
}

func (t *tab) clamp() {
	if t.cursor >= len(t.entries) {
		t.cursor = len(t.entries) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

func (t *tab) selected() (entry, bool) {
	if t.cursor < 0 || t.cursor >= len(t.entries) {
		return entry{}, false
	}
	return t.entries[t.cursor], true
}

func (t *tab) selectedPath() string {
	if e, ok := t.selected(); ok {
		return filepath.Join(t.dir, e.name)
	}
	return ""
}

func (t *tab) label() string {
	b := filepath.Base(t.dir)
	if b == "" || b == string(filepath.Separator) || b == "." {
		return t.dir
	}
	return b
}
