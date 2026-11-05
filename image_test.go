package main

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func writePNG(t *testing.T, path string, w, h int, c color.Color) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	must(t, err)
	defer f.Close()
	must(t, png.Encode(f, img))
}

// waitImages runs pending decodes synchronously, as the program would.
func waitImages(m *model) {
	for key, path := range m.imgLoading {
		m.Update(loadImage(path, key)())
	}
}

func TestImagePreviewAndViewer(t *testing.T) {
	m, dir := setup(t)
	writePNG(t, filepath.Join(dir, "a_red.png"), 40, 20, color.RGBA{255, 0, 0, 255})
	writePNG(t, filepath.Join(dir, "b_clear.png"), 8, 8, color.RGBA{0, 0, 0, 0})
	f, _ := os.Create(filepath.Join(dir, "c_photo.jpg"))
	big := image.NewRGBA(image.Rect(0, 0, 3000, 2000))
	must(t, jpeg.Encode(f, big, nil))
	f.Close()
	must(t, os.WriteFile(filepath.Join(dir, "d_bad.png"), []byte("not really a png"), 0o644))
	m.reloadAll("")

	// Selecting an image starts a background decode and shows "loading".
	m.tab().focus("a_red.png")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	if cmd == nil || len(m.imgLoading) != 1 {
		t.Fatal("expected an image load command")
	}
	if v := checkView(t, m); !strings.Contains(v, "loading") {
		t.Error("loading state missing")
	}
	waitImages(m)
	raw := m.View()
	v := checkView(t, m)
	if !strings.Contains(v, "40×20 png") || !strings.Contains(v, "▀") {
		t.Errorf("image preview missing:\n%s", v)
	}
	if !strings.Contains(raw, "38;2;255;0;0") {
		t.Error("red pixels not drawn in truecolor")
	}

	// Transparent pixels are flattened onto the theme background.
	m.tab().focus("b_clear.png")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	waitImages(m)
	if raw := m.View(); !strings.Contains(raw, "38;2;26;27;38") {
		t.Error("transparent image should show the theme background")
	}

	// A large photo decodes in reasonable time.
	m.tab().focus("c_photo.jpg")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	start := time.Now()
	waitImages(m)
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("decoding 6MP took %v", d)
	}
	if v := checkView(t, m); !strings.Contains(v, "3000×2000 jpeg") {
		t.Errorf("jpeg preview missing:\n%s", v)
	}

	// Broken image: error, not a crash.
	m.tab().focus("d_bad.png")
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	waitImages(m)
	if v := checkView(t, m); !strings.Contains(v, "not a supported image format") {
		t.Errorf("bad image error missing:\n%s", v)
	}

	// Full-screen viewer: enter opens, j flips (wrapping), esc returns to
	// the listing with the cursor on the last image viewed.
	m.tab().focus("a_red.png")
	key(m, "enter")
	if m.mode != modeImage {
		t.Fatal("enter on an image should open the viewer")
	}
	if v := checkView(t, m); !strings.Contains(v, "IMAGE") || !strings.Contains(v, "1/4") {
		t.Errorf("viewer header wrong:\n%s", v)
	}
	key(m, "j")
	key(m, "j")
	if filepath.Base(m.imgPath) != "c_photo.jpg" {
		t.Errorf("j should flip to the next image, at %s", m.imgPath)
	}
	waitImages(m)
	checkView(t, m)
	key(m, "k")
	key(m, "k")
	key(m, "k")
	if filepath.Base(m.imgPath) != "d_bad.png" {
		t.Errorf("k should wrap to the last image, at %s", m.imgPath)
	}
	key(m, "esc")
	if e, _ := m.tab().selected(); m.mode != modeNormal || e.name != "d_bad.png" {
		t.Errorf("esc should return to the list on the viewed image, got %q", e.name)
	}

	// e on an image also opens the viewer rather than the text editor.
	m.tab().focus("a_red.png")
	key(m, "e")
	if m.mode != modeImage {
		t.Error("e on an image should open the viewer")
	}

	// Narrow terminal.
	m.Update(tea.WindowSizeMsg{Width: 50, Height: 12})
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("narrow viewer line %d is %d wide", i, w)
		}
	}
	key(m, "esc")
	for i, l := range strings.Split(m.View(), "\n") {
		if w := ansi.StringWidth(l); w != 50 {
			t.Errorf("narrow preview line %d is %d wide", i, w)
		}
	}
}

func TestHalfBlocksAspect(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	lines := halfBlocks(src, 40, 40)
	// 2:1 image in 40 cells wide -> 40×20 px -> 10 rows.
	if len(lines) != 10 || ansi.StringWidth(lines[0]) != 40 {
		t.Errorf("got %d rows of width %d", len(lines), ansi.StringWidth(lines[0]))
	}
	tall := image.NewRGBA(image.Rect(0, 0, 10, 400))
	lines = halfBlocks(tall, 40, 20)
	if len(lines) != 20 || ansi.StringWidth(lines[0]) > 40 {
		t.Errorf("tall image: %d rows, width %d", len(lines), ansi.StringWidth(lines[0]))
	}
}
