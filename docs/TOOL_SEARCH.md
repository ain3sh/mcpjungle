# Tool Search Feature

## Overview

MCPJungle now provides powerful search functionality to help you find tools across all registered MCP servers. This feature addresses the challenge of discovering relevant tools when dealing with multiple servers and hundreds of tools.

## Features

### 1. Meta-Tool: `mcpjungle__search_tools`

A special meta-tool that is automatically available in the MCP proxy. This tool allows MCP clients (like Claude, Cursor, etc.) to search for tools programmatically.

**Tool Name:** `mcpjungle__search_tools`

**Parameters:**
- `query` (required): Search query string
- `max_results` (optional): Maximum number of results to return (default: 20, max: 100)
- `server_names` (optional): Array of server names to limit search to specific servers
- `only_enabled` (optional): If true, only return enabled tools (default: false)

**Example Usage:**
```json
{
  "name": "mcpjungle__search_tools",
  "arguments": {
    "query": "git commit",
    "max_results": 10,
    "only_enabled": true
  }
}
```

### 2. REST API Endpoint

**Endpoint:** `GET /api/v0/tools/search`

**Query Parameters:**
- `q` (required): Search query string
- `max_results` (optional): Maximum number of results (1-100, default: 20)
- `server[]` (optional): Server names to filter by (can be specified multiple times)
- `only_enabled` (optional): Filter for enabled tools only (true/false)

**Example Request:**
```bash
curl "http://localhost:8080/api/v0/tools/search?q=file&server[]=filesystem&only_enabled=true"
```

**Example Response:**
```json
{
  "query": "file",
  "count": 2,
  "results": [
    {
      "tool_name": "filesystem__read_file",
      "server_name": "filesystem",
      "description": "Read contents of a file from the filesystem",
      "score": 10.0,
      "enabled": true
    },
    {
      "tool_name": "filesystem__write_file",
      "server_name": "filesystem",
      "description": "Write content to a file in the filesystem",
      "score": 8.5,
      "enabled": true
    }
  ]
}
```

## Search Algorithm

The search uses a keyword-based scoring algorithm:

1. **Exact name match**: Highest score (10 points)
2. **Partial name match**: Medium score (5 points)
3. **Description match**: Lower score (1 point per occurrence)

Results are sorted by relevance score in descending order.

## Use Cases

### 1. Tool Discovery
When working with a new MCP server or exploring available functionality, use search to discover relevant tools:
- Search for "git" to find all git-related tools
- Search for "file" to find file manipulation tools
- Search for "api" to find API interaction tools

### 2. Context Limitation
When using AI assistants with token limitations, use search to find the most relevant tools instead of listing all available tools.

### 3. Multi-Server Environments
In environments with dozens of MCP servers, search helps quickly locate tools without knowing which server provides them.

## Implementation Details

### Architecture
- **Search Service**: Core search logic in `internal/service/search/search.go`
- **Meta-Tool Handler**: MCP proxy integration in `internal/service/mcp/search_meta_tool.go`
- **API Handler**: REST endpoint in `internal/api/mcp_search.go`

### Database Queries
The search performs efficient SQL queries with:
- JOIN operations to include server information
- WHERE clauses for filtering by enabled status and server names
- In-memory scoring and sorting for relevance

### Future Enhancements
- **Semantic Search**: Implement embedding-based semantic search for better understanding of tool purposes
- **Search History**: Track popular searches to improve results
- **Fuzzy Matching**: Add fuzzy string matching for typo tolerance
- **Caching**: Cache frequently searched results for better performance

## Testing

Comprehensive tests are included:
- Unit tests for search service logic
- Integration tests for meta-tool functionality
- API endpoint tests

Run tests with:
```bash
go test ./internal/service/search/...
go test -run TestSearchMetaTool ./internal/service/mcp/...
```

## Configuration

No additional configuration is required. The search feature is automatically enabled when MCPJungle starts.

## Performance Considerations

- Search operations are performed in-memory after fetching from database
- Default limit of 20 results keeps response sizes manageable
- Scoring algorithm is O(n*m) where n is number of tools and m is number of search terms

## Troubleshooting

### No Results Found
- Verify tools are registered in the database
- Check if tools are enabled (use `only_enabled=false` to include disabled tools)
- Try broader search terms

### Unexpected Results
- Search is case-insensitive
- Partial matches in tool names score higher than description matches
- Multiple search terms are treated as separate keywords (not phrases)

## Related Issues
- Issue #103: Tool Search - Initial implementation
