package main

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Palette (Tokyo Night–inspired; lipgloss downsamples on 256-color terminals).
var (
	cAccent  = lipgloss.Color("#7D56F4") // brand purple
	cPink    = lipgloss.Color("#F25D94")
	cGreen   = lipgloss.Color("#9ECE6A")
	cTeal    = lipgloss.Color("#2AC3DE")
	cYellow  = lipgloss.Color("#E0AF68")
	cOrange  = lipgloss.Color("#FF9E64")
	cBlue    = lipgloss.Color("#7AA2F7")
	cCyan    = lipgloss.Color("#7DCFFF")
	cRed     = lipgloss.Color("#F7768E")
	cMagenta = lipgloss.Color("#BB9AF7")
	cText    = lipgloss.Color("#C0CAF5")
	cBright  = lipgloss.Color("#FFFFFF")
	cDark    = lipgloss.Color("#1A1B26")
	cMuted   = lipgloss.Color("#565F89")
	cSubtle  = lipgloss.Color("#3B4261")
	cBarBg   = lipgloss.Color("#24283B")
	cSelBg   = lipgloss.Color("#364A82")
)

var (
	styleText     = lipgloss.NewStyle().Foreground(cText)
	styleDim      = lipgloss.NewStyle().Foreground(cMuted)
	styleSep      = lipgloss.NewStyle().Foreground(cSubtle)
	styleErr      = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	styleOK       = lipgloss.NewStyle().Foreground(cGreen)
	styleKey      = lipgloss.NewStyle().Foreground(cYellow).Bold(true)
	styleBar      = lipgloss.NewStyle().Background(cBarBg).Foreground(cText)
	styleBarDim   = lipgloss.NewStyle().Background(cBarBg).Foreground(cMuted)
	styleSel      = lipgloss.NewStyle().Background(cSelBg).Foreground(cBright).Bold(true)
	styleSelMark  = lipgloss.NewStyle().Background(cSelBg).Foreground(cPink).Bold(true)
	styleSelDim   = lipgloss.NewStyle().Background(cSubtle).Foreground(cText)
	styleCrumb    = lipgloss.NewStyle().Foreground(cBlue)
	styleCrumbEnd = lipgloss.NewStyle().Foreground(cPink).Bold(true)
	styleHeading  = lipgloss.NewStyle().Foreground(cMagenta).Bold(true)
)

// pill renders a padded label with a solid background.
func pill(label string, bg, fg lipgloss.Color) string {
	return lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).Padding(0, 1).Render(label)
}

// bar lays out left and right segments across exactly w cells on the bar
// background.
func bar(w int, left, right string) string {
	lw, rw := ansi.StringWidth(left), ansi.StringWidth(right)
	if lw+rw > w {
		left = ansi.Truncate(left, max(0, w-rw), "…")
		lw = ansi.StringWidth(left)
	}
	gap := max(0, w-lw-rw)
	return fit(left+styleBar.Render(strings.Repeat(" ", gap))+right, w)
}

// File types: a glyph and a color per kind.
type fileKind struct {
	glyph string
	color lipgloss.Color
}

var (
	kindDir     = fileKind{"▸", cBlue}
	kindMD      = fileKind{"¶", cGreen}
	kindCode    = fileKind{"λ", cCyan}
	kindData    = fileKind{"≡", cYellow}
	kindExec    = fileKind{"»", cOrange}
	kindImage   = fileKind{"◘", cMagenta}
	kindArchive = fileKind{"▣", cRed}
	kindDoc     = fileKind{"▤", cPink}
	kindHidden  = fileKind{"·", cMuted}
	kindOther   = fileKind{"·", cText}
)

var extKinds = map[string]fileKind{}

func init() {
	add := func(k fileKind, exts ...string) {
		for _, e := range exts {
			extKinds[e] = k
		}
	}
	add(kindMD, ".md", ".markdown", ".txt", ".rst", ".org")
	add(kindCode, ".go", ".py", ".js", ".ts", ".tsx", ".jsx", ".jl", ".rs", ".c", ".h", ".cpp", ".cs",
		".java", ".rb", ".lua", ".sql", ".html", ".css", ".r", ".kt", ".swift", ".mod", ".sum")
	add(kindData, ".json", ".yaml", ".yml", ".toml", ".xml", ".ini", ".csv", ".tsv", ".env", ".conf", ".cfg", ".lock")
	add(kindExec, ".exe", ".ps1", ".psm1", ".bat", ".cmd", ".sh", ".msi", ".dll")
	add(kindImage, ".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".ico", ".bmp")
	add(kindArchive, ".zip", ".tar", ".gz", ".7z", ".rar", ".tgz")
	add(kindDoc, ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt")
}

func kindOf(e entry) fileKind {
	if e.isDir {
		return kindDir
	}
	if strings.HasPrefix(e.name, ".") {
		return kindHidden
	}
	if k, ok := extKinds[strings.ToLower(filepath.Ext(e.name))]; ok {
		return k
	}
	return kindOther
}
