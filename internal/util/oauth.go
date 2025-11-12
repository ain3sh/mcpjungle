package util

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// GenerateOAuthToken generates a cryptographically secure random OAuth token (32 bytes)
func GenerateOAuthToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateAuthorizationCode generates a cryptographically secure authorization code (32 bytes)
func GenerateAuthorizationCode() (string, error) {
	return GenerateOAuthToken()
}

// GeneratePKCEVerifier generates a PKCE code verifier (43-128 characters)
// Per RFC 7636, must be 43-128 characters from [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~"
func GeneratePKCEVerifier() (string, error) {
	// Generate 32 random bytes (will produce 43 base64url characters)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GeneratePKCEChallenge generates a PKCE code challenge from a verifier using S256 method
// Per MCP OAuth spec, S256 (SHA256) is REQUIRED
func GeneratePKCEChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}

// VerifyPKCE verifies that a code verifier matches the code challenge
func VerifyPKCE(verifier, challenge, method string) bool {
	if method != "S256" {
		// Per MCP spec, only S256 is supported
		return false
	}
	expectedChallenge := GeneratePKCEChallenge(verifier)
	return expectedChallenge == challenge
}

// GenerateClientID generates a unique OAuth client ID
func GenerateClientID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate client ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateClientSecret generates a cryptographically secure client secret
func GenerateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate client secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
