package model

import (
	"encoding/json"
	"errors"
	"testing"
)

// Mock implementations for testing
type mockToolGroupChecker struct {
	groups map[string]*ToolGroup
}

func (m *mockToolGroupChecker) GetToolGroup(name string) (*ToolGroup, error) {
	if group, exists := m.groups[name]; exists {
		return group, nil
	}
	return nil, errors.New("tool group not found")
}

type mockToolGroupResolver struct {
	serverTools map[string][]Tool
}

func (m *mockToolGroupResolver) ListToolsByServer(serverName string) ([]Tool, error) {
	if tools, exists := m.serverTools[serverName]; exists {
		return tools, nil
	}
	return []Tool{}, nil
}

func (m *mockToolGroupResolver) ListPromptsByServer(serverName string) ([]Prompt, error) {
	return []Prompt{}, nil
}

// TestGetAllowedToolGroups tests the GetAllowedToolGroups method
func TestGetAllowedToolGroups(t *testing.T) {
	tests := []struct {
		name          string
		client        *McpClient
		expectedLen   int
		expectedError bool
	}{
		{
			name: "nil AllowedToolGroups",
			client: &McpClient{
				AllowedToolGroups: nil,
			},
			expectedLen:   0,
			expectedError: false,
		},
		{
			name: "empty AllowedToolGroups",
			client: &McpClient{
				AllowedToolGroups: []byte("[]"),
			},
			expectedLen:   0,
			expectedError: false,
		},
		{
			name: "single group",
			client: &McpClient{
				AllowedToolGroups: []byte(`["group1"]`),
			},
			expectedLen:   1,
			expectedError: false,
		},
		{
			name: "multiple groups",
			client: &McpClient{
				AllowedToolGroups: []byte(`["group1", "group2", "group3"]`),
			},
			expectedLen:   3,
			expectedError: false,
		},
		{
			name: "invalid JSON",
			client: &McpClient{
				AllowedToolGroups: []byte(`invalid`),
			},
			expectedLen:   0,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			groups, err := tt.client.GetAllowedToolGroups()
			
			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(groups) != tt.expectedLen {
					t.Errorf("expected %d groups, got %d", tt.expectedLen, len(groups))
				}
			}
		})
	}
}

// TestCheckHasToolAccess tests the tool-level ACL logic
func TestCheckHasToolAccess(t *testing.T) {
	// Setup mock checker and resolver
	checker := &mockToolGroupChecker{
		groups: map[string]*ToolGroup{
			"group1": {
				IncludedTools: mustMarshalJSON([]string{"server1__tool1", "server1__tool2"}),
			},
			"group2": {
				IncludedServers: mustMarshalJSON([]string{"server2"}),
			},
		},
	}

	resolver := &mockToolGroupResolver{
		serverTools: map[string][]Tool{
			"server2": {
				{Name: "server2__tool1"},
				{Name: "server2__tool2"},
			},
		},
	}

	tests := []struct {
		name           string
		client         *McpClient
		toolName       string
		expectedAccess bool
		expectedError  bool
	}{
		{
			name: "tool-level ACL: tool in allowed group",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"group1"}),
				AllowList:         mustMarshalJSON([]string{}),
			},
			toolName:       "server1__tool1",
			expectedAccess: true,
			expectedError:  false,
		},
		{
			name: "tool-level ACL: tool not in allowed group",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"group1"}),
				AllowList:         mustMarshalJSON([]string{}),
			},
			toolName:       "server1__tool3",
			expectedAccess: false,
			expectedError:  false,
		},
		{
			name: "tool-level ACL: tool from server in allowed group",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"group2"}),
				AllowList:         mustMarshalJSON([]string{}),
			},
			toolName:       "server2__tool1",
			expectedAccess: true,
			expectedError:  false,
		},
		{
			name: "tool-level ACL: multiple groups, tool in one of them",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"group1", "group2"}),
				AllowList:         mustMarshalJSON([]string{}),
			},
			toolName:       "server2__tool2",
			expectedAccess: true,
			expectedError:  false,
		},
		{
			name: "server-level ACL fallback: no tool groups specified",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{}),
				AllowList:         mustMarshalJSON([]string{"server3"}),
			},
			toolName:       "server3__tool1",
			expectedAccess: true,
			expectedError:  false,
		},
		{
			name: "server-level ACL fallback: server not in allow list",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{}),
				AllowList:         mustMarshalJSON([]string{"server3"}),
			},
			toolName:       "server4__tool1",
			expectedAccess: false,
			expectedError:  false,
		},
		{
			name: "tool-level ACL takes precedence over server-level",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"group1"}),
				AllowList:         mustMarshalJSON([]string{"server2"}),
			},
			toolName:       "server2__tool1",
			expectedAccess: false, // group1 doesn't have server2 tools
			expectedError:  false,
		},
		{
			name: "invalid tool name format",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{}),
				AllowList:         mustMarshalJSON([]string{"server1"}),
			},
			toolName:       "invalid-tool-name",
			expectedAccess: false,
			expectedError:  true,
		},
		{
			name: "nonexistent group in allowed groups",
			client: &McpClient{
				AllowedToolGroups: mustMarshalJSON([]string{"nonexistent"}),
				AllowList:         mustMarshalJSON([]string{}),
			},
			toolName:       "server1__tool1",
			expectedAccess: false,
			expectedError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasAccess, err := tt.client.CheckHasToolAccess(tt.toolName, checker, resolver)

			if tt.expectedError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if hasAccess != tt.expectedAccess {
					t.Errorf("expected access=%v, got %v for tool %s", tt.expectedAccess, hasAccess, tt.toolName)
				}
			}
		})
	}
}

