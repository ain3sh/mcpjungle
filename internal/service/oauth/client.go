package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/util"
	"gorm.io/gorm"
)

// OAuthClientService manages OAuth client functionality for connecting to upstream MCP servers
type OAuthClientService struct {
	db         *gorm.DB
	httpClient *http.Client
}

// NewOAuthClientService creates a new OAuth client service
func NewOAuthClientService(db *gorm.DB) *OAuthClientService {
	return &OAuthClientService{
		db: db,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Limit redirects to prevent infinite loops
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// ===== OAuth Discovery =====

// ProtectedResourceMetadata represents RFC 9728 Protected Resource Metadata
type ProtectedResourceMetadata struct {
	Resource                        string   `json:"resource"`
	AuthorizationServers            []string `json:"authorization_servers"`
	BearerMethodsSupported          []string `json:"bearer_methods_supported,omitempty"`
	ResourceDocumentation           string   `json:"resource_documentation,omitempty"`
	ResourceSigningAlgValuesSupported []string `json:"resource_signing_alg_values_supported,omitempty"`
}

// AuthorizationServerMetadata represents RFC 8414 OAuth 2.0 Authorization Server Metadata
type AuthorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	RevocationEndpoint                         string   `json:"revocation_endpoint,omitempty"`
	RegistrationEndpoint                       string   `json:"registration_endpoint,omitempty"`
	GrantTypesSupported                        []string `json:"grant_types_supported,omitempty"`
	ResponseTypesSupported                     []string `json:"response_types_supported,omitempty"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported,omitempty"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported,omitempty"`
	RevocationEndpointAuthMethodsSupported     []string `json:"revocation_endpoint_auth_methods_supported,omitempty"`
	ScopesSupported                            []string `json:"scopes_supported,omitempty"`
}

// DiscoverProtectedResourceMetadata discovers OAuth configuration from an MCP server
// Per RFC 9728, this is retrieved from /.well-known/oauth-protected-resource
func (s *OAuthClientService) DiscoverProtectedResourceMetadata(serverURL string) (*ProtectedResourceMetadata, error) {
	// Normalize server URL
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	// Construct well-known URL
	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-protected-resource",
		parsedURL.Scheme, parsedURL.Host)

	// Fetch metadata
	resp, err := s.httpClient.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch protected resource metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var metadata ProtectedResourceMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	return &metadata, nil
}

// DiscoverAuthorizationServerMetadata discovers authorization server configuration
// Per RFC 8414, this is retrieved from /.well-known/oauth-authorization-server
func (s *OAuthClientService) DiscoverAuthorizationServerMetadata(authServerURL string) (*AuthorizationServerMetadata, error) {
	// Parse authorization server URL
	parsedURL, err := url.Parse(authServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid authorization server URL: %w", err)
	}

	// Construct well-known URL
	wellKnownURL := fmt.Sprintf("%s://%s/.well-known/oauth-authorization-server",
		parsedURL.Scheme, parsedURL.Host)

	// Fetch metadata
	resp, err := s.httpClient.Get(wellKnownURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch authorization server metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}

	var metadata AuthorizationServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode metadata: %w", err)
	}

	return &metadata, nil
}

// ===== Dynamic Client Registration (RFC 7591) =====

// DynamicClientRegistrationRequest represents a client registration request
type DynamicClientRegistrationRequest struct {
	ClientName   string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	GrantTypes   []string `json:"grant_types,omitempty"`
	Scope        string   `json:"scope,omitempty"`
}

// DynamicClientRegistrationResponse represents a client registration response
type DynamicClientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// RegisterDynamicClient registers a new OAuth client with the authorization server
// Per RFC 7591, this allows automatic client registration without manual setup
func (s *OAuthClientService) RegisterDynamicClient(ctx context.Context, registrationEndpoint, clientName string, redirectURIs []string) (*DynamicClientRegistrationResponse, error) {
	if registrationEndpoint == "" {
		return nil, fmt.Errorf("registration endpoint not provided by authorization server")
	}

	request := DynamicClientRegistrationRequest{
		ClientName:   clientName,
		RedirectURIs: redirectURIs,
		GrantTypes:   []string{"authorization_code", "refresh_token"},
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to encode registration request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", registrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	var response DynamicClientRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode registration response: %w", err)
	}

	return &response, nil
}

// ===== Authorization Flow =====

