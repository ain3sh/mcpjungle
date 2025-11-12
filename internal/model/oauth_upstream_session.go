package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// OAuthUpstreamSession tracks OAuth sessions with upstream MCP servers.
// When MCPJungle acts as an OAuth client to upstream servers, it stores
// the OAuth tokens and PKCE verifiers here.
type OAuthUpstreamSession struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// McpServerName references the upstream MCP server
	McpServerName string `gorm:"not null;uniqueIndex" json:"mcp_server_name"`

	// ClientID is our OAuth client ID registered with the upstream server
	ClientID string `json:"client_id"`

	// ClientSecret is our OAuth client secret (if confidential client)
	// TODO: Encrypt this in production
	ClientSecret string `json:"-"`

	// AccessToken is the current access token for the upstream server
	AccessToken string `json:"-"`

	// RefreshToken is the refresh token for obtaining new access tokens
	RefreshToken string `json:"-"`

	// TokenType is typically "Bearer"
	TokenType string `json:"token_type"`

	// ExpiresAt is when the current access token expires
	ExpiresAt *time.Time `gorm:"index" json:"expires_at,omitempty"`

	// Scope is the space-separated list of scopes granted
	Scope string `json:"scope"`

	// AuthorizationEndpoint is the upstream server's authorization URL
	AuthorizationEndpoint string `json:"authorization_endpoint"`

	// TokenEndpoint is the upstream server's token URL
	TokenEndpoint string `json:"token_endpoint"`

	// ResourceURI is the canonical URI of the MCP server (RFC 8707 resource indicator)
	// This is the value used in the "resource" parameter for token requests
	ResourceURI string `json:"resource_uri"`

	// CodeVerifier is the PKCE code verifier for the current auth flow
	// Temporarily stored during authorization, cleared after token exchange
	CodeVerifier string `json:"-"`

	// RedirectURI is the redirect URI we registered with the upstream server
	RedirectURI string `json:"redirect_uri"`

	// ClientInformation stores additional OAuth client metadata
	ClientInformation datatypes.JSON `gorm:"type:json" json:"client_information,omitempty"`
}

// TableName overrides the table name used by OAuthUpstreamSession
func (OAuthUpstreamSession) TableName() string {
	return "oauth_upstream_sessions"
}

// IsAccessTokenExpired checks if the access token needs refresh
func (s *OAuthUpstreamSession) IsAccessTokenExpired() bool {
	if s.ExpiresAt == nil {
		return false
	}
	// Refresh token 5 minutes before expiry to avoid race conditions
	return time.Now().Add(5 * time.Minute).After(*s.ExpiresAt)
}

// NeedsRefresh checks if we should refresh the access token
func (s *OAuthUpstreamSession) NeedsRefresh() bool {
	return s.RefreshToken != "" && s.IsAccessTokenExpired()
}
