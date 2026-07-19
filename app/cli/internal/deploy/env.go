package deploy

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
)

// copyIfSrcExists copies src to dst (mode 0600) only when src exists and dst
// does not. Silent no-op when either condition is unmet.
func copyIfSrcExists(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil // dst already present — preserve
	}
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil // no example to copy from
	}
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

// ensureEncryptionKey reads envFile and generates a 32-byte hex value for
// SETTINGS_ENCRYPTION_KEY when the existing assignment is blank or missing.
// If the key already has a non-empty value it is left unchanged.
func ensureEncryptionKey(envFile string) error {
	lines, err := readEnvLines(envFile)
	if err != nil {
		return err
	}

	const envKey = "SETTINGS_ENCRYPTION_KEY"
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, envKey+"="); ok {
			if strings.TrimSpace(after) != "" {
				return os.Chmod(envFile, 0600) // already set — preserve content, secure permissions
			}
		}
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Errorf("generate encryption key: %w", err)
	}
	if err := setEnvVar(envFile, lines, envKey, hex.EncodeToString(b)); err != nil {
		return err
	}
	return os.Chmod(envFile, 0600)
}

// setEnvVar writes lines back to path, replacing or appending key=value.
func setEnvVar(path string, lines []string, key, value string) error {
	prefix := key + "="
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = prefix + value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, prefix+value)
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// readEnvLines reads an env file as a slice of raw lines (without the final newline).
func readEnvLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}
