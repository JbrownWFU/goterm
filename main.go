package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	chooseDir := flag.String("choosedir", "", "on quit with q, write the current folder to this file (used by the gt shell wrapper)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: goterm [--choosedir FILE] [DIR]\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	start := "."
	if flag.NArg() > 0 {
		start = flag.Arg(0)
	}
	dir, err := filepath.Abs(start)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	m := newModel(loadConfig(), dir)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if fm, ok := final.(*model); ok && fm.quitCD && *chooseDir != "" {
		_ = os.WriteFile(*chooseDir, []byte(fm.tab().dir), 0o644)
	}
}
