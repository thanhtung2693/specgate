package command

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func expandArtifactDocumentSources(
	body map[string]any,
	packagePath string,
	projectRoot string,
) ([]string, error) {
	rawDocuments, ok := body["documents"]
	if !ok || rawDocuments == nil {
		return nil, nil
	}
	documents, ok := rawDocuments.([]any)
	if !ok {
		return nil, fmt.Errorf("documents must be an array")
	}

	packageDir := filepath.Dir(packagePath)
	realPackageDir, err := filepath.EvalSymlinks(packageDir)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact package directory %s: %w", packageDir, err)
	}
	var realProjectRoot string
	if projectRoot != "" {
		realProjectRoot, err = filepath.EvalSymlinks(projectRoot)
		if err != nil {
			return nil, fmt.Errorf("resolve Git repository root %s: %w", projectRoot, err)
		}
	}

	sources := make([]string, len(documents))
	for index, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("documents[%d] must be an object", index)
		}
		sourceFile, hasSourceFile := stringField(document, "source_file")
		repoFile, hasRepoFile := stringField(document, "repo_file")
		fileURL, hasFileURL := stringField(document, "file_url")
		if !hasSourceFile && !hasRepoFile && !hasFileURL {
			continue
		}

		var sourcePath string
		switch {
		case hasFileURL:
			parsed, parseErr := url.Parse(fileURL)
			if parseErr != nil || parsed.Scheme != "file" || parsed.Host != "" || parsed.Path == "" {
				return nil, fmt.Errorf("documents[%d].file_url must be an absolute local file:// URL", index)
			}
			sourcePath = filepath.FromSlash(parsed.Path)
			if !filepath.IsAbs(sourcePath) {
				return nil, fmt.Errorf("documents[%d].file_url must be an absolute local file:// URL", index)
			}
		case hasRepoFile:
			if realProjectRoot == "" {
				return nil, fmt.Errorf("documents[%d].repo_file requires a Git repository; use source_file for a manifest-contained file or file_url for an explicit external file", index)
			}
			clean, cleanErr := cleanRelativeSource(repoFile)
			if cleanErr != nil {
				return nil, fmt.Errorf("documents[%d].repo_file %w", index, cleanErr)
			}
			sourcePath = filepath.Join(realProjectRoot, filepath.FromSlash(clean))
			if err := requireSourceWithin(sourcePath, realProjectRoot, "repo_file", index); err != nil {
				return nil, err
			}
		default:
			if filepath.IsAbs(sourceFile) {
				return nil, fmt.Errorf("documents[%d].source_file must be relative; use file_url for an explicit external file", index)
			}
			clean := filepath.Clean(sourceFile)
			if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("documents[%d].source_file must stay within the artifact package directory", index)
			}
			sourcePath = filepath.Join(packageDir, clean)
			if err := requireSourceWithin(sourcePath, realPackageDir, "source_file", index); err != nil {
				return nil, err
			}
		}

		sourceInfo, err := os.Lstat(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
		}
		if sourceInfo.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("documents[%d] source %s is a symlink; publish the regular file explicitly", index, sourcePath)
		}
		content, err := readArtifactSource(sourcePath, sourceInfo)
		if err != nil {
			return nil, fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
		}
		if !utf8.Valid(content) {
			return nil, fmt.Errorf("documents[%d] source %s is not valid UTF-8 text", index, sourcePath)
		}
		absoluteSource, err := filepath.Abs(sourcePath)
		if err != nil {
			return nil, fmt.Errorf("resolve documents[%d] source %s: %w", index, sourcePath, err)
		}
		sources[index] = absoluteSource
		document["content"] = string(content)
		delete(document, "source_file")
		delete(document, "repo_file")
		delete(document, "file_url")
	}
	return sources, nil
}

func cleanRelativeSource(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || looksLikeWindowsAbsolutePath(value) {
		return "", fmt.Errorf("must be a non-empty repository-relative path")
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("must use forward slashes and stay within the Git repository")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must stay within the Git repository")
	}
	return filepath.ToSlash(clean), nil
}

func looksLikeWindowsAbsolutePath(value string) bool {
	return len(value) >= 3 &&
		((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) &&
		value[1] == ':' && (value[2] == '/' || value[2] == '\\')
}

func requireSourceWithin(sourcePath, realRoot, field string, index int) error {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("documents[%d] source %s is a symlink; publish the regular file explicitly", index, sourcePath)
	}
	realSource, err := filepath.EvalSymlinks(sourcePath)
	if err != nil {
		return fmt.Errorf("read documents[%d] source %s: %w", index, sourcePath, err)
	}
	relative, err := filepath.Rel(realRoot, realSource)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		boundary := "artifact package directory"
		if field == "repo_file" {
			boundary = "Git repository"
		}
		return fmt.Errorf("documents[%d].%s must stay within the %s", index, field, boundary)
	}
	return nil
}

func readArtifactSource(path string, info os.FileInfo) ([]byte, error) {
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source is not a regular file")
	}
	if info.Size() > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("source changed while it was being opened")
	}
	if !openedInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("source is not a regular file")
	}
	if openedInfo.Size() > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	content, err := io.ReadAll(io.LimitReader(file, artifactSourceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > artifactSourceMaxBytes {
		return nil, fmt.Errorf("source exceeds the 1 MiB limit")
	}
	return content, nil
}

func stringField(values map[string]any, key string) (string, bool) {
	value, ok := values[key]
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}
