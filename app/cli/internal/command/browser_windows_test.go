package command

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsBrowserURLIsDocumentNotCommandParameters(t *testing.T) {
	for _, target := range []string{
		"https://example.invalid/path?one=1&two=2",
		`https://example.invalid/path?literal="&echo harmless&"&env=%PATH%`,
		"https://[::1]:8443/đường?name=value#review",
	} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			failure := errors.New("native browser unavailable")
			err := shellExecuteBrowserURL(target, func(hwnd windows.Handle, verb, file, args, cwd *uint16, show int32) error {
				calls++
				if hwnd != 0 || windows.UTF16PtrToString(verb) != "open" || windows.UTF16PtrToString(file) != target || args != nil || cwd != nil || show != windows.SW_SHOWNORMAL {
					t.Fatal("URL did not remain the sole native document argument")
				}
				return failure
			})
			if calls != 1 || !errors.Is(err, failure) {
				t.Fatalf("native calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestWindowsBrowserRefusesExecutablesAndProtocolHandlers(t *testing.T) {
	for _, target := range []string{`C:\Windows\System32\cmd.exe`, "file:///C:/unsafe.exe", "ms-settings:display", "https://example.invalid/\x00suffix"} {
		calls := 0
		err := shellExecuteBrowserURL(target, func(windows.Handle, *uint16, *uint16, *uint16, *uint16, int32) error { calls++; return nil })
		if err == nil || calls != 0 {
			t.Fatalf("unsafe target reached native call: %q calls=%d error=%v", target, calls, err)
		}
	}
}
