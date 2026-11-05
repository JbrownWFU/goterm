//go:build windows

package main

import (
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000

// detach gives console children (code.cmd, powershell, ...) their own hidden
// console instead of sharing ours. GUI apps ignore the flag.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow, HideWindow: true}
}

// longPath expands 8.3 short names (C:\Users\EXAMPL~1\...) to their full
// form. The last element may not exist (e.g. a deleted file).
func longPath(p string) string {
	dir, base := filepath.Split(p)
	in, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return p
	}
	buf := make([]uint16, 1024)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return p
	}
	return filepath.Join(windows.UTF16ToString(buf[:n]), base)
}

// revealInExplorer opens Explorer on path's folder with path selected.
// Explorer parses its own command line (/select,"path"), so it's passed raw.
func revealInExplorer(path string) error {
	exe, err := exec.LookPath("explorer.exe")
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait() // explorer exits 1 even on success; ignore it
	return nil
}
