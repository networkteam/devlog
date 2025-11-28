package collector

import (
	"context"

	"github.com/gofrs/uuid"
)

type sessionIDKeyType struct{}

var sessionIDKey = sessionIDKeyType{}

// WithSessionID returns a new context with the session ID added.
func WithSessionID(ctx context.Context, sessionID uuid.UUID) context.Context {
	return context.WithValue(ctx, sessionIDKey, sessionID)
}

// SessionIDFromContext retrieves the session ID from the context.
// Returns the session ID and true if found, or uuid.Nil and false if not set.
func SessionIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if sessionID, ok := ctx.Value(sessionIDKey).(uuid.UUID); ok {
		return sessionID, true
	}
	return uuid.Nil, false
}
