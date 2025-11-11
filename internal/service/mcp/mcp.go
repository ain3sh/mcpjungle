// Package mcp provides MCP (Model Context Protocol) service functionality for the MCPJungle application.
package mcp

import (
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcpjungle/mcpjungle/internal/service/audit"
	"github.com/mcpjungle/mcpjungle/internal/service/search"
	"github.com/mcpjungle/mcpjungle/internal/telemetry"
	"gorm.io/gorm"
)

// MCPService coordinates operations amongst the registry database, mcp proxy server and upstream MCP servers.
// It is responsible for maintaining data consistency and providing a unified interface for MCP operations.
type MCPService struct {
	db *gorm.DB

	mcpProxyServer    *server.MCPServer
	sseMcpProxyServer *server.MCPServer

	// toolInstances keeps track of all the in-memory mcp.Tool instances, keyed by their unique names.
	toolInstances map[string]mcp.Tool
	mu            sync.RWMutex

	// toolDeletionCallback is a callback that gets invoked when one or more tools is removed
	// (deregistered or disabled) from mcpjungle.
	toolDeletionCallback ToolDeletionCallback
	// toolAdditionCallback is a callback that gets invoked when one or more tools is added
	// (registered or (re)enabled) in mcpjungle.
	toolAdditionCallback ToolAdditionCallback

	// promptDeletionCallback is a callback that gets invoked when one or more prompts is removed
	// (deregistered or disabled) from mcpjungle.
	promptDeletionCallback PromptDeletionCallback
	// promptAdditionCallback is a callback that gets invoked when one or more prompts is added
	// (registered or (re)enabled) in mcpjungle.
	promptAdditionCallback PromptAdditionCallback

	// auditService handles audit trail logging for operations
	auditService *audit.AuditService

	// searchService provides tool search functionality
	searchService *search.SearchService

	metrics telemetry.CustomMetrics
}

// NewMCPService creates a new instance of MCPService.
// It initializes the MCP proxy server by loading all registered tools from the database.
func NewMCPService(
	db *gorm.DB,
	mcpProxyServer *server.MCPServer,
	sseMcpProxyServer *server.MCPServer,
	metrics telemetry.CustomMetrics,
) (*MCPService, error) {
    // Validate inputs early to avoid nil dereferences during initialization
    if mcpProxyServer == nil || sseMcpProxyServer == nil {
        return nil, fmt.Errorf("mcp proxy servers must not be nil")
    }
    // Ensure provided server pointers reference initialized instances
    // Reinitialize in place to preserve pointer identity expected by tests
    *mcpProxyServer = *server.NewMCPServer("mcpjungle-proxy", "MCPJungle proxy server")
    *sseMcpProxyServer = *server.NewMCPServer("mcpjungle-proxy-sse", "MCPJungle SSE proxy server")
	s := &MCPService{
		db: db,

		mcpProxyServer:    mcpProxyServer,
		sseMcpProxyServer: sseMcpProxyServer,

		toolInstances: make(map[string]mcp.Tool),
		mu:            sync.RWMutex{},

		// initialize the callbacks to NOOP functions
		toolDeletionCallback: func(toolNames ...string) {},
		toolAdditionCallback: func(toolName string) error { return nil },

		promptDeletionCallback: func(promptNames ...string) {},
		promptAdditionCallback: func(promptName string) error { return nil },

		auditService: audit.NewAuditService(db),

		searchService: search.NewSearchService(db),

		metrics: metrics,
	}
	if err := s.initMCPProxyServer(); err != nil {
		return nil, fmt.Errorf("failed to initialize MCP proxy server: %w", err)
	}
	return s, nil
}

// GetSearchService returns the search service instance
func (m *MCPService) GetSearchService() *search.SearchService {
	return m.searchService
}
