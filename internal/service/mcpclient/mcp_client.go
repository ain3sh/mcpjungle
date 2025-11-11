// Package mcpclient provides MCP client service functionality for the MCPJungle application.
package mcpclient

import (
	"context"
	"errors"
	"fmt"

	"github.com/mcpjungle/mcpjungle/internal"
	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/service/audit"
	"gorm.io/gorm"
)

// McpClientService provides methods to manage MCP clients in the database.
type McpClientService struct {
	db           *gorm.DB
	auditService *audit.AuditService
}

func NewMCPClientService(db *gorm.DB) *McpClientService {
	return &McpClientService{
		db:           db,
		auditService: audit.NewAuditService(db),
	}
}

// ListClients retrieves all MCP clients known to mcpjungle from the database
func (m *McpClientService) ListClients() ([]*model.McpClient, error) {
	var clients []*model.McpClient
	if err := m.db.Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// CreateClient creates a new MCP client in the database.
// It also generates a new access token for the client.
func (m *McpClientService) CreateClient(client model.McpClient) (*model.McpClient, error) {
	token, err := internal.GenerateAccessToken()
	if err != nil {
		return nil, fmt.Errorf("failed to generate access token: %w", err)
	}
	client.AccessToken = token

	// Initialize AllowList with empty array if not provided
	if client.AllowList == nil {
		client.AllowList = []byte("[]")
	}

	// Initialize AllowedToolGroups with empty array if not provided
	if client.AllowedToolGroups == nil {
		client.AllowedToolGroups = []byte("[]")
	}

	if err := m.db.Create(&client).Error; err != nil {
		return nil, err
	}

	// Get allowed groups for audit log
	allowedGroups, _ := client.GetAllowedToolGroups()

	// Log client creation
	m.auditService.LogCreate(context.Background(), model.AuditEntityMcpClient, client.Name, client.Name, map[string]interface{}{
		"description":         client.Description,
		"allowed_tool_groups": allowedGroups,
	})

	return &client, nil
}

// GetClientByToken retrieves an MCP client by its access token from the database.
// It returns an error if no such client is found.
func (m *McpClientService) GetClientByToken(token string) (*model.McpClient, error) {
	var client model.McpClient
	if err := m.db.Where("access_token = ?", token).First(&client).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("client not found")
		}
		return nil, err
	}
	return &client, nil
}

// DeleteClient removes an MCP client from the database and immediately revokes its access.
// It is an idempotent operation. Deleting a client that does not exist will not return an error.
func (m *McpClientService) DeleteClient(name string) error {
	result := m.db.Unscoped().Where("name = ?", name).Delete(&model.McpClient{})
	if result.Error != nil {
		return result.Error
	}

	// Log client deletion (only if something was actually deleted)
	if result.RowsAffected > 0 {
		m.auditService.LogDelete(context.Background(), model.AuditEntityMcpClient, name, name)
	}

	return nil
}
