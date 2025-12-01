package collector_test

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/networkteam/devlog/collector"
)

func TestWithSessionIDs_AddsToContext(t *testing.T) {
	ctx := context.Background()
	sessionID := uuid.Must(uuid.NewV4())

	newCtx := collector.WithSessionIDs(ctx, []uuid.UUID{sessionID})

	retrievedIDs, ok := collector.SessionIDsFromContext(newCtx)
	assert.True(t, ok)
	assert.Equal(t, []uuid.UUID{sessionID}, retrievedIDs)
}

func TestWithSessionIDs_MultipleIDs(t *testing.T) {
	ctx := context.Background()
	sessionID1 := uuid.Must(uuid.NewV4())
	sessionID2 := uuid.Must(uuid.NewV4())

	newCtx := collector.WithSessionIDs(ctx, []uuid.UUID{sessionID1, sessionID2})

	retrievedIDs, ok := collector.SessionIDsFromContext(newCtx)
	assert.True(t, ok)
	assert.Len(t, retrievedIDs, 2)
	assert.Contains(t, retrievedIDs, sessionID1)
	assert.Contains(t, retrievedIDs, sessionID2)
}

func TestSessionIDsFromContext_NotSet(t *testing.T) {
	ctx := context.Background()

	retrievedIDs, ok := collector.SessionIDsFromContext(ctx)

	assert.False(t, ok)
	assert.Nil(t, retrievedIDs)
}

func TestSessionIDsFromContext_Set(t *testing.T) {
	sessionID := uuid.Must(uuid.NewV4())
	ctx := collector.WithSessionIDs(context.Background(), []uuid.UUID{sessionID})

	retrievedIDs, ok := collector.SessionIDsFromContext(ctx)

	assert.True(t, ok)
	assert.Equal(t, []uuid.UUID{sessionID}, retrievedIDs)
}
