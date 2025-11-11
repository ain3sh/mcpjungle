package util

import "context"

// AuditContext contains information about the actor performing an operation.
// This is extracted from HTTP request context and passed through the service layer
// to enable proper audit trail logging.
type AuditContext struct {
	// ActorType identifies the type of actor: "user", "mcp_client", or "system"
	ActorType string

	// ActorID is the username, client name, or "system"
	ActorID string

	// IPAddress is the client's IP address (optional)
	IPAddress string

	// UserAgent is the client's user agent string (optional)
	UserAgent string
}

type auditContextKey struct{}

// SetAuditContext stores audit context information in the context.
// This is typically called by middleware to inject actor information.
func SetAuditContext(ctx context.Context, ac *AuditContext) context.Context {
	return context.WithValue(ctx, auditContextKey{}, ac)
}

// GetAuditContext retrieves audit context information from the context.
// Returns nil if no audit context is present (e.g., CLI operations, system tasks).
func GetAuditContext(ctx context.Context) *AuditContext {
	ac, ok := ctx.Value(auditContextKey{}).(*AuditContext)
	if !ok {
		return nil
	}
	return ac
}
