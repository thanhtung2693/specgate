//go:build !darwin && !linux && !windows

package fsutil

import "errors"

func RenameNewDirectory(_, _ string) error {
	return errors.New("exclusive Local store publication is unavailable on this platform")
}
