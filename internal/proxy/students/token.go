package students

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

const tokenPrefix = "sk-ai-"

// GenerateToken creates a new random student token with sk-ai- prefix.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + hex.EncodeToString(b), nil
}
