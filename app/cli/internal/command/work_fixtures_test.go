package command_test

import (
	"bytes"
	"testing"

	"github.com/specgate/specgate/app/cli/internal/command"
	"github.com/specgate/specgate/app/cli/internal/interactive"
)

type fakePrompter struct {
	confirmObserver func()
	selectObserver  func()
	selectedValue   string
	multiValues     []string
	multiDefaults   []string
	searchValue     string
	selectOptions   []interactive.Option
	multiTitle      string
	multiOptions    []interactive.Option
	searchTitle     string
	searchOptions   []interactive.Option
	inputValue      string
	inputValues     []string
	inputTitle      string
	inputTitles     []string
	suggestions     []string
	secretValue     string
	secretTitle     string
	confirmValue    bool
	confirmTitle    string
	inputCalls      int
	secretCalls     int
}

func (f *fakePrompter) Select(_ string, options []interactive.Option) (string, error) {
	if f.selectObserver != nil {
		f.selectObserver()
	}
	f.selectOptions = options
	return f.selectedValue, nil
}

func (f *fakePrompter) MultiSelect(title string, options []interactive.Option, defaults []string) ([]string, error) {
	f.multiTitle = title
	f.multiOptions = options
	f.multiDefaults = defaults
	if f.multiValues != nil {
		return f.multiValues, nil
	}
	return defaults, nil
}

func (f *fakePrompter) SearchSelect(title, _ string, options []interactive.Option) (string, error) {
	f.searchTitle = title
	f.searchOptions = options
	return f.searchValue, nil
}

func (f *fakePrompter) Input(title, _ string, _ func(string) error) (string, error) {
	f.inputCalls++
	f.inputTitle = title
	f.inputTitles = append(f.inputTitles, title)
	if len(f.inputValues) > 0 {
		value := f.inputValues[0]
		f.inputValues = f.inputValues[1:]
		return value, nil
	}
	return f.inputValue, nil
}

func (f *fakePrompter) InputWithSuggestions(title string, _ string, suggestions []string, _ func(string) error) (string, error) {
	f.inputCalls++
	f.inputTitle = title
	f.suggestions = suggestions
	return f.inputValue, nil
}

func (f *fakePrompter) Secret(title string) (string, error) {
	f.secretCalls++
	f.secretTitle = title
	return f.secretValue, nil
}

func (f *fakePrompter) Confirm(title string, _ bool) (bool, error) {
	if f.confirmObserver != nil {
		f.confirmObserver()
	}
	f.confirmTitle = title
	return f.confirmValue, nil
}

func newWorkflowTestDeps(t *testing.T) (*command.Deps, *fakePrompter, *bytes.Buffer) {
	t.Helper()
	deps, out := newTestDeps(t)
	prompt := &fakePrompter{}
	deps.Prompter = prompt
	return deps, prompt, out
}
