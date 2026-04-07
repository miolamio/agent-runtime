package students

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

const tokenPrefix = "sk-ai-"

// GenerateToken creates a new random user token with sk-ai- prefix.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + hex.EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 hash of the given token.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
