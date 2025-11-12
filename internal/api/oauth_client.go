package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/service/oauth"
	"github.com/mcpjungle/mcpjungle/internal/util"
)

// OAuthClientInitiateRequest represents a request to initiate OAuth with an upstream server
type OAuthClientInitiateRequest struct {
	ServerName  string   `json:"server_name" binding:"required"`
	ServerURL   string   `json:"server_url" binding:"required"`
	ClientName  string   `json:"client_name" binding:"required"`
	RedirectURI string   `json:"redirect_uri" binding:"required"`
	Scopes      []string `json:"scopes,omitempty"`
}

// OAuthClientInitiateResponse represents the response containing authorization URL
type OAuthClientInitiateResponse struct {
	AuthorizationURL string `json:"authorization_url"`
	State            string `json:"state"`
}

// OAuthClientCallbackRequest represents OAuth callback parameters
type OAuthClientCallbackRequest struct {
	ServerName string `form:"server_name" binding:"required"`
	Code       string `form:"code" binding:"required"`
	State      string `form:"state" binding:"required"`
}

// OAuthClientCallbackResponse represents the OAuth callback response
type OAuthClientCallbackResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// OAuthClientInitiateHandler initiates OAuth flow with an upstream MCP server
// POST /api/v0/oauth/upstream/initiate
func (s *Server) OAuthClientInitiateHandler(c *gin.Context) {
	var req OAuthClientInitiateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	ctx := c.Request.Context()
	oauthClientService := oauth.NewOAuthClientService(s.db)

	// Step 1: Discover OAuth endpoints from the upstream server
	s.logger.Infof("Discovering OAuth metadata for server %s at %s", req.ServerName, req.ServerURL)
	resourceMetadata, err := oauthClientService.DiscoverProtectedResourceMetadata(req.ServerURL)
	if err != nil {
		s.logger.Errorf("Failed to discover resource metadata: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to discover OAuth endpoints: %v", err)})
		return
	}

	if len(resourceMetadata.AuthorizationServers) == 0 {
		c.JSON(http.StatusBadGateway, gin.H{"error": "no authorization servers found in resource metadata"})
		return
	}

	// Use the first authorization server
	authServerURL := resourceMetadata.AuthorizationServers[0]
	s.logger.Infof("Discovered authorization server: %s", authServerURL)

	// Step 2: Get authorization server metadata
	authServerMetadata, err := oauthClientService.DiscoverAuthorizationServerMetadata(authServerURL)
	if err != nil {
		s.logger.Errorf("Failed to discover authorization server metadata: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to discover authorization server metadata: %v", err)})
		return
	}

	// Step 3: Register as OAuth client (if registration endpoint is available)
	var clientID, clientSecret string
	if authServerMetadata.RegistrationEndpoint != "" {
		s.logger.Infof("Registering OAuth client with registration endpoint: %s", authServerMetadata.RegistrationEndpoint)
		registrationResp, err := oauthClientService.RegisterDynamicClient(ctx, authServerMetadata.RegistrationEndpoint, req.ClientName, []string{req.RedirectURI})
		if err != nil {
			s.logger.Errorf("Failed to register OAuth client: %v", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to register OAuth client: %v", err)})
			return
		}
		clientID = registrationResp.ClientID
		clientSecret = registrationResp.ClientSecret
		s.logger.Infof("Successfully registered OAuth client with ID: %s", clientID)
	} else {
		// If no registration endpoint, client credentials should be pre-configured
		c.JSON(http.StatusBadRequest, gin.H{"error": "server does not support dynamic client registration - manual client configuration required"})
		return
	}

	// Step 4: Generate authorization URL with PKCE
	state, err := util.GenerateOAuthToken() // Use secure random state
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate state parameter"})
		return
	}

	authURL, codeVerifier, err := oauthClientService.GenerateAuthorizationURL(
		authServerMetadata.AuthorizationEndpoint,
		clientID,
		req.RedirectURI,
		resourceMetadata.Resource, // RFC 8707 resource parameter
		state,
	)
	if err != nil {
		s.logger.Errorf("Failed to generate authorization URL: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate authorization URL: %v", err)})
		return
	}

	// Step 5: Store OAuth session with PKCE verifier (temporary, waiting for callback)
	session := &model.OAuthUpstreamSession{
		McpServerName:         req.ServerName,
		ClientID:              clientID,
		ClientSecret:          clientSecret,
		AuthorizationEndpoint: authServerMetadata.AuthorizationEndpoint,
		TokenEndpoint:         authServerMetadata.TokenEndpoint,
		ResourceURI:           resourceMetadata.Resource,
		CodeVerifier:          codeVerifier,
		RedirectURI:           req.RedirectURI,
		Scope:                 joinScopes(req.Scopes),
	}

	if err := oauthClientService.StoreUpstreamSession(session); err != nil {
		s.logger.Errorf("Failed to store OAuth session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to store OAuth session: %v", err)})
		return
	}

	s.logger.Infof("OAuth flow initiated for server %s, authorization URL generated", req.ServerName)

	c.JSON(http.StatusOK, OAuthClientInitiateResponse{
		AuthorizationURL: authURL,
		State:            state,
	})
}

