package interactive_test

import (
	"fmt"
	"os"
	"path/filepath"
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
			previous := os.Stdin
			os.Stdin = input
			t.Cleanup(func() {
				os.Stdin = previous
				_ = input.Close()
			})
			got, err := tc.prompt(interactive.NewHuhPrompter())
			if err != nil || got != tc.want {
				t.Fatalf("entered answer lost: got=%q want=%q err=%v", got, tc.want, err)
			}
		})
	}
}
