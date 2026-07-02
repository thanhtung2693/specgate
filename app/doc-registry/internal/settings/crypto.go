package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

// Crypto provides AES-256-GCM encryption for sensitive setting values.
type Crypto struct {
	aead cipher.AEAD
}

// NewCrypto creates a Crypto from a 32-byte hex-encoded key.
func NewCrypto(hexKey string) (*Crypto, error) {
	if hexKey == "" {
		return nil, errors.New("settings: encryption key is empty")
	}
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("settings: decode hex key: %w", err)
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("settings: key must be 32 bytes, got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, fmt.Errorf("settings: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("settings: new gcm: %w", err)
	}
	return &Crypto{aead: aead}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
func (c *Crypto) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("settings: random nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt decodes a base64-encoded (nonce || ciphertext) and decrypts it.
func (c *Crypto) Decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("settings: base64 decode: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(data) < nonceSize {
		return "", errors.New("settings: ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plain, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("settings: decrypt: %w", err)
	}
	return string(plain), nil
}
