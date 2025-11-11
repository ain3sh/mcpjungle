package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/mcpjungle/mcpjungle/internal/service/search"
)

// searchToolsHandler handles the /api/v0/tools/search endpoint
func (s *Server) searchToolsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get query parameter (required)
		query := c.Query("q")
		if query == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing 'q' query parameter"})
			return
		}

		// Build search options
		opts := search.SearchOptions{
			Query: query,
		}

		// Get optional parameters
		if maxResultsStr := c.Query("max_results"); maxResultsStr != "" {
			maxResults, err := strconv.Atoi(maxResultsStr)
			if err != nil || maxResults < 1 || maxResults > 100 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'max_results' parameter (must be 1-100)"})
				return
			}
			opts.MaxResults = maxResults
		} else {
			opts.MaxResults = 20 // default
		}

		// Get server filter
		if serverNames := c.QueryArray("server"); len(serverNames) > 0 {
			opts.ServerNames = serverNames
		}

		// Get enabled filter
		if onlyEnabledStr := c.Query("only_enabled"); onlyEnabledStr != "" {
			onlyEnabled, err := strconv.ParseBool(onlyEnabledStr)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid 'only_enabled' parameter (must be true/false)"})
				return
			}
			opts.OnlyEnabled = onlyEnabled
		}

		// Get search service from the mcp service
		searchService := s.mcpService.GetSearchService()

		// Perform search
		results, err := searchService.SearchTools(opts)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Return results
		c.JSON(http.StatusOK, gin.H{
			"query":   query,
			"results": results,
			"count":   len(results),
		})
	}
}
