//go:build windows

package fsutil

import "golang.org/x/sys/windows"

// ReplaceFile atomically replaces dest with temp on the same filesystem.
func ReplaceFile(temp, dest string) error {
	from, err := windows.UTF16PtrFromString(temp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dest)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