// TestSplitServerToolName tests the splitServerToolName helper function
func TestSplitServerToolName(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		expectedServer     string
		expectedTool       string
		expectedOK         bool
	}{
		{
			name:           "valid tool name",
			input:          "server1__tool1",
			expectedServer: "server1",
			expectedTool:   "tool1",
			expectedOK:     true,
		},
		{
			name:           "valid tool name with underscores",
			input:          "my_server__my_tool",
			expectedServer: "my_server",
			expectedTool:   "my_tool",
			expectedOK:     true,
		},
		{
			name:           "invalid: single underscore",
			input:          "server_tool",
			expectedServer: "",
			expectedTool:   "",
			expectedOK:     false,
		},
		{
			name:           "invalid: no separator",
			input:          "servertool",
			expectedServer: "",
			expectedTool:   "",
			expectedOK:     false,
		},
		{
			name:           "invalid: empty string",
			input:          "",
			expectedServer: "",
			expectedTool:   "",
			expectedOK:     false,
		},
		{
			name:           "valid: multiple double underscores (takes first)",
			input:          "server__tool__extra",
			expectedServer: "server",
			expectedTool:   "tool__extra",
			expectedOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, tool, ok := splitServerToolName(tt.input)

			if ok != tt.expectedOK {
				t.Errorf("expected ok=%v, got %v", tt.expectedOK, ok)
			}
			if server != tt.expectedServer {
				t.Errorf("expected server=%s, got %s", tt.expectedServer, server)
			}
			if tool != tt.expectedTool {
				t.Errorf("expected tool=%s, got %s", tt.expectedTool, tool)
			}
		})
	}
}

// TestServerLevelFallback ensures that when no tool groups are specified, server-level ACL still works
func TestServerLevelFallback(t *testing.T) {
	checker := &mockToolGroupChecker{groups: map[string]*ToolGroup{}}
	resolver := &mockToolGroupResolver{serverTools: map[string][]Tool{}}

	// Client with only AllowList (no tool groups)
	client := &McpClient{
		AllowedToolGroups: mustMarshalJSON([]string{}),
		AllowList:         mustMarshalJSON([]string{"server1", "server2"}),
	}

	tests := []struct {
		toolName       string
		expectedAccess bool
	}{
		{"server1__tool1", true},
		{"server2__tool1", true},
		{"server3__tool1", false},
	}

	for _, tt := range tests {
		t.Run(tt.toolName, func(t *testing.T) {
			hasAccess, err := client.CheckHasToolAccess(tt.toolName, checker, resolver)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if hasAccess != tt.expectedAccess {
				t.Errorf("expected access=%v, got %v", tt.expectedAccess, hasAccess)
			}
		})
	}
}

// Helper function to marshal JSON for tests
func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}
