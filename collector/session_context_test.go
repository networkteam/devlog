package collector_test

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/networkteam/devlog/collector"
)

func TestWithSessionID_AddsToContext(t *testing.T) {
	ctx := context.Background()
	sessionID := uuid.Must(uuid.NewV4())

	newCtx := collector.WithSessionID(ctx, sessionID)

	retrievedID, ok := collector.SessionIDFromContext(newCtx)
	assert.True(t, ok)
	assert.Equal(t, sessionID, retrievedID)
}

func TestSessionIDFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	retrievedID, ok := collector.SessionIDFromContext(ctx)

	assert.False(t, ok)
	assert.Equal(t, uuid.Nil, retrievedID)
}

func TestSessionIDFromContext_Set(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	ctx := collector.WithSessionID(context.Background(), sessionID)

	retrievedID, ok := collector.SessionIDFromContext(ctx)

	assert.True(t, ok)
	assert.Equal(t, sessionID, retrievedID)
}
