package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at release build time with -ldflags "-X main.version=v1.2".
// Without it, fall back to the module version `go install ...@version` recorded.
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}

func main() {
	chooseDir := flag.String("choosedir", "", "on quit with q, write the current folder to this file (used by the gt shell wrapper)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: goterm [--choosedir FILE] [--version] [DIR]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("goterm", versionString())
		return
	}

	start := "."
	if flag.NArg() > 0 {
		start = flag.Arg(0)
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg, firstRun := loadConfig()
	m := newModel(cfg, dir)
	if firstRun {
		m.setStatus("welcome to goterm · press ? for the key list · config: "+configPath(), nil)
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if fm, ok := final.(*model); ok && fm.quitCD && *chooseDir != "" {
		_ = os.WriteFile(*chooseDir, []byte(fm.tab().dir), 0o644)
	}
}
