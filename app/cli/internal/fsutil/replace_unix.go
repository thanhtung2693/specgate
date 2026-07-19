//go:build !windows

package fsutil

import "os"

// ReplaceFile atomically replaces dest with temp on the same filesystem.
func ReplaceFile(temp, dest string) error {
	return os.Rename(temp, dest)
}
