package collector_test

import (
	"fmt"
	"iter"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/networkteam/devlog/collector"
)

type testRecord struct {
	ID   string
	Data string
}

func (r *testRecord) Visit() iter.Seq2[string, *testRecord] {
	return func(yield func(string, *testRecord) bool) {
		yield(r.ID, r)
	}
}

func TestLookupRingBuffer_Basic(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewLookupRingBuffer[*testRecord, string](3)

	// Check initial state
	assert.Equal(t, uint64(0), rb.Size())
	assert.Equal(t, uint64(3), rb.Capacity())

	// Add a record
	rec1 := &testRecord{ID: "1", Data: "data1"}
	rb.Add(rec1)

	// Check size
	assert.Equal(t, uint64(1), rb.Size())

	// Lookup should find the record
	found, exists := rb.Lookup("1")
	assert.True(t, exists)
	assert.Equal(t, rec1, found)

	// GetRecords should return the record
	records := rb.GetRecords(1)
	assert.Len(t, records, 1)
	assert.Equal(t, rec1, records[0])

	// Add more records
	rec2 := &testRecord{ID: "2", Data: "data2"}
	rec3 := &testRecord{ID: "3", Data: "data3"}
	rb.Add(rec2)
	rb.Add(rec3)

	// Check size
	assert.Equal(t, uint64(3), rb.Size())

	// GetRecords should return all records in correct order (newest last)
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, rec1, records[0])
	assert.Equal(t, rec2, records[1])
	assert.Equal(t, rec3, records[2])

	// Lookup should find all records
	found, exists = rb.Lookup("1")
	assert.True(t, exists)
	assert.Equal(t, rec1, found)

	found, exists = rb.Lookup("2")
	assert.True(t, exists)
	assert.Equal(t, rec2, found)

	found, exists = rb.Lookup("3")
	assert.True(t, exists)
	assert.Equal(t, rec3, found)
}

func TestLookupRingBuffer_Overwrite(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewLookupRingBuffer[*testRecord, string](3)

	// Fill the buffer to capacity
	rb.Add(&testRecord{ID: "1", Data: "data1"})
	rb.Add(&testRecord{ID: "2", Data: "data2"})
	rb.Add(&testRecord{ID: "3", Data: "data3"})

	// Buffer is now full
	assert.Equal(t, uint64(3), rb.Size())

	// Add a new record which should overwrite the oldest one
	rb.Add(&testRecord{ID: "4", Data: "data4"})

	// Size should still be at capacity
	assert.Equal(t, uint64(3), rb.Size())

	// Lookup for the overwritten record should fail
	_, exists := rb.Lookup("1")
	assert.False(t, exists, "Record with ID '1' should have been removed from lookup")

	// Lookup for the new record should succeed
	found, exists := rb.Lookup("4")
	assert.True(t, exists)
	assert.Equal(t, "data4", found.Data)

	// GetRecords should return the records in order (newest last)
	records := rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "2", records[0].ID)
	assert.Equal(t, "3", records[1].ID)
	assert.Equal(t, "4", records[2].ID)

	// Add two more records
	rb.Add(&testRecord{ID: "5", Data: "data5"})
	rb.Add(&testRecord{ID: "6", Data: "data6"})

	// Check that older records are no longer in lookup
	_, exists = rb.Lookup("2")
	assert.False(t, exists, "Record with ID '2' should have been removed from lookup")
	_, exists = rb.Lookup("3")
	assert.False(t, exists, "Record with ID '3' should have been removed from lookup")

	// But newer records are
	found, exists = rb.Lookup("4")
	assert.True(t, exists)
	found, exists = rb.Lookup("5")
	assert.True(t, exists)
	found, exists = rb.Lookup("6")
	assert.True(t, exists)

	// Check order of records
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "4", records[0].ID)
	assert.Equal(t, "5", records[1].ID)
	assert.Equal(t, "6", records[2].ID)
}

func TestLookupRingBuffer_GetRecords(t *testing.T) {
	// Create a new ring buffer with capacity 5
	rb := collector.NewLookupRingBuffer[*testRecord, string](5)

	// Add records
	rb.Add(&testRecord{ID: "1", Data: "data1"})
	rb.Add(&testRecord{ID: "2", Data: "data2"})
	rb.Add(&testRecord{ID: "3", Data: "data3"})

	// Get fewer records than available
	records := rb.GetRecords(2)
	assert.Len(t, records, 2)
	assert.Equal(t, "2", records[0].ID)
	assert.Equal(t, "3", records[1].ID)

	// Get more records than available
	records = rb.GetRecords(10)
	assert.Len(t, records, 3)
	assert.Equal(t, "1", records[0].ID)
	assert.Equal(t, "2", records[1].ID)
	assert.Equal(t, "3", records[2].ID)

	// Fill the buffer beyond capacity
	rb.Add(&testRecord{ID: "4", Data: "data4"})
	rb.Add(&testRecord{ID: "5", Data: "data5"})
	rb.Add(&testRecord{ID: "6", Data: "data6"})

	// Check that we get correct records (the latest ones)
	records = rb.GetRecords(5)
	assert.Len(t, records, 5)
	assert.Equal(t, "2", records[0].ID)
	assert.Equal(t, "3", records[1].ID)
	assert.Equal(t, "4", records[2].ID)
	assert.Equal(t, "5", records[3].ID)
	assert.Equal(t, "6", records[4].ID)

	// Get fewer records than available
	records = rb.GetRecords(3)
	assert.Len(t, records, 3)
	assert.Equal(t, "4", records[0].ID)
	assert.Equal(t, "5", records[1].ID)
	assert.Equal(t, "6", records[2].ID)
}

func TestLookupRingBuffer_EmptyBuffer(t *testing.T) {
	// Create a new ring buffer with capacity 3
	rb := collector.NewLookupRingBuffer[*testRecord, string](3)

	// GetRecords on empty buffer should return empty slice
	records := rb.GetRecords(5)
	assert.Empty(t, records)

	// Lookup on empty buffer should return not found
	_, exists := rb.Lookup("nonexistent")
	assert.False(t, exists)
}

func TestLookupRingBuffer_LargeCapacity(t *testing.T) {
	// Create a buffer with large capacity
	capacity := uint64(1000)
	rb := collector.NewLookupRingBuffer[*testRecord, string](capacity)

	// Add many records
	for i := 0; i < 2000; i++ {
		id := fmt.Sprintf("%d", i)
		rb.Add(&testRecord{ID: id, Data: "data-" + id})
	}

	// Check size
	assert.Equal(t, capacity, rb.Size())

	// Check that only the latest records are available
	_, exists := rb.Lookup("999")
	assert.False(t, exists)

	found, exists := rb.Lookup("1999")
	assert.True(t, exists)
	assert.Equal(t, "data-1999", found.Data)

	// Get the latest 10 records
	records := rb.GetRecords(10)
	assert.Len(t, records, 10)
	assert.Equal(t, "1990", records[0].ID)
	assert.Equal(t, "1999", records[9].ID)
}
