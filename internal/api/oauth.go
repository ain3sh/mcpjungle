package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/service/oauth"
	"github.com/mcpjungle/mcpjungle/internal/util"
)

// ===== OAuth Authorization Endpoint =====

// OAuthAuthorizeHandler handles the OAuth authorization endpoint
// GET /oauth/authorize
func (s *Server) OAuthAuthorizeHandler(c *gin.Context) {
	// Extract parameters
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	responseType := c.Query("response_type")
	scope := c.Query("scope")
	state := c.Query("state")
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.Query("code_challenge_method")

	// Validate required parameters
	if clientID == "" || redirectURI == "" || responseType == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Missing required parameters",
		})
		return
	}

	// Only authorization_code response type is supported
	if responseType != "code" {
		redirectError(c, redirectURI, state, "unsupported_response_type", "Only authorization_code flow is supported")
		return
	}

	// PKCE is REQUIRED per MCP spec
	if codeChallenge == "" || codeChallengeMethod == "" {
		redirectError(c, redirectURI, state, "invalid_request", "PKCE is required: code_challenge and code_challenge_method must be provided")
		return
	}

	// Only S256 method is supported per MCP spec
	if codeChallengeMethod != "S256" {
		redirectError(c, redirectURI, state, "invalid_request", "Only S256 code_challenge_method is supported")
		return
	}

	// Validate client
	oauthService := oauth.NewOAuthService(s.db)
	client, err := oauthService.GetClient(clientID)
	if err != nil || client == nil {
		redirectError(c, redirectURI, state, "invalid_client", "Client not found")
		return
	}

	// Validate redirect URI
	if !oauthService.ValidateRedirectURI(client, redirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Invalid redirect_uri",
		})
		return
	}

	// Validate scopes
	validatedScope, err := oauthService.ValidateScopes(client, scope)
	if err != nil {
		redirectError(c, redirectURI, state, "invalid_scope", err.Error())
		return
	}

	// Get authenticated user from context (set by auth middleware)
	userIDInterface, exists := c.Get("user_id")
	if !exists {
		// User not authenticated - redirect to login
		// In a real implementation, this would redirect to a login page
		// For now, return an error
		redirectError(c, redirectURI, state, "access_denied", "User authentication required")
		return
	}

	userID, ok := userIDInterface.(uint)
	if !ok {
		redirectError(c, redirectURI, state, "server_error", "Invalid user session")
		return
	}

	// Generate authorization code
	code, err := oauthService.CreateAuthorizationCode(clientID, userID, redirectURI, validatedScope, codeChallenge, codeChallengeMethod)
	if err != nil {
		s.logger.Errorf("Failed to create authorization code: %v", err)
		redirectError(c, redirectURI, state, "server_error", "Failed to generate authorization code")
		return
	}

	// Redirect back to client with authorization code
	redirectURL, _ := url.Parse(redirectURI)
	query := redirectURL.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	redirectURL.RawQuery = query.Encode()

	c.Redirect(http.StatusFound, redirectURL.String())
}

// redirectError redirects to the redirect_uri with an error
func redirectError(c *gin.Context, redirectURI, state, errorCode, errorDescription string) {
	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "Invalid redirect_uri",
		})
		return
	}

	query := redirectURL.Query()
	query.Set("error", errorCode)
	if errorDescription != "" {
		query.Set("error_description", errorDescription)
	}
	if state != "" {
		query.Set("state", state)
	}
	redirectURL.RawQuery = query.Encode()

	c.Redirect(http.StatusFound, redirectURL.String())
}

// ===== OAuth Token Endpoint =====

// OAuthTokenRequest represents the token endpoint request
type OAuthTokenRequest struct {
	GrantType    string `form:"grant_type" binding:"required"`
	Code         string `form:"code"`
	RedirectURI  string `form:"redirect_uri"`
	CodeVerifier string `form:"code_verifier"`
	RefreshToken string `form:"refresh_token"`
	ClientID     string `form:"client_id"`
	ClientSecret string `form:"client_secret"`
	Scope        string `form:"scope"`
	Resource     string `form:"resource"` // RFC 8707 - Resource Indicators
}

