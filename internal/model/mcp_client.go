package model

import (
	"encoding/json"
	"fmt"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ToolGroupToolChecker defines the interface needed to check if a tool exists in a tool group.
type ToolGroupToolChecker interface {
	// GetToolGroup retrieves a tool group by name.
	GetToolGroup(name string) (*ToolGroup, error)
}

// McpClient represents MCP clients and their access to the MCP Servers provided MCPJungle MCP server
type McpClient struct {
	gorm.Model

	Name        string `json:"name" gorm:"uniqueIndex;not null"`
	Description string `json:"description"`

	AccessToken string `json:"access_token" gorm:"unique; not null"`

	// AllowList contains a list of MCP Server names that this client is allowed to view and call
	// storing the list of server names as a JSON array is a convenient way for now.
	// In the future, this will be removed in favor of a separate table for ACLs.
	AllowList datatypes.JSON `json:"allow_list" gorm:"type:jsonb; not null"`

	// AllowedToolGroups contains a list of tool group names that this client can access.
	// This provides fine-grained tool-level access control.
	// If specified, tool access is determined by group membership; otherwise, falls back to server-level ACL.
	AllowedToolGroups datatypes.JSON `json:"allowed_tool_groups" gorm:"type:jsonb"`
}

// CheckHasServerAccess returns true if this client has access to the specified MCP server.
// If not, it returns false.
func (c *McpClient) CheckHasServerAccess(serverName string) bool {
	if c.AllowList == nil {
		return false
	}
	var allowedServers []string
	if err := json.Unmarshal(c.AllowList, &allowedServers); err != nil {
		return false
	}
	for _, allowed := range allowedServers {
		if allowed == serverName {
			return true
		}
	}
	return false
}

// GetAllowedToolGroups unmarshals and returns the list of allowed tool groups.
// Returns an empty slice if AllowedToolGroups is nil or empty.
func (c *McpClient) GetAllowedToolGroups() ([]string, error) {
	if c.AllowedToolGroups == nil {
		return []string{}, nil
	}
	var groups []string
	if err := json.Unmarshal(c.AllowedToolGroups, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// CheckHasToolAccess checks if this client has access to a specific tool.
// If AllowedToolGroups is specified, it checks if the tool exists in any of the allowed groups.
// Otherwise, it falls back to server-level ACL using CheckHasServerAccess.
func (c *McpClient) CheckHasToolAccess(toolName string, checker ToolGroupToolChecker, resolver ToolGroupResolver) (bool, error) {
	allowedGroups, err := c.GetAllowedToolGroups()
	if err != nil {
		return false, fmt.Errorf("failed to get allowed tool groups: %w", err)
	}

	// If tool groups are specified, use tool-level ACL
	if len(allowedGroups) > 0 {
		return c.toolExistsInAllowedGroups(toolName, allowedGroups, checker, resolver)
	}

	// Fall back to server-level ACL
	serverName, _, ok := splitServerToolName(toolName)
	if !ok {
		return false, fmt.Errorf("invalid tool name format: %s", toolName)
	}
	return c.CheckHasServerAccess(serverName), nil
}

// toolExistsInAllowedGroups checks if a tool exists in any of the allowed tool groups.
func (c *McpClient) toolExistsInAllowedGroups(toolName string, allowedGroups []string, checker ToolGroupToolChecker, resolver ToolGroupResolver) (bool, error) {
	for _, groupName := range allowedGroups {
		group, err := checker.GetToolGroup(groupName)
		if err != nil {
			// If the group doesn't exist, skip it
			continue
		}

		// Resolve effective tools for this group
		effectiveTools, err := group.ResolveEffectiveTools(resolver)
		if err != nil {
			return false, fmt.Errorf("failed to resolve tools for group %s: %w", groupName, err)
		}

		// Check if the tool is in this group
		for _, tool := range effectiveTools {
			if tool == toolName {
				return true, nil
			}
		}
	}

	return false, nil
}

// splitServerToolName splits a canonical tool name (server__tool) into server and tool names.
func splitServerToolName(name string) (serverName, toolName string, ok bool) {
	const sep = "__"
	for i := 0; i < len(name)-1; i++ {
		if name[i:i+2] == sep {
			return name[:i], name[i+2:], true
		}
	}
	return "", "", false
}
