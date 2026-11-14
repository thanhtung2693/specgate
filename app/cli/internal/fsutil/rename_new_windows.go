package fsutil

import "golang.org/x/sys/windows"

// RenameNewDirectory atomically publishes a complete directory at a new name.
func RenameNewDirectory(stage, target string) error {
	from, err := windows.UTF16PtrFromString(stage)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	return windows.MoveFile(from, to)
}