// OAuthTokenResponse represents the token endpoint response
type OAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// OAuthTokenHandler handles the OAuth token endpoint
// POST /oauth/token
func (s *Server) OAuthTokenHandler(c *gin.Context) {
	var req OAuthTokenRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": err.Error(),
		})
		return
	}

	// Extract client credentials from Authorization header or request body
	clientID, clientSecret := extractClientCredentials(c, req.ClientID, req.ClientSecret)
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_client",
			"error_description": "Client authentication required",
		})
		return
	}

	oauthService := oauth.NewOAuthService(s.db)

	// Validate client (may not have secret for public clients)
	client, err := oauthService.GetClient(clientID)
	if err != nil || client == nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Client not found",
		})
		return
	}

	// Validate client secret if confidential client
	if client.IsConfidential {
		if _, err := oauthService.ValidateClientCredentials(clientID, clientSecret); err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error":             "invalid_client",
				"error_description": "Invalid client credentials",
			})
			return
		}
	}

	switch req.GrantType {
	case "authorization_code":
		s.handleAuthorizationCodeGrant(c, oauthService, client, &req)
	case "refresh_token":
		s.handleRefreshTokenGrant(c, oauthService, client, &req)
	case "client_credentials":
		s.handleClientCredentialsGrant(c, oauthService, client, &req)
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "unsupported_grant_type",
			"error_description": "Grant type not supported",
		})
	}
}

// handleAuthorizationCodeGrant handles the authorization_code grant type
func (s *Server) handleAuthorizationCodeGrant(c *gin.Context, oauthService *oauth.OAuthService, client *model.OAuthClient, req *OAuthTokenRequest) {
	// Validate required parameters
	if req.Code == "" || req.RedirectURI == "" || req.CodeVerifier == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "code, redirect_uri, and code_verifier are required",
		})
		return
	}

	// Get authorization code
	authCode, err := oauthService.GetAuthorizationCode(req.Code)
	if err != nil || authCode == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Invalid authorization code",
		})
		return
	}

	// Validate authorization code
	if !authCode.IsValid() {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Authorization code expired or already used",
		})
		return
	}

	// Validate client ID matches
	if authCode.ClientID != client.ClientID {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Authorization code was issued to a different client",
		})
		return
	}

	// Validate redirect URI matches
	if authCode.RedirectURI != req.RedirectURI {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "redirect_uri does not match",
		})
		return
	}

	// Verify PKCE (REQUIRED per MCP spec)
	if !util.VerifyPKCE(req.CodeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Invalid code_verifier",
		})
		return
	}

	// Mark authorization code as used
	if err := oauthService.MarkAuthorizationCodeUsed(req.Code); err != nil {
		s.logger.Errorf("Failed to mark authorization code as used: %v", err)
	}

	// Determine audience (resource server)
	audience := req.Resource
	if audience == "" {
		// Default to the MCPJungle server itself
		audience = getServerURL(c)
	}

	// Issue refresh token
	refreshToken, err := oauthService.IssueRefreshToken(client.ClientID, authCode.UserID, authCode.Scope)
	if err != nil {
		s.logger.Errorf("Failed to issue refresh token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to issue tokens",
		})
		return
	}

	// Issue access token
	accessToken, err := oauthService.IssueAccessToken(client.ClientID, &authCode.UserID, authCode.Scope, audience, &refreshToken.ID)
	if err != nil {
		s.logger.Errorf("Failed to issue access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to issue tokens",
		})
		return
	}

	// Return token response
	c.JSON(http.StatusOK, OAuthTokenResponse{
		AccessToken:  accessToken.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(time.Until(accessToken.ExpiresAt).Seconds()),
		RefreshToken: refreshToken.RefreshToken,
		Scope:        accessToken.Scope,
	})
}

