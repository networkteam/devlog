package collector

import (
	"context"

	"github.com/gofrs/uuid"
)

type ctxKey string

const (
	groupIDKey ctxKey = "groupID"
)

func groupIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if groupID, ok := ctx.Value(groupIDKey).(uuid.UUID); ok {
		return groupID, true
	}
	return uuid.Nil, false
}

func withGroupID(ctx context.Context, groupID uuid.UUID) context.Context {
	return context.WithValue(ctx, groupIDKey, groupID)
}
