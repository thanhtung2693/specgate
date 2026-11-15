package interactive_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/interactive"
)

// Exercise the production form, not a fake prompter: returning its initial
// value instead of the answer would silently lose onboarding choices.
func TestAccessiblePrompterReturnsEnteredAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		prompt            func(*interactive.HuhPrompter) (string, error)
	}{
		{"input", "Solo developer\n", "Solo developer", func(h *interactive.HuhPrompter) (string, error) {
			return h.Input("Display name", "", nil)
		}},
		{"select", "2\n", "local", func(h *interactive.HuhPrompter) (string, error) {
			return h.Select("Mode", []interactive.Option{{Label: "Full", Value: "full"}, {Label: "Local", Value: "local"}})
		}},
		{"confirm", "y\n", "true", func(h *interactive.HuhPrompter) (string, error) {
			answer, err := h.Confirm("Continue?", false)
			return fmt.Sprint(answer), err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SPECGATE_ACCESSIBLE", "1")
			path := filepath.Join(t.TempDir(), "answers.txt")
			if err := os.WriteFile(path, []byte(tc.input), 0600); err != nil {
				t.Fatal(err)
			}
			input, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			var stderr bytes.Buffer
			stdoutFile, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			defer stdoutFile.Close()
			previous := os.Stdout
			os.Stdout = stdoutFile
			t.Cleanup(func() { os.Stdout = previous })
			got, err := tc.prompt(interactive.NewHuhPrompter(input, &stderr))
			if err != nil || got != tc.want {
				t.Fatalf("entered answer lost: got=%q want=%q err=%v", got, tc.want, err)
			}
			if _, err := stdoutFile.Seek(0, 0); err != nil {
				t.Fatal(err)
			}
			stdout, err := io.ReadAll(stdoutFile)
			if err != nil || len(stdout) != 0 || strings.TrimSpace(stderr.String()) == "" {
				t.Fatalf("stdout = %q, stderr = %q, err = %v", stdout, &stderr, err)
			}
		})
	}
}
