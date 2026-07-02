package settings

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateToken returns a cryptographically random 32-byte token, hex-encoded
// (64 chars). Used to mint the MCP API key (mcp.api_key) when none is configured.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("settings: generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
