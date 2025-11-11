package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/telemetry"
	"github.com/mcpjungle/mcpjungle/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDBForSearch(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Migrate the schema
	err = db.AutoMigrate(&model.McpServer{}, &model.Tool{}, &model.Prompt{}, &model.AuditLog{})
	require.NoError(t, err)

	return db
}

func TestSearchMetaTool(t *testing.T) {
	db := setupTestDBForSearch(t)
	
	// Create MCP proxy servers
	mcpProxyServer := server.NewMCPServer("test-server", "1.0.0")
	sseMcpProxyServer := server.NewMCPServer("test-sse-server", "1.0.0")
	
	// Create MCP service
	mcpService, err := NewMCPService(db, mcpProxyServer, sseMcpProxyServer, telemetry.NewNoopCustomMetrics())
	require.NoError(t, err)

	// Create test servers and tools
	config1, _ := json.Marshal(model.StdioConfig{Command: "git-mcp"})
	server1 := &model.McpServer{
		Name:      "git",
		Transport: types.TransportStdio,
		Config:    datatypes.JSON(config1),
	}
	
	config2, _ := json.Marshal(model.StdioConfig{Command: "fs-mcp"})
	server2 := &model.McpServer{
		Name:      "filesystem",
		Transport: types.TransportStdio,
		Config:    datatypes.JSON(config2),
	}
	require.NoError(t, db.Create(server1).Error)
	require.NoError(t, db.Create(server2).Error)

	tools := []model.Tool{
		{
			ServerID:    server1.ID,
			Name:        "commit",
			Description: "Create a new git commit with a message",
			Enabled:     true,
		},
		{
			ServerID:    server1.ID,
			Name:        "branch",
			Description: "Create or switch git branches",
			Enabled:     true,
		},
		{
			ServerID:    server2.ID,
			Name:        "read_file",
			Description: "Read contents of a file from the filesystem",
			Enabled:     true,
		},
		{
			ServerID:    server2.ID,
			Name:        "write_file",
			Description: "Write content to a file in the filesystem",
			Enabled:     false,
		},
	}

	// Create tools individually to ensure proper handling of Enabled field
	for i := range tools {
		// Use map to ensure false values are properly set in SQLite
		if tools[i].Name == "write_file" {
			err := db.Model(&model.Tool{}).Create(map[string]interface{}{
				"server_id":    tools[i].ServerID,
				"name":         tools[i].Name,
				"description":  tools[i].Description,
				"enabled":      false,
				"input_schema": tools[i].InputSchema,
			}).Error
			require.NoError(t, err)
		} else {
			require.NoError(t, db.Create(&tools[i]).Error)
		}
	}

	// Test the search meta-tool handler
	ctx := context.Background()

	t.Run("Search for git tools", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{
			"query":       "git",
			"max_results": 10,
		}

		result, err := mcpService.searchMetaToolHandler(ctx, request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.NotEmpty(t, result.Content)
		
		// Check that the result mentions git tools
		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "Expected TextContent")
		assert.Contains(t, textContent.Text, "git")
		assert.Contains(t, textContent.Text, "Found")
	})

	t.Run("Search with server filter", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{
			"query":        "file",
			"server_names": []interface{}{"filesystem"},
		}

		result, err := mcpService.searchMetaToolHandler(ctx, request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		assert.NotEmpty(t, result.Content)
		
		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "Expected TextContent")
		assert.Contains(t, textContent.Text, "filesystem")
	})

	t.Run("Search with only_enabled filter", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{
			"query":        "write",
			"only_enabled": true,
		}

		result, err := mcpService.searchMetaToolHandler(ctx, request)
		require.NoError(t, err)
		assert.False(t, result.IsError)
		
		// write_file is disabled, so should not be found
		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "Expected TextContent")
		assert.Contains(t, textContent.Text, "No tools found")
	})

	t.Run("Missing query parameter", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{}

		_, err := mcpService.searchMetaToolHandler(ctx, request)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query")
	})

	t.Run("Invalid max_results type", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{
			"query":       "test",
			"max_results": "invalid",
		}

		result, err := mcpService.searchMetaToolHandler(ctx, request)
		// Since we're using GetInt with a default, invalid types are handled gracefully
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})

	t.Run("Invalid server_names type", func(t *testing.T) {
		request := mcp.CallToolRequest{}
		request.Params.Name = SearchMetaToolName
		request.Params.Arguments = map[string]any{
			"query":        "test",
			"server_names": "invalid",
		}

		result, err := mcpService.searchMetaToolHandler(ctx, request)
		// Since we're using GetStringSlice with a default, invalid types are handled gracefully
		require.NoError(t, err)
		assert.False(t, result.IsError)
	})
}

func TestInitSearchMetaTool(t *testing.T) {
	db := setupTestDBForSearch(t)
	
	// Create MCP proxy servers
	mcpProxyServer := server.NewMCPServer("test-server", "1.0.0")
	sseMcpProxyServer := server.NewMCPServer("test-sse-server", "1.0.0")
	
	// Create MCP service
	mcpService, err := NewMCPService(db, mcpProxyServer, sseMcpProxyServer, telemetry.NewNoopCustomMetrics())
	require.NoError(t, err)

	// Check that the search meta-tool was added
	tool, exists := mcpService.GetToolInstance(SearchMetaToolName)
	assert.True(t, exists)
	assert.Equal(t, SearchMetaToolName, tool.GetName())
	assert.Contains(t, tool.Description, "Search for tools")
}
