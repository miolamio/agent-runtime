package students

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const tokenPrefix = "sk-ai-"

// bcryptCost controls the work factor for bcrypt. Tests override this to
// bcrypt.MinCost to stay fast; production code should not touch it.
var bcryptCost = bcrypt.DefaultCost

// GenerateToken creates a new random user token with sk-ai- prefix.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return tokenPrefix + hex.EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 hash of the given token. Kept for
// backward compatibility — existing v0.6.0 students.json files store tokens
// in this format and are transparently verified (and upgraded) by VerifyToken.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// HashTokenBcrypt returns a bcrypt hash of the given token. New tokens (v0.6.2+)
// are stored in this format.
func HashTokenBcrypt(token string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("bcrypt hash: %w", err)
	}
	return string(hash), nil
}

// VerifyToken compares a plaintext token against a stored hash of either
// algorithm. upgradeNeeded is true when the stored hash is legacy SHA-256 —
// the caller should re-hash the plaintext with bcrypt and persist the result.
func VerifyToken(plaintext, stored string) (match, upgradeNeeded bool) {
	switch {
	case isBcryptHash(stored):
		err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(plaintext))
		return err == nil, false
	case isSHA256Hash(stored):
		want := HashToken(plaintext)
		return subtle.ConstantTimeCompare([]byte(stored), []byte(want)) == 1, true
	default:
		return false, false
	}
}

// isBcryptHash reports whether s looks like a bcrypt-encoded hash.
func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}

// isSHA256Hash reports whether s could be a 64-hex SHA-256 digest.
func isSHA256Hash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isDigit := c >= '0' && c <= '9'
		isHex := c >= 'a' && c <= 'f'
		if !isDigit && !isHex {
			return false
		}
	}
	return true
}
