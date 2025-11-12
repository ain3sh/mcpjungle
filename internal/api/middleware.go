package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/service/oauth"
	"github.com/mcpjungle/mcpjungle/internal/util"
	"github.com/mcpjungle/mcpjungle/pkg/types"
)

// requireInitialized is middleware to reject requests to certain routes if the server is not initialized
func (s *Server) requireInitialized() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := s.configService.GetConfig()
		if err != nil || !cfg.Initialized {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "server is not initialized"})
			return
		}
		// propagate the server mode in context for other middleware/handlers to use
		c.Set("mode", cfg.Mode)
		c.Next()
	}
}

// verifyUserAuthForAPIAccess is middleware that checks for a valid user token if the server is in enterprise mode.
// this middleware doesn't care about the role of the user, it just verifies that they're authenticated.
// Supports both traditional bearer tokens and OAuth access tokens.
func (s *Server) verifyUserAuthForAPIAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		mode, exists := c.Get("mode")
		if !exists {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "server mode not found in context"})
			return
		}
		m, ok := mode.(model.ServerMode)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid server mode in context"})
			return
		}
		if m == model.ModeDev {
			// no auth is required in case of dev mode
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing access token"})
			return
		}

		// Try OAuth token first, then fall back to traditional user token
		oauthService := oauth.NewOAuthService(s.db)
		oauthToken, err := oauthService.ValidateAccessToken(token)
		if err == nil && oauthToken != nil {
			// Valid OAuth token
			if oauthToken.UserID == nil {
				// Client credentials grant (no user)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "user access required"})
				return
			}

			// Get user from OAuth token
			var authenticatedUser model.User
			if err := s.db.First(&authenticatedUser, *oauthToken.UserID).Error; err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user not found"})
				return
			}

			// Store user and OAuth token in context
			c.Set("user", &authenticatedUser)
			c.Set("user_id", authenticatedUser.ID)
			c.Set("oauth_token", oauthToken)

			// Set audit context
			auditCtx := &util.AuditContext{
				ActorType: model.AuditActorUser,
				ActorID:   authenticatedUser.Username,
				IPAddress: c.ClientIP(),
				UserAgent: c.GetHeader("User-Agent"),
			}
			ctx := util.SetAuditContext(c.Request.Context(), auditCtx)
			c.Request = c.Request.WithContext(ctx)

			c.Next()
			return
		}

		// Fall back to traditional user token
		authenticatedUser, err := s.userService.GetUserByAccessToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid access token: " + err.Error()})
			return
		}

		// Store user in context for potential role checks in subsequent handlers
		c.Set("user", authenticatedUser)
		c.Set("user_id", authenticatedUser.ID)

		// Set audit context for tracking operations
		auditCtx := &util.AuditContext{
			ActorType: model.AuditActorUser,
			ActorID:   authenticatedUser.Username,
			IPAddress: c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
		}
		ctx := util.SetAuditContext(c.Request.Context(), auditCtx)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}

// requireAdminUser is middleware that ensures the authenticated user has an admin role when in enterprise mode.
// It assumes that verifyUserAuthForAPIAccess middleware has already run and set the user in context.
func (s *Server) requireAdminUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		mode, exists := c.Get("mode")
		if !exists {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "server mode not found in context"})
			return
		}
		m, ok := mode.(model.ServerMode)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid server mode in context"})
			return
		}
		if m == model.ModeDev {
			// no admin check is required in dev mode
			c.Next()
			return
		}

		authenticatedUser, exists := c.Get("user")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "user is not authenticated"})
			return
		}

		u, ok := authenticatedUser.(*model.User)
		if ok && u.Role == types.UserRoleAdmin {
			c.Next()
			return
		}

		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "user is not authorized to perform this action"})
	}
}