// handleRefreshTokenGrant handles the refresh_token grant type
func (s *Server) handleRefreshTokenGrant(c *gin.Context, oauthService *oauth.OAuthService, client *model.OAuthClient, req *OAuthTokenRequest) {
	if req.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "refresh_token is required",
		})
		return
	}

	// Validate refresh token
	refreshToken, err := oauthService.ValidateRefreshToken(req.RefreshToken)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Invalid refresh token",
		})
		return
	}

	// Validate client ID matches
	if refreshToken.ClientID != client.ClientID {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_grant",
			"error_description": "Refresh token was issued to a different client",
		})
		return
	}

	// Determine scope (can request narrower scope)
	scope := refreshToken.Scope
	if req.Scope != "" {
		// Validate requested scope is subset of original scope
		validatedScope, err := oauthService.ValidateScopes(client, req.Scope)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_scope",
				"error_description": err.Error(),
			})
			return
		}
		scope = validatedScope
	}

	// Determine audience
	audience := req.Resource
	if audience == "" {
		audience = getServerURL(c)
	}

	// Issue new access token
	accessToken, err := oauthService.IssueAccessToken(client.ClientID, &refreshToken.UserID, scope, audience, &refreshToken.ID)
	if err != nil {
		s.logger.Errorf("Failed to issue access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to issue token",
		})
		return
	}

	// Increment rotation count
	if err := oauthService.IncrementRefreshTokenRotation(refreshToken.ID); err != nil {
		s.logger.Warnf("Failed to increment refresh token rotation count: %v", err)
	}

	// Return token response
	c.JSON(http.StatusOK, OAuthTokenResponse{
		AccessToken:  accessToken.AccessToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(time.Until(accessToken.ExpiresAt).Seconds()),
		RefreshToken: refreshToken.RefreshToken,
		Scope:        accessToken.Scope,
	})
}

// handleClientCredentialsGrant handles the client_credentials grant type
func (s *Server) handleClientCredentialsGrant(c *gin.Context, oauthService *oauth.OAuthService, client *model.OAuthClient, req *OAuthTokenRequest) {
	// Client credentials grant doesn't involve a user
	scope := req.Scope
	if scope == "" {
		// Use client's default scopes
		var scopes []string
		if err := json.Unmarshal(client.Scopes, &scopes); err == nil && len(scopes) > 0 {
			scope = strings.Join(scopes, " ")
		}
	} else {
		// Validate requested scopes
		validatedScope, err := oauthService.ValidateScopes(client, scope)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":             "invalid_scope",
				"error_description": err.Error(),
			})
			return
		}
		scope = validatedScope
	}

	// Determine audience
	audience := req.Resource
	if audience == "" {
		audience = getServerURL(c)
	}

	// Issue access token (no user, no refresh token)
	accessToken, err := oauthService.IssueAccessToken(client.ClientID, nil, scope, audience, nil)
	if err != nil {
		s.logger.Errorf("Failed to issue access token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to issue token",
		})
		return
	}

	// Return token response
	c.JSON(http.StatusOK, OAuthTokenResponse{
		AccessToken: accessToken.AccessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(accessToken.ExpiresAt).Seconds()),
		Scope:       accessToken.Scope,
	})
}

// ===== OAuth Revocation Endpoint =====

// OAuthRevokeRequest represents the token revocation request
type OAuthRevokeRequest struct {
	Token         string `form:"token" binding:"required"`
	TokenTypeHint string `form:"token_type_hint"`
}

// OAuthRevokeHandler handles the OAuth token revocation endpoint
// POST /oauth/revoke
func (s *Server) OAuthRevokeHandler(c *gin.Context) {
	var req OAuthRevokeRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": err.Error(),
		})
		return
	}

	// Extract client credentials
	clientID, clientSecret := extractClientCredentials(c, "", "")
	if clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_client",
			"error_description": "Client authentication required",
		})
		return
	}

	oauthService := oauth.NewOAuthService(s.db)

	// Validate client
	if _, err := oauthService.ValidateClientCredentials(clientID, clientSecret); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Invalid client credentials",
		})
		return
	}

	// Attempt to revoke as access token or refresh token
	if err := oauthService.RevokeAccessToken(req.Token); err != nil {
		// If not an access token, try refresh token
		if err := oauthService.RevokeRefreshToken(req.Token); err != nil {
			s.logger.Debugf("Token revocation attempted for non-existent token")
		}
	}

	// Per RFC 7009, always return 200 OK even if token doesn't exist
	c.Status(http.StatusOK)
}

