package students

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestMain dials bcrypt cost down to the minimum allowed value so tests aren't
// throttled by the default cost of 10 (~100ms per hash). Production unaffected.
func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

func TestHashTokenBcrypt_RoundTrip(t *testing.T) {
	tok := "sk-ai-abcdef1234567890"
	hash, err := HashTokenBcrypt(tok)
	if err != nil {
		t.Fatalf("HashTokenBcrypt: %v", err)
	}
	if !isBcryptHash(hash) {
		t.Errorf("hash %q does not look like bcrypt", hash)
	}
	if !strings.HasPrefix(hash, "$2") {
		t.Errorf("expected $2 prefix, got %q", hash[:4])
	}

	// bcrypt is randomized — two hashes of the same input must differ.
	hash2, _ := HashTokenBcrypt(tok)
	if hash == hash2 {
		t.Error("bcrypt hash should include a fresh salt each call")
	}

	// Correct plaintext verifies.
	if match, upgrade := VerifyToken(tok, hash); !match || upgrade {
		t.Errorf("VerifyToken(plain, bcrypt): match=%v upgrade=%v, want match=true upgrade=false", match, upgrade)
	}

	// Wrong plaintext rejects.
	if match, _ := VerifyToken("different", hash); match {
		t.Error("VerifyToken should reject wrong plaintext for bcrypt hash")
	}
}

func TestVerifyToken_SHA256_TriggersUpgrade(t *testing.T) {
	tok := "sk-ai-legacy-user-token"
	stored := HashToken(tok) // legacy v0.6.0/v0.6.1 format

	match, upgrade := VerifyToken(tok, stored)
	if !match {
		t.Error("SHA-256 match should verify")
	}
	if !upgrade {
		t.Error("SHA-256 match should signal upgrade-needed")
	}
}

func TestVerifyToken_UnknownHashRejects(t *testing.T) {
	// Something that is neither bcrypt nor 64-hex.
	stored := "garbage-string-42"
	match, upgrade := VerifyToken("anything", stored)
	if match || upgrade {
		t.Errorf("unknown hash should not verify: match=%v upgrade=%v", match, upgrade)
	}
}

func TestIsSHA256Hash_NonHex(t *testing.T) {
	// 64 chars but contains non-hex — must reject so we don't try HashToken on
	// random strings that happen to be 64 chars.
	bogus := strings.Repeat("g", 64)
	if isSHA256Hash(bogus) {
		t.Error("non-hex 64-char string wrongly classified as SHA-256")
	}
}
