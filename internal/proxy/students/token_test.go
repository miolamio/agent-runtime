package students

import (
	"strings"
	"testing"
)

func TestGenerateToken(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	if !strings.HasPrefix(tok, "sk-ai-") {
		t.Errorf("token %q does not start with sk-ai-", tok)
	}
	// 32 bytes hex = 64 chars + "sk-ai-" prefix = 70
	if len(tok) != 70 {
		t.Errorf("token length = %d, want 70", len(tok))
	}
}

func TestGenerateTokenUniqueness(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, _ := GenerateToken()
		if tokens[tok] {
			t.Fatalf("duplicate token on iteration %d", i)
		}
		tokens[tok] = true
	}
}
