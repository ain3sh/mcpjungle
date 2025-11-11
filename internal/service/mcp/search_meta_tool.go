package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcpjungle/mcpjungle/internal/service/search"
)

const (
	// SearchMetaToolName is the canonical name for the search meta-tool
	SearchMetaToolName = "mcpjungle__search_tools"
)

// initSearchMetaTool creates and registers the search meta-tool in the MCP proxy server
func (m *MCPService) initSearchMetaTool() error {
	// Create the search tool schema
	inputSchema := mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query to find tools. Can be keywords from tool names or descriptions.",
			},
			"max_results": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 20)",
				"minimum":     1,
				"maximum":     100,
			},
			"server_names": map[string]interface{}{
				"type":        "array",
				"description": "Optional list of server names to limit search to specific servers",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
			"only_enabled": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, only return enabled tools (default: false)",
			},
		},
		Required: []string{"query"},
	}

	// Create the MCP tool object
	searchTool := mcp.Tool{
		Name:        SearchMetaToolName,
		Description: "Search for tools across all registered MCP servers in MCPJungle. Returns matching tools with their descriptions and metadata.",
		InputSchema: inputSchema,
	}

	// Register the tool with both proxy servers
	m.mcpProxyServer.AddTool(searchTool, m.searchMetaToolHandler)
	m.sseMcpProxyServer.AddTool(searchTool, m.searchMetaToolHandler)

	// Add to tool instances tracker
	m.addToolInstance(searchTool)

	return nil
}

// searchMetaToolHandler handles calls to the search meta-tool
func (m *MCPService) searchMetaToolHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Parse the search options from the request
	var opts search.SearchOptions
	
	// Extract query (required)
	query, err := request.RequireString("query")
	if err != nil {
		return nil, fmt.Errorf("'query' parameter is required: %w", err)
	}
	if query == "" {
		return nil, fmt.Errorf("'query' parameter must be a non-empty string")
	}
	opts.Query = query

	// Extract max_results (optional)
	opts.MaxResults = request.GetInt("max_results", 20)

	// Extract server_names (optional)
	opts.ServerNames = request.GetStringSlice("server_names", nil)

	// Extract only_enabled (optional)
	opts.OnlyEnabled = request.GetBool("only_enabled", false)

	// Perform the search
	results, err := m.searchService.SearchTools(opts)
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Search failed: %v", err)),
			},
		}, nil
	}

	// Format the results
	if len(results) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("No tools found matching query: %s", opts.Query)),
			},
		}, nil
	}

	// Convert results to JSON for structured output
	resultsJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Failed to format results: %v", err)),
			},
		}, nil
	}

	// Create a summary text
	summaryText := fmt.Sprintf("Found %d tools matching '%s':\n\n", len(results), opts.Query)
	for i, result := range results {
		status := "enabled"
		if !result.Enabled {
			status = "disabled"
		}
		summaryText += fmt.Sprintf("%d. %s (%s) - %s\n   Score: %.2f, Status: %s\n\n", 
			i+1, result.ToolName, result.ServerName, result.Description, result.Score, status)
		
		// Limit summary to first 10 results for readability
		if i >= 9 && len(results) > 10 {
			summaryText += fmt.Sprintf("... and %d more results\n", len(results)-10)
			break
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(summaryText),
			mcp.NewTextContent(fmt.Sprintf("Full results (JSON):\n%s", string(resultsJSON))),
		},
	}, nil
}
