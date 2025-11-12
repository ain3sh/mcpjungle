package model

import (
	"time"
)

// OAuthAuthorizationCode represents a temporary authorization code issued during the OAuth flow.
// Per MCP spec, these must support PKCE (Proof Key for Code Exchange).
type OAuthAuthorizationCode struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	CreatedAt time.Time `json:"created_at"`

	// Code is the authorization code value (cryptographically secure random string)
	Code string `gorm:"uniqueIndex;not null" json:"code"`

	// ClientID references the OAuth client this code was issued to
	ClientID string `gorm:"not null;index" json:"client_id"`

	// UserID references the user who authorized this code
	UserID uint `gorm:"not null;index" json:"user_id"`

	// RedirectURI is the exact redirect URI used in the authorization request
	// Must match exactly when exchanging code for token
	RedirectURI string `gorm:"not null" json:"redirect_uri"`

	// Scope is a space-separated list of granted scopes
	Scope string `json:"scope"`

	// ExpiresAt is when this code expires (typically 10 minutes)
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`

	// CodeChallenge is the PKCE code challenge (SHA256 hash of verifier)
	// REQUIRED per MCP OAuth specification
	CodeChallenge string `gorm:"not null" json:"code_challenge"`

	// CodeChallengeMethod is always "S256" per MCP spec (SHA256)
	CodeChallengeMethod string `gorm:"not null;default:S256" json:"code_challenge_method"`

	// Used tracks if this code has been exchanged for a token (prevent replay)
	Used bool `gorm:"not null;default:false;index" json:"used"`
}

// TableName overrides the table name used by OAuthAuthorizationCode
func (OAuthAuthorizationCode) TableName() string {
	return "oauth_authorization_codes"
}

// IsExpired checks if the authorization code has expired
func (c *OAuthAuthorizationCode) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

// IsValid checks if the code is valid (not used and not expired)
func (c *OAuthAuthorizationCode) IsValid() bool {
	return !c.Used && !c.IsExpired()
}
