package interactive

import (
	"os"

	"github.com/charmbracelet/huh"
)

// HuhPrompter is the production Prompter backed by charmbracelet/huh.
type HuhPrompter struct {
	accessible bool
}

// NewHuhPrompter creates a HuhPrompter. When SPECGATE_ACCESSIBLE=1 it
// falls back to plain-text terminal input instead of the TUI.
func NewHuhPrompter() *HuhPrompter {
	return &HuhPrompter{accessible: os.Getenv("SPECGATE_ACCESSIBLE") == "1"}
}

func (h *HuhPrompter) Select(title string, options []Option) (string, error) {
	return h.selectFromOptions(title, "", options, false)
}

func (h *HuhPrompter) SearchSelect(title, description string, options []Option) (string, error) {
	return h.selectFromOptions(title, description, options, true)
}

func (h *HuhPrompter) selectFromOptions(title, description string, options []Option, filterable bool) (string, error) {
	huhopts := make([]huh.Option[string], len(options))
	for i, o := range options {
		huhopts[i] = huh.NewOption(o.Label, o.Value)
	}
	var selected string
	field := huh.NewSelect[string]().
		Title(title).
		Options(huhopts...).
		Value(&selected)
	if description != "" {
		field = field.Description(description)
	}
	if filterable {
		field = field.Filtering(true).Height(12)
	}
	f := huh.NewForm(huh.NewGroup(
		field,
	))
	if h.accessible {
		f = f.WithAccessible(true)
	}
	return selected, f.Run()
}

func (h *HuhPrompter) Input(title, placeholder string, validate func(string) error) (string, error) {
	return h.InputWithSuggestions(title, placeholder, nil, validate)
}

func (h *HuhPrompter) InputWithSuggestions(title, placeholder string, suggestions []string, validate func(string) error) (string, error) {
	var result string
	field := huh.NewInput().Title(title).Value(&result)
	if placeholder != "" {
		field = field.Placeholder(placeholder)
	}
	if len(suggestions) > 0 {
		field = field.Suggestions(suggestions)
	}
	if validate != nil {
		field = field.Validate(validate)
	}
	f := huh.NewForm(huh.NewGroup(field))
	if h.accessible {
		f = f.WithAccessible(true)
	}
	return result, f.Run()
}

func (h *HuhPrompter) Secret(title string) (string, error) {
	var result string
	field := huh.NewInput().Title(title).Value(&result).EchoMode(huh.EchoModePassword)
	f := huh.NewForm(huh.NewGroup(field))
	if h.accessible {
		f = f.WithAccessible(true)
	}
	return result, f.Run()
}

func (h *HuhPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	value := defaultValue
	f := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&value),
	))
	if h.accessible {
		f = f.WithAccessible(true)
	}
	return value, f.Run()
}
