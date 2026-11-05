package main

// Image preview: images are decoded in the background, shrunk to a
// thumbnail, and drawn with "▀" half blocks in 24-bit color, so every
// terminal cell shows two pixels (top = foreground, bottom = background).

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	thumbMax     = 640         // longest side of the cached thumbnail
	imgMaxPixels = 120_000_000 // refuse to decode anything bigger
	imgCacheSize = 48
)

var imgBG = color.RGBA{0x1A, 0x1B, 0x26, 0xFF} // cDark, behind transparent pixels

var decodableExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".bmp": true, ".webp": true, ".tif": true, ".tiff": true,
}

func isImage(name string) bool { return decodableExts[strings.ToLower(filepath.Ext(name))] }

type imgEntry struct {
	thumb  *image.RGBA
	w, h   int
	format string
	err    error
}

type imgLoadedMsg struct {
	key string
	e   *imgEntry
}

func imgKey(path string, fi fs.FileInfo) string {
	return fmt.Sprintf("%s|%d|%d", path, fi.ModTime().UnixNano(), fi.Size())
}

func loadImage(path, key string) tea.Cmd {
	return func() tea.Msg { return imgLoadedMsg{key: key, e: decodeImage(path)} }
}

func decodeImage(path string) *imgEntry {
	f, err := os.Open(path)
	if err != nil {
		return &imgEntry{err: err}
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return &imgEntry{err: errors.New("not a supported image format")}
		}
		return &imgEntry{err: fmt.Errorf("can't read image: %v", err)}
	}
	ent := &imgEntry{w: cfg.Width, h: cfg.Height, format: format}
	if cfg.Width*cfg.Height > imgMaxPixels {
		ent.err = fmt.Errorf("%d×%d is too large to preview", cfg.Width, cfg.Height)
		return ent
	}
	if _, err := f.Seek(0, 0); err != nil {
		ent.err = err
		return ent
	}
	img, _, err := image.Decode(f)
	if err != nil {
		ent.err = fmt.Errorf("can't decode image: %v", err)
		return ent
	}
	ent.thumb = makeThumb(img, thumbMax)
	return ent
}

// makeThumb shrinks img to fit in maxDim×maxDim (never enlarging) and flattens
// transparency onto the theme background.
func makeThumb(img image.Image, maxDim int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > maxDim || h > maxDim {
		s := min(float64(maxDim)/float64(w), float64(maxDim)/float64(h))
		w, h = max(1, int(float64(w)*s+0.5)), max(1, int(float64(h)*s+0.5))
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{imgBG}, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	return dst
}

// halfBlocks draws src as large as fits in cw×ch cells, keeping its aspect
// ratio. Lines are tw cells wide (not padded).
func halfBlocks(src *image.RGBA, cw, ch int) []string {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	if cw < 1 || ch < 1 || sw < 1 || sh < 1 {
		return nil
	}
	s := min(float64(cw)/float64(sw), float64(2*ch)/float64(sh))
	tw := min(cw, max(1, int(float64(sw)*s+0.5)))
	th := min(2*ch, max(2, int(float64(sh)*s+0.5)))
	th -= th % 2
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	var k draw.Interpolator = draw.CatmullRom
	if s > 1 {
		k = draw.NearestNeighbor // keep small images (icons, pixel art) crisp
	}
	k.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)

	lines := make([]string, 0, th/2)
	buf := make([]byte, 0, tw*40)
	sgr := func(buf []byte, code string, c color.RGBA) []byte {
		buf = append(buf, "\x1b["...)
		buf = append(buf, code...)
		buf = append(buf, ";2;"...)
		buf = strconv.AppendInt(buf, int64(c.R), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64(c.G), 10)
		buf = append(buf, ';')
		buf = strconv.AppendInt(buf, int64(c.B), 10)
		return append(buf, 'm')
	}
	for y := 0; y < th; y += 2 {
		buf = buf[:0]
		var lastT, lastB color.RGBA
		for x := 0; x < tw; x++ {
			t, b := dst.RGBAAt(x, y), dst.RGBAAt(x, y+1)
			if x == 0 || t != lastT {
				buf = sgr(buf, "38", t)
			}
			if x == 0 || b != lastB {
				buf = sgr(buf, "48", b)
			}
			lastT, lastB = t, b
			buf = append(buf, "▀"...)
		}
		buf = append(buf, "\x1b[0m"...)
		lines = append(lines, string(buf))
	}
	return lines
}

// --- model side ---

