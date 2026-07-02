package interactive

// Option is a single selectable entry in a prompt.
type Option struct {
	Label string
	Value string
}

// Prompter abstracts terminal interactive prompts.
// Production code uses HuhPrompter; tests use a deterministic fake.
type Prompter interface {
	Select(title string, options []Option) (string, error)
	SearchSelect(title, description string, options []Option) (string, error)
	Input(title, placeholder string, validate func(string) error) (string, error)
	InputWithSuggestions(title, placeholder string, suggestions []string, validate func(string) error) (string, error)
	// Secret reads a value with the typed input masked (API keys and other secrets).
	Secret(title string) (string, error)
	Confirm(title string, defaultValue bool) (bool, error)
}
