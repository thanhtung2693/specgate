package fsutil

import "golang.org/x/sys/unix"

// RenameNewDirectory atomically publishes a complete directory at a new name.
func RenameNewDirectory(stage, target string) error {
	return unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, target, unix.RENAME_NOREPLACE)
}
