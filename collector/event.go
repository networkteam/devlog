package collector

import (
	"iter"
	"time"

	"github.com/gofrs/uuid"
)

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

func (e *Event) free() {
	if freer, ok := e.Data.(interface{ free() }); ok {
		freer.free()
	}
	for _, child := range e.Children {
		child.free()
	}
}
