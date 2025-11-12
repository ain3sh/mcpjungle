package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// OAuthClient represents an OAuth 2.0 client application (MCP clients like Claude Desktop, ChatGPT).
// Supports both confidential and public clients.
type OAuthClient struct {
	ID        uint           `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`

	// ClientID is the unique identifier for this OAuth client
	ClientID string `gorm:"uniqueIndex;not null" json:"client_id"`

	// ClientSecret is the secret for confidential clients (hashed)
	// Public clients (e.g., mobile apps) may not have a secret
	ClientSecret string `json:"-"`

	// ClientName is a human-readable name for this client
	ClientName string `gorm:"not null" json:"client_name"`

	// RedirectURIs is a JSON array of valid redirect URIs for this client
	// Required for authorization code flow
	RedirectURIs datatypes.JSON `gorm:"type:json" json:"redirect_uris"`

	// GrantTypes specifies which OAuth grant types this client can use
	// e.g., ["authorization_code", "refresh_token", "client_credentials"]
	GrantTypes datatypes.JSON `gorm:"type:json;not null" json:"grant_types"`

	// Scopes is a JSON array of scopes this client is allowed to request
	// Maps to tool groups and server access permissions
	Scopes datatypes.JSON `gorm:"type:json" json:"scopes"`

	// IsConfidential indicates if this is a confidential client (has secret)
	IsConfidential bool `gorm:"not null;default:true" json:"is_confidential"`

	// TokenEndpointAuthMethod specifies how client authenticates at token endpoint
	// e.g., "client_secret_basic", "client_secret_post", "none"
	TokenEndpointAuthMethod string `gorm:"not null;default:client_secret_basic" json:"token_endpoint_auth_method"`

	// UserID links this OAuth client to a specific user (for user-scoped clients)
	// If nil, client has system-level access
	UserID *uint `gorm:"index" json:"user_id,omitempty"`
	User   *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

// TableName overrides the table name used by OAuthClient to `oauth_clients`
func (OAuthClient) TableName() string {
	return "oauth_clients"
}
