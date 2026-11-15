package interactive

import (
	"io"
	"os"

	"github.com/charmbracelet/huh"
)

// HuhPrompter is the production Prompter backed by charmbracelet/huh.
type HuhPrompter struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

// NewHuhPrompter creates a HuhPrompter. When SPECGATE_ACCESSIBLE=1 it
// falls back to plain-text terminal input instead of the TUI.
func NewHuhPrompter(input io.Reader, output io.Writer) *HuhPrompter {
	return &HuhPrompter{input: input, output: output, accessible: os.Getenv("SPECGATE_ACCESSIBLE") == "1"}
}

func (h *HuhPrompter) run(field huh.Field) error {
	return huh.NewForm(huh.NewGroup(field)).
		WithInput(h.input).
		WithOutput(h.output).
		WithAccessible(h.accessible).
		Run()
}

func (h *HuhPrompter) Select(title string, options []Option) (string, error) {
	return h.selectFromOptions(title, "", options, false)
}

func (h *HuhPrompter) MultiSelect(title string, options []Option, defaults []string) ([]string, error) {
	huhopts := make([]huh.Option[string], len(options))
	for i, o := range options {
		huhopts[i] = huh.NewOption(o.Label, o.Value)
	}
	selected := append([]string(nil), defaults...)
	err := h.run(huh.NewMultiSelect[string]().Title(title).Options(huhopts...).Value(&selected))
	return selected, err
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
	err := h.run(field)
	return selected, err
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
	err := h.run(field)
	return result, err
}

func (h *HuhPrompter) Secret(title string) (string, error) {
	var result string
	field := huh.NewInput().Title(title).Value(&result).EchoMode(huh.EchoModePassword)
	err := h.run(field)
	return result, err
}

func (h *HuhPrompter) Confirm(title string, defaultValue bool) (bool, error) {
	value := defaultValue
	err := h.run(huh.NewConfirm().Title(title).Value(&value))
	return value, err
}