// requireServerMode is middleware that checks if the server is in a specific mode.
// If not, the request is rejected with a 403 Forbidden status.
// This is useful for routes that should only be accessible in certain modes (e.g., enterprise-only features).
// NOTE: ModeProd is supported for backwards compatibility, it is equivalent to ModeEnterprise.
func (s *Server) requireServerMode(m model.ServerMode) gin.HandlerFunc {
	return func(c *gin.Context) {
		mode, exists := c.Get("mode")
		if !exists {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "server mode not found in context"})
			return
		}
		currentMode, ok := mode.(model.ServerMode)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid server mode in context"})
			return
		}

		if currentMode == m {
			// current mode matches the required mode, allow access
			c.Next()
			return
		}
		if model.IsEnterpriseMode(currentMode) && model.IsEnterpriseMode(m) {
			// both current and required modes are enterprise modes, allow access
			c.Next()
			return
		}
		// current mode does not match the required mode, reject the request
		c.AbortWithStatusJSON(
			http.StatusForbidden,
			gin.H{"error": fmt.Sprintf("this request is only allowed in %s mode", m)},
		)
	}
}

// checkAuthForMcpProxyAccess is middleware for MCP proxy that checks for a valid MCP client token
// if the server is in enterprise mode.
// In development mode, mcp clients do not require auth to access the MCP proxy.
// Supports both traditional bearer tokens and OAuth access tokens.
func (s *Server) checkAuthForMcpProxyAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		mode, exists := c.Get("mode")
		if !exists {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "server mode not found in context"})
			return
		}
		m, ok := mode.(model.ServerMode)
		if !ok {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "invalid server mode in context"})
			return
		}

		// the gin context doesn't get passed down to the MCP proxy server, so we need to
		// set values in the underlying request's context to be able to access them from proxy.
		ctx := context.WithValue(c.Request.Context(), "mode", m)
		c.Request = c.Request.WithContext(ctx)

		if m == model.ModeDev {
			// no auth is required in case of dev mode
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing MCP client access token"})
			return
		}

		// Try OAuth token first
		oauthService := oauth.NewOAuthService(s.db)
		oauthToken, err := oauthService.ValidateAccessToken(token)
		if err == nil && oauthToken != nil {
			// Valid OAuth token - get the OAuth client
			oauthClient, err := oauthService.GetClient(oauthToken.ClientID)
			if err != nil || oauthClient == nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "OAuth client not found"})
				return
			}

			// Create a pseudo MCP client for context compatibility
			// Map OAuth scopes to server access
			var scopes []string
			if oauthToken.Scope != "" {
				scopes = strings.Split(oauthToken.Scope, " ")
			}

			pseudoClient := &model.McpClient{
				Name:        oauthClient.ClientName,
				Description: "OAuth client: " + oauthClient.ClientID,
				AccessToken: token,
			}

			// Inject the OAuth-authenticated client in context
			ctx = context.WithValue(ctx, "client", pseudoClient)
			ctx = context.WithValue(ctx, "oauth_scopes", scopes)
			ctx = context.WithValue(ctx, "oauth_token", oauthToken)

			// Inject tool group service for tool-level ACL checking
			ctx = context.WithValue(ctx, "toolGroupChecker", s.toolGroupService)

			// Set audit context for tracking operations by OAuth clients
			auditCtx := &util.AuditContext{
				ActorType: model.AuditActorMcpClient,
				ActorID:   oauthClient.ClientName,
				IPAddress: c.ClientIP(),
				UserAgent: c.GetHeader("User-Agent"),
			}
			ctx = util.SetAuditContext(ctx, auditCtx)
			c.Request = c.Request.WithContext(ctx)

			c.Next()
			return
		}

		// Fall back to traditional MCP client token
		client, err := s.mcpClientService.GetClientByToken(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid MCP client token"})
			return
		}

		// inject the authenticated MCP client in context for the proxy to use
		ctx = context.WithValue(ctx, "client", client)

		// Inject tool group service for tool-level ACL checking
		// The tool group service implements both ToolGroupToolChecker and ToolGroupResolver interfaces
		ctx = context.WithValue(ctx, "toolGroupChecker", s.toolGroupService)

		// Set audit context for tracking operations by MCP clients
		auditCtx := &util.AuditContext{
			ActorType: model.AuditActorMcpClient,
			ActorID:   client.Name,
			IPAddress: c.ClientIP(),
			UserAgent: c.GetHeader("User-Agent"),
		}
		ctx = util.SetAuditContext(ctx, auditCtx)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
