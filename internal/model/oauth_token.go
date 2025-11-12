package model

import (
	"time"

	"gorm.io/gorm"
)

// OAuthAccessToken represents an OAuth 2.0 access token.
// Per MCP spec, tokens must be bound to specific resource servers (audience).
type OAuthAccessToken struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// AccessToken is the bearer token value (cryptographically secure random string)
	AccessToken string `gorm:"uniqueIndex;not null" json:"access_token"`

	// ClientID references the OAuth client this token was issued to
	ClientID string `gorm:"not null;index" json:"client_id"`

	// UserID references the user this token represents
	// May be nil for client_credentials grants
	UserID *uint `gorm:"index" json:"user_id,omitempty"`

	// Scope is a space-separated list of granted scopes
	Scope string `json:"scope"`

	// ExpiresAt is when this token expires
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`

	// RefreshTokenID links to the refresh token that can refresh this access token
	RefreshTokenID *uint          `gorm:"index" json:"refresh_token_id,omitempty"`
	RefreshToken   *OAuthRefreshToken `gorm:"foreignKey:RefreshTokenID" json:"refresh_token,omitempty"`

	// Audience is the intended resource server for this token (MCP server URL)
	// REQUIRED per MCP spec (RFC 8707 - Resource Indicators)
	Audience string `gorm:"index" json:"audience"`

	// Revoked indicates if this token has been explicitly revoked
	Revoked bool `gorm:"not null;default:false;index" json:"revoked"`
}

// TableName overrides the table name used by OAuthAccessToken
func (OAuthAccessToken) TableName() string {
	return "oauth_access_tokens"
}

// IsExpired checks if the access token has expired
func (t *OAuthAccessToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsValid checks if the token is valid (not revoked and not expired)
func (t *OAuthAccessToken) IsValid() bool {
	return !t.Revoked && !t.IsExpired()
}

// OAuthRefreshToken represents an OAuth 2.0 refresh token.
// Used to obtain new access tokens without requiring re-authentication.
type OAuthRefreshToken struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// RefreshToken is the refresh token value
	RefreshToken string `gorm:"uniqueIndex;not null" json:"refresh_token"`

	// ClientID references the OAuth client this token was issued to
	ClientID string `gorm:"not null;index" json:"client_id"`

	// UserID references the user this token represents
	UserID uint `gorm:"not null;index" json:"user_id"`

	// Scope is a space-separated list of granted scopes
	Scope string `json:"scope"`

	// ExpiresAt is when this refresh token expires (typically days or weeks)
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`

	// Revoked indicates if this token has been explicitly revoked
	Revoked bool `gorm:"not null;default:false;index" json:"revoked"`

	// RotationCount tracks how many times this refresh token has been used
	// Can be used to implement rotation policies
	RotationCount int `gorm:"not null;default:0" json:"rotation_count"`
}

// TableName overrides the table name used by OAuthRefreshToken
func (OAuthRefreshToken) TableName() string {
	return "oauth_refresh_tokens"
}

// IsExpired checks if the refresh token has expired
func (t *OAuthRefreshToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// IsValid checks if the refresh token is valid (not revoked and not expired)
func (t *OAuthRefreshToken) IsValid() bool {
	return !t.Revoked && !t.IsExpired()
}
