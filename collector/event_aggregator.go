package collector

import (
	"context"
	"sync"
	"time"

	"github.com/gofrs/uuid"
)

// EventAggregator coordinates event collection and dispatches events to registered storages.
// It does not store events itself - each storage has its own buffer.
type EventAggregator struct {
	storages   map[uuid.UUID]EventStorage
	openGroups map[uuid.UUID]*Event

	mu sync.RWMutex
}

// NewEventAggregator creates a new EventAggregator.
func NewEventAggregator() *EventAggregator {
	return &EventAggregator{
		storages:   make(map[uuid.UUID]EventStorage),
		openGroups: make(map[uuid.UUID]*Event),
	}
}

// RegisterStorage registers a storage with the aggregator.
func (a *EventAggregator) RegisterStorage(storage EventStorage) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.storages[storage.ID()] = storage
}

// UnregisterStorage removes a storage from the aggregator.
func (a *EventAggregator) UnregisterStorage(id uuid.UUID) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.storages, id)
}

// GetStorage returns a storage by ID, or nil if not found.
func (a *EventAggregator) GetStorage(id uuid.UUID) EventStorage {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.storages[id]
}

// ShouldCapture returns true if any registered storage wants to capture events for the given context.
func (a *EventAggregator) ShouldCapture(ctx context.Context) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()

	for _, storage := range a.storages {
		if storage.ShouldCapture(ctx) {
			return true
		}
	}
	return false
}

// StartEvent starts a new event and returns a new context with the group ID.
// Child events collected with this context will be grouped under this event.
// Call EndEvent to finish the event.
func (a *EventAggregator) StartEvent(ctx context.Context) context.Context {
	eventID := uuid.Must(uuid.NewV7())

	a.mu.Lock()
	defer a.mu.Unlock()

	evt := &Event{
		ID:    eventID,
		Start: time.Now(),
	}

	// Check if there's an outer group
	outerGroupID, ok := groupIDFromContext(ctx)
	if ok {
		evt.GroupID = &outerGroupID
	}

	a.openGroups[eventID] = evt

	return withGroupID(ctx, eventID)
}

// EndEvent finishes an event started with StartEvent and dispatches it to matching storages.
func (a *EventAggregator) EndEvent(ctx context.Context, data any) {
	groupID, ok := groupIDFromContext(ctx)
	if !ok {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	evt := a.openGroups[groupID]
	if evt == nil {
		return
	}

	evt.Data = data
	evt.End = time.Now()
	evt.Size = evt.calculateSize()

	// Link to parent if exists
	if evt.GroupID != nil {
		parentEvt := a.openGroups[*evt.GroupID]
		if parentEvt != nil {
			parentEvt.Children = append(parentEvt.Children, evt)
		}
	}

	delete(a.openGroups, groupID)

	// Only dispatch top-level events to storages
	if evt.GroupID == nil {
		a.dispatchToStorages(ctx, evt)
	}
}

// CollectEvent creates and immediately completes an event, dispatching to matching storages.
func (a *EventAggregator) CollectEvent(ctx context.Context, data any) {
	eventID := uuid.Must(uuid.NewV7())
	now := time.Now()

	a.mu.Lock()
	defer a.mu.Unlock()

	evt := &Event{
		ID:    eventID,
		Data:  data,
		Start: now,
		End:   now,
	}
	evt.Size = evt.calculateSize()

	// Check if there's a parent group
	outerGroupID, ok := groupIDFromContext(ctx)
	if ok {
		evt.GroupID = &outerGroupID
		parentEvt := a.openGroups[outerGroupID]
		if parentEvt != nil {
			parentEvt.Children = append(parentEvt.Children, evt)
		}
	}

	// Only dispatch top-level events to storages
	if evt.GroupID == nil {
		a.dispatchToStorages(ctx, evt)
	}
}

// dispatchToStorages sends the event to all storages that want to capture it.
// Must be called with lock held.
func (a *EventAggregator) dispatchToStorages(ctx context.Context, evt *Event) {
	for _, storage := range a.storages {
		if storage.ShouldCapture(ctx) {
			storage.Add(evt)
		}
	}
}

// Close releases resources used by the aggregator.
func (a *EventAggregator) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, storage := range a.storages {
		storage.Close()
	}
	a.storages = make(map[uuid.UUID]EventStorage)
	a.openGroups = make(map[uuid.UUID]*Event)
}

// Stats holds aggregated statistics across all storages
type Stats struct {
	TotalMemory  uint64
	EventCount   int
	StorageCount int
}

// CalculateStats computes stats across all storages, de-duplicating events by ID
func (a *EventAggregator) CalculateStats() Stats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	seen := make(map[uuid.UUID]struct{})
	var totalMemory uint64

	for _, storage := range a.storages {
		// Get all events from storage (use a large limit to get all)
		events := storage.GetEvents(100000)
		for _, event := range events {
			if _, exists := seen[event.ID]; !exists {
				seen[event.ID] = struct{}{}
				totalMemory += event.Size
				// Add children sizes
				for _, child := range event.Children {
					totalMemory += child.Size
				}
			}
		}
	}

	return Stats{
		TotalMemory:  totalMemory,
		EventCount:   len(seen),
		StorageCount: len(a.storages),
	}
}
