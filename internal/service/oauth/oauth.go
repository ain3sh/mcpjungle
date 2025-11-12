// Package oauth provides OAuth 2.1 server and client functionality for MCPJungle.
// Implements MCP OAuth specification with PKCE support and resource indicators.
package oauth

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/util"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// OAuthService provides OAuth 2.1 functionality
type OAuthService struct {
	db *gorm.DB
}

// NewOAuthService creates a new OAuth service
func NewOAuthService(db *gorm.DB) *OAuthService {
	return &OAuthService{db: db}
}

// ===== Client Management =====

// RegisterClient registers a new OAuth client
func (s *OAuthService) RegisterClient(clientName string, redirectURIs []string, grantTypes []string, scopes []string, isConfidential bool) (*model.OAuthClient, error) {
	clientID, err := util.GenerateClientID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate client ID: %w", err)
	}

	var clientSecret string
	var hashedSecret string
	if isConfidential {
		clientSecret, err = util.GenerateClientSecret()
		if err != nil {
			return nil, fmt.Errorf("failed to generate client secret: %w", err)
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(clientSecret), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash client secret: %w", err)
		}
		hashedSecret = string(hashed)
	}

	// Default grant types if not specified
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}

	redirectURIsJSON, _ := datatypes.NewJSONType(redirectURIs).MarshalJSON()
	grantTypesJSON, _ := datatypes.NewJSONType(grantTypes).MarshalJSON()
	scopesJSON, _ := datatypes.NewJSONType(scopes).MarshalJSON()

	client := &model.OAuthClient{
		ClientID:                clientID,
		ClientSecret:            hashedSecret,
		ClientName:              clientName,
		RedirectURIs:            redirectURIsJSON,
		GrantTypes:              grantTypesJSON,
		Scopes:                  scopesJSON,
		IsConfidential:          isConfidential,
		TokenEndpointAuthMethod: "client_secret_basic",
	}

	if err := s.db.Create(client).Error; err != nil {
		return nil, fmt.Errorf("failed to create OAuth client: %w", err)
	}

	// Return the plain client secret only once during registration
	if isConfidential {
		client.ClientSecret = clientSecret
	}

	return client, nil
}

// GetClient retrieves an OAuth client by client ID
func (s *OAuthService) GetClient(clientID string) (*model.OAuthClient, error) {
	var client model.OAuthClient
	if err := s.db.Where("client_id = ?", clientID).First(&client).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OAuth client: %w", err)
	}
	return &client, nil
}

// ValidateClientCredentials validates client ID and secret
func (s *OAuthService) ValidateClientCredentials(clientID, clientSecret string) (*model.OAuthClient, error) {
	client, err := s.GetClient(clientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("client not found")
	}

	if client.IsConfidential {
		if err := bcrypt.CompareHashAndPassword([]byte(client.ClientSecret), []byte(clientSecret)); err != nil {
			return nil, fmt.Errorf("invalid client secret")
		}
	}

	return client, nil
}

// ValidateRedirectURI checks if redirect URI is registered for the client
func (s *OAuthService) ValidateRedirectURI(client *model.OAuthClient, redirectURI string) bool {
	var redirectURIs []string
	if err := json.Unmarshal(client.RedirectURIs, &redirectURIs); err != nil {
		return false
	}

	for _, uri := range redirectURIs {
		if uri == redirectURI {
			return true
		}
	}
	return false
}

// ===== Authorization Code Flow =====

// CreateAuthorizationCode creates a new authorization code with PKCE
func (s *OAuthService) CreateAuthorizationCode(clientID string, userID uint, redirectURI, scope, codeChallenge, codeChallengeMethod string) (string, error) {
	// Validate code challenge method (must be S256 per MCP spec)
	if codeChallengeMethod != "S256" {
		return "", fmt.Errorf("invalid code_challenge_method: only S256 is supported")
	}

	code, err := util.GenerateAuthorizationCode()
	if err != nil {
		return "", fmt.Errorf("failed to generate authorization code: %w", err)
	}

	authCode := &model.OAuthAuthorizationCode{
		Code:                    code,
		ClientID:                clientID,
		UserID:                  userID,
		RedirectURI:             redirectURI,
		Scope:                   scope,
		ExpiresAt:               time.Now().Add(10 * time.Minute),
		CodeChallenge:           codeChallenge,
		CodeChallengeMethod:     codeChallengeMethod,
		Used:                    false,
	}

	if err := s.db.Create(authCode).Error; err != nil {
		return "", fmt.Errorf("failed to create authorization code: %w", err)
	}

	return code, nil
}

// GetAuthorizationCode retrieves an authorization code
func (s *OAuthService) GetAuthorizationCode(code string) (*model.OAuthAuthorizationCode, error) {
	var authCode model.OAuthAuthorizationCode
	if err := s.db.Where("code = ?", code).First(&authCode).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get authorization code: %w", err)
	}
	return &authCode, nil
}

// MarkAuthorizationCodeUsed marks an authorization code as used
func (s *OAuthService) MarkAuthorizationCodeUsed(code string) error {
	return s.db.Model(&model.OAuthAuthorizationCode{}).Where("code = ?", code).Update("used", true).Error
}

