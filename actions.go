package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// startDetached launches a program without tying it to our console, so its
// output can't scribble over the TUI.
func startDetached(name string, args []string, dir string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func launchOpener(o Opener, path string) error {
	args := append(append([]string{}, o.Args...), path)
	return startDetached(o.Cmd, args, filepath.Dir(path))
}

// openTerminalTab opens a new tab in the current Windows Terminal window.
func openTerminalTab(dir string, command []string) error {
	wt, err := exec.LookPath("wt")
	if err != nil {
		return errors.New("Windows Terminal (wt.exe) not found · install it from the Microsoft Store, or run goterm inside it")
	}
	if missing := missingProgram(command); missing != "" {
		return fmt.Errorf("%s is not on PATH · install it, or change the command in config.json", missing)
	}
	args := append([]string{"-w", "0", "nt", "-d", dir}, command...)
	return startDetached(wt, args, dir)
}

// missingProgram checks the programs a tab command would run and returns the
// first one that isn't installed, or "". It looks at the first word, and when
// that is a shell with -Command/-c, at the first word of the command too
// (e.g. "claude" in `powershell -NoExit -Command claude`).
func missingProgram(command []string) string {
	if len(command) == 0 {
		return ""
	}
	check := []string{command[0]}
	for i, a := range command {
		if (strings.EqualFold(a, "-Command") || a == "-c") && i+1 < len(command) {
			inner := command[i+1]
			// Only simple "prog args" commands; skip pipelines, builtins and script snippets.
			if first := strings.Fields(inner); len(first) > 0 && !strings.ContainsAny(inner, ";|&$(){}") {
				check = append(check, first[0])
			}
			break
		}
	}
	for _, c := range check {
		if _, err := exec.LookPath(c); err != nil {
			return c
		}
	}
	return ""
}

func openInExplorer(dir string) error {
	if runtime.GOOS != "windows" {
		return errors.New("only supported on Windows")
	}
	return startDetached("explorer", []string{dir}, dir)
}

// recycle sends a file or folder to the Recycle Bin rather than deleting it.
func recycle(path string) error {
	if runtime.GOOS != "windows" {
		return os.RemoveAll(path)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	method := "DeleteFile"
	if fi.IsDir() {
		method = "DeleteDirectory"
	}
	script := fmt.Sprintf(
		"Add-Type -AssemblyName Microsoft.VisualBasic; [Microsoft.VisualBasic.FileIO.FileSystem]::%s('%s', 'OnlyErrorDialogs', 'SendToRecycleBin')",
		method, strings.ReplaceAll(path, "'", "''"))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	detach(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := os.Stat(path); err == nil {
		return errors.New("delete was cancelled or failed")
	}
	return nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func dirIsEmpty(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	names, _ := f.Readdirnames(1)
	return len(names) == 0
}
