package main

// Restoring from the Windows Recycle Bin. Each deleted item lives in
// <drive>:\$Recycle.Bin\<user SID>\ as a pair: $Rxxxxxx.ext is the item
// itself and $Ixxxxxx.ext records its original path and deletion time.
// Restoring = moving $R back to the original path and removing $I, which is
// exactly what Explorer's "Restore" does.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

type binItem struct {
	info    string // $I file
	data    string // $R file or folder
	orig    string
	deleted time.Time
}

// parseBinInfo reads a $I file (format version 1 = Vista/7, 2 = Windows 10+).
func parseBinInfo(b []byte) (orig string, deleted time.Time, ok bool) {
	if len(b) < 24 {
		return "", time.Time{}, false
	}
	le := binary.LittleEndian
	ver := le.Uint64(b[0:8])
	ft := int64(le.Uint64(b[16:24])) // FILETIME: 100ns ticks since 1601
	deleted = time.Unix(0, (ft-116444736000000000)*100)
	var raw []byte
	switch ver {
	case 2:
		if len(b) < 28 {
			return "", time.Time{}, false
		}
		n := int(le.Uint32(b[24:28]))
		if len(b) < 28+2*n {
			return "", time.Time{}, false
		}
		raw = b[28 : 28+2*n]
	case 1:
		if len(b) < 24+520 {
			return "", time.Time{}, false
		}
		raw = b[24 : 24+520]
	default:
		return "", time.Time{}, false
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = le.Uint16(raw[2*i:])
	}
	for i, c := range u {
		if c == 0 {
			u = u[:i]
			break
		}
	}
	return string(utf16.Decode(u)), deleted, true
}

// findInRecycleBin returns the most recently deleted item that came from
// orig, deleted no earlier than notBefore.
func findInRecycleBin(orig string, notBefore time.Time) (binItem, error) {
	vol := filepath.VolumeName(orig)
	if vol == "" {
		return binItem{}, errors.New("not on a drive with a Recycle Bin")
	}
	root := vol + `\$Recycle.Bin`
	users, err := os.ReadDir(root)
	if err != nil {
		return binItem{}, fmt.Errorf("can't read the Recycle Bin on %s", vol)
	}
	var best binItem
	want := longPath(orig)
	for _, u := range users {
		dir := filepath.Join(root, u.Name())
		items, err := os.ReadDir(dir) // other users' folders are access-denied; skip them
		if err != nil {
			continue
		}
		for _, it := range items {
			name := it.Name()
			if !strings.HasPrefix(name, "$I") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			o, when, ok := parseBinInfo(b)
			if !ok || !(samePath(o, orig) || samePath(o, want)) || when.Before(notBefore) {
				continue
			}
			if best.info == "" || when.After(best.deleted) {
				best = binItem{info: filepath.Join(dir, name), data: filepath.Join(dir, "$R"+name[2:]), orig: o, deleted: when}
			}
		}
	}
	if best.info == "" {
		return binItem{}, errors.New("it's no longer in the Recycle Bin (emptied, or restored already?)")
	}
	return best, nil
}

// restoreFromRecycleBin moves orig back from the Recycle Bin.
func restoreFromRecycleBin(orig string, notBefore time.Time) error {
	if _, err := os.Lstat(orig); err == nil {
		return fmt.Errorf("something named %s is already there", filepath.Base(orig))
	}
	it, err := findInRecycleBin(orig, notBefore)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(it.data); err != nil {
		return errors.New("the Recycle Bin entry is incomplete")
	}
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		return err
	}
	if err := os.Rename(it.data, orig); err != nil {
		return err
	}
	_ = os.Remove(it.info)
	return nil
}