// ===== Helper Functions =====

// extractClientCredentials extracts client credentials from Authorization header or request body
func extractClientCredentials(c *gin.Context, bodyClientID, bodyClientSecret string) (string, string) {
	// Try Authorization header first (client_secret_basic)
	auth := c.GetHeader("Authorization")
	if strings.HasPrefix(auth, "Basic ") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "Basic "))
		if err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1]
			}
		}
	}

	// Fall back to request body (client_secret_post)
	return bodyClientID, bodyClientSecret
}

// getServerURL returns the base URL of the server
func getServerURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, c.Request.Host)
}

// ===== OIDC Discovery Endpoints =====

// OAuthAuthorizationServerMetadata represents OAuth 2.0 Authorization Server Metadata (RFC 8414)
type OAuthAuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint,omitempty"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	ScopesSupported                            []string `json:"scopes_supported,omitempty"`
	ServiceDocumentation                       string   `json:"service_documentation,omitempty"`
	ResourceIndicatorsSupported                bool     `json:"resource_indicators_supported,omitempty"`
}

// OAuthDiscoveryHandler handles OAuth 2.0 Authorization Server Metadata discovery
// GET /.well-known/oauth-authorization-server
func (s *Server) OAuthDiscoveryHandler(c *gin.Context) {
	baseURL := getServerURL(c)

	metadata := OAuthAuthorizationServerMetadata{
		Issuer:                baseURL,
		AuthorizationEndpoint: baseURL + "/oauth/authorize",
		TokenEndpoint:         baseURL + "/oauth/token",
		RevocationEndpoint:    baseURL + "/oauth/revoke",
		GrantTypesSupported: []string{
			"authorization_code",
			"refresh_token",
			"client_credentials",
		},
		ResponseTypesSupported: []string{"code"},
		CodeChallengeMethodsSupported: []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{
			"client_secret_basic",
			"client_secret_post",
			"none",
		},
		RevocationEndpointAuthMethodsSupported: []string{
			"client_secret_basic",
			"client_secret_post",
		},
		ServiceDocumentation:        "https://github.com/mcpjungle/mcpjungle",
		ResourceIndicatorsSupported: true,
	}

	c.JSON(http.StatusOK, metadata)
}

// ===== Dynamic Client Registration =====

// DynamicClientRegistrationRequest represents a dynamic client registration request (RFC 7591)
type DynamicClientRegistrationRequest struct {
	ClientName   string   `json:"client_name" binding:"required"`
	RedirectURIs []string `json:"redirect_uris" binding:"required"`
	GrantTypes   []string `json:"grant_types"`
	Scopes       []string `json:"scopes"`
}

// DynamicClientRegistrationResponse represents the response
type DynamicClientRegistrationResponse struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types"`
}

// OAuthRegisterHandler handles dynamic client registration
// POST /oauth/register
func (s *Server) OAuthRegisterHandler(c *gin.Context) {
	var req DynamicClientRegistrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": err.Error(),
		})
		return
	}

	// Validate redirect URIs
	if len(req.RedirectURIs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "At least one redirect_uri is required",
		})
		return
	}

	oauthService := oauth.NewOAuthService(s.db)

	// Register client as confidential by default
	client, err := oauthService.RegisterClient(req.ClientName, req.RedirectURIs, req.GrantTypes, req.Scopes, true)
	if err != nil {
		s.logger.Errorf("Failed to register OAuth client: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":             "server_error",
			"error_description": "Failed to register client",
		})
		return
	}

	// Unmarshal grant types for response
	var grantTypes []string
	_ = json.Unmarshal(client.GrantTypes, &grantTypes)

	var redirectURIs []string
	_ = json.Unmarshal(client.RedirectURIs, &redirectURIs)

	// Return client credentials (client_secret only returned once!)
	c.JSON(http.StatusCreated, DynamicClientRegistrationResponse{
		ClientID:     client.ClientID,
		ClientSecret: client.ClientSecret,
		ClientName:   client.ClientName,
		RedirectURIs: redirectURIs,
		GrantTypes:   grantTypes,
	})
}

