//go:build !windows

package main

import (
	"os/exec"
	"path/filepath"
)

func detach(cmd *exec.Cmd) {}

func longPath(p string) string { return p }

func revealInExplorer(path string) error { return openInExplorer(filepath.Dir(path)) }