// OAuthClientCallbackHandler handles OAuth callback from upstream authorization server
// GET /api/v0/oauth/upstream/callback
func (s *Server) OAuthClientCallbackHandler(c *gin.Context) {
	var req OAuthClientCallbackRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid callback parameters: %v", err)})
		return
	}

	ctx := c.Request.Context()
	oauthClientService := oauth.NewOAuthClientService(s.db)

	// Get the stored session to retrieve PKCE verifier and endpoints
	var session model.OAuthUpstreamSession
	if err := s.db.Where("mcp_server_name = ?", req.ServerName).First(&session).Error; err != nil {
		s.logger.Errorf("Failed to get OAuth session for server %s: %v", req.ServerName, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "no OAuth session found for this server"})
		return
	}

	// Exchange authorization code for tokens
	s.logger.Infof("Exchanging authorization code for tokens (server: %s)", req.ServerName)
	tokenResp, err := oauthClientService.ExchangeAuthorizationCode(
		ctx,
		session.TokenEndpoint,
		session.ClientID,
		session.ClientSecret,
		req.Code,
		session.CodeVerifier,
		session.RedirectURI,
		session.ResourceURI,
	)
	if err != nil {
		s.logger.Errorf("Failed to exchange authorization code: %v", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to exchange authorization code: %v", err)})
		return
	}

	// Update session with tokens and clear code verifier
	session.AccessToken = tokenResp.AccessToken
	session.RefreshToken = tokenResp.RefreshToken
	session.TokenType = tokenResp.TokenType
	session.Scope = tokenResp.Scope
	session.CodeVerifier = "" // Clear after use

	// Calculate expiration time
	if tokenResp.ExpiresIn > 0 {
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		session.ExpiresAt = &expiresAt
	}

	if err := oauthClientService.StoreUpstreamSession(&session); err != nil {
		s.logger.Errorf("Failed to update OAuth session: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to store OAuth tokens: %v", err)})
		return
	}

	s.logger.Infof("Successfully completed OAuth flow for server %s", req.ServerName)

	c.JSON(http.StatusOK, OAuthClientCallbackResponse{
		Success: true,
		Message: fmt.Sprintf("Successfully authenticated with %s", req.ServerName),
	})
}

// OAuthClientStatusHandler returns OAuth session status for an upstream server
// GET /api/v0/oauth/upstream/status/:server_name
func (s *Server) OAuthClientStatusHandler(c *gin.Context) {
	serverName := c.Param("server_name")

	var session model.OAuthUpstreamSession
	if err := s.db.Where("mcp_server_name = ?", serverName).First(&session).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no OAuth session found for this server"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"server_name":  session.McpServerName,
		"client_id":    session.ClientID,
		"scope":        session.Scope,
		"expires_at":   session.ExpiresAt,
		"has_refresh":  session.RefreshToken != "",
		"token_type":   session.TokenType,
		"is_expired":   session.IsAccessTokenExpired(),
		"needs_refresh": session.NeedsRefresh(),
	})
}

// OAuthClientRevokeHandler revokes OAuth session for an upstream server
// DELETE /api/v0/oauth/upstream/:server_name
func (s *Server) OAuthClientRevokeHandler(c *gin.Context) {
	serverName := c.Param("server_name")

	oauthClientService := oauth.NewOAuthClientService(s.db)
	if err := oauthClientService.DeleteUpstreamSession(serverName); err != nil {
		s.logger.Errorf("Failed to revoke OAuth session for server %s: %v", serverName, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to revoke OAuth session"})
		return
	}

	s.logger.Infof("Successfully revoked OAuth session for server %s", serverName)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": fmt.Sprintf("OAuth session revoked for %s", serverName),
	})
}

// Helper function to join scopes
func joinScopes(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	result := ""
	for i, scope := range scopes {
		if i > 0 {
			result += " "
		}
		result += scope
	}
	return result
}