// ===== Protected Resource Metadata Endpoint =====

// ProtectedResourceMetadata represents Protected Resource Metadata (RFC 9728)
type ProtectedResourceMetadata struct {
	Resource                        string   `json:"resource"`
	AuthorizationServers            []string `json:"authorization_servers"`
	BearerMethodsSupported          []string `json:"bearer_methods_supported"`
	ResourceDocumentation           string   `json:"resource_documentation,omitempty"`
	ResourceSigningAlgValuesSupported []string `json:"resource_signing_alg_values_supported,omitempty"`
}

// ResourceMetadataHandler handles Protected Resource Metadata discovery
// GET /.well-known/oauth-protected-resource
func (s *Server) ResourceMetadataHandler(c *gin.Context) {
	baseURL := getServerURL(c)

	metadata := ProtectedResourceMetadata{
		Resource:               baseURL,
		AuthorizationServers:   []string{baseURL},
		BearerMethodsSupported: []string{"header"},
		ResourceDocumentation:  "https://github.com/mcpjungle/mcpjungle",
	}

	c.JSON(http.StatusOK, metadata)
}

// OIDCConfigurationHandler handles OpenID Connect Discovery
// GET /.well-known/openid-configuration
func (s *Server) OIDCConfigurationHandler(c *gin.Context) {
	// For basic OIDC compatibility, redirect to OAuth discovery
	// Full OIDC support (UserInfo, ID tokens) can be added later
	baseURL := getServerURL(c)

	config := map[string]interface{}{
		"issuer":                                baseURL,
		"authorization_endpoint":                baseURL + "/oauth/authorize",
		"token_endpoint":                        baseURL + "/oauth/token",
		"revocation_endpoint":                   baseURL + "/oauth/revoke",
		"grant_types_supported":                 []string{"authorization_code", "refresh_token", "client_credentials"},
		"response_types_supported":              []string{"code"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"service_documentation":                 "https://github.com/mcpjungle/mcpjungle",
	}

	c.JSON(http.StatusOK, config)
}

// JSONSchemaHandler returns JSON schema for token introspection
func (s *Server) OAuthIntrospectHandler(c *gin.Context) {
	// Token introspection endpoint (RFC 7662)
	// Extract token from request
	token := c.PostForm("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":             "invalid_request",
			"error_description": "token parameter is required",
		})
		return
	}

	// Authenticate client
	clientID, clientSecret := extractClientCredentials(c, c.PostForm("client_id"), c.PostForm("client_secret"))
	if clientID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Client authentication required",
		})
		return
	}

	oauthService := oauth.NewOAuthService(s.db)
	if _, err := oauthService.ValidateClientCredentials(clientID, clientSecret); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":             "invalid_client",
			"error_description": "Invalid client credentials",
		})
		return
	}

	// Validate token
	accessToken, err := oauthService.ValidateAccessToken(token)
	if err != nil || accessToken == nil {
		// Token is not active
		c.JSON(http.StatusOK, gin.H{"active": false})
		return
	}

	// Return token info
	response := gin.H{
		"active":    true,
		"client_id": accessToken.ClientID,
		"scope":     accessToken.Scope,
		"exp":       accessToken.ExpiresAt.Unix(),
		"aud":       accessToken.Audience,
	}

	if accessToken.UserID != nil {
		response["sub"] = fmt.Sprintf("%d", *accessToken.UserID)
	}

	c.JSON(http.StatusOK, response)
}
