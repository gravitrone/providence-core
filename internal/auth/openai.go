package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	OpenAIClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAIAuthorizeURL = "https://auth.openai.com/oauth/authorize"
	OpenAITokenURL     = "https://auth.openai.com/oauth/token"
	OpenAIRedirectURI  = "http://localhost:1455/auth/callback"
	OpenAIScope        = "openid profile email offline_access"
	CodexEndpoint      = "https://chatgpt.com/backend-api/codex/responses"
)

// OpenAITokens holds the OAuth tokens returned by the OpenAI auth flow.
type OpenAITokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	AccountID    string    `json:"account_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// IsExpired returns true if the access token will expire within 8 minutes.
func (t *OpenAITokens) IsExpired() bool {
	return time.Now().After(t.ExpiresAt.Add(-8 * time.Minute))
}

// GeneratePKCE creates a PKCE verifier and S256 challenge.
func GeneratePKCE() (verifier, challenge string, err error) {
	buf := make([]byte, 64)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("generate pkce random: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// generateState creates a random hex state parameter.
func generateState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// LoginOpenAI runs the full PKCE OAuth flow:
// 1. Generate PKCE verifier + S256 challenge
// 2. Start local HTTP server on :1455
// 3. Open browser to authorize URL
// 4. Catch callback with authorization code
// 5. Exchange code for tokens
// 6. Extract chatgpt_account_id from JWT
// 7. Return tokens
func LoginOpenAI() (*OpenAITokens, error) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("pkce generation: %w", err)
	}

	state, err := generateState()
	if err != nil {
		return nil, fmt.Errorf("state generation: %w", err)
	}

	// Build authorize URL.
	params := url.Values{
		"client_id":             {OpenAIClientID},
		"redirect_uri":          {OpenAIRedirectURI},
		"response_type":         {"code"},
		"scope":                 {OpenAIScope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	authorizeURL := OpenAIAuthorizeURL + "?" + params.Encode()

	// Channel to receive the authorization code.
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)

	// Start local HTTP server.
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch")}
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			resultCh <- callbackResult{err: fmt.Errorf("oauth error: %s - %s", errParam, desc)}
			http.Error(w, "auth error: "+errParam, http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			resultCh <- callbackResult{err: fmt.Errorf("no code in callback")}
			http.Error(w, "no code", http.StatusBadRequest)
			return
		}
		resultCh <- callbackResult{code: code}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<html><body><h2>Login successful!</h2><p>You can close this tab.</p><script>window.close()</script></body></html>`)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:1455")
	if err != nil {
		return nil, fmt.Errorf("start callback server: %w", err)
	}
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Shutdown(context.Background())
	}()

	// Open browser.
	if err := exec.Command("open", authorizeURL).Start(); err != nil {
		return nil, fmt.Errorf("open browser: %w (url: %s)", err, authorizeURL)
	}

	// Wait for callback (5 minute timeout).
	var code string
	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		code = res.code
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("login timeout (5 minutes)")
	}

	// Exchange code for tokens.
	return exchangeCode(code, verifier)
}

// exchangeCode exchanges an authorization code for tokens.
func exchangeCode(code, verifier string) (*OpenAITokens, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {OpenAIClientID},
		"code":          {code},
		"redirect_uri":  {OpenAIRedirectURI},
		"code_verifier": {verifier},
	}

	resp, err := http.PostForm(OpenAITokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	tokens := &OpenAITokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	// Extract account ID from JWT.
	accountID, err := extractAccountID(tokenResp.IDToken)
	if err != nil {
		// Non-fatal - some flows might not include it.
		accountID = ""
	}
	tokens.AccountID = accountID

	return tokens, nil
}

// extractAccountID decodes the JWT payload and pulls chatgpt_account_id.
func extractAccountID(idToken string) (string, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return "", fmt.Errorf("invalid jwt: not enough parts")
	}

	// base64url decode the payload (add padding if needed).
	payload := parts[1]
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode jwt payload: %w", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", fmt.Errorf("parse jwt claims: %w", err)
	}

	// OpenAI puts account info under "https://api.openai.com/auth" claim.
	authClaim, ok := claims["https://api.openai.com/auth"]
	if !ok {
		return "", fmt.Errorf("no auth claim in jwt")
	}

	authMap, ok := authClaim.(map[string]any)
	if !ok {
		return "", fmt.Errorf("auth claim is not an object")
	}

	accountID, ok := authMap["chatgpt_account_id"].(string)
	if !ok {
		return "", fmt.Errorf("no chatgpt_account_id in auth claim")
	}
	return accountID, nil
}

// RefreshOpenAI refreshes an expired access token using the refresh token.
func RefreshOpenAI(refreshToken string) (*OpenAITokens, error) {
	data := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {OpenAIClientID},
		"refresh_token": {refreshToken},
	}

	resp, err := http.PostForm(OpenAITokenURL, data)
	if err != nil {
		return nil, fmt.Errorf("refresh request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	tokens := &OpenAITokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		IDToken:      tokenResp.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	// If we got a new refresh token, use it; otherwise keep the old one.
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = refreshToken
	}

	// Extract account ID from new JWT.
	if tokens.IDToken != "" {
		if accountID, err := extractAccountID(tokens.IDToken); err == nil {
			tokens.AccountID = accountID
		}
	}

	return tokens, nil
}

// tokenFilePath returns ~/.providence/openai-auth.json.
func tokenFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".providence", "openai-auth.json"), nil
}

// LoadOpenAITokens loads tokens from ~/.providence/openai-auth.json.
func LoadOpenAITokens() (*OpenAITokens, error) {
	path, err := tokenFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var tokens OpenAITokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}
	return &tokens, nil
}

// SaveOpenAITokens saves tokens to ~/.providence/openai-auth.json.
func SaveOpenAITokens(tokens *OpenAITokens) error {
	path, err := tokenFilePath()
	if err != nil {
		return err
	}

	// Create directory if needed.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	// Write with restrictive permissions.
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	return nil
}

// EnsureValidOpenAITokens loads tokens, refreshes if expired, and saves back.
func EnsureValidOpenAITokens() (*OpenAITokens, error) {
	tokens, err := LoadOpenAITokens()
	if err != nil {
		return nil, fmt.Errorf("no saved tokens (run /auth first): %w", err)
	}

	if !tokens.IsExpired() {
		return tokens, nil
	}

	if tokens.RefreshToken == "" {
		return nil, fmt.Errorf("token expired and no refresh token (run /auth again)")
	}

	refreshed, err := RefreshOpenAI(tokens.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh failed (run /auth again): %w", err)
	}

	if err := SaveOpenAITokens(refreshed); err != nil {
		// Non-fatal - tokens are still valid in memory.
		_ = err
	}
	return refreshed, nil
}
