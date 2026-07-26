package command_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/specgate/specgate/app/cli/internal/command"
)

// The CLI reference and the installed skills tell humans and IDE agents which
// commands exist. Pinning those command names as string literals in a docs test
// only restates the doc; walking the real Cobra tree is what catches a renamed,
// removed, or undocumented command. Both directions matter: an invented command
// in a skill wastes an agent turn, and an undocumented one is unreachable.

const repoRoot = "../../../.."

// invocation matches a documented `specgate ...` call up to the depth of the
// deepest command path (`delivery handoff show`). The leading class rejects
// `thanhtung2693/specgate`, and single-line spacing keeps a repository name at
// the end of one line from swallowing the next line's first word.
var invocation = regexp.MustCompile(
	`(?:^|[^\w/.-])specgate[ \t]+([a-z][a-z0-9-]*)(?:[ \t]+([a-z][a-z0-9-]*))?(?:[ \t]+([a-z][a-z0-9-]*))?`,
)

func commandPaths(t *testing.T) map[string]*cobra.Command {
	t.Helper()
	paths := map[string]*cobra.Command{}
	var walk func(cmd *cobra.Command, prefix string)
	walk = func(cmd *cobra.Command, prefix string) {
		for _, child := range cmd.Commands() {
			name := child.Name()
			if name == "help" || name == "completion" {
				continue
			}
			path := strings.TrimSpace(prefix + " " + name)
			paths[path] = child
			walk(child, path)
		}
	}
	walk(command.NewRootCommand(&command.Deps{}), "")
	return paths
}

// resolveCommand returns the deepest command the captured tokens name, so
// `delivery handoff show` resolves to the leaf rather than stopping at the
// `delivery handoff` family.
func resolveCommand(paths map[string]*cobra.Command, match []string) (*cobra.Command, string) {
	tokens := []string{}
	for _, token := range match[1:] {
		if token == "" {
			break
		}
		tokens = append(tokens, token)
	}
	for depth := len(tokens); depth > 0; depth-- {
		path := strings.Join(tokens[:depth], " ")
		if cmd, ok := paths[path]; ok {
			return cmd, path
		}
	}
	return nil, ""
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

// documentedSurfaces are the files a user or IDE agent reads to learn the CLI.
func documentedSurfaces(t *testing.T) map[string]string {
	t.Helper()
	surfaces := map[string]string{}
	for _, rel := range []string{"README.md", "docs/using-specgate", "plugins/skills"} {
		root := filepath.Join(repoRoot, rel)
		err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
				return err
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			key, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				key = path
			}
			surfaces[filepath.ToSlash(key)] = string(raw)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", rel, err)
		}
	}
	if len(surfaces) == 0 {
		t.Fatal("no documented CLI surfaces found")
	}
	return surfaces
}

func TestDocumentedCommandsExist(t *testing.T) {
	t.Parallel()
	paths := commandPaths(t)
	root := command.NewRootCommand(&command.Deps{})

	var unknown []string
	for file, text := range documentedSurfaces(t) {
		for _, match := range invocation.FindAllStringSubmatch(text, -1) {
			if _, path := resolveCommand(paths, match); path != "" {
				continue
			}
			// A root flag (`specgate --version`) or prose ("specgate init and
			// workspace bind create") never names a top-level command.
			if root.Flags().Lookup(match[1]) != nil {
				continue
			}
			unknown = append(unknown, file+": specgate "+match[1])
		}
	}

	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Fatalf("documentation references commands the CLI does not define:\n%s",
			strings.Join(slices.Compact(unknown), "\n"))
	}
}

// A skill that tells an IDE agent to pass a flag the command does not define
// costs a turn and a confused retry. Flags are checked per line, so the command
// named on that line owns them.
func TestDocumentedFlagsExist(t *testing.T) {
	t.Parallel()
	paths := commandPaths(t)
	root := command.NewRootCommand(&command.Deps{})
	flagToken := regexp.MustCompile(`--([a-z][a-z0-9-]*)`)

	var unknown []string
	for file, text := range documentedSurfaces(t) {
		for _, line := range strings.Split(text, "\n") {
			match := invocation.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			cmd, _ := resolveCommand(paths, match)
			if cmd == nil {
				continue
			}
			for _, flag := range flagToken.FindAllStringSubmatch(line, -1) {
				name := flag[1]
				// Cobra registers --help during execution, not construction.
				if name == "help" || cmd.Flags().Lookup(name) != nil ||
					cmd.InheritedFlags().Lookup(name) != nil || root.PersistentFlags().Lookup(name) != nil {
					continue
				}
				unknown = append(unknown, file+": "+cmd.CommandPath()+" --"+name)
			}
		}
	}

	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Fatalf("documentation passes flags the command does not define:\n%s",
			strings.Join(slices.Compact(unknown), "\n"))
	}
}

func TestEveryCommandIsInTheCLIReference(t *testing.T) {
	t.Parallel()
	reference := readRepoFile(t, "docs/using-specgate/reference/cli.md")

	var undocumented []string
	for path, cmd := range commandPaths(t) {
		if cmd.Hidden {
			continue
		}
		// Match the path as a whole word so `artifact coverage <artifact-id>`
		// counts while `demo-data` does not satisfy `demo`.
		mention := regexp.MustCompile("(?:`|specgate )" + regexp.QuoteMeta(path) + "(?:`|\\s|$)")
		if mention.MatchString(reference) {
			continue
		}
		undocumented = append(undocumented, path)
	}

	sort.Strings(undocumented)
	if len(undocumented) > 0 {
		t.Fatalf("commands missing from docs/using-specgate/reference/cli.md:\n%s",
			strings.Join(undocumented, "\n"))
	}
}