// ===== Token Management =====

// IssueAccessToken issues a new access token
func (s *OAuthService) IssueAccessToken(clientID string, userID *uint, scope, audience string, refreshTokenID *uint) (*model.OAuthAccessToken, error) {
	token, err := util.GenerateOAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}

	accessToken := &model.OAuthAccessToken{
		AccessToken:    token,
		ClientID:       clientID,
		UserID:         userID,
		Scope:          scope,
		ExpiresAt:      time.Now().Add(1 * time.Hour), // 1 hour expiry
		RefreshTokenID: refreshTokenID,
		Audience:       audience,
		Revoked:        false,
	}

	if err := s.db.Create(accessToken).Error; err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	return accessToken, nil
}

// IssueRefreshToken issues a new refresh token
func (s *OAuthService) IssueRefreshToken(clientID string, userID uint, scope string) (*model.OAuthRefreshToken, error) {
	token, err := util.GenerateOAuthToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate refresh token: %w", err)
	}

	refreshToken := &model.OAuthRefreshToken{
		RefreshToken:  token,
		ClientID:      clientID,
		UserID:        userID,
		Scope:         scope,
		ExpiresAt:     time.Now().Add(30 * 24 * time.Hour), // 30 days expiry
		Revoked:       false,
		RotationCount: 0,
	}

	if err := s.db.Create(refreshToken).Error; err != nil {
		return nil, fmt.Errorf("failed to create refresh token: %w", err)
	}

	return refreshToken, nil
}

// ValidateAccessToken validates an access token and returns it if valid
func (s *OAuthService) ValidateAccessToken(token string) (*model.OAuthAccessToken, error) {
	var accessToken model.OAuthAccessToken
	if err := s.db.Where("access_token = ?", token).First(&accessToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("token not found")
		}
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	if !accessToken.IsValid() {
		return nil, fmt.Errorf("token is invalid or expired")
	}

	return &accessToken, nil
}

// ValidateRefreshToken validates a refresh token and returns it if valid
func (s *OAuthService) ValidateRefreshToken(token string) (*model.OAuthRefreshToken, error) {
	var refreshToken model.OAuthRefreshToken
	if err := s.db.Where("refresh_token = ?", token).First(&refreshToken).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("token not found")
		}
		return nil, fmt.Errorf("failed to get refresh token: %w", err)
	}

	if !refreshToken.IsValid() {
		return nil, fmt.Errorf("token is invalid or expired")
	}

	return &refreshToken, nil
}

// RevokeAccessToken revokes an access token
func (s *OAuthService) RevokeAccessToken(token string) error {
	return s.db.Model(&model.OAuthAccessToken{}).Where("access_token = ?", token).Update("revoked", true).Error
}

// RevokeRefreshToken revokes a refresh token
func (s *OAuthService) RevokeRefreshToken(token string) error {
	return s.db.Model(&model.OAuthRefreshToken{}).Where("refresh_token = ?", token).Update("revoked", true).Error
}

// IncrementRefreshTokenRotation increments the rotation count for a refresh token
func (s *OAuthService) IncrementRefreshTokenRotation(tokenID uint) error {
	return s.db.Model(&model.OAuthRefreshToken{}).Where("id = ?", tokenID).UpdateColumn("rotation_count", gorm.Expr("rotation_count + ?", 1)).Error
}

// ===== Scope Management =====

// ValidateScopes checks if requested scopes are allowed for the client
func (s *OAuthService) ValidateScopes(client *model.OAuthClient, requestedScopes string) (string, error) {
	var allowedScopes []string
	if err := json.Unmarshal(client.Scopes, &allowedScopes); err != nil {
		return "", fmt.Errorf("failed to parse client scopes: %w", err)
	}

	// If no scopes are configured, allow any scope
	if len(allowedScopes) == 0 {
		return requestedScopes, nil
	}

	// Split requested scopes and validate each one
	requested := strings.Split(requestedScopes, " ")
	var validated []string

	for _, scope := range requested {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}

		allowed := false
		for _, allowedScope := range allowedScopes {
			if scope == allowedScope {
				allowed = true
				break
			}
		}

		if allowed {
			validated = append(validated, scope)
		} else {
			return "", fmt.Errorf("scope not allowed: %s", scope)
		}
	}

	return strings.Join(validated, " "), nil
}

// ===== Cleanup =====

// CleanupExpiredTokens removes expired authorization codes and tokens
func (s *OAuthService) CleanupExpiredTokens() error {
	now := time.Now()

	// Clean up expired authorization codes
	if err := s.db.Where("expires_at < ?", now).Delete(&model.OAuthAuthorizationCode{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup authorization codes: %w", err)
	}

	// Clean up expired access tokens
	if err := s.db.Where("expires_at < ? AND revoked = ?", now, false).Delete(&model.OAuthAccessToken{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup access tokens: %w", err)
	}

	// Clean up expired refresh tokens
	if err := s.db.Where("expires_at < ? AND revoked = ?", now, false).Delete(&model.OAuthRefreshToken{}).Error; err != nil {
		return fmt.Errorf("failed to cleanup refresh tokens: %w", err)
	}

	return nil
}
