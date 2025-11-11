package model

import (
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// AuditLog represents an audit trail entry for tracking operations on key entities.
// It provides observability and compliance capabilities for enterprise deployments.
type AuditLog struct {
	gorm.Model

	// EntityType identifies the type of entity being audited.
	// Valid values: "mcp_server", "tool_group", "mcp_client", "user", "tool", "prompt"
	EntityType string `json:"entity_type" gorm:"type:varchar(30);not null;index:idx_audit_entity"`

	// EntityID is the unique identifier of the entity (name or ID).
	EntityID string `json:"entity_id" gorm:"type:varchar(255);not null;index:idx_audit_entity"`

	// EntityName is the human-readable name of the entity for display purposes.
	EntityName string `json:"entity_name" gorm:"type:varchar(255)"`

	// Operation describes the action performed on the entity.
	// Valid values: "CREATE", "UPDATE", "DELETE", "ENABLE", "DISABLE"
	Operation string `json:"operation" gorm:"type:varchar(20);not null;index:idx_audit_operation"`

	// Changes contains a structured representation of what changed.
	// This is stored as JSON to allow flexible change tracking.
	// For CREATE: contains the initial configuration
	// For UPDATE: contains a diff of changes (e.g., {"tools_added": [...], "description_changed": {...}})
	// For DELETE: contains the final state before deletion
	// For ENABLE/DISABLE: contains count or list of affected items
	Changes datatypes.JSON `json:"changes" gorm:"type:jsonb"`

	// ActorType identifies the type of actor performing the operation.
	// Valid values: "user", "mcp_client", "system"
	ActorType string `json:"actor_type" gorm:"type:varchar(20);not null"`

	// ActorID identifies the specific actor (username, client name, or "system").
	ActorID string `json:"actor_id" gorm:"type:varchar(255);not null"`

	// IPAddress stores the client's IP address for security auditing.
	// Optional field that may be empty for CLI operations or system actions.
	IPAddress string `json:"ip_address" gorm:"type:varchar(45)"` // IPv6 max length

	// UserAgent stores the client's user agent string.
	// Optional field that may be empty for CLI operations or system actions.
	UserAgent string `json:"user_agent" gorm:"type:varchar(255)"`

	// Success indicates whether the operation completed successfully.
	// Failed operations are also logged for security analysis.
	Success bool `json:"success" gorm:"not null;default:true"`

	// ErrorMsg contains the error message if the operation failed.
	// Empty for successful operations.
	ErrorMsg string `json:"error_msg" gorm:"type:text"`
}

// AuditEntityType constants for entity types
const (
	AuditEntityMcpServer  = "mcp_server"
	AuditEntityToolGroup  = "tool_group"
	AuditEntityMcpClient  = "mcp_client"
	AuditEntityUser       = "user"
	AuditEntityTool       = "tool"
	AuditEntityPrompt     = "prompt"
)

// AuditOperation constants for operations
const (
	AuditOpCreate  = "CREATE"
	AuditOpUpdate  = "UPDATE"
	AuditOpDelete  = "DELETE"
	AuditOpEnable  = "ENABLE"
	AuditOpDisable = "DISABLE"
)

// AuditActorType constants for actor types
const (
	AuditActorUser      = "user"
	AuditActorMcpClient = "mcp_client"
	AuditActorSystem    = "system"
)
