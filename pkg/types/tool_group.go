package types

// ToolGroup represents a group (collection) of MCP Tools and Prompts.
// A group can contain a subset of all available tools and prompts in the MCPJungle system.
// This allows you to expose a limited set of tools and prompts to certain mcp clients.
type ToolGroup struct {
	// Name is the unique name of the tool group (mandatory).
	Name string `json:"name"`
	// IncludedTools is a list of tools included in this group.
	IncludedTools []string `json:"included_tools,omitempty"`
	// IncludedServers is a list of MCP server names. All tools and prompts from these servers will be included.
	IncludedServers []string `json:"included_servers,omitempty"`
	// ExcludedTools is a list of tools to exclude from the group (useful with IncludedServers).
	ExcludedTools []string `json:"excluded_tools,omitempty"`

	// IncludedPrompts is a list of prompts included in this group.
	IncludedPrompts []string `json:"included_prompts,omitempty"`
	// ExcludedPrompts is a list of prompts to exclude from the group (useful with IncludedServers).
	ExcludedPrompts []string `json:"excluded_prompts,omitempty"`

	Description string `json:"description"`
}

// ToolGroupEndpoints contains the endpoints a MCP client can use to access a tool group.
type ToolGroupEndpoints struct {
	StreamableHTTPEndpoint string `json:"streamable_http_endpoint"`
	SSEEndpoint            string `json:"sse_endpoint"`
	SSEMessageEndpoint     string `json:"sse_message_endpoint"`
}

type CreateToolGroupResponse struct {
	*ToolGroupEndpoints
}

type GetToolGroupResponse struct {
	*ToolGroup
	*ToolGroupEndpoints
}

// UpdateToolGroupResponse contains the old and new configuration of a tool group after a successful update.
type UpdateToolGroupResponse struct {
	Name string `json:"name"`

	// Old contains the original configuration of the tool group.
	Old *ToolGroup `json:"old"`
	// New contains the now-live configuration of the tool group.
	New *ToolGroup `json:"new"`
}
