package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexTOMLEditsPreserveUnrelatedGrammar(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		preserved  []string
	}{
		{
			name:      "literal dotted root key",
			text:      "[\"plugins.specgate@personal\"]\nkeep = true\n[plugins.\"specgate@personal\"]\nenabled = false\n",
			preserved: []string{"[\"plugins.specgate@personal\"]\nkeep = true\n"},
		},
		{
			name:      "header inside multiline string",
			text:      "instructions = \"\"\"\n[plugins.\"specgate@personal\"]\nkeep this text\n\"\"\"\n[plugins.\"specgate@personal\"]\nenabled = false\n",
			preserved: []string{"instructions = \"\"\"\n[plugins.\"specgate@personal\"]\nkeep this text\n\"\"\"\n"},
		},
		{
			name:      "array table after owned section",
			text:      "[plugins.\"specgate@personal\"]\nenabled = false\n# user agent\n[[agents]]\nname = 'keep'\n",
			preserved: []string{"# user agent\n[[agents]]\nname = 'keep'\n"},
		},
		{
			name:      "multiline owned value and CRLF",
			text:      "[plugins.\"specgate@personal\"]\r\nenabled = false\r\nnotes = '''\r\n[tools]\r\nowned text\r\n'''\r\n[tools]\r\nkeep = true\r\n",
			preserved: []string{"[tools]\r\nkeep = true\r\n"},
		},
		{
			name:      "spaced and escaped key",
			text:      "[ plugins . \"specgate\\u0040personal\" ]\nenabled = false\n[tools]\nkeep = true\n",
			preserved: []string{"[tools]\nkeep = true\n"},
		},
		{
			name:      "multiline array and inline table",
			text:      "[plugins.'specgate@personal'] # managed\nenabled = false\nnotes = [\n '[tools]', # not a table\n { text = '[agents]' },\n]\n# keep comment\n[tools]\nkeep = true",
			preserved: []string{"# keep comment\n[tools]\nkeep = true"},
		},
		{
			name:      "unrelated child table",
			text:      "[plugins.'specgate@personal']\nenabled = false\n[plugins.'specgate@personal'.custom]\nkeep = true\n",
			preserved: []string{"[plugins.'specgate@personal'.custom]\nkeep = true\n"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, operation := range []string{"install", "uninstall"} {
				t.Run(operation, func(t *testing.T) {
					var result []byte
					var err error
					if operation == "install" {
						result, err = updateCodexConfig(tc.text, t.TempDir())
					} else {
						path := filepath.Join(t.TempDir(), "config.toml")
						if err = os.WriteFile(path, []byte(tc.text), 0600); err != nil {
							t.Fatal(err)
						}
						_, err = removeCodexConfigSections(path, false, "")
						if err == nil {
							result, err = os.ReadFile(path)
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					config, err := parseTOML(string(result))
					if err != nil {
						t.Fatalf("edit corrupted valid TOML: %v\n%s", err, result)
					}
					for _, fragment := range tc.preserved {
						if !strings.Contains(string(result), fragment) {
							t.Fatalf("unrelated content changed: want %q\ngot %s", fragment, result)
						}
					}
					plugins, _ := config["plugins"].(map[string]any)
					plugin, _ := plugins["specgate@personal"].(map[string]any)
					_, installed := plugin["enabled"]
					if installed != (operation == "install") {
						t.Fatalf("managed plugin transition incorrect: %s", result)
					}
				})
			}
		})
	}
}

func TestCodexTOMLRefusalLeavesOriginalFile(t *testing.T) {
	for _, text := range []string{
		"plugins = { 'specgate@personal' = { enabled = false } }\n",
		"[plugins.'specgate@personal']\nenabled = false\nbroken = '''\n",
	} {
		if _, err := updateCodexConfig(text, t.TempDir()); err == nil {
			t.Fatalf("install should refuse unsupported or malformed config: %q", text)
		}
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if changed, err := removeCodexConfigSections(path, false, ""); err == nil || changed {
			t.Fatalf("uninstall should refuse without mutation: changed=%v err=%v", changed, err)
		}
		body, err := os.ReadFile(path)
		if err != nil || string(body) != text {
			t.Fatalf("refusal changed original config: %q, %v", body, err)
		}
	}
}

func TestCodexTOMLRefreshIsStable(t *testing.T) {
	root := t.TempDir()
	first, err := updateCodexConfig("instructions = 'keep'\n", root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := updateCodexConfig(string(first), root)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("refresh changed unchanged config:\nfirst: %q\nsecond: %q", first, second)
	}
}
