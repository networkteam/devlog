package collector

import (
	"iter"
	"time"

	"github.com/gofrs/uuid"
)

// Sizer is implemented by event data types to report their memory size
type Sizer interface {
	Size() uint64
}

type Event struct {
	ID uuid.UUID

	GroupID *uuid.UUID

	Data any

	Start time.Time
	End   time.Time

	// Children is a slice of events that are children of this event
	Children []*Event

	// Size is the calculated memory size of this event (excluding children)
	Size uint64
}

// calculateSize computes the memory size of this event (excluding children)
func (e *Event) calculateSize() uint64 {
	const baseEventSize = 100 // UUID, pointers, time.Time fields, slice header
	size := uint64(baseEventSize)
	if sizer, ok := e.Data.(Sizer); ok {
		size += sizer.Size()
	}
	return size
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
