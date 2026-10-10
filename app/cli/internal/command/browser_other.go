//go:build !windows

package command

import (
	"os/exec"
	"runtime"
)

func launchBrowser(target string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, target).Start()
}
