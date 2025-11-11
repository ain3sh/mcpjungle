package search

import (
	"fmt"
	"strings"
	"sync"

	"github.com/mcpjungle/mcpjungle/internal/model"
	"gorm.io/gorm"
)

// SearchService provides search functionality for tools
type SearchService struct {
	db *gorm.DB
	mu sync.RWMutex
}

// NewSearchService creates a new SearchService
func NewSearchService(db *gorm.DB) *SearchService {
	return &SearchService{
		db: db,
	}
}

// SearchResult represents a single tool search result
type SearchResult struct {
	ToolName    string  `json:"tool_name"`
	ServerName  string  `json:"server_name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
	Enabled     bool    `json:"enabled"`
}

// SearchOptions contains options for searching tools
type SearchOptions struct {
	Query       string   `json:"query"`
	MaxResults  int      `json:"max_results,omitempty"`
	ServerNames []string `json:"server_names,omitempty"`
	OnlyEnabled bool     `json:"only_enabled,omitempty"`
}

// SearchTools performs a keyword search across all tools
func (s *SearchService) SearchTools(opts SearchOptions) ([]SearchResult, error) {
	if opts.Query == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// Default to 20 results if not specified
	if opts.MaxResults <= 0 {
		opts.MaxResults = 20
	}

	// Normalize the query for case-insensitive search
	query := strings.ToLower(opts.Query)
	terms := strings.Fields(query) // Split into individual terms

	// Build the base query
	tx := s.db.Table("tools").
		Select("tools.*, mcp_servers.name as server_name").
		Joins("LEFT JOIN mcp_servers ON tools.server_id = mcp_servers.id")

	// Apply filters
	if opts.OnlyEnabled {
		tx = tx.Where("tools.enabled = ?", true)
	}

	if len(opts.ServerNames) > 0 {
		tx = tx.Where("mcp_servers.name IN ?", opts.ServerNames)
	}

	// Fetch all matching tools
	var rawResults []struct {
		model.Tool
		ServerName string `gorm:"column:server_name"`
	}

	if err := tx.Find(&rawResults).Error; err != nil {
		return nil, fmt.Errorf("failed to search tools: %w", err)
	}

	// Score and rank results
	results := make([]SearchResult, 0)
	for _, raw := range rawResults {
		score := s.calculateScore(raw.Name, raw.Description, terms)
		if score > 0 {
			results = append(results, SearchResult{
				ToolName:    fmt.Sprintf("%s__%s", raw.ServerName, raw.Name),
				ServerName:  raw.ServerName,
				Description: raw.Description,
				Score:       score,
				Enabled:     raw.Enabled,
			})
		}
	}

	// Sort by score (highest first)
	s.sortByScore(results)

	// Limit results
	if len(results) > opts.MaxResults {
		results = results[:opts.MaxResults]
	}

	return results, nil
}

// calculateScore calculates a relevance score for a tool based on search terms
func (s *SearchService) calculateScore(name, description string, terms []string) float64 {
	nameLower := strings.ToLower(name)
	descLower := strings.ToLower(description)
	
	var totalScore float64
	
	for _, term := range terms {
		termScore := 0.0
		
		// Exact match in name gets highest score
		if nameLower == term {
			termScore += 10.0
		} else if strings.Contains(nameLower, term) {
			// Partial match in name
			termScore += 5.0
		}
		
		// Match in description
		if strings.Contains(descLower, term) {
			// Count occurrences
			count := strings.Count(descLower, term)
			termScore += float64(count) * 1.0
		}
		
		totalScore += termScore
	}
	
	// Normalize by number of terms to favor matches for all terms
	if len(terms) > 0 {
		totalScore = totalScore / float64(len(terms))
	}
	
	return totalScore
}

// sortByScore sorts results by score in descending order
func (s *SearchService) sortByScore(results []SearchResult) {
	// Simple bubble sort for now (can be optimized if needed)
	n := len(results)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if results[j].Score < results[j+1].Score {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// SemanticSearchTools performs semantic search using embeddings (future enhancement)
// For now, it delegates to keyword search
func (s *SearchService) SemanticSearchTools(opts SearchOptions) ([]SearchResult, error) {
	// TODO: Implement semantic search using embeddings
	// This would involve:
	// 1. Computing embeddings for tool names and descriptions
	// 2. Computing embedding for the query
	// 3. Finding tools with highest cosine similarity
	// For now, we delegate to keyword search
	return s.SearchTools(opts)
}
