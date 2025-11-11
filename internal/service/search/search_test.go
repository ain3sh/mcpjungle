package search

import (
	"encoding/json"
	"testing"

	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Migrate the schema
	err = db.AutoMigrate(&model.McpServer{}, &model.Tool{})
	require.NoError(t, err)

	return db
}

func TestSearchService_SearchTools(t *testing.T) {
	db := setupTestDB(t)
	service := NewSearchService(db)

	// Create test servers with proper config
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

	// Create test tools
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
			ServerID:    server1.ID,
			Name:        "status",
			Description: "Show git repository status",
			Enabled:     false, // Explicitly disabled
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
			Enabled:     true,
		},
	}

	// Create tools individually to ensure proper handling of Enabled field
	for i := range tools {
		// Use map to ensure false values are properly set in SQLite
		if tools[i].Name == "status" {
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

	t.Run("Search by keyword in name", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:      "commit",
			MaxResults: 10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "git__commit", results[0].ToolName)
		assert.Equal(t, "git", results[0].ServerName)
	})

	t.Run("Search by keyword in description", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:      "file",
			MaxResults: 10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 2)
		// Both filesystem tools should be found
		for _, result := range results {
			assert.Equal(t, "filesystem", result.ServerName)
			assert.Contains(t, []string{"filesystem__read_file", "filesystem__write_file"}, result.ToolName)
		}
	})

	t.Run("Search with multiple terms", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:      "git branch",
			MaxResults: 10,
		})
		require.NoError(t, err)
		// Should find git tools, with "branch" having highest score
		assert.Greater(t, len(results), 0)
		// The branch tool should rank highest due to exact name match
		assert.Equal(t, "git__branch", results[0].ToolName)
	})

	t.Run("Filter by enabled status", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:       "git",
			MaxResults:  10,
			OnlyEnabled: true,
		})
		require.NoError(t, err)
		// Should not include the disabled "status" tool
		foundDisabled := false
		for _, result := range results {
			if result.ToolName == "git__status" {
				foundDisabled = true
				t.Logf("Found disabled tool: %s with enabled=%v", result.ToolName, result.Enabled)
			}
			assert.True(t, result.Enabled, "Tool %s should be enabled but has enabled=%v", result.ToolName, result.Enabled)
		}
		assert.False(t, foundDisabled, "Should not have found the disabled git__status tool")
	})

	t.Run("Filter by server names", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:       "file",
			MaxResults:  10,
			ServerNames: []string{"filesystem"},
		})
		require.NoError(t, err)
		assert.Len(t, results, 2)
		for _, result := range results {
			assert.Equal(t, "filesystem", result.ServerName)
		}
	})

	t.Run("Respect max results limit", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:      "git",
			MaxResults: 1,
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
	})

	t.Run("Empty query returns error", func(t *testing.T) {
		_, err := service.SearchTools(SearchOptions{
			Query:      "",
			MaxResults: 10,
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query cannot be empty")
	})

	t.Run("No matching results", func(t *testing.T) {
		results, err := service.SearchTools(SearchOptions{
			Query:      "nonexistent",
			MaxResults: 10,
		})
		require.NoError(t, err)
		assert.Len(t, results, 0)
	})
}

func TestSearchService_CalculateScore(t *testing.T) {
	service := &SearchService{}

	tests := []struct {
		name        string
		toolName    string
		description string
		terms       []string
		minScore    float64
	}{
		{
			name:        "Exact name match",
			toolName:    "commit",
			description: "Create a git commit",
			terms:       []string{"commit"},
			minScore:    10.0,
		},
		{
			name:        "Partial name match",
			toolName:    "git_commit",
			description: "Create a commit",
			terms:       []string{"commit"},
			minScore:    5.0,
		},
		{
			name:        "Description match",
			toolName:    "create",
			description: "commit changes to repository",
			terms:       []string{"commit"},
			minScore:    1.0,
		},
		{
			name:        "Multiple term matches",
			toolName:    "git_commit",
			description: "Create a git commit with message",
			terms:       []string{"git", "commit"},
			minScore:    3.0, // Average of matches
		},
		{
			name:        "No match",
			toolName:    "branch",
			description: "Switch branches",
			terms:       []string{"commit"},
			minScore:    0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := service.calculateScore(tt.toolName, tt.description, tt.terms)
			assert.GreaterOrEqual(t, score, tt.minScore)
		})
	}
}

func TestSearchService_SortByScore(t *testing.T) {
	service := &SearchService{}

	results := []SearchResult{
		{ToolName: "tool1", Score: 2.0},
		{ToolName: "tool2", Score: 5.0},
		{ToolName: "tool3", Score: 1.0},
		{ToolName: "tool4", Score: 8.0},
		{ToolName: "tool5", Score: 3.0},
	}

	service.sortByScore(results)

	// Verify descending order
	assert.Equal(t, 8.0, results[0].Score)
	assert.Equal(t, 5.0, results[1].Score)
	assert.Equal(t, 3.0, results[2].Score)
	assert.Equal(t, 2.0, results[3].Score)
	assert.Equal(t, 1.0, results[4].Score)
}