// wantImage starts decoding the image currently on screen, if needed.
func (m *model) wantImage() tea.Cmd {
	var path string
	switch m.mode {
	case modeImage:
		path = m.imgPath
	case modeEditor, modeHelp, modeOpenWith, modeBookmarks:
		return nil
	case modeFind:
		f, abs, ok := m.findSelected()
		if !ok || f.isDir || !isImage(abs) {
			return nil
		}
		path = abs
	default:
		e, ok := m.tab().selected()
		if !ok || e.isDir || !isImage(e.name) {
			return nil
		}
		path = m.tab().selectedPath()
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := imgKey(path, fi)
	if m.imgs[key] != nil {
		return nil
	}
	if _, busy := m.imgLoading[key]; busy {
		return nil
	}
	m.imgLoading[key] = path
	return loadImage(path, key)
}

func (m *model) storeImage(msg imgLoadedMsg) {
	delete(m.imgLoading, msg.key)
	if _, ok := m.imgs[msg.key]; !ok {
		m.imgOrder = append(m.imgOrder, msg.key)
	}
	m.imgs[msg.key] = msg.e
	for len(m.imgOrder) > imgCacheSize {
		delete(m.imgs, m.imgOrder[0])
		m.imgOrder = m.imgOrder[1:]
	}
}

// imageBlock returns the rendered image for path in cw×ch cells (cached),
// or nil plus a status string while loading / on error.
func (m *model) imageBlock(path string, fi fs.FileInfo, cw, ch int) (*imgEntry, []string) {
	key := imgKey(path, fi)
	ent := m.imgs[key]
	if ent == nil || ent.thumb == nil {
		return ent, nil
	}
	rk := fmt.Sprintf("%s|%d|%d", key, cw, ch)
	if rk != m.imgRKey {
		m.imgRKey, m.imgRLines = rk, halfBlocks(ent.thumb, cw, ch)
	}
	return ent, m.imgRLines
}

func imgInfo(ent *imgEntry) string {
	if ent == nil || ent.w == 0 {
		return "image"
	}
	return fmt.Sprintf("%d×%d %s", ent.w, ent.h, ent.format)
}

// imagePreview fills the preview column for an image file.
func (m *model) imagePreview(e entry, path string, fi fs.FileInfo, w, h int) []string {
	ent, block := m.imageBlock(path, fi, w-2, h-2)
	out := []string{previewHeader(e, fi, w, imgInfo(ent)), fit("", w)}
	switch {
	case ent == nil:
		out = append(out, fit(styleDim.Italic(true).Render("  loading…"), w))
	case ent.err != nil:
		out = append(out, fit(styleErr.Render("  "+ent.err.Error()), w),
			fit(styleDim.Render("  press E to open it in another program"), w))
	default:
		for _, l := range block {
			out = append(out, fit(" "+l, w))
		}
	}
	return padLines(out, w, h)
}

// imageList is the images in the current folder listing, for flipping
// through them in the full-screen view.
func (m *model) imageList() []entry {
	var out []entry
	for _, e := range m.tab().entries {
		if !e.isDir && isImage(e.name) {
			out = append(out, e)
		}
	}
	return out
}

func (m *model) openImage(path string) {
	m.imgPath, m.mode = path, modeImage
}

func (m *model) updateImage(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	t := m.tab()
	step := 0
	switch msg.String() {
	case "esc", "q", "enter", "backspace", "ctrl+c":
		m.mode = modeNormal
		t.focus(filepath.Base(m.imgPath))
	case "j", "down", "l", "right", "n", " ", "pgdown":
		step = 1
	case "k", "up", "h", "left", "p", "pgup":
		step = -1
	case "g", "home":
		if imgs := m.imageList(); len(imgs) > 0 {
			m.imgPath = filepath.Join(t.dir, imgs[0].name)
		}
	case "G", "end":
		if imgs := m.imageList(); len(imgs) > 0 {
			m.imgPath = filepath.Join(t.dir, imgs[len(imgs)-1].name)
		}
	case "E":
		if len(m.openers) == 0 {
			m.setStatus("", fmt.Errorf("no openers available; edit %s", configPath()))
		} else {
			t.focus(filepath.Base(m.imgPath))
			m.openTarget, m.openCursor, m.mode = m.imgPath, 0, modeOpenWith
		}
	case "y":
		m.copy(m.imgPath)
	}
	if step != 0 {
		imgs := m.imageList()
		if len(imgs) > 0 {
			i := 0
			for j, e := range imgs {
				if strings.EqualFold(e.name, filepath.Base(m.imgPath)) {
					i = j
					break
				}
			}
			i = (i + step + len(imgs)) % len(imgs)
			m.imgPath = filepath.Join(t.dir, imgs[i].name)
			m.status = ""
		}
	}
	return m, nil
}

// imageView is the full-screen image viewer.
func (m *model) imageView() string {
	w, h := m.width, m.height
	bodyH := max(1, h-2)
	name := filepath.Base(m.imgPath)
	k := kindOf(entry{name: name})

	imgs := m.imageList()
	pos := ""
	for i, e := range imgs {
		if strings.EqualFold(e.name, name) {
			pos = fmt.Sprintf("%d/%d", i+1, len(imgs))
		}
	}

	var body []string
	info := ""
	fi, err := os.Stat(m.imgPath)
	if err != nil {
		body = []string{fit(styleErr.Render("  "+err.Error()), w)}
	} else {
		ent, block := m.imageBlock(m.imgPath, fi, w-2, bodyH)
		info = imgInfo(ent) + " · " + humanSize(fi.Size())
		switch {
		case ent == nil:
			body = []string{fit(styleDim.Italic(true).Render("  loading…"), w)}
		case ent.err != nil:
			body = []string{fit(styleErr.Render("  "+ent.err.Error()), w)}
		default:
			top := (bodyH - len(block)) / 2
			for range top {
				body = append(body, fit("", w))
			}
			for _, l := range block {
				pad := max(0, (w-ansi.StringWidth(l))/2)
				body = append(body, fit(strings.Repeat(" ", pad)+l, w))
			}
		}
	}

	title := lipgloss.NewStyle().Background(cBarBg).Foreground(k.color).Bold(true).Render(" " + k.glyph + " " + name)
	right := styleBarDim.Render(info + "  ")
	if pos != "" {
		right += pill(pos, cSubtle, cText)
	}
	header := bar(w, pill("IMAGE", cMagenta, cDark)+title, right)

	var left string
	if m.status != "" {
		st := lipgloss.NewStyle().Background(cBarBg).Foreground(cGreen)
		if m.statusErr {
			st = st.Foreground(cRed)
		}
		left = st.Render(" " + m.status)
	} else {
		left = hints(true, [][2]string{{"j/k ←/→", "next / previous"}, {"g/G", "first / last"},
			{"E", "open with"}, {"y", "copy path"}, {"esc", "back"}})
	}
	footer := bar(w, left, "")
	return header + "\n" + strings.Join(padLines(body, w, bodyH), "\n") + "\n" + footer
}