// GenerateAuthorizationURL creates an authorization URL with PKCE
// Per MCP spec, PKCE with S256 is REQUIRED
func (s *OAuthClientService) GenerateAuthorizationURL(authEndpoint, clientID, redirectURI, resource, state string) (authURL string, codeVerifier string, err error) {
	// Generate PKCE parameters (REQUIRED per MCP spec)
	codeVerifier, err = util.GeneratePKCEVerifier()
	if err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}

	codeChallenge := util.GeneratePKCEChallenge(codeVerifier)

	// Build authorization URL
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("code_challenge", codeChallenge)
	params.Set("code_challenge_method", "S256")
	params.Set("resource", resource) // RFC 8707 - REQUIRED per MCP spec
	if state != "" {
		params.Set("state", state)
	}

	authURL = authEndpoint + "?" + params.Encode()
	return authURL, codeVerifier, nil
}

// ===== Token Exchange =====

// TokenResponse represents an OAuth token response
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// ExchangeAuthorizationCode exchanges an authorization code for tokens
// Per MCP spec, must include code_verifier (PKCE) and resource parameter
func (s *OAuthClientService) ExchangeAuthorizationCode(ctx context.Context, tokenEndpoint, clientID, clientSecret, code, codeVerifier, redirectURI, resource string) (*TokenResponse, error) {
	// Build token request
	data := url.Values{}
	data.Set("grant_type", "authorization_code")
	data.Set("code", code)
	data.Set("redirect_uri", redirectURI)
	data.Set("code_verifier", codeVerifier) // PKCE verifier (REQUIRED)
	data.Set("resource", resource)          // RFC 8707 (REQUIRED per MCP spec)
	data.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Add client authentication if client has a secret
	if clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// RefreshAccessToken refreshes an access token using a refresh token
// Per OAuth 2.1, public clients MUST rotate refresh tokens
func (s *OAuthClientService) RefreshAccessToken(ctx context.Context, tokenEndpoint, clientID, clientSecret, refreshToken, resource string) (*TokenResponse, error) {
	// Build refresh request
	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", refreshToken)
	data.Set("resource", resource) // RFC 8707 (REQUIRED per MCP spec)
	data.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	// Add client authentication if client has a secret
	if clientSecret != "" {
		req.SetBasicAuth(clientID, clientSecret)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}

	return &tokenResp, nil
}

// ===== Session Management =====

// GetOrRefreshUpstreamToken gets a valid access token for an upstream server, refreshing if needed
func (s *OAuthClientService) GetOrRefreshUpstreamToken(ctx context.Context, serverName string) (string, error) {
	// Get session from database
	var session model.OAuthUpstreamSession
	if err := s.db.Where("mcp_server_name = ?", serverName).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", fmt.Errorf("no OAuth session found for server %s", serverName)
		}
		return "", fmt.Errorf("failed to get OAuth session: %w", err)
	}

	// Check if token needs refresh
	if !session.NeedsRefresh() {
		return session.AccessToken, nil
	}

	// Refresh the token
	if session.RefreshToken == "" {
		return "", fmt.Errorf("access token expired and no refresh token available")
	}

	// Use the stored resource URI (canonical URI of the MCP server per RFC 8707)
	tokenResp, err := s.RefreshAccessToken(ctx, session.TokenEndpoint, session.ClientID, session.ClientSecret, session.RefreshToken, session.ResourceURI)
	if err != nil {
		return "", fmt.Errorf("failed to refresh access token: %w", err)
	}

	// Update session with new tokens
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	session.AccessToken = tokenResp.AccessToken
	session.ExpiresAt = &expiresAt
	session.TokenType = tokenResp.TokenType

	// Update refresh token if rotated (per OAuth 2.1 for public clients)
	if tokenResp.RefreshToken != "" {
		session.RefreshToken = tokenResp.RefreshToken
	}

	if err := s.db.Save(&session).Error; err != nil {
		return "", fmt.Errorf("failed to update OAuth session: %w", err)
	}

	return session.AccessToken, nil
}

// StoreUpstreamSession stores OAuth session information for an upstream server
func (s *OAuthClientService) StoreUpstreamSession(session *model.OAuthUpstreamSession) error {
	// Check if session already exists
	var existing model.OAuthUpstreamSession
	err := s.db.Where("mcp_server_name = ?", session.McpServerName).First(&existing).Error

	if err == gorm.ErrRecordNotFound {
		// Create new session
		return s.db.Create(session).Error
	} else if err != nil {
		return fmt.Errorf("failed to check existing session: %w", err)
	}

	// Update existing session
	session.ID = existing.ID
	return s.db.Save(session).Error
}

// DeleteUpstreamSession removes an OAuth session for an upstream server
func (s *OAuthClientService) DeleteUpstreamSession(serverName string) error {
	return s.db.Where("mcp_server_name = ?", serverName).Delete(&model.OAuthUpstreamSession{}).Error
}
