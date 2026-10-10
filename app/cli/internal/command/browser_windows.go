package command

import (
	"errors"
	"runtime"
	"syscall"

	"golang.org/x/sys/windows"
)

func launchBrowser(target string) error {
	return shellExecuteBrowserURL(target, windows.ShellExecute)
}

func shellExecuteBrowserURL(target string, execute func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error) error {
	if err := validateBrowserURL(target); err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	// COM initialization and ShellExecute must remain on the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE)
	if err != nil && !errors.Is(err, syscall.Errno(windows.S_FALSE)) {
		return err
	}
	defer windows.CoUninitialize()
	// Treat the HTTP(S) URL as a document, never command-line parameters.
	return execute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}
