// Package audit provides audit trail functionality for tracking operations on key entities.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mcpjungle/mcpjungle/internal/model"
	"github.com/mcpjungle/mcpjungle/internal/util"
	"gorm.io/gorm"
)

// AuditService manages audit trail logging for MCPJungle operations.
type AuditService struct {
	db *gorm.DB
}

// NewAuditService creates a new audit service instance.
func NewAuditService(db *gorm.DB) *AuditService {
	return &AuditService{db: db}
}

// LogCreate logs a CREATE operation on an entity.
func (s *AuditService) LogCreate(ctx context.Context, entityType, entityID, entityName string, data interface{}) {
	changes := s.marshalChanges(map[string]interface{}{
		"created": data,
	})

	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  model.AuditOpCreate,
		Changes:    changes,
		Success:    true,
	})
}

// LogUpdate logs an UPDATE operation on an entity.
// The changes parameter should contain a structured diff of what changed.
func (s *AuditService) LogUpdate(ctx context.Context, entityType, entityID, entityName string, changes map[string]interface{}) {
	changesJSON := s.marshalChanges(changes)

	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  model.AuditOpUpdate,
		Changes:    changesJSON,
		Success:    true,
	})
}

// LogDelete logs a DELETE operation on an entity.
func (s *AuditService) LogDelete(ctx context.Context, entityType, entityID, entityName string) {
	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  model.AuditOpDelete,
		Changes:    s.marshalChanges(map[string]interface{}{}),
		Success:    true,
	})
}

// LogEnable logs an ENABLE operation on an entity.
// The details parameter can contain counts or lists of enabled items.
func (s *AuditService) LogEnable(ctx context.Context, entityType, entityID, entityName string, details map[string]interface{}) {
	changesJSON := s.marshalChanges(details)

	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  model.AuditOpEnable,
		Changes:    changesJSON,
		Success:    true,
	})
}

// LogDisable logs a DISABLE operation on an entity.
// The details parameter can contain counts or lists of disabled items.
func (s *AuditService) LogDisable(ctx context.Context, entityType, entityID, entityName string, details map[string]interface{}) {
	changesJSON := s.marshalChanges(details)

	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  model.AuditOpDisable,
		Changes:    changesJSON,
		Success:    true,
	})
}

// LogError logs a failed operation for security analysis.
func (s *AuditService) LogError(ctx context.Context, entityType, entityID, entityName, operation string, err error) {
	s.logAsync(ctx, &model.AuditLog{
		EntityType: entityType,
		EntityID:   entityID,
		EntityName: entityName,
		Operation:  operation,
		Changes:    s.marshalChanges(map[string]interface{}{}),
		Success:    false,
		ErrorMsg:   err.Error(),
	})
}

// ListByEntity retrieves audit logs for a specific entity.
func (s *AuditService) ListByEntity(entityType, entityID string, limit int) ([]model.AuditLog, error) {
	if limit <= 0 {
		limit = 100 // default limit
	}

	var logs []model.AuditLog
	err := s.db.
		Where("entity_type = ? AND entity_id = ?", entityType, entityID).
		Order("created_at DESC").
		Limit(limit).
		Find(&logs).Error

	return logs, err
}

// ListAll retrieves audit logs with optional filters.
func (s *AuditService) ListAll(filters map[string]interface{}, limit int) ([]model.AuditLog, error) {
	if limit <= 0 {
		limit = 100 // default limit
	}

	query := s.db.Model(&model.AuditLog{})

	// Apply filters if provided
	if entityType, ok := filters["entity_type"].(string); ok && entityType != "" {
		query = query.Where("entity_type = ?", entityType)
	}
	if operation, ok := filters["operation"].(string); ok && operation != "" {
		query = query.Where("operation = ?", operation)
	}
	if actorType, ok := filters["actor_type"].(string); ok && actorType != "" {
		query = query.Where("actor_type = ?", actorType)
	}
	if actorID, ok := filters["actor_id"].(string); ok && actorID != "" {
		query = query.Where("actor_id = ?", actorID)
	}

	var logs []model.AuditLog
	err := query.Order("created_at DESC").Limit(limit).Find(&logs).Error

	return logs, err
}

// logAsync writes an audit log entry asynchronously to avoid blocking primary operations.
// It extracts actor information from context and handles any errors gracefully.
func (s *AuditService) logAsync(ctx context.Context, log *model.AuditLog) {
	// Extract audit context if available
	auditCtx := util.GetAuditContext(ctx)
	if auditCtx != nil {
		log.ActorType = auditCtx.ActorType
		log.ActorID = auditCtx.ActorID
		log.IPAddress = auditCtx.IPAddress
		log.UserAgent = auditCtx.UserAgent
	} else {
		// Default to system if no context is available (e.g., CLI operations)
		log.ActorType = model.AuditActorSystem
		log.ActorID = "system"
	}

	// Write audit log asynchronously to avoid blocking
	go func() {
		defer func() {
			// Recover from any panics to ensure audit logging never crashes the application
			if r := recover(); r != nil {
				// In production, this would be logged to a monitoring system
				fmt.Printf("[WARN] Audit logging panic recovered: %v\n", r)
			}
		}()

		if err := s.db.Create(log).Error; err != nil {
			// Log error but don't fail the operation
			// In production, this would be sent to a monitoring system
			fmt.Printf("[WARN] Failed to write audit log: %v\n", err)
		}
	}()
}

// marshalChanges converts a changes map to JSON for storage.
// It filters out sensitive data like tokens and passwords.
func (s *AuditService) marshalChanges(changes map[string]interface{}) []byte {
	// Filter out sensitive fields
	filtered := s.filterSensitiveData(changes)

	data, err := json.Marshal(filtered)
	if err != nil {
		// Return empty JSON on error
		return []byte("{}")
	}
	return data
}

// filterSensitiveData removes sensitive information from audit data.
func (s *AuditService) filterSensitiveData(data map[string]interface{}) map[string]interface{} {
	filtered := make(map[string]interface{})

	// List of sensitive field names to exclude
	sensitiveFields := map[string]bool{
		"access_token": true,
		"bearer_token": true,
		"password":     true,
		"secret":       true,
		"token":        true,
	}

	for key, value := range data {
		// Skip sensitive fields
		if sensitiveFields[key] {
			filtered[key] = "[REDACTED]"
			continue
		}

		// Recursively filter nested maps
		if nestedMap, ok := value.(map[string]interface{}); ok {
			filtered[key] = s.filterSensitiveData(nestedMap)
			continue
		}

		filtered[key] = value
	}

	return filtered
}
