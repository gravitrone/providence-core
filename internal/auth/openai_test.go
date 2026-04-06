package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE failed: %v", err)
	}

	// Verifier should be base64url-encoded 64 bytes = 86 chars (no padding).
	if len(verifier) != 86 {
		t.Errorf("verifier length = %d, want 86", len(verifier))
	}

	// Challenge should be base64url-encoded SHA256 = 43 chars (no padding).
	if len(challenge) != 43 {
		t.Errorf("challenge length = %d, want 43", len(challenge))
	}

	// Both should be valid base64url (no padding).
	if _, err := base64.RawURLEncoding.DecodeString(verifier); err != nil {
		t.Errorf("verifier is not valid base64url: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		t.Errorf("challenge is not valid base64url: %v", err)
	}

	// Two calls should produce different results.
	v2, c2, _ := GeneratePKCE()
	if verifier == v2 {
		t.Error("two verifiers should be different")
	}
	if challenge == c2 {
		t.Error("two challenges should be different")
	}
}

func TestExtractAccountID(t *testing.T) {
	// Build a fake JWT with the expected claim structure.
	claims := map[string]any{
		"sub":   "user-123",
		"email": "test@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct_abc123",
			"user_id":            "user-123",
		},
	}
	payload, _ := json.Marshal(claims)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)

	// JWT = header.payload.signature (we only need the payload part).
	fakeJWT := "eyJhbGciOiJSUzI1NiJ9." + encodedPayload + ".fake-signature"

	accountID, err := extractAccountID(fakeJWT)
	if err != nil {
		t.Fatalf("extractAccountID failed: %v", err)
	}
	if accountID != "acct_abc123" {
		t.Errorf("accountID = %q, want %q", accountID, "acct_abc123")
	}
}

func TestExtractAccountID_MissingClaim(t *testing.T) {
	claims := map[string]any{"sub": "user-123"}
	payload, _ := json.Marshal(claims)
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	fakeJWT := "header." + encoded + ".sig"

	_, err := extractAccountID(fakeJWT)
	if err == nil {
		t.Error("expected error for missing auth claim")
	}
}

func TestExtractAccountID_InvalidJWT(t *testing.T) {
	_, err := extractAccountID("not-a-jwt")
	if err == nil {
		t.Error("expected error for invalid jwt")
	}
}

func TestTokenExpiry(t *testing.T) {
	// Token that expires in 10 minutes - should NOT be expired (8 min buffer).
	tokens := &OpenAITokens{
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if tokens.IsExpired() {
		t.Error("token expiring in 10min should not be expired")
	}

	// Token that expires in 5 minutes - should BE expired (within 8 min buffer).
	tokens.ExpiresAt = time.Now().Add(5 * time.Minute)
	if !tokens.IsExpired() {
		t.Error("token expiring in 5min should be expired (8min buffer)")
	}

	// Token that already expired.
	tokens.ExpiresAt = time.Now().Add(-1 * time.Minute)
	if !tokens.IsExpired() {
		t.Error("already expired token should be expired")
	}
}

func TestTokenSerialization(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "openai-auth.json")

	original := &OpenAITokens{
		AccessToken:  "access-token-123",
		RefreshToken: "refresh-token-456",
		IDToken:      "id-token-789",
		AccountID:    "acct_abc",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Truncate(time.Second),
	}

	// Write directly (bypassing the home dir logic).
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back.
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var loaded OpenAITokens
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.AccessToken != original.AccessToken {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, original.AccessToken)
	}
	if loaded.RefreshToken != original.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, original.RefreshToken)
	}
	if loaded.AccountID != original.AccountID {
		t.Errorf("AccountID = %q, want %q", loaded.AccountID, original.AccountID)
	}
	if !loaded.ExpiresAt.Equal(original.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", loaded.ExpiresAt, original.ExpiresAt)
	}
}
