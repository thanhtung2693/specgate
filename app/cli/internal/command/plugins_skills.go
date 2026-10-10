package command

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"github.com/specgate/specgate/app/cli/internal/client"
)

// Older inventories may omit served_files; their declared skills still require
// SKILL.md. Supplemental files come only from the validated package inventory.
func pluginSkillFiles(pkg *client.PluginPackage) []string {
	files := map[string]bool{}
	for _, skill := range pluginSkillsFromPackage(pkg) {
		prefix := "skills/" + skill + "/"
		files[prefix+"SKILL.md"] = true
		if pkg != nil {
			for _, file := range pkg.ServedFiles {
				if strings.HasPrefix(file, prefix) {
					files[file] = true
				}
			}
		}
	}
	ordered := make([]string, 0, len(files))
	for file := range files {
		ordered = append(ordered, file)
	}
	sort.Strings(ordered)
	return ordered
}

func isPluginSkillEntry(file string) bool {
	return strings.Count(file, "/") == 2 && strings.HasSuffix(file, "/SKILL.md")
}

func validatePluginFileInventory(pkg *client.PluginPackage, skills map[string]bool) error {
	seen := map[string]bool{}
	for _, file := range pkg.ServedFiles {
		if !portablePluginPath(file) {
			return fmt.Errorf("plugin package has unsafe file path %q", file)
		}
		if seen[file] {
			return fmt.Errorf("plugin package repeats file %q", file)
		}
		seen[file] = true
		if strings.HasPrefix(file, "skills/") {
			parts := strings.Split(file, "/")
			if len(parts) < 3 || !skills[parts[1]] {
				return fmt.Errorf("plugin package file %q has no declared skill", file)
			}
		}
	}
	for _, file := range pluginSkillFiles(pkg) {
		seen[file] = true
		if !isPluginSkillEntry(file) {
			seen[file+pluginOwnerMarker] = true
		}
	}
	for skill := range skills {
		seen["skills/"+skill+"/"+pluginOwnerMarker] = true
	}
	aliases := map[string]string{}
	for file := range seen {
		for name := file; name != "."; name = path.Dir(name) {
			key := pluginPathKey(name)
			if prior, ok := aliases[key]; ok && prior != name {
				return fmt.Errorf("plugin package path %q aliases %q", name, prior)
			}
			aliases[key] = name
			if name != file && seen[name] {
				return fmt.Errorf("plugin package file %q conflicts with directory %q", file, name)
			}
		}
	}
	return nil
}

func pluginPathKey(name string) string {
	return norm.NFC.String(cases.Fold().String(name))
}

// Packages are shared across IDEs and OSes, so validate Windows names even when
// installing on Unix. Ownership markers are installer-only in every component.
func portablePluginPath(file string) bool {
	if file == "." || !fs.ValidPath(file) || !filepath.IsLocal(file) || strings.ContainsAny(file, "\\:?#<>\"|*") || strings.IndexFunc(file, unicode.IsControl) >= 0 {
		return false
	}
	for _, part := range strings.Split(file, "/") {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.HasSuffix(pluginPathKey(part), pluginOwnerMarker) {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		base = strings.TrimRight(base, " ")
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		runes := []rune(base)
		if len(runes) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && strings.ContainsRune("123456789¹²³", runes[3]) {
			return false
		}
	}
	return true
}
