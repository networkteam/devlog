package collector

import (
	"context"

	"github.com/gofrs/uuid"
)

type sessionIDsKeyType struct{}

var sessionIDsKey = sessionIDsKeyType{}

// WithSessionIDs returns a new context with the session IDs added.
// Multiple session IDs are used when multiple dashboard tabs have capture enabled.
func WithSessionIDs(ctx context.Context, sessionIDs []uuid.UUID) context.Context {
	return context.WithValue(ctx, sessionIDsKey, sessionIDs)
}

// SessionIDsFromContext retrieves the session IDs from the context.
// Returns the session IDs and true if found, or nil and false if not set.
func SessionIDsFromContext(ctx context.Context) ([]uuid.UUID, bool) {
	if sessionIDs, ok := ctx.Value(sessionIDsKey).([]uuid.UUID); ok {
		return sessionIDs, true
	}
	return nil, false
}
