package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func runIDFromRequest(req *mcpsdk.CallToolRequest, apiKey string) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return fallbackRunID(apiKey)
	}
	if id := strings.TrimSpace(req.Extra.Header.Get("X-SpecGate-Run-ID")); id != "" {
		return id
	}
	return fallbackRunID(apiKey)
}

func fallbackRunID(apiKey string) string {
	h := sha256.Sum256([]byte(apiKey))
	keyHash := hex.EncodeToString(h[:8])
	window := time.Now().Unix() / 600
	return fmt.Sprintf("fb:%s:%d", keyHash, window)
}
