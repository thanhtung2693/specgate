package command

import (
	"fmt"
	"os/exec"
	"runtime"
)

func defaultOpener(url string) error {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "windows":
		cmd = "cmd"
	default:
		cmd = "xdg-open"
	}
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"/c", "start", url}
	} else {
		args = []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		return fmt.Errorf("open %s: %w", url, err)
	}
	return nil
}
