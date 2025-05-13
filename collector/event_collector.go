package collector

import (
	"context"
	"iter"
	"sync"
	"time"

	"github.com/gofrs/uuid"
)

type ctxKey string

const (
	groupIDKey ctxKey = "groupID"
)

// EventCollector is a collector for events that can be grouped
type EventCollector struct {
	buffer     *LookupRingBuffer[*Event, uuid.UUID]
	openGroups map[uuid.UUID]*Event
	notifier   *Notifier[Event]

	mx sync.RWMutex
}

type EventOptions struct {
	// NotifierOptions are options for notification about new events
	NotifierOptions *NotifierOptions
}

func DefaultEventOptions() EventOptions {
	return EventOptions{}
}

func NewEventCollectorWithOptions(capacity uint64, options EventOptions) *EventCollector {
	notifierOptions := DefaultNotifierOptions()
	if options.NotifierOptions != nil {
		notifierOptions = *options.NotifierOptions
	}

	return &EventCollector{
		buffer:     NewLookupRingBuffer[*Event, uuid.UUID](capacity),
		openGroups: make(map[uuid.UUID]*Event),
		notifier:   NewNotifierWithOptions[Event](notifierOptions),
	}
}

func GroupIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	if groupID, ok := ctx.Value(groupIDKey).(uuid.UUID); ok {
		return groupID, true
	}
	return uuid.Nil, false
}

func WithGroupID(ctx context.Context, groupID uuid.UUID) context.Context {
	return context.WithValue(ctx, groupIDKey, groupID)
}

// CollectEvent directly adds the event to the buffer and notifies subscribers
func (c *EventCollector) CollectEvent(ctx context.Context, data any) {
	eventID := uuid.Must(uuid.NewV7())

	c.mx.Lock()
	defer c.mx.Unlock()

	evt := &Event{
		ID:    eventID,
		Start: time.Now(),
	}

	// Check if the group ID already exists in the context, so we add the event to the outer event as a child
	outerGroupID, ok := GroupIDFromContext(ctx)
	if ok {
		evt.GroupID = &outerGroupID
	}

	evt.Data = data
	evt.End = time.Now()

	// Append the event to the parent event if it exists
	if evt.GroupID != nil {
		outerEvt := c.openGroups[*evt.GroupID]
		if outerEvt != nil {
			outerEvt.Children = append(outerEvt.Children, evt)
		}
	}

	// Add the event to the buffer if it is top-level
	if evt.GroupID == nil {
		c.buffer.Add(evt)
		c.notifier.Notify(*evt)
	}
}

// StartEvent starts a new event and returns a new context with the group ID to group further events added with this context as children of the event to be created.
// Ensure to call EndEvent() to finish the event and collect it for notification.
func (c *EventCollector) StartEvent(ctx context.Context) (newCtx context.Context) {
	eventID := uuid.Must(uuid.NewV7())

	c.mx.Lock()
	defer c.mx.Unlock()

	evt := &Event{
		ID:    eventID,
		Start: time.Now(),
	}

	// Check if the group ID already exists in the context, so we add the event to the outer event as a child
	outerGroupID, ok := GroupIDFromContext(ctx)
	if ok {
		evt.GroupID = &outerGroupID
	}

	c.openGroups[eventID] = evt

	return WithGroupID(ctx, eventID)
}

func (c *EventCollector) EndEvent(ctx context.Context, data any) {
	existingGroupID, ok := GroupIDFromContext(ctx)
	if !ok {
		return
	}

	c.mx.Lock()
	defer c.mx.Unlock()

	evt := c.openGroups[existingGroupID]
	if evt == nil {
		return
	}

	evt.Data = data
	evt.End = time.Now()

	// Append the event to the parent event if it exists
	if evt.GroupID != nil {
		outerEvt := c.openGroups[*evt.GroupID]
		if outerEvt != nil {
			outerEvt.Children = append(outerEvt.Children, evt)
		}
	}

	// Remove the event from the open groups
	delete(c.openGroups, existingGroupID)

	// Add the event to the buffer if it is top-level
	if evt.GroupID == nil {
		c.buffer.Add(evt)
		c.notifier.Notify(*evt)
	}
}

func (c *EventCollector) GetEvents(n uint64) []*Event {
	return c.buffer.GetRecords(n)
}

func (c *EventCollector) GetEvent(id uuid.UUID) (*Event, bool) {
	return c.buffer.Lookup(id)
}

// Subscribe returns a channel that receives notifications of new events
func (c *EventCollector) Subscribe(ctx context.Context) <-chan Event {
	return c.notifier.Subscribe(ctx)
}

// Close releases resources used by the collector
func (c *EventCollector) Close() {
	c.notifier.Close()
}

type Event struct {
	ID uuid.UUID

	GroupID *uuid.UUID

	Data any

	Start time.Time
	End   time.Time

	// Children is a slice of events that are children of this event
	Children []*Event
}

func (e *Event) Visit() iter.Seq2[uuid.UUID, *Event] {
	return func(yield func(uuid.UUID, *Event) bool) {
		e.visitInternal(yield)
	}
}

func (e *Event) visitInternal(yield func(uuid.UUID, *Event) bool) bool {
	if !yield(e.ID, e) {
		return false
	}
	for _, child := range e.Children {
		if !child.visitInternal(yield) {
			return false
		}
	}
	return true
}
