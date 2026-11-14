package local

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxProjectRubricBytes = 64 << 10

type ProjectRubric struct {
	Gate    string
	Content string
}

func projectRubricPath(gate string) string { return ".specgate/rubrics/" + gate + ".md" }

func validateProjectRubric(rubric ProjectRubric) error {
	known := false
	for _, gate := range localSemanticGates {
		known = known || gate.Key == rubric.Gate
	}
	if !known {
		return fmt.Errorf("unsupported project rubric %q", rubric.Gate)
	}
	text := rubric.Content
	if strings.TrimSpace(text) == "" || len(text) > maxProjectRubricBytes || !utf8.ValidString(text) {
		return fmt.Errorf("%s: require nonempty UTF-8 text, at most 64 KiB", projectRubricPath(rubric.Gate))
	}
	for _, char := range text {
		if unicode.IsControl(char) && char != '\n' && char != '\r' && char != '\t' {
			return fmt.Errorf("%s: require text without binary control characters", projectRubricPath(rubric.Gate))
		}
	}
	return nil
}

// ResolveProjectRubrics reads only the explicit project root; the Store never
// resolves working files when publishing or evaluating a frozen artifact.
func ResolveProjectRubrics(projectRoot string) ([]ProjectRubric, error) {
	if projectRoot == "" {
		return nil, nil
	}
	root, err := os.OpenRoot(projectRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, path := range []string{".specgate", ".specgate/rubrics"} {
		info, err := root.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s: require a non-symlink directory", path)
		}
	}
	dir, err := root.Open(".specgate/rubrics")
	if err != nil {
		return nil, fmt.Errorf(".specgate/rubrics: %w", err)
	}
	defer dir.Close()
	present := make(map[string]bool, len(localSemanticGates))
	for _, gate := range localSemanticGates {
		present[gate.Key+".md"] = false
	}
	for {
		names, err := dir.Readdirnames(64)
		for _, name := range names {
			if !strings.EqualFold(filepath.Ext(name), ".md") {
				continue
			}
			if _, known := present[name]; !known {
				return nil, fmt.Errorf(".specgate/rubrics/%s: unsupported rubric filename", name)
			}
			present[name] = true
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf(".specgate/rubrics: %w", err)
		}
	}
	var rubrics []ProjectRubric
	for _, gate := range localSemanticGates {
		if !present[gate.Key+".md"] {
			continue
		}
		path := projectRubricPath(gate.Key)
		info, err := root.Lstat(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() > maxProjectRubricBytes {
			return nil, fmt.Errorf("%s: require a regular non-symlink file, at most 64 KiB", path)
		}
		content, err := readProjectRubric(root, path, info)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		rubric := ProjectRubric{Gate: gate.Key, Content: string(content)}
		if err := validateProjectRubric(rubric); err != nil {
			return nil, err
		}
		rubrics = append(rubrics, rubric)
	}
	return rubrics, nil
}

func readProjectRubric(root *os.Root, path string, before os.FileInfo) ([]byte, error) {
	file, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() > maxProjectRubricBytes {
		return nil, fmt.Errorf("rubric changed while opening or exceeds 64 KiB")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxProjectRubricBytes+1))
	if err != nil {
		return nil, err
	}
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if len(body) > maxProjectRubricBytes || opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, fmt.Errorf("rubric changed during read or exceeds 64 KiB")
	}
	return body, nil
}
