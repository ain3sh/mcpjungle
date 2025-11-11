package types

// McpClient represents an MCP client that is authorized to access the MCPJungle MCP Proxy server.
type McpClient struct {
	// Name is the name of the client that uniquely identifies it within mcpungle.
	Name        string `json:"name"`
	Description string `json:"description"`

	// AllowList is a list of MCP Servers that this client is allowed to access from MCPJungle.
	AllowList []string `json:"allow_list"`

	// AllowedToolGroups is a list of tool group names that this client can access.
	// This provides fine-grained tool-level access control.
	// If specified, tool access is determined by group membership; otherwise, falls back to server-level ACL.
	AllowedToolGroups []string `json:"allowed_tool_groups,omitempty"`
}
